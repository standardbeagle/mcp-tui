# Task: Implement GetResource Method in Service Layer

**Generated from Master Planning**: 2025-01-25
**Context Package**: `/requests/mcp-tui-v0-6-0/context/`
**Next Phase**: [subtasks-execute.md](subtasks-execute.md)

## Task Sizing Assessment
**File Count**: 3 files - Within target range for atomic functionality
**Estimated Time**: 20 minutes - Single method implementation with tests
**Token Estimate**: 75k tokens - Service method + types + tests
**Complexity Level**: 2 (Moderate) - Following established patterns
**Parallelization Benefit**: LOW - Foundation task required by other tasks
**Atomicity Assessment**: ✅ ATOMIC - Single method implementation cannot be split
**Boundary Analysis**: ✅ CLEAR - Service layer only, no TUI dependencies

## Persona Assignment
**Persona**: Software Engineer
**Expertise Required**: Go interfaces, MCP protocol, error handling
**Worktree**: `~/work/worktrees/mcp-tui-v0-6-0/01-get-resource/`

## Context Summary
**Risk Level**: LOW - Following established CallTool pattern exactly
**Integration Points**: Required by resource CLI and TUI screens
**Architecture Pattern**: MCP operation pattern (service.go:371-441)
**Similar Reference**: `CallTool()` method implementation

### Codebase Context (from master analysis)
**Files in Scope**:
```yaml
read_files:   [/internal/mcp/service.go, /internal/mcp/types.go]
modify_files: [/internal/mcp/service.go, /internal/mcp/types.go] 
create_files: [/internal/mcp/service_get_resource_test.go]
```

**Existing Patterns to Follow**:
- `/internal/mcp/service.go:371-441` - CallTool pattern for operation implementation
- `/internal/mcp/service.go:522-570` - ListPrompts pattern for request/response handling
- `/internal/mcp/errors/handler.go` - Error classification and handling

**Dependencies Context**:
- `github.com/modelcontextprotocol/go-sdk/mcp` - Official MCP SDK with GetResource support

## Task Scope Boundaries

**MODIFY Zone** (Direct Changes):
```yaml
primary_files:
  - /internal/mcp/service.go              # Add GetResource method
  - /internal/mcp/types.go                # Add GetResourceRequest/Result types if missing

direct_dependencies:
  - /internal/mcp/service_test.go         # May need test addition
```

**REVIEW Zone** (Check for Impact):
```yaml
check_integration:
  - /internal/mcp/service.go              # Verify interface compliance
  - /internal/cli/cmd_server.go           # Check if GetResource is referenced
```

**IGNORE Zone** (Do Not Touch):
```yaml
ignore_completely:
  - /internal/tui/screens/*               # TUI layer not relevant
  - /internal/cli/tool.go                 # Tool CLI commands unrelated
  - /examples/*                           # Example configs
  - /tests/*                              # Integration tests
  - /main.go                              # Main application entry

ignore_search_patterns:
  - "**/node_modules/**"
  - "**/build/**"
  - "**/dist/**"
```

**Boundary Analysis Results**:
- **Usage Count**: GetResource not currently used (missing implementation)
- **Scope Assessment**: LIMITED scope - service layer only
- **Impact Radius**: 1-2 files in MODIFY zone, 1-2 files in REVIEW zone

## External Context Sources (from master research)

**Primary Documentation**:
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-06-18) - Resources section 4.3 - GetResource operation definition
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) - Official Go implementation - GetResource method signature

**Standards Applied**:
- [MCP Protocol](https://modelcontextprotocol.io/specification/2025-06-18) - Resource retrieval operation compliance - Request/response schema adherence

**Reference Implementation**:
- CallTool method in service.go - Operation pattern to replicate - Same error handling and logging approach

## Task Requirements

**Objective**: Implement missing GetResource method in MCP service following established CallTool pattern

**Success Criteria**:
- [ ] GetResource method implemented with same pattern as CallTool (lines 371-441)
- [ ] GetResourceRequest and GetResourceResult types defined if missing
- [ ] Error handling follows established errorHandler.ClassifyAndHandle pattern
- [ ] MCP request/response logging integrated (logMCPRequest/logMCPResponse)
- [ ] Method includes connection validation and request ID generation
- [ ] Unit test verifies method functionality and error cases

**Validation Commands**:
```bash
# Pattern Application Verification
grep -A 20 "func.*GetResource" internal/mcp/service.go     # Method exists with proper signature
grep -q "logMCPRequest.*resources/get" internal/mcp/service.go # Logging implemented
go test internal/mcp/service_get_resource_test.go          # Tests pass
go build -o mcp-tui .                                      # Build succeeds
```

## Risk Mitigation (from master analysis)

**High-Risk Mitigations**:
- None identified - following proven pattern reduces risk

**Context Validation**:
- [ ] CallTool pattern from service.go:371-441 successfully adapted
- [ ] MCP Go SDK GetResource method signature verified
- [ ] Error handling pattern matches existing service methods

## Integration with Other Tasks

**Dependencies**: None - foundation task
**Integration Points**: Enables resource CLI commands and TUI resource screen
**Shared Context**: Service interface enhancement used by CLI and TUI tasks

## Documentation from Master Context

### Implementation Pattern Reference
From `/context/implementation-patterns.md` - MCP Operation Pattern:

```go
func (s *service) GetResource(ctx context.Context, req GetResourceRequest) (*GetResourceResult, error) {
    // 1. Connection validation
    if !s.IsConnected() {
        return nil, fmt.Errorf("not connected to MCP server")
    }
    
    // 2. Request ID generation and logging
    s.mu.Lock()
    requestID := s.getNextRequestID()
    s.mu.Unlock()
    logMCPRequest("resources/get", params, requestID)
    
    // 3. SDK operation call
    result, err := s.sessionManager.CurrentSession().Client().GetResource(ctx, params)
    
    // 4. Error handling and classification
    if err != nil {
        logMCPError(-1, err.Error(), requestID)
        return nil, s.errorHandler.ClassifyAndHandle(err, "get_resource")
    }
    
    // 5. Response logging and return
    logMCPResponse(result, requestID)
    return &GetResourceResult{result}, nil
}
```

## Execution Notes
- **Start Pattern**: Copy CallTool method (lines 371-441) and adapt for GetResource
- **Key Context**: Maintain exact same error handling and logging patterns
- **Integration Test**: Verify method integrates with existing session management
- **Review Focus**: Ensure MCP protocol compliance in request/response handling