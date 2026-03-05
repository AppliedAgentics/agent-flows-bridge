package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/agentflows/agent-flows-bridge/client/internal/oauth"
)

const (
	markerFileName = ".agent-flows-bridge-bootstrap.json"
	cloudDataRoot  = "/data/openclaw"
)

// ApplyResult summarizes local filesystem writes from bootstrap apply.
type ApplyResult struct {
	RuntimeID          int       `json:"runtime_id"`
	RuntimeKind        string    `json:"runtime_kind"`
	FlowID             int       `json:"flow_id"`
	OpenClawDataDir    string    `json:"openclaw_data_dir"`
	ConfigPath         string    `json:"config_path"`
	EnvPath            string    `json:"env_path"`
	WorkspaceFileCount int       `json:"workspace_file_count"`
	AppliedAt          time.Time `json:"applied_at"`
}

// ApplyMarker captures latest successful bootstrap apply metadata.
type ApplyMarker struct {
	RuntimeID       int       `json:"runtime_id"`
	RuntimeKind     string    `json:"runtime_kind"`
	FlowID          int       `json:"flow_id"`
	OpenClawDataDir string    `json:"openclaw_data_dir"`
	ConfigPath      string    `json:"config_path"`
	EnvPath         string    `json:"env_path"`
	AppliedAt       time.Time `json:"applied_at"`
}

// Apply write bootstrap payload into local OpenClaw filesystem paths.
//
// Writes openclaw config, runtime env file, and workspace files under
// the supplied OpenClaw data directory, then records an apply marker.
//
// Returns an ApplyResult or an error.
func Apply(
	ctx context.Context,
	openClawDataDir string,
	bootstrapPayload oauth.BootstrapPayload,
) (ApplyResult, error) {
	if strings.TrimSpace(openClawDataDir) == "" {
		return ApplyResult{}, fmt.Errorf("openclaw data dir is required")
	}
	if bootstrapPayload.Runtime.ID <= 0 {
		return ApplyResult{}, fmt.Errorf("bootstrap runtime id is required")
	}

	cleanDataDir := filepath.Clean(openClawDataDir)
	if err := os.MkdirAll(cleanDataDir, 0o755); err != nil {
		return ApplyResult{}, fmt.Errorf("create openclaw data dir: %w", err)
	}

	materializedConfig := materializeConfig(bootstrapPayload.Config, bootstrapPayload.Env)
	materializedConfig = rewriteCloudPaths(materializedConfig, cleanDataDir)
	encodedConfig, err := json.MarshalIndent(materializedConfig, "", "  ")
	if err != nil {
		return ApplyResult{}, fmt.Errorf("encode openclaw config: %w", err)
	}
	encodedConfig = append(encodedConfig, '\n')

	configPath := filepath.Join(cleanDataDir, "openclaw.json")
	if err := writeFileAtomically(configPath, encodedConfig, 0o600); err != nil {
		return ApplyResult{}, fmt.Errorf("write openclaw config: %w", err)
	}

	envRaw := buildEnvFile(bootstrapPayload.Env)
	envPath := filepath.Join(cleanDataDir, "agent-flows.env")
	if err := writeFileAtomically(envPath, []byte(envRaw), 0o600); err != nil {
		return ApplyResult{}, fmt.Errorf("write openclaw env file: %w", err)
	}

	workspaceFileCount := 0
	workspaceDirs := sortedKeys(bootstrapPayload.WorkspaceFiles)

	for _, workspaceDir := range workspaceDirs {
		if err := ctx.Err(); err != nil {
			return ApplyResult{}, err
		}

		resolvedDir, err := resolveWorkspaceDir(cleanDataDir, workspaceDir)
		if err != nil {
			return ApplyResult{}, err
		}

		if err := os.MkdirAll(resolvedDir, 0o755); err != nil {
			return ApplyResult{}, fmt.Errorf("create workspace dir: %w", err)
		}

		files := bootstrapPayload.WorkspaceFiles[workspaceDir]
		fileNames := sortedKeys(files)
		for _, fileName := range fileNames {
			filePath, err := resolveWorkspaceFilePath(resolvedDir, fileName)
			if err != nil {
				return ApplyResult{}, err
			}

			if err := writeFileAtomically(filePath, []byte(files[fileName]), 0o644); err != nil {
				return ApplyResult{}, fmt.Errorf("write workspace file %s: %w", filePath, err)
			}
			workspaceFileCount++
		}
	}

	appliedAt := time.Now().UTC().Truncate(time.Second)
	marker := ApplyMarker{
		RuntimeID:       bootstrapPayload.Runtime.ID,
		RuntimeKind:     bootstrapPayload.Runtime.RuntimeKind,
		FlowID:          bootstrapPayload.Runtime.FlowID,
		OpenClawDataDir: cleanDataDir,
		ConfigPath:      configPath,
		EnvPath:         envPath,
		AppliedAt:       appliedAt,
	}

	if err := saveMarker(cleanDataDir, marker); err != nil {
		return ApplyResult{}, err
	}

	result := ApplyResult{
		RuntimeID:          marker.RuntimeID,
		RuntimeKind:        marker.RuntimeKind,
		FlowID:             marker.FlowID,
		OpenClawDataDir:    marker.OpenClawDataDir,
		ConfigPath:         marker.ConfigPath,
		EnvPath:            marker.EnvPath,
		WorkspaceFileCount: workspaceFileCount,
		AppliedAt:          marker.AppliedAt,
	}
	return result, nil
}

// LoadMarker read bootstrap apply marker metadata from the OpenClaw data dir.
//
// Returns ApplyMarker or an error.
func LoadMarker(openClawDataDir string) (ApplyMarker, error) {
	cleanDataDir := filepath.Clean(openClawDataDir)
	markerPath := filepath.Join(cleanDataDir, markerFileName)

	raw, err := os.ReadFile(markerPath)
	if err != nil {
		return ApplyMarker{}, err
	}

	var marker ApplyMarker
	if err := json.Unmarshal(raw, &marker); err != nil {
		return ApplyMarker{}, fmt.Errorf("decode apply marker: %w", err)
	}

	if marker.RuntimeID <= 0 {
		return ApplyMarker{}, fmt.Errorf("invalid apply marker runtime id")
	}

	return marker, nil
}

func saveMarker(openClawDataDir string, marker ApplyMarker) error {
	encoded, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return fmt.Errorf("encode apply marker: %w", err)
	}
	encoded = append(encoded, '\n')

	markerPath := filepath.Join(openClawDataDir, markerFileName)
	if err := writeFileAtomically(markerPath, encoded, 0o600); err != nil {
		return fmt.Errorf("write apply marker: %w", err)
	}

	return nil
}

func materializeConfig(config map[string]any, env map[string]string) map[string]any {
	resolvedConfig := map[string]any{}
	for key, value := range config {
		resolvedConfig[key] = replacePlaceholders(value, env)
	}
	return resolvedConfig
}

func rewriteCloudPaths(config map[string]any, openClawDataDir string) map[string]any {
	rewrittenConfig := map[string]any{}
	for key, value := range config {
		rewrittenConfig[key] = rewriteCloudPathValue(value, openClawDataDir)
	}

	return rewrittenConfig
}

func rewriteCloudPathValue(value any, openClawDataDir string) any {
	switch typedValue := value.(type) {
	case map[string]any:
		rewrittenMap := map[string]any{}
		for key, nestedValue := range typedValue {
			rewrittenMap[key] = rewriteCloudPathValue(nestedValue, openClawDataDir)
		}
		return rewrittenMap
	case []any:
		rewrittenList := make([]any, 0, len(typedValue))
		for _, nestedValue := range typedValue {
			rewrittenList = append(rewrittenList, rewriteCloudPathValue(nestedValue, openClawDataDir))
		}
		return rewrittenList
	case string:
		return rewriteCloudPathString(typedValue, openClawDataDir)
	default:
		return typedValue
	}
}

func rewriteCloudPathString(value string, openClawDataDir string) string {
	cleanDataDir := filepath.Clean(openClawDataDir)
	if value == cloudDataRoot {
		return cleanDataDir
	}

	cloudPathPrefix := cloudDataRoot + "/"
	if !strings.HasPrefix(value, cloudPathPrefix) {
		return value
	}

	pathSuffix := strings.TrimPrefix(value, cloudPathPrefix)
	return filepath.Join(cleanDataDir, filepath.FromSlash(pathSuffix))
}

func replacePlaceholders(value any, env map[string]string) any {
	switch typedValue := value.(type) {
	case map[string]any:
		resolvedMap := map[string]any{}
		for key, nestedValue := range typedValue {
			resolvedMap[key] = replacePlaceholders(nestedValue, env)
		}
		return resolvedMap
	case []any:
		resolvedList := make([]any, 0, len(typedValue))
		for _, nestedValue := range typedValue {
			resolvedList = append(resolvedList, replacePlaceholders(nestedValue, env))
		}
		return resolvedList
	case string:
		replaced := typedValue
		for key, envValue := range env {
			placeholder := "${" + key + "}"
			replaced = strings.ReplaceAll(replaced, placeholder, envValue)
		}
		replaced = strings.ReplaceAll(replaced, "${AGENTFLOWS_URL}", env["AGENT_FLOWS_API_URL"])
		return replaced
	default:
		return typedValue
	}
}

// Build a deterministic dotenv file for the local OpenClaw runtime.
//
// Keys are sorted for reproducible output. Values are emitted as quoted dotenv
// strings so secrets containing whitespace, quotes, dollars, or newlines stay
// parseable for dotenv loaders and safe for shell-sourced debugging.
//
// Returns the rendered env file contents with trailing newlines.
func buildEnvFile(env map[string]string) string {
	builder := strings.Builder{}

	for _, key := range sortedKeys(env) {
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(quoteEnvValue(env[key]))
		builder.WriteString("\n")
	}

	return builder.String()
}

func quoteEnvValue(value string) string {
	replacer := strings.NewReplacer(
		"\\", "\\\\",
		"\"", "\\\"",
		"$", "\\$",
		"\n", "\\n",
		"\r", "\\r",
	)

	return fmt.Sprintf("\"%s\"", replacer.Replace(value))
}

func sortedKeys[T any](root map[string]T) []string {
	keys := make([]string, 0, len(root))
	for key := range root {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resolveWorkspaceDir(openClawDataDir string, workspaceDir string) (string, error) {
	trimmedWorkspaceDir := strings.TrimSpace(workspaceDir)
	if trimmedWorkspaceDir == "" {
		return "", fmt.Errorf("workspace path must not be empty")
	}

	cleanDataDir := filepath.Clean(openClawDataDir)

	var candidatePath string
	if strings.HasPrefix(trimmedWorkspaceDir, "/data/openclaw") {
		suffix := strings.TrimPrefix(trimmedWorkspaceDir, "/data/openclaw")
		suffix = strings.TrimPrefix(suffix, "/")
		candidatePath = filepath.Join(cleanDataDir, filepath.FromSlash(suffix))
	} else {
		if filepath.IsAbs(trimmedWorkspaceDir) {
			return "", fmt.Errorf("workspace path %q is outside openclaw data dir", workspaceDir)
		}
		candidatePath = filepath.Join(cleanDataDir, filepath.FromSlash(trimmedWorkspaceDir))
	}

	cleanCandidatePath := filepath.Clean(candidatePath)
	if !isWithinBase(cleanDataDir, cleanCandidatePath) {
		return "", fmt.Errorf("workspace path %q escapes openclaw data dir", workspaceDir)
	}

	return cleanCandidatePath, nil
}

func validateWorkspaceFileName(fileName string) error {
	_, err := resolveWorkspaceFilePath(".", fileName)
	return err
}

func resolveWorkspaceFilePath(workspaceDir string, filePath string) (string, error) {
	trimmedFilePath := strings.TrimSpace(filePath)
	if trimmedFilePath == "" {
		return "", fmt.Errorf("workspace file path must not be empty")
	}

	normalizedPath := strings.ReplaceAll(trimmedFilePath, "\\", "/")
	cleanRelativePath := path.Clean(normalizedPath)

	if path.IsAbs(cleanRelativePath) {
		return "", fmt.Errorf("workspace file path %q must be relative", filePath)
	}

	if cleanRelativePath == "." || cleanRelativePath == ".." || strings.HasPrefix(cleanRelativePath, "../") {
		return "", fmt.Errorf("workspace file path %q is invalid", filePath)
	}

	candidatePath := filepath.Join(workspaceDir, filepath.FromSlash(cleanRelativePath))
	cleanWorkspaceDir := filepath.Clean(workspaceDir)
	cleanCandidatePath := filepath.Clean(candidatePath)

	if !isWithinBase(cleanWorkspaceDir, cleanCandidatePath) {
		return "", fmt.Errorf("workspace file path %q escapes workspace dir", filePath)
	}

	return cleanCandidatePath, nil
}

func isWithinBase(basePath string, candidatePath string) bool {
	relPath, err := filepath.Rel(basePath, candidatePath)
	if err != nil {
		return false
	}

	if relPath == "." {
		return true
	}

	if strings.HasPrefix(relPath, "..") {
		return false
	}

	return true
}

func writeFileAtomically(path string, value []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	tempFile, err := os.CreateTemp(dir, "afb-bootstrap-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(mode); err != nil {
		tempFile.Close()
		return fmt.Errorf("chmod temp file: %w", err)
	}

	if _, err := tempFile.Write(value); err != nil {
		tempFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("chmod target file: %w", err)
	}

	return nil
}
