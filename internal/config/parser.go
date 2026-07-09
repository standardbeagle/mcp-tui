package config

import (
	"fmt"
	"strings"
	"unicode"
)

// ParsedArgs represents the result of parsing command line arguments
type ParsedArgs struct {
	Connection     *ConnectionConfig
	SubCommand     string
	SubCommandArgs []string
}

// ParseConnectionString parses a connection string into a ConnectionConfig
// Examples:
//   - "npx -y @modelcontextprotocol/server-everything stdio"
//   - "./my-server --mcp"
//   - "http://localhost:8000/mcp"
func ParseConnectionString(connStr string) *ConnectionConfig {
	// Check if it's a URL
	if strings.HasPrefix(connStr, "http://") || strings.HasPrefix(connStr, "https://") {
		transportType := TransportHTTP
		if strings.Contains(connStr, "/events") || strings.Contains(connStr, "sse") {
			transportType = TransportSSE
		}
		return &ConnectionConfig{
			Type: transportType,
			URL:  connStr,
		}
	}

	// Otherwise it's a command string
	parts, err := ParseCommandLine(connStr)
	if err != nil {
		return nil
	}
	if len(parts) == 0 {
		return nil
	}

	return &ConnectionConfig{
		Type:    TransportStdio,
		Command: parts[0],
		Args:    parts[1:],
	}
}

// ParseCommandLine splits a single command line into argv-style fields without
// invoking a shell. It supports single quotes, double quotes, and backslash
// escaping outside single quotes. Shell operators are not interpreted; command
// safety is still enforced later by ValidateCommand.
func ParseCommandLine(input string) ([]string, error) {
	var fields []string
	var current strings.Builder
	var quote rune
	escaped := false
	inField := false

	for _, r := range input {
		if escaped {
			current.WriteRune(r)
			escaped = false
			inField = true
			continue
		}

		if quote != '\'' && r == '\\' {
			escaped = true
			inField = true
			continue
		}

		if quote != 0 {
			if r == quote {
				quote = 0
				inField = true
				continue
			}
			current.WriteRune(r)
			inField = true
			continue
		}

		switch {
		case r == '\'' || r == '"':
			quote = r
			inField = true
		case unicode.IsSpace(r):
			if inField {
				fields = append(fields, current.String())
				current.Reset()
				inField = false
			}
		default:
			current.WriteRune(r)
			inField = true
		}
	}

	if escaped {
		return nil, fmt.Errorf("unterminated escape in command line")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted string in command line")
	}
	if inField {
		fields = append(fields, current.String())
	}

	return fields, nil
}

// ParseArgs parses command line arguments to extract connection info and subcommands
// This handles various usage patterns:
//   - mcp-tui "connection string" [subcommand] [args...]
//   - mcp-tui --cmd command --args arg1,arg2 [subcommand] [args...]
//   - mcp-tui --url http://... [subcommand] [args...]
func ParseArgs(args []string, cmdFlag, urlFlag string, argsFlag []string) *ParsedArgs {
	result := &ParsedArgs{}

	// First priority: explicit flags
	if cmdFlag != "" {
		result.Connection = &ConnectionConfig{
			Type:    TransportStdio,
			Command: cmdFlag,
			Args:    argsFlag,
		}
	} else if urlFlag != "" {
		transportType := TransportHTTP
		if strings.Contains(urlFlag, "/events") || strings.Contains(urlFlag, "sse") {
			transportType = TransportSSE
		}
		result.Connection = &ConnectionConfig{
			Type: transportType,
			URL:  urlFlag,
		}
	}

	// Check if we need to parse positional connection string
	argsToProcess := args
	if result.Connection == nil && len(argsToProcess) > 0 {
		// Skip if first arg is a subcommand
		if !isKnownSubcommand(argsToProcess[0]) && !strings.HasPrefix(argsToProcess[0], "-") {
			result.Connection = ParseConnectionString(argsToProcess[0])
			argsToProcess = argsToProcess[1:] // consume the connection string
		}
	} else if result.Connection != nil && len(args) > 0 {
		// When using flags, we might have a positional arg that's not a connection
		// Check if first arg looks like a connection string or is a subcommand
		if isKnownSubcommand(args[0]) ||
			(len(args) > 1 && isKnownSubcommand(args[1])) {
			// It's likely "some-command tool list" where some-command should be ignored
			if !isKnownSubcommand(args[0]) && len(args) > 1 {
				argsToProcess = args[1:] // skip the non-subcommand first arg
			}
		}
	}

	// Extract subcommand and its args
	if len(argsToProcess) > 0 && isKnownSubcommand(argsToProcess[0]) {
		result.SubCommand = argsToProcess[0]
		result.SubCommandArgs = argsToProcess[1:]
	}

	return result
}

// isKnownSubcommand checks if a string is a known subcommand. Keep this list
// in sync with the AddCommand calls in main.go — every cobra subcommand the
// root registers must appear here so the early-parse pattern in main()
// doesn't mistake the subcommand name for a connection string.
func isKnownSubcommand(arg string) bool {
	knownCommands := []string{"tool", "resource", "prompt", "server", "completion", "help", "capabilities", "verify", "conform"}
	for _, cmd := range knownCommands {
		if arg == cmd {
			return true
		}
	}
	return false
}
