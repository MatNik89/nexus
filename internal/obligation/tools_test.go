//go:build linux

// Exact-intent seam for the obligation tools (Phase-4-r2 codex #15): a
// default-mode session's reminder_set stops at NEEDS_APPROVAL; the EXACT
// approval admits once; a tampered payload re-asks.
package obligation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/s7"
)

func TestReminderSetExactIntentApproval(t *testing.T) {
	h := build(t)
	auth := s7.NewAuthority(nil, time.Minute)
	approvals := effectpath.NewApprovals(nil, time.Minute)
	pep, err := effectpath.NewPEP(Rules(), approvals, nopAudit{}, effectpath.ModeDefault)
	if err != nil {
		t.Fatal(err)
	}
	path, err := effectpath.NewEffectPath(pep, nopMW{},
		effectpath.NewInProcessExecutor(Tools(h.m)),
		effectpath.NewSandboxedProcessExecutor(nil), auth)
	if err != nil {
		t.Fatal(err)
	}
	idem := "ik-rs"
	call, err := contracts.NewToolCall(contracts.ToolCallParams{
		ToolCallID: "tc-rs", ToolID: "reminder_set",
		Arguments:      []byte(`{"id":"rem-exact","body":"exact","year":2026,"month":9,"day":6,"hour":9,"minute":0,"tz":"Europe/Zagreb"}`),
		ArgsSchemaHash: "reminder_set.v1",
		Effect:         contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
		Deadline: time.Now().Add(time.Minute), AttemptNo: 1,
		IdempotencyKey: &idem, ProfileID: "work",
	})
	if err != nil {
		t.Fatal(err)
	}
	g1, _ := auth.Issue("op-rs-1", effectpath.ToolTarget(call))
	if _, err := path.RunTool(context.Background(), call, g1); !errors.Is(err, effectpath.ErrNeedsApproval) {
		t.Fatalf("default-mode reminder_set must stop at NEEDS_APPROVAL: %v", err)
	}
	// Tampered payload under an approval for the ORIGINAL: re-asks.
	pep.Approvals().Approve(call)
	tampered := call
	tampered.Arguments = []byte(`{"id":"rem-exact","body":"EVIL","year":2026,"month":9,"day":6,"hour":9,"minute":0,"tz":"Europe/Zagreb"}`)
	g2, _ := auth.Issue("op-rs-2", effectpath.ToolTarget(tampered))
	if _, err := path.RunTool(context.Background(), tampered, g2); !errors.Is(err, effectpath.ErrNeedsApproval) {
		t.Fatalf("tampered payload rode the approval: %v", err)
	}
	// The EXACT approved call executes.
	g3, _ := auth.Issue("op-rs-3", effectpath.ToolTarget(call))
	if _, err := path.RunTool(context.Background(), call, g3); err != nil {
		t.Fatalf("exactly-approved reminder_set refused: %v", err)
	}
	if st, err := h.m.Status(context.Background(), "rem-exact"); err != nil || st != StateScheduled {
		t.Fatalf("approved reminder not created: %v %v", st, err)
	}
}
