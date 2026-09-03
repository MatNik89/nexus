package contracts

import (
	"fmt"
	"strings"
)

// Typed identifiers. IDs are opaque non-empty strings without control
// characters; ProfileID additionally is a required admission-time stamp
// (HARDQ B3) and therefore validated everywhere it appears.

type (
	SchemaID   string
	EventID    string
	RunID      string
	TurnID     string
	ToolCallID string
	MessageID  string
	BlockID    string
	ActorID    string
	PrincipalID string
	TenantID    string
	WorkspaceID string
	ProfileID   string
	ToolID      string
)

func validID(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

func (v SchemaID) Valid() bool    { return validID(string(v)) }
func (v EventID) Valid() bool     { return validID(string(v)) }
func (v RunID) Valid() bool       { return validID(string(v)) }
func (v TurnID) Valid() bool      { return validID(string(v)) }
func (v ToolCallID) Valid() bool  { return validID(string(v)) }
func (v MessageID) Valid() bool   { return validID(string(v)) }
func (v BlockID) Valid() bool     { return validID(string(v)) }
func (v ActorID) Valid() bool     { return validID(string(v)) }
func (v PrincipalID) Valid() bool { return validID(string(v)) }
func (v TenantID) Valid() bool    { return validID(string(v)) }
func (v WorkspaceID) Valid() bool { return validID(string(v)) }
func (v ProfileID) Valid() bool   { return validID(string(v)) }
func (v ToolID) Valid() bool      { return validID(string(v)) }

func requireID(name, v string) error {
	if !validID(v) {
		// Never echo the value: it may contain a secret or control bytes
		// and this error can reach diagnostic sinks (Phase-1A codex #10).
		return fmt.Errorf("%s: invalid identifier (len=%d)", name, len(v))
	}
	return nil
}
