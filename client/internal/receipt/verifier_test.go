package receipt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyAcceptsHTTP202AndReturnsRunID(t *testing.T) {
	token := "hook-token-abc"
	var observedAuthorization string
	var observedPath string
	var observedAgentID string
	var observedSessionKey string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observedAuthorization = r.Header.Get("Authorization")
		observedPath = r.URL.Path

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}

		if value, ok := payload["agentId"].(string); ok {
			observedAgentID = value
		}
		if value, ok := payload["sessionKey"].(string); ok {
			observedSessionKey = value
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"runId":"run-probe-123"}`))
	}))
	defer server.Close()

	openClawDataDir := t.TempDir()
	configPath := writeOpenClawConfig(t, openClawDataDir, map[string]any{
		"hooks": map[string]any{
			"token":           token,
			"path":            "/hooks",
			"allowedAgentIds": []string{"lead", "writer"},
		},
	})

	verifier := NewVerifier(Options{HTTPClient: server.Client()})
	result, err := verifier.Verify(context.Background(), VerifyInput{
		RuntimeURL:         server.URL,
		OpenClawConfigPath: configPath,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if !result.Accepted {
		t.Fatalf("expected accepted result, got %+v", result)
	}
	if result.HTTPStatus != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", result.HTTPStatus)
	}
	if result.RunID != "run-probe-123" {
		t.Fatalf("unexpected run id: %s", result.RunID)
	}
	if observedAuthorization != "Bearer "+token {
		t.Fatalf("unexpected auth header: %s", observedAuthorization)
	}
	if observedPath != "/hooks/agent" {
		t.Fatalf("unexpected request path: %s", observedPath)
	}
	if observedAgentID != "lead" {
		t.Fatalf("expected first allowed agent, got %q", observedAgentID)
	}
	if !strings.HasPrefix(observedSessionKey, "hook:probe:") {
		t.Fatalf("unexpected session key: %s", observedSessionKey)
	}
}

func TestVerifyReturnsNonAcceptedForHTTP401(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Unauthorized"))
	}))
	defer server.Close()

	openClawDataDir := t.TempDir()
	configPath := writeOpenClawConfig(t, openClawDataDir, map[string]any{
		"hooks": map[string]any{
			"token": "hook-token-abc",
			"path":  "/hooks",
		},
	})

	verifier := NewVerifier(Options{HTTPClient: server.Client()})
	result, err := verifier.Verify(context.Background(), VerifyInput{
		RuntimeURL:         server.URL,
		OpenClawConfigPath: configPath,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if result.Accepted {
		t.Fatalf("expected non-accepted result, got %+v", result)
	}
	if result.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", result.HTTPStatus)
	}
	if !strings.Contains(result.ResponseBody, "Unauthorized") {
		t.Fatalf("unexpected response body: %q", result.ResponseBody)
	}
}

func TestVerifyErrorsWhenHooksTokenMissing(t *testing.T) {
	openClawDataDir := t.TempDir()
	configPath := writeOpenClawConfig(t, openClawDataDir, map[string]any{
		"hooks": map[string]any{
			"path": "/hooks",
		},
	})

	verifier := NewVerifier(Options{})
	_, err := verifier.Verify(context.Background(), VerifyInput{
		RuntimeURL:         "http://127.0.0.1:18789",
		OpenClawConfigPath: configPath,
	})
	if err == nil {
		t.Fatal("expected missing hooks token error")
	}
	if !strings.Contains(err.Error(), "hooks.token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func writeOpenClawConfig(t *testing.T, openClawDataDir string, config map[string]any) string {
	t.Helper()

	if err := os.MkdirAll(openClawDataDir, 0o755); err != nil {
		t.Fatalf("mkdir openclaw dir: %v", err)
	}

	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	configPath := filepath.Join(openClawDataDir, "openclaw.json")
	if err := os.WriteFile(configPath, raw, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return configPath
}
