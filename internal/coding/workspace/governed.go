//go:build linux

// governed.go is the S7/journal wiring for Apply (PLAN-CODING-TRIO.md
// invariant 2 and invariant 4 step 2): pairs Apply's bindDurable hook
// with a real S7 Consume companion batch — the sealed bundle's digest is
// journaled, atomically paired with S7's own durable STARTED event,
// BEFORE Apply writes a single file — reports Apply's outcome through
// S7's durable Report/Cancel, committing a terminal
// workspace.mutation_committed event in the SAME batch as S7's own
// SUCCEEDED transition on success.
//
// SCOPE OF THIS PIECE (revised TWICE after codex review — round 1 found
// v1 under-scoped invariant 2's own pre-existing contract; round 2 found
// v2's retry design fundamentally broken and its cancellation wiring
// inert; see PLAN-CODING-TRIO.md's own status sections for the full
// history):
//   - Operation/target identity is EXACT-INTENT: derived from the
//     JOURNAL's OWN bound profile (j.Profile() — never a caller-supplied
//     profile string, which could disagree with it), the transaction
//     root's own device+inode, and the prepared plan's digest (see
//     ApplyOperationID). AGENTS.md's own exact-intent rule: "any
//     payload/target change invalidates it."
//   - ApplyGoverned makes exactly ONE attempt per call — it does NOT own
//     a retry loop (round-2 finding: an internal loop using wall-clock
//     time.Until against an Authority whose clock is caller-injectable
//     and possibly frozen is a genuine correctness bug, and s7.Execute —
//     the ONE canonical retry-driver this codebase already has —
//     explicitly refuses Durable policies; no durable counterpart
//     exists). This matches the ONLY existing precedent for a durable
//     effect in this codebase, channel/telegram's registerCommands:
//     each call does its own Begin/Next/Consume/Report and returns
//     immediately, including on ErrNotDue — an EXTERNAL caller (a
//     polling tick there; whatever drives RenameSymbol retries here)
//     owns deciding whether and when to call again.
//   - A failure whose rollback Apply's own error chain proves was fully
//     verified (transaction.ErrRollbackIncomplete absent) is reported
//     FailedRetryable with s7.CodeMutationRolledBack; the SAME operation
//     may then be retried by a caller that calls ApplyGoverned again —
//     but ONLY after rebuilding its mutations via
//     workspace.UpdateMutationsAfterRollback(mutations, res.RolledBack)
//     first (round-2 finding HIGH #1: restoring a file via
//     WriteFileBeneath's atomic exchange always mints a NEW inode, so
//     reusing the stale original ExpectedBeforeDev/Ino makes every retry
//     fail its own precondition check, unconditionally — this was a
//     genuine, embarrassing bug in the first retry design, caught only
//     because codex insisted the original test prove a retry after a
//     REAL rollback rather than a synthetic one). A failure whose
//     rollback was INCOMPLETE (ErrRollbackIncomplete present) is
//     reported Unknown instead — never retried blind.
//   - Apply itself is now ctx-aware: it checks ctx.Err() once per
//     mutation, immediately before that file's write — a concurrent
//     Cancel (via the AttemptContext ApplyGoverned derives right after
//     Consume) or an expired deadline stops it from writing FURTHER
//     files rather than silently continuing after S7 has already moved
//     to a terminal state (round-2 finding: the previously-derived
//     AttemptContext was discarded entirely, making the 30s
//     AttemptTimeout unobservable). This is a per-file boundary check,
//     not mid-syscall interruption — local descriptor I/O has no
//     cancellable wait point once a single WriteFileBeneath call starts.
//   - `RunDurableTool` does not exist anywhere in this codebase (grep
//     confirms it — the plan itself names it only as agy's suggested
//     "starting shape... for Slice 3's own review to refine", not a
//     built API; codex round 2 accepted this deferral). S6.0's
//     `PEP.Decide` lives in internal/kernel/effectpath and is called
//     ONLY from `EffectPath.RunTool` — the model-tool-call dispatch
//     pipeline. Neither runner.Run nor telegram's ControlEffect (this
//     codebase's two other already-converged durable effects with the
//     SAME shape) calls it either, for the identical reason: neither is
//     dispatched BY that loop. ApplyGoverned is the same shape — never
//     itself the point a model tool call is authorized; no
//     `rename_symbol` ToolID is wired into the loop anywhere yet. Codex
//     round 2's own caveat stands and is worth repeating here: exact-
//     intent IDs plus S7 do not themselves "re-enforce S6" — the FUTURE
//     model-facing caller must still pass through the real PEP and
//     middleware before ever invoking this primitive.
//   - Deliberately still NOT built here: the restart-time crash-recovery
//     classification table and the governed PolicyWorkspaceRollback
//     operation (invariant 4 steps 6/7); `contracts.EnvelopeParams`'s
//     `SealedPayloadRef` wiring (codex round 2 accepted this deferral —
//     no production GC/recovery consumer exists yet; PLAN-CODING-TRIO.md
//     already recorded this exact gap during Slice 1 and deferred it to
//     whichever future slice DOES need it, which is precisely steps 6/7
//     above); a durable, S7-owned retry-driving primitive analogous to
//     s7.Execute but for Durable policies (this piece's own single-
//     attempt shape sidesteps needing one, matching the telegram
//     precedent, but a real background retry SCHEDULER for RenameSymbol
//     specifically is still unbuilt — a future caller today would have
//     to drive retries by hand).
package workspace

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"golang.org/x/sys/unix"
)

// PolicyWorkspaceApply governs one multi-file Apply transaction:
// irreversible (a filesystem mutation), durable (a crash-recovery table
// this piece does not yet build depends on a durable S7 record surviving
// a restart). MaxAttempts is 1 — this piece deliberately does NOT claim
// retry support (code-review finding, codex round 3 HIGH #3: a real
// retry needs its verified post-rollback preconditions to survive a
// restart, non-forgeably, which nothing durably records yet; codex's own
// offered alternative — narrow this to a single attempt rather than ship
// an unusable/non-durable retry claim — is what this does). A caller
// that DOES want to retry after a verified-clean failure (see
// ApplyGoverned's own doc comment) may override this package var with a
// higher MaxAttempts and its own s7.CodeMutationRolledBack-retryable
// policy; ApplyGoverned's single-attempt-per-call shape and
// workspace.UpdateMutationsAfterRollback already support that caller
// driving its own retries by hand.
var PolicyWorkspaceApply = s7.Policy{
	EffectClass:    contracts.EffectIrreversible,
	MaxAttempts:    1,
	AttemptTimeout: 30 * time.Second,
	Deadline:       2 * time.Minute,
	Durable:        true,
}

// The workspace.* durable event vocabulary.
const (
	// EvApplyStarted is Consume's companion: paired atomically with S7's
	// own durable STARTED event, before any file is written. Names the
	// sealed bundle by reference+digest — never duplicates its bytes
	// into the journal.
	EvApplyStarted = "workspace.apply_started"
	// EvMutationCommitted is the explicit "every file was written"
	// marker a future crash-recovery classification depends on —
	// committed atomically with S7's own SUCCEEDED/terminal transition,
	// in Report's companion batch.
	EvMutationCommitted = "workspace.mutation_committed"
	// EvApplyFailed records a durable operation's non-success landing
	// (retryable, terminal, unknown, or a cancellation before any
	// write). No recovery classification consults it — only
	// EvMutationCommitted's presence/absence does — it exists for audit
	// continuity with every other durable operation in this codebase.
	EvApplyFailed = "workspace.apply_failed"
)

// sha256HexPattern is the shape a durable BundleDigest/artifact
// reference must have — a lowercase hex-encoded SHA-256 — so a payload
// naming garbage (or a caller's copy/paste of the wrong field) is
// rejected at admission, not silently accepted as if it named a real
// sealed artifact (code-review finding, codex round 2 HIGH #5).
func validSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

type applyStartedPayload struct {
	Op           string `json:"op"`
	BundleDigest string `json:"bundle_digest"`
}
type mutationCommittedPayload struct {
	Op           string `json:"op"`
	BundleDigest string `json:"bundle_digest"`
}
type applyFailedPayload struct {
	Op        string `json:"op"`
	AttemptNo int    `json:"attempt_no"`
	Landing   int    `json:"landing"`
	Code      string `json:"code,omitempty"`
}

// validApplyFailedNarrative is the EXACT (attempt_no, landing, code)
// compatibility table for the workspace.apply_failed event — not just a
// closed landing enum (code-review finding, codex round 4 MEDIUM #3: a
// closed enum alone still admits impossible combinations no legitimate
// caller in THIS package ever produces, e.g. attempt_no=0 with Unknown,
// or Retry paired with an unrelated known S7 code). Mirrors s7.Events's
// own validReport, scoped to what THIS owner actually emits:
//   - any non-empty code other than s7.CodeMutationRolledBack is
//     rejected outright — this owner never uses any other code.
//   - Cancelled: any attempt_no (0 = never consumed); code "" (never
//     consumed) or CodeMutationRolledBack when attempt_no>=1 — S7's own
//     Cancel preserves the PRIOR report's code as rec.lastCode when
//     building its companion, so cancelling an operation that already
//     landed FAILED_RETRYABLE/CodeMutationRolledBack once (a legitimate
//     step in a caller-driven retry) carries that code forward (code-
//     review finding, codex round 5 HIGH #1 — a live reproduction, not
//     hypothetical: rejecting this outright made the durable Cancel
//     append itself fail, stranding the operation AUTHORIZED forever).
//   - Terminal: any attempt_no, code "" or CodeMutationRolledBack (Next's
//     own exhaustion-before-any-grant path can terminalize at
//     attempt_no=0 via a passed deadline, carrying whatever code was
//     last reported, possibly "").
//   - Retry: requires a consumed attempt (attempt_no>=1) AND
//     CodeMutationRolledBack — the only landing/code pair Report itself
//     ever proposes for a verified-clean rollback.
//   - Unknown: requires a consumed attempt (attempt_no>=1) and code "".
//   - Succeeded: never valid — this is exclusively the FAILURE companion.
func validApplyFailedNarrative(attemptNo, landing int, code string) bool {
	if code != "" && code != s7.CodeMutationRolledBack {
		return false
	}
	// A non-empty code requires a prior CONSUMED attempt to have produced
	// it — code-review finding, codex round 5 HIGH #1: S7's own Cancel
	// preserves the PRIOR report's code as rec.lastCode when building its
	// companion, so cancelling an operation that already landed
	// FAILED_RETRYABLE/CodeMutationRolledBack once (a legitimate step in
	// the caller-overridden retry path this package explicitly supports)
	// produces EXACTLY (attemptNo>=1, Cancelled, CodeMutationRolledBack) —
	// requiring an ALWAYS-empty code for Cancelled (this file's round-4
	// table) rejected that real, legitimate S7-generated narrative and
	// stranded the operation AUTHORIZED (the durable Cancel append itself
	// failing validation). Terminal carries the identical possibility for
	// the identical reason (Next's own exhaustion path echoes the same
	// rec.lastCode).
	if code != "" && attemptNo < 1 {
		return false
	}
	switch s7.LandingKind(landing) {
	case s7.LandingCancelled, s7.LandingTerminal:
		return true
	case s7.LandingRetry:
		return attemptNo >= 1 && code == s7.CodeMutationRolledBack
	case s7.LandingUnknown:
		return attemptNo >= 1 && code == ""
	default:
		return false
	}
}

// Events registers the closed workspace.* event set. Each validator
// strictly decodes (no unknown fields, no trailing data) and checks the
// closed shapes above — an admitted event that later turns out
// malformed is exactly the "self-certification" gap invariant 5 warns
// against.
func Events() map[string]journal.PayloadValidator {
	return map[string]journal.PayloadValidator{
		EvApplyStarted: func(raw json.RawMessage) error {
			var p applyStartedPayload
			if err := strictDecodeEvent(raw, &p); err != nil {
				return err
			}
			if p.Op == "" {
				return fmt.Errorf("workspace: apply_started requires op")
			}
			if !validSHA256Hex(p.BundleDigest) {
				return fmt.Errorf("workspace: apply_started requires a valid sha256-hex bundle_digest")
			}
			return nil
		},
		EvMutationCommitted: func(raw json.RawMessage) error {
			var p mutationCommittedPayload
			if err := strictDecodeEvent(raw, &p); err != nil {
				return err
			}
			if p.Op == "" {
				return fmt.Errorf("workspace: mutation_committed requires op")
			}
			if !validSHA256Hex(p.BundleDigest) {
				return fmt.Errorf("workspace: mutation_committed requires a valid sha256-hex bundle_digest")
			}
			return nil
		},
		EvApplyFailed: func(raw json.RawMessage) error {
			var p applyFailedPayload
			if err := strictDecodeEvent(raw, &p); err != nil {
				return err
			}
			if p.Op == "" || p.AttemptNo < 0 {
				return fmt.Errorf("workspace: apply_failed requires op and attempt_no>=0 (0 means no attempt was ever consumed)")
			}
			if !validApplyFailedNarrative(p.AttemptNo, p.Landing, p.Code) {
				return fmt.Errorf("workspace: apply_failed names an impossible narrative (attempt_no=%d, landing=%d, code=%q)", p.AttemptNo, p.Landing, p.Code)
			}
			return nil
		},
	}
}

// strictDecodeEvent decodes exactly ONE JSON value with no unknown
// fields and no trailing data (mirrors s7's own private strictDecode).
func strictDecodeEvent(raw json.RawMessage, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if _, err := dec.Token(); err == nil {
		return fmt.Errorf("workspace: trailing data after the JSON value")
	}
	return nil
}

// ApplyOperationID derives the EXACT-INTENT S7 operation identity for one
// Apply transaction: profile + the transaction root's own descriptor
// identity (device+inode — the same identity descriptor.go's own
// discipline already binds every capture to) + the prepared plan's
// digest. Two calls with an identical profile/root/plan always name the
// SAME operation (idempotent — Begin is itself a no-op for a matching
// re-begin); any difference — a different profile, a different root, or
// any change to the plan (PlanDigest binds every edit and preimage, see
// symedit.computePlanDigest) — names a DIFFERENT operation, matching
// AGENTS.md's exact-intent rule ("any payload/target change invalidates
// it").
func ApplyOperationID(profile contracts.ProfileID, rootDev, rootIno uint64, planDigest string) contracts.OperationID {
	return contracts.OperationID(fmt.Sprintf("workspace.apply:%s:%d:%d:%s", profile, rootDev, rootIno, planDigest))
}

// ApplyTargetID derives the resource identity the grant is bound to —
// the transaction root itself, independent of any one plan.
func ApplyTargetID(profile contracts.ProfileID, rootDev, rootIno uint64) contracts.TargetID {
	return contracts.TargetID(fmt.Sprintf("workspace:%s:%d:%d", profile, rootDev, rootIno))
}

// ErrRetryNotDue reports that this operation's retry budget is not
// exhausted, but its backoff has not elapsed yet — the caller may call
// ApplyGoverned again once due (grants.NextAt names when).
var ErrRetryNotDue = errors.New("workspace: apply governed: retry not due yet")

// applyFn is a test seam: apply in production. Tests substitute it to
// deterministically control ApplyGoverned's own branching (a specific
// verified-clean-rollback or an incomplete-rollback outcome) without
// needing a precisely-timed real filesystem race.
var applyFn = apply

// deriveAttemptContext is a test seam: grants.AttemptContext in
// production. Tests substitute it to deterministically force the rare
// "Consume succeeded but the attempt's own context could not be
// derived" scenario (e.g. the operation's deadline expired in the gap
// between Next issuing the grant and Consume finishing) without needing
// a precisely-timed real clock race.
var deriveAttemptContext = func(grants *s7.Authority, ctx context.Context, op contracts.OperationID) (context.Context, context.CancelFunc, error) {
	return grants.AttemptContext(ctx, op, time.Time{})
}

// ApplyGoverned makes ONE governed attempt at Apply: it derives its
// EXACT-INTENT operation/target identity from the journal's own bound
// profile, rootFd's device+inode, and planDigest (see ApplyOperationID),
// begins (or resumes) a PolicyWorkspaceApply operation, obtains a grant,
// and supplies Apply a bindDurable hook that Consumes it with an
// EvApplyStarted companion naming the sealed bundle — so the durable
// STARTED record commits before Apply writes a single file.
//
// On success it commits EvMutationCommitted in the same batch as S7's
// own SUCCEEDED transition and returns. On failure it branches four
// ways, in priority order: bindDurable never completed (Apply refused
// before ever sealing/consuming, e.g. a stale precondition) — the grant
// was never consumed, so the operation is Cancelled and ApplyGoverned
// returns; bindDurable DID complete and Apply's returned error chain
// carries transaction.ErrRollbackIncomplete (transaction.Apply's own
// re-verification found ANY mutation — written-then-rolled-back or
// never reached at all — no longer matching its captured before-state)
// — the workspace may be in a mixed or foreign state, so the operation
// is Reported Unknown, never retried blind, regardless of cause;
// consumed and the rollback was fully verified but the underlying cause
// was ctx cancellation/deadline (errors.Is context.Canceled/
// DeadlineExceeded) — the operation is Cancelled, matching S7's own
// semantics for an attempt that stopped because it was told to, not
// because it failed on its own terms; otherwise (consumed, fully
// verified, NOT a cancellation) — the operation is Reported terminal
// FAILED with s7.CodeMutationRolledBack documenting why. PolicyWorkspaceApply's
// MaxAttempts is 1 by default (see its own doc comment for why this
// piece does not claim retry support) — a caller that overrides it with
// a higher budget and s7.CodeMutationRolledBack as a retryable code may
// call ApplyGoverned again for the SAME operation once due (ErrNotDue,
// wrapped as ErrRetryNotDue, if called too soon), but MUST rebuild its
// mutations via UpdateMutationsAfterRollback(mutations, res.RolledBack)
// first, or the retry's own precondition check will refuse the file
// rollback just restored (its identity changed).
func ApplyGoverned(ctx context.Context, rootFd int, store *sealedstore.Store, mutations []FileMutation, grants *s7.Authority, j *journal.Journal, planDigest string, runID contracts.RunID) (Result, error) {
	if planDigest == "" || !runID.Valid() {
		return Result{}, fmt.Errorf("workspace: apply governed: plan digest/run id are both required (fail closed)")
	}
	profile := j.Profile()
	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		return Result{}, fmt.Errorf("workspace: apply governed: stat root: %w", err)
	}
	rootDev, rootIno := uint64(st.Dev), st.Ino
	op := ApplyOperationID(profile, rootDev, rootIno, planDigest)
	target := ApplyTargetID(profile, rootDev, rootIno)

	if err := grants.Begin(op, target, PolicyWorkspaceApply); err != nil {
		return Result{}, fmt.Errorf("workspace: apply governed: %w", err)
	}

	// attemptNo starts at the count of attempts ALREADY consumed before
	// this call (queried once, here, outside any lock S7 might be holding
	// when it later invokes a builder) and is updated to the real
	// grant.AttemptNo only once Consume actually succeeds, below — never
	// re-queried live from inside failedBuild itself: every builder S7
	// passes to Next/Consume/Report/Cancel is invoked WHILE S7 already
	// holds its own internal lock, so calling grants.Attempts(op) (which
	// also locks) from inside one is a same-goroutine relock — a genuine
	// deadlock this exact shape hit immediately in testing, not a
	// hypothetical (code-review finding, codex round 3 MEDIUM #5, fixed
	// without reintroducing that bug).
	attemptNo := grants.Attempts(op)
	failedBuild := func(l s7.Landing) s7.Companion {
		return s7.Companion{Key: op, Params: envelope(j, op, runID, attemptNo, EvApplyFailed,
			applyFailedPayload{Op: string(op), AttemptNo: attemptNo, Landing: int(l.Kind), Code: l.Code})}
	}

	grant, err := grants.Next(op, failedBuild)
	if errors.Is(err, s7.ErrNotDue) {
		return Result{}, fmt.Errorf("workspace: apply governed: %w", ErrRetryNotDue)
	}
	if err != nil {
		return Result{}, fmt.Errorf("workspace: apply governed: %w", err)
	}

	consumed := false
	// neverWrote is set true ONLY when Consume succeeded (a durable
	// STARTED record committed) but AttemptContext itself then failed —
	// e.g. the operation's own deadline expired in the gap between Next
	// issuing the grant and Consume finishing (code-review finding,
	// codex round 4 MEDIUM #4): Apply's write loop is never even
	// entered in this case, so treating it as "verified rollback, safe
	// to retry" (CodeMutationRolledBack) would be a false narrative —
	// nothing was ever mutated, let alone rolled back. This is the SAME
	// "the attempt never got to run because its own timing bound was
	// already gone" shape as a ctx cancellation/deadline, so it routes
	// to the SAME Cancel branch below, not a code-carrying failure.
	neverWrote := false
	var attemptCancel context.CancelFunc
	attemptCtx := ctx
	bindDurable := func(digest string) error {
		companion := s7.Companion{Key: op, Params: envelope(j, op, runID, grant.AttemptNo, EvApplyStarted,
			applyStartedPayload{Op: string(op), BundleDigest: digest})}
		if err := grants.Consume(grant, companion); err != nil {
			return err
		}
		consumed = true
		attemptNo = grant.AttemptNo
		actx, cancel, aerr := deriveAttemptContext(grants, ctx, op)
		if aerr != nil {
			neverWrote = true
			return aerr
		}
		attemptCtx, attemptCancel = actx, cancel
		return nil
	}

	res, applyErr := applyFn(attemptCtx, rootFd, store, mutations, bindDurable)
	if attemptCancel != nil {
		attemptCancel()
	}

	if applyErr == nil {
		succeededBuild := func(s7.Landing) s7.Companion {
			return s7.Companion{Key: op, Params: envelope(j, op, runID, grant.AttemptNo, EvMutationCommitted,
				mutationCommittedPayload{Op: string(op), BundleDigest: res.BundleDigest})}
		}
		if err := grants.Report(op, s7.OutcomeSucceeded, "", succeededBuild); err != nil {
			return res, fmt.Errorf("workspace: apply governed: report succeeded: %w", err)
		}
		return res, nil
	}

	if !consumed {
		if cerr := grants.Cancel(op, failedBuild); cerr != nil {
			return res, fmt.Errorf("workspace: apply governed: %w (cancel also failed: %v)", applyErr, cerr)
		}
		return res, fmt.Errorf("workspace: apply governed: %w", applyErr)
	}

	if errors.Is(applyErr, ErrRollbackIncomplete) {
		if rerr := grants.Report(op, s7.OutcomeUnknown, "", failedBuild); rerr != nil {
			return res, fmt.Errorf("workspace: apply governed: %w (report unknown also failed: %v)", applyErr, rerr)
		}
		return res, fmt.Errorf("workspace: apply governed: %w", applyErr)
	}

	if neverWrote || errors.Is(applyErr, context.Canceled) || errors.Is(applyErr, context.DeadlineExceeded) {
		// A fully verified rollback caused BY cancellation/deadline is
		// CANCELLED, not a code-carrying failure (code-review finding,
		// codex round 3 HIGH #4) — the attempt stopped because it was
		// told to, matching runner.Run's own selfDeadlineCancelled
		// classification for the identical distinction. neverWrote (an
		// AttemptContext failure between Consume and the write loop,
		// round 4 MEDIUM #4) gets the SAME treatment: nothing was ever
		// mutated, so it is not a "rollback" of anything.
		if cerr := grants.Cancel(op, failedBuild); cerr != nil {
			return res, fmt.Errorf("workspace: apply governed: %w (cancel also failed: %v)", applyErr, cerr)
		}
		return res, fmt.Errorf("workspace: apply governed: %w", applyErr)
	}

	// Verified-clean rollback, not a cancellation: PROPOSE retryable with
	// s7.CodeMutationRolledBack — Report itself, not this call site,
	// decides FailedRetryable vs terminal FAILED from the policy's OWN
	// RetryableCodes/budget. Under PolicyWorkspaceApply's default
	// MaxAttempts=1 this always resolves to terminal FAILED (the budget
	// is already spent); a caller that overrides the policy with a real
	// retry budget and CodeMutationRolledBack as retryable gets a
	// meaningful FAILED_RETRYABLE landing from this SAME call, unchanged.
	if rerr := grants.Report(op, s7.OutcomeFailedRetryable, s7.CodeMutationRolledBack, failedBuild); rerr != nil {
		return res, fmt.Errorf("workspace: apply governed: %w (report also failed: %v)", applyErr, rerr)
	}
	return res, fmt.Errorf("workspace: apply governed: %w", applyErr)
}

func envelope(j *journal.Journal, op contracts.OperationID, runID contracts.RunID, attemptNo int, eventType string, payload any) contracts.EnvelopeParams {
	raw, _ := json.Marshal(payload)
	// The journal's own generic envelope admission requires AttemptNo>=1
	// for every event (contracts.EnvelopeParams.AttemptNo is a generic
	// "which physical attempt produced this event" field, 1-indexed
	// everywhere else in this codebase — s7's own events always use a
	// literal 1). attemptNo can legitimately be 0 here — it means "no S7
	// attempt was EVER consumed for this operation" (an operation
	// Cancelled before Consume) — that DOMAIN fact still needs
	// representing, so it stays truthful in applyFailedPayload's own
	// JSON attempt_no field; only the generic envelope field is clamped.
	envAttemptNo := attemptNo
	if envAttemptNo < 1 {
		envAttemptNo = 1
	}
	return contracts.EnvelopeParams{
		SchemaID: "nexus.event", SchemaVersion: 1,
		EventID:     contracts.EventID("ev-" + string(op) + "-" + eventType + "-" + randHex(4)),
		EventType:   eventType,
		RunID:       runID,
		EmittedAt:   time.Now().UTC(),
		ActorType:   contracts.ActorSystem,
		ActorID:     "workspace",
		PrincipalID: "nexus",
		WorkspaceID: "local",
		ProfileID:   j.Profile(),
		AttemptNo:   envAttemptNo,
		Payload:     raw,
		PayloadHash: "recomputed",
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "fallback"
	}
	return hex.EncodeToString(b)
}
