package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/health"
	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
	"github.com/agentflows/agent-flows-bridge/client/internal/secrets"
)

func TestOAuthCompleteAndHealthProbeIntegration(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()

	cloud := newMockCloudServer(t)
	defer cloud.Close()

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer gateway.Close()

	store, err := secrets.NewStore(secrets.Options{StateDir: stateDir, PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new secret store: %v", err)
	}

	oauthClient := oauth.NewClient(oauth.Options{
		APIBaseURL:    cloud.URL,
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Integration Host",
		Platform:      "darwin",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	start, err := oauthClient.StartLogin(77)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}

	callbackURL := start.RedirectURI + "?code=auth-code-123&state=" + url.QueryEscape(start.State)

	session, err := oauthClient.CompleteLoginFromCallbackURL(ctx, start, callbackURL)
	if err != nil {
		t.Fatalf("complete login: %v", err)
	}

	if session.ConnectorID != 55 {
		t.Fatalf("unexpected connector id: %d", session.ConnectorID)
	}

	storedSession, err := oauthClient.LoadStoredSession(ctx)
	if err != nil {
		t.Fatalf("load stored session: %v", err)
	}
	if storedSession.RefreshToken != "rt_123" {
		t.Fatalf("unexpected stored refresh token: %s", storedSession.RefreshToken)
	}

	prober := health.NewProber(health.Options{Timeout: 2 * time.Second})
	probeResult := prober.Probe(ctx, gateway.URL)
	if !probeResult.GatewayReachable {
		t.Fatalf("expected reachable gateway, got %+v", probeResult)
	}
}

func TestRefreshFlowIntegration(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()

	cloud := newMockCloudServer(t)
	defer cloud.Close()

	store, err := secrets.NewStore(secrets.Options{StateDir: stateDir, PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new secret store: %v", err)
	}

	oauthClient := oauth.NewClient(oauth.Options{
		APIBaseURL:    cloud.URL,
		OAuthClientID: "agent-flows-bridge",
		DeviceName:    "Integration Host",
		Platform:      "darwin",
		RedirectPort:  49200,
		SecretStore:   store,
	})

	start, err := oauthClient.StartLogin(77)
	if err != nil {
		t.Fatalf("start login: %v", err)
	}

	callbackURL := start.RedirectURI + "?code=auth-code-123&state=" + url.QueryEscape(start.State)
	if _, err := oauthClient.CompleteLoginFromCallbackURL(ctx, start, callbackURL); err != nil {
		t.Fatalf("complete login: %v", err)
	}

	refreshedSession, err := oauthClient.RefreshSession(ctx)
	if err != nil {
		t.Fatalf("refresh session: %v", err)
	}

	if refreshedSession.AccessToken != "at_refreshed" {
		t.Fatalf("unexpected refreshed access token: %s", refreshedSession.AccessToken)
	}
	if refreshedSession.RefreshToken != "rt_refreshed" {
		t.Fatalf("unexpected refreshed refresh token: %s", refreshedSession.RefreshToken)
	}
}

func newMockCloudServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":"not_found"}`)
			return
		}

		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `{"error":"invalid_request"}`)
			return
		}

		switch r.Form.Get("grant_type") {
		case "authorization_code":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"access_token":"at_123","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_123","scope":"connector:heartbeat connector:webhook","connector_id":55,"runtime":{"id":77,"runtime_kind":"local_connector"}}`)
		case "refresh_token":
			if r.Form.Get("refresh_token") != "rt_123" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, `{"error":"invalid_grant"}`)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"access_token":"at_refreshed","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_refreshed","scope":"connector:heartbeat connector:webhook","connector_id":55}`)
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, `{"error":"unsupported_grant_type"}`)
		}
	}))
}
