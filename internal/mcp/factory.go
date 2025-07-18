package mcp

// NewService creates a new MCP service using the official modelcontextprotocol/go-sdk
func NewService() Service {
	s := &service{
		info:      &ServerInfo{},
		requestID: 0,
		debugMode: true, // Always enable debug mode - this is a testing tool
	}
	// Enable HTTP debugging immediately
	s.SetDebugMode(true)
	return s
}