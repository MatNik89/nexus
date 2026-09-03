package pathx

import (
	"strings"
	"testing"
)

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
	if _, err := l.ProfileDir("bad\x00id"); err == nil {
		t.Fatal("invalid profile id accepted")
	}
	if _, err := l.ProfileDir(""); err == nil {
		t.Fatal("empty profile id accepted")
	}
}
