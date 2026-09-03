// T12 RED table (tasks-P0): "done" with an empty EvidenceBundle → FAIL;
// an ack for occurrence N grading a contract for N+1 → FAIL (HARDQ B5);
// coding-style (exit/diff-hash) and assistant-style (receipt+ack) contracts
// both gradeable. Anchored to E13 + B5.
package checker

import (
	"testing"
	"time"
)

func intp(i int) *int       { return &i }
func strp(s string) *string { return &s }

func TestEmptyBundleFailsEveryCriterion(t *testing.T) {
	contract := AcceptanceContract{ID: "c1", Worker: "w", Criteria: []Criterion{
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
	contract := AcceptanceContract{ID: "build", Worker: "w", Criteria: []Criterion{
		{ExitCodeIs: intp(0)},
		{FileHashIs: &FileHashCriterion{Path: "notes.txt", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
	}}
	pass, err := Grade(contract, []Evidence{
		{Contract: "build", Producer: "runner", Exit: &ExitEvidence{Code: 0}},
		{Contract: "build", Producer: "runner", FileHash: &FileHashEvidence{Path: "notes.txt", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
	})
	if err != nil || !pass.Pass {
		t.Fatalf("valid coding evidence must pass: %+v %v", pass, err)
	}
	failV, _ := Grade(contract, []Evidence{
		{Contract: "build", Producer: "runner", Exit: &ExitEvidence{Code: 1}},
		{Contract: "build", Producer: "runner", FileHash: &FileHashEvidence{Path: "notes.txt", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
	})
	if failV.Pass {
		t.Fatal("nonzero exit graded PASS")
	}
	wrongHash, _ := Grade(contract, []Evidence{
		{Contract: "build", Producer: "runner", Exit: &ExitEvidence{Code: 0}},
		{Contract: "build", Producer: "runner", FileHash: &FileHashEvidence{Path: "notes.txt", SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}},
	})
	if wrongHash.Pass {
		t.Fatal("wrong file hash graded PASS")
	}
}

// HARDQ B5 literal: receipt+ack must correlate to the SAME occurrence — an
// ack for N never closes N+1.
func TestAckForWrongOccurrenceFails(t *testing.T) {
	contract := AcceptanceContract{ID: "rem", Worker: "w", Criteria: []Criterion{
		{DeliveredAndAcked: strp("occ-2")},
	}}
	v, err := Grade(contract, []Evidence{
		{Contract: "rem", Producer: "gateway", Delivery: &DeliveryEvidence{OccurrenceID: "occ-2", DeliveredAt: time.Unix(1, 0)}},
		{Contract: "rem", Producer: "gateway", Ack: &AckEvidence{OccurrenceID: "occ-1", AckAt: time.Unix(2, 0)}}, // wrong occurrence
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Pass {
		t.Fatal("ack for occurrence 1 closed occurrence 2")
	}
	ok, _ := Grade(contract, []Evidence{
		{Contract: "rem", Producer: "gateway", Delivery: &DeliveryEvidence{OccurrenceID: "occ-2", DeliveredAt: time.Unix(1, 0)}},
		{Contract: "rem", Producer: "gateway", Ack: &AckEvidence{OccurrenceID: "occ-2", AckAt: time.Unix(2, 0)}},
	})
	if !ok.Pass {
		t.Fatal("correctly correlated receipt+ack failed")
	}
}

// Delivery WITHOUT ack is not done (receipt proves delivery, nothing more).
func TestDeliveryAloneIsNotDone(t *testing.T) {
	contract := AcceptanceContract{ID: "rem", Worker: "w", Criteria: []Criterion{
		{DeliveredAndAcked: strp("occ-1")},
	}}
	v, _ := Grade(contract, []Evidence{
		{Contract: "rem", Producer: "gateway", Delivery: &DeliveryEvidence{OccurrenceID: "occ-1", DeliveredAt: time.Unix(1, 0)}},
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
	if _, err := Grade(AcceptanceContract{ID: "c", Worker: "w", Criteria: []Criterion{twoKinds}}, nil); err == nil {
		t.Fatal("criterion with two kinds accepted")
	}
	empty := Evidence{}
	if _, err := Grade(AcceptanceContract{ID: "c", Worker: "w", Criteria: []Criterion{{ExitCodeIs: intp(0)}}},
		[]Evidence{empty}); err == nil {
		t.Fatal("evidence with zero kinds accepted")
	}
}

// checker != worker ENFORCED (codex #1 literal): the judged worker cannot
// grade its own work.
func TestWorkerCannotSupplyOwnEvidence(t *testing.T) {
	contract := AcceptanceContract{ID: "c", Worker: "worker-1", Criteria: []Criterion{
		{ExitCodeIs: intp(0)},
	}}
	_, err := Grade(contract, []Evidence{
		{Contract: "c", Producer: "worker-1", Exit: &ExitEvidence{Code: 0}},
	})
	if err == nil {
		t.Fatal("self-produced evidence accepted — checker == worker")
	}
	ok, err := Grade(contract, []Evidence{
		{Contract: "c", Producer: "runner", Exit: &ExitEvidence{Code: 0}},
	})
	if err != nil || !ok.Pass {
		t.Fatalf("trusted-producer evidence rejected: %v %v", ok, err)
	}
}

// Contradictory duplicates FAIL in EVERY order (codex #3 + kilo #2: no
// ordering games in either direction).
func TestContradictoryEvidenceFailsBothOrders(t *testing.T) {
	contract := AcceptanceContract{ID: "c", Worker: "w", Criteria: []Criterion{{ExitCodeIs: intp(0)}}}
	a := Evidence{Contract: "c", Producer: "runner", Exit: &ExitEvidence{Code: 0}}
	b := Evidence{Contract: "c", Producer: "runner", Exit: &ExitEvidence{Code: 1}}
	for _, bundle := range [][]Evidence{{a, b}, {b, a}} {
		v, err := Grade(contract, bundle)
		if err != nil {
			t.Fatal(err)
		}
		if v.Pass {
			t.Fatal("contradictory exit evidence graded PASS (order-dependent verdict)")
		}
	}
}

// Fabricated zero-value reminder evidence is rejected at validation
// (codex #2 literal).
func TestZeroValueDeliveryEvidenceRejected(t *testing.T) {
	contract := AcceptanceContract{ID: "c", Worker: "w", Criteria: []Criterion{{DeliveredAndAcked: strp("o1")}}}
	if _, err := Grade(contract, []Evidence{
		{Contract: "c", Producer: "gateway", Delivery: &DeliveryEvidence{}},
	}); err == nil {
		t.Fatal("empty-occurrence delivery evidence accepted")
	}
	if _, err := Grade(contract, []Evidence{
		{Delivery: &DeliveryEvidence{OccurrenceID: "o1", DeliveredAt: time.Unix(1, 0)}},
	}); err == nil {
		t.Fatal("producer-less evidence accepted")
	}
}

// Hash SHAPE is validated (codex #4: "abc123" is not a sha256).
func TestNonSha256HashRejected(t *testing.T) {
	bad := Criterion{FileHashIs: &FileHashCriterion{Path: "f", SHA256: "abc123"}}
	if _, err := Grade(AcceptanceContract{ID: "c", Worker: "w", Criteria: []Criterion{bad}}, nil); err == nil {
		t.Fatal("non-sha256 criterion accepted")
	}
}

// Worker identity is REQUIRED (r2 codex #15: omitting it silently disarmed
// checker != worker) and evidence is contract-BOUND (no cross-contract
// replay of a valid artifact).
func TestWorkerRequiredAndEvidenceContractBound(t *testing.T) {
	if _, err := Grade(AcceptanceContract{ID: "c", Criteria: []Criterion{{ExitCodeIs: intp(0)}}}, nil); err == nil {
		t.Fatal("contract without a worker identity accepted")
	}
	contract := AcceptanceContract{ID: "b", Worker: "w", Criteria: []Criterion{{ExitCodeIs: intp(0)}}}
	if _, err := Grade(contract, []Evidence{
		{Contract: "a", Producer: "runner", Exit: &ExitEvidence{Code: 0}}, // replayed from contract "a"
	}); err == nil {
		t.Fatal("evidence bound to another contract accepted (replay)")
	}
	if _, err := Grade(contract, []Evidence{
		{Producer: "runner", Exit: &ExitEvidence{Code: 0}}, // unbound
	}); err == nil {
		t.Fatal("contract-unbound evidence accepted")
	}
}

// B5 ordering: an ack timestamped BEFORE every delivery of its occurrence
// is a contradiction, never proof (r2 codex #17 / kilo #15 literal).
func TestAckBeforeDeliveryFails(t *testing.T) {
	contract := AcceptanceContract{ID: "rem", Worker: "w", Criteria: []Criterion{
		{DeliveredAndAcked: strp("occ-1")},
	}}
	v, err := Grade(contract, []Evidence{
		{Contract: "rem", Producer: "gateway", Delivery: &DeliveryEvidence{OccurrenceID: "occ-1", DeliveredAt: time.Unix(10, 0)}},
		{Contract: "rem", Producer: "gateway", Ack: &AckEvidence{OccurrenceID: "occ-1", AckAt: time.Unix(5, 0)}}, // before delivery
	})
	if err != nil {
		t.Fatal(err)
	}
	if v.Pass {
		t.Fatal("ack preceding its delivery graded PASS")
	}
	// Redelivery AFTER the ack does not break a legitimate pair: the ack
	// need only follow the EARLIEST delivery.
	ok, _ := Grade(contract, []Evidence{
		{Contract: "rem", Producer: "gateway", Delivery: &DeliveryEvidence{OccurrenceID: "occ-1", DeliveredAt: time.Unix(10, 0)}},
		{Contract: "rem", Producer: "gateway", Ack: &AckEvidence{OccurrenceID: "occ-1", AckAt: time.Unix(20, 0)}},
		{Contract: "rem", Producer: "gateway", Delivery: &DeliveryEvidence{OccurrenceID: "occ-1", DeliveredAt: time.Unix(30, 0)}},
	})
	if !ok.Pass {
		t.Fatal("legitimate delivery→ack pair failed under redelivery")
	}
}
