package binding

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrNotFound indicates there is no saved runtime binding.
var ErrNotFound = errors.New("runtime binding not found")

// RuntimeBinding stores local bridge binding identity for reconnect flows.
type RuntimeBinding struct {
	RuntimeID   int       `json:"runtime_id"`
	RuntimeKind string    `json:"runtime_kind"`
	RuntimeName string    `json:"runtime_name,omitempty"`
	FlowID      int       `json:"flow_id,omitempty"`
	ConnectorID int       `json:"connector_id,omitempty"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Save persist runtime binding data in state storage.
//
// The binding is validated and written atomically to the OAuth state directory.
// UpdatedAt is set to the current UTC timestamp when omitted.
//
// Returns nil on success or an error.
func Save(stateDir string, binding RuntimeBinding) error {
	trimmedStateDir := strings.TrimSpace(stateDir)
	if trimmedStateDir == "" {
		return fmt.Errorf("state dir is required")
	}
	if binding.RuntimeID <= 0 {
		return fmt.Errorf("runtime_id must be positive")
	}
	if strings.TrimSpace(binding.RuntimeKind) == "" {
		return fmt.Errorf("runtime_kind is required")
	}

	binding.RuntimeKind = strings.TrimSpace(binding.RuntimeKind)
	binding.RuntimeName = strings.TrimSpace(binding.RuntimeName)

	if binding.UpdatedAt.IsZero() {
		binding.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	} else {
		binding.UpdatedAt = binding.UpdatedAt.UTC().Truncate(time.Second)
	}

	oauthDir := filepath.Join(trimmedStateDir, "oauth")
	if err := os.MkdirAll(oauthDir, 0o700); err != nil {
		return fmt.Errorf("create oauth state directory: %w", err)
	}

	encoded, err := json.Marshal(binding)
	if err != nil {
		return fmt.Errorf("encode runtime binding: %w", err)
	}

	tempFile, err := os.CreateTemp(oauthDir, "runtime-binding-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp runtime binding file: %w", err)
	}

	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(0o600); err != nil {
		tempFile.Close()
		return fmt.Errorf("chmod temp runtime binding file: %w", err)
	}

	if _, err := tempFile.Write(encoded); err != nil {
		tempFile.Close()
		return fmt.Errorf("write temp runtime binding file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp runtime binding file: %w", err)
	}

	finalPath := bindingPath(trimmedStateDir)
	if err := os.Rename(tempPath, finalPath); err != nil {
		return fmt.Errorf("persist runtime binding file: %w", err)
	}

	if err := os.Chmod(finalPath, 0o600); err != nil {
		return fmt.Errorf("chmod runtime binding file: %w", err)
	}

	return nil
}

// Load read runtime binding data from state storage.
//
// The payload is validated after decode to ensure reconnect fields are usable.
// Missing files return ErrNotFound.
//
// Returns RuntimeBinding or an error.
func Load(stateDir string) (RuntimeBinding, error) {
	raw, err := os.ReadFile(bindingPath(stateDir))
	if err != nil {
		if os.IsNotExist(err) {
			return RuntimeBinding{}, ErrNotFound
		}
		return RuntimeBinding{}, fmt.Errorf("read runtime binding file: %w", err)
	}

	var runtimeBinding RuntimeBinding
	if err := json.Unmarshal(raw, &runtimeBinding); err != nil {
		return RuntimeBinding{}, fmt.Errorf("decode runtime binding file: %w", err)
	}

	if runtimeBinding.RuntimeID <= 0 {
		return RuntimeBinding{}, fmt.Errorf("invalid runtime binding payload: runtime_id")
	}
	if strings.TrimSpace(runtimeBinding.RuntimeKind) == "" {
		return RuntimeBinding{}, fmt.Errorf("invalid runtime binding payload: runtime_kind")
	}

	runtimeBinding.RuntimeKind = strings.TrimSpace(runtimeBinding.RuntimeKind)
	runtimeBinding.RuntimeName = strings.TrimSpace(runtimeBinding.RuntimeName)

	return runtimeBinding, nil
}

// Clear delete runtime binding data.
//
// Missing files are treated as success for idempotent cleanup paths.
//
// Returns nil on success or an error.
func Clear(stateDir string) error {
	if err := os.Remove(bindingPath(stateDir)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove runtime binding file: %w", err)
	}

	return nil
}

// Build runtime binding file path under OAuth state storage.
//
// The path is always anchored at `<state_dir>/oauth/runtime-binding.json`.
//
// Returns absolute-or-relative path string depending on stateDir input.
func bindingPath(stateDir string) string {
	return filepath.Join(stateDir, "oauth", "runtime-binding.json")
}
