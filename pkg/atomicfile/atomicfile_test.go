package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteCreatesTheFileWithTheGivenMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "secret.txt")

	if err := Write(path, []byte("contents"), 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "contents" {
		t.Errorf("file holds %q", data)
	}
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestWriteReplacesAnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "new" {
		t.Errorf("file holds %q, want the new contents", data)
	}
}

// The point of the rename is that a failure leaves the previous file intact.
// A plain write would already have truncated it by now.
func TestWriteLeavesTheOldFileWhenTheRenameFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "key.bin")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	previous := RenameFile
	RenameFile = func(string, string) error { return errors.New("rename failed") }
	t.Cleanup(func() { RenameFile = previous })

	if err := Write(path, []byte("replacement"), 0o600); err == nil {
		t.Fatal("Write reported success after the rename failed")
	}

	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Errorf("file holds %q, want the original contents", data)
	}
}

// A failed write must not leave its temporary file behind.
func TestWriteRemovesItsTemporaryFileOnFailure(t *testing.T) {
	dir := t.TempDir()

	previous := RenameFile
	RenameFile = func(string, string) error { return errors.New("rename failed") }
	t.Cleanup(func() { RenameFile = previous })

	if err := Write(filepath.Join(dir, "state.json"), []byte("data"), 0o600); err == nil {
		t.Fatal("Write reported success after the rename failed")
	}

	names, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Errorf("directory holds %d files, want none", len(names))
	}
}
