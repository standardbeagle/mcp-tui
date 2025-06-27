# MCP-TUI Architecture

## Overview

MCP-TUI is a Go-based test client for Model Context Protocol (MCP) servers that provides both an interactive Terminal User Interface (TUI) mode and a scriptable Command Line Interface (CLI) mode.

## Project Structure

```
mcp-tui/
├── cmd/                           # Application entry points
│   └── main.go                   # Main application entry
├── internal/                     # Private application code
│   ├── config/                   # Configuration management
│   │   └── config.go
│   ├── mcp/                      # MCP service layer
│   │   └── service.go
│   ├── cli/                      # CLI command implementations
│   │   ├── base.go              # Common CLI functionality
│   │   ├── tool.go              # Tool commands
│   │   ├── resource.go          # Resource commands (planned)
│   │   └── prompt.go            # Prompt commands (planned)
│   ├── tui/                      # Terminal UI implementation
│   │   ├── app/                 # TUI application management
│   │   │   └── app.go
│   │   ├── screens/             # Individual UI screens
│   │   │   ├── screen.go        # Screen interface and base
│   │   │   ├── connection.go    # Connection setup screen
│   │   │   ├── main.go          # Main interface screen (planned)
│   │   │   └── tool.go          # Tool interaction screen (planned)
│   │   └── components/          # Reusable UI components (planned)
│   ├── platform/                # Platform-specific implementations
│   │   ├── process/             # Process management
│   │   │   ├── manager.go       # Cross-platform interface
│   │   │   ├── manager_unix.go  # Unix implementation
│   │   │   └── manager_windows.go # Windows implementation
│   │   └── signal/              # Signal handling
│   │       └── handler.go
│   ├── debug/                   # Debug and logging infrastructure
│   │   ├── errors.go           # Structured error handling
│   │   └── logger.go           # Structured logging
│   └── transport/               # Transport abstractions (planned)
├── pkg/                         # Public packages (none yet)
├── tests/                       # Integration tests (planned)
├── test-servers/               # Test MCP servers for validation
│   ├── *.js                   # Various misbehaving servers
│   └── test-bad-servers.sh    # Test script
├── Makefile                    # Build and development automation
├── .golangci.yml              # Linting configuration
└── README.md                  # Project documentation
```

## Architecture Principles

### 1. Separation of Concerns
- **CLI layer**: Command-line interface with minimal business logic
- **TUI layer**: Terminal user interface with screen-based navigation
- **Service layer**: Core MCP operations and business logic
- **Platform layer**: OS-specific implementations
- **Transport layer**: Communication with MCP servers

### 2. Dependency Inversion
- High-level modules depend on abstractions, not concrete implementations
- Interfaces define contracts between layers
- Platform-specific code is isolated behind interfaces

### 3. Single Responsibility
- Each package has a single, well-defined purpose
- Large files are split into focused, manageable modules
- Screens are separated by functionality

### 4. Error Handling Strategy
- Structured errors with error codes and context
- Graceful degradation and user-friendly error messages
- Comprehensive logging for debugging

## Key Components

### Configuration Management (`internal/config`)
- Centralized configuration with defaults
- Validation and type-safe settings
- Support for different transport types

### MCP Service Layer (`internal/mcp`)
- High-level interface for MCP operations
- Connection management and lifecycle
- Tool, resource, and prompt operations

### CLI Commands (`internal/cli`)
- Base command with common functionality
- Consistent error handling and timeouts
- Structured command organization

### TUI Application (`internal/tui`)
- Screen-based navigation model
- Responsive UI with proper error boundaries
- Separation of application logic and rendering

### Platform Abstraction (`internal/platform`)
- Cross-platform process management
- Signal handling with graceful shutdown
- OS-specific optimizations

### Debug Infrastructure (`internal/debug`)
- Structured logging with levels and components
- Error codes and context preservation
- Development and production debugging support

## Design Patterns Used

### 1. Strategy Pattern
- Transport types (STDIO, SSE, HTTP)
- Platform-specific implementations
- Error handling strategies

### 2. Command Pattern
- CLI command structure
- UI screen transitions
- MCP operation encapsulation

### 3. Observer Pattern
- Signal handling
- UI event system
- Status updates

### 4. Factory Pattern
- Process manager creation
- Screen instantiation
- Client creation

### 5. Decorator Pattern
- Debug transport wrapper
- Logging middleware
- Error enrichment

## Error Handling

### Error Types
- **Connection errors**: Network and transport issues
- **Protocol errors**: MCP specification violations
- **Server errors**: Remote server problems
- **Tool errors**: Tool execution failures
- **UI errors**: Interface rendering issues
- **System errors**: OS and process issues

### Error Flow
1. Errors are caught at the source
2. Wrapped with appropriate error codes and context
3. Logged with structured information
4. Displayed to users in a friendly format
5. Allow graceful recovery where possible

## Logging Strategy

### Log Levels
- **DEBUG**: Detailed execution information
- **INFO**: General application flow
- **WARN**: Recoverable issues
- **ERROR**: Error conditions
- **FATAL**: Critical failures requiring exit

### Log Components
- Each major component has its own logger
- Structured fields for searchability
- Performance-conscious (no expensive operations in disabled levels)

## Development Workflow

### Build System
```bash
make all          # Full build pipeline
make build        # Just build binary
make test         # Run tests
make lint         # Code linting
make coverage     # Test coverage
```

### Testing Strategy
- Unit tests for individual components
- Integration tests for MCP interactions
- Test servers for failure scenarios
- UI testing with bubbletea framework

### Code Quality
- golangci-lint for static analysis
- 100% test coverage goal for core logic
- Documentation for all public interfaces
- Regular dependency updates

## Future Enhancements

### Phase 1: Core Functionality
- Complete MCP service implementation
- Full CLI command coverage
- Main TUI screens
- Basic error recovery

### Phase 2: Advanced Features
- Configuration file support
- Plugin system for custom tools
- Advanced UI components
- Performance optimizations

### Phase 3: Production Features
- Metrics and monitoring
- Configuration management UI
- Advanced debugging tools
- Multi-server support

## Security Considerations

- Process isolation and cleanup
- Input validation and sanitization
- Secure default configurations
- Error message information disclosure prevention
- Signal handling security

## Performance Considerations

- Lazy loading of UI components
- Efficient process management
- Memory-conscious logging
- Responsive UI with async operations
- Resource cleanup and leak prevention

## Dependencies

### Core Dependencies
- `github.com/mark3labs/mcp-go`: MCP protocol implementation
- `github.com/charmbracelet/bubbletea`: TUI framework
- `github.com/spf13/cobra`: CLI framework

### Development Dependencies
- `golangci-lint`: Code linting
- Various test frameworks
- Build and release tools

## Contributing

1. Follow the established architecture patterns
2. Add tests for new functionality
3. Update documentation for changes
4. Use structured logging and error handling
5. Maintain platform compatibility