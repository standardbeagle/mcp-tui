package models

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A connections file written 0644 into a 0755 directory by an older
// release must come out owner-only after the next save.
func TestSaveConnections_TightensExistingPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := filepath.Join(t.TempDir(), "mcp-tui")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cm := newTestConnectionsManager(t)
	cm.filePath = path

	if err := cm.SaveConnections(); err != nil {
		t.Fatalf("SaveConnections: %v", err)
	}

	for p, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", filepath.Base(p), got, want)
		}
	}
}
