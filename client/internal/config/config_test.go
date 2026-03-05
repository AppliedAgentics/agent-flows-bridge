package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	homeDir := t.TempDir()
	cfg, err := load("", map[string]string{}, homeDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.APIBaseURL != "https://agentflows.appliedagentics.ai" {
		t.Fatalf("unexpected APIBaseURL: %s", cfg.APIBaseURL)
	}
	if cfg.RuntimeURL != "http://127.0.0.1:18789" {
		t.Fatalf("unexpected RuntimeURL: %s", cfg.RuntimeURL)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("unexpected LogLevel: %s", cfg.LogLevel)
	}
	if cfg.OAuthClientID != "agent-flows-bridge" {
		t.Fatalf("unexpected OAuthClientID: %s", cfg.OAuthClientID)
	}
	if cfg.TransportMode != "auto" {
		t.Fatalf("unexpected TransportMode: %s", cfg.TransportMode)
	}

	expectedStateDir := filepath.Join(homeDir, ".agent-flows-bridge")
	if cfg.StateDir != expectedStateDir {
		t.Fatalf("unexpected StateDir: %s", cfg.StateDir)
	}

	expectedOpenClawDataDir := filepath.Join(homeDir, ".openclaw")
	if cfg.OpenClawDataDir != expectedOpenClawDataDir {
		t.Fatalf("unexpected OpenClawDataDir: %s", cfg.OpenClawDataDir)
	}
}

func TestLoadMigratesLegacyPlaceholderAPIBaseURL(t *testing.T) {
	homeDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "bridge.json")

	content := `{
  "api_base_url": "https://agentflows.example.com",
  "runtime_url": "http://127.0.0.1:18789",
  "log_level": "info"
}`

	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg, err := load(cfgPath, map[string]string{}, homeDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.APIBaseURL != "https://agentflows.appliedagentics.ai" {
		t.Fatalf("expected production api base url, got %s", cfg.APIBaseURL)
	}
}

func TestLoadFileOverridesDefaults(t *testing.T) {
	homeDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "bridge.json")

	content := `{
  "api_base_url": "https://saas.example.test",
  "runtime_url": "http://127.0.0.1:27789",
  "log_level": "debug",
  "state_dir": "/tmp/af-bridge-state",
  "openclaw_data_dir": "/tmp/openclaw-from-file",
  "oauth_client_id": "agent-flows-bridge",
  "transport_mode": "wss"
}`

	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg, err := load(cfgPath, map[string]string{}, homeDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.APIBaseURL != "https://saas.example.test" {
		t.Fatalf("unexpected APIBaseURL: %s", cfg.APIBaseURL)
	}
	if cfg.RuntimeURL != "http://127.0.0.1:27789" {
		t.Fatalf("unexpected RuntimeURL: %s", cfg.RuntimeURL)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("unexpected LogLevel: %s", cfg.LogLevel)
	}
	if cfg.StateDir != "/tmp/af-bridge-state" {
		t.Fatalf("unexpected StateDir: %s", cfg.StateDir)
	}
	if cfg.OpenClawDataDir != "/tmp/openclaw-from-file" {
		t.Fatalf("unexpected OpenClawDataDir: %s", cfg.OpenClawDataDir)
	}
	if cfg.TransportMode != "wss" {
		t.Fatalf("unexpected TransportMode: %s", cfg.TransportMode)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	homeDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "bridge.json")

	content := `{
  "api_base_url": "https://from-file.example.test",
  "runtime_url": "http://127.0.0.1:28789",
  "log_level": "warn",
  "state_dir": "/tmp/from-file",
  "openclaw_data_dir": "/tmp/openclaw-from-file",
  "oauth_client_id": "agent-flows-bridge",
  "transport_mode": "wss"
}`

	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	env := map[string]string{
		"AFB_API_BASE_URL":      "https://from-env.example.test",
		"AFB_RUNTIME_URL":       "http://127.0.0.1:29789",
		"AFB_LOG_LEVEL":         "error",
		"AFB_STATE_DIR":         "/tmp/from-env",
		"AFB_OPENCLAW_DATA_DIR": "/tmp/openclaw-from-env",
		"AFB_OAUTH_CLIENT_ID":   "agent-flows-bridge-env",
		"AFB_TRANSPORT_MODE":    "poll",
	}

	cfg, err := load(cfgPath, env, homeDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.APIBaseURL != "https://from-env.example.test" {
		t.Fatalf("unexpected APIBaseURL: %s", cfg.APIBaseURL)
	}
	if cfg.RuntimeURL != "http://127.0.0.1:29789" {
		t.Fatalf("unexpected RuntimeURL: %s", cfg.RuntimeURL)
	}
	if cfg.LogLevel != "error" {
		t.Fatalf("unexpected LogLevel: %s", cfg.LogLevel)
	}
	if cfg.StateDir != "/tmp/from-env" {
		t.Fatalf("unexpected StateDir: %s", cfg.StateDir)
	}
	if cfg.OAuthClientID != "agent-flows-bridge-env" {
		t.Fatalf("unexpected OAuthClientID: %s", cfg.OAuthClientID)
	}
	if cfg.OpenClawDataDir != "/tmp/openclaw-from-env" {
		t.Fatalf("unexpected OpenClawDataDir: %s", cfg.OpenClawDataDir)
	}
	if cfg.TransportMode != "poll" {
		t.Fatalf("unexpected TransportMode: %s", cfg.TransportMode)
	}
}

func TestLoadRejectsInvalidConfig(t *testing.T) {
	homeDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "bridge.json")

	content := `{
  "api_base_url": "not-a-url",
  "runtime_url": "http://127.0.0.1:18789",
  "log_level": "info"
}`

	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := load(cfgPath, map[string]string{}, homeDir)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoadRejectsInvalidTransportMode(t *testing.T) {
	homeDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "bridge.json")

	content := `{
  "api_base_url": "https://valid.example.test",
  "runtime_url": "http://127.0.0.1:18789",
  "log_level": "info",
  "transport_mode": "invalid"
}`

	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := load(cfgPath, map[string]string{}, homeDir)
	if err == nil {
		t.Fatal("expected transport mode validation error")
	}
}

func TestLoadRejectsNonLoopbackHTTPAPIBaseURLWithoutOverride(t *testing.T) {
	homeDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "bridge.json")

	content := `{
  "api_base_url": "http://agentflows.appliedagentics.ai",
  "runtime_url": "http://127.0.0.1:18789",
  "log_level": "info"
}`

	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := load(cfgPath, map[string]string{}, homeDir)
	if err == nil {
		t.Fatal("expected insecure api base url validation error")
	}
}

func TestLoadAllowsLoopbackHTTPAPIBaseURL(t *testing.T) {
	homeDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "bridge.json")

	content := `{
  "api_base_url": "http://127.0.0.1:4000",
  "runtime_url": "http://127.0.0.1:18789",
  "log_level": "info"
}`

	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg, err := load(cfgPath, map[string]string{}, homeDir)
	if err != nil {
		t.Fatalf("expected loopback api base url to be allowed, got %v", err)
	}

	if cfg.APIBaseURL != "http://127.0.0.1:4000" {
		t.Fatalf("unexpected api base url: %s", cfg.APIBaseURL)
	}
}

func TestLoadAllowsInsecureAPIBaseURLWhenOverrideEnabled(t *testing.T) {
	homeDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "bridge.json")

	content := `{
  "api_base_url": "http://agentflows.appliedagentics.ai",
  "runtime_url": "http://127.0.0.1:18789",
  "log_level": "info"
}`

	if err := os.WriteFile(cfgPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	cfg, err := load(cfgPath, map[string]string{"AFB_ALLOW_INSECURE": "true"}, homeDir)
	if err != nil {
		t.Fatalf("expected insecure override to allow config, got %v", err)
	}

	if cfg.APIBaseURL != "http://agentflows.appliedagentics.ai" {
		t.Fatalf("unexpected api base url: %s", cfg.APIBaseURL)
	}
}
