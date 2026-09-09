package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Slice F (AUDIT-FULL F3): the release/acceptance path signed a binary built
// with Go 1.26.4 that carried five reachable stdlib advisories (fix floor
// 1.26.6). These are WHOLE-SCRIPT detectors: p0-accept.sh and deploy.sh run
// end to end against PATH shims, and the assertion is on the externally
// visible sinks — the signature, the trust pointer, the install target, the
// service restart — never on a helper in isolation (plan-review r2 codex #8).

// gateShims builds a PATH directory whose `go`, `govulncheck`, `ssh-keygen`,
// `install`, `systemctl` and `journalctl` are scripted; every effectful sink
// leaves a marker file under markDir so a test can prove it was NEVER reached.
func gateShims(t *testing.T, goVersion string, vulnExit int, withVuln bool) (shimDir, markDir string) {
	t.Helper()
	base := t.TempDir()
	shimDir = filepath.Join(base, "shim")
	markDir = filepath.Join(base, "marks")
	for _, d := range []string{shimDir, markDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(shimDir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// `go build -o X` writes a stand-in binary; `go version X` reports the
	// scripted toolchain; `go test`/`vet` succeed silently.
	write("go", `case "$1" in
build) out=""; while [ $# -gt 0 ]; do [ "$1" = "-o" ] && out="$2"; shift; done; printf 'stand-in nexus binary\n' > "$out" ;;
version) if [ -n "${2:-}" ] && [ -f "$2" ]; then echo "$2: `+goVersion+`"; else echo "go version `+goVersion+` linux/arm64"; fi ;;
*) exit 0 ;;
esac
`)
	if withVuln {
		write("govulncheck", `echo "govulncheck shim: mode=$*"
if [ `+itoa(vulnExit)+` -ne 0 ]; then echo "Vulnerability #1: GO-2026-6218"; fi
exit `+itoa(vulnExit)+`
`)
	}
	write("ssh-keygen", `touch "$SHIM_MARK/sign"; last=""; for a in "$@"; do last="$a"; done; printf 'sig\n' > "$last.sig"; exit 0`)
	write("install", `touch "$SHIM_MARK/install"; exit 0`)
	write("systemctl", `echo "$*" >> "$SHIM_MARK/systemctl"; exit 0`)
	write("journalctl", `echo "nexus daemon: telegram adapter running (sealed capability ON)"`)
	return shimDir, markDir
}

func itoa(i int) string { return strings.TrimSpace(strings.Repeat("", 0) + string(rune('0'+i))) }

// gateEnv is a minimal, HERMETIC environment: the shim dir first, then only
// the system dirs (never $HOME/go/bin, so a real govulncheck cannot leak in).
func gateEnv(t *testing.T, shimDir, markDir string, extra ...string) []string {
	t.Helper()
	conf := t.TempDir()
	key := filepath.Join(t.TempDir(), "release_key")
	for _, p := range []string{key, key + ".pub"} {
		if err := os.WriteFile(p, []byte("ssh-ed25519 AAAA test\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := []string{
		"PATH=" + shimDir + ":/usr/bin:/bin",
		"HOME=" + conf,
		"XDG_CONFIG_HOME=" + conf,
		"SHIM_MARK=" + markDir,
		"NEXUS_RELEASE_KEY=" + key,
		"NEXUS_INSTALL_TO=" + filepath.Join(conf, "installed-nexus"),
		"NEXUS_DEPLOY_SETTLE_SECONDS=0",
	}
	return append(env, extra...)
}

func markReached(markDir, name string) bool {
	_, err := os.Stat(filepath.Join(markDir, name))
	return err == nil
}

func runScript(t *testing.T, script string, env []string) (string, error) {
	t.Helper()
	cmd := exec.Command("sh", script)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The acceptance script must refuse to sign/publish/install when the built
// binary's toolchain is below the floor or govulncheck reports a finding or
// is missing; a clean run must still reach the sign+publish+install sinks
// (the "not always-fail" control).
func TestAcceptReleaseGateBlocksSignAndPublish(t *testing.T) {
	root := repoRootFromCaller(t)
	script := filepath.Join(root, "scripts", "p0-accept.sh")
	cases := []struct {
		name      string
		goVersion string
		vulnExit  int
		withVuln  bool
		wantPass  bool
	}{
		{"old-toolchain", "go1.26.4", 0, true, false}, // the audited case (F3 literal: 1.26.4 < 1.26.6)
		{"scanner-finding", "go1.26.6", 3, true, false},
		{"scanner-missing", "go1.26.6", 0, false, false},
		{"unparsable-version", "devel", 0, true, false},
		{"clean", "go1.26.6", 0, true, true},
		{"newer-clean", "go1.27.0", 0, true, true},
		// The caller environment must NOT be able to lower the floor
		// (code-review r1 codex #1): a hostile RELEASE_GO_FLOOR with the
		// audited 1.26.4 binary is still refused.
		{"env-lowered-floor", "go1.26.4", 0, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shim, marks := gateShims(t, tc.goVersion, tc.vulnExit, tc.withVuln)
			env := gateEnv(t, shim, marks)
			if tc.name == "env-lowered-floor" {
				env = append(env, "RELEASE_GO_FLOOR=1.0.0")
			}
			var conf string
			for _, e := range env {
				if strings.HasPrefix(e, "XDG_CONFIG_HOME=") {
					conf = strings.TrimPrefix(e, "XDG_CONFIG_HOME=")
				}
			}
			out, err := runScript(t, script, env)
			signed := markReached(marks, "sign")
			installed := markReached(marks, "install")
			_, ptrErr := os.Readlink(filepath.Join(conf, "nexus", "trust", "current"))
			published := ptrErr == nil
			if tc.wantPass {
				if err != nil {
					t.Fatalf("clean run failed (gate is always-fail?): %v\n%s", err, out)
				}
				if !signed || !published || !installed {
					t.Fatalf("clean run did not reach sign=%v publish=%v install=%v\n%s", signed, published, installed, out)
				}
				return
			}
			if err == nil {
				t.Fatalf("gate did not fail the script\n%s", out)
			}
			if signed || published || installed {
				t.Fatalf("gate failed but a sink was reached: sign=%v publish=%v install=%v\n%s", signed, published, installed, out)
			}
		})
	}
}

// deploy.sh (autodeploy) runs the SAME gate: an old toolchain or a finding
// must leave the installed binary and the service untouched; a clean run
// installs, restarts and verifies the capability line.
func TestDeployReleaseGateBlocksInstallAndRestart(t *testing.T) {
	root := repoRootFromCaller(t)
	script := filepath.Join(root, "scripts", "deploy.sh")
	if _, err := os.Stat(script); err != nil {
		t.Fatalf("deploy.sh missing: %v", err)
	}
	cases := []struct {
		name      string
		goVersion string
		vulnExit  int
		wantPass  bool
	}{
		{"old-toolchain", "go1.26.4", 0, false},
		{"scanner-finding", "go1.26.6", 3, false},
		{"clean", "go1.26.6", 0, true},
		{"env-lowered-floor", "go1.26.4", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shim, marks := gateShims(t, tc.goVersion, tc.vulnExit, true)
			env := gateEnv(t, shim, marks)
			if tc.name == "env-lowered-floor" {
				env = append(env, "RELEASE_GO_FLOOR=1.0.0")
			}
			out, err := runScript(t, script, env)
			installed := markReached(marks, "install")
			restarted := markReached(marks, "systemctl")
			if tc.wantPass {
				if err != nil {
					t.Fatalf("clean deploy failed: %v\n%s", err, out)
				}
				if !installed || !restarted {
					t.Fatalf("clean deploy did not install=%v/restart=%v\n%s", installed, restarted, out)
				}
				log, _ := os.ReadFile(filepath.Join(marks, "systemctl"))
				if !strings.Contains(string(log), "start") {
					t.Fatalf("service was not started: %q", log)
				}
				return
			}
			if err == nil {
				t.Fatalf("deploy gate did not fail\n%s", out)
			}
			if installed || restarted {
				t.Fatalf("deploy gate failed but install=%v/restart=%v was reached\n%s", installed, restarted, out)
			}
		})
	}
}

// The module pins the toolchain at or above the floor the gate enforces, so
// GOTOOLCHAIN=auto builds with a fixed Go even on a host whose system Go is
// older (the Pi runs 1.26.4).
func TestGoModToolchainMeetsGateFloor(t *testing.T) {
	root := repoRootFromCaller(t)
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^toolchain go(\d+\.\d+\.\d+)\s*$`).FindStringSubmatch(string(mod))
	if m == nil {
		t.Fatalf("go.mod has no `toolchain goX.Y.Z` directive (F3: the ambient 1.26.4 host would build the release)")
	}
	gate, err := os.ReadFile(filepath.Join(root, "scripts", "lib", "release-gate.sh"))
	if err != nil {
		t.Fatalf("release-gate.sh missing: %v", err)
	}
	f := regexp.MustCompile(`(?m)^readonly RELEASE_GO_FLOOR=(\d+\.\d+\.\d+)\s*$`).FindStringSubmatch(string(gate))
	if f == nil {
		t.Fatal("release-gate.sh does not declare an unconditional readonly RELEASE_GO_FLOOR (an env override would lower the floor)")
	}
	if f[1] != "1.26.6" {
		t.Fatalf("gate floor is %s; the audit fix floor is 1.26.6", f[1])
	}
	if !versionGE(m[1], f[1]) {
		t.Fatalf("go.mod toolchain %s is below the gate floor %s", m[1], f[1])
	}
}

func versionGE(a, b string) bool {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		var x, y int
		for _, c := range pa[i] {
			x = x*10 + int(c-'0')
		}
		for _, c := range pb[i] {
			y = y*10 + int(c-'0')
		}
		if x != y {
			return x > y
		}
	}
	return true
}
