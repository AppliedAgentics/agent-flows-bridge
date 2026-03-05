package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	productionAPIBaseURL        = "https://agentflows.appliedagentics.ai"
	legacyPlaceholderAPIBaseURL = "https://agentflows.example.com"
)

// Config stores bridge runtime settings.
type Config struct {
	APIBaseURL      string `json:"api_base_url"`
	RuntimeURL      string `json:"runtime_url"`
	StateDir        string `json:"state_dir"`
	OpenClawDataDir string `json:"openclaw_data_dir"`
	LogLevel        string `json:"log_level"`
	OAuthClientID   string `json:"oauth_client_id"`
	TransportMode   string `json:"transport_mode"`
}

type fileConfig struct {
	APIBaseURL      *string `json:"api_base_url"`
	RuntimeURL      *string `json:"runtime_url"`
	StateDir        *string `json:"state_dir"`
	OpenClawDataDir *string `json:"openclaw_data_dir"`
	LogLevel        *string `json:"log_level"`
	OAuthClientID   *string `json:"oauth_client_id"`
	TransportMode   *string `json:"transport_mode"`
}

// Load resolve config from defaults, optional file, and env overrides.
//
// Precedence is: defaults < file < env variables.
// Env variables:
// - AFB_API_BASE_URL
// - AFB_RUNTIME_URL
// - AFB_STATE_DIR
// - AFB_OPENCLAW_DATA_DIR
// - AFB_LOG_LEVEL
// - AFB_OAUTH_CLIENT_ID
// - AFB_TRANSPORT_MODE
//
// Returns a validated Config or an error.
func Load(path string) (Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve user home: %w", err)
	}

	env := map[string]string{
		"AFB_API_BASE_URL":      strings.TrimSpace(os.Getenv("AFB_API_BASE_URL")),
		"AFB_RUNTIME_URL":       strings.TrimSpace(os.Getenv("AFB_RUNTIME_URL")),
		"AFB_STATE_DIR":         strings.TrimSpace(os.Getenv("AFB_STATE_DIR")),
		"AFB_OPENCLAW_DATA_DIR": strings.TrimSpace(os.Getenv("AFB_OPENCLAW_DATA_DIR")),
		"AFB_LOG_LEVEL":         strings.TrimSpace(os.Getenv("AFB_LOG_LEVEL")),
		"AFB_OAUTH_CLIENT_ID":   strings.TrimSpace(os.Getenv("AFB_OAUTH_CLIENT_ID")),
		"AFB_TRANSPORT_MODE":    strings.TrimSpace(os.Getenv("AFB_TRANSPORT_MODE")),
	}

	cfg, err := load(path, env, homeDir)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// load resolve config from defaults plus file and env overlays.
//
// This helper exists to make precedence behavior deterministic in unit tests.
//
// Returns a validated Config or an error.
func load(path string, env map[string]string, homeDir string) (Config, error) {
	cfg := defaultConfig(homeDir)

	if strings.TrimSpace(path) != "" {
		if err := applyFile(path, &cfg); err != nil {
			return Config{}, err
		}
	}

	applyEnv(env, &cfg)
	normalizeLegacyAPIBaseURL(&cfg)

	if err := validate(cfg, env); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func defaultConfig(homeDir string) Config {
	return Config{
		APIBaseURL:      productionAPIBaseURL,
		RuntimeURL:      "http://127.0.0.1:18789",
		StateDir:        filepath.Join(homeDir, ".agent-flows-bridge"),
		OpenClawDataDir: filepath.Join(homeDir, ".openclaw"),
		LogLevel:        "info",
		OAuthClientID:   "agent-flows-bridge",
		TransportMode:   "auto",
	}
}

func normalizeLegacyAPIBaseURL(cfg *Config) {
	if strings.TrimSpace(cfg.APIBaseURL) == legacyPlaceholderAPIBaseURL {
		cfg.APIBaseURL = productionAPIBaseURL
	}
}

func applyFile(path string, cfg *Config) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file %q: %w", path, err)
	}

	var fc fileConfig
	if err := json.Unmarshal(content, &fc); err != nil {
		return fmt.Errorf("parse config file %q: %w", path, err)
	}

	if fc.APIBaseURL != nil {
		cfg.APIBaseURL = strings.TrimSpace(*fc.APIBaseURL)
	}
	if fc.RuntimeURL != nil {
		cfg.RuntimeURL = strings.TrimSpace(*fc.RuntimeURL)
	}
	if fc.StateDir != nil {
		cfg.StateDir = strings.TrimSpace(*fc.StateDir)
	}
	if fc.OpenClawDataDir != nil {
		cfg.OpenClawDataDir = strings.TrimSpace(*fc.OpenClawDataDir)
	}
	if fc.LogLevel != nil {
		cfg.LogLevel = strings.TrimSpace(*fc.LogLevel)
	}
	if fc.OAuthClientID != nil {
		cfg.OAuthClientID = strings.TrimSpace(*fc.OAuthClientID)
	}
	if fc.TransportMode != nil {
		cfg.TransportMode = strings.TrimSpace(*fc.TransportMode)
	}

	return nil
}

func applyEnv(env map[string]string, cfg *Config) {
	if value := strings.TrimSpace(env["AFB_API_BASE_URL"]); value != "" {
		cfg.APIBaseURL = value
	}
	if value := strings.TrimSpace(env["AFB_RUNTIME_URL"]); value != "" {
		cfg.RuntimeURL = value
	}
	if value := strings.TrimSpace(env["AFB_STATE_DIR"]); value != "" {
		cfg.StateDir = value
	}
	if value := strings.TrimSpace(env["AFB_OPENCLAW_DATA_DIR"]); value != "" {
		cfg.OpenClawDataDir = value
	}
	if value := strings.TrimSpace(env["AFB_LOG_LEVEL"]); value != "" {
		cfg.LogLevel = value
	}
	if value := strings.TrimSpace(env["AFB_OAUTH_CLIENT_ID"]); value != "" {
		cfg.OAuthClientID = value
	}
	if value := strings.TrimSpace(env["AFB_TRANSPORT_MODE"]); value != "" {
		cfg.TransportMode = value
	}
}

func validate(cfg Config, env map[string]string) error {
	if err := validateURL("api_base_url", cfg.APIBaseURL, allowInsecureAPIBaseURL(env)); err != nil {
		return err
	}
	if err := validateURL("runtime_url", cfg.RuntimeURL, true); err != nil {
		return err
	}

	if strings.TrimSpace(cfg.StateDir) == "" {
		return fmt.Errorf("state_dir must not be empty")
	}
	if strings.TrimSpace(cfg.OpenClawDataDir) == "" {
		return fmt.Errorf("openclaw_data_dir must not be empty")
	}

	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
		// valid
	default:
		return fmt.Errorf("log_level must be one of debug|info|warn|error")
	}

	if strings.TrimSpace(cfg.OAuthClientID) == "" {
		return fmt.Errorf("oauth_client_id must not be empty")
	}

	switch cfg.TransportMode {
	case "auto", "wss", "poll":
		// valid
	default:
		return fmt.Errorf("transport_mode must be one of auto|wss|poll")
	}

	return nil
}

func validateURL(field string, raw string, allowInsecure bool) error {
	parsedURL, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%s invalid: %w", field, err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("%s invalid: unsupported scheme %q", field, parsedURL.Scheme)
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("%s invalid: host is required", field)
	}

	if field == "api_base_url" && parsedURL.Scheme != "https" && !allowInsecureHost(parsedURL.Hostname(), allowInsecure) {
		return fmt.Errorf("%s invalid: https is required for non-loopback hosts", field)
	}

	return nil
}

func allowInsecureAPIBaseURL(env map[string]string) bool {
	return strings.EqualFold(strings.TrimSpace(env["AFB_ALLOW_INSECURE"]), "true")
}

func allowInsecureHost(host string, allowInsecure bool) bool {
	if allowInsecure {
		return true
	}

	switch strings.TrimSpace(strings.ToLower(host)) {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
