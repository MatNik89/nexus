//go:build linux

package obligation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/effectpath"
	"github.com/MatNik89/nexus/internal/schedule"
)

// Specs are the SEALED tool declarations for the obligation tools.
func Specs() map[contracts.ToolID]effectpath.ToolSpec {
	return map[contracts.ToolID]effectpath.ToolSpec{
		"reminder_set": {Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
			ArgsSchemaHash: "reminder_set.v1",
			Description:    `set a reminder; args {"id":"...","body":"...","year":2026,"month":9,"day":5,"hour":12,"minute":30,"tz":"Europe/Zagreb"}`},
		"reminder_ack": {Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
			ArgsSchemaHash: "reminder_ack.v1",
			Description:    `acknowledge a delivered reminder; args {"occurrence_id":"occ-<id>#1"}`},
		"task_create": {Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
			ArgsSchemaHash: "task_create.v1",
			Description:    `create a typed task; args {"id":"...","kind":"file_note","params":"{\"note\":\"...\"}"}`},
		"task_file_note": {Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
			ArgsSchemaHash: "task_file_note.v1",
			Description:    `execute a file_note task; args {"task_id":"..."}`},
		"task_done": {Effect: contracts.EffectReversible, ExecutionKind: contracts.ExecInProcess,
			ArgsSchemaHash: "task_done.v1",
			Description:    `close a task after its postcondition verifies; args {"task_id":"..."}`},
	}
}

// Rules: reminder_set is ASK (a durable state change under exact-intent
// approval); reminder_ack is ALLOW (the user's own closing gesture).
func Rules() map[contracts.ToolID]effectpath.Decision {
	return map[contracts.ToolID]effectpath.Decision{
		"reminder_set":   effectpath.DecisionAsk,
		"reminder_ack":   effectpath.DecisionAllow,
		"task_create":    effectpath.DecisionAsk,
		"task_file_note": effectpath.DecisionAllow, // creation was the consent; execution is governed by grant+spec
		"task_done":      effectpath.DecisionAllow,
	}
}

// Tools exposes the manager as in-process tools; every handler refuses a
// call whose ProfileID differs from the journal's bound profile.
func Tools(m *Manager) map[contracts.ToolID]effectpath.InProcFunc {
	profileGuard := func(c contracts.ToolCall) error {
		if c.ProfileID != m.j.Profile() {
			return fmt.Errorf("obligation: call profile %q does not match the store profile (fail closed)", c.ProfileID)
		}
		return nil
	}
	return map[contracts.ToolID]effectpath.InProcFunc{
		"reminder_set": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			if err := profileGuard(c); err != nil {
				return contracts.ToolResult{}, err
			}
			var args struct {
				ID     string `json:"id"`
				Body   string `json:"body"`
				Year   int    `json:"year"`
				Month  int    `json:"month"`
				Day    int    `json:"day"`
				Hour   int    `json:"hour"`
				Minute int    `json:"minute"`
				TZ     string `json:"tz"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || args.ID == "" || args.Body == "" {
				return contracts.ToolResult{}, fmt.Errorf("reminder_set: id and body are required (fail closed)")
			}
			w := schedule.WallTime{Year: args.Year, Month: time.Month(args.Month), Day: args.Day,
				Hour: args.Hour, Minute: args.Minute, TZ: args.TZ}
			if err := m.CreateReminder(ctx, args.ID, args.Body, w); err != nil {
				return contracts.ToolResult{}, err
			}
			return oblResult(c, fmt.Sprintf("reminder %s set for %04d-%02d-%02d %02d:%02d %s",
				args.ID, args.Year, args.Month, args.Day, args.Hour, args.Minute, args.TZ), true)
		},
		"reminder_ack": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			if err := profileGuard(c); err != nil {
				return contracts.ToolResult{}, err
			}
			var args struct {
				OccurrenceID string `json:"occurrence_id"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || args.OccurrenceID == "" {
				return contracts.ToolResult{}, fmt.Errorf("reminder_ack: an occurrence_id is required (fail closed)")
			}
			// The user gesture IS this authenticated tool call (same-UID
			// UDS session): its call id identifies the ack source.
			if err := m.MarkAcked(ctx, args.OccurrenceID, AckGesture{Source: "tool:" + string(c.ToolCallID)}); err != nil {
				return contracts.ToolResult{}, err
			}
			return oblResult(c, "acknowledged "+args.OccurrenceID, true)
		},
		"task_create": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			if err := profileGuard(c); err != nil {
				return contracts.ToolResult{}, err
			}
			var args struct {
				ID     string `json:"id"`
				Kind   string `json:"kind"`
				Params string `json:"params"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || args.ID == "" || args.Kind == "" {
				return contracts.ToolResult{}, fmt.Errorf("task_create: id and kind are required (fail closed)")
			}
			if err := m.CreateTask(ctx, args.ID, args.Kind, args.Params); err != nil {
				return contracts.ToolResult{}, err
			}
			return oblResult(c, "task "+args.ID+" created ("+args.Kind+")", true)
		},
		// task_file_note is THE governed executable — whether the model or
		// RunTask dispatches it, the SAME sequence holds: intent journaled
		// BEFORE the effect, reconcile-before-retry, attestation after
		// (Phase-4 codex #3/#5 — no path skips the discipline).
		"task_file_note": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			if err := profileGuard(c); err != nil {
				return contracts.ToolResult{}, err
			}
			var args struct {
				TaskID string `json:"task_id"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || args.TaskID == "" {
				return contracts.ToolResult{}, fmt.Errorf("task_file_note: a task_id is required (fail closed)")
			}
			path, line, err := m.executeGoverned(ctx, args.TaskID, string(c.ToolCallID))
			if err != nil {
				return contracts.ToolResult{}, err
			}
			attest, _ := json.Marshal(map[string]string{"path": path, "line": line})
			return oblResult(c, string(attest), true)
		},
		"task_done": func(ctx context.Context, c contracts.ToolCall) (contracts.ToolResult, error) {
			if err := profileGuard(c); err != nil {
				return contracts.ToolResult{}, err
			}
			var args struct {
				TaskID string `json:"task_id"`
			}
			if err := json.Unmarshal(c.Arguments, &args); err != nil || args.TaskID == "" {
				return contracts.ToolResult{}, fmt.Errorf("task_done: a task_id is required (fail closed)")
			}
			if err := m.MarkTaskDone(ctx, args.TaskID); err != nil {
				return contracts.ToolResult{}, err
			}
			return oblResult(c, "task "+args.TaskID+" verified done", true)
		},
	}
}

func oblResult(c contracts.ToolCall, text string, committed bool) (contracts.ToolResult, error) {
	sum := sha256.Sum256([]byte(text))
	content := text
	block, err := contracts.NewContextBlock(contracts.ContextBlockParams{
		BlockID: contracts.BlockID("obs-" + string(c.ToolCallID)), Kind: "tool_output",
		Content: &content, ContentHash: hex.EncodeToString(sum[:]),
		SourceURI: "nexus://obligation", Producer: "obligation",
		Trust: contracts.TrustToolTrusted, Sensitivity: contracts.SensitivityInternal,
		Lineage: []string{string(c.ToolCallID)}, ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		return contracts.ToolResult{}, err
	}
	var receipt *contracts.CommitReceipt
	if committed {
		receipt = &contracts.CommitReceipt{Phase: contracts.PhaseAfterCommit,
			ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo, ContentHash: hex.EncodeToString(sum[:])}
	}
	now := time.Now().UTC()
	return contracts.ToolResult{ToolCallID: c.ToolCallID, AttemptNo: c.AttemptNo,
		Status: contracts.ResultSucceeded, Output: []contracts.ContextBlock{block},
		StartedAt: now, FinishedAt: now.Add(time.Millisecond), Commit: receipt}, nil
}
