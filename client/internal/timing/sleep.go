package timing

import (
	"context"
	"time"
)

// Sleep wait for a duration or context cancellation.
//
// Uses a timer so callers can stop waiting immediately when the context is
// canceled.
//
// Returns nil after the duration elapses or the context error when canceled.
func Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
