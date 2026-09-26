package screens

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// The replay script is documented as runnable (./mcp-tui-session-*.sh), so it
// must be executable; both files stay owner-only because events carry payloads.
func TestExportSession_ScriptIsOwnerExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	dir := t.TempDir()
	t.Chdir(dir)

	if msg, level := exportSession(&versionStubService{}); level != StatusSuccess {
		t.Fatalf("exportSession: %s", msg)
	}

	want := map[string]os.FileMode{".json": 0o600, ".sh": 0o700}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(want) {
		t.Fatalf("exported %d files, want %d", len(entries), len(want))
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want[filepath.Ext(e.Name())] {
			t.Errorf("%s mode = %o, want %o", e.Name(), got, want[filepath.Ext(e.Name())])
		}
	}
}
