package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ShWords returns the arguments sh makes of words, a POSIX command line
// with the program name left off. The words go to sh in a script file, so
// no other layer (a Windows command line) quotes them first. On Windows sh
// is Git for Windows' (on the CI runners' PATH).
func ShWords(t *testing.T, words string) []string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "words.sh")
	if err := os.WriteFile(script, []byte(`printf '%s\0' `+words+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("sh", script).Output() //nolint:gosec // G204: script is this test's own temp file
	if err != nil {
		t.Fatalf("sh could not read %s: %v", words, err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
}
