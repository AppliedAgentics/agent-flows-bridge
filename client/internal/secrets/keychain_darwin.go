//go:build darwin

package secrets

import (
	"context"
	"fmt"
	"strings"

	keyring "github.com/zalando/go-keyring"
)

type keychainStore struct {
	serviceName string
}

func newKeychainStore(serviceName string) (Store, error) {
	trimmedServiceName := strings.TrimSpace(serviceName)
	if trimmedServiceName == "" {
		return nil, fmt.Errorf("service name is required")
	}

	return &keychainStore{serviceName: trimmedServiceName}, nil
}

func (k *keychainStore) Name() string {
	return "keychain"
}

func (k *keychainStore) Metadata() Metadata {
	return Metadata{Backend: "keychain"}
}

func (k *keychainStore) Save(_ context.Context, key string, value []byte) error {
	sanitizedKey, ok := sanitizeKey(key)
	if !ok {
		return ErrInvalidKey
	}

	if err := keyring.Set(k.serviceName, sanitizedKey, string(value)); err != nil {
		return fmt.Errorf("save keychain secret: %w", err)
	}

	return nil
}

func (k *keychainStore) Load(_ context.Context, key string) ([]byte, error) {
	sanitizedKey, ok := sanitizeKey(key)
	if !ok {
		return nil, ErrInvalidKey
	}

	value, err := keyring.Get(k.serviceName, sanitizedKey)
	if err != nil {
		if err == keyring.ErrNotFound {
			return nil, ErrNotFound
		}

		return nil, fmt.Errorf("load keychain secret: %w", err)
	}

	return []byte(value), nil
}

func (k *keychainStore) Delete(_ context.Context, key string) error {
	sanitizedKey, ok := sanitizeKey(key)
	if !ok {
		return ErrInvalidKey
	}

	if err := keyring.Delete(k.serviceName, sanitizedKey); err != nil {
		if err == keyring.ErrNotFound {
			return ErrNotFound
		}

		return fmt.Errorf("delete keychain secret: %w", err)
	}

	return nil
}
