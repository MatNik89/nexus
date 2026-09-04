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
	}
}

// Rules: reminder_set is ASK (a durable state change under exact-intent
// approval); reminder_ack is ALLOW (the user's own closing gesture).
func Rules() map[contracts.ToolID]effectpath.Decision {
	return map[contracts.ToolID]effectpath.Decision{
		"reminder_set": effectpath.DecisionAsk,
		"reminder_ack": effectpath.DecisionAllow,
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
			if err := m.MarkAcked(ctx, args.OccurrenceID); err != nil {
				return contracts.ToolResult{}, err
			}
			return oblResult(c, "acknowledged "+args.OccurrenceID, true)
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
