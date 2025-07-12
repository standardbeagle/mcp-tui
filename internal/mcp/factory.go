package mcp

// NewService creates a new MCP service using the official modelcontextprotocol/go-sdk
func NewService() Service {
	return &service{
		info:      &ServerInfo{},
		requestID: 0,
		debugMode: false,
	}
}