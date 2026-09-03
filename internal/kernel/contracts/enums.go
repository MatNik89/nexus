package contracts

import "fmt"

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
type ExecutionKind uint8

const (
	ExecInvalid ExecutionKind = iota
	ExecInProcess
	ExecProcess
)

var execKindNames = map[ExecutionKind]string{
	ExecInProcess: "IN_PROCESS", ExecProcess: "PROCESS",
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
)

var runStateNames = map[RunState]string{
	RunCreated: "CREATED", RunAdmitted: "ADMITTED", RunRunning: "RUNNING",
	RunSucceeded: "SUCCEEDED", RunFailed: "FAILED", RunCancelled: "CANCELLED", RunUnknown: "UNKNOWN",
}

type TurnState uint8

const (
	TurnInvalid TurnState = iota
	TurnCreated
	TurnRunning
	TurnSucceeded
	TurnFailed
	TurnCancelled
)

var turnStateNames = map[TurnState]string{
	TurnCreated: "CREATED", TurnRunning: "RUNNING",
	TurnSucceeded: "SUCCEEDED", TurnFailed: "FAILED", TurnCancelled: "CANCELLED",
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
)

var attemptStateNames = map[AttemptState]string{
	AttemptPlanned: "PLANNED", AttemptAuthorized: "AUTHORIZED", AttemptRunning: "RUNNING",
	AttemptSucceeded: "SUCCEEDED", AttemptFailed: "FAILED", AttemptCancelled: "CANCELLED",
	AttemptUnknown: "UNKNOWN",
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
	names map[T]string
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
	resultSpec      = newEnumSpec(resultStatusNames)
)

func (v Role) Valid() bool          { return roleSpec.valid(v) }
func (v Role) String() string       { return roleSpec.str(v, "ROLE") }
func ParseRole(s string) (Role, error) { return roleSpec.parse(s, "role") }

func (v EffectClass) Valid() bool    { return effectSpec.valid(v) }
func (v EffectClass) String() string { return effectSpec.str(v, "EFFECT_CLASS") }
func ParseEffectClass(s string) (EffectClass, error) { return effectSpec.parse(s, "effect_class") }

func (v TrustClass) Valid() bool    { return trustSpec.valid(v) }
func (v TrustClass) String() string { return trustSpec.str(v, "TRUST_CLASS") }
func ParseTrustClass(s string) (TrustClass, error) { return trustSpec.parse(s, "trust_class") }

func (v Sensitivity) Valid() bool    { return sensitivitySpec.valid(v) }
func (v Sensitivity) String() string { return sensitivitySpec.str(v, "SENSITIVITY") }
func ParseSensitivity(s string) (Sensitivity, error) { return sensitivitySpec.parse(s, "sensitivity") }

func (v ErrorCategory) Valid() bool    { return errCatSpec.valid(v) }
func (v ErrorCategory) String() string { return errCatSpec.str(v, "ERROR_CATEGORY") }
func ParseErrorCategory(s string) (ErrorCategory, error) { return errCatSpec.parse(s, "error_category") }

func (v Retryability) Valid() bool    { return retrySpec.valid(v) }
func (v Retryability) String() string { return retrySpec.str(v, "RETRYABILITY") }
func ParseRetryability(s string) (Retryability, error) { return retrySpec.parse(s, "retryability") }

func (v ExecutionKind) Valid() bool    { return execKindSpec.valid(v) }
func (v ExecutionKind) String() string { return execKindSpec.str(v, "EXECUTION_KIND") }
func ParseExecutionKind(s string) (ExecutionKind, error) { return execKindSpec.parse(s, "execution_kind") }

func (v EffectPhase) Valid() bool    { return phaseSpec.valid(v) }
func (v EffectPhase) String() string { return phaseSpec.str(v, "EFFECT_PHASE") }
func ParseEffectPhase(s string) (EffectPhase, error) { return phaseSpec.parse(s, "effect_phase") }

func (v Decision) Valid() bool    { return decisionSpec.valid(v) }
func (v Decision) String() string { return decisionSpec.str(v, "DECISION") }
func ParseDecision(s string) (Decision, error) { return decisionSpec.parse(s, "decision") }

func (v PolicyMode) Valid() bool    { return policyModeSpec.valid(v) }
func (v PolicyMode) String() string { return policyModeSpec.str(v, "POLICY_MODE") }
func ParsePolicyMode(s string) (PolicyMode, error) { return policyModeSpec.parse(s, "policy_mode") }

func (v RunState) Valid() bool    { return runStateSpec.valid(v) }
func (v RunState) String() string { return runStateSpec.str(v, "RUN_STATE") }
func ParseRunState(s string) (RunState, error) { return runStateSpec.parse(s, "run_state") }

func (v TurnState) Valid() bool    { return turnStateSpec.valid(v) }
func (v TurnState) String() string { return turnStateSpec.str(v, "TURN_STATE") }
func ParseTurnState(s string) (TurnState, error) { return turnStateSpec.parse(s, "turn_state") }

func (v AttemptState) Valid() bool    { return attemptSpec.valid(v) }
func (v AttemptState) String() string { return attemptSpec.str(v, "ATTEMPT_STATE") }
func ParseAttemptState(s string) (AttemptState, error) { return attemptSpec.parse(s, "attempt_state") }

func (v ResultStatus) Valid() bool    { return resultSpec.valid(v) }
func (v ResultStatus) String() string { return resultSpec.str(v, "RESULT_STATUS") }
func ParseResultStatus(s string) (ResultStatus, error) { return resultSpec.parse(s, "result_status") }
