package packaging

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentflows/agent-flows-bridge/client/internal/config"
)

func TestBuildInstallPlanUsesLaunchdOnDarwin(t *testing.T) {
	plan, err := BuildInstallPlan(BuildInstallPlanOptions{
		GOOS:             "darwin",
		HomeDir:          "/Users/tester",
		StateDir:         "/tmp/afb-state",
		SourceBinaryPath: "/tmp/build/agent-flows-bridge",
	})
	if err != nil {
		t.Fatalf("build install plan: %v", err)
	}

	if plan.ServiceKind != ServiceKindLaunchd {
		t.Fatalf("expected launchd service kind, got %s", plan.ServiceKind)
	}

	expectedServicePath := filepath.Join("/Users/tester", "Library", "LaunchAgents", "com.agentflows.bridge.plist")
	if plan.ServicePath != expectedServicePath {
		t.Fatalf("expected service path %s, got %s", expectedServicePath, plan.ServicePath)
	}
}

func TestBuildInstallPlanUsesSystemdUserOnLinux(t *testing.T) {
	plan, err := BuildInstallPlan(BuildInstallPlanOptions{
		GOOS:             "linux",
		HomeDir:          "/home/tester",
		StateDir:         "/tmp/afb-state",
		SourceBinaryPath: "/tmp/build/agent-flows-bridge",
	})
	if err != nil {
		t.Fatalf("build install plan: %v", err)
	}

	if plan.ServiceKind != ServiceKindSystemdUser {
		t.Fatalf("expected systemd user kind, got %s", plan.ServiceKind)
	}

	expectedServicePath := filepath.Join("/home/tester", ".config", "systemd", "user", "agent-flows-bridge.service")
	if plan.ServicePath != expectedServicePath {
		t.Fatalf("expected service path %s, got %s", expectedServicePath, plan.ServicePath)
	}
}

func TestBuildInstallPlanRejectsUnsupportedOS(t *testing.T) {
	_, err := BuildInstallPlan(BuildInstallPlanOptions{
		GOOS:             "solaris",
		HomeDir:          "/home/tester",
		StateDir:         "/tmp/afb-state",
		SourceBinaryPath: "/tmp/build/agent-flows-bridge",
	})
	if err == nil {
		t.Fatal("expected unsupported OS error")
	}

	if !strings.Contains(err.Error(), "unsupported goos") {
		t.Fatalf("expected unsupported goos error, got %v", err)
	}
}

func TestRenderServiceFileLaunchdIncludesConfigArgument(t *testing.T) {
	plan := InstallPlan{
		InstalledBinaryPath: "/Users/tester/.agent-flows-bridge/bin/agent-flows-bridge",
		ConfigPath:          "/Users/tester/.agent-flows-bridge/config/bridge.json",
		ServiceKind:         ServiceKindLaunchd,
	}

	serviceBody, err := RenderServiceFile(plan)
	if err != nil {
		t.Fatalf("render launchd file: %v", err)
	}

	if !strings.Contains(serviceBody, "<key>ProgramArguments</key>") {
		t.Fatalf("expected launchd ProgramArguments block, got %s", serviceBody)
	}

	if !strings.Contains(serviceBody, "-config") {
		t.Fatalf("expected -config arg in launchd service body, got %s", serviceBody)
	}

	if !strings.Contains(serviceBody, "-run-daemon") {
		t.Fatalf("expected -run-daemon arg in launchd service body, got %s", serviceBody)
	}
}

func TestRenderServiceFileSystemdIncludesExecStart(t *testing.T) {
	plan := InstallPlan{
		InstalledBinaryPath: "/home/tester/.agent-flows-bridge/bin/agent-flows-bridge",
		ConfigPath:          "/home/tester/.agent-flows-bridge/config/bridge.json",
		ServiceKind:         ServiceKindSystemdUser,
	}

	serviceBody, err := RenderServiceFile(plan)
	if err != nil {
		t.Fatalf("render systemd file: %v", err)
	}

	if !strings.Contains(serviceBody, "ExecStart=") {
		t.Fatalf("expected ExecStart in systemd service body, got %s", serviceBody)
	}

	if !strings.Contains(serviceBody, "Restart=always") {
		t.Fatalf("expected restart policy in systemd service body, got %s", serviceBody)
	}

	if !strings.Contains(serviceBody, "-run-daemon") {
		t.Fatalf("expected daemon mode arg in systemd service body, got %s", serviceBody)
	}
}

func TestInstallWritesBinaryConfigAndServiceFiles(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	stateDir := filepath.Join(tempDir, "state")
	sourceBinaryPath := filepath.Join(tempDir, "bridge-source")

	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(sourceBinaryPath, []byte("#!/bin/sh\necho bridge\n"), 0o755); err != nil {
		t.Fatalf("write source binary: %v", err)
	}

	plan, err := BuildInstallPlan(BuildInstallPlanOptions{
		GOOS:             "linux",
		HomeDir:          homeDir,
		StateDir:         stateDir,
		SourceBinaryPath: sourceBinaryPath,
	})
	if err != nil {
		t.Fatalf("build install plan: %v", err)
	}

	cfg := config.Config{
		APIBaseURL:    "https://saas.example.test",
		RuntimeURL:    "http://127.0.0.1:18789",
		StateDir:      stateDir,
		LogLevel:      "info",
		OAuthClientID: "agent-flows-bridge",
	}

	result, err := Install(InstallOptions{
		Plan:   plan,
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	if _, err := os.Stat(result.InstalledBinaryPath); err != nil {
		t.Fatalf("installed binary missing: %v", err)
	}
	if _, err := os.Stat(result.ConfigPath); err != nil {
		t.Fatalf("config missing: %v", err)
	}
	if _, err := os.Stat(result.ServicePath); err != nil {
		t.Fatalf("service missing: %v", err)
	}

	if len(result.ActivationCommands) == 0 {
		t.Fatalf("expected activation commands, got none")
	}
}

func TestInstallDoesNotOverwriteExistingConfig(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	stateDir := filepath.Join(tempDir, "state")
	sourceBinaryPath := filepath.Join(tempDir, "bridge-source")

	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(sourceBinaryPath, []byte("#!/bin/sh\necho bridge\n"), 0o755); err != nil {
		t.Fatalf("write source binary: %v", err)
	}

	plan, err := BuildInstallPlan(BuildInstallPlanOptions{
		GOOS:             "linux",
		HomeDir:          homeDir,
		StateDir:         stateDir,
		SourceBinaryPath: sourceBinaryPath,
	})
	if err != nil {
		t.Fatalf("build install plan: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(plan.ConfigPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	existingConfig := []byte(`{"runtime_url":"http://127.0.0.1:9999"}`)
	if err := os.WriteFile(plan.ConfigPath, existingConfig, 0o600); err != nil {
		t.Fatalf("write existing config: %v", err)
	}

	cfg := config.Config{
		APIBaseURL:    "https://saas.example.test",
		RuntimeURL:    "http://127.0.0.1:18789",
		StateDir:      stateDir,
		LogLevel:      "info",
		OAuthClientID: "agent-flows-bridge",
	}

	result, err := Install(InstallOptions{
		Plan:   plan,
		Config: cfg,
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}

	if result.ConfigCreated {
		t.Fatalf("expected config not to be rewritten")
	}

	gotConfig, err := os.ReadFile(plan.ConfigPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}

	if string(gotConfig) != string(existingConfig) {
		t.Fatalf("expected existing config to remain unchanged, got %s", string(gotConfig))
	}
}

func TestInstallSkipsBinaryBackupWhenSourceMatchesInstalledBinary(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	stateDir := filepath.Join(tempDir, "state")
	sourceBinaryPath := filepath.Join(tempDir, "bridge-source")
	sourceBinary := []byte("#!/bin/sh\necho bridge\n")

	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(sourceBinaryPath, sourceBinary, 0o755); err != nil {
		t.Fatalf("write source binary: %v", err)
	}

	plan, err := BuildInstallPlan(BuildInstallPlanOptions{
		GOOS:             "linux",
		HomeDir:          homeDir,
		StateDir:         stateDir,
		SourceBinaryPath: sourceBinaryPath,
	})
	if err != nil {
		t.Fatalf("build install plan: %v", err)
	}

	cfg := config.Config{
		APIBaseURL:    "https://agentflows.example.test",
		RuntimeURL:    "http://127.0.0.1:18789",
		StateDir:      stateDir,
		OAuthClientID: "agent-flows-bridge",
	}

	if _, err := Install(InstallOptions{Plan: plan, Config: cfg}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if _, err := Install(InstallOptions{Plan: plan, Config: cfg}); err != nil {
		t.Fatalf("second install: %v", err)
	}

	if _, err := os.Stat(plan.InstalledBinaryPath + ".prev"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no previous binary backup, got %v", err)
	}
}

func TestInstallBacksUpInstalledBinaryWhenSourceChanges(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	stateDir := filepath.Join(tempDir, "state")
	sourceBinaryPath := filepath.Join(tempDir, "bridge-source")

	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(sourceBinaryPath, []byte("#!/bin/sh\necho first\n"), 0o755); err != nil {
		t.Fatalf("write first source binary: %v", err)
	}

	plan, err := BuildInstallPlan(BuildInstallPlanOptions{
		GOOS:             "linux",
		HomeDir:          homeDir,
		StateDir:         stateDir,
		SourceBinaryPath: sourceBinaryPath,
	})
	if err != nil {
		t.Fatalf("build install plan: %v", err)
	}

	cfg := config.Config{
		APIBaseURL:    "https://agentflows.example.test",
		RuntimeURL:    "http://127.0.0.1:18789",
		StateDir:      stateDir,
		OAuthClientID: "agent-flows-bridge",
	}

	if _, err := Install(InstallOptions{Plan: plan, Config: cfg}); err != nil {
		t.Fatalf("first install: %v", err)
	}

	if err := os.WriteFile(sourceBinaryPath, []byte("#!/bin/sh\necho second\n"), 0o755); err != nil {
		t.Fatalf("write second source binary: %v", err)
	}

	if _, err := Install(InstallOptions{Plan: plan, Config: cfg}); err != nil {
		t.Fatalf("second install: %v", err)
	}

	installedBinary, err := os.ReadFile(plan.InstalledBinaryPath)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if string(installedBinary) != "#!/bin/sh\necho second\n" {
		t.Fatalf("unexpected installed binary: %q", string(installedBinary))
	}

	backupBinary, err := os.ReadFile(plan.InstalledBinaryPath + ".prev")
	if err != nil {
		t.Fatalf("read previous binary backup: %v", err)
	}
	if string(backupBinary) != "#!/bin/sh\necho first\n" {
		t.Fatalf("unexpected previous binary backup: %q", string(backupBinary))
	}
}

func TestUninstallRemovesServiceAndBinaryButPreservesConfig(t *testing.T) {
	tempDir := t.TempDir()
	homeDir := filepath.Join(tempDir, "home")
	stateDir := filepath.Join(tempDir, "state")
	sourceBinaryPath := filepath.Join(tempDir, "bridge-source")

	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	if err := os.WriteFile(sourceBinaryPath, []byte("#!/bin/sh\necho bridge\n"), 0o755); err != nil {
		t.Fatalf("write source binary: %v", err)
	}

	plan, err := BuildInstallPlan(BuildInstallPlanOptions{
		GOOS:             "linux",
		HomeDir:          homeDir,
		StateDir:         stateDir,
		SourceBinaryPath: sourceBinaryPath,
	})
	if err != nil {
		t.Fatalf("build install plan: %v", err)
	}

	cfg := config.Config{
		APIBaseURL:    "https://agentflows.example.test",
		RuntimeURL:    "http://127.0.0.1:18789",
		StateDir:      stateDir,
		OAuthClientID: "agent-flows-bridge",
	}

	if _, err := Install(InstallOptions{Plan: plan, Config: cfg}); err != nil {
		t.Fatalf("install: %v", err)
	}

	result, err := Uninstall(UninstallOptions{Plan: plan})
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	if !result.BinaryRemoved || !result.ServiceRemoved {
		t.Fatalf("expected binary and service removed, got %+v", result)
	}
	if len(result.DeactivationCommands) == 0 {
		t.Fatalf("expected deactivation commands, got %+v", result)
	}

	if _, err := os.Stat(plan.InstalledBinaryPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected installed binary removed, got %v", err)
	}
	if _, err := os.Stat(plan.ServicePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected service file removed, got %v", err)
	}
	if _, err := os.Stat(plan.ConfigPath); err != nil {
		t.Fatalf("expected config file preserved, got %v", err)
	}
}
