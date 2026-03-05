package bootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
)

func TestApplyWritesOpenClawConfigEnvWorkspaceAndMarker(t *testing.T) {
	ctx := context.Background()
	openClawDataDir := filepath.Join(t.TempDir(), "openclaw")

	payload := oauth.BootstrapPayload{
		Runtime: oauth.BootstrapRuntime{ID: 77, RuntimeKind: "local_connector", FlowID: 42},
		Env: map[string]string{
			"AGENT_FLOWS_API_URL": "https://agentflows.example.test",
			"AGENT_FLOWS_API_KEY": "runtime_key_123",
			"ANTHROPIC_API_KEY":   "sk-ant-test-123",
		},
		Config: map[string]any{
			"env": map[string]any{
				"vars": map[string]any{
					"AGENT_FLOWS_API_URL": "${AGENTFLOWS_URL}",
					"AGENT_FLOWS_API_KEY": "${AGENT_FLOWS_API_KEY}",
					"ANTHROPIC_API_KEY":   "${ANTHROPIC_API_KEY}",
				},
			},
		},
		WorkspaceFiles: map[string]map[string]string{
			"/data/openclaw/workspace": {
				"AGENTS.md": "hello from bootstrap",
			},
		},
	}

	result, err := Apply(ctx, openClawDataDir, payload)
	if err != nil {
		t.Fatalf("apply bootstrap payload: %v", err)
	}

	if result.RuntimeID != 77 {
		t.Fatalf("unexpected runtime id: %d", result.RuntimeID)
	}

	configPath := filepath.Join(openClawDataDir, "openclaw.json")
	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read openclaw config: %v", err)
	}

	if strings.Contains(string(configRaw), "${AGENT_FLOWS_API_KEY}") {
		t.Fatalf("expected placeholder replacement in openclaw config, got %s", string(configRaw))
	}

	var config map[string]any
	if err := json.Unmarshal(configRaw, &config); err != nil {
		t.Fatalf("decode openclaw config: %v", err)
	}

	envVars := getMap(getMap(config, "env"), "vars")
	if envVars["AGENT_FLOWS_API_KEY"] != "runtime_key_123" {
		t.Fatalf("unexpected config api key value: %v", envVars["AGENT_FLOWS_API_KEY"])
	}
	if envVars["AGENT_FLOWS_API_URL"] != "https://agentflows.example.test" {
		t.Fatalf("unexpected config api url value: %v", envVars["AGENT_FLOWS_API_URL"])
	}
	if envVars["ANTHROPIC_API_KEY"] != "sk-ant-test-123" {
		t.Fatalf("unexpected anthropic api key value: %v", envVars["ANTHROPIC_API_KEY"])
	}

	envFilePath := filepath.Join(openClawDataDir, "agent-flows.env")
	envRaw, err := os.ReadFile(envFilePath)
	if err != nil {
		t.Fatalf("read env file: %v", err)
	}
	if !strings.Contains(string(envRaw), "AGENT_FLOWS_API_KEY=\"runtime_key_123\"") {
		t.Fatalf("expected api key in env file, got %s", string(envRaw))
	}
	if !strings.Contains(string(envRaw), "ANTHROPIC_API_KEY=\"sk-ant-test-123\"") {
		t.Fatalf("expected anthropic key in env file, got %s", string(envRaw))
	}

	workspaceFilePath := filepath.Join(openClawDataDir, "workspace", "AGENTS.md")
	workspaceRaw, err := os.ReadFile(workspaceFilePath)
	if err != nil {
		t.Fatalf("read workspace file: %v", err)
	}
	if string(workspaceRaw) != "hello from bootstrap" {
		t.Fatalf("unexpected workspace file content: %s", string(workspaceRaw))
	}

	marker, err := LoadMarker(openClawDataDir)
	if err != nil {
		t.Fatalf("load marker: %v", err)
	}
	if marker.RuntimeID != 77 {
		t.Fatalf("unexpected marker runtime id: %d", marker.RuntimeID)
	}
}

func TestBuildEnvFileQuotesAndEscapesValues(t *testing.T) {
	envFile := buildEnvFile(map[string]string{
		"AGENT_FLOWS_API_KEY": "sk test$123",
		"MULTILINE":           "line1\nline2\r\nline3",
		"QUOTES":              `value "inside"`,
	})

	lines := strings.Split(strings.TrimSpace(envFile), "\n")
	expectedLines := []string{
		`AGENT_FLOWS_API_KEY="sk test\$123"`,
		`MULTILINE="line1\nline2\r\nline3"`,
		`QUOTES="value \"inside\""`,
	}

	if len(lines) != len(expectedLines) {
		t.Fatalf("unexpected env line count: got=%d want=%d env=%q", len(lines), len(expectedLines), envFile)
	}

	for index, line := range lines {
		if line != expectedLines[index] {
			t.Fatalf("unexpected env line at %d: got=%q want=%q", index, line, expectedLines[index])
		}
	}
}

func TestApplyRewritesCloudPathsToLocalDataDir(t *testing.T) {
	ctx := context.Background()
	openClawDataDir := filepath.Join(t.TempDir(), "openclaw")

	payload := oauth.BootstrapPayload{
		Runtime: oauth.BootstrapRuntime{ID: 77, RuntimeKind: "local_connector", FlowID: 42},
		Env: map[string]string{
			"AGENT_FLOWS_API_URL": "https://agentflows.example.test",
			"AGENT_FLOWS_API_KEY": "runtime_key_123",
		},
		Config: map[string]any{
			"agents": map[string]any{
				"defaults": map[string]any{
					"workspace": "/data/openclaw/workspace",
					"memorySearch": map[string]any{
						"store": map[string]any{
							"path": "/data/openclaw/memory/main.sqlite",
						},
					},
				},
				"list": []any{
					map[string]any{"id": "lead", "workspace": "/data/openclaw/workspace"},
					map[string]any{"id": "writer", "workspace": "/data/openclaw/workspace-writer"},
					map[string]any{"id": "social", "workspace": "/data/openclaw/workspace-social"},
				},
			},
			"skills": map[string]any{
				"load": map[string]any{
					"extraDirs": []any{"/data/openclaw/skills"},
				},
			},
		},
		WorkspaceFiles: map[string]map[string]string{
			"/data/openclaw/workspace": {
				"AGENTS.md": "hello from bootstrap",
			},
		},
	}

	_, err := Apply(ctx, openClawDataDir, payload)
	if err != nil {
		t.Fatalf("apply bootstrap payload: %v", err)
	}

	configPath := filepath.Join(openClawDataDir, "openclaw.json")
	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read openclaw config: %v", err)
	}

	if strings.Contains(string(configRaw), "/data/openclaw") {
		t.Fatalf("expected no /data/openclaw paths in local config, got %s", string(configRaw))
	}

	var config map[string]any
	if err := json.Unmarshal(configRaw, &config); err != nil {
		t.Fatalf("decode openclaw config: %v", err)
	}

	defaults := getMap(getMap(config, "agents"), "defaults")
	if defaults["workspace"] != filepath.Join(openClawDataDir, "workspace") {
		t.Fatalf("unexpected defaults.workspace: %v", defaults["workspace"])
	}

	memoryPath := getMap(getMap(defaults, "memorySearch"), "store")["path"]
	if memoryPath != filepath.Join(openClawDataDir, "memory", "main.sqlite") {
		t.Fatalf("unexpected memory store path: %v", memoryPath)
	}

	agents := getSlice(getMap(config, "agents"), "list")
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	leadPath := agents[0]["workspace"]
	if leadPath != filepath.Join(openClawDataDir, "workspace") {
		t.Fatalf("unexpected lead workspace path: %v", leadPath)
	}

	writerPath := agents[1]["workspace"]
	if writerPath != filepath.Join(openClawDataDir, "workspace-writer") {
		t.Fatalf("unexpected writer workspace path: %v", writerPath)
	}

	socialPath := agents[2]["workspace"]
	if socialPath != filepath.Join(openClawDataDir, "workspace-social") {
		t.Fatalf("unexpected social workspace path: %v", socialPath)
	}

	extraDirs := getAnySlice(getMap(getMap(config, "skills"), "load"), "extraDirs")
	if len(extraDirs) != 1 {
		t.Fatalf("expected one extra dir, got %d", len(extraDirs))
	}

	if extraDirs[0] != filepath.Join(openClawDataDir, "skills") {
		t.Fatalf("unexpected skills extra dir path: %v", extraDirs[0])
	}
}

func TestApplyRewritesExactDataRoot(t *testing.T) {
	ctx := context.Background()
	openClawDataDir := filepath.Join(t.TempDir(), "openclaw")

	payload := oauth.BootstrapPayload{
		Runtime: oauth.BootstrapRuntime{ID: 77, RuntimeKind: "local_connector", FlowID: 42},
		Env: map[string]string{
			"AGENT_FLOWS_API_URL": "https://agentflows.example.test",
			"AGENT_FLOWS_API_KEY": "runtime_key_123",
		},
		Config: map[string]any{
			"skills": map[string]any{
				"load": map[string]any{
					"extraDirs": []any{"/data/openclaw"},
				},
			},
		},
		WorkspaceFiles: map[string]map[string]string{
			"/data/openclaw/workspace": {
				"AGENTS.md": "hello from bootstrap",
			},
		},
	}

	_, err := Apply(ctx, openClawDataDir, payload)
	if err != nil {
		t.Fatalf("apply bootstrap payload: %v", err)
	}

	configPath := filepath.Join(openClawDataDir, "openclaw.json")
	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read openclaw config: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(configRaw, &config); err != nil {
		t.Fatalf("decode openclaw config: %v", err)
	}

	extraDirs := getAnySlice(getMap(getMap(config, "skills"), "load"), "extraDirs")
	if len(extraDirs) != 1 {
		t.Fatalf("expected one extra dir, got %d", len(extraDirs))
	}

	if extraDirs[0] != openClawDataDir {
		t.Fatalf("expected exact data root rewrite to %q, got %v", openClawDataDir, extraDirs[0])
	}
}

func TestApplyDoesNotRewriteNonDataPaths(t *testing.T) {
	ctx := context.Background()
	openClawDataDir := filepath.Join(t.TempDir(), "openclaw")

	payload := oauth.BootstrapPayload{
		Runtime: oauth.BootstrapRuntime{ID: 77, RuntimeKind: "local_connector", FlowID: 42},
		Env: map[string]string{
			"AGENT_FLOWS_API_URL": "https://agentflows.example.test",
			"AGENT_FLOWS_API_KEY": "runtime_key_123",
		},
		Config: map[string]any{
			"agents": map[string]any{
				"defaults": map[string]any{
					"workspace": "workspace",
				},
				"list": []any{
					map[string]any{"id": "lead", "workspace": "/tmp/external/workspace"},
					map[string]any{"id": "writer", "workspace": "/data/openclaws/workspace-writer"},
				},
			},
			"skills": map[string]any{
				"load": map[string]any{
					"extraDirs": []any{"skills", "/tmp/skills", "/data/openclaws/skills"},
				},
			},
		},
		WorkspaceFiles: map[string]map[string]string{
			"/data/openclaw/workspace": {
				"AGENTS.md": "hello from bootstrap",
			},
		},
	}

	_, err := Apply(ctx, openClawDataDir, payload)
	if err != nil {
		t.Fatalf("apply bootstrap payload: %v", err)
	}

	configPath := filepath.Join(openClawDataDir, "openclaw.json")
	configRaw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read openclaw config: %v", err)
	}

	var config map[string]any
	if err := json.Unmarshal(configRaw, &config); err != nil {
		t.Fatalf("decode openclaw config: %v", err)
	}

	defaults := getMap(getMap(config, "agents"), "defaults")
	if defaults["workspace"] != "workspace" {
		t.Fatalf("expected relative workspace to remain unchanged, got %v", defaults["workspace"])
	}

	agents := getSlice(getMap(config, "agents"), "list")
	if len(agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(agents))
	}

	if agents[0]["workspace"] != "/tmp/external/workspace" {
		t.Fatalf("expected non-data absolute path unchanged, got %v", agents[0]["workspace"])
	}
	if agents[1]["workspace"] != "/data/openclaws/workspace-writer" {
		t.Fatalf("expected boundary path unchanged, got %v", agents[1]["workspace"])
	}

	extraDirs := getAnySlice(getMap(getMap(config, "skills"), "load"), "extraDirs")
	if len(extraDirs) != 3 {
		t.Fatalf("expected 3 extra dirs, got %d", len(extraDirs))
	}
	if extraDirs[0] != "skills" || extraDirs[1] != "/tmp/skills" || extraDirs[2] != "/data/openclaws/skills" {
		t.Fatalf("expected non-data extraDirs unchanged, got %+v", extraDirs)
	}
}

func TestApplyRejectsUnsafeWorkspacePath(t *testing.T) {
	ctx := context.Background()
	openClawDataDir := filepath.Join(t.TempDir(), "openclaw")

	payload := oauth.BootstrapPayload{
		Runtime: oauth.BootstrapRuntime{ID: 77, RuntimeKind: "local_connector", FlowID: 42},
		Env: map[string]string{
			"AGENT_FLOWS_API_URL": "https://agentflows.example.test",
			"AGENT_FLOWS_API_KEY": "runtime_key_123",
		},
		Config: map[string]any{"hooks": map[string]any{"enabled": true}},
		WorkspaceFiles: map[string]map[string]string{
			"/etc": {
				"AGENTS.md": "bad-path",
			},
		},
	}

	_, err := Apply(ctx, openClawDataDir, payload)
	if err == nil {
		t.Fatal("expected unsafe workspace path error")
	}
	if !strings.Contains(err.Error(), "workspace path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyRejectsUnsafeWorkspaceFileName(t *testing.T) {
	ctx := context.Background()
	openClawDataDir := filepath.Join(t.TempDir(), "openclaw")

	payload := oauth.BootstrapPayload{
		Runtime: oauth.BootstrapRuntime{ID: 77, RuntimeKind: "local_connector", FlowID: 42},
		Env: map[string]string{
			"AGENT_FLOWS_API_URL": "https://agentflows.example.test",
			"AGENT_FLOWS_API_KEY": "runtime_key_123",
		},
		Config: map[string]any{"hooks": map[string]any{"enabled": true}},
		WorkspaceFiles: map[string]map[string]string{
			"/data/openclaw/workspace": {
				"../evil.sh": "bad-name",
			},
		},
	}

	_, err := Apply(ctx, openClawDataDir, payload)
	if err == nil {
		t.Fatal("expected unsafe workspace file name error")
	}
	if !strings.Contains(err.Error(), "workspace file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestApplyAllowsNestedWorkspaceFilePath(t *testing.T) {
	ctx := context.Background()
	openClawDataDir := filepath.Join(t.TempDir(), "openclaw")

	payload := oauth.BootstrapPayload{
		Runtime: oauth.BootstrapRuntime{ID: 77, RuntimeKind: "local_connector", FlowID: 42},
		Env: map[string]string{
			"AGENT_FLOWS_API_URL": "https://agentflows.example.test",
			"AGENT_FLOWS_API_KEY": "runtime_key_123",
		},
		Config: map[string]any{"hooks": map[string]any{"enabled": true}},
		WorkspaceFiles: map[string]map[string]string{
			"/data/openclaw/skills": {
				"agent-flows/SKILL.md": "skill-content",
			},
		},
	}

	result, err := Apply(ctx, openClawDataDir, payload)
	if err != nil {
		t.Fatalf("apply bootstrap payload: %v", err)
	}

	if result.WorkspaceFileCount != 1 {
		t.Fatalf("unexpected workspace file count: %d", result.WorkspaceFileCount)
	}

	skillPath := filepath.Join(openClawDataDir, "skills", "agent-flows", "SKILL.md")
	skillRaw, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read nested workspace file: %v", err)
	}

	if string(skillRaw) != "skill-content" {
		t.Fatalf("unexpected nested workspace file content: %s", string(skillRaw))
	}
}

func getMap(root map[string]any, key string) map[string]any {
	value, ok := root[key]
	if !ok {
		return map[string]any{}
	}

	typed, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}

	return typed
}

func getSlice(root map[string]any, key string) []map[string]any {
	value, ok := root[key]
	if !ok {
		return []map[string]any{}
	}

	list, ok := value.([]any)
	if !ok {
		return []map[string]any{}
	}

	result := make([]map[string]any, 0, len(list))
	for _, entry := range list {
		typed, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, typed)
	}

	return result
}

func getAnySlice(root map[string]any, key string) []any {
	value, ok := root[key]
	if !ok {
		return []any{}
	}

	list, ok := value.([]any)
	if !ok {
		return []any{}
	}

	return list
}
