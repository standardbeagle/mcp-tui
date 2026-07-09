// Package testutil provides cross-platform helpers for tests that need to spawn
// a real child process -- typically a stand-in MCP server.
//
// Tests used to spawn `sh -c ...`, `python3 -c ...` and bare `echo`, none of
// which exist (or behave the same) on Windows. PowerShell 7 (`pwsh`) runs
// identically on Linux, macOS and Windows and is preinstalled on all three
// GitHub-hosted runner images, so it is the single scripting host used here.
//
// Scripts are always written to a file and invoked with -File rather than
// passed inline with -Command. mcp-tui's own ValidateCommand rejects arguments
// containing shell metacharacters (';', '|', '>', '$(', '`'), which a
// non-trivial inline script inevitably contains. A file path carries none.
package testutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// PwshExe is the PowerShell 7 executable. It is named the same on every
// platform; on Windows exec.LookPath resolves it via PATHEXT.
const PwshExe = "pwsh"

// requirePwshEnv makes a missing pwsh a hard failure rather than a skip.
// CI sets it so that the cross-platform tests can never silently stop running.
const requirePwshEnv = "MCP_TUI_REQUIRE_PWSH"

// LookPwsh returns the path to pwsh, or an error if it is not installed.
func LookPwsh() (string, error) {
	return exec.LookPath(PwshExe)
}

// RequirePwsh skips the test when pwsh is unavailable, unless MCP_TUI_REQUIRE_PWSH
// is set, in which case the absence is a failure. Skipping keeps the suite usable
// on a machine without PowerShell; the env var stops CI from quietly skipping.
func RequirePwsh(t *testing.T) {
	t.Helper()
	if _, err := LookPwsh(); err != nil {
		if os.Getenv(requirePwshEnv) != "" {
			t.Fatalf("pwsh is required (%s is set) but was not found: %v", requirePwshEnv, err)
		}
		t.Skipf("pwsh not installed; skipping cross-platform process test: %v", err)
	}
}

// ScriptPath writes body to a .ps1 file in the test's temp directory and returns
// its path. The file is removed when the test ends.
func ScriptPath(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name+".ps1")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

// Script returns the command and arguments that run body as a PowerShell script.
// Use it wherever a test needs a child process that behaves identically on
// every platform.
func Script(t *testing.T, name, body string) (command string, args []string) {
	t.Helper()
	RequirePwsh(t)
	return PwshExe, []string{"-NoProfile", "-NonInteractive", "-File", ScriptPath(t, name, body)}
}

// ServerExitsImmediately is a stand-in MCP server that writes a line to stdout
// and exits 0 without speaking the protocol. It replaces the bare `echo` that
// tests previously used as a command.
//
// The scripts here write through [Console] rather than Write-Output/Add-Content:
// the cmdlets engage PowerShell's object-formatting pipeline, which costs about
// a second of startup per spawn. The raw .NET writers cost ~0.44s, the same as
// an empty script.
func ServerExitsImmediately(t *testing.T) (command string, args []string) {
	t.Helper()
	return Script(t, "exits-immediately", "[Console]::Out.WriteLine('test')\nexit 0\n")
}

// ServerFailsWithStderr is a stand-in MCP server that writes msg to stderr and
// exits non-zero, modelling a server that dies during startup.
func ServerFailsWithStderr(t *testing.T, msg string) (command string, args []string) {
	t.Helper()
	body := "[Console]::Error.WriteLine(" + psQuote(msg) + ")\nexit 1\n"
	return Script(t, "fails-with-stderr", body)
}

// ServerSleeps is a stand-in MCP server that stays alive for the given number of
// seconds without speaking the protocol.
func ServerSleeps(t *testing.T, seconds float64) (command string, args []string) {
	t.Helper()
	body := "Start-Sleep -Seconds " + strconv.FormatFloat(seconds, 'f', -1, 64) + "\n"
	return Script(t, "sleeps", body)
}

// ServerPrintsThenSleeps writes msg to stdout and then stays alive, modelling a
// server that announces itself but never speaks MCP.
func ServerPrintsThenSleeps(t *testing.T, msg string, seconds float64) (command string, args []string) {
	t.Helper()
	body := "[Console]::Out.WriteLine(" + psQuote(msg) + ")\n" +
		"Start-Sleep -Seconds " + strconv.FormatFloat(seconds, 'f', -1, 64) + "\n"
	return Script(t, "prints-then-sleeps", body)
}

// ServerRecordsInvocation appends a line to counterPath each time it runs, then
// stays alive briefly. Used to assert a server process is started exactly once.
func ServerRecordsInvocation(t *testing.T, counterPath string, seconds float64) (command string, args []string) {
	t.Helper()
	body := "[IO.File]::AppendAllText(" + psQuote(counterPath) + ", 'x')\n" +
		"Start-Sleep -Seconds " + strconv.FormatFloat(seconds, 'f', -1, 64) + "\n"
	return Script(t, "records-invocation", body)
}

// ServerFlags renders a stand-in server as mcp-tui CLI flags:
//
//	--cmd pwsh --args -NoProfile --args -NonInteractive --args -File --args <script>
//
// The --args flag is a StringSlice, so repeating it avoids relying on comma
// splitting, which a Windows path could otherwise disturb.
func ServerFlags(t *testing.T, command string, args []string) []string {
	t.Helper()
	flags := []string{"--cmd", command}
	for _, arg := range args {
		flags = append(flags, "--args", arg)
	}
	return flags
}

// ExeName appends the platform's executable suffix to a base binary name.
func ExeName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

// psQuote renders s as a PowerShell single-quoted literal, where the only
// escape is a doubled single quote.
func psQuote(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '\'')
	for _, r := range s {
		if r == '\'' {
			out = append(out, '\'')
		}
		out = append(out, r)
	}
	out = append(out, '\'')
	return string(out)
}
