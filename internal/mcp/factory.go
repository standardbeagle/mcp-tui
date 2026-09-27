package mcp

// ClientVersion is the version mcp-tui reports as clientInfo.version in the
// handshake. main sets it from its -ldflags version before any Service
// connects; "dev" marks a build that was never given one.
var ClientVersion = "dev"

// NewService creates a new MCP service using the official modelcontextprotocol/go-sdk
func NewService() Service {
	s := &service{
		info:      &ServerInfo{},
		requestID: 0,
		debugMode: true, // Always enable debug mode - this is a testing tool
	}
	return s
}
