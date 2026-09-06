// Automated T01 RED (Phase-0 review kilo #6 / codex #11): the static-check
// gate must (a) fail on a dynamically linked binary, (b) refuse to decide on
// a non-ELF input, (c) pass the real CGO_ENABLED=0 nexus build.
package buildcheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	// Source-tree-relative, NOT git-dependent (fresh-audit codex #5): a
	// clean `git archive` export must still run these oracles.
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("repo root: runtime.Caller failed")
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(self)))
	if _, err := os.Stat(filepath.Join(root, "scripts", "static-check.sh")); err != nil {
		t.Fatalf("repo root %s does not hold scripts/static-check.sh: %v", root, err)
	}
	return root
}

func runCheck(t *testing.T, root, target string) (int, string) {
	t.Helper()
	cmd := exec.Command(filepath.Join(root, "scripts", "static-check.sh"), target)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return 0, string(out)
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(out)
	}
	t.Fatalf("running static-check: %v", err)
	return -1, ""
}

func TestStaticCheckPassesStaticNexusBinary(t *testing.T) {
	root := repoRoot(t)
	bin := filepath.Join(t.TempDir(), "nexus")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/MatNik89/nexus/cmd/nexus")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	if code, out := runCheck(t, root, bin); code != 0 {
		t.Fatalf("static nexus build must pass, exit=%d\n%s", code, out)
	}
}

func TestStaticCheckFailsDynamicBinary(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only fixture")
	}
	// Any dynamically linked system binary serves as the negative fixture —
	// no cgo toolchain needed in CI.
	fixture := "/bin/ls"
	if _, err := os.Stat(fixture); err != nil {
		t.Skip("no /bin/ls fixture")
	}
	root := repoRoot(t)
	code, out := runCheck(t, root, fixture)
	if code != 1 {
		t.Fatalf("dynamic binary must yield exit 1, got %d\n%s", code, out)
	}
}

func TestStaticCheckRefusesNonELF(t *testing.T) {
	root := repoRoot(t)
	f := filepath.Join(t.TempDir(), "not-elf.txt")
	if err := os.WriteFile(f, []byte("just text\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runCheck(t, root, f); code != 2 {
		t.Fatalf("non-ELF input must yield exit 2 (cannot decide), got %d\n%s", code, out)
	}
}
