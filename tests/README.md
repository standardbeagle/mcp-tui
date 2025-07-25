# MCP-TUI Enhanced Error Handling Test Suite

This directory contains comprehensive tests for the enhanced error handling implementation that addresses the MCP server startup error issues reported in the bug report.

## Test Coverage

### 1. Error Classification Tests (`internal/mcp/errors/classification_test.go`)

Tests the new `CategoryServerStartup` error classification and pattern detection:

- **Environment Variable Errors**: Detects missing required environment variables
- **Usage Errors**: Identifies when servers show usage information due to missing arguments
- **NPM Package Errors**: Recognizes npm 404 errors and package not found issues
- **Module Not Found**: Catches Node.js module dependency issues
- **Command Not Found**: Properly categorizes command execution failures
- **Recovery Actions**: Validates that appropriate recovery suggestions are provided

### 2. Error Handler Tests (`internal/mcp/errors/handler_test.go`)

Tests the enhanced error handler that integrates server startup error detection:

- **Server Startup Detection**: Ensures startup errors are properly classified during session connection
- **Statistics Tracking**: Verifies error statistics correctly count different error types
- **User-Friendly Messages**: Tests generation of helpful error messages with recovery actions
- **Retry Logic**: Confirms that server startup errors are not retried (since they require user intervention)
- **JSON Formatting**: Validates error information is properly structured for API responses

### 3. Enhanced STDIO Transport Tests (`internal/mcp/transports/stdio_enhanced_test.go`)

Tests the pre-flight server validation and enhanced transport functionality:

- **Server Startup Error Detection**: Tests `isServerStartupError()` with various error patterns
- **Server Ready Detection**: Tests `isServerReadyForMCP()` to identify successful server starts
- **Error Message Analysis**: Tests `looksLikeError()` for proper error identification
- **Suggestion Generation**: Comprehensive tests for `generateSuggestion()` with different error types
- **Error Classification**: Tests the `ServerStartupErrorClassifier` integration

### 4. Integration Tests (`internal/mcp/transports/integration_test.go`)

Tests the complete enhanced STDIO transport integration:

- **Command Validation**: Ensures security validation still works (prevents injection attacks)
- **Factory Integration**: Verifies the transport factory uses the enhanced transport
- **Pre-flight Validation**: Tests actual command execution and validation
- **Error Scenarios**: Tests real-world scenarios with failing commands
- **Working Commands**: Ensures successful commands still work

### 5. End-to-End Tests (`enhanced_error_handling_test.go`)

Comprehensive integration tests that verify the complete error handling flow:

- **Regression Prevention**: Specifically tests that the old "EOF" error pattern doesn't return
- **Real Server Simulation**: Uses Python scripts to simulate actual server failure scenarios
- **Working Server Compatibility**: Ensures working servers continue to function normally
- **Security Validation**: Confirms command injection protection still works
- **Performance Testing**: Benchmarks to ensure error handling doesn't impact performance

## Running the Tests

### Run All Error Handling Tests
```bash
# Run all tests with coverage
go test -cover ./internal/mcp/errors/
go test -cover ./internal/mcp/transports/
go test -cover . -run "TestEnhancedErrorHandling"

# Run with verbose output
go test -v ./internal/mcp/errors/
go test -v ./internal/mcp/transports/ -short
go test -v . -run "TestEnhancedErrorHandling"
```

### Run Integration Tests
```bash
# Run longer integration tests (not in short mode)
go test -v ./internal/mcp/transports/
go test -v . -run "TestEnhancedErrorHandling"
```

### Run Specific Test Categories
```bash
# Test error classification only
go test -v ./internal/mcp/errors/ -run "TestServerStartupError"

# Test suggestion generation
go test -v ./internal/mcp/transports/ -run "TestGenerateSuggestion"

# Test regression prevention
go test -v . -run "TestErrorHandlingRegressionPrevention"
```

## Test Scenarios Covered

### Server Startup Failure Scenarios

1. **Missing Environment Variables**
   - Input: `"Error: BRAVE_API_KEY environment variable is required"`
   - Expected: Specific variable name extraction and setup guidance

2. **Missing Command Arguments**
   - Input: `"Usage: mcp-server-filesystem <allowed-directory>"`
   - Expected: Argument configuration guidance

3. **Package/Dependency Not Found**
   - Input: `"npm error 404 Not Found"` or `"Cannot find module"`
   - Expected: Package installation guidance

4. **Command Not Found**
   - Input: `"executable file not found in $PATH"`
   - Expected: Installation/PATH configuration guidance

### Success Scenarios

1. **Server Ready Messages**
   - Input: `"MCP server running on stdio"`
   - Expected: Pre-flight validation passes, normal MCP connection proceeds

2. **Working Commands**
   - Input: Commands that start successfully
   - Expected: No interference with normal operation

### Security Scenarios

1. **Command Injection Prevention**
   - Input: Commands with dangerous characters (`;`, `|`, `` ` ``, `$`)
   - Expected: Security validation blocks execution

## Expected Test Results

All tests should pass and demonstrate:

✅ **No more generic "EOF" errors**  
✅ **Specific, actionable error messages**  
✅ **Proper error categorization**  
✅ **Helpful recovery suggestions**  
✅ **Working server compatibility**  
✅ **Security validation maintained**  
✅ **Performance impact minimized**

## Debugging Failed Tests

If tests fail:

1. **Check Error Messages**: Look for changes in error classification logic
2. **Verify Test Expectations**: Ensure test expectations match actual behavior
3. **Run Individual Tests**: Isolate failing components
4. **Check Logs**: Use `-v` flag to see detailed test output
5. **Update Test Data**: If behavior changes are intentional, update test expectations

## Contributing

When adding new error patterns or scenarios:

1. Add test cases to the appropriate test files
2. Ensure both positive and negative test cases are covered
3. Update this README with new test scenarios
4. Verify all existing tests still pass
5. Add regression tests for any bugs discovered

This comprehensive test suite ensures that the enhanced error handling continues to work correctly and prevents regressions that would bring back the original "EOF" error problem.