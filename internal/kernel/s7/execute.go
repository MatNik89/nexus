//go:build linux

package s7

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MatNik89/nexus/internal/kernel/contracts"
)

// Attempt is ONE physical attempt: it must Consume the grant inside the
// transport and report the honest outcome + code. It returns the value
// error the caller wants surfaced; S7 decides retry vs terminal.
type Attempt func(ctx context.Context, g Grant) (Outcome, string, error)

// Execute is the S7-OWNED synchronous driver for NON-durable operations:
// Begin, Next, callback, Report, backoff wait under ctx, repeat until
// terminal. The caller submits one operation and never sleeps, loops or
// calls Next (SPEC P0.2: the adapter/loop holds no retry loop). A callback
// that returns without consuming its grant is a local refusal: the
// operation is CANCELLED and its bytes are never trusted.
func (a *Authority) Execute(ctx context.Context, op contracts.OperationID, target contracts.TargetID, policy Policy, attempt Attempt) error {
	if attempt == nil {
		return fmt.Errorf("s7 execute: an attempt callback is required (fail closed)")
	}
	if policy.Durable {
		return fmt.Errorf("s7 execute: durable operations are tick-driven through Next (fail closed)")
	}
	if err := a.Begin(op, target, policy); err != nil {
		return err
	}
	var lastErr error
	for {
		g, err := a.Next(op, nil)
		switch {
		case err == nil:
		case errors.Is(err, ErrNotDue):
			if werr := a.waitDue(ctx, op); werr != nil {
				return werr
			}
			continue
		case errors.Is(err, ErrExhausted):
			if lastErr != nil {
				return fmt.Errorf("s7 execute: %w: %w", err, lastErr)
			}
			return err
		default:
			return err
		}
		outcome, code, aerr := attempt(ctx, g)
		st, _ := a.State(op)
		if st == contracts.AttemptAuthorized {
			// Never consumed: nothing physical ran; the callback's result is
			// a claim, not a transport outcome.
			_ = a.Cancel(op, nil)
			if aerr == nil {
				aerr = fmt.Errorf("s7 execute: attempt returned without consuming its grant (fail closed)")
			}
			return aerr
		}
		if rerr := a.Report(op, outcome, code, nil); rerr != nil {
			return fmt.Errorf("s7 execute: landing failed (fail closed): %w", rerr)
		}
		st, _ = a.State(op)
		switch st {
		case contracts.AttemptSucceeded:
			return nil
		case contracts.AttemptFailedRetryable:
			lastErr = aerr
			continue
		default:
			if aerr == nil {
				aerr = fmt.Errorf("s7 execute: attempt landed %s", st)
			}
			return aerr
		}
	}
}

// waitDue sleeps until the operation's next_attempt_at (or ctx ends).
func (a *Authority) waitDue(ctx context.Context, op contracts.OperationID) error {
	at := a.NextAt(op)
	a.mu.Lock()
	now := a.now()
	a.mu.Unlock()
	d := at.Sub(now)
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
	}
	// Fail closed if the authority's clock did not reach the due time (an
	// injected/frozen clock): never spin waiting for a clock that will not
	// move.
	a.mu.Lock()
	still := a.now().Before(at)
	a.mu.Unlock()
	if still {
		return fmt.Errorf("s7 execute: clock did not reach next_attempt_at: %w", ErrNotDue)
	}
	return nil
}
