# Codebase Context Documentation

## Existing Architecture Patterns

### Service Architecture
- **MCP Service Layer**: `internal/mcp/service.go` - Centralized service implementing all MCP operations
- **Transport Abstraction**: `internal/mcp/transports/` - Multiple transport types (stdio, HTTP, SSE)
- **Configuration Management**: `internal/mcp/config/` - Unified configuration system
- **Session Management**: `internal/mcp/session/` - Connection lifecycle management

### Data Layer
- **Types Definition**: `internal/mcp/types.go` - Core MCP data structures
- **Error Handling**: `internal/mcp/errors/` - Classification and handling system
- **Debug Infrastructure**: `internal/mcp/debug/` - Event tracing and middleware

### API Patterns
- **Service Interface**: Clean interface with context-based operations
- **Request/Response Types**: Structured request/response patterns for all MCP operations
- **Async Operations**: Context-based cancellation and timeout support
- **Logging Integration**: Built-in request/response logging with debug support

### Authentication
- **No Authentication Required**: MCP protocol handles auth at transport level
- **Transport Security**: Handled by individual transport implementations

### Configuration
- **Unified Config**: `internal/mcp/config/unified.go` - Centralized configuration management
- **Builder Pattern**: `internal/mcp/config/builder.go` - Configuration construction
- **Validation**: Built-in configuration validation and error reporting

## Similar Feature Implementations

### Tool Implementation (Reference Pattern)
- **Service Methods**: `ListTools()`, `CallTool()` in `internal/mcp/service.go:371-441`
- **TUI Screen**: `internal/tui/screens/tool.go` - Complete tool execution interface
- **CLI Commands**: `internal/cli/tool.go` - Tool listing, description, execution
- **Data Flow**: Service → Screen → UI components → User interaction → Service

### Progress Components (Existing)
- **Progress Bar**: `internal/tui/components/progress.go` - Determinate and indeterminate progress
- **Spinner**: `internal/tui/components/spinner.go` - Multiple spinner styles
- **Usage Pattern**: Used in tool execution screen for operation feedback

### Main Screen Navigation
- **Tab System**: `internal/tui/screens/main.go:33` - activeTab with tools/resources/prompts/events
- **Loading States**: Separate loading states per tab (toolsLoading, resourcesLoading, promptsLoading)
- **Data Management**: Separate arrays for each content type

## Dependency Analysis

### Core Dependencies
- **MCP Go SDK**: `github.com/modelcontextprotocol/go-sdk/mcp` - Official MCP protocol implementation
- **Bubble Tea**: `github.com/charmbracelet/bubbletea` - TUI framework for all UI components
- **Lipgloss**: `github.com/charmbracelet/lipgloss` - Styling system for visual components
- **Cobra**: `github.com/spf13/cobra` - CLI framework for command structure

### Dev Dependencies
- **Go Testing**: Built-in testing framework with comprehensive test coverage
- **Golangci-lint**: Code quality and style enforcement

### External Services
- **MCP Servers**: Various MCP server implementations via stdio/HTTP/SSE transports
- **No external dependencies**: Self-contained application

## File Dependency Mapping

```yaml
high_change_areas:
  - /internal/mcp/service.go: [prompt and resource operations implementation]
  - /internal/tui/screens/main.go: [enhanced tab navigation and content display]
  - /internal/tui/screens/: [new prompt and resource screens]
  - /internal/cli/: [new prompt and resource CLI commands]
  
medium_change_areas:
  - /internal/mcp/types.go: [prompt and resource type definitions]
  - /internal/tui/components/: [enhanced progress components]
  - /main.go: [new CLI command registration]
  
low_change_areas:
  - /internal/debug/: [enhanced MCP message logging]
  - /docs/: [documentation updates]
  - /examples/: [example configurations]
```

## Current Prompt & Resource Support Status

### Service Layer (✅ IMPLEMENTED)
- **ListPrompts**: `internal/mcp/service.go:522` - Full implementation with error handling
- **GetPrompt**: `internal/mcp/service.go:575` - Complete prompt retrieval with argument support
- **ListResources**: `internal/mcp/service.go:442` - Resource enumeration implemented
- **GetResource**: Missing implementation - needs to be added

### TUI Layer (⚠️ PARTIAL)
- **Main Screen**: Has prompt/resource tabs but limited functionality
- **Loading States**: Separate loading states for prompts/resources exist
- **Data Storage**: Arrays for prompts/resources exist but underutilized
- **Navigation**: Tab system supports prompts/resources but needs content screens

### CLI Layer (❌ NOT IMPLEMENTED)
- **Prompt Commands**: Not implemented - needs `cmd_prompt.go`
- **Resource Commands**: Not implemented - needs `cmd_resource.go`
- **Main Registration**: Placeholder commands exist but non-functional

## Progress System Analysis

### Existing Components (✅ AVAILABLE)
- **ProgressBar**: Determinate progress with percentage display
- **IndeterminateProgress**: Moving block animation for unknown duration
- **Spinner**: Three styles (dots, line, circle) with configurable FPS
- **ProgressMessage**: Combines spinner with elapsed time display

### Current Usage Patterns
- **Tool Execution**: Tool screen uses progress components during execution
- **Connection**: Main screen shows connection progress during startup
- **Pattern**: Start spinner → Show progress → Complete with result

### Integration Points
- **Service Layer**: Operations return contexts for cancellation
- **TUI Components**: Progress components integrate with bubble tea framework
- **Time Tracking**: Built-in elapsed time tracking and formatting

## Debug and Logging Infrastructure

### MCP Message Logging (✅ IMPLEMENTED)
- **Request Logging**: `logMCPRequest()` in service.go:22
- **Response Logging**: `logMCPResponse()` in service.go:35  
- **Error Logging**: `logMCPError()` in service.go:45
- **Debug Integration**: All messages logged to debug system

### Event System
- **Event Storage**: Main screen has events array for MCP log entries
- **Real-time Updates**: Event tick system for live updating
- **Debug Screen**: Ctrl+D opens comprehensive debug interface

### Missing Elements
- **Resource Content Display**: No dedicated resource viewing screen
- **Prompt Execution Results**: No prompt result display system
- **Enhanced Message Filtering**: Debug view needs better filtering options