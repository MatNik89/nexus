//go:build linux

package s7

import (
	"errors"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

func faultOn(types ...string) func([]string) error {
	return func(batch []string) error {
		for _, b := range batch {
			for _, t := range types {
				if b == t {
					return errors.New("injected append fault")
				}
			}
		}
		return nil
	}
}

// Detector 9: an authorization that cannot be journaled issues NO grant and
// leaves the operation PLANNED; the STARTED+companion batch failing leaves
// the lease intact (no consume, attempts 0) — both recover once the journal
// is back.
func TestDurableAppendFaultsFailClosed(t *testing.T) {
	j := openJournal(t, t.TempDir())
	c := &clock{time.Unix(1000, 0)}
	a, _ := New(j, c.now, time.Minute)
	pol := PolicyDelivery
	if err := a.Begin("delivery:f", "channel:tg:delivery:f", pol); err != nil {
		t.Fatal(err)
	}
	a.SetAppendFault(faultOn(EvAttemptAuthorized))
	if _, err := a.Next("delivery:f", ownerBuilder(a, "delivery:f")); !errors.Is(err, ErrNotDurable) {
		t.Fatalf("authorization append fault not surfaced as ErrNotDurable: %v", err)
	}
	if st, _ := a.State("delivery:f"); st != contracts.AttemptPlanned {
		t.Fatalf("state moved without a durable authorization: %s", st)
	}
	if got := countEvents(t, j)[EvAttemptAuthorized]; got != 0 {
		t.Fatalf("authorization journaled despite the fault: %d", got)
	}
	a.SetAppendFault(nil)
	g, err := a.Next("delivery:f", ownerBuilder(a, "delivery:f"))
	if err != nil {
		t.Fatal(err)
	}
	a.SetAppendFault(faultOn(EvAttemptStarted))
	err = a.Consume(g, Companion{Key: "delivery:f", Params: a.params(evOwnerMark, nil)})
	if !errors.Is(err, ErrNotDurable) {
		t.Fatalf("STARTED+park batch fault not surfaced: %v", err)
	}
	if st, _ := a.State("delivery:f"); st != contracts.AttemptAuthorized || a.Attempts("delivery:f") != 0 {
		t.Fatalf("lease lost after a failed STARTED batch: %s attempts=%d", st, a.Attempts("delivery:f"))
	}
	if got := countEvents(t, j); got[EvAttemptStarted] != 0 || got[evOwnerMark] != 0 {
		t.Fatalf("half of the paired batch was journaled: %+v", got)
	}
	a.SetAppendFault(nil)
	if err := a.Consume(g, Companion{Key: "delivery:f", Params: a.params(evOwnerMark, nil)}); err != nil {
		t.Fatalf("consume after the journal recovered: %v", err)
	}
}

// Detector 5/9c: the exhaustion landing is ONE batch — with the former
// inter-append seam faulted, S7 stays non-terminal and NO terminal event or
// companion exists (no S7=FAILED/outbox=PENDING state); when the journal is
// back the landing commits terminal + companion together. A faulted Report
// leaves RUNNING, which rehydrates UNKNOWN (crash semantics), never half-landed.
func TestExhaustionSeamFaultLeavesNoTornState(t *testing.T) {
	j := openJournal(t, t.TempDir())
	c := &clock{time.Unix(1000, 0)}
	a, _ := New(j, c.now, time.Minute)
	pol := Policy{EffectClass: contracts.EffectIrreversible, MaxAttempts: 1, Deadline: time.Second, Durable: true}
	a.Begin("delivery:x", "channel:tg:delivery:x", pol)
	c.advance(2 * time.Second)
	a.SetAppendFault(faultOn(EvOperationTerminal))
	_, err := a.Next("delivery:x", ownerBuilder(a, "delivery:x"))
	if !errors.Is(err, ErrNotDurable) || errors.Is(err, ErrExhausted) {
		t.Fatalf("faulted exhaustion reported as landed: %v", err)
	}
	if st, _ := a.State("delivery:x"); st != contracts.AttemptPlanned {
		t.Fatalf("S7 terminalized without the companion: %s", st)
	}
	if got := countEvents(t, j); got[EvOperationTerminal] != 0 || got[evOwnerMark] != 0 {
		t.Fatalf("torn state: %+v", got)
	}
	a.SetAppendFault(nil)
	if _, err := a.Next("delivery:x", ownerBuilder(a, "delivery:x")); !errors.Is(err, ErrExhausted) {
		t.Fatalf("exhaustion after recovery: %v", err)
	}
	if got := countEvents(t, j); got[EvOperationTerminal] != 1 || got[evOwnerMark] != 1 {
		t.Fatalf("terminal and companion not both committed: %+v", got)
	}
	a.Begin("delivery:r", "channel:tg:delivery:r", PolicyDelivery)
	g, _ := a.Next("delivery:r", ownerBuilder(a, "delivery:r"))
	consumeOK(t, a, g, Companion{Key: "delivery:r", Params: a.params(evOwnerMark, nil)})
	a.SetAppendFault(faultOn(EvAttemptReported))
	if err := a.Report("delivery:r", OutcomeSucceeded, "", ownerBuilder(a, "delivery:r")); !errors.Is(err, ErrNotDurable) {
		t.Fatalf("report fault not surfaced: %v", err)
	}
	if st, _ := a.State("delivery:r"); st != contracts.AttemptRunning {
		t.Fatalf("state after failed report: %s", st)
	}
	a2, _ := New(j, c.now, time.Minute)
	if st, _ := a2.State("delivery:r"); st != contracts.AttemptUnknown {
		t.Fatalf("rehydrated unreported attempt is %s, want UNKNOWN", st)
	}
}
