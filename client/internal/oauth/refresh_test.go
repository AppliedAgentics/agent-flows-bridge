package oauth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentflows/agent-flows-bridge/client/internal/secrets"
)

func TestRefreshSessionRotatesTokensAndPersistsResult(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}

		if r.Form.Get("grant_type") != "refresh_token" {
			t.Fatalf("unexpected grant_type: %s", r.Form.Get("grant_type"))
		}
		if r.Form.Get("client_id") != "agent-flows-bridge" {
			t.Fatalf("unexpected client_id: %s", r.Form.Get("client_id"))
		}
		if r.Form.Get("refresh_token") != "rt_old" {
			t.Fatalf("unexpected refresh_token: %s", r.Form.Get("refresh_token"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"at_new","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_new","scope":"connector:heartbeat connector:webhook","connector_id":55}`)
	}))
	defer server.Close()

	client := NewClient(Options{
		APIBaseURL:    server.URL,
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Sid MacBook Pro",
		Platform:      "macos",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	initialSession := Session{
		AccessToken:  "at_old",
		RefreshToken: "rt_old",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
	}

	if err := client.persistSession(ctx, initialSession); err != nil {
		t.Fatalf("persist initial session: %v", err)
	}

	refreshedSession, err := client.RefreshSession(ctx)
	if err != nil {
		t.Fatalf("refresh session: %v", err)
	}

	if refreshedSession.AccessToken != "at_new" {
		t.Fatalf("unexpected access token: %s", refreshedSession.AccessToken)
	}
	if refreshedSession.RefreshToken != "rt_new" {
		t.Fatalf("unexpected refresh token: %s", refreshedSession.RefreshToken)
	}
	if refreshedSession.RuntimeID != 77 {
		t.Fatalf("expected runtime id to be preserved, got %d", refreshedSession.RuntimeID)
	}
	if refreshedSession.RuntimeKind != "local_connector" {
		t.Fatalf("expected runtime kind to be preserved, got %s", refreshedSession.RuntimeKind)
	}

	storedSession, err := client.LoadStoredSession(ctx)
	if err != nil {
		t.Fatalf("load stored session: %v", err)
	}

	if storedSession.AccessToken != "at_new" || storedSession.RefreshToken != "rt_new" {
		t.Fatal("stored session was not rotated")
	}
}

func TestRefreshSessionDoesNotOverwriteStoredSessionOnFailure(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"error":"invalid_grant"}`)
	}))
	defer server.Close()

	client := NewClient(Options{
		APIBaseURL:    server.URL,
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Sid MacBook Pro",
		Platform:      "macos",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	initialSession := Session{
		AccessToken:  "at_old",
		RefreshToken: "rt_old",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
	}

	if err := client.persistSession(ctx, initialSession); err != nil {
		t.Fatalf("persist initial session: %v", err)
	}

	_, err = client.RefreshSession(ctx)
	if err == nil {
		t.Fatal("expected refresh failure")
	}

	storedSession, err := client.LoadStoredSession(ctx)
	if err != nil {
		t.Fatalf("load stored session: %v", err)
	}

	if storedSession.RefreshToken != "rt_old" {
		t.Fatalf("expected old refresh token to remain, got %s", storedSession.RefreshToken)
	}
}
