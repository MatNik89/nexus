//go:build linux

package runner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"

	"github.com/MatNik89/nexus/internal/kernel/journal"
)

// codingRunSHA256Hex matches the digest format journalRunEvent/
// journalGoplsRenameEvent always populate (DigestTree/toolchain pin
// digests, PolicyHash) — same closed shape as sealedstore's own digestRE
// and workspace's own validSHA256Hex, each package owning its narrow copy
// rather than sharing one cross-package regex for a two-line check.
var codingRunSHA256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// codingRunPayload is the UNION of journalRunEvent's (Args/ExitOK) and
// journalGoplsRenameEvent's (File/Line/Character/NewName/HasResult/
// HasLSPError) own payload shapes — both emit the SAME "coding.run"
// EventType (a rename session IS a coding-run, just LSP-driven instead of
// one-shot-subprocess-driven; see journalGoplsRenameEvent's own doc
// comment), so the one registered validator must accept whichever shape a
// given emitter actually wrote. Strict decoding (DisallowUnknownFields)
// still refuses anything outside this closed union.
type codingRunPayload struct {
	Args            []string `json:"args,omitempty"`
	ExitOK          *bool    `json:"exit_ok,omitempty"`
	File            string   `json:"file,omitempty"`
	Line            *int     `json:"line,omitempty"`
	Character       *int     `json:"character,omitempty"`
	NewName         string   `json:"new_name,omitempty"`
	HasResult       *bool    `json:"has_result,omitempty"`
	HasLSPError     *bool    `json:"has_lsp_error,omitempty"`
	SnapshotDigest  string   `json:"snapshot_digest"`
	ToolchainDigest string   `json:"toolchain_digest"`
	PolicyHash      string   `json:"policy_hash"`
	Error           string   `json:"error,omitempty"`
}

// Events registers this package's OWN closed event vocabulary — just
// "coding.run" today (journalRunEvent's doc comment: "this package's own
// closed event vocabulary, never routed through EffectPath's tool-call
// event shape") — for a production journal.Open call. Every caller that
// drives runner.Run or runner.RunGoplsRename against a real journal MUST
// fold this in (mirrors memory.Events()/schedule.Events()'s own
// registration convention) or every such call fails closed with "unknown
// event type" the first time it actually tries to journal an outcome.
func Events() map[string]journal.PayloadValidator {
	return map[string]journal.PayloadValidator{
		"coding.run": func(raw json.RawMessage) error {
			var p codingRunPayload
			if err := strictDecodeCodingRunEvent(raw, &p); err != nil {
				return fmt.Errorf("runner: coding.run: %w", err)
			}
			if !codingRunSHA256Hex.MatchString(p.SnapshotDigest) {
				return fmt.Errorf("runner: coding.run requires a valid sha256-hex snapshot_digest")
			}
			if !codingRunSHA256Hex.MatchString(p.ToolchainDigest) {
				return fmt.Errorf("runner: coding.run requires a valid sha256-hex toolchain_digest")
			}
			if p.PolicyHash == "" {
				return fmt.Errorf("runner: coding.run requires policy_hash")
			}
			isRunShape := p.Args != nil || p.ExitOK != nil
			isRenameShape := p.File != "" || p.NewName != "" || p.Line != nil || p.Character != nil
			if isRunShape == isRenameShape {
				// Neither shape (isRunShape==isRenameShape==false) or a
				// forged event asserting BOTH at once — reject either
				// way (fail closed).
				return fmt.Errorf("runner: coding.run must match exactly one of the run/rename payload shapes")
			}
			if isRunShape && p.ExitOK == nil {
				return fmt.Errorf("runner: coding.run (run shape) requires exit_ok")
			}
			if isRenameShape && (p.HasResult == nil || p.HasLSPError == nil) {
				return fmt.Errorf("runner: coding.run (rename shape) requires has_result and has_lsp_error")
			}
			return nil
		},
	}
}

// strictDecodeCodingRunEvent decodes exactly ONE JSON value with no
// unknown fields and no trailing data (mirrors workspace's own
// strictDecodeEvent / s7's own private strictDecode).
func strictDecodeCodingRunEvent(raw json.RawMessage, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return fmt.Errorf("trailing data after the JSON value")
	}
	return nil
}
