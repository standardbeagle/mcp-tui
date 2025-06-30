package config

import (
	"reflect"
	"testing"
)

func TestParseConnectionString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *ConnectionConfig
	}{
		{
			name:  "simple command",
			input: "npx @modelcontextprotocol/server-everything stdio",
			expected: &ConnectionConfig{
				Type:    TransportStdio,
				Command: "npx",
				Args:    []string{"@modelcontextprotocol/server-everything", "stdio"},
			},
		},
		{
			name:  "command with flags",
			input: "./brum --mcp --verbose",
			expected: &ConnectionConfig{
				Type:    TransportStdio,
				Command: "./brum",
				Args:    []string{"--mcp", "--verbose"},
			},
		},
		{
			name:  "http url",
			input: "http://localhost:8000/mcp",
			expected: &ConnectionConfig{
				Type: TransportHTTP,
				URL:  "http://localhost:8000/mcp",
			},
		},
		{
			name:  "sse url",
			input: "http://localhost:8000/sse",
			expected: &ConnectionConfig{
				Type: TransportSSE,
				URL:  "http://localhost:8000/sse",
			},
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:  "single command no args",
			input: "mcp-server",
			expected: &ConnectionConfig{
				Type:    TransportStdio,
				Command: "mcp-server",
				Args:    []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseConnectionString(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("ParseConnectionString(%q) = %+v, want %+v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestParseArgs(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		cmdFlag     string
		urlFlag     string
		argsFlag    []string
		expected    *ParsedArgs
		description string
	}{
		{
			name: "natural CLI with tool list",
			args: []string{"npx @modelcontextprotocol/server-everything stdio", "tool", "list"},
			expected: &ParsedArgs{
				Connection: &ConnectionConfig{
					Type:    TransportStdio,
					Command: "npx",
					Args:    []string{"@modelcontextprotocol/server-everything", "stdio"},
				},
				SubCommand:     "tool",
				SubCommandArgs: []string{"list"},
			},
			description: "Should parse connection string and tool list command",
		},
		{
			name: "natural CLI with tool call",
			args: []string{"./brum --mcp", "tool", "call", "echo", "message=hello"},
			expected: &ParsedArgs{
				Connection: &ConnectionConfig{
					Type:    TransportStdio,
					Command: "./brum",
					Args:    []string{"--mcp"},
				},
				SubCommand:     "tool",
				SubCommandArgs: []string{"call", "echo", "message=hello"},
			},
			description: "Should parse connection and tool call with parameters",
		},
		{
			name:     "flag-based connection",
			args:     []string{"tool", "list"},
			cmdFlag:  "npx",
			argsFlag: []string{"@modelcontextprotocol/server-everything", "stdio"},
			expected: &ParsedArgs{
				Connection: &ConnectionConfig{
					Type:    TransportStdio,
					Command: "npx",
					Args:    []string{"@modelcontextprotocol/server-everything", "stdio"},
				},
				SubCommand:     "tool",
				SubCommandArgs: []string{"list"},
			},
			description: "Should use flags when no connection string in args",
		},
		{
			name:    "url-based connection",
			args:    []string{"server"},
			urlFlag: "http://localhost:8000/mcp",
			expected: &ParsedArgs{
				Connection: &ConnectionConfig{
					Type: TransportHTTP,
					URL:  "http://localhost:8000/mcp",
				},
				SubCommand:     "server",
				SubCommandArgs: []string{},
			},
			description: "Should parse URL flag and server command",
		},
		{
			name: "no connection just subcommand",
			args: []string{"tool", "list"},
			expected: &ParsedArgs{
				Connection:     nil,
				SubCommand:     "tool",
				SubCommandArgs: []string{"list"},
			},
			description: "Should parse subcommand without connection",
		},
		{
			name: "connection string only for TUI",
			args: []string{"npx @modelcontextprotocol/server-everything stdio"},
			expected: &ParsedArgs{
				Connection: &ConnectionConfig{
					Type:    TransportStdio,
					Command: "npx",
					Args:    []string{"@modelcontextprotocol/server-everything", "stdio"},
				},
				SubCommand:     "",
				SubCommandArgs: nil,
			},
			description: "Should parse connection for TUI mode",
		},
		{
			name:     "flags take precedence",
			args:     []string{"some-command", "tool", "list"},
			cmdFlag:  "override-cmd",
			argsFlag: []string{"override-arg"},
			expected: &ParsedArgs{
				Connection: &ConnectionConfig{
					Type:    TransportStdio,
					Command: "override-cmd",
					Args:    []string{"override-arg"},
				},
				SubCommand:     "tool",
				SubCommandArgs: []string{"list"},
			},
			description: "Explicit flags should override positional connection",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseArgs(tt.args, tt.cmdFlag, tt.urlFlag, tt.argsFlag)

			// Compare connections
			if !reflect.DeepEqual(result.Connection, tt.expected.Connection) {
				t.Errorf("%s\nConnection mismatch:\ngot:  %+v\nwant: %+v",
					tt.description, result.Connection, tt.expected.Connection)
			}

			// Compare subcommand
			if result.SubCommand != tt.expected.SubCommand {
				t.Errorf("%s\nSubCommand mismatch: got %q, want %q",
					tt.description, result.SubCommand, tt.expected.SubCommand)
			}

			// Compare subcommand args
			if !reflect.DeepEqual(result.SubCommandArgs, tt.expected.SubCommandArgs) {
				t.Errorf("%s\nSubCommandArgs mismatch:\ngot:  %v\nwant: %v",
					tt.description, result.SubCommandArgs, tt.expected.SubCommandArgs)
			}
		})
	}
}

func TestIsKnownSubcommand(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"tool", true},
		{"resource", true},
		{"prompt", true},
		{"server", true},
		{"completion", true},
		{"help", true},
		{"unknown", false},
		{"", false},
		{"Tool", false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isKnownSubcommand(tt.input)
			if result != tt.expected {
				t.Errorf("isKnownSubcommand(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
