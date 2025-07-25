# External Context Sources

## Primary Documentation

### MCP Protocol Specification
- **MCP Specification**: [https://modelcontextprotocol.io/specification/2025-06-18](https://modelcontextprotocol.io/specification/2025-06-18) - Official protocol specification
- **Relevant Sections**: Prompts (section 4.2), Resources (section 4.3), Progress reporting
- **Implementation Guidance**: Request/response schemas, error handling patterns, transport requirements

### MCP Go SDK Documentation  
- **Go SDK Repository**: [https://github.com/modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) - Official Go implementation
- **Version Used**: Latest stable - needs verification of prompt/resource support completeness
- **Key Insights**: Official types and interfaces, established patterns for MCP operations

### Bubble Tea Framework
- **Bubble Tea**: [https://github.com/charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework documentation
- **Relevant Sections**: Component architecture, message passing, state management
- **Implementation Patterns**: Model-View-Update pattern, async operations, component composition

### Lipgloss Styling
- **Lipgloss**: [https://github.com/charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss) - Styling system
- **Usage Patterns**: Style composition, color schemes, layout systems

## Industry Standards & Best Practices

### Terminal UI Standards
- **TUI Design Principles**: [Terminal UI Guidelines](https://tui-lib.com/guidelines) - User experience standards
- **Applicable Guidelines**: Tab navigation, progress indicators, content display
- **Implementation Requirements**: Keyboard navigation, accessibility, responsive design

### CLI Application Standards
- **Command Line Interface Guidelines**: [GNU Coding Standards](https://www.gnu.org/prep/standards/standards.html#Command_002dLine-Interfaces) - CLI design standards
- **Applicable Sections**: Option parsing, output formatting, error handling
- **Implementation Requirements**: Consistent flag patterns, structured output, help text

### Progress Indication Standards
- **Progress Feedback**: [Progress Indicators](https://www.nngroup.com/articles/progress-indicators/) - UX guidelines for progress display
- **Target Metrics**: Show progress within 1 second, update frequently
- **Implementation Requirements**: Appropriate indicator type per operation duration

## Reference Implementations

### Similar TUI Applications
- **lazygit**: [https://github.com/jesseduffield/lazygit](https://github.com/jesseduffield/lazygit) - Git TUI with excellent navigation patterns
- **Pattern Demonstrated**: Multi-panel layouts, context-sensitive help, progress indicators
- **Adaptation Needed**: Tab navigation, content viewing patterns, operation feedback

### MCP Client Examples
- **Claude Desktop**: Reference implementation for MCP client patterns
- **Integration Pattern**: Service discovery, operation execution, result display
- **Lessons Learned**: User workflow patterns, error handling approaches

### CLI Application Examples
- **Docker CLI**: [https://docs.docker.com/engine/reference/commandline/](https://docs.docker.com/engine/reference/commandline/) - Comprehensive CLI patterns
- **Pattern**: Resource management, status display, operation feedback
- **Adaptation**: Structured output, progress reporting, error handling

## Standards Applied

### MCP Protocol Compliance
- **MCP Protocol Specification**: [Full compliance required](https://modelcontextprotocol.io/specification/2025-06-18)
- **Specific Requirements**: 
  - Prompt execution with argument collection
  - Resource content retrieval and display
  - Progress reporting where supported
  - Error handling per spec

### Go Development Standards
- **Effective Go**: [https://golang.org/doc/effective_go.html](https://golang.org/doc/effective_go.html) - Go best practices
- **Project Standards**: Interface design, error handling, testing patterns

### Terminal Application Standards  
- **Keyboard Navigation**: Standard key bindings (Tab, Arrow keys, Enter, Esc)
- **Progress Feedback**: Immediate feedback for operations >1 second
- **Error Display**: Clear error messages with recovery suggestions

## Architecture References

### Event-Driven UI Architecture
- **Bubble Tea Patterns**: [Examples](https://github.com/charmbracelet/bubbletea/tree/master/examples) - Reference implementations
- **Message Patterns**: Command/message flow, state updates, async operations
- **Component Patterns**: Reusable components, composition strategies

### Service Layer Patterns
- **Go Service Patterns**: Clean architecture with interfaces, dependency injection
- **Error Handling**: Structured error types, context propagation
- **Testing Strategies**: Service mocking, integration testing

### CLI Design Patterns
- **Cobra Framework**: [https://cobra.dev/](https://cobra.dev/) - CLI framework documentation
- **Command Structure**: Hierarchical commands, flag handling, completion
- **Output Formatting**: Structured output, multiple formats (text, JSON)

## Implementation Research

### Prompt Execution Patterns
- **Argument Collection**: Dynamic form generation based on prompt schema
- **Execution Flow**: Validation → Execution → Result display
- **Error Handling**: Schema validation, execution errors, result formatting

### Resource Display Patterns
- **Content Types**: Text, binary, structured data display strategies
- **Pagination**: Large content handling, scroll navigation
- **Metadata Display**: Resource properties, headers, size information

### Progress Reporting Integration
- **Transport Layer**: Progress events from MCP servers (if supported)
- **Local Progress**: Client-side progress for long operations
- **Fallback Strategies**: Spinner when progress unavailable

### Debug Message Display
- **Real-time Logging**: Live message stream display
- **Filtering**: Message type, direction, timestamp filtering
- **Export**: Log export functionality for debugging