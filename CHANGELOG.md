# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- **BREAKING**: Migrated from mark3labs/mcp-go to official modelcontextprotocol/go-sdk
- **BREAKING**: Only SSE transport is currently supported (STDIO and HTTP coming soon)
- **BREAKING**: Service interface now uses type-safe `*config.ConnectionConfig` parameter instead of `interface{}`

### Added
- Comprehensive error handling for JSON schema operations
- Improved logging for schema conversion failures
- Type safety enforcement for connection configuration
- Missing `isJSONError` function for test compatibility

### Fixed
- Critical compilation error in test suite
- Unsafe interface{} parameter replaced with type-safe alternative
- JSON schema conversion now gracefully handles errors
- Prompt argument validation to prevent empty names

### Removed
- Legacy mark3labs/mcp-go dependency
- mark3labs-specific transport implementations
- Unsafe type assertions in connection handling

## Migration Guide

### From mark3labs SDK to Official SDK

#### Transport Support
- **Currently Supported**: SSE (Server-Sent Events)
  ```bash
  # SSE connection example
  mcp-tui --url http://localhost:8000/sse
  ```

- **Not Yet Implemented**: STDIO, HTTP, Streamable HTTP
  - These transports will be added in future releases
  - Use SSE transport as alternative for testing

#### API Changes
- `Connect()` method now requires `*config.ConnectionConfig` instead of `interface{}`
- This change improves type safety but shouldn't affect existing usage
- All CLI and TUI functionality preserved

#### Error Handling
- JSON schema errors are now logged but don't crash the application
- Invalid schemas continue processing with nil values
- More detailed error messages for debugging

#### Testing
- Official SDK provides better MCP protocol compliance
- Test coverage maintained for core functionality
- Some error message assertions may need updating

### Troubleshooting

#### Connection Issues
- Ensure your MCP server supports SSE transport
- For Playwright server, use SSE endpoint: `http://localhost:PORT/sse`
- Check server logs for detailed error information

#### Schema Errors
- Schema conversion errors are logged but processing continues
- Check debug output for detailed schema error information
- Report persistent schema issues for investigation