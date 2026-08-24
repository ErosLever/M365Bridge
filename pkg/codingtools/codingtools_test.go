package codingtools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testManager(t *testing.T, workspace string) *Manager {
	t.Helper()
	m, err := New(Config{
		Enabled:       true,
		WorkspaceDir:  workspace,
		Timeout:       5 * time.Second,
		MaxOutput:     1 << 20,
		MaxReadBytes:  1 << 20,
		MaxIterations: 4,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

func TestSearchFilesFindsAMatchInsideTheWorkspace(t *testing.T) {
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "notes.txt"), []byte("alpha\nbeta needle gamma\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := testManager(t, workspace)

	out, _, err := m.searchFiles(map[string]any{"query": "needle"})
	if err != nil {
		t.Fatalf("searchFiles: %v", err)
	}
	if !strings.Contains(out, "notes.txt:2:beta needle gamma") {
		t.Errorf("searchFiles = %q", out)
	}
}

// The walk hands over a path, and by the time the file is opened that path may
// point somewhere else. Without the check inside readWalkedFile a search would
// report the contents of a file the workspace does not contain.
func TestReadWalkedFileRefusesAPathLeavingTheWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("needle"), 0o600); err != nil {
		t.Fatal(err)
	}

	workspace := t.TempDir()
	link := filepath.Join(workspace, "innocent.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	m := testManager(t, workspace)

	if _, err := m.readWalkedFile(link); err == nil {
		t.Fatal("readWalkedFile read a file outside the workspace")
	}

	// The whole search must refuse it too, not just the helper.
	out, _, err := m.searchFiles(map[string]any{"query": "needle"})
	if err != nil {
		t.Fatalf("searchFiles: %v", err)
	}
	if strings.Contains(out, "needle") {
		t.Errorf("a file outside the workspace reached the results: %q", out)
	}
}

func TestReadWalkedFileRefusesSomethingThatIsNotARegularFile(t *testing.T) {
	workspace := t.TempDir()
	sub := filepath.Join(workspace, "sub")
	if err := os.Mkdir(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	m := testManager(t, workspace)

	if _, err := m.readWalkedFile(sub); err == nil {
		t.Error("readWalkedFile accepted a directory")
	}
}

func TestReadWalkedFileReadsARegularFile(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "plain.txt")
	if err := os.WriteFile(path, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := testManager(t, workspace)

	data, err := m.readWalkedFile(path)
	if err != nil {
		t.Fatalf("readWalkedFile: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("read %q", data)
	}
}

// A file the coding tools write carries whatever the caller put in it, so it
// must not land readable by every user on the host.
func TestWriteFileCreatesAnOwnerOnlyFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits do not apply")
	}
	workspace := t.TempDir()
	m := testManager(t, workspace)

	if _, err := m.writeFile(map[string]any{"path": "nested/out.txt", "content": "data"}); err != nil {
		t.Fatalf("writeFile: %v", err)
	}
	info, err := os.Stat(filepath.Join(workspace, "nested", "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	dir, err := os.Stat(filepath.Join(workspace, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dir.Mode().Perm(); perm != 0o750 {
		t.Errorf("directory mode = %o, want 750", perm)
	}
}
