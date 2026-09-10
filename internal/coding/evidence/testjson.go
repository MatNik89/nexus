//go:build linux

// Package evidence is the Slice 1 proof-of-done owner (PLAN-CODING-TRIO.md):
// it captures a base run and a candidate run of `go test -json` through
// Slice 0's governed internal/coding/runner.Run, classifies each test's
// transition (FAIL_TO_PASS / PASS_TO_PASS / a regression), and journals a
// two-event chain (coding.evidence_bound -> coding.evidence_completed)
// that a checker.CodingProofCriterion can grade. Depends only on
// existing machinery (internal/kernel/journal, internal/kernel/checker)
// plus internal/coding/runner — never internal/coding/impact (TIA) or a
// symedit package that doesn't exist yet.
package evidence

import (
	"bufio"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// TestOutcome is a test's terminal result from one `go test -json` run.
type TestOutcome int

const (
	OutcomeUnknown TestOutcome = iota
	OutcomePass
	OutcomeFail
	OutcomeSkip
)

// TestKey identifies one test within one package.
type TestKey struct {
	Package string
	Test    string
}

// ParseTestJSON parses a `go test -json` stdout stream into each test's
// TERMINAL outcome (the last pass/fail/skip action observed per key) —
// intermediate "run"/"output"/"pause"/"cont" actions are not outcomes.
// A package-level aggregate line (Test == "") is skipped: Slice 1 grades
// individual tests, not whole packages. Fails closed on a line that
// isn't valid JSON — a caller must never treat a corrupted stream as "no
// tests ran" (silently swallowing a parse error would look identical to
// an empty, all-passing suite).
func ParseTestJSON(stdout string) (map[TestKey]TestOutcome, error) {
	outcomes := make(map[TestKey]TestOutcome)
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), 16<<20) // a single JSON line rarely exceeds this; fail closed if it does
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev struct {
			Action  string `json:"Action"`
			Package string `json:"Package"`
			Test    string `json:"Test"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("evidence: malformed go test -json line %d: %w", lineNo, err)
		}
		if ev.Test == "" {
			continue // package-level aggregate, not a per-test outcome
		}
		key := TestKey{Package: ev.Package, Test: ev.Test}
		switch ev.Action {
		case "pass":
			outcomes[key] = OutcomePass
		case "fail":
			outcomes[key] = OutcomeFail
		case "skip":
			outcomes[key] = OutcomeSkip
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("evidence: reading go test -json stream: %w", err)
	}
	return outcomes, nil
}

// TestDiff is the classified transition of every test key present in
// EITHER run, keyed by invariant 8's own vocabulary.
type TestDiff struct {
	FailToPass []string // base FAIL, candidate PASS — proof of a behavioral fix
	PassToPass []string // PASS in both — preserved behavior
	PassToFail []string // base PASS, candidate FAIL — a REGRESSION
	FailToFail []string // FAIL in both — an unresolved preexisting failure
	NewPass    []string // present only in candidate, PASS
	NewFail    []string // present only in candidate, FAIL
	Missing    []string // present in base, ABSENT from candidate — never silently dropped
}

// formatKey renders a TestKey as "package.Test" — the string vocabulary
// invariant 8 and the checker extension both use.
func formatKey(k TestKey) string { return k.Package + "." + k.Test }

// Classify compares base and candidate outcome maps per invariant 8. A
// test present in base but ABSENT from candidate is neither silently
// dropped nor treated as a pass — it is reported in Missing, which the
// checker extension refuses to grade PASS in the presence of (a
// disappeared test is not proof it still passes).
func Classify(base, candidate map[TestKey]TestOutcome) TestDiff {
	var d TestDiff
	for key, baseOutcome := range base {
		candOutcome, ok := candidate[key]
		if !ok {
			d.Missing = append(d.Missing, formatKey(key))
			continue
		}
		switch {
		case baseOutcome == OutcomeFail && candOutcome == OutcomePass:
			d.FailToPass = append(d.FailToPass, formatKey(key))
		case baseOutcome == OutcomePass && candOutcome == OutcomePass:
			d.PassToPass = append(d.PassToPass, formatKey(key))
		case baseOutcome == OutcomePass && candOutcome == OutcomeFail:
			d.PassToFail = append(d.PassToFail, formatKey(key))
		case baseOutcome == OutcomeFail && candOutcome == OutcomeFail:
			d.FailToFail = append(d.FailToFail, formatKey(key))
		}
	}
	for key, candOutcome := range candidate {
		if _, inBase := base[key]; inBase {
			continue
		}
		switch candOutcome {
		case OutcomePass:
			d.NewPass = append(d.NewPass, formatKey(key))
		case OutcomeFail:
			d.NewFail = append(d.NewFail, formatKey(key))
		}
	}
	sort.Strings(d.FailToPass)
	sort.Strings(d.PassToPass)
	sort.Strings(d.PassToFail)
	sort.Strings(d.FailToFail)
	sort.Strings(d.NewPass)
	sort.Strings(d.NewFail)
	sort.Strings(d.Missing)
	return d
}
