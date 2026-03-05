package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUISmokePrimaryRoutesAndActions(t *testing.T) {
	recordedActions := make([]Action, 0, 3)

	shell := NewShell(Options{OnAction: func(action Action) {
		recordedActions = append(recordedActions, action)
	}})
	shell.Update(State{Status: StateDisconnected, RuntimeID: 77, ErrorCode: "NETWORK_RETRY"})

	server := httptest.NewServer(shell.Handler())
	defer server.Close()

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatalf("get root: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("unexpected root status: %d", response.StatusCode)
	}
	response.Body.Close()

	stateResponse, err := http.Get(server.URL + "/api/state")
	if err != nil {
		t.Fatalf("get state: %v", err)
	}

	var state State
	if err := json.NewDecoder(stateResponse.Body).Decode(&state); err != nil {
		t.Fatalf("decode state: %v", err)
	}
	stateResponse.Body.Close()

	if state.Status != StateDisconnected {
		t.Fatalf("unexpected state status: %s", state.Status)
	}

	actionsResponse, err := http.Get(server.URL + "/api/actions")
	if err != nil {
		t.Fatalf("get actions: %v", err)
	}

	if actionsResponse.StatusCode != http.StatusOK {
		t.Fatalf("unexpected actions status: %d", actionsResponse.StatusCode)
	}

	var actionPayload map[string][]string
	if err := json.NewDecoder(actionsResponse.Body).Decode(&actionPayload); err != nil {
		t.Fatalf("decode actions payload: %v", err)
	}
	actionsResponse.Body.Close()

	if len(actionPayload["actions"]) != 3 {
		t.Fatalf("unexpected actions payload: %+v", actionPayload)
	}

	for _, path := range []string{"/api/action/authorize", "/api/action/reconnect", "/api/action/export-diagnostics"} {
		request, err := http.NewRequest(http.MethodPost, server.URL+path, nil)
		if err != nil {
			t.Fatalf("build action request: %v", err)
		}

		actionResponse, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("do action request: %v", err)
		}

		if actionResponse.StatusCode != http.StatusAccepted {
			t.Fatalf("unexpected action status for %s: %d", path, actionResponse.StatusCode)
		}
		actionResponse.Body.Close()
	}

	if len(recordedActions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(recordedActions))
	}
}
