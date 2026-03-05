package oauth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentflows/agent-flows-bridge/client/internal/secrets"
)

func TestPendingStartRoundTripUsesSecretStore(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	store, err := secrets.NewStore(secrets.Options{StateDir: stateDir, PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	start := StartLoginResult{
		AuthorizeURL:  "https://example.test/oauth/authorize?...",
		State:         "state-123",
		CodeVerifier:  "verifier-123",
		RedirectURI:   "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:     77,
		OAuthClientID: "agent-flows-bridge",
	}

	if err := SavePendingStart(ctx, stateDir, store, start); err != nil {
		t.Fatalf("save pending start: %v", err)
	}

	loaded, err := LoadPendingStart(ctx, stateDir, store)
	if err != nil {
		t.Fatalf("load pending start: %v", err)
	}

	if loaded.State != start.State || loaded.CodeVerifier != start.CodeVerifier {
		t.Fatalf("unexpected loaded payload: %+v", loaded)
	}

	pendingPath := pendingStartPath(stateDir)
	if _, err := os.Stat(pendingPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no plaintext pending start file, got %v", err)
	}
}

func TestLoadPendingStartMigratesLegacyFileIntoSecretStore(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	store, err := secrets.NewStore(secrets.Options{StateDir: stateDir, PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	start := StartLoginResult{
		AuthorizeURL:  "https://example.test/oauth/authorize?...",
		State:         "state-123",
		CodeVerifier:  "verifier-123",
		RedirectURI:   "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:     77,
		OAuthClientID: "agent-flows-bridge",
	}

	raw, err := encodePendingStart(start)
	if err != nil {
		t.Fatalf("encode pending start: %v", err)
	}

	legacyPath := pendingStartPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("create oauth dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, raw, 0o600); err != nil {
		t.Fatalf("write legacy pending start: %v", err)
	}

	loaded, err := LoadPendingStart(ctx, stateDir, store)
	if err != nil {
		t.Fatalf("load pending start: %v", err)
	}

	if loaded.State != start.State || loaded.CodeVerifier != start.CodeVerifier {
		t.Fatalf("unexpected loaded payload: %+v", loaded)
	}

	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected legacy file removed after migration, got %v", err)
	}

	storedRaw, err := store.Load(ctx, pendingStartSecretKey)
	if err != nil {
		t.Fatalf("load migrated secret: %v", err)
	}

	var stored StartLoginResult
	if err := decodePendingStart(storedRaw, &stored); err != nil {
		t.Fatalf("decode migrated secret: %v", err)
	}

	if stored.State != start.State {
		t.Fatalf("expected stored state %q, got %q", start.State, stored.State)
	}
}

func TestLoadPendingStartReturnsNotFoundWhenMissing(t *testing.T) {
	ctx := context.Background()
	store, err := secrets.NewStore(secrets.Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	_, err = LoadPendingStart(ctx, t.TempDir(), store)
	if !errors.Is(err, ErrPendingStartNotFound) {
		t.Fatalf("expected ErrPendingStartNotFound, got %v", err)
	}
}

func TestClearPendingStartRemovesSecretAndLegacyFile(t *testing.T) {
	ctx := context.Background()
	stateDir := t.TempDir()
	store, err := secrets.NewStore(secrets.Options{StateDir: stateDir, PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	start := StartLoginResult{
		State:        "state-123",
		CodeVerifier: "verifier-123",
		RedirectURI:  "http://127.0.0.1:49200/oauth/callback",
		RuntimeID:    77,
	}

	if err := SavePendingStart(ctx, stateDir, store, start); err != nil {
		t.Fatalf("save pending start: %v", err)
	}

	legacyPath := pendingStartPath(stateDir)
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatalf("create oauth dir: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte(`{"state":"legacy","code_verifier":"legacy"}`), 0o600); err != nil {
		t.Fatalf("write legacy pending start: %v", err)
	}

	if err := ClearPendingStart(ctx, stateDir, store); err != nil {
		t.Fatalf("clear pending start: %v", err)
	}

	_, err = LoadPendingStart(ctx, stateDir, store)
	if !errors.Is(err, ErrPendingStartNotFound) {
		t.Fatalf("expected ErrPendingStartNotFound, got %v", err)
	}
}
