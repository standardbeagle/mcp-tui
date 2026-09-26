package privatefile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWrite_ReplacesWorldReadableFileWithOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, []byte("new")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Errorf("contents = %q, want %q", got, "new")
	}
	if runtime.GOOS != "windows" {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("mode = %o, want 600", perm)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d entries, want only the target (temp file left behind?)", len(entries))
	}
}

func TestWrite_MissingDirectoryFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent", "file.json")
	if err := Write(path, []byte("x")); err == nil {
		t.Fatal("Write into a missing directory succeeded, want error")
	}
}
