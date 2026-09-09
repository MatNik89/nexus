//go:build linux

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
	"github.com/MatNik89/nexus/internal/kernel/s7"
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

// Generate is ONE physical structured generation: attempt 1 (reask=false)
// produces the initial reply, attempt 2 (reask=true) the re-ask. The
// TRANSPORT consumes the grant (provider.Chat/Stream); a callback that
// never consumes is a local refusal — its bytes are a claim, not a result.
type Generate func(ctx context.Context, g s7.Grant, reask bool) ([]byte, error)

// Extractor is the S2.3 structured-output owner. It VALIDATES and PROPOSES;
// S7 (Execute, PolicyStructured) owns the one re-ask.
type Extractor struct {
	auth *s7.Authority
}

func NewExtractor(auth *s7.Authority) *Extractor { return &Extractor{auth: auth} }

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

// ErrStructuredInvalid is the P0.7 literal: invalid output NEVER comes back
// as a value without an error.
var ErrStructuredInvalid = errors.New("structured: output invalid after validate, re-ask and salvage (never silently accepted)")

// ExtractVia runs the P0.7 ladder as ONE S7 operation (Slice B3, plan v12):
// attempt 1 = initial generation + strict validation; for GENERAL only, an
// invalid result is the typed proposal invalid_general and S7 issues EXACTLY
// one re-ask (PolicyStructured MaxAttempts 2); attempt 2 = re-ask + strict
// validation, then — INSIDE the final attempt's outcome decision — the
// salvage ladder (re-ask bytes, then the original bytes). An accepted value
// lands SUCCEEDED; nothing accepted lands FAILED_TERMINAL and NO value is
// returned: the caller's result and the S7 state are one decision.
// SECURITY/EFFECT classes propose terminal on the first invalid result (no
// re-ask, no salvage). The extractor never calls Issue/Next: S7 owns the
// retry.
func ExtractVia[T any](ctx context.Context, e *Extractor, class PayloadClass, validate func(T) error,
	op contracts.OperationID, target contracts.TargetID, generate Generate) (T, error) {
	var zero T
	if validate == nil {
		return zero, fmt.Errorf("structured: a validator is required (fail closed)")
	}
	if generate == nil {
		return zero, fmt.Errorf("structured: a generator is required (fail closed)")
	}
	var (
		accepted T
		gotValue bool
		firstRaw []byte
		lastErr  error
	)
	final := s7.PolicyStructured.MaxAttempts
	attempt := func(ctx context.Context, g s7.Grant) (s7.Outcome, string, error) {
		reask := g.AttemptNo > 1
		raw, err := generate(ctx, g, reask)
		if err != nil {
			// A transport failure of the generation itself is terminal for
			// this operation — but on the FINAL attempt the salvage ladder
			// still runs over whatever bytes exist (re-ask bytes, then the
			// original) before the outcome is decided (plan B3; code-review
			// r2 codex #5).
			lastErr = err
			if g.AttemptNo >= final {
				for _, src := range [][]byte{raw, firstRaw} {
					if obj, ok := salvageJSON(src); ok {
						if out, verr := decodeStrict(obj, validate); verr == nil {
							accepted, gotValue = out, true
							return s7.OutcomeSucceeded, "", nil
						}
					}
				}
			}
			return s7.OutcomeFailedTerminal, s7.CodeTransportPostWrite, err
		}
		if !reask {
			firstRaw = raw
		}
		if out, verr := decodeStrict(raw, validate); verr == nil {
			accepted, gotValue = out, true
			return s7.OutcomeSucceeded, "", nil
		} else {
			lastErr = verr
			if class != ClassGeneral {
				// SECURITY/EFFECT: strict, no tolerant accept of any kind.
				lastErr = fmt.Errorf("structured: %d-class payload invalid — no repair permitted (fail closed): %w", class, verr)
				return s7.OutcomeFailedTerminal, s7.CodeInvalidGeneral, lastErr
			}
		}
		if g.AttemptNo < final {
			return s7.OutcomeFailedRetryable, s7.CodeInvalidGeneral, lastErr
		}
		// FINAL attempt: salvage INSIDE the outcome decision — re-ask bytes,
		// then the original bytes. Salvage relocates bytes; it never skips
		// validation.
		for _, src := range [][]byte{raw, firstRaw} {
			if obj, ok := salvageJSON(src); ok {
				if out, verr := decodeStrict(obj, validate); verr == nil {
					accepted, gotValue = out, true
					return s7.OutcomeSucceeded, "", nil
				}
			}
		}
		return s7.OutcomeFailedTerminal, s7.CodeInvalidGeneral, lastErr
	}
	err := e.auth.Execute(ctx, op, target, s7.PolicyStructured, attempt)
	st, _ := e.auth.State(op)
	if err == nil && gotValue && st == contracts.AttemptSucceeded {
		return accepted, nil
	}
	if gotValue && st != contracts.AttemptSucceeded {
		// Never hand back a value S7 did not land as SUCCEEDED.
		return zero, fmt.Errorf("structured: S7 outcome not recorded — value refused (fail closed): %v", err)
	}
	if err == nil {
		err = ErrStructuredInvalid
	}
	return zero, fmt.Errorf("%w: %v", ErrStructuredInvalid, err)
}
