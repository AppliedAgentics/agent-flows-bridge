//go:build !darwin

package secrets

func newKeychainStore(_ string) (Store, error) {
	return nil, ErrUnsupportedBackend
}
