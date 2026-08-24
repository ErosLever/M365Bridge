package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreatePepperGeneratesAndPersists(t *testing.T) {
	useTemporaryWorkingDirectory(t)

	pepper, err := loadOrCreatePepper()
	if err != nil {
		t.Fatalf("loadOrCreatePepper: %v", err)
	}
	if len(pepper) != pepperSize {
		t.Fatalf("pepper length = %d, want %d", len(pepper), pepperSize)
	}

	path := filepath.Join(pepperDir, pepperFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected the pepper to be persisted at %s: %v", path, err)
	}
}

func TestLoadOrCreatePepperIsStableAcrossCalls(t *testing.T) {
	useTemporaryWorkingDirectory(t)

	first, err := loadOrCreatePepper()
	if err != nil {
		t.Fatalf("loadOrCreatePepper: %v", err)
	}
	second, err := loadOrCreatePepper()
	if err != nil {
		t.Fatalf("loadOrCreatePepper (second call): %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("loadOrCreatePepper returned a different pepper on a second call")
	}
}

func TestLoadOrCreatePepperRejectsAMalformedFile(t *testing.T) {
	useTemporaryWorkingDirectory(t)

	if err := os.MkdirAll(pepperDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(pepperDir, pepperFileName)
	if err := os.WriteFile(path, []byte("too short"), 0600); err != nil {
		t.Fatalf("write malformed pepper file: %v", err)
	}

	if _, err := loadOrCreatePepper(); err == nil {
		t.Fatal("loadOrCreatePepper accepted a malformed pepper file")
	}
}
