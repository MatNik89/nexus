package pathx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

func contractsProfile(s string) contracts.ProfileID { return contracts.ProfileID(s) }

func TestProfilePathsIsolatedPerProfile(t *testing.T) {
	l := Layout{Base: "/tmp/nexus-test"}
	work, err := l.ProfileJournal("work")
	if err != nil {
		t.Fatal(err)
	}
	private, err := l.ProfileJournal("private")
	if err != nil {
		t.Fatal(err)
	}
	if work == private {
		t.Fatal("profiles share a journal path (B3 physical isolation broken)")
	}
	if !strings.Contains(work, "/profiles/work/") || !strings.Contains(private, "/profiles/private/") {
		t.Fatalf("unexpected layout: %s / %s", work, private)
	}
	if strings.Contains(l.SystemDir(), "profiles") {
		t.Fatal("system dir must live outside profile subtrees")
	}
}

func TestInvalidProfileRefused(t *testing.T) {
	l := Layout{Base: "/tmp/x"}
	for _, bad := range []string{"bad\x00id", "", "../system", "a/b", "..", ".", "a.b", "sneaky/../../etc"} {
		if _, err := l.ProfileDir(contractsProfile(bad)); err == nil {
			t.Fatalf("traversal-capable profile id accepted: %q", bad)
		}
	}
}

// EnsureDir refuses unsafe pre-existing directories and symlinks
// (Phase-1B codex #15 literals).
func TestEnsureDirRefusesUnsafeExisting(t *testing.T) {
	base := t.TempDir()
	open := filepath.Join(base, "open")
	if err := os.Mkdir(open, 0o777); err != nil {
		t.Fatal(err)
	}
	os.Chmod(open, 0o777)
	if err := EnsureDir(base, open); err == nil {
		t.Fatal("pre-existing 0777 directory accepted")
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(base, link); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDir(base, link); err == nil {
		t.Fatal("symlinked path accepted")
	}
	good := filepath.Join(base, "good")
	if err := EnsureDir(base, good); err != nil {
		t.Fatalf("fresh private dir refused: %v", err)
	}
}

// A symlinked PARENT component cannot redirect storage: the no-follow walk
// refuses it even when the FINAL directory is real, private, and owned
// (Phase-1B-r2 codex #11 literal).
func TestEnsureDirRefusesSymlinkedParent(t *testing.T) {
	base := t.TempDir()
	elsewhere := t.TempDir()
	link := filepath.Join(base, "redirect")
	if err := os.Symlink(elsewhere, link); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDir(base, filepath.Join(link, "child")); err == nil {
		t.Fatal("directory under a symlinked parent accepted")
	}
	// Escape via Rel: a target outside the root is refused outright.
	if err := EnsureDir(base, elsewhere); err == nil {
		t.Fatal("target outside the trusted root accepted")
	}
	// Nested creation below the root still works (every component verified).
	deep := filepath.Join(base, "a", "b", "c")
	if err := EnsureDir(base, deep); err != nil {
		t.Fatalf("legitimate nested dir refused: %v", err)
	}
}
