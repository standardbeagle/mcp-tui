package config

import (
	"strings"
)

// ParsedArgs represents the result of parsing command line arguments
type ParsedArgs struct {
	Connection    *ConnectionConfig
	SubCommand    string
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
	parts := strings.Fields(connStr)
	if len(parts) == 0 {
		return nil
	}
	
	return &ConnectionConfig{
		Type:    TransportStdio,
		Command: parts[0],
		Args:    parts[1:],
	}
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

// isKnownSubcommand checks if a string is a known subcommand
func isKnownSubcommand(arg string) bool {
	knownCommands := []string{"tool", "resource", "prompt", "server", "completion", "help"}
	for _, cmd := range knownCommands {
		if arg == cmd {
			return true
		}
	}
	return false
}