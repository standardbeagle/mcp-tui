# Task: Create Resource Screen for TUI

**Generated from Master Planning**: 2025-01-25
**Context Package**: `/requests/mcp-tui-v0-6-0/context/`
**Next Phase**: [subtasks-execute.md](subtasks-execute.md)

## Task Sizing Assessment
**File Count**: 3 files - New screen + test + main screen integration
**Estimated Time**: 30 minutes - Content display screen with various content types
**Token Estimate**: 110k tokens - Screen implementation with content handling
**Complexity Level**: 3 (Complex) - Content type detection and display strategies
**Parallelization Benefit**: HIGH - Fully independent from prompt screen
**Atomicity Assessment**: ✅ ATOMIC - Complete resource screen functionality
**Boundary Analysis**: ✅ CLEAR - TUI layer only, uses GetResource method

## Persona Assignment
**Persona**: Software Engineer
**Expertise Required**: Bubble Tea framework, content display, binary/text handling
**Worktree**: `~/work/worktrees/mcp-tui-v0-6-0/05-resource-screen/`

## Context Summary
**Risk Level**: MEDIUM - Content display complexity for various resource types
**Integration Points**: Main screen navigation, service GetResource method
**Architecture Pattern**: Screen creation pattern (tool.go:84-100)
**Similar Reference**: `internal/tui/screens/tool.go` - result display patterns

### Codebase Context (from master analysis)
**Files in Scope**:
```yaml
read_files:   [/internal/tui/screens/tool.go, /internal/tui/screens/main.go, /internal/mcp/service.go]
modify_files: [/internal/tui/screens/main.go]
create_files: [/internal/tui/screens/resource.go, /internal/tui/screens/resource_test.go]
```

**Existing Patterns to Follow**:
- `/internal/tui/screens/tool.go:84-100` - Screen creation and initialization  
- `/internal/tui/screens/tool.go:1121-1190` - Result parsing and field extraction
- `/internal/tui/screens/tool.go:975-1035` - Result viewing mode with navigation
- `/internal/tui/components/progress.go:67-97` - Progress for content loading

**Dependencies Context**:
- `github.com/charmbracelet/bubbletea` - TUI framework
- Task 01 GetResource method - required for content retrieval

## Task Scope Boundaries

**MODIFY Zone** (Direct Changes):
```yaml
primary_files:
  - /internal/tui/screens/resource.go     # New resource screen implementation
  - /internal/tui/screens/main.go         # Add resource screen navigation

create_files:
  - /internal/tui/screens/resource.go     # Complete resource screen
  - /internal/tui/screens/resource_test.go # Unit tests for resource screen
```

**REVIEW Zone** (Check for Impact):
```yaml
check_integration:
  - /internal/tui/screens/tool.go         # Pattern consistency verification
  - /internal/mcp/service.go              # GetResource method availability  
  - /internal/tui/components/progress.go  # Progress component usage
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
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-06-18) - Resources section 4.3 - Content types and metadata
- [Bubble Tea Examples](https://github.com/charmbracelet/bubbletea/tree/master/examples) - Content display and scrolling

**Standards Applied**:
- [TUI Design Principles](https://tui-lib.com/guidelines) - Content display and navigation
- Project TUI patterns - Consistent with tool result display

**Reference Implementation**:
- Tool screen result display - Content viewing patterns - Scrolling, field extraction, copy functionality

## Task Requirements

**Objective**: Create dedicated resource screen for content viewing with metadata and various content type support

**Success Criteria**:
- [ ] ResourceScreen struct following tool screen pattern
- [ ] Resource loading with progress indicators during content retrieval
- [ ] Content type detection and appropriate display strategies
- [ ] Text content display with syntax highlighting hints
- [ ] Binary content display with hex dump and ASCII preview
- [ ] Large content handling with pagination and scrolling
- [ ] Metadata display (size, type, URI, headers if available)
- [ ] Search functionality within text content
- [ ] Copy functionality for content sections
- [ ] Navigation integration with main screen
- [ ] Error handling for content access failures
- [ ] Unit tests covering content display and navigation

**Validation Commands**:
```bash
# Screen Integration Verification
go build -o mcp-tui .                                      # Build succeeds
# Manual TUI test - navigate to resource screen
./mcp-tui                                                  # TUI launches
go test internal/tui/screens/resource_test.go              # Tests pass
```

## Risk Mitigation (from master analysis)

**High-Risk Mitigations**:
- Large content performance - Implement pagination and lazy loading
- Binary content display - Provide safe hex dump view with limits
- Content type detection - Implement fallback strategies for unknown types

**Context Validation**:
- [ ] Tool screen result display pattern successfully adapted
- [ ] Content display handles various types appropriately  
- [ ] Progress integration works for content loading

## Integration with Other Tasks

**Dependencies**: Task 01 (GetResource method) - required for content retrieval
**Integration Points**: Main screen navigation, shared progress components
**Shared Context**: Screen patterns shared with prompt screen task

## Documentation from Master Context

### Implementation Pattern Reference
From `/context/implementation-patterns.md` - Screen Creation Pattern:

```go
// ResourceScreen structure following tool screen pattern
type ResourceScreen struct {
    *BaseScreen
    logger debug.Logger
    
    // Resource info
    resource   mcp.Resource
    mcpService mcp.Service
    
    // Content state
    content       string
    contentType   string
    contentSize   int64
    loading       bool
    loadingStart  time.Time
    
    // Display state
    viewMode      int  // 0=content, 1=metadata, 2=raw
    scrollPos     int
    searchQuery   string
    searchMode    bool
    
    // Content display
    lines         []string
    visibleLines  int
    maxLineWidth  int
}
```

### Content Display Strategy
```go
// Content type handling (from architecture decisions)
func (rs *ResourceScreen) displayContent() string {
    switch {
    case isTextContent(rs.contentType):
        return rs.displayTextContent()
    case isBinaryContent(rs.contentType):
        return rs.displayBinaryContent()
    case isJSONContent(rs.contentType):
        return rs.displayJSONContent()
    default:
        return rs.displayRawContent()
    }
}

func (rs *ResourceScreen) displayTextContent() string {
    // Line-based display with scroll support
    // Search highlighting
    // Line numbers for large content
}

func (rs *ResourceScreen) displayBinaryContent() string {
    // Hex dump with ASCII preview
    // Size limits to prevent UI freeze
    // Clear binary content indicators
}
```

### Content Loading Pattern
```go
// Async content loading (adapted from tool execution pattern)
func (rs *ResourceScreen) loadContent() tea.Cmd {
    rs.loading = true
    rs.loadingStart = time.Now()
    
    return tea.Batch(
        // Progress ticker
        tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
            return resourceSpinnerTickMsg{}
        }),
        // Content loading
        func() tea.Msg {
            ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
            defer cancel()
            
            result, err := rs.mcpService.GetResource(ctx, GetResourceRequest{
                URI: rs.resource.URI,
            })
            
            return resourceLoadedMsg{
                Content: result,
                Error:   err,
            }
        },
    )
}
```

## Execution Notes
- **Start Pattern**: Copy tool.go screen structure, focus on result display patterns
- **Key Context**: Emphasize content type detection and appropriate display strategies
- **Integration Test**: Verify screen navigation and content loading work correctly
- **Review Focus**: Ensure content display is safe and performant for various content types