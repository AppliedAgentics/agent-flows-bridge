package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
)

func TestTickRefreshesWhenSessionNearExpiry(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	fake := &fakeRefresher{
		session: oauth.Session{
			AccessToken:  "at_old",
			RefreshToken: "rt_old",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			Scope:        "connector:heartbeat connector:webhook",
			ConnectorID:  55,
			RuntimeID:    77,
			RuntimeKind:  "local_connector",
			IssuedAt:     now.Add(-59 * time.Minute),
		},
		refreshedSession: oauth.Session{AccessToken: "at_new", RefreshToken: "rt_new"},
	}

	refreshedCalled := false
	worker := NewRefreshWorker(RefreshWorkerOptions{
		Refresher:    fake,
		Now:          func() time.Time { return now },
		RefreshAhead: 2 * time.Minute,
		OnRefreshed: func(_ oauth.Session) {
			refreshedCalled = true
		},
	})

	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}

	if fake.refreshCalls != 1 {
		t.Fatalf("expected refresh call, got %d", fake.refreshCalls)
	}
	if !refreshedCalled {
		t.Fatal("expected OnRefreshed callback")
	}
}

func TestTickSkipsRefreshWhenSessionStillFresh(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	fake := &fakeRefresher{
		session: oauth.Session{
			AccessToken:  "at_old",
			RefreshToken: "rt_old",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			Scope:        "connector:heartbeat connector:webhook",
			ConnectorID:  55,
			RuntimeID:    77,
			RuntimeKind:  "local_connector",
			IssuedAt:     now.Add(-5 * time.Minute),
		},
	}

	worker := NewRefreshWorker(RefreshWorkerOptions{
		Refresher:    fake,
		Now:          func() time.Time { return now },
		RefreshAhead: 2 * time.Minute,
	})

	if err := worker.Tick(context.Background()); err != nil {
		t.Fatalf("tick returned error: %v", err)
	}

	if fake.refreshCalls != 0 {
		t.Fatalf("expected no refresh call, got %d", fake.refreshCalls)
	}
}

func TestTickCallsFailureCallbackWhenRefreshFails(t *testing.T) {
	now := time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)
	fake := &fakeRefresher{
		session: oauth.Session{
			AccessToken:  "at_old",
			RefreshToken: "rt_old",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
			Scope:        "connector:heartbeat connector:webhook",
			ConnectorID:  55,
			RuntimeID:    77,
			RuntimeKind:  "local_connector",
			IssuedAt:     now.Add(-59 * time.Minute),
		},
		refreshErr: errors.New("boom"),
	}

	failureCalled := false
	worker := NewRefreshWorker(RefreshWorkerOptions{
		Refresher:    fake,
		Now:          func() time.Time { return now },
		RefreshAhead: 2 * time.Minute,
		OnFailure: func(_ error) {
			failureCalled = true
		},
	})

	err := worker.Tick(context.Background())
	if err == nil {
		t.Fatal("expected error from failed refresh")
	}
	if !failureCalled {
		t.Fatal("expected OnFailure callback")
	}
}

type fakeRefresher struct {
	session          oauth.Session
	refreshedSession oauth.Session
	refreshErr       error
	refreshCalls     int
}

func (f *fakeRefresher) LoadStoredSession(_ context.Context) (oauth.Session, error) {
	return f.session, nil
}

func (f *fakeRefresher) RefreshSession(_ context.Context) (oauth.Session, error) {
	f.refreshCalls++
	if f.refreshErr != nil {
		return oauth.Session{}, f.refreshErr
	}
	return f.refreshedSession, nil
}
