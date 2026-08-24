// Package crypto provides AES encryption/decryption for refresh tokens at rest.
// The encryption key is stored in the project data directory.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/KilimcininKorOglu/M365Bridge/pkg/atomicfile"
	"github.com/KilimcininKorOglu/M365Bridge/pkg/logging"
)

const (
	// legacyKeyFileName is the plaintext DEK file written before passphrase
	// wrapping existed. Still read for backward compatibility, and still
	// written when no passphrase is configured at all.
	legacyKeyFileName = "encryption.key"
	// wrappedKeyFileName holds the DEK once a master passphrase is configured.
	// The .enc extension makes it visually obvious, next to legacyKeyFileName,
	// that this file's contents are not a usable key on their own.
	wrappedKeyFileName = "encryption.key.enc"
	// keyDir is the directory where the encryption key is stored.
	keyDir = "data/tokens"
	// pbkdf2Iterations follows OWASP's current PBKDF2-HMAC-SHA256 guidance.
	// The derivation runs at most once per process lifetime (loadOrCreateKey
	// caches the result), so its cost never lands on a request path.
	pbkdf2Iterations = 600_000
	// dekSize is 32 bytes for AES-256, matching the unwrapped key length.
	dekSize = 32

	// passphraseValueEnv holds the passphrase itself, set by the
	// scripts/with-passphrase.{sh,ps1} wrappers for one child process only.
	passphraseValueEnv = "M365_MASTER_PASSPHRASE_VALUE"
	// passphraseFileEnv names a file whose contents are the passphrase,
	// mirroring the M365_PROVISION_SECRET_FILE convention used for the
	// browser-extension provisioning secret.
	passphraseFileEnv = "M365_MASTER_PASSPHRASE_FILE"
)

// keyState caches the resolved DEK for the process lifetime. Deriving a KEK
// from a passphrase (pbkdf2Iterations) is deliberately slow, and
// loadOrCreateKey sits on the hot path of every Encrypt/Decrypt call, so the
// derivation must happen at most once per process rather than once per call.
var keyState struct {
	once sync.Once
	key  []byte
	err  error
}

// TokenRefreshError is returned when token refresh operations fail.
var (
	ErrKeyGeneration     = errors.New("failed to generate encryption key")
	ErrEncryption        = errors.New("encryption failed")
	ErrDecryption        = errors.New("decryption failed")
	ErrInvalidKey        = errors.New("invalid key length")
	ErrInvalidCiphertext = errors.New("invalid ciphertext")
)

// loadOrCreateKey returns the DEK (data-encryption key) used by Encrypt and
// Decrypt, deriving and caching it at most once per process lifetime.
func loadOrCreateKey() ([]byte, error) {
	keyState.once.Do(func() {
		keyState.key, keyState.err = resolveKey()
	})
	return keyState.key, keyState.err
}

// resolveKey loads or creates the DEK. With a master passphrase configured,
// the DEK is generated once and persisted wrapped (encrypted) under a KEK
// derived from the passphrase and the pepper; without one, it falls back to
// the original plaintext-on-disk behavior, loudly.
func resolveKey() ([]byte, error) {
	passphrase, err := loadMasterPassphrase()
	if err != nil {
		return nil, err
	}

	if passphrase == "" {
		logging.Warn("crypto: SECURITY WARNING: no master passphrase configured " +
			"(set " + passphraseValueEnv + " or " + passphraseFileEnv + "); " +
			"the encryption key is stored in the clear at " +
			filepath.Join(keyDir, legacyKeyFileName) + ". Anyone who can read " +
			"that file can decrypt every credential this gateway stores.")
		return loadOrCreatePlaintextKey()
	}

	return loadOrCreateWrappedKey(passphrase)
}

// loadMasterPassphrase resolves the master passphrase from the environment.
// M365_MASTER_PASSPHRASE_VALUE (set by the with-passphrase wrapper scripts)
// takes precedence over M365_MASTER_PASSPHRASE_FILE. Returns "" if neither is
// set, which callers treat as "no passphrase configured."
func loadMasterPassphrase() (string, error) {
	if value := strings.TrimSpace(os.Getenv(passphraseValueEnv)); value != "" {
		return value, nil
	}

	path := strings.TrimSpace(os.Getenv(passphraseFileEnv))
	if path == "" {
		return "", nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", passphraseFileEnv, err)
	}
	return strings.TrimSpace(string(data)), nil
}

// loadOrCreatePlaintextKey is the pre-passphrase behavior: the DEK itself,
// unencrypted, on disk. Kept for compatibility when no passphrase is set.
func loadOrCreatePlaintextKey() ([]byte, error) {
	path := filepath.Join(keyDir, legacyKeyFileName)

	// The path is this process's own key file under the gitignored data/
	// tree, never a caller-supplied value.
	// #nosec G304
	if keyData, err := os.ReadFile(path); err == nil {
		logging.Debug("crypto: loaded existing plaintext encryption key")
		return keyData, nil
	}

	logging.Info("crypto: creating new plaintext encryption key")
	key := make([]byte, dekSize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrKeyGeneration, err)
	}

	// A half-written key decrypts nothing, and every credential on disk is
	// encrypted under it, so this file above all others is replaced in one step.
	if err := atomicfile.Write(path, key, 0600); err != nil {
		return nil, fmt.Errorf("failed to write key file: %w", err)
	}

	return key, nil
}

// loadOrCreateWrappedKey loads the DEK from the passphrase-wrapped key file,
// or generates one and persists it wrapped. If a legacy plaintext key already
// exists, it is reused as the DEK (and wrapped going forward) rather than
// silently orphaning every credential already encrypted under it.
func loadOrCreateWrappedKey(passphrase string) ([]byte, error) {
	wrappedPath := filepath.Join(keyDir, wrappedKeyFileName)

	// #nosec G304 -- this process's own key file under the gitignored data/ tree.
	if wrapped, err := os.ReadFile(wrappedPath); err == nil {
		return unwrapKey(wrapped, passphrase)
	}

	var dek []byte
	migratingLegacy := false
	legacyPath := filepath.Join(keyDir, legacyKeyFileName)
	// #nosec G304
	if legacyKey, err := os.ReadFile(legacyPath); err == nil {
		logging.Warn("crypto: migrating existing plaintext encryption key to passphrase-wrapped storage at " + wrappedPath)
		dek = legacyKey
		migratingLegacy = true
	} else {
		logging.Info("crypto: creating new passphrase-wrapped encryption key")
		dek = make([]byte, dekSize)
		if _, err := rand.Read(dek); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrKeyGeneration, err)
		}
	}

	wrapped, err := wrapKey(dek, passphrase)
	if err != nil {
		return nil, err
	}
	if err := atomicfile.Write(wrappedPath, wrapped, 0600); err != nil {
		return nil, fmt.Errorf("failed to write wrapped key file: %w", err)
	}
	if migratingLegacy {
		if err := os.Remove(legacyPath); err != nil {
			logging.Warnf("crypto: wrapped key written but could not remove legacy plaintext key %s: %v", legacyPath, err)
		}
	}

	return dek, nil
}

// deriveKEK derives a key-encryption key from the master passphrase and the
// pepper via PBKDF2-HMAC-SHA256. The pepper acts as the salt: unlike a salt it
// is itself secret and kept out of the file the wrapped DEK lives in.
func deriveKEK(passphrase string) ([]byte, error) {
	pepper, err := loadOrCreatePepper()
	if err != nil {
		return nil, err
	}
	return pbkdf2.Key(sha256.New, passphrase, pepper, pbkdf2Iterations, dekSize)
}

// wrapKey encrypts the DEK under the passphrase-derived KEK using AES-GCM.
func wrapKey(dek []byte, passphrase string) ([]byte, error) {
	kek, err := deriveKEK(passphrase)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEncryption, err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("%w: failed to generate nonce", ErrEncryption)
	}

	return gcm.Seal(nonce, nonce, dek, nil), nil
}

// unwrapKey decrypts a DEK previously wrapped by wrapKey.
func unwrapKey(wrapped []byte, passphrase string) ([]byte, error) {
	kek, err := deriveKEK(passphrase)
	if err != nil {
		return nil, err
	}

	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDecryption, err)
	}

	nonceSize := gcm.NonceSize()
	if len(wrapped) < nonceSize {
		return nil, fmt.Errorf("%w: wrapped key too short", ErrInvalidCiphertext)
	}
	nonce, ciphertext := wrapped[:nonceSize], wrapped[nonceSize:]

	dek, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: wrong passphrase or pepper, or corrupted key file: %v", ErrDecryption, err)
	}
	return dek, nil
}

// Encrypt encrypts plaintext using AES-GCM.
// Returns base64-encoded ciphertext.
func Encrypt(plaintext string) (string, error) {
	key, err := loadOrCreateKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrEncryption, err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("%w: failed to generate nonce", ErrEncryption)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64-encoded ciphertext using AES-GCM.
// Returns the original plaintext.
func Decrypt(ciphertext string) (string, error) {
	key, err := loadOrCreateKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDecryption, err)
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("%w: invalid base64", ErrInvalidCiphertext)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("%w: ciphertext too short", ErrInvalidCiphertext)
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrDecryption, err)
	}

	return string(plaintext), nil
}
