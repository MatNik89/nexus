// T11 RED table (tasks-P0): fail-closed Resolve (unknown/cycle/conflict →
// REJECTED); sealed snapshot — an ablated gate (bwrap probe failed) turns
// exactly its capability OFF while conversation-only startup continues;
// unknown capability in config → reject. Anchored to HARDQ B9 + ledger.
package closure

import (
	"strings"
	"testing"
)

func allProbesPass() []ProbeResult {
	return []ProbeResult{
		{Name: "provider", Passed: true},
		{Name: "store", Passed: true},
		{Name: "channel", Passed: true},
		{Name: "sandbox", Passed: true},
	}
}

func allP0() []string {
	return []string{"conversation", "memory", "obligations", "profiles", "telegram", "exec"}
}

func TestResolveUnknownCapabilityRejected(t *testing.T) {
	if _, err := Resolve(P0Capabilities(), []string{"quantum-mode"}); err == nil {
		t.Fatal("unknown capability accepted")
	}
}

func TestResolveCycleRejected(t *testing.T) {
	cyclic := []Manifest{
		{Name: "a", Requires: []string{"b"}},
		{Name: "b", Requires: []string{"a"}},
	}
	if _, err := Resolve(cyclic, []string{"a"}); err == nil ||
		!strings.Contains(err.Error(), "cycle") {
		t.Fatal("dependency cycle accepted")
	}
}

func TestResolveConflictRejectedTransitively(t *testing.T) {
	ms := []Manifest{
		{Name: "a", Requires: []string{"b"}},
		{Name: "b", Conflicts: []string{"c"}},
		{Name: "c"},
	}
	// b arrives transitively via a; c is requested directly → conflict.
	if _, err := Resolve(ms, []string{"a", "c"}); err == nil ||
		!strings.Contains(err.Error(), "conflicts") {
		t.Fatal("transitive conflict accepted")
	}
}

func TestResolveTopologicalAndDeterministic(t *testing.T) {
	o1, err := Resolve(P0Capabilities(), allP0())
	if err != nil {
		t.Fatal(err)
	}
	o2, _ := Resolve(P0Capabilities(), allP0())
	if strings.Join(o1, ",") != strings.Join(o2, ",") {
		t.Fatal("resolution not deterministic")
	}
	pos := map[string]int{}
	for i, n := range o1 {
		pos[n] = i
	}
	if pos["conversation"] > pos["telegram"] || pos["profiles"] > pos["telegram"] {
		t.Fatalf("dependencies must precede dependents: %v", o1)
	}
}

// The ledger's ablation RED: bwrap probe fails → exec OFF with the probe
// reason, everything else stays ON (conversation-only-style continuation).
func TestAblatedSandboxProbeTurnsOnlyExecOff(t *testing.T) {
	probes := allProbesPass()
	for i := range probes {
		if probes[i].Name == "sandbox" {
			probes[i] = ProbeResult{Name: "sandbox", Passed: false, Detail: "bwrap missing"}
		}
	}
	snap, err := Seal(P0Capabilities(), allP0(), probes)
	if err != nil {
		t.Fatalf("an ablated probe must not abort startup: %v", err)
	}
	if snap.On("exec") {
		t.Fatal("exec ON despite failed sandbox probe")
	}
	if !strings.Contains(snap.Status("exec").Reason, "bwrap missing") {
		t.Fatalf("OFF reason lost: %+v", snap.Status("exec"))
	}
	for _, name := range []string{"conversation", "memory", "obligations", "profiles", "telegram"} {
		if !snap.On(name) {
			t.Fatalf("%s OFF although its probes passed", name)
		}
	}
}

// A MISSING probe result counts as failed (fail closed), and dependents of
// an OFF capability go OFF with a dependency reason.
func TestMissingProbeFailsClosedAndPropagates(t *testing.T) {
	probes := []ProbeResult{
		{Name: "store", Passed: true},
		{Name: "channel", Passed: true},
		{Name: "sandbox", Passed: true},
		// provider probe MISSING entirely
	}
	snap, err := Seal(P0Capabilities(), allP0(), probes)
	if err != nil {
		t.Fatal(err)
	}
	if snap.On("conversation") {
		t.Fatal("conversation ON with its probe missing")
	}
	if snap.On("telegram") {
		t.Fatal("telegram ON although it requires OFF conversation")
	}
	if !strings.Contains(snap.Status("telegram").Reason, "requires") {
		t.Fatalf("dependency OFF reason lost: %+v", snap.Status("telegram"))
	}
	if !snap.On("profiles") {
		t.Fatal("profiles must stay ON (independent of provider)")
	}
}

// Unknown capability names queried against the sealed snapshot are OFF —
// never a skippable error.
func TestUnknownCapabilityQueriesOff(t *testing.T) {
	snap, err := Seal(P0Capabilities(), allP0(), allProbesPass())
	if err != nil {
		t.Fatal(err)
	}
	if snap.On("does-not-exist") {
		t.Fatal("unknown capability reported ON")
	}
}
