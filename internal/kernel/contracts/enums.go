package contracts

import (
	"encoding/json"
	"fmt"
)

// Every enum follows the same fail-closed pattern (E3): the zero value is
// Invalid, Valid() accepts only known members, and JSON decoding goes
// through closed string tables — an unknown discriminator is REJECTED,
// never defaulted (Annex P0.1: "unknown discriminator se odbija").

// Role of a message author.
type Role uint8

const (
	RoleInvalid Role = iota
	RoleSystem
	RoleUser
	RoleAssistant
	RoleTool
)

var roleNames = map[Role]string{
	RoleSystem: "SYSTEM", RoleUser: "USER", RoleAssistant: "ASSISTANT", RoleTool: "TOOL",
}

// EffectClass of a tool call (Annex P0.1).
type EffectClass uint8

const (
	EffectInvalid EffectClass = iota
	EffectReadOnly
	EffectReversible
	EffectIrreversible
)

var effectNames = map[EffectClass]string{
	EffectReadOnly: "READ_ONLY", EffectReversible: "REVERSIBLE", EffectIrreversible: "IRREVERSIBLE",
}

// TrustClass of a context block (P0.1; monotonicity is enforced by
// consumers: transformations may only TIGHTEN trust).
type TrustClass uint8

const (
	TrustInvalid TrustClass = iota
	TrustSystem
	TrustUser
	TrustToolTrusted
	TrustUntrustedExternal
)

var trustNames = map[TrustClass]string{
	TrustSystem: "SYSTEM", TrustUser: "USER",
	TrustToolTrusted: "TOOL_TRUSTED", TrustUntrustedExternal: "UNTRUSTED_EXTERNAL",
}

// Sensitivity of a context block (P0.1).
type Sensitivity uint8

const (
	SensitivityInvalid Sensitivity = iota
	SensitivityPublic
	SensitivityInternal
	SensitivityConfidential
	SensitivitySecret
)

var sensitivityNames = map[Sensitivity]string{
	SensitivityPublic: "PUBLIC", SensitivityInternal: "INTERNAL",
	SensitivityConfidential: "CONFIDENTIAL", SensitivitySecret: "SECRET",
}

// ErrorCategory (P0.1 TypedError).
type ErrorCategory uint8

const (
	ErrCatInvalid ErrorCategory = iota
	ErrCatValidation
	ErrCatAuthn
	ErrCatAuthz
	ErrCatPolicy
	ErrCatResource
	ErrCatTimeout
	ErrCatCancelled
	ErrCatDependency
	ErrCatConflict
	ErrCatInternal
	ErrCatUnknownEffect
)

var errCatNames = map[ErrorCategory]string{
	ErrCatValidation: "VALIDATION", ErrCatAuthn: "AUTHN", ErrCatAuthz: "AUTHZ",
	ErrCatPolicy: "POLICY", ErrCatResource: "RESOURCE", ErrCatTimeout: "TIMEOUT",
	ErrCatCancelled: "CANCELLED", ErrCatDependency: "DEPENDENCY", ErrCatConflict: "CONFLICT",
	ErrCatInternal: "INTERNAL", ErrCatUnknownEffect: "UNKNOWN_EFFECT",
}

// Retryability (P0.1 TypedError): an arbitrary string must never drive
// retry — only this closed enum, and only S7 acts on it.
type Retryability uint8

const (
	RetryInvalid Retryability = iota
	RetryNever
	RetryS7Policy
	RetryAfter
)

var retryNames = map[Retryability]string{
	RetryNever: "NEVER", RetryS7Policy: "S7_POLICY", RetryAfter: "AFTER",
}

// ExecutionKind seals how a tool executes (DESIGN-FIXES-r2): unknown kind
// is rejected by the effect path, NEVER run in-process.
//
// ExecInProcess is an ABSOLUTE guarantee: a tool declaring this kind never
// touches the OS process boundary — no exec, no subprocess, ever. That
// guarantee is what lets `EffectPath` treat every ExecInProcess tool as
// safe-by-construction with respect to the sandbox (nothing to sandbox).
//
// ExecInProcessGoverned (2026-09-12, owner-approved architectural
// decision; full rationale and known gaps: docs/ARCHITECTURE-ESSENTIALS.md
// E8, docs/SECTION-MAP.md's own owner-invariant amendment) is for the
// DIFFERENT case: a Go-native tool whose own implementation is itself
// responsible for routing its own subprocess launches through the real
// S6.2 sandbox (e.g. `rename_symbol`, via `internal/coding/runner`), at
// a finer grain than EffectPath's own single-process
// SandboxedProcessExecutor model assumes. EffectPath provides NO
// sandboxing of its own for this kind — it dispatches through the SAME
// InProcessExecutor as ExecInProcess (identical mechanics). The
// obligation to sandbox is the HANDLER's, not EffectPath's; this kind
// exists so that obligation is truthfully named rather than silently
// implied by mislabeling the tool ExecInProcess. Deliberately a
// SEPARATE kind rather than an exception carved into ExecInProcess:
// that keeps ExecInProcess's own guarantee absolute and auditable
// (e.g. "grep for exec.Command reachable from any ExecInProcess
// handler" needs no per-handler exception list).
type ExecutionKind uint8

const (
	ExecInvalid ExecutionKind = iota
	ExecInProcess
	ExecProcess
	ExecInProcessGoverned
)

var execKindNames = map[ExecutionKind]string{
	ExecInProcess: "IN_PROCESS", ExecProcess: "PROCESS", ExecInProcessGoverned: "IN_PROCESS_GOVERNED",
}

// EffectPhase classifies when an effect committed (DESIGN-FIXES-r2 /
// Essentials E9): anything effectful without a valid receipt is UNKNOWN and
// reconciles — never a blind retry.
type EffectPhase uint8

const (
	PhaseInvalid EffectPhase = iota
	PhaseBeforeCommit
	PhaseAfterCommit
	PhaseUnknown
)

var phaseNames = map[EffectPhase]string{
	PhaseBeforeCommit: "BEFORE_COMMIT", PhaseAfterCommit: "AFTER_COMMIT", PhaseUnknown: "UNKNOWN",
}

// Decision is the S6.0 PEP outcome (DESIGN-FIXES-r2): zero value denies.
type Decision uint8

const (
	DecisionInvalid Decision = iota
	DecisionAllow
	DecisionAsk
	DecisionDeny
)

var decisionNames = map[Decision]string{
	DecisionAllow: "ALLOW", DecisionAsk: "ASK", DecisionDeny: "DENY",
}

// PolicyMode selects confirmation behavior (HARDQ F2): Yolo maps ASK to
// ALLOW and never touches DENY or any safety net.
type PolicyMode uint8

const (
	PolicyModeInvalid PolicyMode = iota
	PolicyModeDefault
	PolicyModeYolo
)

var policyModeNames = map[PolicyMode]string{
	PolicyModeDefault: "DEFAULT", PolicyModeYolo: "YOLO",
}

// RunState / TurnState / AttemptState per P0.1 state lists. States are
// reconstructed by folding journal events (S0.2); these enums only name them.
type RunState uint8

const (
	RunInvalid RunState = iota
	RunCreated
	RunAdmitted
	RunRunning
	RunSucceeded
	RunFailed
	RunCancelled
	RunUnknown
	RunManualRecovery
)

var runStateNames = map[RunState]string{
	RunCreated: "CREATED", RunAdmitted: "ADMITTED", RunRunning: "RUNNING",
	RunSucceeded: "SUCCEEDED", RunFailed: "FAILED", RunCancelled: "CANCELLED", RunUnknown: "UNKNOWN",
	RunManualRecovery: "MANUAL_RECOVERY",
}

type TurnState uint8

const (
	TurnInvalid TurnState = iota
	TurnCreated
	TurnRunning
	TurnSucceeded
	TurnFailed
	TurnCancelled
	// TurnSuspendedState: the turn committed a durable HITL suspension
	// (B6) and waits for the owner's decision — appended LAST to keep
	// existing serialized state bytes stable (Phase-5-r2 codex #4).
	TurnSuspendedState
)

var turnStateNames = map[TurnState]string{
	TurnCreated: "CREATED", TurnRunning: "RUNNING",
	TurnSucceeded: "SUCCEEDED", TurnFailed: "FAILED", TurnCancelled: "CANCELLED",
	TurnSuspendedState: "SUSPENDED",
}

type AttemptState uint8

const (
	AttemptInvalid AttemptState = iota
	AttemptPlanned
	AttemptAuthorized
	AttemptRunning
	AttemptSucceeded
	AttemptFailed
	AttemptCancelled
	AttemptUnknown
	AttemptManualRecovery
	// AttemptFailedRetryable (SPEC P0.2 FAILED_RETRYABLE) is the full-S7
	// state from which ONLY S7 may issue the next AttemptGrant (Slice B1).
	// Appended so existing numeric values stay stable.
	AttemptFailedRetryable
)

// Name mapping to SPEC P0.2: PLANNED=PENDING, AUTHORIZED=GRANTED, RUNNING,
// FAILED_RETRYABLE, FAILED=FAILED_TERMINAL, CANCELLED, UNKNOWN.
var attemptStateNames = map[AttemptState]string{
	AttemptPlanned: "PLANNED", AttemptAuthorized: "AUTHORIZED", AttemptRunning: "RUNNING",
	AttemptSucceeded: "SUCCEEDED", AttemptFailed: "FAILED", AttemptCancelled: "CANCELLED",
	AttemptUnknown: "UNKNOWN", AttemptManualRecovery: "MANUAL_RECOVERY",
	AttemptFailedRetryable: "FAILED_RETRYABLE",
}

// ActorType is the closed set of event emitters (Phase-1A r2 codex #7:
// an open string here let unknown discriminators reach the journal).
type ActorType uint8

const (
	ActorInvalid ActorType = iota
	ActorUser
	ActorSystem
	ActorScheduler
	ActorChannel
	ActorTool
)

var actorTypeNames = map[ActorType]string{
	ActorUser: "USER", ActorSystem: "SYSTEM", ActorScheduler: "SCHEDULER",
	ActorChannel: "CHANNEL", ActorTool: "TOOL",
}

// ResultStatus of a tool result (P0.1).
type ResultStatus uint8

const (
	ResultInvalid ResultStatus = iota
	ResultSucceeded
	ResultFailed
	ResultCancelled
	ResultUnknown
)

var resultStatusNames = map[ResultStatus]string{
	ResultSucceeded: "SUCCEEDED", ResultFailed: "FAILED",
	ResultCancelled: "CANCELLED", ResultUnknown: "UNKNOWN",
}

// ---- generated-by-hand shared plumbing (one generic implementation) ----

type enumSpec[T comparable] struct {
	names  map[T]string
	byName map[string]T
}

func newEnumSpec[T comparable](names map[T]string) enumSpec[T] {
	byName := make(map[string]T, len(names))
	for v, n := range names {
		byName[n] = v
	}
	return enumSpec[T]{names: names, byName: byName}
}

func (s enumSpec[T]) valid(v T) bool { _, ok := s.names[v]; return ok }

func (s enumSpec[T]) str(v T, kind string) string {
	if n, ok := s.names[v]; ok {
		return n
	}
	return fmt.Sprintf("INVALID_%s", kind)
}

func (s enumSpec[T]) parse(name, kind string) (T, error) {
	if v, ok := s.byName[name]; ok {
		return v, nil
	}
	var zero T
	return zero, fmt.Errorf("unknown %s discriminator %q (fail closed)", kind, name)
}

// marshalJSON emits the canonical string; an invalid member cannot be
// serialized (fail closed on the way OUT too).
func (s enumSpec[T]) marshalJSON(v T, kind string) ([]byte, error) {
	n, ok := s.names[v]
	if !ok {
		return nil, fmt.Errorf("cannot marshal invalid %s value", kind)
	}
	return json.Marshal(n)
}

// unmarshalJSON accepts ONLY a known canonical string — numbers and unknown
// strings are rejected (closed wire discriminators, Annex P0.1).
func (s enumSpec[T]) unmarshalJSON(b []byte, kind string) (T, error) {
	var zero T
	var name string
	if err := json.Unmarshal(b, &name); err != nil {
		return zero, fmt.Errorf("%s discriminator must be a canonical string, got %s", kind, string(b))
	}
	return s.parse(name, kind)
}

var (
	roleSpec        = newEnumSpec(roleNames)
	effectSpec      = newEnumSpec(effectNames)
	trustSpec       = newEnumSpec(trustNames)
	sensitivitySpec = newEnumSpec(sensitivityNames)
	errCatSpec      = newEnumSpec(errCatNames)
	retrySpec       = newEnumSpec(retryNames)
	execKindSpec    = newEnumSpec(execKindNames)
	phaseSpec       = newEnumSpec(phaseNames)
	decisionSpec    = newEnumSpec(decisionNames)
	policyModeSpec  = newEnumSpec(policyModeNames)
	runStateSpec    = newEnumSpec(runStateNames)
	turnStateSpec   = newEnumSpec(turnStateNames)
	attemptSpec     = newEnumSpec(attemptStateNames)
	actorTypeSpec   = newEnumSpec(actorTypeNames)
	resultSpec      = newEnumSpec(resultStatusNames)
)

func (v Role) Valid() bool             { return roleSpec.valid(v) }
func (v Role) String() string          { return roleSpec.str(v, "ROLE") }
func ParseRole(s string) (Role, error) { return roleSpec.parse(s, "role") }

func (v EffectClass) Valid() bool                    { return effectSpec.valid(v) }
func (v EffectClass) String() string                 { return effectSpec.str(v, "EFFECT_CLASS") }
func ParseEffectClass(s string) (EffectClass, error) { return effectSpec.parse(s, "effect_class") }

func (v TrustClass) Valid() bool                   { return trustSpec.valid(v) }
func (v TrustClass) String() string                { return trustSpec.str(v, "TRUST_CLASS") }
func ParseTrustClass(s string) (TrustClass, error) { return trustSpec.parse(s, "trust_class") }

func (v Sensitivity) Valid() bool                    { return sensitivitySpec.valid(v) }
func (v Sensitivity) String() string                 { return sensitivitySpec.str(v, "SENSITIVITY") }
func ParseSensitivity(s string) (Sensitivity, error) { return sensitivitySpec.parse(s, "sensitivity") }

func (v ErrorCategory) Valid() bool    { return errCatSpec.valid(v) }
func (v ErrorCategory) String() string { return errCatSpec.str(v, "ERROR_CATEGORY") }
func ParseErrorCategory(s string) (ErrorCategory, error) {
	return errCatSpec.parse(s, "error_category")
}

func (v Retryability) Valid() bool                     { return retrySpec.valid(v) }
func (v Retryability) String() string                  { return retrySpec.str(v, "RETRYABILITY") }
func ParseRetryability(s string) (Retryability, error) { return retrySpec.parse(s, "retryability") }

func (v ExecutionKind) Valid() bool    { return execKindSpec.valid(v) }
func (v ExecutionKind) String() string { return execKindSpec.str(v, "EXECUTION_KIND") }
func ParseExecutionKind(s string) (ExecutionKind, error) {
	return execKindSpec.parse(s, "execution_kind")
}

func (v EffectPhase) Valid() bool                    { return phaseSpec.valid(v) }
func (v EffectPhase) String() string                 { return phaseSpec.str(v, "EFFECT_PHASE") }
func ParseEffectPhase(s string) (EffectPhase, error) { return phaseSpec.parse(s, "effect_phase") }

func (v Decision) Valid() bool                 { return decisionSpec.valid(v) }
func (v Decision) String() string              { return decisionSpec.str(v, "DECISION") }
func ParseDecision(s string) (Decision, error) { return decisionSpec.parse(s, "decision") }

func (v PolicyMode) Valid() bool                   { return policyModeSpec.valid(v) }
func (v PolicyMode) String() string                { return policyModeSpec.str(v, "POLICY_MODE") }
func ParsePolicyMode(s string) (PolicyMode, error) { return policyModeSpec.parse(s, "policy_mode") }

func (v RunState) Valid() bool                 { return runStateSpec.valid(v) }
func (v RunState) String() string              { return runStateSpec.str(v, "RUN_STATE") }
func ParseRunState(s string) (RunState, error) { return runStateSpec.parse(s, "run_state") }

func (v TurnState) Valid() bool                  { return turnStateSpec.valid(v) }
func (v TurnState) String() string               { return turnStateSpec.str(v, "TURN_STATE") }
func ParseTurnState(s string) (TurnState, error) { return turnStateSpec.parse(s, "turn_state") }

func (v AttemptState) Valid() bool                     { return attemptSpec.valid(v) }
func (v AttemptState) String() string                  { return attemptSpec.str(v, "ATTEMPT_STATE") }
func ParseAttemptState(s string) (AttemptState, error) { return attemptSpec.parse(s, "attempt_state") }

func (v ResultStatus) Valid() bool                     { return resultSpec.valid(v) }
func (v ResultStatus) String() string                  { return resultSpec.str(v, "RESULT_STATUS") }
func ParseResultStatus(s string) (ResultStatus, error) { return resultSpec.parse(s, "result_status") }

func (v Role) MarshalJSON() ([]byte, error) { return roleSpec.marshalJSON(v, "role") }
func (v *Role) UnmarshalJSON(b []byte) error {
	x, err := roleSpec.unmarshalJSON(b, "role")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v EffectClass) MarshalJSON() ([]byte, error) { return effectSpec.marshalJSON(v, "effect_class") }
func (v *EffectClass) UnmarshalJSON(b []byte) error {
	x, err := effectSpec.unmarshalJSON(b, "effect_class")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v TrustClass) MarshalJSON() ([]byte, error) { return trustSpec.marshalJSON(v, "trust_class") }
func (v *TrustClass) UnmarshalJSON(b []byte) error {
	x, err := trustSpec.unmarshalJSON(b, "trust_class")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v Sensitivity) MarshalJSON() ([]byte, error) {
	return sensitivitySpec.marshalJSON(v, "sensitivity")
}
func (v *Sensitivity) UnmarshalJSON(b []byte) error {
	x, err := sensitivitySpec.unmarshalJSON(b, "sensitivity")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v ErrorCategory) MarshalJSON() ([]byte, error) {
	return errCatSpec.marshalJSON(v, "error_category")
}
func (v *ErrorCategory) UnmarshalJSON(b []byte) error {
	x, err := errCatSpec.unmarshalJSON(b, "error_category")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v Retryability) MarshalJSON() ([]byte, error) { return retrySpec.marshalJSON(v, "retryability") }
func (v *Retryability) UnmarshalJSON(b []byte) error {
	x, err := retrySpec.unmarshalJSON(b, "retryability")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v ExecutionKind) MarshalJSON() ([]byte, error) {
	return execKindSpec.marshalJSON(v, "execution_kind")
}
func (v *ExecutionKind) UnmarshalJSON(b []byte) error {
	x, err := execKindSpec.unmarshalJSON(b, "execution_kind")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v EffectPhase) MarshalJSON() ([]byte, error) { return phaseSpec.marshalJSON(v, "effect_phase") }
func (v *EffectPhase) UnmarshalJSON(b []byte) error {
	x, err := phaseSpec.unmarshalJSON(b, "effect_phase")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v Decision) MarshalJSON() ([]byte, error) { return decisionSpec.marshalJSON(v, "decision") }
func (v *Decision) UnmarshalJSON(b []byte) error {
	x, err := decisionSpec.unmarshalJSON(b, "decision")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v PolicyMode) MarshalJSON() ([]byte, error) {
	return policyModeSpec.marshalJSON(v, "policy_mode")
}
func (v *PolicyMode) UnmarshalJSON(b []byte) error {
	x, err := policyModeSpec.unmarshalJSON(b, "policy_mode")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v RunState) MarshalJSON() ([]byte, error) { return runStateSpec.marshalJSON(v, "run_state") }
func (v *RunState) UnmarshalJSON(b []byte) error {
	x, err := runStateSpec.unmarshalJSON(b, "run_state")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v TurnState) MarshalJSON() ([]byte, error) { return turnStateSpec.marshalJSON(v, "turn_state") }
func (v *TurnState) UnmarshalJSON(b []byte) error {
	x, err := turnStateSpec.unmarshalJSON(b, "turn_state")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v AttemptState) MarshalJSON() ([]byte, error) {
	return attemptSpec.marshalJSON(v, "attempt_state")
}
func (v *AttemptState) UnmarshalJSON(b []byte) error {
	x, err := attemptSpec.unmarshalJSON(b, "attempt_state")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v ResultStatus) MarshalJSON() ([]byte, error) {
	return resultSpec.marshalJSON(v, "result_status")
}
func (v *ResultStatus) UnmarshalJSON(b []byte) error {
	x, err := resultSpec.unmarshalJSON(b, "result_status")
	if err != nil {
		return err
	}
	*v = x
	return nil
}

func (v ActorType) Valid() bool                  { return actorTypeSpec.valid(v) }
func (v ActorType) String() string               { return actorTypeSpec.str(v, "ACTOR_TYPE") }
func ParseActorType(s string) (ActorType, error) { return actorTypeSpec.parse(s, "actor_type") }
func (v ActorType) MarshalJSON() ([]byte, error) { return actorTypeSpec.marshalJSON(v, "actor_type") }
func (v *ActorType) UnmarshalJSON(b []byte) error {
	x, err := actorTypeSpec.unmarshalJSON(b, "actor_type")
	if err != nil {
		return err
	}
	*v = x
	return nil
}
