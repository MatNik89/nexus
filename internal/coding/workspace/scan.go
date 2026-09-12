//go:build linux

// scan.go is the restart-time SCAN half of invariant 4 steps 6/7: on
// startup, finds every workspace.apply operation S7 has rehydrated to
// UNKNOWN (a durable STARTED record with no Report/Cancel ever landing —
// s7/events.go's own rehydrate, already run by the time an *s7.Authority
// reaches this code) and classifies each one's sealed bundle through
// recovery.go's ClassifyBundle.
//
// IDENTITY DISCOVERY — DELIBERATE, NOT INCIDENTAL (code-review finding,
// round 1 HIGH #2): a candidate operation's identity is taken ONLY from
// S7's OWN authoritative `s7.attempt_started` event's `op` field — the
// value S7 itself wrote when Consume durably started THAT exact attempt
// — never from this package's own `workspace.apply_started` companion
// payload. S7's Consume enforces `Companion.Key == the operation being
// consumed` (s7.go's own check), but that is a STRUCTURAL field; the
// companion's PAYLOAD is an opaque JSON blob this package authored and
// S7 never parses or cross-checks — a payload whose own `op` FIELD
// disagrees with the real operation (a bug, or a hand-built companion
// bypassing the normal ApplyGoverned path) would otherwise let the REAL
// crash-orphaned operation go completely undiscovered (its digest filed
// under the wrong key) while a decoy string gets scanned instead —
// reproduced live in review. Since `s7.attempt_started` and its
// companion are appended in ONE atomic batch (Consume's own
// appendLocked call — this journal's single-write-owner discipline
// guarantees no other writer can interleave between them), the
// companion is always the VERY NEXT event Replay delivers after its
// s7.attempt_started — used here to pair the authoritative op with the
// companion's own (comparatively low-risk, digest-self-verified by
// sealedstore.Store.Get) bundle digest.
//
// Even an authoritative op string sharing this root's own exact-intent
// prefix is not trusted blindly: s7.Authority.Target (new, mirrors
// State) is cross-checked against THIS root's own ApplyTargetID, and
// s7.Authority.MatchesPolicy against PolicyWorkspaceApply, before a
// candidate is scanned at all. A TARGET mismatch (reproduced live via a
// hand-built Begin call binding a prefix-colliding op string to a
// different resource) is excluded outright — provably a DIFFERENT
// resource, whose own scan (against its own root) legitimately picks it
// up instead. A POLICY mismatch is NOT excluded the same way: unlike
// target, policy is a TUNABLE that can legitimately differ from the
// CURRENT PolicyWorkspaceApply (a caller-driven-retry override,
// governed.go's own documented mechanism, or simply a restart/upgrade
// after the default changed) — silently excluding it would make a
// genuine crash-orphan vanish from recovery with no other scan ever
// catching it (round 4 HIGH #1, reproduced live). It surfaces as
// FOREIGN instead — this piece cannot durably tell "legitimate policy
// drift" from "genuine foreign-policy collision" apart without
// persisting a policy identity of its own, a larger redesign outside
// this increment's scope.
//
// Per-ATTEMPT tracking, not per-op blind overwrite (round 2 HIGH #1,
// reproduced live): each `s7.attempt_started` seen resets that op's
// tracked digest to "not yet confirmed" — a stale digest from an EARLIER
// attempt is never carried forward and mistaken for the current one. If
// the very next event is not a valid `workspace.apply_started` (missing,
// malformed, or some other event type — e.g. a corrupted companion, or a
// crash landing between the two halves of what should have been one
// atomic append), the operation is NOT silently dropped from the scan —
// it surfaces as FOREIGN, since an authoritative S7 record proves an
// attempt truly started, but this piece can no longer prove WHICH sealed
// bundle it belongs to.
//
// RECONCILIATION SCOPE — narrowed after round 1 HIGH #1 (reproduced
// live: PolicyWorkspaceApply's own MaxAttempts:1 means Reconcile(op,
// false, ...) for a NEVER_STARTED finding lands the operation terminal
// FAILED immediately — its one-and-only attempt is already spent — so a
// caller can never retry it again; a genuinely-safe-to-retry, nothing-
// ever-written transaction would become PERMANENTLY dead the moment this
// scan touched it). This piece therefore REPORTS every classification —
// NEVER_STARTED included — and acts on NONE of them. Advancing an
// operation past UNKNOWN (whether that means retrying a NEVER_STARTED
// one, or rolling back a MID_CRASH one through the not-yet-built
// governed `PolicyWorkspaceRollback`) is a FUTURE retry/recovery
// driver's job, once one exists — this piece's job is discovery +
// classification, told truthfully, nothing more.
//
// A closed transaction (Report(Succeeded) already landed) never appears
// in findings at all — not via a separate explicit check, but because
// ApplyGoverned's succeededBuild commits workspace.mutation_committed IN
// THE SAME ATOMIC BATCH as S7's own SUCCEEDED transition, so a closed
// operation can never simultaneously be AttemptUnknown; the two are
// structurally mutually exclusive (self-caught during my own fight-the-
// fix pass: an earlier draft tracked EvMutationCommitted separately as
// a belt-and-suspenders skip — ablating it left every test green, dead
// code, removed).
package workspace

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/MatNik89/nexus/internal/foundation/sealedstore"
	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/journal"
	"github.com/MatNik89/nexus/internal/kernel/s7"
	"golang.org/x/sys/unix"
)

// ScanFinding is one rehydrated-UNKNOWN workspace.apply operation
// RestartScan found and classified against its sealed bundle. Purely
// informational — RestartScan takes no S7 action on any finding.
type ScanFinding struct {
	Op      contracts.OperationID
	State   TransactionState
	Classes []FileClassification
	// BundleDigest is Op's own AUTHORITATIVE latest sealed-bundle digest
	// — confirmed via the SAME s7.attempt_started/workspace.apply_started
	// atomic-pairing discovery this whole scan already performs, never a
	// caller-supplied value. Empty when no confirmed digest exists for
	// this finding (the broken-companion and policy-mismatch FOREIGN
	// cases below never reach a confirmed digest at all). A caller that
	// needs to act on a MID_CRASH finding (governed_rollback.go's
	// RollbackGoverned, specifically) uses THIS field rather than
	// accepting a bundle digest as its own input — exactly so a forged
	// or stale digest can never be substituted for the real one (code-
	// review finding: governed_rollback.go's own round-1 review, HIGH
	// #1 — an earlier version trusted a caller-supplied bundle digest
	// with no cross-check against S7's own authoritative record at all).
	BundleDigest string
}

// s7AttemptStartedPayload is the minimal shape this package reads back
// from S7's OWN "s7.attempt_started" event — s7's own startedPayload is
// unexported, and this package only needs the authoritative Op field.
type s7AttemptStartedPayload struct {
	Op string `json:"op"`
}

// RestartScan finds every workspace.apply operation, bound to THIS
// rootFd's own identity (profile + device + inode — the SAME exact-intent
// components ApplyOperationID derives), that S7 currently, authoritatively
// reports UNKNOWN, and classifies its sealed bundle. rootFd, grants, and
// store must already be open/rehydrated by the caller (grants — an
// *s7.Authority built via s7.New — has already run its own rehydrate()
// by construction). Read-only: performs no S7 transition, no rollback,
// no retry (see this file's own doc comment for why).
func RestartScan(rootFd int, j *journal.Journal, grants *s7.Authority, store *sealedstore.Store) ([]ScanFinding, error) {
	if grants.BoundJournal() != j {
		// Discovery replays j; every state/target/policy cross-check below
		// trusts grants as authoritative for what it discovers there. If
		// grants was rehydrated from a DIFFERENT journal, its answers
		// describe a DIFFERENT event history entirely — a same-shaped
		// operation ID existing in BOTH is enough to misreport a CLOSED
		// transaction in j as MID_CRASH using an UNKNOWN record that
		// actually belongs to the other journal (code-review finding,
		// round 3 HIGH #1, reproduced live with two real journals sharing
		// one profile+root+plan-digest).
		return nil, fmt.Errorf("workspace: restart scan: grants is not bound to j (fail closed)")
	}
	var st unix.Stat_t
	if err := unix.Fstat(rootFd, &st); err != nil {
		return nil, fmt.Errorf("workspace: restart scan: stat root: %w", err)
	}
	profile := j.Profile()
	rootDev, rootIno := uint64(st.Dev), st.Ino
	prefix := fmt.Sprintf("workspace.apply:%s:%d:%d:", profile, rootDev, rootIno)
	wantTarget := ApplyTargetID(profile, rootDev, rootIno)

	// tracked[op] is nil while an attempt's own companion is unconfirmed
	// (either no companion has appeared yet, or the one that did appear
	// wasn't a valid workspace.apply_started for it) — non-nil only once
	// a matching companion's digest has actually been recorded for the
	// MOST RECENT attempt_started seen for that op.
	tracked := make(map[contracts.OperationID]*string)
	var pendingOp contracts.OperationID
	err := j.Replay(0, func(ev journal.Event) error {
		switch ev.Envelope.EventType {
		case s7.EvAttemptStarted:
			var p s7AttemptStartedPayload
			if err := json.Unmarshal(ev.Envelope.Payload, &p); err != nil {
				pendingOp = ""
				return nil
			}
			pendingOp = contracts.OperationID(p.Op)
			if strings.HasPrefix(string(pendingOp), prefix) {
				tracked[pendingOp] = nil // a NEW attempt starts: any earlier attempt's digest is now stale
			}
		case EvApplyStarted:
			op := pendingOp
			pendingOp = ""
			if op == "" {
				return nil // an orphaned/unpaired companion — ignored, never trusted as this op's digest
			}
			var p applyStartedPayload
			if err := json.Unmarshal(ev.Envelope.Payload, &p); err == nil {
				if _, tracking := tracked[op]; tracking {
					digest := p.BundleDigest
					tracked[op] = &digest
				}
			}
		default:
			pendingOp = "" // anything else interleaving means the prior attempt_started had no companion (non-durable policy)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("workspace: restart scan: replay journal: %w", err)
	}

	ops := make([]contracts.OperationID, 0, len(tracked))
	for op := range tracked {
		ops = append(ops, op)
	}
	sort.Slice(ops, func(i, j int) bool { return ops[i] < ops[j] }) // deterministic order; map iteration is not

	var findings []ScanFinding
	for _, op := range ops {
		state, ok := grants.State(op)
		if !ok || state != contracts.AttemptUnknown {
			continue // not a crash-orphan: never reached UNKNOWN, or already resolved in-process before this restart
		}
		if target, ok := grants.Target(op); !ok || target != wantTarget {
			continue // an op string that merely COLLIDES with this root's prefix, bound to a different resource — not mine
		}
		if matches, ok := grants.MatchesPolicy(op, PolicyWorkspaceApply); ok && !matches {
			// Bound to a DIFFERENT policy than this package's CURRENT
			// PolicyWorkspaceApply. Unlike a target mismatch (provably a
			// DIFFERENT resource — some OTHER root's own scan legitimately
			// picks it up), a policy mismatch has no other rightful owner:
			// it could be a genuine foreign-policy collision (round 2 HIGH
			// #2's own repro), OR a LEGITIMATE operation Begin'd under an
			// intentionally overridden policy (governed.go's own
			// documented caller-driven-retry override) that no longer
			// matches after a restart/upgrade restored the default —
			// codex round 4, HIGH #1, reproduced live: silently excluding
			// it made a genuine crash-orphan vanish from recovery
			// consideration entirely, with no other scan ever catching
			// it. This piece cannot durably tell the two apart without
			// persisting a policy identity of its own (a larger redesign,
			// not this increment's job) — so, matching the SAME "report,
			// never silently omit" discipline the broken-companion case
			// below established, it surfaces as FOREIGN either way rather
			// than disappearing.
			findings = append(findings, ScanFinding{Op: op, State: TransactionForeign})
			continue
		} else if !ok {
			continue // op vanished from S7's live map between the State() and MatchesPolicy() checks above — nothing to report
		}
		digest := tracked[op]
		if digest == nil {
			// S7 authoritatively proves an attempt started, but no valid
			// companion for THAT attempt was ever found — this piece
			// cannot prove which sealed bundle (if any) belongs to it.
			findings = append(findings, ScanFinding{Op: op, State: TransactionForeign})
			continue
		}
		findings = append(findings, classify(op, rootFd, store, *digest))
	}
	return findings, nil
}

func classify(op contracts.OperationID, rootFd int, store *sealedstore.Store, digest string) ScanFinding {
	raw, err := store.Get(digest)
	if err != nil {
		// A durable STARTED record naming a sealed bundle the store can no
		// longer produce (missing or corrupt) is itself an anomaly, not a
		// decodable state — invariant 4's own table: treat as FOREIGN.
		return ScanFinding{Op: op, State: TransactionForeign}
	}
	b, err := DecodeBundle(raw)
	if err != nil {
		return ScanFinding{Op: op, State: TransactionForeign}
	}
	classes, state, err := ClassifyBundle(rootFd, b)
	if err != nil {
		// A real filesystem error walking the write set (not ErrNotExist,
		// which classifyOneFile already resolves to BEFORE/FOREIGN itself)
		// is exactly the "never a guess" case — fail-safe as FOREIGN rather
		// than letting a transient I/O error masquerade as a clean state.
		return ScanFinding{Op: op, State: TransactionForeign}
	}
	return ScanFinding{Op: op, State: state, Classes: classes, BundleDigest: digest}
}
