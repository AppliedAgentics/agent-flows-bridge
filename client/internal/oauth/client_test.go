package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/secrets"
)

func TestStartLoginBuildsAuthorizeURLWithPKCE(t *testing.T) {
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	client := NewClient(Options{
		APIBaseURL:    "https://saas.example.test",
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Sid MacBook Pro",
		Platform:      "macos",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	start, err := client.StartLogin(77)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}

	if start.State == "" {
		t.Fatal("expected non-empty state")
	}
	if start.CodeVerifier == "" {
		t.Fatal("expected non-empty code verifier")
	}
	if start.RedirectURI != "http://127.0.0.1:49200/oauth/callback" {
		t.Fatalf("unexpected redirect uri: %s", start.RedirectURI)
	}

	authorizeURL, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}

	if authorizeURL.Path != "/oauth/bridge/sign-in" {
		t.Fatalf("unexpected path: %s", authorizeURL.Path)
	}

	query := authorizeURL.Query()
	assertQuery(t, query, "response_type", "code")
	assertQuery(t, query, "client_id", "agent-flows-bridge")
	assertQuery(t, query, "redirect_uri", start.RedirectURI)
	assertQuery(t, query, "scope", "connector:bootstrap connector:heartbeat connector:webhook")
	assertQuery(t, query, "state", start.State)
	assertQuery(t, query, "code_challenge_method", "S256")
	assertQuery(t, query, "runtime_id", "77")
	assertQuery(t, query, "device_name", "Sid MacBook Pro")
	assertQuery(t, query, "platform", "macos")

	challenge := query.Get("code_challenge")
	if challenge == "" {
		t.Fatal("expected code_challenge to be present")
	}

	expectedChallenge := pkceChallenge(start.CodeVerifier)
	if challenge != expectedChallenge {
		t.Fatalf("unexpected challenge\nwant=%s\ngot=%s", expectedChallenge, challenge)
	}
}

func TestStartLoginOmitsRuntimeIDWhenUnspecified(t *testing.T) {
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	client := NewClient(Options{
		APIBaseURL:    "https://saas.example.test",
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Sid MacBook Pro",
		Platform:      "macos",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	start, err := client.StartLogin(0)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}

	authorizeURL, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}

	query := authorizeURL.Query()
	if query.Has("runtime_id") {
		t.Fatalf("expected runtime_id to be omitted, got %q", query.Get("runtime_id"))
	}
	if start.RuntimeID != 0 {
		t.Fatalf("expected runtime id 0, got %d", start.RuntimeID)
	}
}

func TestStartLoginWithIntentIncludesReconnectFields(t *testing.T) {
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	client := NewClient(Options{
		APIBaseURL:    "https://saas.example.test",
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Sid MacBook Pro",
		Platform:      "macos",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	start, err := client.StartLoginWithIntent(77, "reconnect")
	if err != nil {
		t.Fatalf("start login with intent: %v", err)
	}

	authorizeURL, err := url.Parse(start.AuthorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}

	query := authorizeURL.Query()
	assertQuery(t, query, "runtime_id", "77")
	assertQuery(t, query, "intent", "reconnect")

	if start.Intent != "reconnect" {
		t.Fatalf("expected start intent reconnect, got %q", start.Intent)
	}
}

func TestCompleteLoginFromCallbackURLExchangesTokenAndPersistsSession(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		if r.URL.Path != "/oauth/token" {
			t.Fatalf("expected /oauth/token, got %s", r.URL.Path)
		}

		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}

		if r.Form.Get("grant_type") != "authorization_code" {
			t.Fatalf("unexpected grant_type: %s", r.Form.Get("grant_type"))
		}
		if r.Form.Get("client_id") != "agent-flows-bridge" {
			t.Fatalf("unexpected client_id: %s", r.Form.Get("client_id"))
		}
		if r.Form.Get("code") != "auth-code-123" {
			t.Fatalf("unexpected code: %s", r.Form.Get("code"))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"at_123","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_123","scope":"connector:heartbeat connector:webhook","connector_id":55,"runtime":{"id":77,"runtime_kind":"local_connector"}}`)
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

	start, err := client.StartLogin(77)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}

	callbackURL := start.RedirectURI + "?code=auth-code-123&state=" + url.QueryEscape(start.State)

	tokenSet, err := client.CompleteLoginFromCallbackURL(ctx, start, callbackURL)
	if err != nil {
		t.Fatalf("complete login: %v", err)
	}

	if tokenSet.AccessToken != "at_123" {
		t.Fatalf("unexpected access token: %s", tokenSet.AccessToken)
	}
	if tokenSet.RefreshToken != "rt_123" {
		t.Fatalf("unexpected refresh token: %s", tokenSet.RefreshToken)
	}
	if tokenSet.ConnectorID != 55 {
		t.Fatalf("unexpected connector id: %d", tokenSet.ConnectorID)
	}

	stored, err := client.LoadStoredSession(ctx)
	if err != nil {
		t.Fatalf("load stored session: %v", err)
	}

	if stored.AccessToken != tokenSet.AccessToken || stored.RefreshToken != tokenSet.RefreshToken {
		t.Fatal("stored session does not match exchanged token set")
	}
}

func TestCompleteLoginFromCallbackURLRejectsStateMismatch(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	client := NewClient(Options{
		APIBaseURL:    "https://saas.example.test",
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Sid MacBook Pro",
		Platform:      "macos",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	start, err := client.StartLogin(77)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}

	callbackURL := start.RedirectURI + "?code=auth-code-123&state=wrong-state"
	_, err = client.CompleteLoginFromCallbackURL(ctx, start, callbackURL)
	if err == nil {
		t.Fatal("expected state mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "state") {
		t.Fatalf("expected state mismatch error, got %v", err)
	}

	_, err = client.LoadStoredSession(ctx)
	if err == nil {
		t.Fatal("expected no stored session")
	}
}

func TestLoadStoredSessionReturnsErrorForInvalidPayload(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	if err := store.Save(ctx, sessionSecretKey, []byte("not-json")); err != nil {
		t.Fatalf("save secret: %v", err)
	}

	client := NewClient(Options{
		APIBaseURL:    "https://saas.example.test",
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Sid MacBook Pro",
		Platform:      "macos",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	_, err = client.LoadStoredSession(ctx)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestPersistSessionEncodesRoundTripJSON(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	client := NewClient(Options{
		APIBaseURL:    "https://saas.example.test",
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Sid MacBook Pro",
		Platform:      "macos",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	session := Session{
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	raw, err := store.Load(ctx, sessionSecretKey)
	if err != nil {
		t.Fatalf("load raw secret: %v", err)
	}

	var decoded Session
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal stored session: %v", err)
	}

	if decoded.ConnectorID != session.ConnectorID || decoded.RuntimeID != session.RuntimeID {
		t.Fatalf("unexpected decoded session: %+v", decoded)
	}
}

func TestClearLocalStateDeletesStoredSessionAndBootstrap(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	client := NewClient(Options{
		APIBaseURL:    "https://saas.example.test",
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Sid MacBook Pro",
		Platform:      "macos",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	session := Session{
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	bootstrapPayload := BootstrapPayload{
		Runtime: BootstrapRuntime{
			ID:          77,
			RuntimeKind: "local_connector",
			FlowID:      42,
		},
		Env: map[string]string{
			"AGENT_FLOWS_API_URL": "https://saas.example.test",
			"AGENT_FLOWS_API_KEY": "runtime_key_123",
		},
		Config:         map[string]any{"hooks": map[string]any{"enabled": true}},
		WorkspaceFiles: map[string]map[string]string{"/data/openclaw/workspace": {"AGENTS.md": "content"}},
		FetchedAt:      time.Now().UTC().Truncate(time.Second),
	}

	if err := client.persistBootstrap(ctx, bootstrapPayload); err != nil {
		t.Fatalf("persist bootstrap payload: %v", err)
	}

	if err := client.ClearLocalState(ctx); err != nil {
		t.Fatalf("clear local state: %v", err)
	}

	_, err = client.LoadStoredSession(ctx)
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("expected session not found after clear, got %v", err)
	}

	_, err = client.LoadStoredBootstrap(ctx)
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("expected bootstrap not found after clear, got %v", err)
	}
}

func TestSyncBootstrapFetchesPayloadAndPersistsIt(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	bootstrapCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		if r.URL.Path != "/api/connectors/bootstrap" {
			t.Fatalf("expected /api/connectors/bootstrap, got %s", r.URL.Path)
		}

		if r.Header.Get("Authorization") != "Bearer at_123" {
			t.Fatalf("unexpected authorization header: %s", r.Header.Get("Authorization"))
		}

		bootstrapCalls++

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":{"runtime":{"id":77,"runtime_kind":"local_connector","flow_id":42},"env":{"AGENT_FLOWS_API_URL":"https://agentflows.example.test","AGENT_FLOWS_API_KEY":"runtime_key_123"},"config":{"hooks":{"enabled":true}},"workspace_files":{"/data/openclaw/workspace":{"AGENTS.md":"content"}}},"meta":{"timestamp":"2026-03-04T00:00:00Z"}}`)
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

	session := Session{
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	bootstrapPayload, err := client.SyncBootstrap(ctx)
	if err != nil {
		t.Fatalf("sync bootstrap: %v", err)
	}

	if bootstrapCalls != 1 {
		t.Fatalf("expected one bootstrap call, got %d", bootstrapCalls)
	}

	if bootstrapPayload.Runtime.ID != 77 {
		t.Fatalf("unexpected runtime id: %d", bootstrapPayload.Runtime.ID)
	}

	if bootstrapPayload.Env["AGENT_FLOWS_API_KEY"] != "runtime_key_123" {
		t.Fatalf("unexpected api key in bootstrap env: %s", bootstrapPayload.Env["AGENT_FLOWS_API_KEY"])
	}

	storedBootstrap, err := client.LoadStoredBootstrap(ctx)
	if err != nil {
		t.Fatalf("load stored bootstrap: %v", err)
	}

	if storedBootstrap.Runtime.FlowID != 42 {
		t.Fatalf("unexpected stored flow id: %d", storedBootstrap.Runtime.FlowID)
	}
}

func TestDisconnectConnectorRevokesConnector(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}

		if r.URL.Path != "/api/connectors/disconnect" {
			t.Fatalf("expected /api/connectors/disconnect, got %s", r.URL.Path)
		}

		if got := r.Header.Get("Authorization"); got != "Bearer at_123" {
			t.Fatalf("unexpected authorization header: %s", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":{"revoked":true,"connector_id":55}}`)
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

	session := Session{
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	result, err := client.DisconnectConnector(ctx)
	if err != nil {
		t.Fatalf("disconnect connector: %v", err)
	}

	if !result.Revoked {
		t.Fatalf("expected revoked=true, got %+v", result)
	}
	if result.ConnectorID != 55 {
		t.Fatalf("expected connector id 55, got %d", result.ConnectorID)
	}
}

func TestDisconnectConnectorReturnsErrorForFailedRequest(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"error":"invalid_token"}`)
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

	session := Session{
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	_, err = client.DisconnectConnector(ctx)
	if err == nil {
		t.Fatal("expected disconnect error")
	}
	if !strings.Contains(err.Error(), "disconnect request failed") {
		t.Fatalf("expected disconnect request failed error, got %v", err)
	}
}

func TestLoadFreshSessionReturnsStoredSessionWhenStillFresh(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	client := NewClient(Options{
		APIBaseURL:    "https://saas.example.test",
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Sid MacBook Pro",
		Platform:      "macos",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	session := Session{
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Add(-1 * time.Minute).Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	loaded, err := client.LoadFreshSession(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("load fresh session: %v", err)
	}

	if loaded.AccessToken != session.AccessToken {
		t.Fatalf("expected stored session without refresh, got %q", loaded.AccessToken)
	}
}

func TestLoadFreshSessionRefreshesExpiredSession(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	refreshCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalls++

		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}

		if r.Form.Get("grant_type") != "refresh_token" {
			t.Fatalf("unexpected grant_type: %s", r.Form.Get("grant_type"))
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

	session := Session{
		AccessToken:  "at_old",
		RefreshToken: "rt_old",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	loaded, err := client.LoadFreshSession(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("load fresh session: %v", err)
	}

	if refreshCalls != 1 {
		t.Fatalf("expected one refresh call, got %d", refreshCalls)
	}
	if loaded.AccessToken != "at_new" {
		t.Fatalf("expected refreshed access token, got %q", loaded.AccessToken)
	}
}

func TestClaimWebhookEventRefreshesExpiringSessionBeforeRequest(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	serverCalls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls = append(serverCalls, r.URL.Path)

		switch r.URL.Path {
		case "/oauth/token":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"access_token":"at_refreshed","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_refreshed","scope":"connector:heartbeat connector:webhook","connector_id":55}`)
		case "/api/connectors/webhooks/claim":
			if got := r.Header.Get("Authorization"); got != "Bearer at_refreshed" {
				t.Fatalf("unexpected authorization header: %s", got)
			}

			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"data":{"event":null}}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
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

	session := Session{
		AccessToken:  "at_expiring",
		RefreshToken: "rt_old",
		TokenType:    "Bearer",
		ExpiresIn:    60,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Add(-59 * time.Second).Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	event, err := client.ClaimWebhookEvent(ctx)
	if err != nil {
		t.Fatalf("claim webhook event: %v", err)
	}

	if event != nil {
		t.Fatalf("expected nil event, got %+v", event)
	}
	if len(serverCalls) != 2 || serverCalls[0] != "/oauth/token" || serverCalls[1] != "/api/connectors/webhooks/claim" {
		t.Fatalf("unexpected server call sequence: %+v", serverCalls)
	}
}

func TestCompleteLoginFromCallbackURLRejectsOversizedTokenResponse(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, oversizedJSONBody())
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

	start, err := client.StartLogin(77)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}

	callbackURL := start.RedirectURI + "?code=auth-code-123&state=" + url.QueryEscape(start.State)
	_, err = client.CompleteLoginFromCallbackURL(ctx, start, callbackURL)
	if err == nil || !strings.Contains(err.Error(), "response body exceeds 1048576 bytes") {
		t.Fatalf("expected oversized body error, got %v", err)
	}
}

func TestRefreshSessionRejectsOversizedRefreshResponse(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, oversizedJSONBody())
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

	session := Session{
		AccessToken:  "at_old",
		RefreshToken: "rt_old",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	_, err = client.RefreshSession(ctx)
	if err == nil || !strings.Contains(err.Error(), "response body exceeds 1048576 bytes") {
		t.Fatalf("expected oversized body error, got %v", err)
	}
}

func TestSyncBootstrapRejectsOversizedBootstrapResponse(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/connectors/bootstrap" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, oversizedJSONBody())
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

	session := Session{
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	_, err = client.SyncBootstrap(ctx)
	if err == nil || !strings.Contains(err.Error(), "response body exceeds 1048576 bytes") {
		t.Fatalf("expected oversized body error, got %v", err)
	}
}

func TestClaimWebhookEventRejectsOversizedResponse(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/connectors/webhooks/claim" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, oversizedJSONBody())
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

	session := Session{
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	_, err = client.ClaimWebhookEvent(ctx)
	if err == nil || !strings.Contains(err.Error(), "response body exceeds 1048576 bytes") {
		t.Fatalf("expected oversized body error, got %v", err)
	}
}

func TestReportWebhookResultRejectsOversizedResponse(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/connectors/webhooks/result" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, oversizedJSONBody())
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

	session := Session{
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	_, err = client.ReportWebhookResult(ctx, "wev_123", "delivered", "run_123", "")
	if err == nil || !strings.Contains(err.Error(), "response body exceeds 1048576 bytes") {
		t.Fatalf("expected oversized body error, got %v", err)
	}
}

func TestDisconnectConnectorRejectsOversizedResponse(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/connectors/disconnect" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, oversizedJSONBody())
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

	session := Session{
		AccessToken:  "at_123",
		RefreshToken: "rt_123",
		TokenType:    "Bearer",
		ExpiresIn:    3600,
		Scope:        "connector:heartbeat connector:webhook",
		ConnectorID:  55,
		RuntimeID:    77,
		RuntimeKind:  "local_connector",
		IssuedAt:     time.Now().UTC().Truncate(time.Second),
	}

	if err := client.persistSession(ctx, session); err != nil {
		t.Fatalf("persist session: %v", err)
	}

	_, err = client.DisconnectConnector(ctx)
	if err == nil || !strings.Contains(err.Error(), "response body exceeds 1048576 bytes") {
		t.Fatalf("expected oversized body error, got %v", err)
	}
}

func oversizedJSONBody() string {
	return `{"data":"` + strings.Repeat("x", 1_048_576) + `"}`
}

func assertQuery(t *testing.T, query url.Values, key, expected string) {
	t.Helper()
	actual := query.Get(key)
	if actual != expected {
		t.Fatalf("unexpected query %s: want=%s got=%s", key, expected, actual)
	}
}
