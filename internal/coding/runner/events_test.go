//go:build linux

package runner

import (
	"encoding/json"
	"strings"
	"testing"
)

func codingRunValidator(t *testing.T) func(json.RawMessage) error {
	t.Helper()
	v, ok := Events()["coding.run"]
	if !ok {
		t.Fatal(`Events() does not register "coding.run"`)
	}
	return v
}

const validDigest = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// Detector: the exact shape journalRunEvent emits (Run's own "args" +
// "exit_ok" payload) is accepted.
func TestCodingRunEventsAcceptsRunShape(t *testing.T) {
	v := codingRunValidator(t)
	payload := []byte(`{"args":["test","./..."],"exit_ok":true,"snapshot_digest":"` + validDigest + `","toolchain_digest":"` + validDigest + `","policy_hash":"ph1"}`)
	if err := v(payload); err != nil {
		t.Fatalf("real run-shape payload rejected: %v", err)
	}
}

// Detector: the exact shape journalGoplsRenameEvent emits (rename's own
// "file"/"line"/"character"/"new_name"/"has_result"/"has_lsp_error"
// payload) is accepted.
func TestCodingRunEventsAcceptsRenameShape(t *testing.T) {
	v := codingRunValidator(t)
	payload := []byte(`{"file":"main.go","line":2,"character":5,"new_name":"Bar","has_result":true,"has_lsp_error":false,"snapshot_digest":"` + validDigest + `","toolchain_digest":"` + validDigest + `","policy_hash":"ph1"}`)
	if err := v(payload); err != nil {
		t.Fatalf("real rename-shape payload rejected: %v", err)
	}
}

// Detector: a payload asserting BOTH shapes at once (a forged/corrupted
// event) is refused, not silently accepted as "whichever fields are set".
func TestCodingRunEventsRejectsMixedShape(t *testing.T) {
	v := codingRunValidator(t)
	payload := []byte(`{"args":["x"],"exit_ok":true,"file":"main.go","line":1,"character":1,"new_name":"Bar","has_result":true,"has_lsp_error":false,"snapshot_digest":"` + validDigest + `","toolchain_digest":"` + validDigest + `","policy_hash":"ph1"}`)
	err := v(payload)
	if err == nil || !strings.Contains(err.Error(), "exactly one of the run/rename payload shapes") {
		t.Fatalf("expected a mixed-shape rejection, got: %v", err)
	}
}

// Detector: a payload matching NEITHER shape (no args/exit_ok and no
// file/line/character/new_name) is refused fail-closed.
func TestCodingRunEventsRejectsNeitherShape(t *testing.T) {
	v := codingRunValidator(t)
	payload := []byte(`{"snapshot_digest":"` + validDigest + `","toolchain_digest":"` + validDigest + `","policy_hash":"ph1"}`)
	err := v(payload)
	if err == nil || !strings.Contains(err.Error(), "exactly one of the run/rename payload shapes") {
		t.Fatalf("expected a neither-shape rejection, got: %v", err)
	}
}

// Detector: an unknown field is refused (strict decode, no silent drop —
// closes the same "self-certification" gap workspace.Events() guards
// against).
func TestCodingRunEventsRejectsUnknownField(t *testing.T) {
	v := codingRunValidator(t)
	payload := []byte(`{"args":["x"],"exit_ok":true,"snapshot_digest":"` + validDigest + `","toolchain_digest":"` + validDigest + `","policy_hash":"ph1","injected":"nope"}`)
	if err := v(payload); err == nil {
		t.Fatal("expected an unknown-field rejection")
	}
}

// Detector: a non-hex/wrong-length digest is refused — a malformed
// digest here would silently defeat verifyStagedTypeChecks' own
// "wantDigest must be the caller's own already-authenticated digest"
// discipline for anyone later auditing the journal.
func TestCodingRunEventsRejectsMalformedDigest(t *testing.T) {
	v := codingRunValidator(t)
	payload := []byte(`{"args":["x"],"exit_ok":true,"snapshot_digest":"not-hex","toolchain_digest":"` + validDigest + `","policy_hash":"ph1"}`)
	err := v(payload)
	if err == nil || !strings.Contains(err.Error(), "snapshot_digest") {
		t.Fatalf("expected a snapshot_digest rejection, got: %v", err)
	}
}
