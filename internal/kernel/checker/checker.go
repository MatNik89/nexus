// Package checker is the minimal deterministic checker (S16.6-det, K0
// primitive; tasks-P0 T12). It grades ARTIFACTS against a typed
// AcceptanceContract — never a worker's prose: free text is not even a
// representable evidence kind (anti-sycophancy is structural, E13). The
// checker is a separate principal from any worker: Grade is a pure function
// over the contract and the evidence bundle.
package checker

import (
	"fmt"
	"time"
)

// Criterion is one closed-kind acceptance criterion. Exactly one of the
// typed fields is set (XOR, validated).
type Criterion struct {
	// ExitCodeIs: a command exited with this code.
	ExitCodeIs *int
	// FileHashIs: a file at Path has exactly this sha256 (hex).
	FileHashIs *FileHashCriterion
	// DeliveredAndAcked: the reminder occurrence was durably delivered AND
	// user-acknowledged — both correlated to the SAME occurrence id
	// (HARDQ B5: an ack for occurrence N never closes N+1).
	DeliveredAndAcked *string // occurrence id
	// FileContainsLine: a file at Path contains EXACTLY this line — the
	// stable postcondition for one appended record (Phase-4 codex #6: a
	// whole-file hash breaks when a later task appends legitimately).
	FileContainsLine *FileLineCriterion
	// CodingProofIs: a coding-run's base-vs-candidate test transitions
	// (PLAN-CODING-TRIO.md Slice 1, invariant 8) satisfy the named
	// requirement.
	CodingProofIs *CodingProofCriterion
}

// CodingProofMode distinguishes a BEHAVIORAL fix (must show at least one
// FAIL_TO_PASS transition — invariant 8's default) from a
// semantics-preserving REFACTOR (an empty FAIL_TO_PASS set is legitimate
// ONLY alongside at least one predeclared, PASSED structural
// postcondition — invariant 8's own exemption, never a bare
// "allow empty" escape hatch with nothing backing it).
type CodingProofMode int

const (
	CodingProofBehavioral CodingProofMode = iota
	CodingProofRefactor
)

// CodingProofCriterion names what one coding-run must prove. Evidence
// for this criterion is produced ONLY by internal/coding/evidence.Capture
// — never hand-constructed — so Grade trusts the causal ordering of the
// underlying journal chain (bound -> base-run -> candidate-run) was
// already enforced by that single, trusted producer at write time; Grade
// grades the DOMAIN claim (which tests transitioned how), the same
// separation of concerns FileHashIs/DeliveredAndAcked already have from
// the journal machinery that produced their evidence.
type CodingProofCriterion struct {
	Mode CodingProofMode
	// RequiredFailToPass/RequiredPassToPass, when non-empty, name SPECIFIC
	// "package.Test" keys that must appear in the evidence's own
	// FailToPass/PassToPass sets — a stronger requirement than "the set
	// is merely non-empty" when the contract cares about a named test.
	RequiredFailToPass []string
	RequiredPassToPass []string
	// RequiredStructural names structural postconditions (Mode ==
	// CodingProofRefactor only) that MUST all appear, and pass, in the
	// evidence's StructuralPassed set when FailToPass is empty.
	RequiredStructural []string
}

// FileLineCriterion pins one immutable marker line.
type FileLineCriterion struct {
	Path string
	Line string
}

// FileHashCriterion pins a file's content.
type FileHashCriterion struct {
	Path   string
	SHA256 string
}

func sha256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (c Criterion) validate() error {
	if c.FileHashIs != nil && (c.FileHashIs.Path == "" || !sha256Hex(c.FileHashIs.SHA256)) {
		return fmt.Errorf("file-hash criterion requires a path and a 64-char hex sha256")
	}
	if c.DeliveredAndAcked != nil && *c.DeliveredAndAcked == "" {
		return fmt.Errorf("delivered-and-acked criterion requires a non-empty occurrence id")
	}
	if c.FileContainsLine != nil && (c.FileContainsLine.Path == "" || c.FileContainsLine.Line == "") {
		return fmt.Errorf("file-line criterion requires a path and a non-empty line")
	}
	if c.CodingProofIs != nil {
		if c.CodingProofIs.Mode != CodingProofBehavioral && c.CodingProofIs.Mode != CodingProofRefactor {
			return fmt.Errorf("coding-proof criterion has an unknown mode (fail closed)")
		}
		if c.CodingProofIs.Mode == CodingProofRefactor && len(c.CodingProofIs.RequiredStructural) == 0 {
			return fmt.Errorf("coding-proof criterion in REFACTOR mode requires at least one RequiredStructural postcondition — a bare mode switch with nothing backing it is not a proof")
		}
	}
	set := 0
	if c.ExitCodeIs != nil {
		set++
	}
	if c.FileHashIs != nil {
		set++
	}
	if c.DeliveredAndAcked != nil {
		set++
	}
	if c.FileContainsLine != nil {
		set++
	}
	if c.CodingProofIs != nil {
		set++
	}
	if set != 1 {
		return fmt.Errorf("criterion must set exactly one kind, has %d", set)
	}
	return nil
}

// AcceptanceContract names what "done" means — written from the SPEC side,
// before the work. Worker is REQUIRED and names the principal whose work is
// being judged: evidence PRODUCED by that same principal is rejected
// outright (checker != worker is enforced, not assumed — Phase-1B codex #1;
// r2 codex #15: an omitted Worker silently disarmed the separation check).
type AcceptanceContract struct {
	ID       string
	Worker   string
	Criteria []Criterion
}

// Evidence is one closed-kind artifact. Exactly one typed field set.
// There is deliberately NO free-text kind. Producer names the TRUSTED OWNER
// that emitted the artifact (journal, delivery gateway, execution runner) —
// never the worker; full provenance authentication arrives with the real
// producers (T19/T21 emit journal-backed evidence). topknot ceiling:
// producer strings are declared, not yet cryptographically attested;
// upgrade when the journal-receipt evidence kind lands (T21).
// Contract BINDS the artifact to one contract id — a valid artifact from
// contract A can never be replayed into contract B (r2 codex #15).
type Evidence struct {
	Contract string
	Producer string
	Exit     *ExitEvidence
	FileHash *FileHashEvidence
	Delivery *DeliveryEvidence
	Ack      *AckEvidence
	FileLine *FileLineEvidence
	Coding   *CodingProofEvidence
}

// CodingProofEvidence is one coding-run's classified base-vs-candidate
// test transitions (PLAN-CODING-TRIO.md Slice 1). Every "package.Test"
// string uses the SAME formatting internal/coding/evidence.Classify
// produces ("package.Test") — Grade does not re-derive or re-parse it.
type CodingProofEvidence struct {
	FailToPass       []string
	PassToPass       []string
	PassToFail       []string // regressions — non-empty ALWAYS fails grading
	Missing          []string // present in base, absent from candidate — non-empty ALWAYS fails grading
	StructuralPassed []string // structural postcondition names that held (REFACTOR mode)
}

// FileLineEvidence reports whether the verifier FOUND the exact line.
type FileLineEvidence struct {
	Path    string
	Line    string
	Present bool
}

type ExitEvidence struct {
	Code int
}

type FileHashEvidence struct {
	Path   string
	SHA256 string
}

type DeliveryEvidence struct {
	OccurrenceID string
	DeliveredAt  time.Time
}

type AckEvidence struct {
	OccurrenceID string
	AckAt        time.Time
}

func (e Evidence) validate() error {
	if e.Producer == "" {
		return fmt.Errorf("evidence requires a producer identity")
	}
	if e.Contract == "" {
		return fmt.Errorf("evidence requires a contract binding")
	}
	if e.Delivery != nil && (e.Delivery.OccurrenceID == "" || e.Delivery.DeliveredAt.IsZero()) {
		return fmt.Errorf("delivery evidence requires a non-empty occurrence id and time")
	}
	if e.Ack != nil && (e.Ack.OccurrenceID == "" || e.Ack.AckAt.IsZero()) {
		return fmt.Errorf("ack evidence requires a non-empty occurrence id and time")
	}
	if e.FileHash != nil && !sha256Hex(e.FileHash.SHA256) {
		return fmt.Errorf("file-hash evidence requires a 64-char hex sha256")
	}
	if e.FileLine != nil && (e.FileLine.Path == "" || e.FileLine.Line == "") {
		return fmt.Errorf("file-line evidence requires a path and a non-empty line")
	}
	set := 0
	if e.Exit != nil {
		set++
	}
	if e.FileHash != nil {
		set++
	}
	if e.Delivery != nil {
		set++
	}
	if e.Ack != nil {
		set++
	}
	if e.FileLine != nil {
		set++
	}
	if e.Coding != nil {
		set++
	}
	if set != 1 {
		return fmt.Errorf("evidence must set exactly one kind, has %d", set)
	}
	return nil
}

// CriterionResult explains one grading decision.
type CriterionResult struct {
	Index  int
	Pass   bool
	Reason string
}

// Verdict is the deterministic grading outcome.
type Verdict struct {
	Pass    bool
	Results []CriterionResult
}

// Grade evaluates the bundle against the contract. An empty or invalid
// bundle FAILS every criterion — a claim without artifacts is a failure,
// not an unknown (E13).
func Grade(contract AcceptanceContract, bundle []Evidence) (Verdict, error) {
	if contract.ID == "" || len(contract.Criteria) == 0 {
		return Verdict{}, fmt.Errorf("checker: contract requires an id and at least one criterion")
	}
	if contract.Worker == "" {
		return Verdict{}, fmt.Errorf("checker: contract requires the judged worker's identity (checker != worker is not optional)")
	}
	for i, c := range contract.Criteria {
		if err := c.validate(); err != nil {
			return Verdict{}, fmt.Errorf("checker: criterion %d: %w", i, err)
		}
	}
	for i, e := range bundle {
		if err := e.validate(); err != nil {
			return Verdict{}, fmt.Errorf("checker: evidence %d: %w", i, err)
		}
		// checker != worker, ENFORCED: the judged principal cannot supply
		// its own artifacts (codex #1 self-grading literal).
		if e.Producer == contract.Worker {
			return Verdict{}, fmt.Errorf("checker: evidence %d produced by the judged worker %q (rejected — checker != worker)", i, contract.Worker)
		}
		// Contract binding: an artifact for another contract is rejected,
		// never silently graded (r2 codex #15 replay literal).
		if e.Contract != contract.ID {
			return Verdict{}, fmt.Errorf("checker: evidence %d is bound to contract %q, not %q (rejected — no cross-contract replay)", i, e.Contract, contract.ID)
		}
	}
	v := Verdict{Pass: true}
	for i, c := range contract.Criteria {
		res := gradeOne(i, c, bundle)
		if !res.Pass {
			v.Pass = false
		}
		v.Results = append(v.Results, res)
	}
	return v, nil
}

func gradeOne(idx int, c Criterion, bundle []Evidence) CriterionResult {
	fail := func(reason string) CriterionResult {
		return CriterionResult{Index: idx, Pass: false, Reason: reason}
	}
	switch {
	case c.ExitCodeIs != nil:
		// ALL exit evidence must agree — order-independent, so neither a
		// favorable prepend (codex #3) nor an unfavorable one (kilo #2) can
		// game the verdict; contradictions FAIL.
		found := false
		for _, e := range bundle {
			if e.Exit == nil {
				continue
			}
			found = true
			if e.Exit.Code != *c.ExitCodeIs {
				return fail(fmt.Sprintf("exit code %d contradicts the contracted %d", e.Exit.Code, *c.ExitCodeIs))
			}
		}
		if !found {
			return fail("no exit-code evidence in the bundle")
		}
		return CriterionResult{Index: idx, Pass: true}
	case c.FileHashIs != nil:
		found := false
		for _, e := range bundle {
			if e.FileHash == nil || e.FileHash.Path != c.FileHashIs.Path {
				continue
			}
			found = true
			if e.FileHash.SHA256 != c.FileHashIs.SHA256 {
				return fail("a file-hash record for the contracted path contradicts the contract")
			}
		}
		if !found {
			return fail("no file-hash evidence for the contracted path")
		}
		return CriterionResult{Index: idx, Pass: true}
	case c.FileContainsLine != nil:
		// ALL matching evidence must agree the line is PRESENT.
		found := false
		for _, e := range bundle {
			if e.FileLine == nil || e.FileLine.Path != c.FileContainsLine.Path || e.FileLine.Line != c.FileContainsLine.Line {
				continue
			}
			found = true
			if !e.FileLine.Present {
				return fail("the contracted line is absent from the file")
			}
		}
		if !found {
			return fail("no file-line evidence for the contracted path+line")
		}
		return CriterionResult{Index: idx, Pass: true}
	case c.DeliveredAndAcked != nil:
		occ := *c.DeliveredAndAcked
		var firstDelivered time.Time
		delivered := false
		for _, e := range bundle {
			// Correlation is to the SAME occurrence id (B5).
			if e.Delivery != nil && e.Delivery.OccurrenceID == occ {
				if !delivered || e.Delivery.DeliveredAt.Before(firstDelivered) {
					firstDelivered = e.Delivery.DeliveredAt
				}
				delivered = true
			}
		}
		if !delivered {
			return fail("no delivery receipt for occurrence " + occ)
		}
		// B5 order: an acknowledgment can only FOLLOW a delivery — an ack
		// timestamped before every delivery of the occurrence is a
		// contradiction, not proof (r2 codex #17 / kilo #15).
		acked := false
		for _, e := range bundle {
			if e.Ack == nil || e.Ack.OccurrenceID != occ {
				continue
			}
			if e.Ack.AckAt.Before(firstDelivered) {
				return fail("an acknowledgment for occurrence " + occ + " precedes its earliest delivery (contradiction)")
			}
			acked = true
		}
		if !acked {
			return fail("no user acknowledgment for occurrence " + occ)
		}
		return CriterionResult{Index: idx, Pass: true}
	case c.CodingProofIs != nil:
		return gradeCodingProof(idx, *c.CodingProofIs, bundle, fail)
	}
	return fail("unknown criterion kind (fail closed)")
}

// gradeCodingProof grades one CodingProofCriterion. ALL matching evidence
// in the bundle must agree (same order-independence discipline as every
// other criterion kind above) — contradictory coding-proof evidence for
// the same contract FAILS, it does not average or take the first.
func gradeCodingProof(idx int, c CodingProofCriterion, bundle []Evidence, fail func(string) CriterionResult) CriterionResult {
	found := false
	for _, e := range bundle {
		if e.Coding == nil {
			continue
		}
		found = true
		ce := e.Coding
		// A regression or a vanished test ALWAYS fails grading — invariant
		// 8 never trades away a broken previously-passing test for a
		// newly-fixed one, and a test that disappeared is not proof it
		// still passes (Classify's own Missing/PassToFail semantics).
		if len(ce.PassToFail) > 0 {
			return fail(fmt.Sprintf("coding-proof regression: previously passing test(s) now fail: %v", ce.PassToFail))
		}
		if len(ce.Missing) > 0 {
			return fail(fmt.Sprintf("coding-proof: previously passing test(s) are absent from the candidate run: %v", ce.Missing))
		}
		switch c.Mode {
		case CodingProofBehavioral:
			if len(ce.FailToPass) == 0 {
				return fail("coding-proof (BEHAVIORAL mode) requires at least one FAIL_TO_PASS transition")
			}
		case CodingProofRefactor:
			if len(ce.FailToPass) == 0 {
				for _, want := range c.RequiredStructural {
					if !containsString(ce.StructuralPassed, want) {
						return fail("coding-proof (REFACTOR mode, empty FAIL_TO_PASS) missing required structural postcondition: " + want)
					}
				}
			}
		}
		for _, want := range c.RequiredFailToPass {
			if !containsString(ce.FailToPass, want) {
				return fail("coding-proof missing required FAIL_TO_PASS test: " + want)
			}
		}
		for _, want := range c.RequiredPassToPass {
			if !containsString(ce.PassToPass, want) {
				return fail("coding-proof missing required PASS_TO_PASS test: " + want)
			}
		}
	}
	if !found {
		return fail("no coding-proof evidence in the bundle")
	}
	return CriterionResult{Index: idx, Pass: true}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
