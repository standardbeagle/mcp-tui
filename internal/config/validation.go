package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Security validation for command execution to prevent command injection

// DangerousCommandPatterns contains patterns that indicate potential command injection
var DangerousCommandPatterns = []string{
	";",     // Command separator
	"&&",    // Command chaining (AND)
	"||",    // Command chaining (OR)
	"|",     // Pipe operator
	">",     // Output redirection
	"<",     // Input redirection
	">>",    // Append redirection
	"$(",    // Command substitution
	"`",     // Command substitution (backtick)
	"${",    // Variable expansion
	"../",   // Directory traversal
	"rm ",   // Remove command
	"del ",  // Delete command (Windows)
	"format ", // Format command (dangerous)
	"shutdown", // System shutdown
	"reboot",   // System reboot
}

// ValidateCommand validates a command and its arguments for security
func ValidateCommand(command string, args []string) error {
	if command == "" {
		return fmt.Errorf("command cannot be empty")
	}

	// Check for dangerous patterns in command
	for _, pattern := range DangerousCommandPatterns {
		if strings.Contains(command, pattern) {
			return fmt.Errorf("command contains dangerous pattern '%s' which is not allowed for security reasons", pattern)
		}
	}

	// Check for dangerous patterns in arguments
	for i, arg := range args {
		for _, pattern := range DangerousCommandPatterns {
			if strings.Contains(arg, pattern) {
				return fmt.Errorf("argument %d contains dangerous pattern '%s' which is not allowed for security reasons", i+1, pattern)
			}
		}
	}

	// Validate paths don't contain directory traversal
	if err := validatePaths(command, args); err != nil {
		return err
	}

	return nil
}

// validatePaths checks for directory traversal attempts
func validatePaths(command string, args []string) error {
	// Check command path
	if strings.Contains(command, "..") {
		cleanPath := filepath.Clean(command)
		if strings.Contains(cleanPath, "..") {
			return fmt.Errorf("command path contains directory traversal which is not allowed")
		}
	}

	// Check argument paths
	for i, arg := range args {
		if strings.Contains(arg, "..") {
			cleanPath := filepath.Clean(arg)
			if strings.Contains(cleanPath, "..") {
				return fmt.Errorf("argument %d contains directory traversal which is not allowed", i+1)
			}
		}
	}

	return nil
}

// IsCommandSafe returns true if the command is considered safe for execution
func IsCommandSafe(command string, args []string) bool {
	return ValidateCommand(command, args) == nil
}

// SanitizeCommand removes potentially dangerous characters from command
// Note: This is for display purposes only, not for execution
func SanitizeCommand(command string) string {
	// Remove or replace dangerous characters for safe display
	sanitized := command
	replacements := map[string]string{
		"$": "\\$",
		"`": "\\`",
		";": "\\;",
	}
	
	for old, new := range replacements {
		sanitized = strings.ReplaceAll(sanitized, old, new)
	}
	
	return sanitized
}