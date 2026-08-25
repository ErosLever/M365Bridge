// Package atomicfile writes a file so a reader never sees a partial one.
//
// Every file this gateway keeps is read back by the next process that starts:
// the encryption key, the credentials it protects, the session mapping and the
// transcripts. A plain write truncates the file first, so a crash or a full
// disk in the middle of it leaves a shorter file that still parses as itself.
// A half-written encryption key makes every stored credential unreadable.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// RenameFile is the rename step, exposed so a test can make it fail. Production
// code never replaces it.
var RenameFile = os.Rename

// Write creates path with the given contents and mode, and replaces any
// existing file in one step.
//
// The data goes to a temporary file in the same directory, which is synced and
// closed before the rename. A rename within one directory either happens or
// does not, so a reader sees the old file or the new one and never a mixture.
// The directory is synced after the rename, because syncing the file commits
// its contents and not the name that points at them.
func Write(path string, data []byte, mode os.FileMode) (returnErr error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	temporary, err := os.CreateTemp(dir, ".atomic-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer func() {
		if returnErr != nil {
			_ = temporary.Close()
			_ = os.Remove(temporaryPath)
		}
	}()

	// CreateTemp makes the file 0600. Chmod applies the caller's mode before
	// anything is written, so the contents are never readable under a wider one.
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("failed to set temporary file permissions: %w", err)
	}
	if _, err := temporary.Write(data); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("failed to sync temporary file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %w", err)
	}
	if err := RenameFile(temporaryPath, path); err != nil {
		return fmt.Errorf("failed to replace file: %w", err)
	}
	// The rename itself lives in the directory, and syncing the file does not
	// commit it. Without this a power cut just after a successful write can
	// leave the directory entry pointing at the old file, so the write is lost
	// rather than torn. A filesystem that refuses to sync a directory is not a
	// failed write, so the error is logged by the caller's absence of one: the
	// data is already on disk either way.
	syncDir(dir)
	return nil
}

// syncDir commits a rename in dir. The error is deliberately dropped: the file
// contents are already synced, some platforms refuse to open a directory for
// sync at all, and failing a write that succeeded would be the worse outcome.
func syncDir(dir string) {
	handle, err := os.Open(dir)
	if err != nil {
		return
	}
	defer func() { _ = handle.Close() }()
	_ = handle.Sync()
}
