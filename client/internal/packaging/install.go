package packaging

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/agentflows/agent-flows-bridge/client/internal/config"
)

// ServiceKind identifies the startup manager used for a user install.
type ServiceKind string

const (
	// ServiceKindLaunchd represents a macOS launchd agent file.
	ServiceKindLaunchd ServiceKind = "launchd"
	// ServiceKindSystemdUser represents a Linux systemd user unit.
	ServiceKindSystemdUser ServiceKind = "systemd_user"
)

// BuildInstallPlanOptions configure platform path resolution for install.
type BuildInstallPlanOptions struct {
	GOOS             string
	HomeDir          string
	StateDir         string
	SourceBinaryPath string
	BinaryName       string
}

// InstallPlan stores resolved install targets for binary/config/service files.
type InstallPlan struct {
	GOOS                string
	HomeDir             string
	StateDir            string
	SourceBinaryPath    string
	InstalledBinaryPath string
	ConfigPath          string
	ServicePath         string
	ServiceKind         ServiceKind
}

// InstallOptions controls install side effects for a plan.
type InstallOptions struct {
	Plan   InstallPlan
	Config config.Config
}

// InstallResult describes written files and activation commands.
type InstallResult struct {
	InstalledBinaryPath string   `json:"installed_binary_path"`
	ConfigPath          string   `json:"config_path"`
	ConfigCreated       bool     `json:"config_created"`
	ServicePath         string   `json:"service_path"`
	ServiceKind         string   `json:"service_kind"`
	ActivationCommands  []string `json:"activation_commands"`
}

// UninstallOptions controls removal of startup service artifacts.
type UninstallOptions struct {
	Plan InstallPlan
}

// UninstallResult describes removed files and deactivation commands.
type UninstallResult struct {
	InstalledBinaryPath  string   `json:"installed_binary_path"`
	InstalledBinaryPrev  string   `json:"installed_binary_prev"`
	BinaryRemoved        bool     `json:"binary_removed"`
	ServicePath          string   `json:"service_path"`
	ServiceRemoved       bool     `json:"service_removed"`
	ServiceKind          string   `json:"service_kind"`
	DeactivationCommands []string `json:"deactivation_commands"`
}

// BuildInstallPlan resolve install paths and service type for a target OS.
//
// Requires `homeDir`, `stateDir`, and `sourceBinaryPath`.
//
// Returns an InstallPlan or an error for invalid input/unsupported OS.
func BuildInstallPlan(options BuildInstallPlanOptions) (InstallPlan, error) {
	goos := strings.TrimSpace(options.GOOS)
	if goos == "" {
		goos = runtime.GOOS
	}

	homeDir := strings.TrimSpace(options.HomeDir)
	if homeDir == "" {
		return InstallPlan{}, fmt.Errorf("home dir is required")
	}

	stateDir := strings.TrimSpace(options.StateDir)
	if stateDir == "" {
		return InstallPlan{}, fmt.Errorf("state dir is required")
	}

	sourceBinaryPath := strings.TrimSpace(options.SourceBinaryPath)
	if sourceBinaryPath == "" {
		return InstallPlan{}, fmt.Errorf("source binary path is required")
	}

	binaryName := strings.TrimSpace(options.BinaryName)
	if binaryName == "" {
		binaryName = filepath.Base(sourceBinaryPath)
	}
	if binaryName == "" || binaryName == "." || binaryName == string(filepath.Separator) {
		binaryName = "agent-flows-bridge"
	}

	plan := InstallPlan{
		GOOS:                goos,
		HomeDir:             homeDir,
		StateDir:            stateDir,
		SourceBinaryPath:    sourceBinaryPath,
		InstalledBinaryPath: filepath.Join(stateDir, "bin", binaryName),
		ConfigPath:          filepath.Join(stateDir, "config", "bridge.json"),
	}

	switch goos {
	case "darwin":
		plan.ServiceKind = ServiceKindLaunchd
		plan.ServicePath = filepath.Join(homeDir, "Library", "LaunchAgents", "com.agentflows.bridge.plist")
	case "linux":
		plan.ServiceKind = ServiceKindSystemdUser
		plan.ServicePath = filepath.Join(homeDir, ".config", "systemd", "user", "agent-flows-bridge.service")
	default:
		return InstallPlan{}, fmt.Errorf("unsupported goos %q", goos)
	}

	return plan, nil
}

// RenderServiceFile generate a launchd plist or systemd unit body.
//
// Uses the plan's installed binary and config paths.
//
// Returns rendered service file contents or an error.
func RenderServiceFile(plan InstallPlan) (string, error) {
	if strings.TrimSpace(plan.InstalledBinaryPath) == "" {
		return "", fmt.Errorf("installed binary path is required")
	}
	if strings.TrimSpace(plan.ConfigPath) == "" {
		return "", fmt.Errorf("config path is required")
	}

	switch plan.ServiceKind {
	case ServiceKindLaunchd:
		launchd := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.agentflows.bridge</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>-config</string>
    <string>%s</string>
    <string>-run-daemon</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
</dict>
</plist>
`, xmlEscape(plan.InstalledBinaryPath), xmlEscape(plan.ConfigPath))
		return launchd, nil
	case ServiceKindSystemdUser:
		systemd := fmt.Sprintf(`[Unit]
Description=Agent Flows Bridge
After=network-online.target

[Service]
Type=simple
ExecStart=%s -config %s -run-daemon
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
`, systemdQuote(plan.InstalledBinaryPath), systemdQuote(plan.ConfigPath))
		return systemd, nil
	default:
		return "", fmt.Errorf("unsupported service kind %q", plan.ServiceKind)
	}
}

// Install write binary/config/service files for a user-level bridge install.
//
// Config file is created only if missing to avoid destructive rewrites.
//
// Returns install output paths and activation commands or an error.
func Install(options InstallOptions) (InstallResult, error) {
	plan := options.Plan

	if strings.TrimSpace(plan.SourceBinaryPath) == "" {
		return InstallResult{}, fmt.Errorf("source binary path is required")
	}
	if strings.TrimSpace(plan.InstalledBinaryPath) == "" {
		return InstallResult{}, fmt.Errorf("installed binary path is required")
	}
	if strings.TrimSpace(plan.ConfigPath) == "" {
		return InstallResult{}, fmt.Errorf("config path is required")
	}
	if strings.TrimSpace(plan.ServicePath) == "" {
		return InstallResult{}, fmt.Errorf("service path is required")
	}

	if err := os.MkdirAll(filepath.Dir(plan.InstalledBinaryPath), 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create bin dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.ConfigPath), 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(plan.ServicePath), 0o755); err != nil {
		return InstallResult{}, fmt.Errorf("create service dir: %w", err)
	}

	if err := copyBinary(plan.SourceBinaryPath, plan.InstalledBinaryPath); err != nil {
		return InstallResult{}, err
	}

	configCreated, err := writeConfigIfMissing(plan.ConfigPath, options.Config, plan.StateDir)
	if err != nil {
		return InstallResult{}, err
	}

	serviceBody, err := RenderServiceFile(plan)
	if err != nil {
		return InstallResult{}, err
	}

	if err := os.WriteFile(plan.ServicePath, []byte(serviceBody), 0o644); err != nil {
		return InstallResult{}, fmt.Errorf("write service file: %w", err)
	}

	result := InstallResult{
		InstalledBinaryPath: plan.InstalledBinaryPath,
		ConfigPath:          plan.ConfigPath,
		ConfigCreated:       configCreated,
		ServicePath:         plan.ServicePath,
		ServiceKind:         string(plan.ServiceKind),
		ActivationCommands:  activationCommands(plan),
	}
	return result, nil
}

func copyBinary(sourcePath string, targetPath string) error {
	cleanSource := filepath.Clean(sourcePath)
	cleanTarget := filepath.Clean(targetPath)

	if cleanSource == cleanTarget {
		if err := os.Chmod(targetPath, 0o755); err != nil {
			return fmt.Errorf("chmod installed binary: %w", err)
		}
		return nil
	}

	sameContents, err := filesMatch(cleanSource, cleanTarget)
	if err != nil {
		return err
	}
	if sameContents {
		if err := os.Chmod(cleanTarget, 0o755); err != nil {
			return fmt.Errorf("chmod installed binary: %w", err)
		}
		return nil
	}

	if _, err := os.Stat(cleanTarget); err == nil {
		backupPath := cleanTarget + ".prev"
		if removeErr := os.Remove(backupPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("remove previous binary backup: %w", removeErr)
		}
		if err := copyFile(cleanTarget, backupPath, 0o755); err != nil {
			return fmt.Errorf("backup installed binary: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat installed binary: %w", err)
	}

	if err := copyFile(cleanSource, cleanTarget, 0o755); err != nil {
		return err
	}

	return nil
}

func filesMatch(sourcePath string, targetPath string) (bool, error) {
	sourceDigest, err := fileDigest(sourcePath)
	if err != nil {
		return false, err
	}

	targetDigest, err := fileDigest(targetPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}

	return sourceDigest == targetDigest, nil
}

func fileDigest(path string) ([32]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return [32]byte{}, fmt.Errorf("hash file %s: %w", path, err)
	}

	var digest [32]byte
	copy(digest[:], hasher.Sum(nil))
	return digest, nil
}

func copyFile(sourcePath string, targetPath string, mode os.FileMode) error {
	sourceHandle, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open source binary: %w", err)
	}
	defer sourceHandle.Close()

	targetHandle, err := os.OpenFile(targetPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return fmt.Errorf("open target binary: %w", err)
	}

	if _, err := io.Copy(targetHandle, sourceHandle); err != nil {
		_ = targetHandle.Close()
		return fmt.Errorf("copy binary: %w", err)
	}

	if err := targetHandle.Close(); err != nil {
		return fmt.Errorf("close target binary: %w", err)
	}

	if err := os.Chmod(targetPath, mode); err != nil {
		return fmt.Errorf("chmod installed binary: %w", err)
	}

	return nil
}

// Uninstall remove the managed service file and installed bridge binary.
//
// Config/state files are intentionally preserved so reconnect and diagnostics
// data survive service removal. Deactivation commands are returned for the
// caller to present or run separately.
//
// Returns an UninstallResult or an error.
func Uninstall(options UninstallOptions) (UninstallResult, error) {
	plan := options.Plan
	if strings.TrimSpace(plan.InstalledBinaryPath) == "" {
		return UninstallResult{}, fmt.Errorf("installed binary path is required")
	}
	if strings.TrimSpace(plan.ServicePath) == "" {
		return UninstallResult{}, fmt.Errorf("service path is required")
	}

	binaryRemoved, err := removeIfExists(plan.InstalledBinaryPath)
	if err != nil {
		return UninstallResult{}, fmt.Errorf("remove installed binary: %w", err)
	}

	serviceRemoved, err := removeIfExists(plan.ServicePath)
	if err != nil {
		return UninstallResult{}, fmt.Errorf("remove service file: %w", err)
	}

	result := UninstallResult{
		InstalledBinaryPath:  plan.InstalledBinaryPath,
		InstalledBinaryPrev:  plan.InstalledBinaryPath + ".prev",
		BinaryRemoved:        binaryRemoved,
		ServicePath:          plan.ServicePath,
		ServiceRemoved:       serviceRemoved,
		ServiceKind:          string(plan.ServiceKind),
		DeactivationCommands: deactivationCommands(plan),
	}

	return result, nil
}

func writeConfigIfMissing(configPath string, cfg config.Config, stateDir string) (bool, error) {
	_, err := os.Stat(configPath)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat config file: %w", err)
	}

	if strings.TrimSpace(cfg.StateDir) == "" {
		cfg.StateDir = stateDir
	}

	encodedConfig, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, fmt.Errorf("encode config: %w", err)
	}

	encodedConfig = append(encodedConfig, '\n')

	if err := os.WriteFile(configPath, encodedConfig, 0o600); err != nil {
		return false, fmt.Errorf("write config file: %w", err)
	}

	return true, nil
}

func activationCommands(plan InstallPlan) []string {
	switch plan.ServiceKind {
	case ServiceKindLaunchd:
		return []string{
			fmt.Sprintf("launchctl bootstrap gui/$(id -u) %q", plan.ServicePath),
			"launchctl enable gui/$(id -u)/com.agentflows.bridge",
		}
	case ServiceKindSystemdUser:
		return []string{
			"systemctl --user daemon-reload",
			"systemctl --user enable --now agent-flows-bridge.service",
		}
	default:
		return nil
	}
}

func deactivationCommands(plan InstallPlan) []string {
	switch plan.ServiceKind {
	case ServiceKindLaunchd:
		return []string{
			fmt.Sprintf("launchctl bootout gui/$(id -u) %q", plan.ServicePath),
			"launchctl disable gui/$(id -u)/com.agentflows.bridge",
		}
	case ServiceKindSystemdUser:
		return []string{
			"systemctl --user disable --now agent-flows-bridge.service",
			"systemctl --user daemon-reload",
		}
	default:
		return nil
	}
}

func removeIfExists(path string) (bool, error) {
	err := os.Remove(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}

	return false, err
}

func systemdQuote(value string) string {
	quoted := strings.ReplaceAll(value, "\\", "\\\\")
	quoted = strings.ReplaceAll(quoted, "\"", "\\\"")
	return fmt.Sprintf("\"%s\"", quoted)
}

func xmlEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&apos;",
	)
	return replacer.Replace(value)
}
