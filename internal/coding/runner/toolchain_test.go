//go:build linux

package runner

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func realGoBinary(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("no go binary on PATH: %v", err)
	}
	return path
}

// Detector: against the REAL go toolchain on this host, ResolveToolchain
// returns non-empty binds/env/digest, and the binds are actual existing
// directories (not just plausible-looking strings).
func TestResolveToolchainAgainstRealHost(t *testing.T) {
	goBin := realGoBinary(t)
	pin, err := ResolveToolchain(goBin)
	if err != nil {
		t.Fatal(err)
	}
	if len(pin.ROBinds) != 2 {
		t.Fatalf("ROBinds = %v, want exactly 2 (GOROOT, GOMODCACHE)", pin.ROBinds)
	}
	for _, dir := range pin.ROBinds {
		if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
			t.Fatalf("ROBind %s is not a real directory: %v", dir, statErr)
		}
	}
	if pin.Env["CGO_ENABLED"] != "0" {
		t.Fatalf("CGO_ENABLED = %q, want \"0\"", pin.Env["CGO_ENABLED"])
	}
	if pin.Env["GOTOOLCHAIN"] != "local" {
		t.Fatalf("GOTOOLCHAIN = %q, want \"local\"", pin.Env["GOTOOLCHAIN"])
	}
	if pin.Env["GOROOT"] == "" {
		t.Fatal("GOROOT env not set")
	}
	if pin.HashDigest == "" {
		t.Fatal("empty HashDigest")
	}
}

// Detector: the hash digest is content-sensitive — two DIFFERENT
// GOTOOLDIR trees (one with a tampered "binary") produce different
// digests, and re-hashing the SAME unchanged tree is deterministic.
func TestHashToolchainExecutablesIsDeterministicAndContentSensitive(t *testing.T) {
	goroot := t.TempDir()
	gotooldir := filepath.Join(goroot, "pkg", "tool", "linux_arm64")
	if err := os.MkdirAll(gotooldir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(goroot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExec(filepath.Join(gotooldir, "compile"), "COMPILE-V1")
	writeExec(filepath.Join(goroot, "bin", "go"), "GO-V1")
	writeExec(filepath.Join(goroot, "bin", "gofmt"), "GOFMT-V1")

	d1, err := hashToolchainExecutables(goroot, gotooldir)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := hashToolchainExecutables(goroot, gotooldir)
	if err != nil {
		t.Fatal(err)
	}
	if d1 != d2 {
		t.Fatalf("re-hashing the same unchanged tree produced different digests: %s vs %s", d1, d2)
	}

	// Tamper with bin/go (the entry point, per codex's finding — not part
	// of GOTOOLDIR itself).
	writeExec(filepath.Join(goroot, "bin", "go"), "GO-V2-TAMPERED")
	d3, err := hashToolchainExecutables(goroot, gotooldir)
	if err != nil {
		t.Fatal(err)
	}
	if d1 == d3 {
		t.Fatal("tampering with bin/go did not change the digest — the entry point is not actually covered")
	}
}

// Detector: an unresolvable go binary fails closed, never silently
// returns an empty/zero-value pin.
func TestResolveToolchainFailsClosedOnBadBinary(t *testing.T) {
	if _, err := ResolveToolchain("/nonexistent/go/binary"); err == nil {
		t.Fatal("ResolveToolchain accepted a nonexistent go binary")
	}
}
