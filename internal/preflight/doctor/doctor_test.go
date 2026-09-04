// T03 RED table (tasks-P0): EACH of the five prerequisites individually
// broken must yield its capability-scoped OFF outcome; all five green must
// yield prerequisites-ready. Anchored to HARDQ F1, not to the implementation.
package doctor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/MatNik89/nexus/internal/preflight/probe"
)

func healthyEnv(t *testing.T) Env {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "nexus")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"NEXUS_API_KEY":        "k",
		"NEXUS_TELEGRAM_TOKEN": "t",
	}
	return Env{
		LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		DataDir:   dir,
		Detect: func() (probe.Availability, error) {
			return probe.Availability{BwrapPath: "/usr/bin/bwrap", BwrapVersion: "bubblewrap test"}, nil
		},
		FloorProbe: func(probe.Availability) error { return nil },
	}
}

func offCapability(t *testing.T, checks []Check, capability string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Capability == capability && c.Status == StatusOff {
			if c.Fix == "" {
				t.Fatalf("OFF check %q must carry an exact Fix instruction", c.Name)
			}
			return c
		}
	}
	t.Fatalf("expected capability %q OFF, checks: %+v", capability, checks)
	return Check{}
}

func TestAllGreenIsPrerequisitesReady(t *testing.T) {
	checks := Run(healthyEnv(t))
	if !Ready(checks) {
		t.Fatalf("healthy env must be prerequisites-ready, got: %+v", checks)
	}
}

func TestEachBrokenPrerequisiteScopedOff(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*Env)
		capability string
	}{
		{"bwrap-absent", func(e *Env) {
			e.Detect = func() (probe.Availability, error) {
				return probe.Availability{}, errors.New("SANDBOX_CAPABILITY_UNAVAILABLE: bwrap not found")
			}
		}, "exec"},
		{"kernel-floor-forced-fail", func(e *Env) {
			e.FloorProbe = func(probe.Availability) error {
				return errors.New("SANDBOX_CAPABILITY_UNAVAILABLE: user namespaces disabled")
			}
		}, "exec"},
		{"provider-key-missing", func(e *Env) {
			inner := e.LookupEnv
			e.LookupEnv = func(k string) (string, bool) {
				if k == "NEXUS_API_KEY" {
					return "", false
				}
				return inner(k)
			}
		}, "conversation"},
		{"telegram-token-missing", func(e *Env) {
			inner := e.LookupEnv
			e.LookupEnv = func(k string) (string, bool) {
				if k == "NEXUS_TELEGRAM_TOKEN" {
					return "", false
				}
				return inner(k)
			}
		}, "telegram"},
		{"data-dir-too-open", func(e *Env) {
			if err := os.Chmod(e.DataDir, 0o755); err != nil {
				t.Fatal(err)
			}
		}, "stateful-startup"},
		{"data-dir-not-a-dir", func(e *Env) {
			os.RemoveAll(e.DataDir)
			if err := os.WriteFile(e.DataDir, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, "stateful-startup"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := healthyEnv(t)
			tc.mutate(&env)
			checks := Run(env)
			offCapability(t, checks, tc.capability)
			if Ready(checks) {
				t.Fatal("broken prerequisite must not be prerequisites-ready")
			}
		})
	}
}

// The doctor may never report a capability ON while its prerequisite is
// broken (fail-closed, no degraded-but-on mode).
func TestNoDegradedButOn(t *testing.T) {
	env := healthyEnv(t)
	env.Detect = func() (probe.Availability, error) {
		return probe.Availability{}, errors.New("SANDBOX_CAPABILITY_UNAVAILABLE")
	}
	for _, c := range Run(env) {
		if c.Capability == "exec" && c.Status == StatusOK {
			t.Fatal("exec reported OK while sandbox backend is unavailable")
		}
	}
}

// Scheduler observability is FAIL-CLOSED in doctor (Phase-4-r4 codex #7):
// a malformed counter, an unreadable mirror, and a non-empty health file
// are all OFF — only ENOENT-before-first-fire and a clean state are OK.
func TestSchedulerObservabilityFailClosed(t *testing.T) {
	find := func(checks []Check, name string) Check {
		for _, c := range checks {
			if c.Name == name {
				return c
			}
		}
		t.Fatalf("check %s missing", name)
		return Check{}
	}
	env := func(t *testing.T) Env { return healthyEnv(t) }
	// Baseline: absent files are OK.
	e := env(t)
	checks := Run(e)
	if find(checks, "scheduler-fires").Status != StatusOK || find(checks, "scheduler-health").Status != StatusOK {
		t.Fatal("absent scheduler files must be OK before the first fire")
	}
	// Malformed counter → OFF.
	e2 := env(t)
	os.MkdirAll(filepath.Join(e2.DataDir, "system"), 0o700)
	os.WriteFile(filepath.Join(e2.DataDir, "system", "last_occurrence_fired"), []byte("not-a-number\n"), 0o600)
	if find(Run(e2), "scheduler-fires").Status != StatusOff {
		t.Fatal("malformed counter mirror reported OK")
	}
	// Non-empty health file → OFF.
	e3 := env(t)
	os.MkdirAll(filepath.Join(e3.DataDir, "system"), 0o700)
	os.WriteFile(filepath.Join(e3.DataDir, "system", "scheduler_health"), []byte("schedule: counter mirror: boom"), 0o600)
	if find(Run(e3), "scheduler-health").Status != StatusOff {
		t.Fatal("recorded sweep failure reported OK")
	}
	// Unreadable health state (path is a DIRECTORY) → OFF.
	e4 := env(t)
	os.MkdirAll(filepath.Join(e4.DataDir, "system", "scheduler_health"), 0o700)
	if find(Run(e4), "scheduler-health").Status != StatusOff {
		t.Fatal("unreadable health mirror reported OK")
	}
}

// A live-daemon heartbeat with a MISSING health mirror is OFF — the
// startup health write failed (Phase-4-r5 codex #4).
func TestLiveDaemonMissingHealthMirrorIsOff(t *testing.T) {
	e := healthyEnv(t)
	sys := filepath.Join(e.DataDir, "system")
	os.MkdirAll(sys, 0o700)
	os.WriteFile(filepath.Join(sys, "heartbeat"), []byte("beat"), 0o600) // fresh mtime
	got := Check{}
	for _, c := range Run(e) {
		if c.Name == "scheduler-health" {
			got = c
		}
	}
	if got.Status != StatusOff {
		t.Fatalf("live daemon without a health mirror reported %v", got)
	}
}
