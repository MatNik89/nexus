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
		EmittedAt: time.Unix(1000, 0), ActorType: ActorUser, ActorID: "actor-1",
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
		"actor_type":    func(p *EnvelopeParams) { p.ActorType = ActorInvalid },
		"actor_type_oob": func(p *EnvelopeParams) { p.ActorType = ActorType(99) },
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
		BlockID: "b1", Kind: "text", ContentHash: "h", SourceURI: "local:test",
		Producer: "test", Trust: TrustUser,
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
		SourceURI: "local:test", Producer: "test",
		Trust: TrustClass(99), Sensitivity: SensitivityInternal,
		Lineage: []string{}, ObservedAt: time.Unix(1000, 0),
	}
	if _, err := NewContextBlock(p); err == nil {
		t.Fatal("unknown trust_class accepted")
	}
	p.Trust = TrustUser
	p.Sensitivity = Sensitivity(99)
	if _, err := NewContextBlock(p); err == nil {
		t.Fatal("unknown sensitivity accepted")
	}
	p.Sensitivity = SensitivityInternal
	p.Lineage = nil
	if _, err := NewContextBlock(p); err == nil {
		t.Fatal("absent lineage accepted (must be present, possibly empty)")
	}
	p.Lineage = []string{}
	p.SourceURI = ""
	if _, err := NewContextBlock(p); err == nil {
		t.Fatal("empty source_uri accepted")
	}
	p.SourceURI = "local:test"
	p.Producer = ""
	if _, err := NewContextBlock(p); err == nil {
		t.Fatal("empty producer accepted")
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
		`"run_id":"r","sequence":1,"emitted_at":"2026-01-01T00:00:00Z","actor_type":"USER",` +
		`"actor_id":"a","principal_id":"p","workspace_id":"w","attempt_no":1,` +
		`"payload":{"k":1},"payload_hash":"h"}`)
	if _, err := ParseEnvelope(raw); err == nil || !strings.Contains(err.Error(), "profile_id") {
		t.Fatalf("wire envelope without profile_id must fail on profile_id, got: %v", err)
	}
}


func validBlock(t *testing.T) ContextBlock {
	t.Helper()
	c := "content"
	b, err := NewContextBlock(ContextBlockParams{
		BlockID: "b1", Kind: "text", Content: &c, ContentHash: "h",
		SourceURI: "local:test", Producer: "test", Trust: TrustUser,
		Sensitivity: SensitivityInternal, Lineage: []string{}, ObservedAt: time.Unix(1000, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Enums on the JSON wire are CLOSED STRINGS (Phase-1A codex #4 / kilo #1).
func TestEnumJSONWireIsClosedStrings(t *testing.T) {
	b := validBlock(t)
	msg, err := NewMessage("m1", RoleUser, []ContextBlock{b}, time.Unix(1000, 0))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"role":"USER"`) || !strings.Contains(string(raw), `"trust_class":"USER"`) {
		t.Fatalf("enums must marshal as canonical strings: %s", raw)
	}
	var back Message
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("canonical round trip failed: %v", err)
	}
	// Unknown string discriminator rejected.
	if err := json.Unmarshal([]byte(strings.Replace(string(raw), `"role":"USER"`, `"role":"SUPERUSER"`, 1)), &back); err == nil {
		t.Fatal("unknown role string accepted from the wire")
	}
	// NUMERIC discriminator rejected (numbers could smuggle out-of-range
	// values past Valid()).
	if err := json.Unmarshal([]byte(strings.Replace(string(raw), `"role":"USER"`, `"role":2`, 1)), &back); err == nil {
		t.Fatal("numeric role accepted from the wire")
	}
	// Invalid member cannot even be marshaled.
	if _, err := json.Marshal(Role(99)); err == nil {
		t.Fatal("out-of-range enum marshaled")
	}
}

// Recursive validation: a forged zero block inside a Message is rejected
// (Phase-1A codex #6).
func TestMessageRejectsForgedBlock(t *testing.T) {
	if _, err := NewMessage("m1", RoleUser, []ContextBlock{{}}, time.Unix(1000, 0)); err == nil {
		t.Fatal("zero-value ContextBlock accepted inside a Message")
	}
}

func TestToolResultRejectsForgedCall(t *testing.T) {
	if _, err := NewToolResult(ToolCall{}, ResultSucceeded, nil, nil, time.Unix(1,0), time.Unix(2,0), nil); err == nil {
		t.Fatal("zero-value ToolCall accepted as the originating call")
	}
}

// Constructor-level schema admission (Phase-1A codex #3): the wire parser
// is not the only gate.
func TestConstructorRejectsUnknownSchema(t *testing.T) {
	p := validEnvelopeParams()
	p.SchemaID = "evil.schema"
	if _, err := NewEnvelope(p); err == nil {
		t.Fatal("unknown schema accepted by the constructor")
	}
	p = validEnvelopeParams()
	p.SchemaVersion = 99
	if _, err := NewEnvelope(p); err == nil {
		t.Fatal("unknown schema version accepted by the constructor")
	}
}

// Known-schema unknown FIELDS are preserved via Wire (Phase-1A codex #5).
func TestParseEnvelopePreservesUnknownFields(t *testing.T) {
	env, err := NewEnvelope(validEnvelopeParams())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(env)
	extended := strings.Replace(string(raw), `{`, `{"future_ext_field":"keep-me",`, 1)
	back, err := ParseEnvelope([]byte(extended))
	if err != nil {
		t.Fatalf("known schema with extra field must be admitted: %v", err)
	}
	if !strings.Contains(string(back.Wire), "keep-me") {
		t.Fatal("unknown field lost — Wire must preserve the admitted bytes")
	}
	// LOSSLESS wire round trip (r2 codex #4): parse → marshal → parse keeps
	// the unknown field in the actual serialized output, not just a sidecar.
	re, err := json.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(re), "keep-me") {
		t.Fatalf("re-marshal dropped the unknown field: %s", re)
	}
	back2, err := ParseEnvelope(re)
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if back2.EventID != back.EventID {
		t.Fatal("second round trip lost known fields")
	}
}

// Nested normalization (r2 codex #6): a non-UTC block inside a Message
// comes out canonical, and post-construction mutation of the caller's
// slice does not reach the constructed Message.
func TestMessageNormalizesAndCopiesBlocks(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	c := "content"
	blk := ContextBlock{
		BlockID: "b1", Kind: "text", Content: &c, ContentHash: "h",
		SourceURI: "local:test", Producer: "test", Trust: TrustUser,
		Sensitivity: SensitivityInternal, Lineage: []string{"a"},
		ObservedAt: time.Date(2026, 1, 1, 12, 0, 0, 0, loc),
	}
	in := []ContextBlock{blk}
	msg, err := NewMessage("m1", RoleUser, in, time.Unix(1000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if msg.Blocks[0].ObservedAt.Location() != time.UTC {
		t.Fatal("nested block not normalized to UTC")
	}
	in[0].Lineage[0] = "mutated"
	*in[0].Content = "mutated"
	if msg.Blocks[0].Lineage[0] == "mutated" || *msg.Blocks[0].Content == "mutated" {
		t.Fatal("constructed Message aliases caller-owned memory")
	}
}

// Every persisted timestamp is UTC (Phase-1A codex #8).
func TestTimestampsNormalizedToUTC(t *testing.T) {
	loc := time.FixedZone("CET", 3600)
	p := validEnvelopeParams()
	p.EmittedAt = time.Date(2026, 1, 1, 12, 0, 0, 0, loc)
	env, err := NewEnvelope(p)
	if err != nil {
		t.Fatal(err)
	}
	if env.EmittedAt.Location() != time.UTC {
		t.Fatalf("emitted_at not UTC: %v", env.EmittedAt.Location())
	}
}

func TestManualRecoveryStatesExist(t *testing.T) {
	if !RunManualRecovery.Valid() || RunManualRecovery.String() != "MANUAL_RECOVERY" {
		t.Fatal("RunManualRecovery missing/mislabeled (P0.1 UNKNOWN exit vocabulary)")
	}
	if !AttemptManualRecovery.Valid() {
		t.Fatal("AttemptManualRecovery missing")
	}
}
