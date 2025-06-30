package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/cli"
	"github.com/standardbeagle/mcp-tui/internal/config"
)

// TestMainFunctionFlow tests the main function's argument handling
func TestMainFunctionFlow(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectedConfig *config.ConnectionConfig
		expectedOsArgs []string
		description    string
	}{
		{
			name: "natural CLI pattern",
			args: []string{"mcp-tui", "npx server stdio", "tool", "list"},
			expectedConfig: &config.ConnectionConfig{
				Type:    config.TransportStdio,
				Command: "npx",
				Args:    []string{"server", "stdio"},
			},
			expectedOsArgs: []string{"mcp-tui", "tool", "list"},
			description:    "Should extract connection and adjust os.Args for Cobra",
		},
		{
			name:           "TUI mode with connection",
			args:           []string{"mcp-tui", "npx server stdio"},
			expectedConfig: nil, // No global config set for TUI mode
			expectedOsArgs: []string{"mcp-tui", "npx server stdio"},
			description:    "TUI mode should not modify os.Args",
		},
		{
			name:           "flag-based CLI",
			args:           []string{"mcp-tui", "--cmd", "npx", "--args", "server", "--args", "stdio", "tool", "list"},
			expectedConfig: nil, // Flags are parsed later by Cobra
			expectedOsArgs: []string{"mcp-tui", "--cmd", "npx", "--args", "server", "--args", "stdio", "tool", "list"},
			description:    "Flag-based usage should pass through unchanged",
		},
		{
			name:           "subcommand without connection",
			args:           []string{"mcp-tui", "tool", "list"},
			expectedConfig: nil,
			expectedOsArgs: []string{"mcp-tui", "tool", "list"},
			description:    "Subcommand alone should pass through",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original os.Args
			oldArgs := os.Args
			defer func() { os.Args = oldArgs }()

			// Set test args
			os.Args = tt.args

			// Simulate the early parsing logic from main()
			var detectedConfig *config.ConnectionConfig
			if len(os.Args) > 1 {
				parsedArgs := config.ParseArgs(os.Args[1:], "", "", nil)
				if parsedArgs.Connection != nil && parsedArgs.SubCommand != "" {
					detectedConfig = parsedArgs.Connection
					cli.SetGlobalConnection(detectedConfig)

					// Simulate the os.Args adjustment
					newArgs := []string{os.Args[0], parsedArgs.SubCommand}
					newArgs = append(newArgs, parsedArgs.SubCommandArgs...)
					os.Args = newArgs
				}
			}

			// Verify the connection config
			if tt.expectedConfig != nil {
				if detectedConfig == nil {
					t.Errorf("%s\nExpected connection config, got nil", tt.description)
				} else if !reflect.DeepEqual(detectedConfig, tt.expectedConfig) {
					t.Errorf("%s\nConnection mismatch:\ngot:  %+v\nwant: %+v",
						tt.description, detectedConfig, tt.expectedConfig)
				}
			}

			// Verify os.Args adjustment
			if !reflect.DeepEqual(os.Args, tt.expectedOsArgs) {
				t.Errorf("%s\nos.Args mismatch:\ngot:  %v\nwant: %v",
					tt.description, os.Args, tt.expectedOsArgs)
			}

			// Clean up global state
			cli.SetGlobalConnection(nil)
		})
	}
}

// TestEndToEndScenarios demonstrates complete usage scenarios
func TestEndToEndScenarios(t *testing.T) {
	scenarios := []struct {
		name        string
		command     string
		description string
		flow        string
	}{
		{
			name:        "natural tool list",
			command:     `mcp-tui "npx -y @modelcontextprotocol/server-everything stdio" tool list`,
			description: "List tools using natural CLI syntax",
			flow: `
1. Main parses args: ["npx -y @modelcontextprotocol/server-everything stdio", "tool", "list"]
2. ParseArgs identifies:
   - Connection: npx with args ["-y", "@modelcontextprotocol/server-everything", "stdio"]
   - Subcommand: "tool"
   - SubcommandArgs: ["list"]
3. Global connection is set
4. os.Args adjusted to: ["mcp-tui", "tool", "list"]
5. Cobra sees "tool list" command
6. Tool command uses global connection
7. MCP client connects with: npx -y @modelcontextprotocol/server-everything stdio`,
		},
		{
			name:        "natural tool call",
			command:     `mcp-tui "./brum --mcp" tool call echo message="Hello World"`,
			description: "Call a tool with parameters using natural syntax",
			flow: `
1. Main parses args: ["./brum --mcp", "tool", "call", "echo", "message=Hello World"]
2. ParseArgs identifies:
   - Connection: ./brum with args ["--mcp"]
   - Subcommand: "tool"
   - SubcommandArgs: ["call", "echo", "message=Hello World"]
3. Global connection is set
4. os.Args adjusted to: ["mcp-tui", "tool", "call", "echo", "message=Hello World"]
5. Tool call command parses key=value pairs
6. Executes tool with parsed arguments`,
		},
		{
			name:        "flag-based usage",
			command:     `mcp-tui --cmd ./brum --args --mcp tool list`,
			description: "Traditional flag-based approach still works",
			flow: `
1. No connection string detected in positional args
2. os.Args unchanged
3. Cobra parses flags normally
4. Tool command reads --cmd and --args flags
5. Creates connection from flags
6. Executes normally`,
		},
		{
			name:        "url-based connection",
			command:     `mcp-tui --url http://localhost:8000/mcp server`,
			description: "HTTP/SSE server connection",
			flow: `
1. No positional connection string
2. Cobra parses --url flag
3. Server command creates HTTP connection
4. Queries server info over HTTP`,
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			t.Logf("\nScenario: %s", scenario.description)
			t.Logf("Command: %s", scenario.command)
			t.Logf("Flow:%s", scenario.flow)
		})
	}
}
