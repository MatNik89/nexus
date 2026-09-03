// T11 RED table (tasks-P0): fail-closed Resolve (unknown/cycle/conflict →
// REJECTED); sealed snapshot — an ablated gate (bwrap probe failed) turns
// exactly its capability OFF while conversation-only startup continues;
// unknown capability in config → reject. Anchored to HARDQ B9 + ledger.
package closure

import (
	"strings"
	"testing"

	"github.com/MatNik89/nexus/internal/foundation/config"
)

// The REAL config owner computes the binding (r3 codex #10): tests seal
// against an actually-resolved configuration, not an invented token.
func mustResolve(cli map[string]string) config.Resolved {
	r, err := config.Resolve("/nonexistent", "/nonexistent", nil, cli)
	if err != nil {
		panic(err)
	}
	return r
}

var cfgBinding = mustResolve(nil)
var cfgHash = cfgBinding.ConfigHash()

// fakeBinding exists ONLY to exercise the shape rejections.
type fakeBinding string

func (f fakeBinding) ConfigHash() string { return string(f) }

func allProbesPass() []ProbeResult {
	return []ProbeResult{
		{Name: "provider", Passed: true, ConfigHash: cfgHash},
		{Name: "store", Passed: true, ConfigHash: cfgHash},
		{Name: "channel", Passed: true, ConfigHash: cfgHash},
		{Name: "sandbox", Passed: true, ConfigHash: cfgHash},
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
			probes[i] = ProbeResult{Name: "sandbox", Passed: false, Detail: "bwrap missing", ConfigHash: cfgHash}
		}
	}
	snap, err := Seal(P0Capabilities(), allP0(), probes, cfgBinding)
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
		{Name: "store", Passed: true, ConfigHash: cfgHash},
		{Name: "channel", Passed: true, ConfigHash: cfgHash},
		{Name: "sandbox", Passed: true, ConfigHash: cfgHash},
		// provider probe MISSING entirely
	}
	snap, err := Seal(P0Capabilities(), allP0(), probes, cfgBinding)
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
	snap, err := Seal(P0Capabilities(), allP0(), allProbesPass(), cfgBinding)
	if err != nil {
		t.Fatal(err)
	}
	if snap.On("does-not-exist") {
		t.Fatal("unknown capability reported ON")
	}
}

// Duplicate probe names are rejected in BOTH orders — input order can never
// decide capability state (codex #19 literal).
func TestDuplicateProbeNamesRejectedBothOrders(t *testing.T) {
	dup1 := append(allProbesPass(), ProbeResult{Name: "sandbox", Passed: false, ConfigHash: cfgHash})
	if _, err := Seal(P0Capabilities(), allP0(), dup1, cfgBinding); err == nil {
		t.Fatal("duplicate probe (pass-then-fail) accepted")
	}
	dup2 := append([]ProbeResult{{Name: "sandbox", Passed: false, ConfigHash: cfgHash}}, allProbesPass()...)
	if _, err := Seal(P0Capabilities(), allP0(), dup2, cfgBinding); err == nil {
		t.Fatal("duplicate probe (fail-then-pass) accepted")
	}
}

// A probe measured under a DIFFERENT config hash is stale → its capability
// stays OFF (codex #18: replay-after-config-change literal).
func TestStaleProbeFromOtherConfigTurnsCapabilityOff(t *testing.T) {
	probes := allProbesPass()
	for i := range probes {
		if probes[i].Name == "sandbox" {
			probes[i].ConfigHash = "OLD-config"
		}
	}
	snap, err := Seal(P0Capabilities(), allP0(), probes, cfgBinding)
	if err != nil {
		t.Fatal(err)
	}
	if snap.On("exec") {
		t.Fatal("exec ON from a stale probe measured under another config")
	}
	for _, bad := range []string{"", "cfg-hash-1", "not-a-digest"} {
		if _, err := Seal(P0Capabilities(), allP0(), allProbesPass(), fakeBinding(bad)); err == nil {
			t.Fatalf("non-digest config binding accepted: %q", bad)
		}
	}
	if _, err := Seal(P0Capabilities(), allP0(), allProbesPass(), nil); err == nil {
		t.Fatal("nil config binding accepted")
	}
	if snap.ConfigHash() != cfgHash {
		t.Fatal("snapshot does not retain its config binding")
	}
}

// A conflict declaration naming an UNKNOWN capability is a manifest defect,
// rejected at Resolve (Phase-1B-r2 codex #14 / kilo #11 literal).
func TestUnknownConflictTargetRejected(t *testing.T) {
	manifests := append(P0Capabilities(), Manifest{Name: "extra", Conflicts: []string{"telegarm"}})
	if _, err := Resolve(manifests, []string{"extra"}); err == nil {
		t.Fatal("conflict with unknown capability accepted (typo silently disarmed)")
	}
	// Even when the conflicting manifest is not requested: the manifest SET
	// is invalid.
	if _, err := Resolve(manifests, []string{"conversation"}); err == nil {
		t.Fatal("manifest set with a dangling conflict accepted")
	}
}

// A shape-valid but INVENTED digest cannot attest a differently-resolved
// config (r3 codex #10 literal): probes measured under another real config
// — or under a fabricated 64-hex token — never turn a capability ON when
// sealing under the actual resolved config.
func TestInventedOrForeignDigestCannotAttest(t *testing.T) {
	other := mustResolve(map[string]string{"provider_model": "other-model"})
	if other.ConfigHash() == cfgHash {
		t.Fatal("oracle vacuous: different resolved configs share a digest")
	}
	for name, probeHash := range map[string]string{
		"foreign-config":  other.ConfigHash(),
		"invented-64-hex": "1111111111111111111111111111111111111111111111111111111111111111",
	} {
		probes := allProbesPass()
		for i := range probes {
			probes[i].ConfigHash = probeHash
		}
		snap, err := Seal(P0Capabilities(), allP0(), probes, cfgBinding)
		if err != nil {
			t.Fatal(err)
		}
		for _, cap := range allP0() {
			if snap.On(cap) {
				t.Fatalf("%s: capability %q ON from a probe not measured under the sealed config", name, cap)
			}
		}
	}
	// Determinism: the same resolution yields the same digest.
	if mustResolve(nil).ConfigHash() != cfgHash {
		t.Fatal("config digest is not deterministic")
	}
}
