package ui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStateEndpointReturnsCurrentState(t *testing.T) {
	shell := NewShell(Options{})
	shell.Update(State{Status: StateConnected, RuntimeID: 77})

	server := httptest.NewServer(shell.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/api/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d", response.StatusCode)
	}

	var state State
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if state.Status != StateConnected {
		t.Fatalf("unexpected state status: %s", state.Status)
	}
	if state.RuntimeID != 77 {
		t.Fatalf("unexpected runtime_id: %d", state.RuntimeID)
	}
}

func TestRootPageRendersStatusAndNeverLeaksTokenFields(t *testing.T) {
	shell := NewShell(Options{})
	shell.Update(State{Status: StateAuthorizing, RuntimeID: 77, ErrorCode: "NETWORK_RETRY"})

	server := httptest.NewServer(shell.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	html := string(body)
	if !strings.Contains(html, "authorizing") {
		t.Fatalf("expected status text in html: %s", html)
	}
	if !strings.Contains(html, "NETWORK_RETRY") {
		t.Fatalf("expected error code in html: %s", html)
	}
	if strings.Contains(strings.ToLower(html), "access_token") || strings.Contains(strings.ToLower(html), "refresh_token") {
		t.Fatalf("root page leaked token field names: %s", html)
	}
}

func TestActionEndpointsInvokeCallbacks(t *testing.T) {
	actions := make([]Action, 0, 3)
	shell := NewShell(Options{OnAction: func(action Action) {
		actions = append(actions, action)
	}})

	server := httptest.NewServer(shell.Handler())
	defer server.Close()

	actionPaths := []string{"/api/action/authorize", "/api/action/reconnect", "/api/action/export-diagnostics"}
	for _, actionPath := range actionPaths {
		request, err := http.NewRequest(http.MethodPost, server.URL+actionPath, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("post action: %v", err)
		}
		response.Body.Close()

		if response.StatusCode != http.StatusAccepted {
			t.Fatalf("unexpected action status code for %s: %d", actionPath, response.StatusCode)
		}
	}

	if len(actions) != 3 {
		t.Fatalf("expected three actions, got %d", len(actions))
	}

	if actions[0] != ActionAuthorize || actions[1] != ActionReconnect || actions[2] != ActionExportDiagnostics {
		t.Fatalf("unexpected actions: %+v", actions)
	}
}

func TestInvalidActionReturnsNotFound(t *testing.T) {
	shell := NewShell(Options{})
	server := httptest.NewServer(shell.Handler())
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/action/unknown", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("post request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unexpected status code: %d", response.StatusCode)
	}
}
