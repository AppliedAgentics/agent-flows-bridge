package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agentflows/agent-flows-bridge/client/internal/diagnostics"
	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
	"github.com/agentflows/agent-flows-bridge/client/internal/secrets"
	keyring "github.com/zalando/go-keyring"
)

func TestAuthorizeActionStartsOAuthAndPersistsPendingState(t *testing.T) {
	keyring.MockInit()

	pendingStateDir := t.TempDir()
	shell := NewShell(Options{InitialState: State{Status: StateNotConfigured}})

	starter := &fakeAuthStarter{start: oauth.StartLoginResult{
		AuthorizeURL:  "https://saas.example.test/oauth/authorize?x=1",
		State:         "state-123",
		CodeVerifier:  "verifier-123",
		RedirectURI:   "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:     77,
		OAuthClientID: "agent-flows-bridge",
	}}

	launchedURL := ""
	controller := NewController(ControllerOptions{
		Shell:           shell,
		RuntimeID:       77,
		AuthStarter:     starter,
		PendingState:    starter,
		PendingStateDir: pendingStateDir,
		OnAuthorizeURL: func(authorizeURL string) {
			launchedURL = authorizeURL
		},
	})

	controller.HandleAction(ActionAuthorize)

	if starter.calls != 1 {
		t.Fatalf("expected one start call, got %d", starter.calls)
	}
	if launchedURL == "" {
		t.Fatal("expected authorize URL callback")
	}

	state := shell.State()
	if state.Status != StateAuthorizing {
		t.Fatalf("expected authorizing, got %s", state.Status)
	}
	if state.ErrorCode != "" {
		t.Fatalf("expected no error_code, got %s", state.ErrorCode)
	}

	pending, err := oauth.LoadPendingStart(context.Background(), pendingStateDir, newTestSecretStore(pendingStateDir))
	if err != nil {
		t.Fatalf("load pending start: %v", err)
	}
	if pending.State != "state-123" {
		t.Fatalf("unexpected pending state: %s", pending.State)
	}
}

func TestAuthorizeActionFailureMovesStateToDegraded(t *testing.T) {
	shell := NewShell(Options{InitialState: State{Status: StateNotConfigured}})

	controller := NewController(ControllerOptions{
		Shell:       shell,
		RuntimeID:   77,
		AuthStarter: &fakeAuthStarter{err: errors.New("boom")},
	})

	controller.HandleAction(ActionAuthorize)

	state := shell.State()
	if state.Status != StateDegraded {
		t.Fatalf("expected degraded, got %s", state.Status)
	}
	if state.ErrorCode != "AUTHORIZE_FAILED" {
		t.Fatalf("unexpected error code: %s", state.ErrorCode)
	}
}

func TestReconnectActionUsesStoredSession(t *testing.T) {
	shell := NewShell(Options{InitialState: State{Status: StateDisconnected}})

	loader := &fakeSessionLoader{session: oauth.Session{RuntimeID: 88, RuntimeKind: "local_connector"}}
	controller := NewController(ControllerOptions{Shell: shell, SessionLoader: loader})

	controller.HandleAction(ActionReconnect)

	state := shell.State()
	if state.Status != StateConnected {
		t.Fatalf("expected connected, got %s", state.Status)
	}
	if state.RuntimeID != 88 {
		t.Fatalf("unexpected runtime id: %d", state.RuntimeID)
	}
}

func TestExportDiagnosticsActionStoresInfoMessage(t *testing.T) {
	shell := NewShell(Options{InitialState: State{Status: StateConnected, RuntimeID: 77}})

	exporter := &fakeExporter{bundlePath: "/tmp/diag-bundle-123"}
	controller := NewController(ControllerOptions{
		Shell:    shell,
		Exporter: exporter,
		DiagnosticsMetadata: map[string]any{
			"bridge_version":  "1.2.3",
			"secrets_backend": "keychain",
		},
	})

	controller.HandleAction(ActionExportDiagnostics)

	if exporter.calls != 1 {
		t.Fatalf("expected one export call, got %d", exporter.calls)
	}
	if exporter.lastInput.Metadata["bridge_version"] != "1.2.3" {
		t.Fatalf("expected bridge_version metadata, got %+v", exporter.lastInput.Metadata)
	}
	if exporter.lastInput.Metadata["secrets_backend"] != "keychain" {
		t.Fatalf("expected secrets_backend metadata, got %+v", exporter.lastInput.Metadata)
	}

	state := shell.State()
	if !strings.Contains(state.InfoMessage, "/tmp/diag-bundle-123") {
		t.Fatalf("expected info message to include bundle path, got %s", state.InfoMessage)
	}
	if state.ErrorCode != "" {
		t.Fatalf("expected empty error code, got %s", state.ErrorCode)
	}
}

type fakeAuthStarter struct {
	start oauth.StartLoginResult
	err   error
	calls int
}

func (f *fakeAuthStarter) StartLogin(runtimeID int) (oauth.StartLoginResult, error) {
	f.calls++
	if f.err != nil {
		return oauth.StartLoginResult{}, f.err
	}
	result := f.start
	result.RuntimeID = runtimeID
	return result, nil
}

func (f *fakeAuthStarter) SavePendingStart(
	_ context.Context,
	stateDir string,
	start oauth.StartLoginResult,
) error {
	return oauth.SavePendingStart(context.Background(), stateDir, newTestSecretStore(stateDir), start)
}

type fakeSessionLoader struct {
	session oauth.Session
	err     error
}

func (f *fakeSessionLoader) LoadStoredSession(_ context.Context) (oauth.Session, error) {
	if f.err != nil {
		return oauth.Session{}, f.err
	}
	return f.session, nil
}

type fakeExporter struct {
	bundlePath string
	err        error
	calls      int
	lastInput  diagnostics.BundleInput
}

func (f *fakeExporter) Export(input diagnostics.BundleInput) (string, error) {
	f.calls++
	f.lastInput = input
	if f.err != nil {
		return "", f.err
	}
	return f.bundlePath, nil
}

func newTestSecretStore(stateDir string) secrets.Store {
	store, err := secrets.NewStore(secrets.Options{StateDir: stateDir})
	if err != nil {
		panic(err)
	}
	return store
}
