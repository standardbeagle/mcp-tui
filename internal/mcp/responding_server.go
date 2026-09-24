package mcp

import (
	"encoding/json"
	"fmt"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/debug"
)

// RespondingServer identifies the server that produced a result, from the
// result's _meta io.modelcontextprotocol/serverInfo (SEP-2575, protocol
// 2026-07-28). Behind a gateway or load balancer it can differ from the
// server that answered server/discover.
type RespondingServer struct {
	Name    string `json:"name"`
	Title   string `json:"title,omitempty"`
	Version string `json:"version"`
}

// String renders "name version".
func (r RespondingServer) String() string {
	return fmt.Sprintf("%s %s", r.Name, r.Version)
}

// respondingServer reads the serverInfo _meta entry of a result; nil when it
// has none (every result before 2026-07-28). The entry is the server's
// Implementation, decoded by the SDK into a generic map.
func respondingServer(meta officialMCP.Meta) *RespondingServer {
	raw, ok := meta[officialMCP.MetaKeyServerInfo]
	if !ok {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		debug.Warn("Unreadable serverInfo in result _meta", debug.F("error", err))
		return nil
	}
	var server RespondingServer
	if err := json.Unmarshal(encoded, &server); err != nil || server.Name == "" {
		debug.Warn("Malformed serverInfo in result _meta", debug.F("value", string(encoded)))
		return nil
	}
	return &server
}
