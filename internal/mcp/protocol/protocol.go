// Package protocol holds MCP protocol-version facts that several mcp-tui
// packages branch on.
package protocol

// StatelessVersion is the first MCP protocol version without sessions
// (SEP-2575): no initialize handshake, no session ID, no ping, and
// list_changed only on a subscriptions/listen stream.
const StatelessVersion = "2026-07-28"

// IsStateless reports whether version is StatelessVersion or later.
// Protocol versions are ISO dates, so they order as strings.
func IsStateless(version string) bool {
	return version >= StatelessVersion
}
