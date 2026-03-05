package session

import (
	"context"
	"fmt"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
)

// SessionRefresher define oauth session refresh behavior needed by the worker.
type SessionRefresher interface {
	LoadStoredSession(ctx context.Context) (oauth.Session, error)
	RefreshSession(ctx context.Context) (oauth.Session, error)
}

// RefreshWorkerOptions configure token refresh polling and callbacks.
type RefreshWorkerOptions struct {
	Refresher    SessionRefresher
	PollInterval time.Duration
	RefreshAhead time.Duration
	Now          func() time.Time
	OnRefreshed  func(session oauth.Session)
	OnFailure    func(err error)
}

// RefreshWorker periodically refreshes OAuth session tokens ahead of expiry.
type RefreshWorker struct {
	refresher    SessionRefresher
	pollInterval time.Duration
	refreshAhead time.Duration
	now          func() time.Time
	onRefreshed  func(session oauth.Session)
	onFailure    func(err error)
}

// NewRefreshWorker construct a refresh worker with defaults.
//
// Defaults:
// - PollInterval: 30s
// - RefreshAhead: 5m
// - Now: time.Now().UTC
//
// Returns a configured worker instance.
func NewRefreshWorker(opts RefreshWorkerOptions) *RefreshWorker {
	pollInterval := opts.PollInterval
	refreshAhead := opts.RefreshAhead
	now := opts.Now

	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}
	if refreshAhead <= 0 {
		refreshAhead = 5 * time.Minute
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	return &RefreshWorker{
		refresher:    opts.Refresher,
		pollInterval: pollInterval,
		refreshAhead: refreshAhead,
		now:          now,
		onRefreshed:  opts.OnRefreshed,
		onFailure:    opts.OnFailure,
	}
}

// Tick run one refresh evaluation cycle.
//
// Loads the stored session and refreshes when expiry is within `RefreshAhead`.
// On failure, invokes OnFailure callback when provided.
//
// Returns nil on success/no-op, or an error on evaluation/refresh failure.
func (w *RefreshWorker) Tick(ctx context.Context) error {
	if w.refresher == nil {
		err := fmt.Errorf("session refresher is required")
		w.notifyFailure(err)
		return err
	}

	session, err := w.refresher.LoadStoredSession(ctx)
	if err != nil {
		w.notifyFailure(err)
		return err
	}

	if session.IssuedAt.IsZero() || session.ExpiresIn <= 0 {
		err := fmt.Errorf("stored session has invalid expiry metadata")
		w.notifyFailure(err)
		return err
	}

	now := w.now().UTC()
	expiresAt := session.IssuedAt.UTC().Add(time.Duration(session.ExpiresIn) * time.Second)
	refreshAt := expiresAt.Add(-w.refreshAhead)

	if now.Before(refreshAt) {
		return nil
	}

	refreshedSession, err := w.refresher.RefreshSession(ctx)
	if err != nil {
		w.notifyFailure(err)
		return err
	}

	if w.onRefreshed != nil {
		w.onRefreshed(refreshedSession)
	}

	return nil
}

// Run execute refresh checks until context cancellation.
//
// Performs an immediate Tick followed by periodic checks at PollInterval.
// Returns nil when context is canceled.
//
// Returns nil on normal shutdown or error only when immediate tick fails.
func (w *RefreshWorker) Run(ctx context.Context) error {
	if err := w.Tick(ctx); err != nil {
		return err
	}

	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_ = w.Tick(ctx)
		}
	}
}

func (w *RefreshWorker) notifyFailure(err error) {
	if w.onFailure != nil {
		w.onFailure(err)
	}
}
