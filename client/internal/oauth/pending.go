package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentflows/agent-flows-bridge/client/internal/secrets"
)

const pendingStartSecretKey = "oauth_pending_start"

// ErrPendingStartNotFound indicates no pending OAuth start payload is stored.
var ErrPendingStartNotFound = errors.New("pending oauth start not found")

// SavePendingStart persist OAuth start data in secret storage.
//
// Writes pending PKCE verifier/state data to the configured secret backend.
// Plaintext file storage is no longer used for new writes.
//
// Returns nil on success or an error.
func SavePendingStart(
	ctx context.Context,
	stateDir string,
	secretStore secrets.Store,
	start StartLoginResult,
) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state dir is required")
	}
	if secretStore == nil {
		return fmt.Errorf("secret store is required")
	}
	if strings.TrimSpace(start.State) == "" || strings.TrimSpace(start.CodeVerifier) == "" {
		return fmt.Errorf("pending start requires state and code verifier")
	}

	encoded, err := encodePendingStart(start)
	if err != nil {
		return err
	}

	if err := secretStore.Save(ctx, pendingStartSecretKey, encoded); err != nil {
		return fmt.Errorf("save pending oauth start: %w", err)
	}

	return nil
}

// LoadPendingStart read pending OAuth start data from secret storage.
//
// Falls back to the legacy plaintext file path once, migrates it into the
// secret store, then deletes the legacy file.
//
// Returns StartLoginResult or an error.
func LoadPendingStart(
	ctx context.Context,
	stateDir string,
	secretStore secrets.Store,
) (StartLoginResult, error) {
	if strings.TrimSpace(stateDir) == "" {
		return StartLoginResult{}, fmt.Errorf("state dir is required")
	}
	if secretStore == nil {
		return StartLoginResult{}, fmt.Errorf("secret store is required")
	}

	raw, err := secretStore.Load(ctx, pendingStartSecretKey)
	if err == nil {
		var start StartLoginResult
		if err := decodePendingStart(raw, &start); err != nil {
			return StartLoginResult{}, err
		}

		return start, nil
	}

	if !errors.Is(err, secrets.ErrNotFound) {
		return StartLoginResult{}, fmt.Errorf("load pending oauth start: %w", err)
	}

	start, err := loadPendingStartFromLegacyFile(stateDir)
	if err != nil {
		return StartLoginResult{}, err
	}

	if saveErr := SavePendingStart(ctx, stateDir, secretStore, start); saveErr != nil {
		return StartLoginResult{}, saveErr
	}
	if clearErr := clearPendingStartLegacyFile(stateDir); clearErr != nil {
		return StartLoginResult{}, clearErr
	}

	return start, nil
}

// ClearPendingStart delete pending OAuth start data from all storage paths.
//
// Secret-store and legacy-file cleanup are both best-effort idempotent deletes.
//
// Returns nil on success or an error.
func ClearPendingStart(ctx context.Context, stateDir string, secretStore secrets.Store) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state dir is required")
	}
	if secretStore == nil {
		return fmt.Errorf("secret store is required")
	}

	if err := secretStore.Delete(ctx, pendingStartSecretKey); err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return fmt.Errorf("delete pending oauth start: %w", err)
	}

	if err := clearPendingStartLegacyFile(stateDir); err != nil {
		return err
	}

	return nil
}

func encodePendingStart(start StartLoginResult) ([]byte, error) {
	encoded, err := json.Marshal(start)
	if err != nil {
		return nil, fmt.Errorf("encode pending oauth start: %w", err)
	}

	return encoded, nil
}

func decodePendingStart(raw []byte, start *StartLoginResult) error {
	if err := json.Unmarshal(raw, start); err != nil {
		return fmt.Errorf("decode pending oauth start: %w", err)
	}

	if strings.TrimSpace(start.State) == "" || strings.TrimSpace(start.CodeVerifier) == "" {
		return fmt.Errorf("invalid pending start payload")
	}

	return nil
}

func loadPendingStartFromLegacyFile(stateDir string) (StartLoginResult, error) {
	pendingPath := pendingStartPath(stateDir)
	raw, err := os.ReadFile(pendingPath)
	if err != nil {
		if os.IsNotExist(err) {
			return StartLoginResult{}, ErrPendingStartNotFound
		}

		return StartLoginResult{}, fmt.Errorf("read pending start file: %w", err)
	}

	var start StartLoginResult
	if err := decodePendingStart(raw, &start); err != nil {
		return StartLoginResult{}, err
	}

	return start, nil
}

func clearPendingStartLegacyFile(stateDir string) error {
	pendingPath := pendingStartPath(stateDir)
	if err := os.Remove(pendingPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove pending start file: %w", err)
	}

	return nil
}

func pendingStartPath(stateDir string) string {
	return filepath.Join(stateDir, "oauth", "pending-start.json")
}
