// Package doctor implements the install/first-start preflight (tasks-P0 T03,
// HARDQ F1). Results are capability-scoped: a missing prerequisite turns its
// capability OFF (fail-closed) and never yields a degraded-but-on mode. The
// final label here is at most "prerequisites-ready"; "P0-capable" is granted
// only by the T27 acceptance run against the live criteria.
package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
}

// DefaultEnv resolves the real host environment.
func DefaultEnv() (Env, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Env{}, fmt.Errorf("cannot resolve user config dir: %w", err)
	}
	return Env{
		LookupEnv: os.LookupEnv,
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

	// 4. Provider key → conversation capability.
	if v, ok := e.LookupEnv("NEXUS_API_KEY"); !ok || v == "" {
		checks = append(checks, Check{
			Name: "provider-key", Capability: "conversation", Status: StatusOff,
			Detail: "NEXUS_API_KEY is not set",
			Fix:    "export NEXUS_API_KEY=<your provider API key>",
		})
	} else {
		checks = append(checks, Check{Name: "provider-key", Capability: "conversation", Status: StatusOK, Detail: "set"})
	}

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
		checks = append(checks, Check{Name: "scheduler-health", Capability: "obligations",
			Status: StatusOK, Detail: "no health mirror yet (daemon not started)"})
	} else {
		// EACCES/EISDIR/I-O is broken observability, never health
		// (Phase-4-r4 codex #7).
		checks = append(checks, Check{Name: "scheduler-health", Capability: "obligations",
			Status: StatusOff, Detail: "health mirror unreadable: " + herr.Error(),
			Fix: "fix permissions on " + healthPath})
	}

	// 5. Telegram token → telegram capability.
	if v, ok := e.LookupEnv("NEXUS_TELEGRAM_TOKEN"); !ok || v == "" {
		checks = append(checks, Check{
			Name: "telegram-token", Capability: "telegram", Status: StatusOff,
			Detail: "NEXUS_TELEGRAM_TOKEN is not set",
			Fix:    "export NEXUS_TELEGRAM_TOKEN=<bot token from @BotFather>",
		})
	} else {
		checks = append(checks, Check{Name: "telegram-token", Capability: "telegram", Status: StatusOK, Detail: "set"})
	}

	return checks
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
