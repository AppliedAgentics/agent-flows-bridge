package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// ErrUnsupportedBackend indicates a backend cannot run on this platform.
var ErrUnsupportedBackend = errors.New("unsupported secrets backend")

// ErrNotFound indicates a secret key has no stored value.
var ErrNotFound = errors.New("secret not found")

// ErrInvalidKey indicates a secret key contains unsupported characters.
var ErrInvalidKey = errors.New("invalid secret key")

// Store provide secret persistence operations.
type Store interface {
	Name() string
	Save(ctx context.Context, key string, value []byte) error
	Load(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
}

// Metadata describe the selected secret backend and any operator warning.
type Metadata struct {
	Backend string
	Warning string
}

type metadataProvider interface {
	Metadata() Metadata
}

// Options configure store selection and paths.
type Options struct {
	StateDir         string
	ServiceName      string
	PreferredBackend string
	OS               string
}

var buildBackendFunc = buildBackend

// NewStore create a secrets store with native-first fallback behavior.
//
// Selection order is preferred backend when provided, otherwise native backend
// by OS. Unsupported native backends automatically fall back to file storage.
//
// Returns a Store implementation or an error.
func NewStore(opts Options) (Store, error) {
	resolvedOpts, err := resolveOptions(opts)
	if err != nil {
		return nil, err
	}

	if resolvedOpts.PreferredBackend != "" {
		store, err := buildBackendFunc(resolvedOpts.PreferredBackend, resolvedOpts)
		if err == nil {
			return store, nil
		}

		if !errors.Is(err, ErrUnsupportedBackend) {
			return nil, err
		}
	}

	nativeBackend := backendFromOS(resolvedOpts.OS)
	if nativeBackend != "" {
		store, err := buildBackendFunc(nativeBackend, resolvedOpts)
		if err == nil {
			if resolvedOpts.PreferredBackend == "" && nativeBackend == "keychain" {
				return newMigratingStore(store, newLazyFileStore(resolvedOpts.StateDir)), nil
			}
			return store, nil
		}

		if !errors.Is(err, ErrUnsupportedBackend) {
			return nil, err
		}
	}

	fileStore, err := newFileStore(resolvedOpts.StateDir)
	if err != nil {
		return nil, err
	}

	if resolvedOpts.PreferredBackend == "" && nativeBackend == "keychain" {
		return newWarningStore(
			fileStore,
			"macOS Keychain unavailable; using file-backed secrets in the bridge state directory.",
		), nil
	}

	return fileStore, nil
}

type resolvedOptions struct {
	StateDir         string
	ServiceName      string
	PreferredBackend string
	OS               string
}

func resolveOptions(opts Options) (resolvedOptions, error) {
	stateDir := strings.TrimSpace(opts.StateDir)
	if stateDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return resolvedOptions{}, fmt.Errorf("resolve user home: %w", err)
		}

		stateDir = filepath.Join(homeDir, ".agent-flows-bridge")
	}

	serviceName := strings.TrimSpace(opts.ServiceName)
	if serviceName == "" {
		serviceName = "agent-flows-bridge"
	}

	osName := strings.TrimSpace(opts.OS)
	if osName == "" {
		osName = runtime.GOOS
	}

	preferredBackend := strings.TrimSpace(strings.ToLower(opts.PreferredBackend))
	if preferredBackend != "" && !isAllowedBackend(preferredBackend) {
		return resolvedOptions{}, fmt.Errorf("unknown secrets backend %q", preferredBackend)
	}

	resolved := resolvedOptions{
		StateDir:         stateDir,
		ServiceName:      serviceName,
		PreferredBackend: preferredBackend,
		OS:               osName,
	}

	return resolved, nil
}

func isAllowedBackend(name string) bool {
	switch name {
	case "file", "keychain", "credential-manager", "secret-service":
		return true
	default:
		return false
	}
}

func backendFromOS(osName string) string {
	switch osName {
	case "darwin":
		return "keychain"
	case "windows":
		return "credential-manager"
	case "linux":
		return "secret-service"
	default:
		return ""
	}
}

func buildBackend(backend string, opts resolvedOptions) (Store, error) {
	switch backend {
	case "file":
		return newFileStore(opts.StateDir)
	case "keychain":
		return newKeychainStore(opts.ServiceName)
	case "credential-manager", "secret-service":
		return nil, ErrUnsupportedBackend
	default:
		return nil, fmt.Errorf("unknown secrets backend %q", backend)
	}
}

// Describe return backend metadata for a store implementation.
//
// Stores can optionally expose warning metadata beyond the backend name.
//
// Returns backend metadata for status and diagnostics surfaces.
func Describe(store Store) Metadata {
	if store == nil {
		return Metadata{}
	}

	if provider, ok := store.(metadataProvider); ok {
		metadata := provider.Metadata()
		if strings.TrimSpace(metadata.Backend) == "" {
			metadata.Backend = store.Name()
		}
		return metadata
	}

	return Metadata{Backend: store.Name()}
}

type lazyFileStore struct {
	stateDir string
	once     sync.Once
	store    *FileStore
	err      error
}

func newLazyFileStore(stateDir string) *lazyFileStore {
	return &lazyFileStore{stateDir: stateDir}
}

func (l *lazyFileStore) Name() string {
	return "file"
}

func (l *lazyFileStore) Metadata() Metadata {
	return Metadata{Backend: "file"}
}

func (l *lazyFileStore) Save(ctx context.Context, key string, value []byte) error {
	store, err := l.ensure()
	if err != nil {
		return err
	}

	return store.Save(ctx, key, value)
}

func (l *lazyFileStore) Load(ctx context.Context, key string) ([]byte, error) {
	store, err := l.ensure()
	if err != nil {
		return nil, err
	}

	return store.Load(ctx, key)
}

func (l *lazyFileStore) Delete(ctx context.Context, key string) error {
	store, err := l.ensure()
	if err != nil {
		return err
	}

	return store.Delete(ctx, key)
}

func (l *lazyFileStore) ensure() (*FileStore, error) {
	l.once.Do(func() {
		l.store, l.err = newFileStore(l.stateDir)
	})

	if l.err != nil {
		return nil, l.err
	}

	return l.store, nil
}

type migratingStore struct {
	primary Store
	legacy  Store
}

func newMigratingStore(primary Store, legacy Store) *migratingStore {
	return &migratingStore{primary: primary, legacy: legacy}
}

func (m *migratingStore) Name() string {
	return m.primary.Name()
}

func (m *migratingStore) Metadata() Metadata {
	return Describe(m.primary)
}

func (m *migratingStore) Save(ctx context.Context, key string, value []byte) error {
	return m.primary.Save(ctx, key, value)
}

func (m *migratingStore) Load(ctx context.Context, key string) ([]byte, error) {
	value, err := m.primary.Load(ctx, key)
	if err == nil {
		return value, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	if m.legacy == nil {
		return nil, ErrNotFound
	}

	legacyValue, legacyErr := m.legacy.Load(ctx, key)
	if legacyErr != nil {
		return nil, legacyErr
	}

	if saveErr := m.primary.Save(ctx, key, legacyValue); saveErr != nil {
		return nil, saveErr
	}
	if deleteErr := m.legacy.Delete(ctx, key); deleteErr != nil && !errors.Is(deleteErr, ErrNotFound) {
		return nil, deleteErr
	}

	return legacyValue, nil
}

func (m *migratingStore) Delete(ctx context.Context, key string) error {
	primaryErr := m.primary.Delete(ctx, key)
	legacyErr := ErrNotFound
	if m.legacy != nil {
		legacyErr = m.legacy.Delete(ctx, key)
	}

	if primaryErr == nil {
		if legacyErr != nil && !errors.Is(legacyErr, ErrNotFound) {
			return legacyErr
		}
		return nil
	}

	if errors.Is(primaryErr, ErrNotFound) {
		if legacyErr == nil || errors.Is(legacyErr, ErrNotFound) {
			return nil
		}
		return legacyErr
	}

	return primaryErr
}

type warningStore struct {
	Store
	warning string
}

func newWarningStore(store Store, warning string) *warningStore {
	return &warningStore{Store: store, warning: strings.TrimSpace(warning)}
}

func (w *warningStore) Metadata() Metadata {
	metadata := Describe(w.Store)
	if strings.TrimSpace(metadata.Backend) == "" {
		metadata.Backend = w.Store.Name()
	}
	metadata.Warning = w.warning
	return metadata
}

// FileStore persist secrets under the local state directory.
type FileStore struct {
	rootDir   string
	masterKey []byte
}

func newFileStore(stateDir string) (*FileStore, error) {
	rootDir := filepath.Join(stateDir, "secrets")
	if err := os.MkdirAll(rootDir, 0o700); err != nil {
		return nil, fmt.Errorf("create secrets directory: %w", err)
	}

	masterKey, err := loadOrCreateMasterKey(rootDir)
	if err != nil {
		return nil, err
	}

	return &FileStore{rootDir: rootDir, masterKey: masterKey}, nil
}

// Name return the backend label.
func (f *FileStore) Name() string {
	return "file"
}

func (f *FileStore) Metadata() Metadata {
	return Metadata{Backend: "file"}
}

// Save write a secret value for a key.
//
// Values are written with restrictive permissions and atomic rename to reduce
// partial write risks.
//
// Returns nil on success or an error.
func (f *FileStore) Save(_ context.Context, key string, value []byte) error {
	secretPath := f.secretPath(key)
	if secretPath == "" {
		return ErrInvalidKey
	}

	encryptedValue, err := f.encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt secret value: %w", err)
	}

	tempFile, err := os.CreateTemp(f.rootDir, "secret-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp secret file: %w", err)
	}

	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(0o600); err != nil {
		tempFile.Close()
		return fmt.Errorf("chmod temp secret file: %w", err)
	}

	if _, err := tempFile.Write(encryptedValue); err != nil {
		tempFile.Close()
		return fmt.Errorf("write temp secret file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp secret file: %w", err)
	}

	if err := os.Rename(tempPath, secretPath); err != nil {
		return fmt.Errorf("persist secret file: %w", err)
	}

	if err := os.Chmod(secretPath, 0o600); err != nil {
		return fmt.Errorf("chmod secret file: %w", err)
	}

	return nil
}

// Load read a secret value for a key.
//
// Returns ErrNotFound when the key has no persisted value.
//
// Returns the secret bytes or an error.
func (f *FileStore) Load(_ context.Context, key string) ([]byte, error) {
	secretPath := f.secretPath(key)
	if secretPath == "" {
		return nil, ErrInvalidKey
	}

	value, err := os.ReadFile(secretPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("read secret file: %w", err)
	}

	decryptedValue, err := f.decrypt(value)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret value: %w", err)
	}

	return decryptedValue, nil
}

// Delete remove a secret value by key.
//
// Returns ErrNotFound when the key has no persisted value.
//
// Returns nil on success or an error.
func (f *FileStore) Delete(_ context.Context, key string) error {
	secretPath := f.secretPath(key)
	if secretPath == "" {
		return ErrInvalidKey
	}

	if err := os.Remove(secretPath); err != nil {
		if os.IsNotExist(err) {
			return ErrNotFound
		}

		return fmt.Errorf("delete secret file: %w", err)
	}

	return nil
}

func (f *FileStore) secretPath(key string) string {
	sanitizedKey, ok := sanitizeKey(key)
	if !ok {
		return ""
	}

	return filepath.Join(f.rootDir, sanitizedKey+".secret")
}

func sanitizeKey(key string) (string, bool) {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return "", false
	}

	for _, char := range trimmedKey {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			continue
		}

		if char == '-' || char == '_' || char == '.' {
			continue
		}

		return "", false
	}

	return trimmedKey, true
}

func loadOrCreateMasterKey(rootDir string) ([]byte, error) {
	keyPath := filepath.Join(rootDir, "master.key")

	existingKey, err := os.ReadFile(keyPath)
	if err == nil {
		if len(existingKey) != 32 {
			return nil, fmt.Errorf("invalid master key length %d", len(existingKey))
		}
		return existingKey, nil
	}

	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read master key: %w", err)
	}

	newKey := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, newKey); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}

	tempFile, err := os.CreateTemp(rootDir, "master-key-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create temp master key file: %w", err)
	}

	tempPath := tempFile.Name()
	defer os.Remove(tempPath)

	if err := tempFile.Chmod(0o600); err != nil {
		tempFile.Close()
		return nil, fmt.Errorf("chmod temp master key file: %w", err)
	}

	if _, err := tempFile.Write(newKey); err != nil {
		tempFile.Close()
		return nil, fmt.Errorf("write temp master key file: %w", err)
	}

	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("close temp master key file: %w", err)
	}

	if err := os.Rename(tempPath, keyPath); err != nil {
		return nil, fmt.Errorf("persist master key file: %w", err)
	}

	if err := os.Chmod(keyPath, 0o600); err != nil {
		return nil, fmt.Errorf("chmod master key file: %w", err)
	}

	return newKey, nil
}

func (f *FileStore) encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(f.masterKey)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}

	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	return append(nonce, ciphertext...), nil
}

func (f *FileStore) decrypt(payload []byte) ([]byte, error) {
	block, err := aes.NewCipher(f.masterKey)
	if err != nil {
		return nil, err
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	if len(payload) < aead.NonceSize() {
		return nil, fmt.Errorf("encrypted payload too short")
	}

	nonce := payload[:aead.NonceSize()]
	ciphertext := payload[aead.NonceSize():]

	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}

	return plaintext, nil
}
