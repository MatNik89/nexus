// T12 RED table (tasks-P0): "done" with an empty EvidenceBundle → FAIL;
// an ack for occurrence N grading a contract for N+1 → FAIL (HARDQ B5);
// coding-style (exit/diff-hash) and assistant-style (receipt+ack) contracts
// both gradeable. Anchored to E13 + B5.
package checker

import (
	"testing"
	"time"
)

func intp(i int) *int { return &i }
func strp(s string) *string { return &s }

func TestEmptyBundleFailsEveryCriterion(t *testing.T) {
	contract := AcceptanceContract{ID: "c1", Criteria: []Criterion{
		{ExitCodeIs: intp(0)},
		{DeliveredAndAcked: strp("occ-1")},
	}}
	v, err := Grade(contract, nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Pass {
		t.Fatal("empty bundle graded PASS — a claim without artifacts must fail")
	}
	for _, r := range v.Results {
		if r.Pass || r.Reason == "" {
			t.Fatalf("criterion %d must fail with a reason: %+v", r.Index, r)
		}
	}
}

func TestCodingStyleContract(t *testing.T) {
	contract := AcceptanceContract{ID: "build", Criteria: []Criterion{
		{ExitCodeIs: intp(0)},
		{FileHashIs: &FileHashCriterion{Path: "notes.txt", SHA256: "abc123"}},
	}}
	pass, err := Grade(contract, []Evidence{
		{Exit: &ExitEvidence{Code: 0}},
		{FileHash: &FileHashEvidence{Path: "notes.txt", SHA256: "abc123"}},
	})
	if err != nil || !pass.Pass {
		t.Fatalf("valid coding evidence must pass: %+v %v", pass, err)
	}
	failV, _ := Grade(contract, []Evidence{
		{Exit: &ExitEvidence{Code: 1}},
		{FileHash: &FileHashEvidence{Path: "notes.txt", SHA256: "abc123"}},
	})
	if failV.Pass {
		t.Fatal("nonzero exit graded PASS")
	}
	wrongHash, _ := Grade(contract, []Evidence{
		{Exit: &ExitEvidence{Code: 0}},
		{FileHash: &FileHashEvidence{Path: "notes.txt", SHA256: "OTHER"}},
	})
	if wrongHash.Pass {
		t.Fatal("wrong file hash graded PASS")
	}
}

// HARDQ B5 literal: receipt+ack must correlate to the SAME occurrence — an
// ack for N never closes N+1.
func TestAckForWrongOccurrenceFails(t *testing.T) {
	contract := AcceptanceContract{ID: "rem", Criteria: []Criterion{
		{DeliveredAndAcked: strp("occ-2")},
	}}
	v, err := Grade(contract, []Evidence{
		{Delivery: &DeliveryEvidence{OccurrenceID: "occ-2", DeliveredAt: time.Unix(1, 0)}},
		{Ack: &AckEvidence{OccurrenceID: "occ-1", AckAt: time.Unix(2, 0)}}, // wrong occurrence
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Pass {
		t.Fatal("ack for occurrence 1 closed occurrence 2")
	}
	ok, _ := Grade(contract, []Evidence{
		{Delivery: &DeliveryEvidence{OccurrenceID: "occ-2", DeliveredAt: time.Unix(1, 0)}},
		{Ack: &AckEvidence{OccurrenceID: "occ-2", AckAt: time.Unix(2, 0)}},
	})
	if !ok.Pass {
		t.Fatal("correctly correlated receipt+ack failed")
	}
}

// Delivery WITHOUT ack is not done (receipt proves delivery, nothing more).
func TestDeliveryAloneIsNotDone(t *testing.T) {
	contract := AcceptanceContract{ID: "rem", Criteria: []Criterion{
		{DeliveredAndAcked: strp("occ-1")},
	}}
	v, _ := Grade(contract, []Evidence{
		{Delivery: &DeliveryEvidence{OccurrenceID: "occ-1", DeliveredAt: time.Unix(1, 0)}},
	})
	if v.Pass {
		t.Fatal("delivery receipt alone graded PASS")
	}
}

// Malformed contracts and evidence are rejected, never silently graded.
func TestMalformedInputsRejected(t *testing.T) {
	if _, err := Grade(AcceptanceContract{}, nil); err == nil {
		t.Fatal("contract without id/criteria accepted")
	}
	twoKinds := Criterion{ExitCodeIs: intp(0), DeliveredAndAcked: strp("x")}
	if _, err := Grade(AcceptanceContract{ID: "c", Criteria: []Criterion{twoKinds}}, nil); err == nil {
		t.Fatal("criterion with two kinds accepted")
	}
	empty := Evidence{}
	if _, err := Grade(AcceptanceContract{ID: "c", Criteria: []Criterion{{ExitCodeIs: intp(0)}}},
		[]Evidence{empty}); err == nil {
		t.Fatal("evidence with zero kinds accepted")
	}
}
