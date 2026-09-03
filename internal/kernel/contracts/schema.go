package contracts

import (
	"encoding/json"
	"fmt"
)

// Schema admission (P0 scope of Annex P0.1 migration, HARDQ C7): P0 knows a
// closed set of (schema_id, version) pairs and REJECTS anything else
// UNPROCESSED, with the raw input preserved for the caller (the v2
// migration layer adds the upcast chain + quarantine sink; never a lossy
// downcast, never a silent drop).

// knownSchemas is the closed P0 registry — private and immutable at
// runtime; additions are code changes (Phase-1A codex #3).
var knownSchemas = map[SchemaID]map[int]bool{
	"nexus.event": {1: true},
}

// schemaKnown is the single admission check used by BOTH the wire parser
// and the constructor.
func schemaKnown(id SchemaID, version int) bool {
	v, ok := knownSchemas[id]
	return ok && v[version]
}

// UnknownSchemaError preserves the raw, unprocessed input (C7).
type UnknownSchemaError struct {
	SchemaID      SchemaID
	SchemaVersion int
	Raw           []byte // byte-for-byte input, untouched
}

func (e *UnknownSchemaError) Error() string {
	return fmt.Sprintf("unknown schema %s v%d rejected unprocessed (raw preserved, %d bytes)",
		e.SchemaID, e.SchemaVersion, len(e.Raw))
}

// ParseEnvelope decodes raw into a validated Envelope. Unknown schema
// ID/version → *UnknownSchemaError carrying the raw bytes; any field
// failing the P0.1 MUSTs → error. Unknown JSON fields are PRESERVED inside
// Payload semantics but the envelope-level decode is strict about types.
func ParseEnvelope(raw []byte) (Envelope, error) {
	var probe struct {
		SchemaID      SchemaID `json:"schema_id"`
		SchemaVersion int      `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return Envelope{}, fmt.Errorf("envelope is not valid JSON: %w", err)
	}
	if !schemaKnown(probe.SchemaID, probe.SchemaVersion) {
		rawCopy := make([]byte, len(raw))
		copy(rawCopy, raw)
		return Envelope{}, &UnknownSchemaError{
			SchemaID: probe.SchemaID, SchemaVersion: probe.SchemaVersion, Raw: rawCopy,
		}
	}
	var wire Envelope
	if err := json.Unmarshal(raw, &wire); err != nil {
		return Envelope{}, fmt.Errorf("envelope decode: %w", err)
	}
	// Re-run the constructor so wire-decoded envelopes obey the same MUSTs
	// as locally built ones (single validation owner). The exact admitted
	// bytes are preserved on the result (unknown-field preservation).
	env, err := NewEnvelope(EnvelopeParams{
		SchemaID: wire.SchemaID, SchemaVersion: wire.SchemaVersion, EventID: wire.EventID,
		EventType: wire.EventType, RunID: wire.RunID, TurnID: wire.TurnID,
		ToolCallID: wire.ToolCallID, ParentEventID: wire.ParentEventID,
		Sequence: wire.Sequence, EmittedAt: wire.EmittedAt, ActorType: wire.ActorType,
		ActorID: wire.ActorID, PrincipalID: wire.PrincipalID, TenantID: wire.TenantID,
		WorkspaceID: wire.WorkspaceID, ProfileID: wire.ProfileID, AttemptNo: wire.AttemptNo,
		IdempotencyKey: wire.IdempotencyKey, Payload: wire.Payload, PayloadHash: wire.PayloadHash,
	})
	if err != nil {
		return Envelope{}, err
	}
	rawCopy := make([]byte, len(raw))
	copy(rawCopy, raw)
	env.Wire = rawCopy
	return env, nil
}
