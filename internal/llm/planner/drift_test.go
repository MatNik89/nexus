// tgout plan detectors — drift classification (plan rounds 1-17).
package planner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/kernel/loop"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
	"github.com/MatNik89/nexus/internal/llm/provider"
)

type replyChat struct {
	auth  *s7min.Authority
	reply string
	calls int
}

func (f *replyChat) Chat(ctx context.Context, msgs []provider.ChatMessage, g s7min.Grant) (provider.ChatOutput, error) {
	if err := f.auth.Consume(g); err != nil {
		return provider.ChatOutput{}, err
	}
	f.calls++
	return provider.ChatOutput{Content: f.reply}, nil
}

func driftPlanner(t *testing.T, reply string) (*ChatPlanner, *replyChat) {
	t.Helper()
	auth := s7min.NewAuthority(nil, time.Minute)
	fc := &replyChat{auth: auth, reply: reply}
	p, err := New(fc, auth, "provider:test")
	if err != nil {
		t.Fatal(err)
	}
	specs := map[contracts.ToolID]effectpath.ToolSpec{
		"memory_recall": {Effect: contracts.EffectReadOnly, ExecutionKind: contracts.ExecInProcess, ArgsSchemaHash: "v1", Description: "recall"},
		"reminder_set":  {Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess, ArgsSchemaHash: "v1", Description: "set"},
	}
	pt, err := p.WithTools(specs, "work")
	if err != nil {
		t.Fatal(err)
	}
	return pt, fc
}

func planDrift(t *testing.T, reply string) (loop.Action, error, int) {
	t.Helper()
	p, fc := driftPlanner(t, reply)
	text := "hi"
	b, err := contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: "b1", Kind: "user_message", Content: &text,
		ContentHash: "h", SourceURI: "test://b", Producer: "t",
		Trust: contracts.TrustUser, Sensitivity: contracts.Sensitivity(1),
		Lineage: []string{}, ObservedAt: time.Unix(1, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	a, perr := p.Plan(context.Background(), []contracts.ContextBlock{b})
	return a, perr, fc.calls
}

// POSITIVE CONTROL: a valid, duplicate-free bare protocol call executes
// exactly as today (plan r14 — the strict path must stay live).
func TestValidBareCallStillExecutes(t *testing.T) {
	a, err, calls := planDrift(t, `{"action":"tool","tool_id":"memory_recall","arguments":{"q":"x"}}`)
	if err != nil || a.Call == nil {
		t.Fatalf("valid bare call did not execute: %v %+v", err, a)
	}
	if calls != 1 {
		t.Fatalf("transport calls %d, want exactly 1", calls)
	}
}

// DRIFT DIALECTS: bare and single-fenced shapes naming a known tool via
// action/tool_id/name land as the typed sentinel — one transport call,
// never the raw JSON, never execution.
func TestDriftDialectsBecomeTypedError(t *testing.T) {
	for name, reply := range map[string]string{
		"observed-dogfood": `{"action":"memory_recall","query":"prva poruka danas"}`,
		"tool_id-dialect":  `{"action":"call","tool_id":"memory_recall","arguments":{}}`,
		"name-dialect":     `{"action":"call","name":"memory_recall","arguments":{}}`,
		"fenced":           "```json\n{\"action\":\"memory_recall\",\"query\":\"x\"}\n```",
		"case-alias":       `{"action":"not-a-call","Action":"tool","tool_id":"memory_recall","arguments":{}}`,
		"duplicate":        `{"action":"memory_recall","action":"tool"}`,
		"fenced-duplicate": "```json\n{\"action\":\"memory_recall\",\"action\":\"tool\"}\n```",
	} {
		a, err, calls := planDrift(t, reply)
		var de loop.DriftError
		if err == nil || !errors.As(err, &de) {
			t.Fatalf("%s: want DriftError, got err=%v action=%+v", name, err, a)
		}
		if de.Typed.Code != "TOOL_SCHEMA_DRIFT" || de.Tool != "memory_recall" {
			t.Fatalf("%s: wrong typed outcome: %+v", name, de)
		}
		if de.Typed.Retryability != contracts.RetryNever {
			t.Fatalf("%s: drift must be RetryNever", name)
		}
		if calls != 1 {
			t.Fatalf("%s: transport calls %d, want exactly 1 (no retry)", name, calls)
		}
	}
}

// DECLARED LIMITS (plan v5): legitimate JSON with an unrelated action is
// delivered; other-key mentions stay prose; double-fenced is prose;
// mixed prose+JSON is prose.
func TestDeclaredProseLimits(t *testing.T) {
	for name, reply := range map[string]string{
		"unrelated-action": `{"action":"deploy","target":"prod"}`,
		"other-key":        `{"description":"memory_recall"}`,
		"double-fenced":    "```\n```json\n{\"action\":\"memory_recall\"}\n```\n```",
		"mixed":            "Sure! Here it is: {\"action\":\"memory_recall\"}",
	} {
		a, err, _ := planDrift(t, reply)
		if err != nil || a.Final == nil {
			t.Fatalf("%s: want prose delivery, got err=%v", name, err)
		}
	}
}

// KNOWN-TOOL COLLISION is an INTENTIONAL fail-closed limit (plan v5):
// a content answer whose action equals a registered tool id is
// suppressed into the typed final — declared, visible.
func TestDeclaredCollisionSuppression(t *testing.T) {
	_, err, _ := planDrift(t, `{"action":"memory_recall","enabled":"true"}`)
	var de loop.DriftError
	if !errors.As(err, &de) {
		t.Fatalf("declared collision did not classify as drift: %v", err)
	}
}

// PRECEDENCE: tiers action > tool_id > name; within a tier the FIRST
// source-order occurrence wins (plan v13/v15).
func TestDriftToolPrecedence(t *testing.T) {
	cases := map[string]string{
		`{"action":"x","tool_id":"reminder_set","name":"memory_recall"}`:             "reminder_set",
		`{"action":"memory_recall","tool_id":"reminder_set","name":"x"}`:             "memory_recall",
		`{"action":"memory_recall","Action":"reminder_set"}`:                         "memory_recall",
		"```json\n{\"action\":\"memory_recall\",\"Action\":\"reminder_set\"}\n```":   "memory_recall",
		`{"name":"reminder_set","action":"x","whatever":"y","name":"memory_recall"}`: "reminder_set",
	}
	for reply, want := range cases {
		_, err, _ := planDrift(t, reply)
		var de loop.DriftError
		if !errors.As(err, &de) {
			t.Fatalf("%s: not drift: %v", reply, err)
		}
		if string(de.Tool) != want {
			t.Fatalf("%s: selected %q, want %q", reply, de.Tool, want)
		}
	}
}

var _ = strings.TrimSpace
