package testutil

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestPsQuote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"plain", "'plain'"},
		{"with space", "'with space'"},
		{"it's", "'it''s'"},
		{`C:\Users\tmp\x`, `'C:\Users\tmp\x'`},
		{"", "''"},
	}
	for _, tt := range tests {
		if got := psQuote(tt.in); got != tt.want {
			t.Errorf("psQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The scripts must never carry a character that mcp-tui's own ValidateCommand
// denylist rejects, or the stdio transport will refuse to launch them.
func TestScriptArgsCarryNoDeniedMetacharacters(t *testing.T) {
	_, args := Script(t, "probe", "exit 0\n")

	denied := []string{";", "&&", "||", "|", ">", "<", "$(", "`", "${", "../"}
	for _, arg := range args {
		for _, pattern := range denied {
			if strings.Contains(arg, pattern) {
				t.Errorf("argument %q contains denied pattern %q", arg, pattern)
			}
		}
	}
}

func TestServerFailsWithStderr(t *testing.T) {
	command, args := ServerFailsWithStderr(t, "Error: REQUIRED_VAR environment variable is required")

	out, err := exec.Command(command, args...).CombinedOutput()
	if err == nil {
		t.Fatal("expected a non-zero exit")
	}
	if !strings.Contains(string(out), "REQUIRED_VAR environment variable is required") {
		t.Errorf("stderr not propagated; got %q", out)
	}
}

func TestServerExitsImmediately(t *testing.T) {
	command, args := ServerExitsImmediately(t)

	out, err := exec.Command(command, args...).Output()
	if err != nil {
		t.Fatalf("expected a clean exit, got %v", err)
	}
	if !strings.Contains(string(out), "test") {
		t.Errorf("stdout not propagated; got %q", out)
	}
}

func TestServerRecordsInvocation(t *testing.T) {
	counter := t.TempDir() + string(os.PathSeparator) + "invocations"
	command, args := ServerRecordsInvocation(t, counter, 0)

	if err := exec.Command(command, args...).Run(); err != nil {
		t.Fatalf("running recorder: %v", err)
	}
	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatalf("reading counter: %v", err)
	}
	if got := strings.Count(string(data), "x"); got != 1 {
		t.Errorf("recorded %d invocations, want 1", got)
	}
}

// A quoted message containing a single quote must survive intact.
func TestServerFailsWithStderrQuoting(t *testing.T) {
	command, args := ServerFailsWithStderr(t, "it's broken")

	out, _ := exec.Command(command, args...).CombinedOutput()
	if !strings.Contains(string(out), "it's broken") {
		t.Errorf("quoting mangled the message; got %q", out)
	}
}
