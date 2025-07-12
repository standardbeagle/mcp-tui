# Todo: Complete MCP Transport Implementation & Critical Issue Resolution

**Generated from**: Full Planning on 2025-07-11
**Next Phase**: [tasks-execute.md](tasks-execute.md)

## Context Summary
- **Risk Level**: HIGH | **Project Phase**: Beta (Critical functionality missing)
- **Estimated Effort**: 25-35 hours | **Files**: 12+ core files affected
- **Feature Flag Required**: No (implementing missing core functionality)

## Context & Background
**Request**: Fix all critical issues identified in QA assessment, especially implementing STDIO and HTTP transport support
**Analysis Date**: 2025-07-11
**Estimated Effort**: 25-35 hours
**Risk Level**: HIGH (Core functionality missing, extensive test failures)

### Codebase Context
**Existing Functionality**: 
- ✅ SSE transport implemented and working - Files: `internal/mcp/service.go:150-154`
- ✅ Process management infrastructure exists - Files: `internal/platform/process/manager.go`
- ✅ Official MCP Go SDK v0.2.0 integrated - Available: `NewCommandTransport`, `NewStdioTransport`
- ❌ STDIO transport completely missing - Location: `internal/mcp/service.go:147-148`
- ❌ HTTP transport missing - Location: `internal/mcp/service.go:157-158`
- ❌ Command validation not implemented - Security vulnerability
- ⚠️ 47+ test failures due to missing transports and validation

**Similar Implementations**: 
- `internal/mcp/service.go:150-154` - SSE transport pattern to follow for HTTP
- `internal/platform/process/manager.go` - Process management for STDIO commands
- `internal/mcp/http_debug.go` - HTTP client patterns for HTTP transport

**Dependencies**: 
- `github.com/modelcontextprotocol/go-sdk@v0.2.0` - Official SDK with transport support
- `os/exec` - For STDIO command execution
- `net/http` - For HTTP transport implementation

**Architecture Integration**:
- Transport implementations fit into existing switch statement in Connect method
- Will interact with: CLI argument parsing, TUI connection screens, error handling
- Data flow: Config → Transport → Official SDK → MCP Server

### External Context Sources
**Primary Documentation**:
- [MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk) - Official transport API patterns
- [MCP Specification](https://modelcontextprotocol.io/specification/2025-06-18) - Protocol compliance requirements

**Code References**:
- [Official SDK Examples](https://github.com/modelcontextprotocol/go-sdk/blob/main/examples) - STDIO and HTTP transport patterns
- [Go exec.Command](https://pkg.go.dev/os/exec) - Command execution patterns for STDIO

**Standards Applied**:
- [Go Security](https://golang.org/doc/security) - Command injection prevention
- [MCP Transport Spec](https://modelcontextprotocol.io) - Transport implementation standards

**Performance/Security Context**:
- Baseline: SSE transport working, no validation
- Target: All transports working with command validation
- Constraints: Must prevent command injection, maintain backward compatibility

### User/Business Context
- **User Need**: Restore primary advertised functionality (STDIO), add HTTP support
- **Success Criteria**: All documented examples work, tests pass, no security vulnerabilities
- **Project Phase**: Critical fixes for Beta release
- **Timeline**: Immediate (blocking user adoption)

## Implementation Plan

### Phase 1: Critical STDIO Transport Implementation (Risk: HIGH)
**Files**: `internal/mcp/service.go`, `internal/config/validation.go` (new)
**Objective**: Implement working STDIO transport with command validation
**Validation**: `./mcp-tui "npx -y @modelcontextprotocol/server-everything stdio" tool list`

- [ ] Task 1.1: Add command validation security module
  - **Risk**: HIGH - Security vulnerability if commands not validated properly
  - **Files**: `internal/config/validation.go` (new)
  - **Success Criteria**: 
    - [ ] Dangerous commands (containing `;`, `&`, `|`, etc.) rejected
    - [ ] Path validation prevents directory traversal
    - [ ] Test `TestCommandValidation/dangerous_command_rejected` passes
  - **Implementation**: Create validation functions for command security
  - **Rollback**: Remove validation module if issues arise

- [ ] Task 1.2: Implement STDIO transport using official SDK
  - **Risk**: HIGH - Core functionality, must work correctly
  - **Files**: `internal/mcp/service.go:147-148`
  - **Success Criteria**: 
    - [ ] STDIO case uses `NewCommandTransport(exec.Command(config.Command, config.Args...))`
    - [ ] Integration with official SDK connect flow
    - [ ] Test command `./mcp-tui "npx -y @modelcontextprotocol/server-everything stdio" tool list` works
  - **Implementation**: Replace TODO with proper CommandTransport implementation
  - **Rollback**: Revert to error return if transport fails

- [ ] Task 1.3: Update default transport configuration
  - **Risk**: MEDIUM - Changes user experience but improves functionality
  - **Files**: CLI flag defaults, `internal/config/config.go`
  - **Success Criteria**: 
    - [ ] Default transport changed from "stdio" to "sse" until STDIO stable
    - [ ] Clear error messages when STDIO fails
    - [ ] Backward compatibility maintained
  - **Implementation**: Update default transport handling
  - **Rollback**: Revert default changes

### Phase 2: HTTP Transport Implementation (Risk: MEDIUM)
**Files**: `internal/mcp/service.go`
**Objective**: Implement HTTP transport for complete protocol coverage
**Validation**: `./mcp-tui --transport http --url http://localhost:8080/mcp tool list`

- [ ] Task 2.1: Research HTTP transport pattern in official SDK
  - **Risk**: MEDIUM - Need to understand proper implementation
  - **Files**: Research only
  - **Success Criteria**: 
    - [ ] Understand if HTTP transport exists in official SDK
    - [ ] Document implementation approach
    - [ ] Identify any missing SDK functionality
  - **Implementation**: Review official SDK documentation and examples
  - **Rollback**: N/A (research only)

- [ ] Task 2.2: Implement HTTP transport or wrapper
  - **Risk**: MEDIUM - New functionality, may need custom implementation
  - **Files**: `internal/mcp/service.go:157-158`
  - **Success Criteria**: 
    - [ ] HTTP case properly implemented
    - [ ] Works with standard HTTP MCP servers
    - [ ] Follows SDK patterns where possible
  - **Implementation**: Use SDK HTTP transport or create wrapper
  - **Rollback**: Revert to error return if implementation fails

- [ ] Task 2.3: Add HTTP transport testing
  - **Risk**: LOW - Testing new functionality
  - **Files**: `internal/mcp/http_transport_test.go` (new)
  - **Success Criteria**: 
    - [ ] HTTP transport unit tests
    - [ ] Integration test with mock HTTP server
    - [ ] Error handling tests
  - **Implementation**: Comprehensive test coverage for HTTP transport
  - **Rollback**: Remove test file if issues arise

### Phase 3: Documentation and User Experience Fixes (Risk: LOW)
**Files**: `README.md`, help text, examples
**Objective**: Align documentation with actual functionality
**Validation**: All documented examples work without errors

- [ ] Task 3.1: Update README.md examples
  - **Risk**: LOW - Documentation only
  - **Files**: `README.md`, CLI help text
  - **Success Criteria**: 
    - [ ] All examples use working transports
    - [ ] Clear instructions for each transport type
    - [ ] Migration guide for transport selection
  - **Implementation**: Replace non-working examples with SSE/working alternatives
  - **Rollback**: Revert documentation changes

- [ ] Task 3.2: Update CLI help text and defaults
  - **Risk**: LOW - User interface improvements
  - **Files**: CLI command definitions, help text
  - **Success Criteria**: 
    - [ ] Help text matches actual functionality
    - [ ] Examples in help work correctly
    - [ ] Clear transport selection guidance
  - **Implementation**: Update help text to reflect working transports
  - **Rollback**: Revert help text changes

- [ ] Task 3.3: Add comprehensive transport usage documentation
  - **Risk**: LOW - Documentation enhancement
  - **Files**: `TRANSPORT_GUIDE.md` (new)
  - **Success Criteria**: 
    - [ ] Complete guide for each transport type
    - [ ] Troubleshooting section for common issues
    - [ ] Examples for different use cases
  - **Implementation**: Create comprehensive transport documentation
  - **Rollback**: Remove new documentation file

### Phase 4: Test Suite Restoration (Risk: MEDIUM)
**Files**: Multiple test files across the project
**Objective**: Fix failing tests and restore quality assurance
**Validation**: `go test ./... -v` shows majority of tests passing

- [ ] Task 4.1: Fix transport-dependent test failures
  - **Risk**: MEDIUM - Many interconnected test failures
  - **Files**: `internal/mcp/*_test.go`, `integration_test.go`
  - **Success Criteria**: 
    - [ ] Connection tests pass with implemented transports
    - [ ] Command validation tests pass
    - [ ] Integration tests work with real transports
  - **Implementation**: Update tests to use working transports, fix validation
  - **Rollback**: Skip failing tests if fixes break other functionality

- [ ] Task 4.2: Separate unit tests from integration tests
  - **Risk**: MEDIUM - Test organization refactoring
  - **Files**: Test file organization, build tags
  - **Success Criteria**: 
    - [ ] Unit tests run without external dependencies
    - [ ] Integration tests clearly separated
    - [ ] CI can run different test suites independently
  - **Implementation**: Add build tags and reorganize test structure
  - **Rollback**: Maintain current test organization

- [ ] Task 4.3: Add comprehensive transport integration tests
  - **Risk**: LOW - Additional test coverage
  - **Files**: `internal/mcp/transport_integration_test.go` (new)
  - **Success Criteria**: 
    - [ ] Each transport tested with real MCP server
    - [ ] Error scenarios covered
    - [ ] Performance and stability testing
  - **Implementation**: Create comprehensive transport test suite
  - **Rollback**: Remove new test files

### Phase 5: Security and Performance Hardening (Risk: MEDIUM)
**Files**: Security validation, performance monitoring
**Objective**: Ensure production readiness and security
**Validation**: Security scan passes, no race conditions detected

- [ ] Task 5.1: Implement comprehensive command injection prevention
  - **Risk**: HIGH - Security critical
  - **Files**: `internal/config/validation.go`, command handling
  - **Success Criteria**: 
    - [ ] All command injection vectors blocked
    - [ ] Security test suite passes
    - [ ] Penetration testing scenarios handled
  - **Implementation**: Robust command validation and sanitization
  - **Rollback**: Revert to basic validation if issues arise

- [ ] Task 5.2: Fix race conditions in concurrent operations
  - **Risk**: MEDIUM - Stability under load
  - **Files**: `internal/mcp/service.go`, logger implementations
  - **Success Criteria**: 
    - [ ] Race detector tests pass
    - [ ] Concurrent connection tests stable
    - [ ] No goroutine leaks under stress
  - **Implementation**: Fix mutex usage and goroutine management
  - **Rollback**: Revert concurrency changes if stability decreases

- [ ] Task 5.3: Performance optimization and monitoring
  - **Risk**: LOW - Performance improvements
  - **Files**: Transport implementations, connection pooling
  - **Success Criteria**: 
    - [ ] Connection establishment times optimized
    - [ ] Memory usage stable under load
    - [ ] Performance regression tests pass
  - **Implementation**: Optimize transport implementations
  - **Rollback**: Revert optimizations if they cause issues

## Gotchas & Considerations
- **Known Issues**: 
  - Official SDK may not have HTTP transport - might need custom implementation
  - Command validation must be very robust to prevent security issues
  - Many existing tests assume transports work - will need extensive updates
- **Edge Cases**: 
  - STDIO commands with unusual arguments or paths
  - HTTP servers with non-standard endpoints or authentication
  - Concurrent transport operations and error handling
- **Performance**: 
  - STDIO process startup overhead for each connection
  - HTTP connection pooling and keep-alive considerations
- **Backwards Compatibility**: 
  - Existing CLI usage patterns must continue working
  - Configuration file formats should remain compatible
- **Security**: 
  - Command injection is critical vulnerability - validate everything
  - Process cleanup to prevent resource leaks
  - Error messages must not leak sensitive information

## Definition of Done
- [ ] All critical QA issues resolved: STDIO and HTTP transports working
- [ ] Tests pass: `go test ./... -v` shows >80% pass rate
- [ ] Security validated: Command injection prevention working
- [ ] Documentation accurate: All examples work as documented
- [ ] Integration verified: Real MCP servers work with all transports
- [ ] Performance acceptable: No regressions in connection times
- [ ] User experience: New users can follow examples successfully

## Validation Commands
```bash
# Critical functionality test
./mcp-tui "npx -y @modelcontextprotocol/server-everything stdio" tool list

# HTTP transport test (after implementation)
./mcp-tui --transport http --url http://localhost:8080/mcp tool list

# Security validation test
./mcp-tui --cmd "ls;rm" --args "test" tool list  # Should fail safely

# Complete test suite
go test ./... -v

# Integration verification
go test -tags=integration ./... -v

# Security scanning
go test -race ./... -v
```

## Priority Execution Order
🚨 **START WITH PHASE 1**: Critical STDIO implementation (Tasks 1.1-1.3)
📋 **IMMEDIATE FOLLOW-UP**: Documentation fixes (Task 3.1) to remove misleading info
🔧 **PARALLEL WORK**: HTTP transport research (Task 2.1) while STDIO is being implemented
📊 **VALIDATION**: Test each phase thoroughly before proceeding to next

**This plan addresses all critical QA findings and restores core functionality while maintaining security and stability.**