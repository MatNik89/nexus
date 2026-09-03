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
	LookupEnv func(string) (string, bool)
	DataDir   string // resolved nexus data directory
	Detect    func() (probe.Availability, error)
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
	}, nil
}

// Run executes all five prerequisite checks (F1): kernel/backend floor,
// bwrap, data dir, provider key, Telegram token.
func Run(e Env) []Check {
	var checks []Check

	// 1+2. Sandbox backend floor: bwrap present and answering. (The deep
	// hostile suite is the T02 artifact; doctor verifies presence only.)
	if av, err := e.Detect(); err != nil {
		checks = append(checks, Check{
			Name: "sandbox-backend", Capability: "exec", Status: StatusOff,
			Detail: err.Error(),
			Fix:    "sudo apt install bubblewrap   # consented install; exec stays OFF until present",
		})
	} else {
		checks = append(checks, Check{
			Name: "sandbox-backend", Capability: "exec", Status: StatusOK,
			Detail: av.BwrapVersion + " at " + av.BwrapPath,
		})
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
	info, err := os.Stat(dir)
	switch {
	case os.IsNotExist(err):
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			return off("cannot create "+dir+": "+mkErr.Error(), "mkdir -p "+dir+" && chmod 700 "+dir)
		}
		return Check{Name: "data-dir", Capability: "stateful-startup", Status: StatusOK, Detail: dir + " (created, 0700)"}
	case err != nil:
		return off("cannot stat "+dir+": "+err.Error(), "check permissions on "+dir)
	case !info.IsDir():
		return off(dir+" exists but is not a directory", "remove or rename it, then re-run doctor")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return off(fmt.Sprintf("%s permissions %o are too open (need 0700)", dir, info.Mode().Perm()),
			"chmod 700 "+dir)
	}
	probeFile := filepath.Join(dir, ".doctor-write-probe")
	if err := os.WriteFile(probeFile, []byte("ok"), 0o600); err != nil {
		return off(dir+" is not writable: "+err.Error(), "chown/chmod the directory so your user can write")
	}
	os.Remove(probeFile)
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
