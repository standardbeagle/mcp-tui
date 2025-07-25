# Task: Create Prompt Screen for TUI

**Generated from Master Planning**: 2025-01-25
**Context Package**: `/requests/mcp-tui-v0-6-0/context/`
**Next Phase**: [subtasks-execute.md](subtasks-execute.md)

## Task Sizing Assessment
**File Count**: 3 files - New screen + test + enhanced main screen integration
**Estimated Time**: 30 minutes - Complex screen with form handling and execution
**Token Estimate**: 120k tokens - Complete screen implementation with form logic
**Complexity Level**: 3 (Complex) - Prompt argument collection and execution workflow
**Parallelization Benefit**: HIGH - Independent from resource screen development
**Atomicity Assessment**: ✅ ATOMIC - Complete prompt screen functionality
**Boundary Analysis**: ✅ CLEAR - TUI layer only, uses existing service methods

## Persona Assignment
**Persona**: Software Engineer
**Expertise Required**: Bubble Tea framework, form handling, Go TUI patterns
**Worktree**: `~/work/worktrees/mcp-tui-v0-6-0/04-prompt-screen/`

## Context Summary
**Risk Level**: MEDIUM - Complex form generation and validation logic
**Integration Points**: Main screen navigation, service layer prompt methods
**Architecture Pattern**: Screen creation pattern (tool.go:84-100)
**Similar Reference**: `internal/tui/screens/tool.go` - complete screen implementation

### Codebase Context (from master analysis)
**Files in Scope**:
```yaml
read_files:   [/internal/tui/screens/tool.go, /internal/tui/screens/main.go, /internal/mcp/types.go]
modify_files: [/internal/tui/screens/main.go]
create_files: [/internal/tui/screens/prompt.go, /internal/tui/screens/prompt_test.go]
```

**Existing Patterns to Follow**:
- `/internal/tui/screens/tool.go:84-100` - Screen creation and initialization
- `/internal/tui/screens/tool.go:267-332` - Schema parsing for form generation
- `/internal/tui/screens/tool.go:646-768` - Async execution with progress display
- `/internal/tui/screens/tool.go:928-951` - Progress integration in View method

**Dependencies Context**:
- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/bubbles/textinput` - Form input components
- Existing service ListPrompts/GetPrompt methods

## Task Scope Boundaries

**MODIFY Zone** (Direct Changes):
```yaml
primary_files:
  - /internal/tui/screens/prompt.go       # New prompt screen implementation
  - /internal/tui/screens/main.go         # Add prompt screen navigation

create_files:
  - /internal/tui/screens/prompt.go       # Complete prompt screen
  - /internal/tui/screens/prompt_test.go  # Unit tests for prompt screen
```

**REVIEW Zone** (Check for Impact):
```yaml
check_integration:
  - /internal/tui/screens/tool.go         # Ensure pattern consistency
  - /internal/tui/components/progress.go  # Verify progress component usage
  - /internal/mcp/service.go              # Confirm prompt method availability
```

**IGNORE Zone** (Do Not Touch):
```yaml
ignore_completely:
  - /internal/cli/*                       # CLI layer
  - /internal/mcp/config/*                # Configuration layer
  - /examples/*                           # Example files
  - /internal/debug/*                     # Debug infrastructure

ignore_search_patterns:
  - "**/node_modules/**"
  - "**/build/**"
  - "**/dist/**"
  - "**/*.min.js"
```

**Boundary Analysis Results**:
- **Usage Count**: New functionality integrated with main screen
- **Scope Assessment**: MODERATE scope - TUI screen layer
- **Impact Radius**: 2 files in MODIFY zone, 3 files in REVIEW zone

## External Context Sources (from master research)

**Primary Documentation**:
- [Bubble Tea Examples](https://github.com/charmbracelet/bubbletea/tree/master/examples) - Form handling and async operations
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-06-18) - Prompts section 4.2 - Argument schema and execution

**Standards Applied**:
- [TUI Design Principles](https://tui-lib.com/guidelines) - Form navigation and user experience
- Project TUI patterns - Consistent with tool screen navigation and execution

**Reference Implementation**:
- Tool screen implementation - Complete pattern to adapt - Form generation, execution workflow, progress display

## Task Requirements

**Objective**: Create dedicated prompt screen for argument collection and execution with results display

**Success Criteria**:
- [ ] PromptScreen struct following ToolScreen pattern (tool.go:25-65)
- [ ] Dynamic form generation based on prompt argument schema
- [ ] Form validation and input handling for required/optional arguments
- [ ] Async prompt execution with progress indicators
- [ ] Result display with JSON formatting and field extraction
- [ ] Navigation integration with main screen (tab or selection)
- [ ] Error handling and user feedback for execution failures
- [ ] Keyboard navigation consistent with tool screen
- [ ] Copy/paste support for results
- [ ] Unit tests covering form generation and execution flow

**Validation Commands**:
```bash
# Screen Integration Verification
go build -o mcp-tui .                                      # Build succeeds
# Manual TUI test - navigate to prompt screen and verify functionality
./mcp-tui                                                  # TUI launches
# Press appropriate keys to access prompt screen
go test internal/tui/screens/prompt_test.go                # Tests pass
```

## Risk Mitigation (from master analysis)

**High-Risk Mitigations**:
- Complex form generation - Follow tool screen schema parsing pattern exactly
- Argument validation - Implement client-side validation before execution
- Result display for various content types - Use existing tool result patterns

**Context Validation**:
- [ ] Tool screen pattern from tool.go successfully adapted
- [ ] Form generation handles various argument types correctly
- [ ] Progress integration matches tool screen implementation

## Integration with Other Tasks

**Dependencies**: Service layer prompt methods (already exist)
**Integration Points**: Main screen navigation, shared progress components
**Shared Context**: Screen patterns shared with resource screen task

## Documentation from Master Context

### Implementation Pattern Reference
From `/context/implementation-patterns.md` - Screen Creation Pattern:

```go
// PromptScreen structure following tool screen pattern
type PromptScreen struct {
    *BaseScreen
    logger debug.Logger
    
    // Prompt info
    prompt     mcp.Prompt
    mcpService mcp.Service
    
    // Form fields for prompt arguments
    fields []promptField
    cursor int
    
    // Execution state
    executing      bool
    executionStart time.Time
    result         *mcp.GetPromptResult
    resultJSON     string
    
    // Result viewing
    viewingResult bool
    resultFields  []resultField
    resultCursor  int
}

type promptField struct {
    name        string
    description string
    fieldType   string
    required    bool
    input       textinput.Model
    validationError string
}
```

### Form Generation Pattern
```go
// Parse prompt schema to create form fields (adapted from tool.go:267-332)
func (ps *PromptScreen) parseSchema() {
    ps.fields = []promptField{}
    
    if ps.prompt.Arguments == nil {
        return
    }
    
    // Parse argument schema similar to tool input schema
    // Create textinput fields for each argument
    // Handle required/optional arguments
    // Set appropriate placeholders and validation
}
```

### Execution Pattern  
```go
// Execute prompt with collected arguments (adapted from tool.go:646-768)
func (ps *PromptScreen) executePrompt() tea.Cmd {
    ps.executing = true
    ps.executionStart = time.Now()
    
    return tea.Batch(
        // Progress ticker
        tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
            return promptSpinnerTickMsg{}
        }),
        // Prompt execution
        func() tea.Msg {
            // Collect form arguments
            // Call service.GetPrompt()
            // Return completion message
        },
    )
}
```

## Execution Notes
- **Start Pattern**: Copy tool.go screen structure and adapt for prompts
- **Key Context**: Focus on dynamic form generation based on prompt argument schema
- **Integration Test**: Verify screen navigation from main screen works
- **Review Focus**: Ensure argument collection and validation work correctly