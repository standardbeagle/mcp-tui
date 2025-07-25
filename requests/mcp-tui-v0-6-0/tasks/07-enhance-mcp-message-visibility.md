# Task: Enhance MCP Message Visibility and Debug Features

**Generated from Master Planning**: 2025-01-25
**Context Package**: `/requests/mcp-tui-v0-6-0/context/`
**Next Phase**: [subtasks-execute.md](subtasks-execute.md)

## Task Sizing Assessment
**File Count**: 4 files - Debug screen + event system + message filtering + export
**Estimated Time**: 30 minutes - Enhanced debug interface with filtering and export
**Token Estimate**: 100k tokens - Debug screen enhancements and message handling
**Complexity Level**: 2 (Moderate) - Building on existing debug infrastructure
**Parallelization Benefit**: LOW - Benefits from other task completion for testing
**Atomicity Assessment**: ✅ ATOMIC - Complete debug enhancement functionality
**Boundary Analysis**: ✅ CLEAR - Debug layer only, non-intrusive enhancement

## Persona Assignment
**Persona**: Software Engineer
**Expertise Required**: Bubble Tea UI, debug systems, message filtering
**Worktree**: `~/work/worktrees/mcp-tui-v0-6-0/07-debug-enhance/`

## Context Summary
**Risk Level**: LOW - Enhancement to existing proven debug system
**Integration Points**: Debug screen, MCP logging, event system
**Architecture Pattern**: Debug enhancement pattern (debug screen exists)
**Similar Reference**: `internal/tui/screens/debug.go` - existing debug interface

### Codebase Context (from master analysis)
**Files in Scope**:
```yaml
read_files:   [/internal/tui/screens/debug.go, /internal/debug/logger.go, /internal/mcp/service.go]
modify_files: [/internal/tui/screens/debug.go, /internal/debug/logger.go]
create_files: [/internal/debug/message_filter.go, /internal/tui/screens/debug_test.go]
```

**Existing Patterns to Follow**:
- `/internal/mcp/service.go:22-66` - MCP message logging functions
- `/internal/tui/screens/main.go:38` - Event storage and display
- `/internal/debug/logger.go` - Debug infrastructure foundation

**Dependencies Context**:
- Existing debug system with MCP message logging
- Debug screen accessible via Ctrl+D
- Event system for real-time updates

## Task Scope Boundaries

**MODIFY Zone** (Direct Changes):
```yaml
primary_files:
  - /internal/tui/screens/debug.go        # Enhanced debug screen with filtering
  - /internal/debug/logger.go             # Message filtering and export support

create_files:
  - /internal/debug/message_filter.go     # Message filtering logic
  - /internal/tui/screens/debug_test.go   # Unit tests for debug enhancements
```

**REVIEW Zone** (Check for Impact):
```yaml
check_integration:
  - /internal/mcp/service.go              # MCP logging integration
  - /internal/tui/screens/main.go         # Event system integration
  - /internal/debug/mcp_logger.go         # MCP-specific logging
```

**IGNORE Zone** (Do Not Touch):
```yaml
ignore_completely:
  - /internal/cli/*                       # CLI layer
  - /internal/mcp/config/*                # Configuration layer
  - /internal/tui/screens/tool.go         # Tool screen
  - /examples/*                           # Example files

ignore_search_patterns:
  - "**/node_modules/**"
  - "**/build/**"
  - "**/dist/**"
  - "**/*.min.js"
```

**Boundary Analysis Results**:
- **Usage Count**: Debug screen accessed via Ctrl+D from any screen
- **Scope Assessment**: LIMITED scope - Debug layer enhancement
- **Impact Radius**: 2 files in MODIFY zone, 3 files in REVIEW zone

## External Context Sources (from master research)

**Primary Documentation**:
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-06-18) - Message format and protocol compliance
- [Bubble Tea Examples](https://github.com/charmbracelet/bubbletea/tree/master/examples) - Filter and search UI patterns

**Standards Applied**:
- [Debug Interface Standards](https://tui-lib.com/guidelines) - Filter UI, export functionality, real-time updates
- Project debug patterns - Consistent with existing debug infrastructure

**Reference Implementation**:
- Existing debug screen - Foundation to enhance - Message display, real-time updates

## Task Requirements

**Objective**: Enhance debug screen with comprehensive MCP message visibility, filtering, and export capabilities

**Success Criteria**:
- [ ] Message type filtering (requests, responses, notifications, errors)
- [ ] Direction filtering (incoming, outgoing, both)
- [ ] Real-time message streaming with auto-scroll option
- [ ] Message search functionality with highlight
- [ ] Timestamp-based message filtering (last 1m, 5m, 1h, all)
- [ ] Message export to file (JSON and text formats)
- [ ] Message details view with pretty-printed JSON
- [ ] Connection state indicators in debug view
- [ ] Message statistics (count by type, error rate)
- [ ] Clear message history functionality
- [ ] Enhanced keyboard navigation for filtering
- [ ] Unit tests covering filtering and export functionality

**Validation Commands**:
```bash
# Debug Enhancement Verification
go build -o mcp-tui .                                      # Build succeeds
# Manual testing - access debug screen (Ctrl+D) and verify features
./mcp-tui                                                  # Launch TUI
# Test MCP operations and verify messages appear in debug screen
go test internal/tui/screens/debug_test.go                 # Tests pass
```

## Risk Mitigation (from master analysis)

**High-Risk Mitigations**:
- Message buffer overflow with high-frequency operations - Implement circular buffer with size limits
- UI performance with large message volumes - Add message batching and lazy rendering

**Context Validation**:
- [ ] Existing debug infrastructure successfully enhanced
- [ ] Message filtering doesn't impact logging performance
- [ ] Export functionality handles large message volumes

## Integration with Other Tasks

**Dependencies**: Benefits from all other tasks generating MCP messages for testing
**Integration Points**: All MCP operations, existing debug system
**Shared Context**: Debug enhancement improves development experience for all features

## Documentation from Master Context

### Implementation Pattern Reference
From `/context/implementation-patterns.md` - Debug Enhancement Pattern:

```go
// Enhanced debug screen with filtering
type DebugScreen struct {
    *BaseScreen
    
    // Message storage and filtering
    messages      []debug.MCPLogEntry
    filteredMsgs  []debug.MCPLogEntry
    filters       MessageFilters
    searchQuery   string
    searchMode    bool
    
    // Display state
    selectedIndex int
    autoScroll    bool
    showDetails   bool
    detailsIndex  int
    
    // Statistics
    stats         MessageStats
}

type MessageFilters struct {
    MessageTypes []string  // requests, responses, notifications, errors
    Direction    string    // incoming, outgoing, both
    TimeRange    string    // 1m, 5m, 1h, all
    SearchQuery  string    // text search in message content
}

type MessageStats struct {
    TotalMessages int
    RequestCount  int
    ResponseCount int
    ErrorCount    int
    NotificationCount int
}
```

### Message Filtering Implementation
```go
// Message filtering logic
func (ds *DebugScreen) applyFilters() {
    ds.filteredMsgs = []debug.MCPLogEntry{}
    
    for _, msg := range ds.messages {
        if ds.matchesFilters(msg) {
            ds.filteredMsgs = append(ds.filteredMsgs, msg)
        }
    }
    
    // Update statistics
    ds.updateStats()
}

func (ds *DebugScreen) matchesFilters(msg debug.MCPLogEntry) bool {
    // Type filtering
    if len(ds.filters.MessageTypes) > 0 {
        if !contains(ds.filters.MessageTypes, msg.Type) {
            return false
        }
    }
    
    // Direction filtering
    if ds.filters.Direction != "both" {
        if msg.Direction != ds.filters.Direction {
            return false
        }
    }
    
    // Time range filtering
    if !ds.matchesTimeRange(msg.Timestamp) {
        return false
    }
    
    // Search query filtering
    if ds.filters.SearchQuery != "" {
        if !strings.Contains(strings.ToLower(msg.Content), 
                           strings.ToLower(ds.filters.SearchQuery)) {
            return false
        }
    }
    
    return true
}
```

### Export Functionality
```go
// Message export with format support
func (ds *DebugScreen) exportMessages(format string) error {
    filename := fmt.Sprintf("mcp-messages-%s.%s", 
                           time.Now().Format("20060102-150405"), format)
    
    file, err := os.Create(filename)
    if err != nil {
        return err
    }
    defer file.Close()
    
    switch format {
    case "json":
        return ds.exportJSON(file)
    case "txt":
        return ds.exportText(file)
    default:
        return fmt.Errorf("unsupported format: %s", format)
    }
}

func (ds *DebugScreen) exportJSON(w io.Writer) error {
    encoder := json.NewEncoder(w)
    encoder.SetIndent("", "  ")
    
    exportData := map[string]interface{}{
        "exportTime": time.Now(),
        "messageCount": len(ds.filteredMsgs),
        "filters": ds.filters,
        "statistics": ds.stats,
        "messages": ds.filteredMsgs,
    }
    
    return encoder.Encode(exportData)
}
```

### Real-time Message Updates
```go
// Enhanced message streaming with filtering
func (ds *DebugScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case debug.MCPLogEntry:
        // Add new message
        ds.messages = append(ds.messages, msg)
        
        // Apply filters to update filtered view
        ds.applyFilters()
        
        // Auto-scroll if enabled and at bottom
        if ds.autoScroll && ds.selectedIndex == len(ds.filteredMsgs)-2 {
            ds.selectedIndex = len(ds.filteredMsgs) - 1
        }
        
        return ds, nil
        
    case tea.KeyMsg:
        return ds.handleKeyMsg(msg)
    }
    
    return ds, nil
}
```

## Execution Notes
- **Start Pattern**: Enhance existing debug screen with filtering and export capabilities
- **Key Context**: Maintain real-time performance while adding comprehensive filtering
- **Integration Test**: Verify all MCP operations generate visible debug messages
- **Review Focus**: Ensure debug enhancements don't impact application performance