# Task: Enhance Progress Indicators for All MCP Operations

**Generated from Master Planning**: 2025-01-25
**Context Package**: `/requests/mcp-tui-v0-6-0/context/`
**Next Phase**: [subtasks-execute.md](subtasks-execute.md)

## Task Sizing Assessment
**File Count**: 4 files - Progress components + service integration + main screen + tool screen
**Estimated Time**: 25 minutes - Enhancing existing progress system
**Token Estimate**: 95k tokens - Progress component enhancements and integration
**Complexity Level**: 2 (Moderate) - Building on existing progress infrastructure
**Parallelization Benefit**: MEDIUM - Can run after screen tasks are complete
**Atomicity Assessment**: ✅ ATOMIC - Complete progress system enhancement
**Boundary Analysis**: ✅ CLEAR - Components and integration points only

## Persona Assignment
**Persona**: Software Engineer
**Expertise Required**: Bubble Tea components, Go concurrency, progress design
**Worktree**: `~/work/worktrees/mcp-tui-v0-6-0/06-progress-enhance/`

## Context Summary
**Risk Level**: LOW - Building on proven progress component foundation
**Integration Points**: All MCP operations, existing progress components
**Architecture Pattern**: Progress integration pattern (tool.go:928-951)
**Similar Reference**: `internal/tui/components/progress.go` - existing progress system

### Codebase Context (from master analysis)
**Files in Scope**:
```yaml
read_files:   [/internal/tui/components/progress.go, /internal/tui/components/spinner.go, /internal/tui/screens/tool.go]
modify_files: [/internal/tui/components/progress.go, /internal/tui/screens/main.go, /internal/mcp/service.go]
create_files: [/internal/tui/components/progress_test.go]
```

**Existing Patterns to Follow**:
- `/internal/tui/components/progress.go:100-110` - ProgressMessage function
- `/internal/tui/screens/tool.go:737-767` - Async operation with progress
- `/internal/tui/screens/main.go:46-50` - Loading states per operation

**Dependencies Context**:
- Existing progress components - spinner, progress bar, indeterminate progress
- All MCP service operations - tools, prompts, resources

## Task Scope Boundaries

**MODIFY Zone** (Direct Changes):
```yaml
primary_files:
  - /internal/tui/components/progress.go  # Enhanced progress components
  - /internal/tui/screens/main.go         # Tab loading progress
  - /internal/mcp/service.go              # Operation progress hooks

create_files:
  - /internal/tui/components/progress_test.go # Unit tests for progress enhancements
```

**REVIEW Zone** (Check for Impact):
```yaml
check_integration:
  - /internal/tui/screens/tool.go         # Existing progress integration
  - /internal/tui/screens/prompt.go       # New screen progress (from task 04)
  - /internal/tui/screens/resource.go     # New screen progress (from task 05)
```

**IGNORE Zone** (Do Not Touch):
```yaml
ignore_completely:
  - /internal/cli/*                       # CLI layer doesn't need progress UI
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
- **Usage Count**: Progress components used across multiple screens
- **Scope Assessment**: MODERATE scope - Component enhancement with integration
- **Impact Radius**: 3 files in MODIFY zone, 3 files in REVIEW zone

## External Context Sources (from master research)

**Primary Documentation**:
- [Progress Indicators UX](https://www.nngroup.com/articles/progress-indicators/) - Show progress within 1 second, update frequently
- [Bubble Tea Examples](https://github.com/charmbracelet/bubbletea/tree/master/examples) - Progress component patterns

**Standards Applied**:
- [Progress Feedback Standards](https://www.nngroup.com/articles/progress-indicators/) - Immediate feedback for operations >1 second
- Project UX consistency - Uniform progress display across all operations

**Reference Implementation**:
- Existing progress components - Foundation to build upon - Spinner, progress bar, message formatting

## Task Requirements

**Objective**: Ensure all MCP operations show appropriate progress indicators with universal coverage

**Success Criteria**:
- [ ] Enhanced ProgressMessage component with operation-specific messaging
- [ ] Universal spinner integration for all service method calls
- [ ] Operation timeout warnings with clear countdown display
- [ ] Progress state management for concurrent operations
- [ ] Graceful progress degradation when operations complete quickly
- [ ] Consistent progress styling across all screens
- [ ] Progress integration in main screen tab loading
- [ ] Progress feedback for connection establishment
- [ ] Error state progress handling (failed operations)
- [ ] Unit tests covering all progress enhancement scenarios

**Validation Commands**:
```bash
# Progress Integration Verification
go build -o mcp-tui .                                      # Build succeeds
# Manual testing - verify progress shows for all operations
./mcp-tui                                                  # Launch and test all tabs
go test internal/tui/components/progress_test.go           # Tests pass
```

## Risk Mitigation (from master analysis)

**High-Risk Mitigations**:
- Performance impact of frequent UI updates - Optimize update frequency and batching
- Progress state conflicts in concurrent operations - Implement proper state isolation

**Context Validation**:
- [ ] Existing progress pattern successfully enhanced
- [ ] Performance impact minimized through efficient update strategies
- [ ] Progress display remains responsive during operations

## Integration with Other Tasks

**Dependencies**: Screen tasks (04, 05) benefit from enhanced progress
**Integration Points**: All service operations, screen implementations
**Shared Context**: Progress components used across all screens

## Documentation from Master Context

### Implementation Pattern Reference
From `/context/implementation-patterns.md` - Progress Integration Pattern:

```go
// Enhanced progress message with operation context
func OperationProgressMessage(operation string, elapsed time.Duration, phase string) string {
    timeStr := formatDuration(elapsed)
    spinner := NewSpinner(SpinnerLine)
    spinnerFrame := spinner.Frame(elapsed)
    
    message := fmt.Sprintf("%s %s", operation, phase)
    return fmt.Sprintf("%s %s (%s)", spinnerFrame, message, timeStr)
}

// Operation-specific progress messages
func ToolExecutionProgress(toolName string, elapsed time.Duration) string {
    return OperationProgressMessage(fmt.Sprintf("Executing tool '%s'", toolName), elapsed, "")
}

func ResourceLoadingProgress(uri string, elapsed time.Duration) string {
    return OperationProgressMessage(fmt.Sprintf("Loading resource"), elapsed, "")
}

func PromptExecutionProgress(promptName string, elapsed time.Duration) string {
    return OperationProgressMessage(fmt.Sprintf("Executing prompt '%s'", promptName), elapsed, "")
}
```

### Universal Progress Integration
```go
// Service operation wrapper with automatic progress
func (s *service) withProgress(operation string, fn func() error) error {
    // Emit progress start event
    s.emitProgressEvent(ProgressStartEvent{Operation: operation})
    
    start := time.Now()
    err := fn()
    elapsed := time.Since(start)
    
    if err != nil {
        s.emitProgressEvent(ProgressErrorEvent{Operation: operation, Error: err, Elapsed: elapsed})
    } else {
        s.emitProgressEvent(ProgressCompleteEvent{Operation: operation, Elapsed: elapsed})
    }
    
    return err
}
```

### Progress State Management
```go
// Screen progress state tracking
type ProgressState struct {
    Active      bool
    Operation   string
    StartTime   time.Time
    Message     string
    CanCancel   bool
    TimeoutWarn bool
}

func (s *Screen) updateProgress() {
    if s.progressState.Active {
        elapsed := time.Since(s.progressState.StartTime)
        
        // Show timeout warning after 10 seconds
        if elapsed > 10*time.Second && !s.progressState.TimeoutWarn {
            s.progressState.TimeoutWarn = true
            s.SetStatus("Operation taking longer than expected...", StatusWarning)
        }
        
        // Update progress message
        s.progressMessage = OperationProgressMessage(s.progressState.Operation, elapsed, "")
    }
}
```

### Timeout Handling Enhancement
```go
// Enhanced timeout display with countdown
func TimeoutWarningMessage(elapsed time.Duration, timeout time.Duration) string {
    remaining := timeout - elapsed
    if remaining <= 0 {
        return "⚠️  Operation timeout - cancelling..."
    }
    
    if remaining < 10*time.Second {
        return fmt.Sprintf("⚠️  Timeout in %ds - press Esc to cancel", int(remaining.Seconds()))
    }
    
    return fmt.Sprintf("⚠️  Operation taking longer than expected (%s elapsed)", formatDuration(elapsed))
}
```

## Execution Notes
- **Start Pattern**: Enhance existing progress components with operation-specific messaging
- **Key Context**: Ensure progress shows immediately for all operations >100ms
- **Integration Test**: Verify progress appears consistently across all MCP operations  
- **Review Focus**: Ensure progress doesn't impact UI responsiveness or performance