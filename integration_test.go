package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// testBinary is the mcp-tui binary the integration tests run, built once per
// test process by the first test that needs it and removed by TestMain.
// Building it per test cost ~2s of CPU each (7-8s uncached) while the rest of
// the suite ran in parallel packages.
var testBinary struct {
	once sync.Once
	dir  string
	path string
	err  error
}

func TestMain(m *testing.M) {
	// Tests start this binary as a stdio MCP server (testutil.StdioServer).
	testutil.ServeStdioIfRequested()
	code := m.Run()
	if testBinary.dir != "" {
		if err := os.RemoveAll(testBinary.dir); err != nil {
			fmt.Fprintf(os.Stderr, "removing test binary dir %s: %v\n", testBinary.dir, err)
		}
	}
	os.Exit(code)
}

// buildTestBinary returns an absolute path to the mcp-tui binary, compiling it
// on first use. An absolute path matters: a bare relative name like
// "mcp-tui-test" is looked up on PATH by os/exec rather than in the working
// directory, and on Windows the binary needs an .exe suffix.
func buildTestBinary(t *testing.T) string {
	t.Helper()
	testBinary.once.Do(func() {
		dir, err := os.MkdirTemp("", "mcp-tui-integration-")
		if err != nil {
			testBinary.err = err
			return
		}
		testBinary.dir = dir
		bin := filepath.Join(dir, testutil.ExeName("mcp-tui-test"))

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		out, err := exec.CommandContext(ctx, "go", "build", "-o", bin, ".").CombinedOutput()
		if err != nil {
			testBinary.err = fmt.Errorf("go build: %w: %s", err, out)
			return
		}
		testBinary.path = bin
	})
	require.NoError(t, testBinary.err, "Failed to build mcp-tui binary")
	return testBinary.path
}

func TestCLIIntegration(t *testing.T) {
	// Skip integration tests in short mode
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	bin := buildTestBinary(t)

	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		contains []string
	}{
		{
			name:     "help command",
			args:     []string{"--help"},
			wantErr:  false,
			contains: []string{"MCP", "client"},
		},
		{
			name:     "version command",
			args:     []string{"--version"},
			wantErr:  false,
			contains: []string{"mcp-tui", "version"},
		},
		{
			name:     "tool help",
			args:     []string{"tool", "--help"},
			wantErr:  false,
			contains: []string{"list", "call", "describe"},
		},
		{
			name:     "tool list without connection",
			args:     []string{"tool", "list"},
			wantErr:  true,
			contains: []string{"no MCP server connection specified"},
		},
		{
			name:     "tool list with invalid command",
			args:     []string{"tool", "list", "--cmd", "nonexistent-command-xyz"},
			wantErr:  true,
			contains: []string{"executable file not found"},
		},
		{
			// Version validation runs before the transport exists, so the
			// bogus command is never spawned and the error names the
			// versions the user can pick instead.
			name:     "unsupported protocol version rejected before spawn",
			args:     []string{"tool", "list", "--cmd", "nonexistent-command-xyz", "--protocol-version", "2099-01-01"},
			wantErr:  true,
			contains: []string{"unsupported MCP protocol version", "2026-07-28", "2025-11-25", "2024-11-05"},
		},
		{
			name:     "server help",
			args:     []string{"server", "--help"},
			wantErr:  false,
			contains: []string{"Show information about"},
		},
		{
			name:     "resource help",
			args:     []string{"resource", "--help"},
			wantErr:  false,
			contains: []string{"resources"},
		},
		{
			name:     "prompt help",
			args:     []string{"prompt", "--help"},
			wantErr:  false,
			contains: []string{"prompts"},
		},
	}

	// None of these cases needs PATH, but "tool list --cmd <missing>" resolves
	// the command through it. A lookup that finds nothing stats every PATH
	// entry; on WSL, where half of PATH is /mnt/c over 9P, that took 0.2s idle
	// and past the 10s budget under a full-suite load. An empty PATH directory
	// tests the same lookup failure without timing the host's filesystem.
	isolatedPath := "PATH=" + t.TempDir()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// None of these spawn a server: measured 0.17s median / 0.4s max
			// idle and 0.94s max with the full suite running (-count=10).
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, bin, tt.args...)
			cmd.Env = append(os.Environ(), isolatedPath)
			start := time.Now()
			output, err := cmd.CombinedOutput()
			t.Logf("mcp-tui %v took %v", tt.args, time.Since(start))
			outputStr := string(output)

			if tt.wantErr {
				assert.Error(t, err, "Expected command to fail")
			} else {
				assert.NoError(t, err, "Expected command to succeed. Output: %s", outputStr)
			}

			for _, contains := range tt.contains {
				assert.Contains(t, outputStr, contains, "Output should contain '%s'", contains)
			}
		})
	}
}

func TestCommandValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Build the binary
	bin := buildTestBinary(t)

	// An absolute path to a real executable, on every platform.
	scriptPath := testutil.ScriptPath(t, "absolute-path-server", "exit 0\n")
	testutil.RequirePwsh(t)
	pwshPath, err := testutil.LookPwsh()
	require.NoError(t, err)

	tests := []struct {
		name     string
		args     []string
		contains []string
	}{
		{
			name:     "dangerous command rejected",
			args:     []string{"tool", "list", "--cmd", "ls;rm", "--args", "test"},
			contains: []string{"dangerous pattern", "ls;rm"}, // validator catches ';' before exec
		},
		{
			// An absolute command path passes validation and is executed; the MCP
			// handshake then fails because the script does not speak the protocol.
			name:     "absolute path works but fails at MCP level",
			args:     append([]string{"tool", "list"}, testutil.ServerFlags(t, pwshPath, []string{"-NoProfile", "-NonInteractive", "-File", scriptPath})...),
			contains: []string{"Starting process", pwshPath},
		},
		{
			name:     "empty command rejected",
			args:     []string{"tool", "list", "--cmd", ""},
			contains: []string{"no MCP server connection specified"}, // Empty cmd means no connection
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, bin, tt.args...)
			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			// Should fail with validation error
			assert.Error(t, err, "Expected command validation to fail")

			for _, contains := range tt.contains {
				assert.Contains(t, outputStr, contains, "Output should contain '%s'", contains)
			}
		})
	}
}

// TestServerArgReachesTheServerAsIs: each --arg is one argument of the
// server process, exactly as given. --args splits on commas, so an
// argument holding one (a CSV column list, a JSON array) could not be
// passed at all.
func TestServerArgReachesTheServerAsIs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	bin := buildTestBinary(t)
	recorded := filepath.Join(t.TempDir(), "argv")
	// The server writes the arguments it was given, NUL-separated, and exits.
	serverCmd, serverArgs := testutil.Script(t, "records-args",
		"[IO.File]::WriteAllText("+psQuoteForTest(recorded)+", [string]::Join([char]0, $args))\nexit 1\n")
	want := []string{"--columns=id,name,owner", "two words", `it's "quoted"`, `["a","b"]`}

	args := make([]string, 0, 4+2*(len(serverArgs)+len(want)))
	// pwsh can take longer than the 10s default connect timeout to start on
	// a loaded machine; what is under test is the arguments, not the timing.
	args = append(args, "--timeout", "25s", "tool", "list", "--cmd", serverCmd)
	for _, arg := range append(serverArgs, want...) {
		args = append(args, "--arg", arg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	require.Error(t, err, "the stand-in server does not speak MCP")

	got, readErr := os.ReadFile(recorded)
	require.NoError(t, readErr, "the server did not run; output:\n%s", output)
	assert.Equal(t, want, strings.Split(string(got), "\x00"))
}

// TestServerArgAndArgsRefusedTogether: pflag keeps no order across two
// flags, so mixing them would pass the arguments in a guessed order.
func TestServerArgAndArgsRefusedTogether(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	bin := buildTestBinary(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, bin, "tool", "list",
		"--cmd", "npx", "--args", "@modelcontextprotocol/server-everything", "--arg", "stdio").CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(output), "--arg and --args cannot be combined")
}

// psQuoteForTest quotes s as a PowerShell single-quoted string.
func psQuoteForTest(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func TestStdioServerIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Test with a stand-in server command that we know will start
	// This tests the stdio transport validation without requiring an actual MCP server

	// Build the binary
	bin := buildTestBinary(t)

	t.Run("stdio transport with a stand-in server", func(t *testing.T) {
		// Spawning the CLI, which spawns a server process and fails the MCP
		// handshake, takes ~2.5s unloaded. A 5s budget leaves no headroom: on a
		// loaded machine the command is killed mid-run and the assertions below
		// see empty output rather than the error they are checking for.
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// A stand-in server that passes validation but never speaks MCP.
		serverCmd, serverArgs := testutil.ServerExitsImmediately(t)
		args := append([]string{"tool", "list"}, testutil.ServerFlags(t, serverCmd, serverArgs)...)
		cmd := exec.CommandContext(ctx, bin, args...)
		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		// Should fail at initialization, not validation
		assert.Error(t, err, "Expected MCP initialization to fail")

		// Should not contain validation errors
		assert.NotContains(t, outputStr, "command validation failed")

		// The stand-in prints "test" to stdout; the error quotes it.
		assert.Contains(t, outputStr, `not a JSON-RPC message: "test"`)
	})
}

// TestNaturalCLIConnectionString runs the CLI the way the README shows it,
// with the server as one positional connection string before the
// subcommand: `mcp-tui "server --stdio" tool list`. main pulls the string
// out before cobra parses flags and hands it to the subcommand; if that
// hand-off broke, tool list would report no connection instead of
// spawning the server.
func TestNaturalCLIConnectionString(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	bin := buildTestBinary(t)

	// Same budget as TestStdioServerIntegration: the CLI spawns a stand-in
	// server that fails the MCP handshake.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	serverCmd, serverArgs := testutil.ServerExitsImmediately(t)
	fields := make([]string, 0, len(serverArgs)+1)
	for _, field := range append([]string{serverCmd}, serverArgs...) {
		fields = append(fields, "'"+field+"'")
	}
	// --timeout: a slow pwsh start on a loaded machine is not what is tested.
	output, err := exec.CommandContext(ctx, bin, "--timeout", "25s", strings.Join(fields, " "), "tool", "list").CombinedOutput()
	outputStr := string(output)

	assert.Error(t, err, "a server that never speaks MCP must fail the handshake")
	assert.Contains(t, outputStr, "Starting process: "+serverCmd)
	assert.Contains(t, outputStr, `not a JSON-RPC message: "test"`)
	assert.NotContains(t, outputStr, "no MCP server connection specified")
}

func TestDebugMode(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Build the binary
	bin := buildTestBinary(t)

	t.Run("debug flag", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		serverCmd, serverArgs := testutil.ServerExitsImmediately(t)
		args := append([]string{"tool", "list", "--log-level", "debug"}, testutil.ServerFlags(t, serverCmd, serverArgs)...)
		cmd := exec.CommandContext(ctx, bin, args...)
		output, err := cmd.CombinedOutput()
		outputStr := string(output)

		// Should fail at MCP initialization (expected)
		assert.Error(t, err)

		// Debug output should be present
		assert.Contains(t, outputStr, "Creating MCP service")
	})
}

func TestOutputFormats(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	// Build the binary
	bin := buildTestBinary(t)

	t.Run("verbose output", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		serverCmd, serverArgs := testutil.ServerExitsImmediately(t)
		args := append([]string{"tool", "list"}, testutil.ServerFlags(t, serverCmd, serverArgs)...)
		cmd := exec.CommandContext(ctx, bin, args...)
		output, _ := cmd.CombinedOutput()
		outputStr := string(output)

		// Should have stderr output with emojis for user-friendly messages
		lines := strings.Split(outputStr, "\n")
		hasEmojiOutput := false
		for _, line := range lines {
			if strings.Contains(line, "🔄") || strings.Contains(line, "🚀") || strings.Contains(line, "❌") {
				hasEmojiOutput = true
				break
			}
		}
		assert.True(t, hasEmojiOutput, "Should have user-friendly output with emojis")
	})
}

// A CLI run whose server never answers the handshake exits at --timeout. The
// server must not outlive it: the SDK's graceful close gives a server that
// ignores its stdin closing seconds before signaling it, and the CLI exits
// long before, which used to orphan the server for good. The CLI kills it at
// the deadline and waits for it to be reaped before exiting.
func TestCLIConnectTimeoutLeavesNoServerBehind(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}
	bin := buildTestBinary(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	command, env := testutil.StdioServer(t, testutil.StdioServerOptions{Silent: true, PIDFile: pidFile})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Long enough for the server (this test binary) to start and record its
	// PID under a loaded race run; the CLI exits when it runs out.
	cmd := exec.CommandContext(ctx, bin, "tool", "list", "--cmd", command, "--timeout", "5s")
	cmd.Env = os.Environ()
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "the handshake must time out:\n%s", output)

	data, err := os.ReadFile(pidFile)
	require.NoError(t, err, "the server never started:\n%s", output)
	pid, err := strconv.Atoi(string(data))
	require.NoError(t, err)
	t.Cleanup(func() {
		if proc, findErr := os.FindProcess(pid); findErr == nil {
			_ = proc.Kill()
		}
	})
	// The CLI waits for the killed server's background close, which reaps it.
	require.True(t, testutil.ProcessExited(pid), "the server process outlived the CLI:\n%s", output)
}
