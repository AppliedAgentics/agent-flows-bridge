package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewStoreUsesMigratingKeychainStoreOnDarwinWhenAvailable(t *testing.T) {
	originalBuilder := buildBackendFunc
	t.Cleanup(func() {
		buildBackendFunc = originalBuilder
	})

	primaryStore := newFakeStore("keychain")
	buildBackendFunc = func(backend string, opts resolvedOptions) (Store, error) {
		switch backend {
		case "keychain":
			return primaryStore, nil
		case "file":
			return newFileStore(opts.StateDir)
		default:
			return nil, ErrUnsupportedBackend
		}
	}

	stateDir := t.TempDir()
	store, err := NewStore(Options{StateDir: stateDir, OS: "darwin"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	if store.Name() != "keychain" {
		t.Fatalf("expected keychain-backed store, got %s", store.Name())
	}

	if err := store.Save(context.Background(), "oauth_session", []byte("secret")); err != nil {
		t.Fatalf("save secret: %v", err)
	}

	if _, err := os.Stat(filepath.Join(stateDir, "secrets", "master.key")); !os.IsNotExist(err) {
		t.Fatalf("expected no legacy master.key on fresh save, got %v", err)
	}
}

func TestNewStoreReturnsWarningWhenDarwinFallsBackToFile(t *testing.T) {
	originalBuilder := buildBackendFunc
	t.Cleanup(func() {
		buildBackendFunc = originalBuilder
	})

	buildBackendFunc = func(backend string, opts resolvedOptions) (Store, error) {
		if backend == "keychain" {
			return nil, ErrUnsupportedBackend
		}
		return buildBackend(backend, opts)
	}

	store, err := NewStore(Options{StateDir: t.TempDir(), OS: "darwin"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	metadata := Describe(store)
	if metadata.Backend != "file" {
		t.Fatalf("expected file backend metadata, got %+v", metadata)
	}
	if strings.TrimSpace(metadata.Warning) == "" {
		t.Fatalf("expected fallback warning, got %+v", metadata)
	}
}

func TestMigratingStoreLoadsLegacyValueAndDeletesLegacyCopy(t *testing.T) {
	ctx := context.Background()
	primaryStore := newFakeStore("keychain")
	legacyStore := newFakeStore("file")
	legacyStore.values["oauth_session"] = []byte("secret-value")

	store := newMigratingStore(primaryStore, legacyStore)

	loaded, err := store.Load(ctx, "oauth_session")
	if err != nil {
		t.Fatalf("load migrated secret: %v", err)
	}

	if string(loaded) != "secret-value" {
		t.Fatalf("unexpected loaded secret: %q", string(loaded))
	}
	if string(primaryStore.values["oauth_session"]) != "secret-value" {
		t.Fatalf("expected primary store migration, got %+v", primaryStore.values)
	}
	if _, ok := legacyStore.values["oauth_session"]; ok {
		t.Fatalf("expected legacy secret removal, got %+v", legacyStore.values)
	}
}

func TestFileStoreSaveLoadDelete(t *testing.T) {
	store, err := NewStore(Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	ctx := context.Background()
	key := "connector_tokens"
	secret := []byte("super-secret-value")

	if err := store.Save(ctx, key, secret); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := store.Load(ctx, key)
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if string(loaded) != string(secret) {
		t.Fatalf("unexpected loaded secret: %q", string(loaded))
	}

	if err := store.Delete(ctx, key); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	_, err = store.Load(ctx, key)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestFileStoreUsesRestrictedPermissions(t *testing.T) {
	store, err := NewStore(Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	fileStore, ok := store.(*FileStore)
	if !ok {
		t.Fatalf("expected *FileStore, got %T", store)
	}

	ctx := context.Background()
	key := "connector_tokens"
	if err := fileStore.Save(ctx, key, []byte("secret")); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	path := fileStore.secretPath(key)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("expected file permissions to hide group/other access, got %o", info.Mode().Perm())
	}
}

func TestFileStoreEncryptsPayloadAtRest(t *testing.T) {
	store, err := NewStore(Options{StateDir: t.TempDir(), PreferredBackend: "file"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	fileStore, ok := store.(*FileStore)
	if !ok {
		t.Fatalf("expected *FileStore, got %T", store)
	}

	ctx := context.Background()
	key := "connector_tokens"
	secret := "plain-visible-token-value"

	if err := fileStore.Save(ctx, key, []byte(secret)); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	path := fileStore.secretPath(key)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read raw secret file: %v", err)
	}

	if strings.Contains(string(raw), secret) {
		t.Fatal("expected encrypted payload at rest, found plaintext secret bytes")
	}
}

type fakeStore struct {
	name   string
	values map[string][]byte
}

func newFakeStore(name string) *fakeStore {
	return &fakeStore{name: name, values: map[string][]byte{}}
}

func (f *fakeStore) Name() string {
	return f.name
}

func (f *fakeStore) Metadata() Metadata {
	return Metadata{Backend: f.name}
}

func (f *fakeStore) Save(_ context.Context, key string, value []byte) error {
	f.values[key] = append([]byte(nil), value...)
	return nil
}

func (f *fakeStore) Load(_ context.Context, key string) ([]byte, error) {
	value, ok := f.values[key]
	if !ok {
		return nil, ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (f *fakeStore) Delete(_ context.Context, key string) error {
	if _, ok := f.values[key]; !ok {
		return ErrNotFound
	}

	delete(f.values, key)
	return nil
}
