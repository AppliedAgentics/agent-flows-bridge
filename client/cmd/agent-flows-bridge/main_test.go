package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/binding"
	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
	"github.com/agentflows/agent-flows-bridge/client/internal/secrets"
	"github.com/agentflows/agent-flows-bridge/client/internal/wss"
	keyring "github.com/zalando/go-keyring"
)

func TestRunOAuthStartWritesAuthorizeURLAndPersistsPendingState(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-config", cfgPath, "-oauth-start-runtime-id", "77"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}

	authorizeURL, ok := payload["authorize_url"].(string)
	if !ok || !strings.Contains(authorizeURL, "/oauth/bridge/sign-in") {
		t.Fatalf("unexpected authorize_url payload: %+v", payload)
	}

	pending, err := oauth.LoadPendingStart(context.Background(), stateDir, testSecretStore(t, stateDir))
	if err != nil {
		t.Fatalf("load pending start: %v", err)
	}

	if pending.RuntimeID != 77 {
		t.Fatalf("expected runtime id 77, got %d", pending.RuntimeID)
	}
}

func TestRunRejectsDaemonAndOAuthModesTogether(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-config", cfgPath, "-run-daemon", "-oauth-start"}, stdout, stderr)
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d stderr=%s", exitCode, stderr.String())
	}

	if !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive error, got %s", stderr.String())
	}
}

func TestRunOAuthStartWithoutRuntimeIDPersistsPendingState(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-config", cfgPath, "-oauth-start"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}

	authorizeURL, ok := payload["authorize_url"].(string)
	if !ok || !strings.Contains(authorizeURL, "/oauth/bridge/sign-in") {
		t.Fatalf("unexpected authorize_url payload: %+v", payload)
	}

	parsedURL, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}

	if parsedURL.Query().Has("runtime_id") {
		t.Fatalf("expected runtime_id query to be omitted, got %q", parsedURL.Query().Get("runtime_id"))
	}

	if payload["runtime_id"] != nil {
		t.Fatalf("expected runtime_id to be null, got %+v", payload["runtime_id"])
	}

	pending, err := oauth.LoadPendingStart(context.Background(), stateDir, testSecretStore(t, stateDir))
	if err != nil {
		t.Fatalf("load pending start: %v", err)
	}

	if pending.RuntimeID != 0 {
		t.Fatalf("expected pending runtime id 0, got %d", pending.RuntimeID)
	}
}

func TestRunOAuthStartUsesSavedBindingForReconnectIntent(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	if err := binding.Save(stateDir, binding.RuntimeBinding{
		RuntimeID:   98,
		RuntimeKind: "local_connector",
		ConnectorID: 2,
		FlowID:      44,
	}); err != nil {
		t.Fatalf("save runtime binding: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-config", cfgPath, "-oauth-start"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}

	authorizeURL, ok := payload["authorize_url"].(string)
	if !ok || !strings.Contains(authorizeURL, "/oauth/bridge/sign-in") {
		t.Fatalf("unexpected authorize_url payload: %+v", payload)
	}

	parsedURL, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}

	if parsedURL.Query().Get("runtime_id") != "98" {
		t.Fatalf("expected runtime_id=98, got %q", parsedURL.Query().Get("runtime_id"))
	}

	if parsedURL.Query().Get("intent") != "reconnect" {
		t.Fatalf("expected intent=reconnect, got %q", parsedURL.Query().Get("intent"))
	}

	intent, ok := payload["intent"].(string)
	if !ok || intent != "reconnect" {
		t.Fatalf("expected intent=reconnect payload, got %+v", payload)
	}

	pending, err := oauth.LoadPendingStart(context.Background(), stateDir, testSecretStore(t, stateDir))
	if err != nil {
		t.Fatalf("load pending start: %v", err)
	}

	if pending.RuntimeID != 98 {
		t.Fatalf("expected pending runtime id 98, got %d", pending.RuntimeID)
	}
}

func TestRunOAuthStartRuntimeIDOverridesSavedBinding(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	if err := binding.Save(stateDir, binding.RuntimeBinding{
		RuntimeID:   98,
		RuntimeKind: "local_connector",
		ConnectorID: 2,
		FlowID:      44,
	}); err != nil {
		t.Fatalf("save runtime binding: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-config", cfgPath, "-oauth-start-runtime-id", "123"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}

	authorizeURL, ok := payload["authorize_url"].(string)
	if !ok || !strings.Contains(authorizeURL, "/oauth/bridge/sign-in") {
		t.Fatalf("unexpected authorize_url payload: %+v", payload)
	}

	parsedURL, err := url.Parse(authorizeURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}

	if parsedURL.Query().Get("runtime_id") != "123" {
		t.Fatalf("expected runtime_id=123, got %q", parsedURL.Query().Get("runtime_id"))
	}

	if parsedURL.Query().Has("intent") {
		t.Fatalf("expected intent to be omitted for explicit runtime, got %q", parsedURL.Query().Get("intent"))
	}
}

func TestRunOAuthCompleteExchangesTokenAndClearsPendingState(t *testing.T) {
	stateDir := t.TempDir()
	bootstrapCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/oauth/token":
			_, _ = fmt.Fprintf(w, `{"access_token":"at_123","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_123","scope":"connector:heartbeat connector:webhook","connector_id":55,"runtime":{"id":77,"runtime_kind":"local_connector"}}`)
		case "/api/connectors/bootstrap":
			bootstrapCalls++
			_, _ = fmt.Fprintf(w, `{"data":{"runtime":{"id":77,"runtime_kind":"local_connector","flow_id":42},"env":{"AGENT_FLOWS_API_URL":"https://agentflows.example.test","AGENT_FLOWS_API_KEY":"runtime_key_123"},"config":{"hooks":{"enabled":true}},"workspace_files":{"/data/openclaw/workspace":{"AGENTS.md":"content"}}},"meta":{"timestamp":"2026-03-04T00:00:00Z"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":"not_found"}`)
		}
	}))
	defer server.Close()

	cfgPath := writeConfigFile(t, stateDir, server.URL)

	pending := oauth.StartLoginResult{
		AuthorizeURL:  server.URL + "/oauth/authorize",
		State:         "state-123",
		CodeVerifier:  "verifier-123",
		RedirectURI:   "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:     77,
		OAuthClientID: "agent-flows-bridge",
	}

	if err := oauth.SavePendingStart(context.Background(), stateDir, testSecretStore(t, stateDir), pending); err != nil {
		t.Fatalf("save pending start: %v", err)
	}

	callbackURL := pending.RedirectURI + "?code=auth-code-123&state=" + pending.State

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-config", cfgPath, "-oauth-complete-callback-url", callbackURL}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var completePayload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &completePayload); err != nil {
		t.Fatalf("decode oauth complete payload: %v", err)
	}

	bootstrapReady, ok := completePayload["bootstrap_ready"].(bool)
	if !ok || !bootstrapReady {
		t.Fatalf("expected bootstrap_ready=true, got %+v", completePayload)
	}
	bootstrapApplied, ok := completePayload["bootstrap_applied"].(bool)
	if !ok || !bootstrapApplied {
		t.Fatalf("expected bootstrap_applied=true, got %+v", completePayload)
	}
	openClawConfigPath, ok := completePayload["openclaw_config_path"].(string)
	if !ok || strings.TrimSpace(openClawConfigPath) == "" {
		t.Fatalf("expected openclaw_config_path in payload, got %+v", completePayload)
	}
	if _, err := os.Stat(openClawConfigPath); err != nil {
		t.Fatalf("expected openclaw config file to exist: %v", err)
	}
	openClawEnvPath, ok := completePayload["openclaw_env_path"].(string)
	if !ok || strings.TrimSpace(openClawEnvPath) == "" {
		t.Fatalf("expected openclaw_env_path in payload, got %+v", completePayload)
	}
	if _, err := os.Stat(openClawEnvPath); err != nil {
		t.Fatalf("expected openclaw env file to exist: %v", err)
	}

	if bootstrapCalls != 1 {
		t.Fatalf("expected one bootstrap call, got %d", bootstrapCalls)
	}

	client := oauth.NewClient(oauth.Options{
		APIBaseURL:    server.URL,
		OAuthClientID: "agent-flows-bridge",
		SecretStore:   testSecretStore(t, stateDir),
	})
	session, err := client.LoadStoredSession(t.Context())
	if err != nil {
		t.Fatalf("load stored session: %v", err)
	}

	if session.RefreshToken != "rt_123" {
		t.Fatalf("unexpected stored refresh token: %s", session.RefreshToken)
	}

	_, err = oauth.LoadPendingStart(context.Background(), stateDir, testSecretStore(t, stateDir))
	if err == nil {
		t.Fatal("expected pending state to be cleared")
	}

	runtimeBinding, err := binding.Load(stateDir)
	if err != nil {
		t.Fatalf("load runtime binding: %v", err)
	}

	if runtimeBinding.RuntimeID != 77 {
		t.Fatalf("expected runtime id 77 in binding, got %d", runtimeBinding.RuntimeID)
	}

	if runtimeBinding.ConnectorID != 55 {
		t.Fatalf("expected connector id 55 in binding, got %d", runtimeBinding.ConnectorID)
	}

	if runtimeBinding.FlowID != 42 {
		t.Fatalf("expected flow id 42 in binding, got %d", runtimeBinding.FlowID)
	}
}

func TestRunOAuthCompleteRewritesCloudPathsInWrittenOpenClawConfig(t *testing.T) {
	stateDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/oauth/token":
			_, _ = fmt.Fprintf(w, `{"access_token":"at_123","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_123","scope":"connector:heartbeat connector:webhook","connector_id":55,"runtime":{"id":77,"runtime_kind":"local_connector"}}`)
		case "/api/connectors/bootstrap":
			_, _ = fmt.Fprintf(w, `{"data":{"runtime":{"id":77,"runtime_kind":"local_connector","flow_id":42},"env":{"AGENT_FLOWS_API_URL":"https://agentflows.example.test","AGENT_FLOWS_API_KEY":"runtime_key_123"},"config":{"agents":{"defaults":{"workspace":"/data/openclaw/workspace","memorySearch":{"store":{"path":"/data/openclaw/memory/main.sqlite"}}},"list":[{"id":"lead","workspace":"/data/openclaw/workspace"},{"id":"writer","workspace":"/data/openclaw/workspace-writer"},{"id":"social","workspace":"/data/openclaw/workspace-social"}]},"skills":{"load":{"extraDirs":["/data/openclaw/skills"]}}},"workspace_files":{"/data/openclaw/workspace":{"AGENTS.md":"content"}}},"meta":{"timestamp":"2026-03-04T00:00:00Z"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":"not_found"}`)
		}
	}))
	defer server.Close()

	cfgPath := writeConfigFile(t, stateDir, server.URL)

	pending := oauth.StartLoginResult{
		AuthorizeURL:  server.URL + "/oauth/authorize",
		State:         "state-123",
		CodeVerifier:  "verifier-123",
		RedirectURI:   "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:     77,
		OAuthClientID: "agent-flows-bridge",
	}

	if err := oauth.SavePendingStart(context.Background(), stateDir, testSecretStore(t, stateDir), pending); err != nil {
		t.Fatalf("save pending start: %v", err)
	}

	callbackURL := pending.RedirectURI + "?code=auth-code-123&state=" + pending.State

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{"-config", cfgPath, "-oauth-complete-callback-url", callbackURL}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode oauth complete payload: %v", err)
	}

	openClawConfigPath, ok := payload["openclaw_config_path"].(string)
	if !ok || strings.TrimSpace(openClawConfigPath) == "" {
		t.Fatalf("expected openclaw_config_path in payload, got %+v", payload)
	}

	configRaw, err := os.ReadFile(openClawConfigPath)
	if err != nil {
		t.Fatalf("read openclaw config: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(configRaw, &config); err != nil {
		t.Fatalf("decode openclaw config: %v", err)
	}

	openClawDataDir := filepath.Join(stateDir, "openclaw")

	agentsRoot, ok := config["agents"].(map[string]any)
	if !ok {
		t.Fatalf("expected agents config map, got %+v", config["agents"])
	}

	defaults, ok := agentsRoot["defaults"].(map[string]any)
	if !ok {
		t.Fatalf("expected agents.defaults map, got %+v", agentsRoot["defaults"])
	}

	defaultWorkspace, _ := defaults["workspace"].(string)
	if defaultWorkspace != filepath.Join(openClawDataDir, "workspace") {
		t.Fatalf("unexpected defaults workspace: %q", defaultWorkspace)
	}

	memorySearch, ok := defaults["memorySearch"].(map[string]any)
	if !ok {
		t.Fatalf("expected memorySearch map, got %+v", defaults["memorySearch"])
	}

	store, ok := memorySearch["store"].(map[string]any)
	if !ok {
		t.Fatalf("expected memorySearch.store map, got %+v", memorySearch["store"])
	}

	storePath, _ := store["path"].(string)
	if storePath != filepath.Join(openClawDataDir, "memory", "main.sqlite") {
		t.Fatalf("unexpected memory search store path: %q", storePath)
	}

	skillsRoot, ok := config["skills"].(map[string]any)
	if !ok {
		t.Fatalf("expected skills config map, got %+v", config["skills"])
	}

	loadRoot, ok := skillsRoot["load"].(map[string]any)
	if !ok {
		t.Fatalf("expected skills.load config map, got %+v", skillsRoot["load"])
	}

	extraDirs, ok := loadRoot["extraDirs"].([]any)
	if !ok || len(extraDirs) != 1 {
		t.Fatalf("expected skills.load.extraDirs with one value, got %+v", loadRoot["extraDirs"])
	}

	extraDir, _ := extraDirs[0].(string)
	if extraDir != filepath.Join(openClawDataDir, "skills") {
		t.Fatalf("unexpected skills extra dir: %q", extraDir)
	}
}

func TestRunUIServeModeStartsAndStopsAfterDuration(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{
		"-config", cfgPath,
		"-ui-serve",
		"-ui-listen", "127.0.0.1:0",
		"-ui-runtime-id", "77",
		"-ui-serve-duration", "20ms",
	}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	if !strings.Contains(stdout.String(), "ui server listening") {
		t.Fatalf("expected ui startup message, got stdout=%s", stdout.String())
	}
}

func TestRunInstallUserServiceInstallsArtifactsAndPrintsInstructions(t *testing.T) {
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	homeDir := filepath.Join(tempDir, "home")
	sourceBinaryPath := filepath.Join(tempDir, "agent-flows-bridge")

	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(sourceBinaryPath, []byte("#!/bin/sh\necho bridge\n"), 0o755); err != nil {
		t.Fatalf("write source binary: %v", err)
	}

	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{
		"-config", cfgPath,
		"-install-user-service",
		"-install-source-binary", sourceBinaryPath,
		"-install-home-dir", homeDir,
		"-install-goos", "linux",
	}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode install payload: %v", err)
	}

	installedBinaryPath, _ := payload["installed_binary_path"].(string)
	servicePath, _ := payload["service_path"].(string)
	commands, _ := payload["activation_commands"].([]any)

	if installedBinaryPath == "" || servicePath == "" {
		t.Fatalf("expected install paths in payload, got %+v", payload)
	}

	if _, err := os.Stat(installedBinaryPath); err != nil {
		t.Fatalf("expected installed binary to exist: %v", err)
	}

	if _, err := os.Stat(servicePath); err != nil {
		t.Fatalf("expected installed service file to exist: %v", err)
	}

	if len(commands) == 0 {
		t.Fatalf("expected activation commands, got none")
	}
}

func TestRunUninstallUserServiceRemovesManagedArtifacts(t *testing.T) {
	tempDir := t.TempDir()
	stateDir := filepath.Join(tempDir, "state")
	homeDir := filepath.Join(tempDir, "home")
	sourceBinaryPath := filepath.Join(tempDir, "agent-flows-bridge")

	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(sourceBinaryPath, []byte("#!/bin/sh\necho bridge\n"), 0o755); err != nil {
		t.Fatalf("write source binary: %v", err)
	}

	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	installStdout := &bytes.Buffer{}
	installStderr := &bytes.Buffer{}
	installExitCode := run([]string{
		"-config", cfgPath,
		"-install-user-service",
		"-install-source-binary", sourceBinaryPath,
		"-install-home-dir", homeDir,
		"-install-goos", "linux",
	}, installStdout, installStderr)
	if installExitCode != 0 {
		t.Fatalf("expected install exit code 0, got %d stderr=%s", installExitCode, installStderr.String())
	}

	uninstallStdout := &bytes.Buffer{}
	uninstallStderr := &bytes.Buffer{}
	uninstallExitCode := run([]string{
		"-config", cfgPath,
		"-uninstall-user-service",
		"-uninstall-home-dir", homeDir,
		"-uninstall-goos", "linux",
	}, uninstallStdout, uninstallStderr)
	if uninstallExitCode != 0 {
		t.Fatalf("expected uninstall exit code 0, got %d stderr=%s", uninstallExitCode, uninstallStderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(uninstallStdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode uninstall payload: %v", err)
	}

	if payload["binary_removed"] != true || payload["service_removed"] != true {
		t.Fatalf("expected removed binary/service payload, got %+v", payload)
	}

	if _, err := os.Stat(filepath.Join(stateDir, "bin", "agent-flows-bridge")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected installed binary removed, got %v", err)
	}
}

func TestRunOAuthSessionStatusWithoutStoredSessionReturnsDisconnectedPayload(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-config", cfgPath, "-oauth-session-status"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode oauth status payload: %v", err)
	}

	connected, ok := payload["connected"].(bool)
	if !ok {
		t.Fatalf("expected connected bool, got %+v", payload)
	}

	if connected {
		t.Fatalf("expected disconnected payload, got %+v", payload)
	}

	if payload["secrets_backend"] == nil {
		t.Fatalf("expected secrets_backend in payload, got %+v", payload)
	}
	if payload["bridge_version"] == nil {
		t.Fatalf("expected bridge_version in payload, got %+v", payload)
	}
}

func TestRunVersionPrintsBuildMetadata(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-version"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode version payload: %v", err)
	}

	if payload["version"] == nil || payload["commit"] == nil || payload["build_date"] == nil {
		t.Fatalf("expected version metadata payload, got %+v", payload)
	}
	if payload["goos"] != runtime.GOOS {
		t.Fatalf("expected goos %q, got %+v", runtime.GOOS, payload)
	}
}

func TestRunRuntimeBindingStatusReturnsUnboundWhenBindingMissing(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-config", cfgPath, "-runtime-binding-status"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode runtime binding status payload: %v", err)
	}

	bound, ok := payload["bound"].(bool)
	if !ok || bound {
		t.Fatalf("expected bound=false, got %+v", payload)
	}
}

func TestRunRuntimeBindingStatusReturnsBindingWhenPresent(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	if err := binding.Save(stateDir, binding.RuntimeBinding{
		RuntimeID:   98,
		RuntimeKind: "local_connector",
		ConnectorID: 2,
		FlowID:      44,
	}); err != nil {
		t.Fatalf("save runtime binding: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-config", cfgPath, "-runtime-binding-status"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode runtime binding status payload: %v", err)
	}

	bound, ok := payload["bound"].(bool)
	if !ok || !bound {
		t.Fatalf("expected bound=true, got %+v", payload)
	}

	runtimeID, ok := payload["runtime_id"].(float64)
	if !ok || int(runtimeID) != 98 {
		t.Fatalf("expected runtime_id=98, got %+v", payload)
	}
}

func TestRunRuntimeBindingClearRemovesBinding(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	if err := binding.Save(stateDir, binding.RuntimeBinding{
		RuntimeID:   98,
		RuntimeKind: "local_connector",
		ConnectorID: 2,
		FlowID:      44,
	}); err != nil {
		t.Fatalf("save runtime binding: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-config", cfgPath, "-runtime-binding-clear"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode runtime binding clear payload: %v", err)
	}

	cleared, ok := payload["cleared"].(bool)
	if !ok || !cleared {
		t.Fatalf("expected cleared=true, got %+v", payload)
	}

	_, err := binding.Load(stateDir)
	if !errors.Is(err, binding.ErrNotFound) {
		t.Fatalf("expected runtime binding removed, got %v", err)
	}
}

func TestRunDisconnectRuntimeRevokesConnector(t *testing.T) {
	stateDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/connectors/disconnect" {
			t.Fatalf("expected disconnect path, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer at_123" {
			t.Fatalf("unexpected authorization header: %s", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"data":{"revoked":true,"connector_id":55}}`)
	}))
	defer server.Close()

	cfgPath := writeConfigFile(t, stateDir, server.URL)

	store, err := secrets.NewStore(secrets.Options{StateDir: stateDir, PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	sessionRaw := mustJSON(t, map[string]any{
		"access_token":  "at_123",
		"refresh_token": "rt_123",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "connector:heartbeat connector:webhook",
		"connector_id":  55,
		"runtime_id":    98,
		"runtime_kind":  "local_connector",
		"issued_at":     time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	})
	if err := store.Save(t.Context(), "oauth_session", sessionRaw); err != nil {
		t.Fatalf("save oauth session: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{"-config", cfgPath, "-disconnect-runtime"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode disconnect payload: %v", err)
	}

	revoked, ok := payload["revoked"].(bool)
	if !ok || !revoked {
		t.Fatalf("expected revoked=true, got %+v", payload)
	}

	connectorID, ok := payload["connector_id"].(float64)
	if !ok || int(connectorID) != 55 {
		t.Fatalf("expected connector_id=55, got %+v", payload)
	}
}

func TestRunDisconnectRuntimeReturnsErrorWhenRequestFails(t *testing.T) {
	stateDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"error":"invalid_token"}`)
	}))
	defer server.Close()

	cfgPath := writeConfigFile(t, stateDir, server.URL)

	store, err := secrets.NewStore(secrets.Options{StateDir: stateDir, PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	sessionRaw := mustJSON(t, map[string]any{
		"access_token":  "at_123",
		"refresh_token": "rt_123",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "connector:heartbeat connector:webhook",
		"connector_id":  55,
		"runtime_id":    98,
		"runtime_kind":  "local_connector",
		"issued_at":     time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	})
	if err := store.Save(t.Context(), "oauth_session", sessionRaw); err != nil {
		t.Fatalf("save oauth session: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{"-config", cfgPath, "-disconnect-runtime"}, stdout, stderr)
	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}

	if !strings.Contains(stderr.String(), "disconnect runtime") {
		t.Fatalf("expected disconnect runtime stderr, got %s", stderr.String())
	}
}

func TestRunOAuthClearLocalStateRemovesStoredSecretsAndPendingState(t *testing.T) {
	stateDir := t.TempDir()
	cfgPath := writeConfigFile(t, stateDir, "https://saas.example.test")

	store, err := secrets.NewStore(secrets.Options{StateDir: stateDir, PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	sessionRaw := mustJSON(t, map[string]any{
		"access_token":  "at_123",
		"refresh_token": "rt_123",
		"token_type":    "Bearer",
		"expires_in":    3600,
		"scope":         "connector:heartbeat connector:webhook",
		"connector_id":  2,
		"runtime_id":    98,
		"runtime_kind":  "local_connector",
		"issued_at":     time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
	})
	if err := store.Save(t.Context(), "oauth_session", sessionRaw); err != nil {
		t.Fatalf("save oauth session: %v", err)
	}

	bootstrapRaw := []byte(`{"runtime":{"id":98,"runtime_kind":"local_connector","flow_id":44},"env":{"AGENT_FLOWS_API_URL":"https://agentflows.appliedagentics.ai","AGENT_FLOWS_API_KEY":"runtime_key_123"},"config":{"hooks":{"enabled":true}},"workspace_files":{"/data/openclaw/workspace":{"AGENTS.md":"content"}}}`)
	if err := store.Save(t.Context(), "connector_bootstrap_payload", bootstrapRaw); err != nil {
		t.Fatalf("save bootstrap payload: %v", err)
	}

	if err := oauth.SavePendingStart(context.Background(), stateDir, testSecretStore(t, stateDir), oauth.StartLoginResult{
		AuthorizeURL:  "https://agentflows.appliedagentics.ai/oauth/authorize",
		State:         "state-123",
		CodeVerifier:  "verifier-123",
		RedirectURI:   "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:     98,
		OAuthClientID: "agent-flows-bridge",
	}); err != nil {
		t.Fatalf("save pending start: %v", err)
	}

	openClawDir := filepath.Join(stateDir, "openclaw")
	if err := os.MkdirAll(openClawDir, 0o755); err != nil {
		t.Fatalf("mkdir openclaw dir: %v", err)
	}

	markerPath := filepath.Join(openClawDir, ".agent-flows-bridge-bootstrap.json")
	if err := os.WriteFile(markerPath, []byte(`{"runtime_id":98}`), 0o600); err != nil {
		t.Fatalf("write marker file: %v", err)
	}

	if err := binding.Save(stateDir, binding.RuntimeBinding{
		RuntimeID:   98,
		RuntimeKind: "local_connector",
		ConnectorID: 2,
		FlowID:      44,
	}); err != nil {
		t.Fatalf("save runtime binding: %v", err)
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	exitCode := run([]string{"-config", cfgPath, "-oauth-clear-local-state"}, stdout, stderr)
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode clear payload: %v", err)
	}
	cleared, ok := payload["cleared"].(bool)
	if !ok || !cleared {
		t.Fatalf("expected cleared=true, got %+v", payload)
	}

	_, err = store.Load(t.Context(), "oauth_session")
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("expected oauth_session secret removed, got %v", err)
	}

	_, err = store.Load(t.Context(), "connector_bootstrap_payload")
	if !errors.Is(err, secrets.ErrNotFound) {
		t.Fatalf("expected bootstrap payload secret removed, got %v", err)
	}

	_, err = oauth.LoadPendingStart(context.Background(), stateDir, testSecretStore(t, stateDir))
	if !errors.Is(err, oauth.ErrPendingStartNotFound) {
		t.Fatalf("expected pending oauth start removed, got %v", err)
	}

	if _, err := os.Stat(markerPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected marker file removed, got %v", err)
	}

	runtimeBinding, err := binding.Load(stateDir)
	if err != nil {
		t.Fatalf("expected runtime binding to be preserved, got %v", err)
	}

	if runtimeBinding.RuntimeID != 98 {
		t.Fatalf("expected runtime binding runtime id 98, got %d", runtimeBinding.RuntimeID)
	}
}

func TestRunOAuthSessionStatusIncludesBootstrapReadyWhenAvailable(t *testing.T) {
	stateDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/oauth/token":
			_, _ = fmt.Fprintf(w, `{"access_token":"at_123","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_123","scope":"connector:heartbeat connector:webhook","connector_id":55,"runtime":{"id":77,"runtime_kind":"local_connector"}}`)
		case "/api/connectors/bootstrap":
			_, _ = fmt.Fprintf(w, `{"data":{"runtime":{"id":77,"runtime_kind":"local_connector","flow_id":42},"env":{"AGENT_FLOWS_API_URL":"https://agentflows.example.test","AGENT_FLOWS_API_KEY":"runtime_key_123"},"config":{"hooks":{"enabled":true}},"workspace_files":{"/data/openclaw/workspace":{"AGENTS.md":"content"}}},"meta":{"timestamp":"2026-03-04T00:00:00Z"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":"not_found"}`)
		}
	}))
	defer server.Close()

	cfgPath := writeConfigFile(t, stateDir, server.URL)

	pending := oauth.StartLoginResult{
		AuthorizeURL:  server.URL + "/oauth/authorize",
		State:         "state-123",
		CodeVerifier:  "verifier-123",
		RedirectURI:   "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:     77,
		OAuthClientID: "agent-flows-bridge",
	}

	if err := oauth.SavePendingStart(context.Background(), stateDir, testSecretStore(t, stateDir), pending); err != nil {
		t.Fatalf("save pending start: %v", err)
	}

	callbackURL := pending.RedirectURI + "?code=auth-code-123&state=" + pending.State

	completeStdout := &bytes.Buffer{}
	completeStderr := &bytes.Buffer{}

	completeExitCode := run([]string{"-config", cfgPath, "-oauth-complete-callback-url", callbackURL}, completeStdout, completeStderr)
	if completeExitCode != 0 {
		t.Fatalf("expected complete exit code 0, got %d stderr=%s", completeExitCode, completeStderr.String())
	}

	statusStdout := &bytes.Buffer{}
	statusStderr := &bytes.Buffer{}

	statusExitCode := run([]string{"-config", cfgPath, "-oauth-session-status"}, statusStdout, statusStderr)
	if statusExitCode != 0 {
		t.Fatalf("expected status exit code 0, got %d stderr=%s", statusExitCode, statusStderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(statusStdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode oauth status payload: %v", err)
	}

	connected, ok := payload["connected"].(bool)
	if !ok || !connected {
		t.Fatalf("expected connected=true, got %+v", payload)
	}

	bootstrapReady, ok := payload["bootstrap_ready"].(bool)
	if !ok || !bootstrapReady {
		t.Fatalf("expected bootstrap_ready=true, got %+v", payload)
	}
	bootstrapApplied, ok := payload["bootstrap_applied"].(bool)
	if !ok || !bootstrapApplied {
		t.Fatalf("expected bootstrap_applied=true, got %+v", payload)
	}
}

func TestRunVerifyOpenClawReceiptReturnsAcceptedPayload(t *testing.T) {
	stateDir := t.TempDir()
	hookToken := "hook-token-probe-123"

	hookCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/oauth/token":
			_, _ = fmt.Fprintf(w, `{"access_token":"at_123","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_123","scope":"connector:heartbeat connector:webhook","connector_id":55,"runtime":{"id":77,"runtime_kind":"local_connector"}}`)
		case "/api/connectors/bootstrap":
			_, _ = fmt.Fprintf(w, `{"data":{"runtime":{"id":77,"runtime_kind":"local_connector","flow_id":42},"env":{"AGENT_FLOWS_API_URL":"https://agentflows.example.test","AGENT_FLOWS_API_KEY":"runtime_key_123"},"config":{"hooks":{"enabled":true,"token":%q,"path":"/hooks","allowedAgentIds":["lead"]}},"workspace_files":{"/data/openclaw/workspace":{"AGENTS.md":"content"}}},"meta":{"timestamp":"2026-03-04T00:00:00Z"}}`, hookToken)
		case "/hooks/agent":
			hookCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer "+hookToken {
				t.Fatalf("unexpected authorization header: %s", got)
			}
			_, _ = fmt.Fprintf(w, `{"runId":"run-probe-123"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":"not_found"}`)
		}
	}))
	defer server.Close()

	cfgPath := writeConfigFileWithRuntimeURL(t, stateDir, server.URL, server.URL)

	pending := oauth.StartLoginResult{
		AuthorizeURL:  server.URL + "/oauth/authorize",
		State:         "state-123",
		CodeVerifier:  "verifier-123",
		RedirectURI:   "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:     77,
		OAuthClientID: "agent-flows-bridge",
	}

	if err := oauth.SavePendingStart(context.Background(), stateDir, testSecretStore(t, stateDir), pending); err != nil {
		t.Fatalf("save pending start: %v", err)
	}

	callbackURL := pending.RedirectURI + "?code=auth-code-123&state=" + pending.State

	completeStdout := &bytes.Buffer{}
	completeStderr := &bytes.Buffer{}
	completeExitCode := run([]string{"-config", cfgPath, "-oauth-complete-callback-url", callbackURL}, completeStdout, completeStderr)
	if completeExitCode != 0 {
		t.Fatalf("expected complete exit code 0, got %d stderr=%s", completeExitCode, completeStderr.String())
	}

	verifyStdout := &bytes.Buffer{}
	verifyStderr := &bytes.Buffer{}
	verifyExitCode := run([]string{"-config", cfgPath, "-verify-openclaw-receipt"}, verifyStdout, verifyStderr)
	if verifyExitCode != 0 {
		t.Fatalf("expected verify exit code 0, got %d stderr=%s", verifyExitCode, verifyStderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(verifyStdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode verify payload: %v", err)
	}

	accepted, ok := payload["accepted"].(bool)
	if !ok || !accepted {
		t.Fatalf("expected accepted=true, got %+v", payload)
	}

	httpStatus, ok := payload["http_status"].(float64)
	if !ok || int(httpStatus) != 200 {
		t.Fatalf("expected http_status=200, got %+v", payload)
	}

	tokenMatchesBootstrap, ok := payload["token_matches_bootstrap"].(bool)
	if !ok || !tokenMatchesBootstrap {
		t.Fatalf("expected token_matches_bootstrap=true, got %+v", payload)
	}

	if hookCalls != 1 {
		t.Fatalf("expected one hook probe call, got %d", hookCalls)
	}
}

func TestRunVerifyOpenClawReceiptFailsWhenOpenClawRejectsProbe(t *testing.T) {
	stateDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, "Unauthorized")
	}))
	defer server.Close()

	cfgPath := writeConfigFileWithRuntimeURL(t, stateDir, "https://saas.example.test", server.URL)

	openClawDir := filepath.Join(stateDir, "openclaw")
	if err := os.MkdirAll(openClawDir, 0o755); err != nil {
		t.Fatalf("mkdir openclaw dir: %v", err)
	}

	openClawConfig := `{
  "hooks": {
    "token": "hook-token-abc",
    "path": "/hooks",
    "allowedAgentIds": ["lead"]
  }
}`
	if err := os.WriteFile(filepath.Join(openClawDir, "openclaw.json"), []byte(openClawConfig), 0o600); err != nil {
		t.Fatalf("write openclaw config: %v", err)
	}

	verifyStdout := &bytes.Buffer{}
	verifyStderr := &bytes.Buffer{}
	verifyExitCode := run([]string{"-config", cfgPath, "-verify-openclaw-receipt"}, verifyStdout, verifyStderr)
	if verifyExitCode != 1 {
		t.Fatalf("expected verify exit code 1, got %d stderr=%s", verifyExitCode, verifyStderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(verifyStdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode verify payload: %v", err)
	}

	accepted, ok := payload["accepted"].(bool)
	if !ok || accepted {
		t.Fatalf("expected accepted=false, got %+v", payload)
	}

	httpStatus, ok := payload["http_status"].(float64)
	if !ok || int(httpStatus) != 401 {
		t.Fatalf("expected http_status=401, got %+v", payload)
	}
}

func TestRunProcessWebhookOnceClaimsDeliversAndReportsResult(t *testing.T) {
	stateDir := t.TempDir()
	hookToken := "hook-token-probe-123"

	claimCalls := 0
	hooksCalls := 0
	resultCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/oauth/token":
			_, _ = fmt.Fprintf(w, `{"access_token":"at_123","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_123","scope":"connector:heartbeat connector:webhook","connector_id":55,"runtime":{"id":77,"runtime_kind":"local_connector"}}`)
		case "/api/connectors/bootstrap":
			_, _ = fmt.Fprintf(w, `{"data":{"runtime":{"id":77,"runtime_kind":"local_connector","flow_id":42},"env":{"AGENT_FLOWS_API_URL":"https://agentflows.example.test","AGENT_FLOWS_API_KEY":"runtime_key_123"},"config":{"hooks":{"enabled":true,"token":%q,"path":"/hooks","allowedAgentIds":["lead"]}},"workspace_files":{"/data/openclaw/workspace":{"AGENTS.md":"content"}}},"meta":{"timestamp":"2026-03-04T00:00:00Z"}}`, hookToken)
		case "/api/connectors/webhooks/claim":
			claimCalls++
			_, _ = fmt.Fprintf(w, `{"data":{"event":{"event_id":"wev_123","runtime_id":77,"task_id":42,"agent_id":"writer","event_type":"mentioned","payload":{"message":"[AgentFlows:mentioned] task_id=42","name":"AgentFlows","agentId":"writer","sessionKey":"hook:task:42","wakeMode":"now","deliver":false,"timeoutSeconds":180},"attempts":1}},"meta":{"timestamp":"2026-03-04T00:02:00Z"}}`)
		case "/hooks/agent":
			hooksCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer "+hookToken {
				t.Fatalf("unexpected authorization header: %s", got)
			}
			_, _ = fmt.Fprintf(w, `{"runId":"run-local-123"}`)
		case "/api/connectors/webhooks/result":
			resultCalls++

			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode result payload: %v", err)
			}

			if payload["event_id"] != "wev_123" {
				t.Fatalf("unexpected event id payload: %+v", payload)
			}
			if payload["outcome"] != "delivered" {
				t.Fatalf("unexpected outcome payload: %+v", payload)
			}

			_, _ = fmt.Fprintf(w, `{"data":{"event_id":"wev_123","status":"delivered","run_id":"run-local-123"},"meta":{"timestamp":"2026-03-04T00:03:00Z"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":"not_found"}`)
		}
	}))
	defer server.Close()

	cfgPath := writeConfigFileWithRuntimeURL(t, stateDir, server.URL, server.URL)

	pending := oauth.StartLoginResult{
		AuthorizeURL:  server.URL + "/oauth/authorize",
		State:         "state-123",
		CodeVerifier:  "verifier-123",
		RedirectURI:   "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:     77,
		OAuthClientID: "agent-flows-bridge",
	}

	if err := oauth.SavePendingStart(context.Background(), stateDir, testSecretStore(t, stateDir), pending); err != nil {
		t.Fatalf("save pending start: %v", err)
	}

	callbackURL := pending.RedirectURI + "?code=auth-code-123&state=" + pending.State

	completeStdout := &bytes.Buffer{}
	completeStderr := &bytes.Buffer{}
	completeExitCode := run([]string{"-config", cfgPath, "-oauth-complete-callback-url", callbackURL}, completeStdout, completeStderr)
	if completeExitCode != 0 {
		t.Fatalf("expected complete exit code 0, got %d stderr=%s", completeExitCode, completeStderr.String())
	}

	processStdout := &bytes.Buffer{}
	processStderr := &bytes.Buffer{}
	processExitCode := run([]string{"-config", cfgPath, "-process-webhook-once"}, processStdout, processStderr)
	if processExitCode != 0 {
		t.Fatalf("expected process exit code 0, got %d stderr=%s", processExitCode, processStderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(processStdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode process payload: %v", err)
	}

	processed, ok := payload["processed"].(bool)
	if !ok || !processed {
		t.Fatalf("expected processed=true, got %+v", payload)
	}

	outcome, ok := payload["outcome"].(string)
	if !ok || outcome != "delivered" {
		t.Fatalf("expected delivered outcome, got %+v", payload)
	}

	if claimCalls != 1 {
		t.Fatalf("expected one claim call, got %d", claimCalls)
	}
	if hooksCalls != 1 {
		t.Fatalf("expected one hook delivery call, got %d", hooksCalls)
	}
	if resultCalls != 1 {
		t.Fatalf("expected one result call, got %d", resultCalls)
	}
}

func TestRunProcessWebhookOnceReturnsNoEventWhenClaimIsEmpty(t *testing.T) {
	stateDir := t.TempDir()
	hookToken := "hook-token-probe-123"

	claimCalls := 0
	resultCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/oauth/token":
			_, _ = fmt.Fprintf(w, `{"access_token":"at_123","token_type":"Bearer","expires_in":3600,"refresh_token":"rt_123","scope":"connector:heartbeat connector:webhook","connector_id":55,"runtime":{"id":77,"runtime_kind":"local_connector"}}`)
		case "/api/connectors/bootstrap":
			_, _ = fmt.Fprintf(w, `{"data":{"runtime":{"id":77,"runtime_kind":"local_connector","flow_id":42},"env":{"AGENT_FLOWS_API_URL":"https://agentflows.example.test","AGENT_FLOWS_API_KEY":"runtime_key_123"},"config":{"hooks":{"enabled":true,"token":%q,"path":"/hooks","allowedAgentIds":["lead"]}},"workspace_files":{"/data/openclaw/workspace":{"AGENTS.md":"content"}}},"meta":{"timestamp":"2026-03-04T00:00:00Z"}}`, hookToken)
		case "/api/connectors/webhooks/claim":
			claimCalls++
			_, _ = fmt.Fprintf(w, `{"data":{"event":null},"meta":{"timestamp":"2026-03-04T00:02:00Z"}}`)
		case "/api/connectors/webhooks/result":
			resultCalls++
			_, _ = fmt.Fprintf(w, `{"data":{"event_id":"wev_123","status":"delivered","run_id":"run-local-123"},"meta":{"timestamp":"2026-03-04T00:03:00Z"}}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":"not_found"}`)
		}
	}))
	defer server.Close()

	cfgPath := writeConfigFileWithRuntimeURL(t, stateDir, server.URL, server.URL)

	pending := oauth.StartLoginResult{
		AuthorizeURL:  server.URL + "/oauth/authorize",
		State:         "state-123",
		CodeVerifier:  "verifier-123",
		RedirectURI:   "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:     77,
		OAuthClientID: "agent-flows-bridge",
	}

	if err := oauth.SavePendingStart(context.Background(), stateDir, testSecretStore(t, stateDir), pending); err != nil {
		t.Fatalf("save pending start: %v", err)
	}

	callbackURL := pending.RedirectURI + "?code=auth-code-123&state=" + pending.State

	completeStdout := &bytes.Buffer{}
	completeStderr := &bytes.Buffer{}
	completeExitCode := run([]string{"-config", cfgPath, "-oauth-complete-callback-url", callbackURL}, completeStdout, completeStderr)
	if completeExitCode != 0 {
		t.Fatalf("expected complete exit code 0, got %d stderr=%s", completeExitCode, completeStderr.String())
	}

	processStdout := &bytes.Buffer{}
	processStderr := &bytes.Buffer{}
	processExitCode := run([]string{"-config", cfgPath, "-process-webhook-once"}, processStdout, processStderr)
	if processExitCode != 0 {
		t.Fatalf("expected process exit code 0, got %d stderr=%s", processExitCode, processStderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(processStdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode process payload: %v", err)
	}

	processed, ok := payload["processed"].(bool)
	if !ok || processed {
		t.Fatalf("expected processed=false, got %+v", payload)
	}

	if claimCalls != 1 {
		t.Fatalf("expected one claim call, got %d", claimCalls)
	}
	if resultCalls != 0 {
		t.Fatalf("expected no result calls, got %d", resultCalls)
	}
}

func TestRefreshingWSSDialerRefreshesTokenAfterUnauthorizedDial(t *testing.T) {
	baseDialer := &fakeManagerDialer{
		results: []fakeManagerDialResult{
			{err: &wss.HTTPDialError{StatusCode: http.StatusUnauthorized, Body: `{"error":"INVALID_CONNECTOR_TOKEN"}`}},
			{connection: &fakeManagerConnection{}},
		},
	}

	refresher := &fakeConnectorSessionRefresher{
		session: oauth.Session{AccessToken: "access-token-refreshed"},
	}

	dialer := &refreshingWSSDialer{
		baseDialer:       baseDialer,
		sessionRefresher: refresher,
		accessToken:      "access-token-initial",
	}

	connection, err := dialer.Dial(context.Background(), "wss://agentflows.example.test/socket", "")
	if err != nil {
		t.Fatalf("dial with refresh: %v", err)
	}
	if connection == nil {
		t.Fatal("expected connection after refresh")
	}

	if len(baseDialer.tokens) != 2 {
		t.Fatalf("expected 2 dial attempts, got %d", len(baseDialer.tokens))
	}
	if baseDialer.tokens[0] != "access-token-initial" {
		t.Fatalf("unexpected first token %q", baseDialer.tokens[0])
	}
	if baseDialer.tokens[1] != "access-token-refreshed" {
		t.Fatalf("unexpected second token %q", baseDialer.tokens[1])
	}
	if refresher.calls != 1 {
		t.Fatalf("expected one refresh call, got %d", refresher.calls)
	}
}

func TestRefreshingWSSDialerReturnsErrorWhenRefreshFails(t *testing.T) {
	baseDialer := &fakeManagerDialer{
		results: []fakeManagerDialResult{
			{err: &wss.HTTPDialError{StatusCode: http.StatusUnauthorized}},
		},
	}

	refresher := &fakeConnectorSessionRefresher{
		err: errors.New("refresh failed"),
	}

	dialer := &refreshingWSSDialer{
		baseDialer:       baseDialer,
		sessionRefresher: refresher,
		accessToken:      "access-token-initial",
	}

	_, err := dialer.Dial(context.Background(), "wss://agentflows.example.test/socket", "")
	if err == nil {
		t.Fatal("expected refresh failure error")
	}
	if !strings.Contains(err.Error(), "refresh connector session after unauthorized dial") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRefreshingWSSDialerDoesNotRefreshForNonUnauthorizedDialError(t *testing.T) {
	expectedErr := errors.New("network unavailable")

	baseDialer := &fakeManagerDialer{
		results: []fakeManagerDialResult{
			{err: expectedErr},
		},
	}

	refresher := &fakeConnectorSessionRefresher{
		session: oauth.Session{AccessToken: "access-token-refreshed"},
	}

	dialer := &refreshingWSSDialer{
		baseDialer:       baseDialer,
		sessionRefresher: refresher,
		accessToken:      "access-token-initial",
	}

	_, err := dialer.Dial(context.Background(), "wss://agentflows.example.test/socket", "")
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected original error, got %v", err)
	}
	if refresher.calls != 0 {
		t.Fatalf("expected no refresh calls, got %d", refresher.calls)
	}
}

func TestStartSessionRefreshWorkerUpdatesWSSDialerToken(t *testing.T) {
	refresher := &fakeConnectorSessionRefresher{
		storedSession: oauth.Session{
			AccessToken:  "access-token-initial",
			RefreshToken: "refresh-token-initial",
			ExpiresIn:    3600,
			IssuedAt:     time.Now().UTC().Add(-59 * time.Minute),
		},
		session: oauth.Session{
			AccessToken:  "access-token-refreshed",
			RefreshToken: "refresh-token-refreshed",
			ExpiresIn:    3600,
			IssuedAt:     time.Now().UTC(),
		},
	}

	dialer := &refreshingWSSDialer{accessToken: "access-token-initial"}
	stderr := &bytes.Buffer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	startSessionRefreshWorker(ctx, refresher, stderr, func(refreshedSession oauth.Session) {
		dialer.setAccessToken(refreshedSession.AccessToken)
		cancel()
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if dialer.currentAccessToken() == "access-token-refreshed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if dialer.currentAccessToken() != "access-token-refreshed" {
		t.Fatalf("expected refreshed access token, got %q stderr=%s", dialer.currentAccessToken(), stderr.String())
	}
	if refresher.loadCalls == 0 {
		t.Fatal("expected refresh worker to load stored session")
	}
	if refresher.calls == 0 {
		t.Fatal("expected refresh worker to refresh session")
	}
}

func TestSelectOAuthRedirectPortUsesNextAvailablePort(t *testing.T) {
	listenerOne, err := net.Listen("tcp", "127.0.0.1:49200")
	if err != nil {
		t.Fatalf("occupy first redirect port: %v", err)
	}
	defer listenerOne.Close()

	listenerTwo, err := net.Listen("tcp", "127.0.0.1:49201")
	if err != nil {
		t.Fatalf("occupy second redirect port: %v", err)
	}
	defer listenerTwo.Close()

	port, err := selectOAuthRedirectPort()
	if err != nil {
		t.Fatalf("select oauth redirect port: %v", err)
	}

	if port != 49202 {
		t.Fatalf("expected port 49202, got %d", port)
	}
}

func writeConfigFile(t *testing.T, stateDir string, apiBaseURL string) string {
	t.Helper()
	return writeConfigFileWithRuntimeURL(t, stateDir, apiBaseURL, "http://127.0.0.1:18789")
}

func testSecretStore(t *testing.T, stateDir string) secrets.Store {
	t.Helper()

	store, err := secrets.NewStore(secrets.Options{StateDir: stateDir})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	return store
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}

	return encoded
}

type fakeManagerDialResult struct {
	connection wss.Connection
	err        error
}

type fakeManagerDialer struct {
	results []fakeManagerDialResult
	tokens  []string
	index   int
}

func (f *fakeManagerDialer) Dial(_ context.Context, _ string, token string) (wss.Connection, error) {
	f.tokens = append(f.tokens, token)

	if f.index >= len(f.results) {
		return nil, errors.New("unexpected dial call")
	}

	result := f.results[f.index]
	f.index++

	if result.err != nil {
		return nil, result.err
	}

	return result.connection, nil
}

type fakeManagerConnection struct{}

func (f *fakeManagerConnection) Read(ctx context.Context) error {
	return ctx.Err()
}

func (f *fakeManagerConnection) Close() error {
	return nil
}

type fakeConnectorSessionRefresher struct {
	storedSession oauth.Session
	session       oauth.Session
	err           error
	calls         int
	loadCalls     int
	loadErr       error
}

func (f *fakeConnectorSessionRefresher) LoadStoredSession(_ context.Context) (oauth.Session, error) {
	f.loadCalls++
	if f.loadErr != nil {
		return oauth.Session{}, f.loadErr
	}
	if f.storedSession.AccessToken != "" {
		return f.storedSession, nil
	}
	return f.session, nil
}

func (f *fakeConnectorSessionRefresher) RefreshSession(_ context.Context) (oauth.Session, error) {
	f.calls++
	if f.err != nil {
		return oauth.Session{}, f.err
	}
	return f.session, nil
}

func writeConfigFileWithRuntimeURL(t *testing.T, stateDir string, apiBaseURL string, runtimeURL string) string {
	t.Helper()
	keyring.MockInit()

	cfg := fmt.Sprintf(`{
  "api_base_url": %q,
  "runtime_url": %q,
  "state_dir": %q,
  "openclaw_data_dir": %q,
  "log_level": "info",
  "oauth_client_id": "agent-flows-bridge"
}`,
		apiBaseURL,
		runtimeURL,
		stateDir,
		filepath.Join(stateDir, "openclaw"),
	)

	cfgPath := filepath.Join(t.TempDir(), "bridge.json")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	return cfgPath
}
