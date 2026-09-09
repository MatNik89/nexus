// Package doctor implements the install/first-start preflight (tasks-P0 T03,
// HARDQ F1). Results are capability-scoped: a missing prerequisite turns its
// capability OFF (fail-closed) and never yields a degraded-but-on mode. The
// final label here is at most "prerequisites-ready"; "P0-capable" is granted
// only by the T27 acceptance run against the live criteria.
package doctor

import (
	"fmt"
	"github.com/MatNik89/nexus/internal/channel/health"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MatNik89/nexus/internal/preflight/probe"
)

type Status int

const (
	StatusOK Status = iota
	StatusOff
)

// Check is one prerequisite result. Capability names the thing that is OFF
// when the check fails; Fix is the exact consented command or instruction.
type Check struct {
	Name       string
	Capability string
	Status     Status
	Detail     string
	Fix        string
}

// Env abstracts the host so the RED table can break each prerequisite
// independently.
type Env struct {
	LookupEnv  func(string) (string, bool)
	DataDir    string // resolved nexus data directory
	Detect     func() (probe.Availability, error)
	FloorProbe func(probe.Availability) error
	// Secrets names the env vars that hold the secrets, RESOLVED from the
	// typed configuration (Slice E, AUDIT-FULL F7) — doctor never hardcodes
	// NEXUS_API_KEY / NEXUS_TELEGRAM_TOKEN: a custom provider_key_env must
	// not report a false OFF, and a populated DEFAULT name must not report
	// a false ON when the configuration points elsewhere.
	Secrets Secrets
}

// Secrets is the pair of configured secret-reference names
// (config.Config.ProviderKeyEnv / TelegramTokenEnv). An empty name is a
// misconfiguration and reports the capability OFF (fail closed).
type Secrets struct {
	ProviderKeyEnv   string
	TelegramTokenEnv string
}

// DefaultEnv resolves the real host environment for the given configured
// secret names.
func DefaultEnv(secrets Secrets) (Env, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Env{}, fmt.Errorf("cannot resolve user config dir: %w", err)
	}
	return Env{
		LookupEnv: os.LookupEnv,
		Secrets:   secrets,
		DataDir:   filepath.Join(base, "nexus"),
		Detect:    probe.Detect,
		// The floor probe runs the REAL production launch path against this
		// very binary (static ELF): its hidden __probe-ptrace subcommand
		// must be DENIED by the syscall floor inside the sandbox.
		FloorProbe: func(av probe.Availability) error {
			return probe.FloorProbe(av, "/proc/self/exe", []string{"__probe-ptrace"})
		},
	}, nil
}

// Run executes all five prerequisite checks (F1): kernel/backend floor,
// bwrap, data dir, provider key, Telegram token.
func Run(e Env) []Check {
	var checks []Check

	// 1. Sandbox backend present + at/above the published version floor.
	av, detectErr := e.Detect()
	if detectErr != nil {
		checks = append(checks, Check{
			Name: "sandbox-backend", Capability: "exec", Status: StatusOff,
			Detail: detectErr.Error(),
			Fix:    "sudo apt install bubblewrap   # consented install; exec stays OFF until present",
		})
	} else {
		checks = append(checks, Check{
			Name: "sandbox-backend", Capability: "exec", Status: StatusOK,
			Detail: av.BwrapVersion + " at " + av.BwrapPath,
		})
	}

	// 2. Kernel floor: the backend must actually be able to CREATE a sandbox
	// (user namespaces usable). bwrap --version alone proves nothing
	// (Phase-0 review kilo #1 / codex #6).
	switch {
	case detectErr != nil:
		checks = append(checks, Check{
			Name: "sandbox-floor", Capability: "exec", Status: StatusOff,
			Detail: "not probed: backend unavailable",
			Fix:    "install bubblewrap first, then re-run doctor",
		})
	default:
		if err := e.FloorProbe(av); err != nil {
			checks = append(checks, Check{
				Name: "sandbox-floor", Capability: "exec", Status: StatusOff,
				Detail: err.Error(),
				Fix:    "enable unprivileged user namespaces (sysctl kernel.unprivileged_userns_clone=1 or distro equivalent)",
			})
		} else {
			checks = append(checks, Check{Name: "sandbox-floor", Capability: "exec", Status: StatusOK, Detail: "confined launch succeeded"})
		}
	}

	// 3. Data directory: exists (or creatable), private, writable.
	checks = append(checks, checkDataDir(e.DataDir))

	// 4. Provider key → conversation capability (the CONFIGURED name).
	checks = append(checks, checkSecret(e, "provider-key", "conversation",
		e.Secrets.ProviderKeyEnv, "provider_key_env", "<your provider API key>"))

	// 6 (C8 observability): scheduler last_occurrence_fired counter — an
	// informational mirror the daemon writes after every fire; absent
	// before the first fire (that is not a failure).
	counterPath := filepath.Join(e.DataDir, "system", "last_occurrence_fired")
	if b, err := os.ReadFile(counterPath); err == nil {
		// STRICT parse (Phase-4-r2 codex #11): the mirror must be a bare
		// counter — anything else is corruption, not health.
		v := strings.TrimSpace(string(b))
		if _, perr := strconv.ParseUint(v, 10, 64); perr == nil {
			checks = append(checks, Check{Name: "scheduler-fires", Capability: "obligations",
				Status: StatusOK, Detail: "last_occurrence_fired=" + v})
		} else {
			checks = append(checks, Check{Name: "scheduler-fires", Capability: "obligations",
				Status: StatusOff, Detail: "counter mirror is malformed",
				Fix: "remove " + counterPath + "; the next sweep rewrites it from the journal projection"})
		}
	} else if os.IsNotExist(err) {
		checks = append(checks, Check{Name: "scheduler-fires", Capability: "obligations",
			Status: StatusOK, Detail: "no occurrences fired yet (counter file absent)"})
	} else {
		// EACCES/I/O is NOT "nothing fired" — it is broken observability.
		checks = append(checks, Check{Name: "scheduler-fires", Capability: "obligations",
			Status: StatusOff, Detail: "counter mirror unreadable: " + err.Error(),
			Fix: "fix permissions on " + counterPath})
	}

	// 6b: scheduler health mirror — a non-empty file is a live failure.
	healthPath := filepath.Join(e.DataDir, "system", "scheduler_health")
	if hb, herr := os.ReadFile(healthPath); herr == nil {
		if v := strings.TrimSpace(string(hb)); v != "" {
			checks = append(checks, Check{Name: "scheduler-health", Capability: "obligations",
				Status: StatusOff, Detail: "last sweep failed: " + v,
				Fix: "inspect the daemon log; the next successful sweep clears this"})
		} else {
			checks = append(checks, Check{Name: "scheduler-health", Capability: "obligations",
				Status: StatusOK, Detail: "no sweep failures recorded"})
		}
	} else if os.IsNotExist(herr) {
		// A LIVE daemon (fresh heartbeat) with NO health mirror means the
		// startup health write failed — that is broken observability, not
		// a never-started daemon (Phase-4-r5 codex #4).
		hbPath := filepath.Join(e.DataDir, "system", "heartbeat")
		if info, hbErr := os.Stat(hbPath); hbErr == nil && time.Since(info.ModTime()) < time.Minute {
			checks = append(checks, Check{Name: "scheduler-health", Capability: "obligations",
				Status: StatusOff, Detail: "daemon is live but its health mirror is missing",
				Fix: "inspect daemon stderr; the mirror write is failing"})
		} else {
			checks = append(checks, Check{Name: "scheduler-health", Capability: "obligations",
				Status: StatusOK, Detail: "no health mirror yet (daemon not started)"})
		}
	} else {
		// EACCES/EISDIR/I-O is broken observability, never health
		// (Phase-4-r4 codex #7).
		checks = append(checks, Check{Name: "scheduler-health", Capability: "obligations",
			Status: StatusOff, Detail: "health mirror unreadable: " + herr.Error(),
			Fix: "fix permissions on " + healthPath})
	}

	// 6c (Slice D): channel runtime health — the adapter's journal-independent
	// projection. A recorded non-healthy component reports its typed class;
	// a malformed/unreadable projection is broken observability (OFF).
	chPath := filepath.Join(e.DataDir, "system", "channel_health.json")
	if entries, herr := health.Read(chPath); herr != nil {
		checks = append(checks, Check{Name: "channel-health", Capability: "telegram", Status: StatusOff,
			Detail: "channel health projection unreadable: " + herr.Error(), Fix: "fix or remove " + chPath})
	} else {
		bad := 0
		for _, en := range entries {
			if !en.Healthy {
				bad++
				checks = append(checks, Check{Name: "channel-health", Capability: "telegram", Status: StatusOff,
					Detail: fmt.Sprintf("%s %s/%s at %s (stopped=%v): %s", en.Component, en.Class, en.Code, en.At.Format(time.RFC3339), en.Stopped, en.Detail),
					Fix:    "inspect the daemon log; repair (token/config/journal) then restart the daemon"})
			}
		}
		if bad == 0 {
			checks = append(checks, Check{Name: "channel-health", Capability: "telegram", Status: StatusOK, Detail: "no unhealthy channel component recorded"})
		}
	}

	// 5. Telegram token → telegram capability (the CONFIGURED name).
	checks = append(checks, checkSecret(e, "telegram-token", "telegram",
		e.Secrets.TelegramTokenEnv, "telegram_token_env", "<bot token from @BotFather>"))

	return checks
}

// checkSecret reports one secret-backed capability from the CONFIGURED env
// var name: no name -> OFF (misconfiguration), name unset/empty -> OFF with
// the exact export to run, otherwise OK. The value itself is never echoed.
func checkSecret(e Env, name, capability, envName, configKey, placeholder string) Check {
	if envName == "" {
		return Check{Name: name, Capability: capability, Status: StatusOff,
			Detail: "no env var name configured (" + configKey + " is empty)",
			Fix:    "set " + configKey + " in config.json (or NEXUS_CFG_" + strings.ToUpper(configKey) + ")"}
	}
	if v, ok := e.LookupEnv(envName); !ok || v == "" {
		return Check{Name: name, Capability: capability, Status: StatusOff,
			Detail: envName + " is not set",
			Fix:    "export " + envName + "=" + placeholder}
	}
	return Check{Name: name, Capability: capability, Status: StatusOK, Detail: envName + " set"}
}

func checkDataDir(dir string) Check {
	off := func(detail, fix string) Check {
		return Check{Name: "data-dir", Capability: "stateful-startup", Status: StatusOff, Detail: detail, Fix: fix}
	}
	// Lstat first: a symlinked data directory is rejected outright — a stale
	// or hostile link would redirect every later store write (codex #10).
	linfo, lerr := os.Lstat(dir)
	switch {
	case os.IsNotExist(lerr):
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			return off("cannot create "+dir+": "+mkErr.Error(), "mkdir -p "+dir+" && chmod 700 "+dir)
		}
		return Check{Name: "data-dir", Capability: "stateful-startup", Status: StatusOK, Detail: dir + " (created, 0700)"}
	case lerr != nil:
		return off("cannot lstat "+dir+": "+lerr.Error(), "check permissions on "+dir)
	case linfo.Mode()&os.ModeSymlink != 0:
		return off(dir+" is a symlink — refused", "remove the symlink and create a real directory")
	case !linfo.IsDir():
		return off(dir+" exists but is not a directory", "remove or rename it, then re-run doctor")
	}
	if linfo.Mode().Perm()&0o077 != 0 {
		return off(fmt.Sprintf("%s permissions %o are too open (need 0700)", dir, linfo.Mode().Perm()),
			"chmod 700 "+dir)
	}
	// Randomized, exclusive, no-follow write probe; failed cleanup = OFF.
	probeFile, err := os.CreateTemp(dir, ".doctor-probe-*")
	if err != nil {
		return off(dir+" is not writable: "+err.Error(), "chown/chmod the directory so your user can write")
	}
	name := probeFile.Name()
	probeFile.Close()
	if err := os.Remove(name); err != nil {
		return off("cannot clean probe file in "+dir+": "+err.Error(), "check ownership of "+dir)
	}
	return Check{Name: "data-dir", Capability: "stateful-startup", Status: StatusOK, Detail: dir}
}

// Ready reports whether every prerequisite is OK ("prerequisites-ready").
func Ready(checks []Check) bool {
	for _, c := range checks {
		if c.Status != StatusOK {
			return false
		}
	}
	return true
}
