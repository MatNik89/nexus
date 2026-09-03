package contracts

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Hand-written typed kernel contracts (S0.1, Annex P0.1). These structs are
// the source of truth; schemas are derived projections. Opaque payloads are
// json.RawMessage behind closed discriminator validation — no
// map[string]any (E3). Constructors are the only sanctioned way to build
// values: every P0.1 MUST is enforced there, fail-closed.

// Envelope is the canonical event envelope (P0.1). ProfileID is the
// mandatory admission-time stamp carried immutably through the causal chain
// (HARDQ B3).
type Envelope struct {
	SchemaID      SchemaID        `json:"schema_id"`
	SchemaVersion int             `json:"schema_version"`
	EventID       EventID         `json:"event_id"`
	EventType     string          `json:"event_type"`
	RunID         RunID           `json:"run_id"`
	TurnID        *TurnID         `json:"turn_id,omitempty"`
	ToolCallID    *ToolCallID     `json:"tool_call_id,omitempty"`
	ParentEventID *EventID        `json:"parent_event_id,omitempty"`
	Sequence      uint64          `json:"sequence"`
	EmittedAt     time.Time       `json:"emitted_at"`
	ActorType     string          `json:"actor_type"`
	ActorID       ActorID         `json:"actor_id"`
	PrincipalID   PrincipalID     `json:"principal_id"`
	TenantID      *TenantID       `json:"tenant_id,omitempty"`
	WorkspaceID   WorkspaceID     `json:"workspace_id"`
	ProfileID     ProfileID       `json:"profile_id"`
	AttemptNo     int             `json:"attempt_no"`
	IdempotencyKey *string        `json:"idempotency_key,omitempty"`
	Payload       json.RawMessage `json:"payload"`
	PayloadHash   string          `json:"payload_hash"`
}

// EnvelopeParams carries the constructor inputs; optional fields are
// pointers so absence is explicit, never a zero-value guess.
type EnvelopeParams struct {
	SchemaID      SchemaID
	SchemaVersion int
	EventID       EventID
	EventType     string
	RunID         RunID
	TurnID        *TurnID
	ToolCallID    *ToolCallID
	ParentEventID *EventID
	Sequence      uint64
	EmittedAt     time.Time
	ActorType     string
	ActorID       ActorID
	PrincipalID   PrincipalID
	TenantID      *TenantID
	WorkspaceID   WorkspaceID
	ProfileID     ProfileID
	AttemptNo     int
	IdempotencyKey *string
	Payload       json.RawMessage
	PayloadHash   string
}

func NewEnvelope(p EnvelopeParams) (Envelope, error) {
	var errs []error
	check := func(name, v string) {
		if err := requireID(name, v); err != nil {
			errs = append(errs, err)
		}
	}
	check("schema_id", string(p.SchemaID))
	check("event_id", string(p.EventID))
	check("event_type", p.EventType)
	check("run_id", string(p.RunID))
	check("actor_type", p.ActorType)
	check("actor_id", string(p.ActorID))
	check("principal_id", string(p.PrincipalID))
	check("workspace_id", string(p.WorkspaceID))
	check("profile_id", string(p.ProfileID)) // non-null ProfileID, always (B3)
	if p.SchemaVersion < 1 {
		errs = append(errs, fmt.Errorf("schema_version %d must be >= 1", p.SchemaVersion))
	}
	if p.EmittedAt.IsZero() {
		errs = append(errs, errors.New("emitted_at is required"))
	}
	if p.AttemptNo < 1 {
		errs = append(errs, fmt.Errorf("attempt_no %d must be >= 1", p.AttemptNo))
	}
	if p.TurnID != nil && !p.TurnID.Valid() {
		errs = append(errs, errors.New("turn_id present but invalid"))
	}
	if p.ToolCallID != nil && !p.ToolCallID.Valid() {
		errs = append(errs, errors.New("tool_call_id present but invalid"))
	}
	if p.ParentEventID != nil && !p.ParentEventID.Valid() {
		errs = append(errs, errors.New("parent_event_id present but invalid"))
	}
	if p.TenantID != nil && !p.TenantID.Valid() {
		errs = append(errs, errors.New("tenant_id present but invalid"))
	}
	if len(p.Payload) == 0 || !json.Valid(p.Payload) {
		errs = append(errs, errors.New("payload must be valid JSON"))
	}
	if p.PayloadHash == "" {
		errs = append(errs, errors.New("payload_hash is required"))
	}
	if len(errs) > 0 {
		return Envelope{}, fmt.Errorf("invalid envelope: %w", errors.Join(errs...))
	}
	return Envelope{
		SchemaID: p.SchemaID, SchemaVersion: p.SchemaVersion, EventID: p.EventID,
		EventType: p.EventType, RunID: p.RunID, TurnID: p.TurnID, ToolCallID: p.ToolCallID,
		ParentEventID: p.ParentEventID, Sequence: p.Sequence, EmittedAt: p.EmittedAt,
		ActorType: p.ActorType, ActorID: p.ActorID, PrincipalID: p.PrincipalID,
		TenantID: p.TenantID, WorkspaceID: p.WorkspaceID, ProfileID: p.ProfileID,
		AttemptNo: p.AttemptNo, IdempotencyKey: p.IdempotencyKey,
		Payload: p.Payload, PayloadHash: p.PayloadHash,
	}, nil
}

// ContextBlock (P0.1): exactly one of Content / ContentRef (XOR, enforced),
// with mandatory trust/sensitivity/lineage provenance.
type ContextBlock struct {
	BlockID     BlockID     `json:"block_id"`
	Kind        string      `json:"kind"`
	Content     *string     `json:"content,omitempty"`
	ContentRef  *string     `json:"content_ref,omitempty"`
	ContentHash string      `json:"content_hash"`
	SourceURI   string      `json:"source_uri"`
	Producer    string      `json:"producer"`
	Trust       TrustClass  `json:"trust_class"`
	Sensitivity Sensitivity `json:"sensitivity"`
	Lineage     []string    `json:"lineage"`
	ObservedAt  time.Time   `json:"observed_at"`
	ExpiresAt   *time.Time  `json:"expires_at,omitempty"`
}

type ContextBlockParams struct {
	BlockID     BlockID
	Kind        string
	Content     *string
	ContentRef  *string
	ContentHash string
	SourceURI   string
	Producer    string
	Trust       TrustClass
	Sensitivity Sensitivity
	Lineage     []string
	ObservedAt  time.Time
	ExpiresAt   *time.Time
}

func NewContextBlock(p ContextBlockParams) (ContextBlock, error) {
	var errs []error
	if err := requireID("block_id", string(p.BlockID)); err != nil {
		errs = append(errs, err)
	}
	if p.Kind == "" {
		errs = append(errs, errors.New("kind is required"))
	}
	// Content XOR ContentRef — enforced in the constructor (E3).
	if (p.Content == nil) == (p.ContentRef == nil) {
		errs = append(errs, errors.New("exactly one of content / content_ref is required"))
	}
	if p.ContentHash == "" {
		errs = append(errs, errors.New("content_hash is required"))
	}
	if !p.Trust.Valid() {
		errs = append(errs, fmt.Errorf("trust_class %d invalid", p.Trust))
	}
	if !p.Sensitivity.Valid() {
		errs = append(errs, fmt.Errorf("sensitivity %d invalid", p.Sensitivity))
	}
	if p.ObservedAt.IsZero() {
		errs = append(errs, errors.New("observed_at is required"))
	}
	if p.Lineage == nil {
		errs = append(errs, errors.New("lineage is required (may be empty, never absent)"))
	}
	if len(errs) > 0 {
		return ContextBlock{}, fmt.Errorf("invalid context block: %w", errors.Join(errs...))
	}
	return ContextBlock{
		BlockID: p.BlockID, Kind: p.Kind, Content: p.Content, ContentRef: p.ContentRef,
		ContentHash: p.ContentHash, SourceURI: p.SourceURI, Producer: p.Producer,
		Trust: p.Trust, Sensitivity: p.Sensitivity, Lineage: p.Lineage,
		ObservedAt: p.ObservedAt, ExpiresAt: p.ExpiresAt,
	}, nil
}

// Message (P0.1): every content element is a validated ContextBlock.
type Message struct {
	MessageID MessageID      `json:"message_id"`
	Role      Role           `json:"role"`
	Blocks    []ContextBlock `json:"content_blocks"`
	CreatedAt time.Time      `json:"created_at"`
}

func NewMessage(id MessageID, role Role, blocks []ContextBlock, createdAt time.Time) (Message, error) {
	var errs []error
	if err := requireID("message_id", string(id)); err != nil {
		errs = append(errs, err)
	}
	if !role.Valid() {
		errs = append(errs, fmt.Errorf("role %d invalid", role))
	}
	if createdAt.IsZero() {
		errs = append(errs, errors.New("created_at is required"))
	}
	if len(blocks) == 0 {
		errs = append(errs, errors.New("content_blocks must not be empty"))
	}
	if len(errs) > 0 {
		return Message{}, fmt.Errorf("invalid message: %w", errors.Join(errs...))
	}
	return Message{MessageID: id, Role: role, Blocks: blocks, CreatedAt: createdAt}, nil
}

// ToolCall (P0.1): an idempotency key is MANDATORY for every state-changing
// call (anything not READ_ONLY).
type ToolCall struct {
	ToolCallID     ToolCallID      `json:"tool_call_id"`
	ToolID         ToolID          `json:"tool_id"`
	Arguments      json.RawMessage `json:"arguments"`
	ArgsSchemaHash string          `json:"arguments_schema_hash"`
	Effect         EffectClass     `json:"effect_class"`
	ExecutionKind  ExecutionKind   `json:"execution_kind"`
	Deadline       time.Time       `json:"deadline"`
	AttemptNo      int             `json:"attempt_no"`
	IdempotencyKey *string         `json:"idempotency_key,omitempty"`
	ProfileID      ProfileID       `json:"profile_id"`
}

type ToolCallParams struct {
	ToolCallID     ToolCallID
	ToolID         ToolID
	Arguments      json.RawMessage
	ArgsSchemaHash string
	Effect         EffectClass
	ExecutionKind  ExecutionKind
	Deadline       time.Time
	AttemptNo      int
	IdempotencyKey *string
	ProfileID      ProfileID
}

func NewToolCall(p ToolCallParams) (ToolCall, error) {
	var errs []error
	if err := requireID("tool_call_id", string(p.ToolCallID)); err != nil {
		errs = append(errs, err)
	}
	if err := requireID("tool_id", string(p.ToolID)); err != nil {
		errs = append(errs, err)
	}
	if err := requireID("profile_id", string(p.ProfileID)); err != nil {
		errs = append(errs, err)
	}
	if len(p.Arguments) == 0 || !json.Valid(p.Arguments) {
		errs = append(errs, errors.New("arguments must be valid JSON"))
	}
	if p.ArgsSchemaHash == "" {
		errs = append(errs, errors.New("arguments_schema_hash is required"))
	}
	if !p.Effect.Valid() {
		errs = append(errs, fmt.Errorf("effect_class %d invalid", p.Effect))
	}
	if !p.ExecutionKind.Valid() {
		errs = append(errs, fmt.Errorf("execution_kind %d invalid", p.ExecutionKind))
	}
	if p.Deadline.IsZero() {
		errs = append(errs, errors.New("deadline is required"))
	}
	if p.AttemptNo < 1 {
		errs = append(errs, fmt.Errorf("attempt_no %d must be >= 1", p.AttemptNo))
	}
	if p.Effect != EffectReadOnly && (p.IdempotencyKey == nil || *p.IdempotencyKey == "") {
		errs = append(errs, errors.New("idempotency_key is mandatory for state-changing calls"))
	}
	if len(errs) > 0 {
		return ToolCall{}, fmt.Errorf("invalid tool call: %w", errors.Join(errs...))
	}
	return ToolCall{
		ToolCallID: p.ToolCallID, ToolID: p.ToolID, Arguments: p.Arguments,
		ArgsSchemaHash: p.ArgsSchemaHash, Effect: p.Effect, ExecutionKind: p.ExecutionKind,
		Deadline: p.Deadline, AttemptNo: p.AttemptNo, IdempotencyKey: p.IdempotencyKey,
		ProfileID: p.ProfileID,
	}, nil
}

// CommitReceipt attests when an effect committed (DESIGN-FIXES-r2); it is
// valid only for the exact (call, attempt) it names.
type CommitReceipt struct {
	Phase       EffectPhase `json:"phase"`
	ToolCallID  ToolCallID  `json:"tool_call_id"`
	AttemptNo   int         `json:"attempt_no"`
	ContentHash string      `json:"content_hash"`
}

// ValidFor reports whether the receipt attests THIS call+attempt.
func (r CommitReceipt) ValidFor(c ToolCall) bool {
	return r.Phase.Valid() &&
		r.ToolCallID == c.ToolCallID &&
		r.AttemptNo == c.AttemptNo &&
		r.ContentHash != ""
}

// ToolResult (P0.1): must correlate to an existing call and the same attempt.
type ToolResult struct {
	ToolCallID ToolCallID     `json:"tool_call_id"`
	AttemptNo  int            `json:"attempt_no"`
	Status     ResultStatus   `json:"status"`
	Output     []ContextBlock `json:"output_blocks"`
	Err        *TypedError    `json:"error,omitempty"`
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Commit     *CommitReceipt `json:"commit,omitempty"`
}

// NewToolResult validates correlation against the originating call.
func NewToolResult(call ToolCall, status ResultStatus, output []ContextBlock, terr *TypedError, started, finished time.Time, commit *CommitReceipt) (ToolResult, error) {
	var errs []error
	if !status.Valid() {
		errs = append(errs, fmt.Errorf("status %d invalid", status))
	}
	if started.IsZero() || finished.IsZero() || finished.Before(started) {
		errs = append(errs, errors.New("started_at/finished_at must be set and ordered"))
	}
	if status == ResultFailed && terr == nil {
		errs = append(errs, errors.New("FAILED result requires a typed error"))
	}
	if commit != nil && !commit.ValidFor(call) {
		errs = append(errs, errors.New("commit receipt does not attest this call+attempt"))
	}
	if len(errs) > 0 {
		return ToolResult{}, fmt.Errorf("invalid tool result: %w", errors.Join(errs...))
	}
	return ToolResult{
		ToolCallID: call.ToolCallID, AttemptNo: call.AttemptNo, Status: status,
		Output: output, Err: terr, StartedAt: started, FinishedAt: finished, Commit: commit,
	}, nil
}

// TypedError (P0.1): the closed category/retryability pair drives handling;
// an arbitrary string never does.
type TypedError struct {
	Code         string        `json:"code"`
	Category     ErrorCategory `json:"category"`
	Retryability Retryability  `json:"retryability"`
	SafeMessage  string        `json:"safe_message"`
	RetryAfter   *time.Duration `json:"retry_after,omitempty"`
	Origin       string        `json:"origin"`
	CauseEventID *EventID      `json:"cause_event_id,omitempty"`
}

func NewTypedError(code string, cat ErrorCategory, retry Retryability, safeMsg, origin string, retryAfter *time.Duration, cause *EventID) (TypedError, error) {
	var errs []error
	if code == "" {
		errs = append(errs, errors.New("code is required"))
	}
	if !cat.Valid() {
		errs = append(errs, fmt.Errorf("category %d invalid", cat))
	}
	if !retry.Valid() {
		errs = append(errs, fmt.Errorf("retryability %d invalid", retry))
	}
	if safeMsg == "" {
		errs = append(errs, errors.New("safe_message is required"))
	}
	if origin == "" {
		errs = append(errs, errors.New("origin is required"))
	}
	if retry == RetryAfter && retryAfter == nil {
		errs = append(errs, errors.New("retryability AFTER requires retry_after"))
	}
	if cause != nil && !cause.Valid() {
		errs = append(errs, errors.New("cause_event_id present but invalid"))
	}
	if len(errs) > 0 {
		return TypedError{}, fmt.Errorf("invalid typed error: %w", errors.Join(errs...))
	}
	return TypedError{
		Code: code, Category: cat, Retryability: retry, SafeMessage: safeMsg,
		RetryAfter: retryAfter, Origin: origin, CauseEventID: cause,
	}, nil
}

func (e TypedError) Error() string {
	return fmt.Sprintf("%s [%s/%s]: %s", e.Code, e.Category, e.Retryability, e.SafeMessage)
}
