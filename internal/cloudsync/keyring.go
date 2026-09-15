package cloudsync

import "github.com/zalando/go-keyring"

// keyringService/keyringUser identify the one secret this package ever
// stores in the OS keychain (macOS Keychain / Windows Credential Manager) -
// the R2 Secret Access Key, deliberately never written to Settings' own
// plaintext settings.json since it grants write access to a bucket every
// technician shares.
const (
	keyringService = "PDT"
	keyringUser    = "r2-secret-access-key"
)

// SaveSecretKey stores secret in the OS keychain, overwriting whatever was
// stored before.
func SaveSecretKey(secret string) error {
	return keyring.Set(keyringService, keyringUser, secret)
}

// LoadSecretKey returns the stored secret, or "" (not an error) if nothing
// has been saved yet - the common case on a technician's first run before
// Settings has ever been filled in.
func LoadSecretKey() (string, error) {
	secret, err := keyring.Get(keyringService, keyringUser)
	if err == keyring.ErrNotFound {
		return "", nil
	}
	return secret, err
}

// HasSecretKey reports whether a secret is currently stored, without
// returning its value - what Settings' own GetSettings uses to show "a key
// is already configured" without ever re-displaying the key itself.
func HasSecretKey() bool {
	secret, err := LoadSecretKey()
	return err == nil && secret != ""
}

// DeleteSecretKey removes the stored secret, if any (a no-op, not an error,
// if nothing was stored).
func DeleteSecretKey() error {
	err := keyring.Delete(keyringService, keyringUser)
	if err == keyring.ErrNotFound {
		return nil
	}
	return err
}
