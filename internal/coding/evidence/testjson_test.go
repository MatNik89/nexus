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
	outcomes, err := ParseTestJSON(stream)
	if err != nil {
		t.Fatal(err)
	}
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 test outcomes (package aggregate excluded), got %d: %+v", len(outcomes), outcomes)
	}
	if outcomes[TestKey{"pkg", "TestA"}] != OutcomePass {
		t.Fatalf("TestA: want pass, got %v", outcomes[TestKey{"pkg", "TestA"}])
	}
	if outcomes[TestKey{"pkg", "TestB"}] != OutcomeFail {
		t.Fatalf("TestB: want fail, got %v", outcomes[TestKey{"pkg", "TestB"}])
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
	outcomes, err := ParseTestJSON(stream)
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[TestKey{"pkg", "TestFlaky"}] != OutcomePass {
		t.Fatalf("expected the LAST terminal action (pass) to win, got %v", outcomes[TestKey{"pkg", "TestFlaky"}])
	}
}

func TestClassifyInvariant8Vocabulary(t *testing.T) {
	base := map[TestKey]TestOutcome{
		{"pkg", "TestFixed"}:      OutcomeFail,
		{"pkg", "TestStable"}:     OutcomePass,
		{"pkg", "TestBroken"}:     OutcomePass, // will regress
		{"pkg", "TestStillFails"}: OutcomeFail,
		{"pkg", "TestVanished"}:   OutcomePass,
	}
	candidate := map[TestKey]TestOutcome{
		{"pkg", "TestFixed"}:      OutcomePass,
		{"pkg", "TestStable"}:     OutcomePass,
		{"pkg", "TestBroken"}:     OutcomeFail,
		{"pkg", "TestStillFails"}: OutcomeFail,
		{"pkg", "TestNew"}:        OutcomePass,
		{"pkg", "TestNewFail"}:    OutcomeFail,
	}
	d := Classify(base, candidate)

	assertEqual(t, "FailToPass", d.FailToPass, []string{"pkg.TestFixed"})
	assertEqual(t, "PassToPass", d.PassToPass, []string{"pkg.TestStable"})
	assertEqual(t, "PassToFail", d.PassToFail, []string{"pkg.TestBroken"})
	assertEqual(t, "FailToFail", d.FailToFail, []string{"pkg.TestStillFails"})
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
