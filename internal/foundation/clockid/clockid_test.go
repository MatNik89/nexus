// T05 RED: injected clock/ID — two runs of the same scenario produce
// IDENTICAL event timelines (determinism conformance, HARDQ A2 K0).
package clockid

import (
	"fmt"
	"testing"
	"time"
)

func scenario(c Clock, ids IDGen) []string {
	var timeline []string
	for i := 0; i < 3; i++ {
		id := ids.NewID("ev")
		timeline = append(timeline, fmt.Sprintf("%s@%d", id, c.Now().UnixNano()))
		if f, ok := c.(*Fake); ok {
			f.Advance(time.Second)
		}
	}
	return timeline
}

func TestSameScenarioIdenticalTimelines(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	run1 := scenario(NewFake(start), &SeqIDs{})
	run2 := scenario(NewFake(start), &SeqIDs{})
	if len(run1) != len(run2) {
		t.Fatal("timeline lengths differ")
	}
	for i := range run1 {
		if run1[i] != run2[i] {
			t.Fatalf("timelines diverge at %d: %q vs %q", i, run1[i], run2[i])
		}
	}
}

func TestFakeClockAdvances(t *testing.T) {
	f := NewFake(time.Unix(100, 0))
	t0 := f.Now()
	f.Advance(5 * time.Second)
	if d := f.Since(t0); d != 5*time.Second {
		t.Fatalf("want 5s elapsed, got %v", d)
	}
}

func TestRandomIDsUniqueAndPrefixed(t *testing.T) {
	g := RandomIDs{}
	a, b := g.NewID("x"), g.NewID("x")
	if a == b {
		t.Fatal("two random IDs collided")
	}
	if a[:2] != "x-" {
		t.Fatalf("prefix missing: %q", a)
	}
}
