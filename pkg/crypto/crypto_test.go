package crypto

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// resetKeyState clears the process-lifetime key cache so each test starts
// from a clean slate, and restores it afterward so tests don't leak state
// into each other or into non-crypto tests sharing the same test binary.
func resetKeyState(t *testing.T) {
	t.Helper()
	keyState = struct {
		once sync.Once
		key  []byte
		err  error
	}{}
	t.Cleanup(func() {
		keyState = struct {
			once sync.Once
			key  []byte
			err  error
		}{}
	})
}

func useTemporaryWorkingDirectory(t *testing.T) {
	t.Helper()
	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatalf("change working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
}

func TestEncryptDecryptRoundTripsWithoutAPassphrase(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	resetKeyState(t)

	ciphertext, err := Encrypt("super secret value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if strings.Contains(ciphertext, "super secret value") {
		t.Fatal("ciphertext contains the plaintext")
	}

	plaintext, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plaintext != "super secret value" {
		t.Fatalf("plaintext = %q, want the original value", plaintext)
	}

	if _, err := os.Stat(filepath.Join(keyDir, legacyKeyFileName)); err != nil {
		t.Fatalf("expected a plaintext key file when no passphrase is configured: %v", err)
	}
}

func TestEncryptDecryptRoundTripsWithAPassphrase(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	resetKeyState(t)
	t.Setenv(passphraseValueEnv, "correct horse battery staple")

	ciphertext, err := Encrypt("super secret value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	plaintext, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if plaintext != "super secret value" {
		t.Fatalf("plaintext = %q, want the original value", plaintext)
	}

	if _, err := os.Stat(filepath.Join(keyDir, wrappedKeyFileName)); err != nil {
		t.Fatalf("expected a wrapped key file when a passphrase is configured: %v", err)
	}
	if _, err := os.Stat(filepath.Join(keyDir, legacyKeyFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected no plaintext key file when a passphrase is configured, stat err = %v", err)
	}
}

func TestWrappedKeyFileDoesNotContainTheDEKInTheClear(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	resetKeyState(t)
	t.Setenv(passphraseValueEnv, "correct horse battery staple")

	if _, err := Encrypt("irrelevant"); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	dek, err := loadOrCreateKey()
	if err != nil {
		t.Fatalf("loadOrCreateKey: %v", err)
	}

	wrapped, err := os.ReadFile(filepath.Join(keyDir, wrappedKeyFileName))
	if err != nil {
		t.Fatalf("read wrapped key file: %v", err)
	}
	if strings.Contains(string(wrapped), string(dek)) {
		t.Fatal("wrapped key file contains the DEK in the clear")
	}
}

func TestWrappedKeyPersistsAcrossProcessRestarts(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	t.Setenv(passphraseValueEnv, "correct horse battery staple")

	resetKeyState(t)
	ciphertext, err := Encrypt("persisted value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	// Simulate a process restart: nothing on disk changes, but the in-memory
	// cache (and the pbkdf2 derivation) starts over.
	resetKeyState(t)
	plaintext, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt after simulated restart: %v", err)
	}
	if plaintext != "persisted value" {
		t.Fatalf("plaintext = %q, want the original value", plaintext)
	}
}

func TestWrappedKeyFailsToUnwrapWithTheWrongPassphrase(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	resetKeyState(t)
	t.Setenv(passphraseValueEnv, "correct horse battery staple")

	if _, err := Encrypt("irrelevant"); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	resetKeyState(t)
	t.Setenv(passphraseValueEnv, "wrong passphrase entirely")

	if _, err := loadOrCreateKey(); err == nil {
		t.Fatal("loadOrCreateKey succeeded with the wrong passphrase")
	}
}

func TestExistingPlaintextKeyIsMigratedWhenAPassphraseIsAdded(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	resetKeyState(t)

	// Establish a plaintext key and encrypt something under it, as if the
	// gateway had been running without a passphrase configured.
	ciphertext, err := Encrypt("pre-existing credential")
	if err != nil {
		t.Fatalf("Encrypt without a passphrase: %v", err)
	}

	// Now a passphrase is configured and the process restarts.
	resetKeyState(t)
	t.Setenv(passphraseValueEnv, "correct horse battery staple")

	plaintext, err := Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt after migration: %v", err)
	}
	if plaintext != "pre-existing credential" {
		t.Fatalf("plaintext = %q, want the pre-existing credential", plaintext)
	}

	if _, err := os.Stat(filepath.Join(keyDir, wrappedKeyFileName)); err != nil {
		t.Fatalf("expected the key to be migrated to wrapped storage: %v", err)
	}
	if _, err := os.Stat(filepath.Join(keyDir, legacyKeyFileName)); !os.IsNotExist(err) {
		t.Fatalf("expected the legacy plaintext key to be removed after migration, stat err = %v", err)
	}
}

func TestMasterPassphraseCanBeSuppliedByFile(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	resetKeyState(t)

	passphraseFile := filepath.Join(t.TempDir(), "passphrase.txt")
	if err := os.WriteFile(passphraseFile, []byte("  a passphrase from a file  \n"), 0600); err != nil {
		t.Fatalf("write passphrase file: %v", err)
	}
	t.Setenv(passphraseFileEnv, passphraseFile)

	if _, err := Encrypt("irrelevant"); err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := os.Stat(filepath.Join(keyDir, wrappedKeyFileName)); err != nil {
		t.Fatalf("expected a wrapped key file when M365_MASTER_PASSPHRASE_FILE is set: %v", err)
	}
}

func TestPassphraseValueEnvTakesPrecedenceOverFile(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	resetKeyState(t)

	passphraseFile := filepath.Join(t.TempDir(), "passphrase.txt")
	if err := os.WriteFile(passphraseFile, []byte("file passphrase"), 0600); err != nil {
		t.Fatalf("write passphrase file: %v", err)
	}
	t.Setenv(passphraseFileEnv, passphraseFile)
	t.Setenv(passphraseValueEnv, "value passphrase")

	passphrase, err := loadMasterPassphrase()
	if err != nil {
		t.Fatalf("loadMasterPassphrase: %v", err)
	}
	if passphrase != "value passphrase" {
		t.Fatalf("passphrase = %q, want the env value to take precedence over the file", passphrase)
	}
}

func TestKeyDerivationHappensAtMostOncePerProcess(t *testing.T) {
	useTemporaryWorkingDirectory(t)
	resetKeyState(t)
	t.Setenv(passphraseValueEnv, "correct horse battery staple")

	first, err := loadOrCreateKey()
	if err != nil {
		t.Fatalf("loadOrCreateKey: %v", err)
	}

	// Changing the passphrase after the first call must not affect the
	// cached key: if it did, the derivation would be running again on every
	// call, which is the exact per-request latency cost caching avoids.
	t.Setenv(passphraseValueEnv, "a different passphrase")

	second, err := loadOrCreateKey()
	if err != nil {
		t.Fatalf("loadOrCreateKey (second call): %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("loadOrCreateKey re-derived the key on a later call instead of returning the cached one")
	}
}
