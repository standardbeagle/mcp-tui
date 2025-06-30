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

## Dependencies: Carefully Chosen, Thoughtfully Managed

**Philosophy:** Every dependency is a commitment. Choose dependencies that align with our values and enhance developer productivity.

### 🌍 Core Dependencies: The Foundation

**📝 `github.com/mark3labs/mcp-go` - MCP Protocol Implementation**
- *Why we chose it:* Official, well-maintained Go implementation of MCP specification
- *What it gives us:* Type-safe protocol handling, automatic compatibility updates
- *Developer benefit:* Focus on application logic, not protocol details
- *Risk mitigation:* Active community, clear versioning, comprehensive test suite

**🎮 `github.com/charmbracelet/bubbletea` - TUI Framework**
- *Why we chose it:* Modern, reactive terminal UI framework with excellent architecture
- *What it gives us:* Component-based UI, smooth animations, responsive interactions
- *Developer benefit:* Build complex UIs without fighting terminal limitations
- *Risk mitigation:* Backed by Charm, actively developed, great documentation

**⚡ `github.com/spf13/cobra` - CLI Framework**
- *Why we chose it:* Industry standard for Go CLI applications
- *What it gives us:* Consistent command structure, automatic help generation, flag parsing
- *Developer benefit:* Familiar interface patterns, extensive ecosystem compatibility
- *Risk mitigation:* Extremely stable, used by kubectl and many other critical tools

### 🛠️ Development Dependencies: Productivity Multipliers

**🔍 `golangci-lint` - Comprehensive Code Analysis**
- *Why we chose it:* Aggregates 50+ linters into one fast tool
- *What it gives us:* Consistent code quality, security vulnerability detection
- *Developer benefit:* Catch issues before code review, maintain high standards
- *Performance:* Parallel execution, caching, incremental analysis

**🧪 Test Frameworks - Quality Assurance**
- *Standard library testing:* Zero external dependencies for unit tests
- *testify/assert:* Readable assertions and mocking capabilities
- *bubbletea testing:* UI component testing with realistic interactions
- *Developer benefit:* Comprehensive testing with minimal setup overhead

**📦 Build and Release Tools - Deployment Excellence**
- *goreleaser:* Cross-platform builds and GitHub releases
- *Docker multi-stage builds:* Efficient containerization
- *GitHub Actions:* Automated CI/CD pipeline
- *Developer benefit:* One-command releases to all platforms

## Contributing: How You Can Make MCP-TUI Even Better

**Philosophy:** The best architectures evolve through thoughtful contributions from diverse perspectives.

### 🎯 Why Your Contribution Matters

**For the MCP Ecosystem:**
- 🌍 **Accelerate Adoption** - Better tooling means faster MCP ecosystem growth
- 🛡️ **Improve Reliability** - More eyes on the code means fewer bugs in production
- 🚀 **Drive Innovation** - Fresh perspectives lead to breakthrough improvements

**For Your Development:**
- 📊 **Skill Building** - Work with modern Go patterns and architectural principles
- 🤝 **Community Recognition** - Build reputation in the growing MCP community
- 🔧 **Solve Your Problems** - Fix the issues that impact your own workflow

### 🛠️ Contribution Guidelines: Quality Through Structure

**1. 🏗️ Follow Established Architecture Patterns**
- *Why this matters:* Consistency makes the codebase maintainable and predictable
- *How to succeed:* Study existing code, use the same patterns, ask questions in issues
- *Examples:* Use the same error handling patterns, follow the same package structure
- *Benefit:* Your code integrates seamlessly and feels native to the project

**2. 🧪 Add Tests for New Functionality**
- *Why this matters:* Tests prevent regressions and document expected behavior
- *How to succeed:* Write tests first (TDD), cover happy path and error cases
- *Examples:* Unit tests for business logic, integration tests for MCP interactions
- *Benefit:* Confidence that your changes work and won't break in the future

**3. 📝 Update Documentation for Changes**
- *Why this matters:* Undocumented features might as well not exist
- *How to succeed:* Update README, add GoDoc comments, include examples
- *Examples:* New CLI flags, changed behavior, new architecture components
- *Benefit:* Users and future contributors understand how to use your work

**4. 🔍 Use Structured Logging and Error Handling**
- *Why this matters:* Consistent observability makes debugging and monitoring possible
- *How to succeed:* Use the debug package, follow error wrapping patterns
- *Examples:* Component-specific loggers, structured error codes, helpful context
- *Benefit:* Your features integrate with the overall observability strategy

**5. 💻 Maintain Platform Compatibility**
- *Why this matters:* MCP-TUI users work on Windows, macOS, and Linux
- *How to succeed:* Test on multiple platforms, use build tags for OS-specific code
- *Examples:* Process management, signal handling, file path handling
- *Benefit:* Your contribution works for all users, not just your development environment

### 🚀 High-Impact Contribution Opportunities

**🎆 Quick Wins (1-2 hours)**
- 📝 **Documentation Improvements** - Fix typos, add examples, clarify confusing sections
- 🔍 **Test Coverage** - Add tests for uncovered code paths
- 🛡️ **Bug Fixes** - Resolve issues from the GitHub issue tracker
- 🎮 **UI Polish** - Improve keyboard shortcuts, error messages, help text

**📈 Medium Impact (1-2 days)**
- 🔧 **New CLI Commands** - Add missing MCP operations to CLI interface
- 🎮 **TUI Enhancements** - New screens, better navigation, visual improvements
- 🧪 **Test Servers** - Add new problematic server scenarios for testing
- 📊 **Performance Optimizations** - Profile and improve slow operations

**🎆 Major Features (1-2 weeks)**
- 🔌 **Plugin System** - Design and implement extensibility framework
- 🌍 **New Transport Types** - Add support for emerging MCP transport methods
- 📊 **Metrics Dashboard** - Build monitoring and analytics capabilities
- 🤖 **Configuration Management** - Visual configuration editor and validation

Ready to contribute? Start by reading our [Contributing Guide](CONTRIBUTING.md) and checking out the [good first issue](https://github.com/standardbeagle/mcp-tui/labels/good%20first%20issue) label on GitHub!

## Visual Architecture Diagrams

**Note:** While we can't include actual images in markdown, here are text-based representations of key architectural concepts:

### 🔄 Data Flow Architecture

```
🤖 User Input → CLI/TUI Layer → MCP Service → Transport → MCP Server
    ↑                ↓              ↓          ↓         ↓
📊 Results   ← UI Rendering ← Business Logic ← Protocol ← Server Response
    ↑                ↑              ↑          ↑         ↑
🔍 Debug Info ← Error Display ← Error Handling ← Transport ← Error Response
```

### 🏗️ Component Interaction Model

```
┌────────────────────────┐
│      User Interface       │
│  ┌────────┐ ┌────────┐  │
│  │   CLI    │ │   TUI    │  │
│  └────────┘ └────────┘  │
└───────────┬────────────┘
             │
┌────────────┼───────────┐
│     MCP Service Layer      │
│  • Connection Management    │
│  • Tool Operations         │
│  • Resource Access         │
└────────────┼───────────┘
             │
┌────────────┼───────────┐
│    Transport Layer         │
│ ┌──────┐┌─────┐┌──────┐ │
│ │STDIO││HTTP││ SSE  │ │
│ └──────┘└─────┘└──────┘ │
└────────────┼───────────┘
             │
┌────────────┼───────────┐
│      MCP Servers           │
│  • Any Language            │
│  • Any Transport           │
│  • Local or Remote         │
└────────────────────────┘
```

### 🛡️ Error Handling Flow

```
Error Occurs → Wrap with Context → Log Structured Data → User-Friendly Display
     │               │                  │                    │
     v               v                  v                    v
• Network      • Error Code      • Component       • Clear Message
• Protocol     • Stack Trace     • Severity        • Suggested Fix
• Server       • User Context    • Correlation     • Recovery Options
• Tool         • Operation       • Performance     • Help Links
```

### 🚀 Performance Optimization Points

```
User Request → UI Layer → Service Layer → Transport → MCP Server
     │           │          │             │          │
     v           v          v             v          v
• Input      • Async    • Connection  • Pooling   • Caching
  Validation   Operations   Caching       Reuse       Strategies
• Debouncing • Progress  • Schema      • Streaming • Batching
• Caching    Tracking     Validation     JSON        Requests
```

These diagrams illustrate the key architectural concepts and can serve as references when contributing to or extending MCP-TUI.