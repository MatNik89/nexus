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
}

// FileHashCriterion pins a file's content.
type FileHashCriterion struct {
	Path   string
	SHA256 string
}

func (c Criterion) validate() error {
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
	if set != 1 {
		return fmt.Errorf("criterion must set exactly one kind, has %d", set)
	}
	return nil
}

// AcceptanceContract names what "done" means — written from the SPEC side,
// before the work.
type AcceptanceContract struct {
	ID       string
	Criteria []Criterion
}

// Evidence is one closed-kind artifact. Exactly one typed field set.
// There is deliberately NO free-text kind.
type Evidence struct {
	Exit     *ExitEvidence
	FileHash *FileHashEvidence
	Delivery *DeliveryEvidence
	Ack      *AckEvidence
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
	for i, c := range contract.Criteria {
		if err := c.validate(); err != nil {
			return Verdict{}, fmt.Errorf("checker: criterion %d: %w", i, err)
		}
	}
	for i, e := range bundle {
		if err := e.validate(); err != nil {
			return Verdict{}, fmt.Errorf("checker: evidence %d: %w", i, err)
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
		for _, e := range bundle {
			if e.Exit != nil {
				if e.Exit.Code == *c.ExitCodeIs {
					return CriterionResult{Index: idx, Pass: true}
				}
				return fail(fmt.Sprintf("exit code %d, want %d", e.Exit.Code, *c.ExitCodeIs))
			}
		}
		return fail("no exit-code evidence in the bundle")
	case c.FileHashIs != nil:
		for _, e := range bundle {
			if e.FileHash != nil && e.FileHash.Path == c.FileHashIs.Path {
				if e.FileHash.SHA256 == c.FileHashIs.SHA256 {
					return CriterionResult{Index: idx, Pass: true}
				}
				return fail("file content hash does not match the contract")
			}
		}
		return fail("no file-hash evidence for the contracted path")
	case c.DeliveredAndAcked != nil:
		occ := *c.DeliveredAndAcked
		delivered, acked := false, false
		for _, e := range bundle {
			// Correlation is to the SAME occurrence id (B5).
			if e.Delivery != nil && e.Delivery.OccurrenceID == occ {
				delivered = true
			}
			if e.Ack != nil && e.Ack.OccurrenceID == occ {
				acked = true
			}
		}
		switch {
		case !delivered:
			return fail("no delivery receipt for occurrence " + occ)
		case !acked:
			return fail("no user acknowledgment for occurrence " + occ)
		}
		return CriterionResult{Index: idx, Pass: true}
	}
	return fail("unknown criterion kind (fail closed)")
}
