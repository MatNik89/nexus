// T04 RED table (tasks-P0): constructors must reject every malformed case
// in the Annex P0.1 MUST lists; unknown discriminators and unknown schema
// versions are rejected (schema: unprocessed, raw preserved — HARDQ C7).
// Assertions anchor to the spec lists, never to the implementation.
package contracts

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func str(s string) *string { return &s }

func validEnvelopeParams() EnvelopeParams {
	return EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1, EventID: "ev-1",
		EventType: "run.created", RunID: "run-1", Sequence: 1,
		EmittedAt: time.Unix(1000, 0), ActorType: "user", ActorID: "actor-1",
		PrincipalID: "matej", WorkspaceID: "ws-1", ProfileID: "work",
		AttemptNo: 1, Payload: json.RawMessage(`{"k":"v"}`), PayloadHash: "h",
	}
}

func TestEnvelopeConstructorAcceptsValid(t *testing.T) {
	if _, err := NewEnvelope(validEnvelopeParams()); err != nil {
		t.Fatalf("valid envelope rejected: %v", err)
	}
}

func TestEnvelopeConstructorRejectsEachMissingMust(t *testing.T) {
	cases := map[string]func(*EnvelopeParams){
		"schema_id":     func(p *EnvelopeParams) { p.SchemaID = "" },
		"event_id":      func(p *EnvelopeParams) { p.EventID = "" },
		"event_type":    func(p *EnvelopeParams) { p.EventType = "" },
		"run_id":        func(p *EnvelopeParams) { p.RunID = "" },
		"emitted_at":    func(p *EnvelopeParams) { p.EmittedAt = time.Time{} },
		"actor_type":    func(p *EnvelopeParams) { p.ActorType = "" },
		"actor_id":      func(p *EnvelopeParams) { p.ActorID = "" },
		"principal_id":  func(p *EnvelopeParams) { p.PrincipalID = "" },
		"workspace_id":  func(p *EnvelopeParams) { p.WorkspaceID = "" },
		"profile_id":    func(p *EnvelopeParams) { p.ProfileID = "" }, // B3: non-null always
		"attempt_no":    func(p *EnvelopeParams) { p.AttemptNo = 0 },
		"payload":       func(p *EnvelopeParams) { p.Payload = json.RawMessage(`{not json`) },
		"payload_hash":  func(p *EnvelopeParams) { p.PayloadHash = "" },
		"schema_version": func(p *EnvelopeParams) { p.SchemaVersion = 0 },
		"control-char-id": func(p *EnvelopeParams) { p.RunID = "run\x00evil" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := validEnvelopeParams()
			mutate(&p)
			if _, err := NewEnvelope(p); err == nil {
				t.Fatalf("envelope with broken %s accepted", name)
			}
		})
	}
}

func TestContextBlockXOR(t *testing.T) {
	base := ContextBlockParams{
		BlockID: "b1", Kind: "text", ContentHash: "h", Trust: TrustUser,
		Sensitivity: SensitivityInternal, Lineage: []string{}, ObservedAt: time.Unix(1000, 0),
	}
	both := base
	both.Content, both.ContentRef = str("x"), str("ref")
	if _, err := NewContextBlock(both); err == nil {
		t.Fatal("block with BOTH content and content_ref accepted (XOR violated)")
	}
	neither := base
	if _, err := NewContextBlock(neither); err == nil {
		t.Fatal("block with NEITHER content nor content_ref accepted (XOR violated)")
	}
	one := base
	one.Content = str("x")
	if _, err := NewContextBlock(one); err != nil {
		t.Fatalf("valid single-content block rejected: %v", err)
	}
}

func TestContextBlockRejectsInvalidProvenance(t *testing.T) {
	p := ContextBlockParams{
		BlockID: "b1", Kind: "text", Content: str("x"), ContentHash: "h",
		Trust: TrustClass(99), Sensitivity: SensitivityInternal,
		Lineage: []string{}, ObservedAt: time.Unix(1000, 0),
	}
	if _, err := NewContextBlock(p); err == nil {
		t.Fatal("unknown trust_class accepted")
	}
	p.Trust = TrustUser
	p.Lineage = nil
	if _, err := NewContextBlock(p); err == nil {
		t.Fatal("absent lineage accepted (must be present, possibly empty)")
	}
}

func TestToolCallIdempotencyMandatoryForStateChanging(t *testing.T) {
	base := ToolCallParams{
		ToolCallID: "tc1", ToolID: "exec", Arguments: json.RawMessage(`{}`),
		ArgsSchemaHash: "h", Effect: EffectReversible, ExecutionKind: ExecProcess,
		Deadline: time.Unix(2000, 0), AttemptNo: 1, ProfileID: "work",
	}
	if _, err := NewToolCall(base); err == nil {
		t.Fatal("state-changing call WITHOUT idempotency_key accepted")
	}
	withKey := base
	withKey.IdempotencyKey = str("idem-1")
	if _, err := NewToolCall(withKey); err != nil {
		t.Fatalf("valid state-changing call rejected: %v", err)
	}
	readOnly := base
	readOnly.Effect = EffectReadOnly
	if _, err := NewToolCall(readOnly); err != nil {
		t.Fatalf("read-only call without idempotency_key must be fine: %v", err)
	}
}

func TestToolCallRejectsInvalidKinds(t *testing.T) {
	p := ToolCallParams{
		ToolCallID: "tc1", ToolID: "exec", Arguments: json.RawMessage(`{}`),
		ArgsSchemaHash: "h", Effect: EffectClass(42), ExecutionKind: ExecProcess,
		Deadline: time.Unix(2000, 0), AttemptNo: 1, ProfileID: "work",
		IdempotencyKey: str("k"),
	}
	if _, err := NewToolCall(p); err == nil {
		t.Fatal("unknown effect_class accepted")
	}
	p.Effect = EffectReadOnly
	p.ExecutionKind = ExecutionKind(42)
	if _, err := NewToolCall(p); err == nil {
		t.Fatal("unknown execution_kind accepted")
	}
}

func TestToolResultCorrelation(t *testing.T) {
	call, err := NewToolCall(ToolCallParams{
		ToolCallID: "tc1", ToolID: "exec", Arguments: json.RawMessage(`{}`),
		ArgsSchemaHash: "h", Effect: EffectReadOnly, ExecutionKind: ExecProcess,
		Deadline: time.Unix(2000, 0), AttemptNo: 2, ProfileID: "work",
	})
	if err != nil {
		t.Fatal(err)
	}
	t0, t1 := time.Unix(1000, 0), time.Unix(1001, 0)
	// FAILED without a typed error → reject.
	if _, err := NewToolResult(call, ResultFailed, nil, nil, t0, t1, nil); err == nil {
		t.Fatal("FAILED result without typed error accepted")
	}
	// Receipt for a DIFFERENT attempt → reject (P1.4 seed: receipts bind
	// exact call+attempt).
	badReceipt := &CommitReceipt{Phase: PhaseAfterCommit, ToolCallID: "tc1", AttemptNo: 1, ContentHash: "h"}
	if _, err := NewToolResult(call, ResultSucceeded, nil, nil, t0, t1, badReceipt); err == nil {
		t.Fatal("receipt attesting a different attempt accepted")
	}
	// Ordered timestamps enforced.
	if _, err := NewToolResult(call, ResultSucceeded, nil, nil, t1, t0, nil); err == nil {
		t.Fatal("finished_at before started_at accepted")
	}
	good := &CommitReceipt{Phase: PhaseAfterCommit, ToolCallID: "tc1", AttemptNo: 2, ContentHash: "h"}
	if _, err := NewToolResult(call, ResultSucceeded, nil, nil, t0, t1, good); err != nil {
		t.Fatalf("valid result rejected: %v", err)
	}
}

func TestTypedErrorClosedEnums(t *testing.T) {
	if _, err := NewTypedError("X", ErrorCategory(77), RetryNever, "m", "o", nil, nil); err == nil {
		t.Fatal("unknown category accepted")
	}
	if _, err := NewTypedError("X", ErrCatTimeout, RetryAfter, "m", "o", nil, nil); err == nil {
		t.Fatal("retryability AFTER without retry_after accepted")
	}
	d := 5 * time.Second
	if _, err := NewTypedError("X", ErrCatTimeout, RetryAfter, "m", "o", &d, nil); err != nil {
		t.Fatalf("valid typed error rejected: %v", err)
	}
}

func TestUnknownDiscriminatorParsersFailClosed(t *testing.T) {
	if _, err := ParseTrustClass("SYSTEM"); err != nil {
		t.Fatalf("known discriminator rejected: %v", err)
	}
	for _, parse := range []func() error{
		func() error { _, err := ParseTrustClass("TOTALLY_TRUSTED"); return err },
		func() error { _, err := ParseEffectClass("MOSTLY_HARMLESS"); return err },
		func() error { _, err := ParseExecutionKind("INLINE"); return err },
		func() error { _, err := ParseDecision("MAYBE"); return err },
		func() error { _, err := ParsePolicyMode("SUPER_YOLO"); return err },
		func() error { _, err := ParseResultStatus("KINDA_OK"); return err },
	} {
		if err := parse(); err == nil {
			t.Fatal("unknown discriminator accepted (must fail closed)")
		}
	}
}

func TestZeroEnumValuesAreInvalid(t *testing.T) {
	// The zero value of every enum must be Invalid (fail-closed defaults).
	if Role(0).Valid() || EffectClass(0).Valid() || TrustClass(0).Valid() ||
		Sensitivity(0).Valid() || ErrorCategory(0).Valid() || Retryability(0).Valid() ||
		ExecutionKind(0).Valid() || EffectPhase(0).Valid() || Decision(0).Valid() ||
		PolicyMode(0).Valid() || RunState(0).Valid() || TurnState(0).Valid() ||
		AttemptState(0).Valid() || ResultStatus(0).Valid() {
		t.Fatal("an enum's zero value validates — fail-closed default broken")
	}
}

func TestParseEnvelopeUnknownSchemaPreservesRaw(t *testing.T) {
	raw := []byte(`{"schema_id":"nexus.event","schema_version":99,"junk":true}`)
	_, err := ParseEnvelope(raw)
	var ue *UnknownSchemaError
	if !errors.As(err, &ue) {
		t.Fatalf("unknown schema version must yield UnknownSchemaError, got: %v", err)
	}
	if string(ue.Raw) != string(raw) {
		t.Fatal("raw input not preserved byte-for-byte")
	}
	if ue.SchemaVersion != 99 {
		t.Fatalf("wrong version in rejection: %d", ue.SchemaVersion)
	}
	// Unknown schema ID likewise.
	if _, err := ParseEnvelope([]byte(`{"schema_id":"evil.schema","schema_version":1}`)); err == nil {
		t.Fatal("unknown schema id accepted")
	} else if !errors.As(err, &ue) {
		t.Fatalf("want UnknownSchemaError, got %v", err)
	}
}

func TestParseEnvelopeKnownSchemaRoundTrip(t *testing.T) {
	env, err := NewEnvelope(validEnvelopeParams())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseEnvelope(raw)
	if err != nil {
		t.Fatalf("round trip failed: %v", err)
	}
	if back.EventID != env.EventID || back.ProfileID != env.ProfileID || back.Sequence != env.Sequence {
		t.Fatal("round trip lost fields")
	}
}

func TestParseEnvelopeWireObeysSameMusts(t *testing.T) {
	// A wire envelope with a missing profile_id must be rejected by the SAME
	// validation as local construction (single owner).
	raw := []byte(`{"schema_id":"nexus.event","schema_version":1,"event_id":"e","event_type":"t",` +
		`"run_id":"r","sequence":1,"emitted_at":"2026-01-01T00:00:00Z","actor_type":"user",` +
		`"actor_id":"a","principal_id":"p","workspace_id":"w","attempt_no":1,` +
		`"payload":{"k":1},"payload_hash":"h"}`)
	if _, err := ParseEnvelope(raw); err == nil || !strings.Contains(err.Error(), "profile_id") {
		t.Fatalf("wire envelope without profile_id must fail on profile_id, got: %v", err)
	}
}
