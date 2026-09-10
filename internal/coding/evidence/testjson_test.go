//go:build linux

package evidence

import (
	"strings"
	"testing"
)

func TestParseTestJSONExtractsTerminalOutcomes(t *testing.T) {
	stream := `
{"Action":"start","Package":"pkg"}
{"Action":"run","Package":"pkg","Test":"TestA"}
{"Action":"output","Package":"pkg","Test":"TestA","Output":"=== RUN   TestA\n"}
{"Action":"pass","Package":"pkg","Test":"TestA"}
{"Action":"run","Package":"pkg","Test":"TestB"}
{"Action":"fail","Package":"pkg","Test":"TestB"}
{"Action":"output","Package":"pkg","Output":"FAIL\n"}
{"Action":"fail","Package":"pkg"}
`
	parsed, err := ParseTestJSON(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Outcomes) != 2 {
		t.Fatalf("expected 2 test outcomes (package aggregate excluded), got %d: %+v", len(parsed.Outcomes), parsed.Outcomes)
	}
	if parsed.Outcomes[TestKey{"pkg", "TestA"}] != OutcomePass {
		t.Fatalf("TestA: want pass, got %v", parsed.Outcomes[TestKey{"pkg", "TestA"}])
	}
	if parsed.Outcomes[TestKey{"pkg", "TestB"}] != OutcomeFail {
		t.Fatalf("TestB: want fail, got %v", parsed.Outcomes[TestKey{"pkg", "TestB"}])
	}
	if len(parsed.FailedPackages) != 1 || parsed.FailedPackages[0] != "pkg" {
		t.Fatalf("expected FailedPackages=[pkg] (the trailing package-level fail line), got %v", parsed.FailedPackages)
	}
}

// Detector (code-review finding, codex): a package that fails to COMPILE
// emits ONLY a package-level "fail" action, with zero per-test events —
// this must be captured as a FailedPackage, not silently treated as
// "zero relevant tests ran".
func TestParseTestJSONCapturesPackageLevelCompileFailure(t *testing.T) {
	stream := `{"Action":"start","Package":"broken/pkg"}
{"Action":"output","Package":"broken/pkg","Output":"# broken/pkg\n./x.go:1:1: syntax error\n"}
{"Action":"fail","Package":"broken/pkg","Elapsed":0}
`
	parsed, err := ParseTestJSON(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.Outcomes) != 0 {
		t.Fatalf("expected zero per-test outcomes for a compile failure, got %+v", parsed.Outcomes)
	}
	if len(parsed.FailedPackages) != 1 || parsed.FailedPackages[0] != "broken/pkg" {
		t.Fatalf("expected FailedPackages=[broken/pkg], got %v", parsed.FailedPackages)
	}
}

func TestParseTestJSONFailsClosedOnMalformedLine(t *testing.T) {
	stream := `{"Action":"pass","Package":"pkg","Test":"TestA"}
not json at all
`
	if _, err := ParseTestJSON(stream); err == nil {
		t.Fatal("expected a malformed line to be refused, not silently skipped")
	}
}

func TestParseTestJSONTakesLastActionAsTerminal(t *testing.T) {
	// A retried/rerun test (go test -json can legitimately emit run/pass
	// or run/fail more than once for the same key under -count>1 or
	// subtests re-entering) — the LAST terminal action wins.
	stream := `{"Action":"fail","Package":"pkg","Test":"TestFlaky"}
{"Action":"run","Package":"pkg","Test":"TestFlaky"}
{"Action":"pass","Package":"pkg","Test":"TestFlaky"}
`
	parsed, err := ParseTestJSON(stream)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Outcomes[TestKey{"pkg", "TestFlaky"}] != OutcomePass {
		t.Fatalf("expected the LAST terminal action (pass) to win, got %v", parsed.Outcomes[TestKey{"pkg", "TestFlaky"}])
	}
}

func TestClassifyInvariant8Vocabulary(t *testing.T) {
	base := map[TestKey]TestOutcome{
		{"pkg", "TestFixed"}:      OutcomeFail,
		{"pkg", "TestStable"}:     OutcomePass,
		{"pkg", "TestBroken"}:     OutcomePass, // will regress
		{"pkg", "TestStillFails"}: OutcomeFail,
		{"pkg", "TestVanished"}:   OutcomePass,
		{"pkg", "TestSkipped"}:    OutcomePass, // will be skipped in candidate
	}
	candidate := map[TestKey]TestOutcome{
		{"pkg", "TestFixed"}:      OutcomePass,
		{"pkg", "TestStable"}:     OutcomePass,
		{"pkg", "TestBroken"}:     OutcomeFail,
		{"pkg", "TestStillFails"}: OutcomeFail,
		{"pkg", "TestNew"}:        OutcomePass,
		{"pkg", "TestNewFail"}:    OutcomeFail,
		{"pkg", "TestSkipped"}:    OutcomeSkip,
	}
	d := Classify(base, candidate)

	assertEqual(t, "FailToPass", d.FailToPass, []string{"pkg.TestFixed"})
	assertEqual(t, "PassToPass", d.PassToPass, []string{"pkg.TestStable"})
	assertEqual(t, "PassToFail", d.PassToFail, []string{"pkg.TestBroken"})
	assertEqual(t, "FailToFail", d.FailToFail, []string{"pkg.TestStillFails"})
	assertEqual(t, "PassToSkip", d.PassToSkip, []string{"pkg.TestSkipped"})
	assertEqual(t, "NewPass", d.NewPass, []string{"pkg.TestNew"})
	assertEqual(t, "NewFail", d.NewFail, []string{"pkg.TestNewFail"})
	assertEqual(t, "Missing", d.Missing, []string{"pkg.TestVanished"})
}

func assertEqual(t *testing.T, label string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("%s: got %v, want %v", label, got, want)
	}
}

// Detector (Slice 2 tia orchestrator requirement, code-review finding,
// codex): TerminalPackages must record every package that reached its
// OWN package-level TERMINAL action (pass/fail/skip) — including a
// package with zero tests (a bare package-level "pass", never producing
// a per-test outcome or a FailedPackages entry) — so a caller can
// distinguish "legitimately covered, zero tests" from "never finished"
// (e.g. the process crashed before reaching that package's own terminal
// action).
func TestParseTestJSONTracksTerminalPackagesIncludingNoTestPackages(t *testing.T) {
	stream := `{"Action":"start","Package":"pkg/withtests"}
{"Action":"pass","Package":"pkg/withtests","Test":"TestA"}
{"Action":"pass","Package":"pkg/withtests"}
{"Action":"start","Package":"pkg/notests"}
{"Action":"pass","Package":"pkg/notests"}
`
	parsed, err := ParseTestJSON(stream)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pkg/notests", "pkg/withtests"}
	if len(parsed.TerminalPackages) != len(want) {
		t.Fatalf("TerminalPackages = %v, want %v", parsed.TerminalPackages, want)
	}
	for i, w := range want {
		if parsed.TerminalPackages[i] != w {
			t.Fatalf("TerminalPackages = %v, want %v", parsed.TerminalPackages, want)
		}
	}
}

// Detector (code-review finding, codex, round 5: the exact deeper
// counterexample that broke round-4's first attempt): a package that
// emits ONLY a "start" action — and never reaches its own package-level
// terminal pass/fail/skip, e.g. because the `go test` process crashed
// mid-package — must NOT appear in TerminalPackages. "Started" is not
// "finished."
func TestParseTestJSONExcludesPackageThatOnlyStarted(t *testing.T) {
	stream := `{"Action":"fail","Package":"pkg/a"}
{"Action":"start","Package":"pkg/b"}
`
	parsed, err := ParseTestJSON(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(parsed.TerminalPackages) != 1 || parsed.TerminalPackages[0] != "pkg/a" {
		t.Fatalf("TerminalPackages = %v, want [pkg/a] (pkg/b only started, never reached a terminal action)", parsed.TerminalPackages)
	}
}
