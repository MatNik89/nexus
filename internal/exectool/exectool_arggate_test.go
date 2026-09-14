//go:build linux

// S6.1 per-argument PEP gating: exec_allow only ever admits a LEXICALLY
// CLEAN path, on both the config side (New) and the request side
// (parseArgs, shared by Launch and ArgGate) — closing the live-
// reproduced symlink+".." divergence from round 3's code review, where
// "/safe/link/../git" (link -> /evil/x) cleans lexically to "/safe/git"
// while the kernel actually opens "/evil/git".
package exectool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/sandbox"
	"github.com/MatNik89/nexus/internal/security/redact"
)

// New refuses a non-lexically-clean exec_allow entry outright — it must
// never silently filepath.Clean it into a DIFFERENT key than what was
// configured.
func TestNewRejectsNonCanonicalExecAllowEntry(t *testing.T) {
	b := sandbox.NewBwrap()
	rep, err := b.Probe(ctxT())
	if err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}
	if _, err := New(b, rep, redact.None{}, []string{"/safe/link/../git"}); err == nil {
		t.Fatal("non-canonical exec_allow entry accepted")
	}
}

// A perfectly clean entry is stored verbatim and matches itself — the
// ordinary, non-adversarial case must keep working unchanged.
func TestNewAcceptsCleanExecAllowEntry(t *testing.T) {
	a := adapter(t) // testAllow(t) entries are already clean absolute paths
	c := execCall(t, "tc-clean", testAllow(t)[0], nil, contracts.ExecProcess, contracts.EffectIrreversible)
	if err := a.ArgGate(c.Arguments); err != nil {
		t.Fatalf("a clean, allowlisted command was denied: %v", err)
	}
}

// parseArgs (hence both Launch and ArgGate) refuses ANY non-lexically-
// clean command spelling, independent of whether a real symlink exists —
// closing the class by construction rather than by resolving paths.
func TestParseArgsRefusesNonCanonicalCommandSpelling(t *testing.T) {
	for _, cmd := range []string{"/bin/../bin/ls", "/bin/./ls", "/bin//ls"} {
		if _, err := parseArgs(json.RawMessage(`{"command":"` + cmd + `"}`)); err == nil {
			t.Fatalf("non-canonical spelling %q accepted", cmd)
		}
	}
	// The canonical spelling itself must still be accepted.
	if _, err := parseArgs(json.RawMessage(`{"command":"/bin/ls"}`)); err != nil {
		t.Fatalf("canonical spelling refused: %v", err)
	}
}

// A raw invalid UTF-8 byte and an unpaired surrogate escape (\ud800 with
// no low-surrogate partner) BOTH decode to the SAME literal U+FFFD
// replacement character — two distinct byte sequences would otherwise
// alias to one visible string (round-1 code-review, live-reproduced).
// parseArgs must reject both, for Command and every Args element.
func TestParseArgsRejectsInvalidUTF8(t *testing.T) {
	rawByte := append([]byte(`{"command":"/tmp/tool-`), 0xff)
	rawByte = append(rawByte, []byte(`"}`)...)
	if _, err := parseArgs(json.RawMessage(rawByte)); err == nil {
		t.Fatal("a raw invalid UTF-8 byte in command was accepted")
	}
	surrogate := json.RawMessage(`{"command":"/tmp/tool-\ud800"}`)
	if _, err := parseArgs(surrogate); err == nil {
		t.Fatal("an unpaired surrogate escape in command was accepted")
	}
	inArgs := json.RawMessage(`{"command":"/bin/ls","args":["/tmp/x-\ud800"]}`)
	if _, err := parseArgs(inArgs); err == nil {
		t.Fatal("an unpaired surrogate escape in an args element was accepted")
	}
}

// A JSON `null` array ELEMENT must never silently become an empty
// string — the approved/requested representation would then diverge
// from what actually runs (round-2 code-review, live-reproduced:
// {"args":[null]} decoded to Args==[]string{""}). A top-level `"args":
// null` (or omitting the field) still legitimately means "no args".
func TestParseArgsRejectsNullArgsElement(t *testing.T) {
	if _, err := parseArgs(json.RawMessage(`{"command":"/bin/ls","args":[null]}`)); err == nil {
		t.Fatal("a null args element was silently accepted as an empty string")
	}
	if _, err := parseArgs(json.RawMessage(`{"command":"/bin/ls","args":["/",null]}`)); err == nil {
		t.Fatal("a null args element among real ones was silently accepted")
	}
	// Top-level null/omitted args is legitimately "no args".
	args, err := parseArgs(json.RawMessage(`{"command":"/bin/ls","args":null}`))
	if err != nil || len(args.Args) != 0 {
		t.Fatalf("top-level null args refused or non-empty: args=%+v err=%v", args, err)
	}
	// An explicit empty STRING element (not null) must still be
	// accepted — it's a legitimate, if unusual, argv element, and must
	// not be confused with the null-element rejection above (codex
	// round-3 non-blocking note: this distinction was verified via a
	// disposable probe but never checked in).
	args, err = parseArgs(json.RawMessage(`{"command":"/bin/ls","args":[""]}`))
	if err != nil || len(args.Args) != 1 || args.Args[0] != "" {
		t.Fatalf("explicit empty-string args element wrongly refused or mangled: args=%+v err=%v", args, err)
	}
}

// The exact live reproduction from round 3's code review: a symlink
// component followed by ".." cleans lexically to an allowlisted-looking
// path, but the kernel would resolve it to a completely different
// binary. The canonical-form check must refuse this BEFORE any
// resolution is attempted (no TOCTOU window, no dependence on the
// symlink's target existing).
func TestExecSymlinkTraversalCannotMatchAllowlistedNeighbor(t *testing.T) {
	dir := t.TempDir()
	safe := filepath.Join(dir, "safe")
	evil := filepath.Join(dir, "evil", "x")
	if err := os.MkdirAll(safe, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(evil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(safe, "link")
	if err := os.Symlink(evil, link); err != nil {
		t.Fatal(err)
	}
	allowlisted := filepath.Join(safe, "git") // what the gate is meant to protect
	// Built by string concatenation, NOT filepath.Join — Join cleans its
	// result internally, which would silently give us the already-clean
	// string instead of the dirty symlink+".." spelling under test.
	traversal := link + "/../git"
	if filepath.Clean(traversal) != allowlisted {
		t.Fatalf("test setup: lexical clean of %q = %q, want %q", traversal, filepath.Clean(traversal), allowlisted)
	}

	b := sandbox.NewBwrap()
	rep, err := b.Probe(ctxT())
	if err != nil {
		t.Skipf("bwrap unavailable: %v", err)
	}
	a, err := New(b, rep, redact.None{}, []string{allowlisted})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"command": traversal})
	if err := a.ArgGate(raw); err == nil {
		t.Fatal("symlink+.. traversal matched the allowlisted neighbor")
	}
}

// ArgGate denies a non-allowlisted command BEFORE any approval would be
// consumed (round 1's finding: approving a call Launch was always going
// to refuse anyway accomplished nothing) — and, symmetrically, an
// allowlisted command is let through (nil error) so the static Ask still
// applies; ArgGate never itself grants execution.
func TestArgGateAllowsListedDeniesUnlisted(t *testing.T) {
	a := adapter(t)
	listed, _ := json.Marshal(map[string]any{"command": testAllow(t)[0]})
	if err := a.ArgGate(listed); err != nil {
		t.Fatalf("allowlisted command denied by the gate: %v", err)
	}
	unlisted, _ := json.Marshal(map[string]any{"command": "/bin/env"})
	if err := a.ArgGate(unlisted); err == nil {
		t.Fatal("non-allowlisted command accepted by the gate")
	}
}
