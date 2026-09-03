//go:build linux

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7min"
)

// PayloadClass drives the tolerance ladder (SPEC P0.7): GENERAL payloads
// may be re-asked and salvaged; SECURITY and EFFECT payloads are STRICT —
// a malformed discriminator reaches no sink, ever.
type PayloadClass uint8

const (
	ClassInvalid PayloadClass = iota
	ClassGeneral
	ClassSecurity
	ClassEffect
)

// ReAsk is one additional PHYSICAL provider attempt. The extractor issues
// a fresh S7 AttemptGrant; the TRANSPORT consumes it (provider.Chat/
// Stream), so a callback that tries a second physical call on the same
// grant dies with ATTEMPT_NOT_AUTHORIZED before any bytes leave the
// process (Phase-2 codex #2: consumption outside the transport was
// decorative). Adapters never retry on their own.
type ReAsk func(ctx context.Context, g s7min.Grant) ([]byte, error)

// Extractor is the S2.3-min structured-output owner.
type Extractor struct {
	auth *s7min.Authority
}

func NewExtractor(auth *s7min.Authority) *Extractor { return &Extractor{auth: auth} }

// decodeStrict is the ONLY acceptance path: strict JSON (unknown fields
// rejected, one value, no trailing data) + the caller's validator.
func decodeStrict[T any](raw []byte, validate func(T) error) (T, error) {
	var out T
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, fmt.Errorf("structured: %w", err)
	}
	if err := validate(out); err != nil {
		return out, fmt.Errorf("structured: validation: %w", err)
	}
	// dec.More() misses a lone bracket after the value; only EOF proves
	// there is no trailing data (Phase-2 kilo #7).
	if _, err := dec.Token(); err != io.EOF {
		return out, fmt.Errorf("structured: trailing data after the JSON value")
	}
	return out, nil
}

// salvageJSON extracts the first balanced top-level {...} object from
// prose. Salvage NEVER bypasses validation — it only relocates bytes.
func salvageJSON(raw []byte) ([]byte, bool) {
	depth, start := 0, -1
	inString, escaped := false, false
	for i, b := range raw {
		if inString {
			switch {
			case escaped:
				escaped = false
			case b == '\\':
				escaped = true
			case b == '"':
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			if depth > 0 {
				inString = true
			}
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
				if depth == 0 {
					return raw[start : i+1], true
				}
			}
		}
	}
	return nil, false
}

// Extract runs the P0.7 ladder for ONE structured payload:
//
//	strict decode+validate → (GENERAL only) re-ask with a fresh
//	AttemptGrant → salvage from re-ask, then from the original → error.
//
// It NEVER returns a value without a passed validation, and a nil
// validator is itself a refusal — "no validation" is silent accept.
func Extract[T any](ctx context.Context, e *Extractor, class PayloadClass, raw []byte,
	validate func(T) error, op contracts.OperationID, target contracts.TargetID, reask ReAsk) (T, error) {
	var zero T
	if validate == nil {
		return zero, fmt.Errorf("structured: a validator is required (fail closed)")
	}
	if out, err := decodeStrict(raw, validate); err == nil {
		return out, nil
	} else if class != ClassGeneral {
		// SECURITY/EFFECT: strict, no tolerant accept of any kind.
		return zero, fmt.Errorf("structured: %d-class payload invalid — no repair permitted (fail closed): %w", class, err)
	}
	// GENERAL: one re-ask, a NEW physical attempt. The transport consumes
	// the grant; the extractor only issues it and lands the outcome AFTER
	// salvage, so the S7 record matches what was actually returned
	// (Phase-2 kilo #6: reporting terminal before salvage understated a
	// recovered result).
	var reaskRaw []byte
	if reask != nil {
		if g, gerr := e.auth.Issue(op, target); gerr == nil {
			if rr, rerr := reask(ctx, g); rerr == nil {
				reaskRaw = rr
				if out, err := decodeStrict(rr, validate); err == nil {
					e.auth.Report(op, s7min.OutcomeSucceeded)
					return out, nil
				}
			}
		}
	}
	// Salvage: re-ask output first, then the original. A salvage hit from
	// the re-ask output means that PHYSICAL attempt ultimately produced
	// the accepted value.
	for _, src := range [][]byte{reaskRaw, raw} {
		if len(src) == 0 {
			continue
		}
		if obj, ok := salvageJSON(src); ok {
			if out, err := decodeStrict(obj, validate); err == nil {
				if len(reaskRaw) > 0 && &src[0] == &reaskRaw[0] {
					e.auth.Report(op, s7min.OutcomeSucceeded)
				} else if len(reaskRaw) > 0 {
					e.auth.Report(op, s7min.OutcomeFailedTerminal)
				}
				return out, nil
			}
		}
	}
	if len(reaskRaw) > 0 {
		e.auth.Report(op, s7min.OutcomeFailedTerminal)
	}
	return zero, fmt.Errorf("structured: output invalid after validate, re-ask and salvage (never silently accepted)")
}
