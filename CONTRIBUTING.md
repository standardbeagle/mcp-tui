# Contributing to MCP-TUI

Thank you for your interest in contributing to MCP-TUI! This document provides guidelines and information for contributors.

## Table of Contents

- [Getting Started](#getting-started)
- [Development Environment](#development-environment)
- [Code Organization](#code-organization)
- [Coding Standards](#coding-standards)
- [Testing](#testing)
- [Submitting Changes](#submitting-changes)
- [Architecture Guidelines](#architecture-guidelines)

## Getting Started

### Prerequisites

- Go 1.21 or later
- Node.js (for test servers)
- Make (for build automation)
- Git

### Setting up the Development Environment

1. **Clone the repository**
   ```bash
   git clone https://github.com/your-org/mcp-tui.git
   cd mcp-tui
   ```

2. **Install dependencies**
   ```bash
   make deps
   ```

3. **Verify the setup**
   ```bash
   make test
   make build
   ```

4. **Install development tools**
   ```bash
   make lint  # This will install golangci-lint if not present
   ```

## Development Environment

### Build Commands

```bash
# Full development pipeline
make all

# Individual commands
make build          # Build binary
make test           # Run tests
make coverage       # Generate coverage report
make lint           # Run linter
make fmt            # Format code
make vet            # Run go vet

# Development helpers
make dev            # Build with debug symbols
make run            # Build and run TUI
make test-servers   # Test with problematic servers
```

### Project Structure

Follow the established architecture in `ARCHITECTURE.md`. Key principles:

- **internal/**: Private application code, organized by domain
- **cmd/**: Application entry points
- **pkg/**: Public packages (when needed)
- **tests/**: Integration tests

### Development Workflow

1. **Create a feature branch**
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make your changes**
   - Follow the coding standards below
   - Add tests for new functionality
   - Update documentation as needed

3. **Test your changes**
   ```bash
   make all
   make test-servers  # Test with problematic MCP servers
   ```

4. **Commit your changes**
   ```bash
   git add .
   git commit -m "descriptive commit message"
   ```

## Code Organization

### Package Guidelines

- **One concept per package**: Each package should have a single, well-defined responsibility
- **Clear interfaces**: Define interfaces for major abstractions
- **Minimize dependencies**: Avoid circular dependencies and unnecessary coupling
- **Platform abstraction**: Use build tags for platform-specific code

### File Naming Conventions

- `manager.go` - Main implementation
- `manager_unix.go` - Unix-specific implementation (with build tags)
- `manager_windows.go` - Windows-specific implementation (with build tags)
- `errors.go` - Error definitions and handling
- `types.go` - Type definitions
- `interface.go` - Interface definitions

### Import Organization

```go
package example

import (
    // Standard library
    "context"
    "fmt"
    "os"

    // Third-party packages
    "github.com/spf13/cobra"
    "github.com/charmbracelet/bubbletea"

    // Internal packages
    "github.com/your-org/mcp-tui/internal/config"
    "github.com/your-org/mcp-tui/internal/debug"
)
```

## Coding Standards

### General Guidelines

1. **Follow Go conventions**
   - Use `gofmt` for formatting
   - Follow effective Go guidelines
   - Use meaningful variable and function names

2. **Error handling**
   - Always handle errors explicitly
   - Use structured errors with context
   - Wrap errors with additional context

   ```go
   if err != nil {
       return debug.WrapError(err, debug.ErrorCodeConnectionFailed, 
           "failed to connect to MCP server")
   }
   ```

3. **Logging**
   - Use structured logging with the debug package
   - Include relevant context in log messages
   - Use appropriate log levels

   ```go
   logger := debug.Component("my-component")
   logger.Info("starting operation", 
       debug.F("operation", "connect"),
       debug.F("server", serverURL))
   ```

4. **Documentation**
   - Document all public functions and types
   - Include examples for complex APIs
   - Keep documentation up to date

### Code Style

1. **Function length**: Keep functions under 50 lines when possible
2. **Cyclomatic complexity**: Aim for complexity < 10
3. **Interface segregation**: Prefer small, focused interfaces
4. **Dependency injection**: Use interfaces for testability

### Specific Guidelines

#### TUI Code
- Separate model, update, and view logic
- Use the screen abstraction for different UI states
- Handle terminal resize gracefully
- Provide keyboard shortcuts documentation

#### CLI Code
- Extend the base command for consistency
- Use cobra for command structure
- Provide helpful error messages
- Support JSON output when appropriate

#### MCP Integration
- Use the service layer abstraction
- Handle connection failures gracefully
- Validate MCP responses
- Support all MCP transport types

## Testing

### Test Organization

```bash
# Unit tests
go test ./internal/...

# Integration tests
go test ./tests/...

# Test with problematic servers
make test-servers
```

### Test Guidelines

1. **Unit tests**
   - Test individual functions and methods
   - Use table-driven tests when appropriate
   - Mock external dependencies

2. **Integration tests**
   - Test component interactions
   - Use real MCP servers when possible
   - Test error conditions

3. **UI tests**
   - Test screen transitions
   - Verify key handling
   - Test responsive behavior

### Test Structure

```go
func TestFunctionName(t *testing.T) {
    tests := []struct {
        name     string
        input    InputType
        expected ExpectedType
        wantErr  bool
    }{
        {
            name:     "valid input",
            input:    validInput,
            expected: expectedOutput,
            wantErr:  false,
        },
        // Add more test cases
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := FunctionName(tt.input)
            
            if tt.wantErr {
                assert.Error(t, err)
                return
            }
            
            assert.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

## Submitting Changes

### Pull Request Process

1. **Ensure tests pass**
   ```bash
   make all
   ```

2. **Update documentation**
   - Update README.md if needed
   - Update ARCHITECTURE.md for architectural changes
   - Add/update comments for new public APIs

3. **Create pull request**
   - Provide a clear description of changes
   - Reference any related issues
   - Include screenshots for UI changes

4. **Code review**
   - Address reviewer feedback
   - Ensure CI passes
   - Squash commits if requested

### Commit Message Guidelines

Use clear, descriptive commit messages:

```
Add tool execution timeout configuration

- Add timeout configuration option to config package
- Implement timeout in CLI tool commands
- Add timeout display in TUI tool screen
- Update documentation with timeout usage

Fixes #123
```

## Architecture Guidelines

### Adding New Features

1. **Design first**
   - Consider the overall architecture
   - Define interfaces before implementations
   - Plan for testability

2. **Implement incrementally**
   - Start with the service layer
   - Add CLI interface
   - Add TUI interface
   - Add comprehensive tests

3. **Consider error cases**
   - Plan for failure scenarios
   - Provide meaningful error messages
   - Ensure graceful degradation

### Refactoring Guidelines

1. **Maintain backward compatibility** when possible
2. **Update tests** to reflect changes
3. **Update documentation** for API changes
4. **Consider migration paths** for breaking changes

### Performance Considerations

1. **Profile before optimizing**
2. **Prefer clarity over cleverness**
3. **Consider memory usage** in long-running operations
4. **Optimize UI responsiveness** over raw speed

## Getting Help

- **Issues**: Check existing issues or create a new one
- **Discussions**: Use GitHub discussions for questions
- **Architecture**: Refer to ARCHITECTURE.md
- **Code examples**: Look at existing implementations

## License

By contributing to MCP-TUI, you agree that your contributions will be licensed under the same license as the project.