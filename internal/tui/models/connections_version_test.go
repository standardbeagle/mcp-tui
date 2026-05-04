package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

// TestUpdateLastUsedWithVersion verifies the new method persists both the
// last-used timestamp and the negotiated MCP protocol version into the saved
// connection entry. The version is the load-bearing field for this Tier-2
// task: users open the saved-connections list and want to see which spec a
// given server agreed to last time without re-connecting.
func TestUpdateLastUsedWithVersion(t *testing.T) {
	cm := newTestConnectionsManager(t)
	cm.config.Servers["srv1"] = &ConnectionEntry{
		ID:        "srv1",
		Name:      "Server One",
		Transport: config.TransportStdio,
		Command:   "echo",
	}

	cm.UpdateLastUsedWithVersion("srv1", true, "2025-11-25")

	entry := cm.config.Servers["srv1"]
	if entry.LastSeenVersion != "2025-11-25" {
		t.Errorf("LastSeenVersion = %q; want %q", entry.LastSeenVersion, "2025-11-25")
	}
	if !entry.Success {
		t.Errorf("Success = false; want true after successful update")
	}
	if entry.LastUsed == nil {
		t.Errorf("LastUsed not set after successful update")
	}
}

// TestUpdateLastUsedWithVersion_EmptyVersionLeavesPriorIntact ensures we
// never erase a previously-recorded version. A failed reconnect (empty
// version, success=false) still updates LastUsed but must preserve the
// last successful negotiation so the UI can keep showing it.
func TestUpdateLastUsedWithVersion_EmptyVersionLeavesPriorIntact(t *testing.T) {
	cm := newTestConnectionsManager(t)
	cm.config.Servers["srv1"] = &ConnectionEntry{
		ID:              "srv1",
		Name:            "Server One",
		Transport:       config.TransportStdio,
		Command:         "echo",
		LastSeenVersion: "2024-11-05",
	}

	cm.UpdateLastUsedWithVersion("srv1", false, "")

	entry := cm.config.Servers["srv1"]
	if entry.LastSeenVersion != "2024-11-05" {
		t.Errorf("LastSeenVersion overwritten to %q; want preserved 2024-11-05", entry.LastSeenVersion)
	}
}

// TestConnectionEntry_LastSeenVersion_RoundTrip verifies the new field
// persists through the JSON marshal/unmarshal cycle used by SaveConnections /
// LoadConnections. Without round-trip stability the value would never be
// visible across runs of mcp-tui.
func TestConnectionEntry_LastSeenVersion_RoundTrip(t *testing.T) {
	original := &ConnectionEntry{
		ID:              "srv1",
		Name:            "Server One",
		Transport:       config.TransportStdio,
		Command:         "echo",
		LastSeenVersion: "2025-11-25",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var decoded ConnectionEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if decoded.LastSeenVersion != "2025-11-25" {
		t.Errorf("LastSeenVersion after round-trip = %q; want 2025-11-25", decoded.LastSeenVersion)
	}
}

// TestConnectionEntry_LastSeenVersion_OmitEmpty verifies the field is
// omitted from JSON when empty. A fresh ConnectionEntry should not pollute
// the saved-connections file with empty version strings — we only want the
// key present once a connection has actually negotiated a version.
func TestConnectionEntry_LastSeenVersion_OmitEmpty(t *testing.T) {
	entry := &ConnectionEntry{
		ID:        "srv1",
		Name:      "Server One",
		Transport: config.TransportStdio,
		Command:   "echo",
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if _, present := raw["lastSeenVersion"]; present {
		t.Errorf("empty LastSeenVersion serialized to JSON; want omitempty")
	}
}

// newTestConnectionsManager constructs a ConnectionsManager whose state file
// lives in a temp dir, isolating the test from the user's real config.
func newTestConnectionsManager(t *testing.T) *ConnectionsManager {
	t.Helper()
	cm := NewConnectionsManager()
	cm.filePath = filepath.Join(t.TempDir(), "connections.json")
	cm.config.Servers = make(map[string]*ConnectionEntry)
	// Defensive: ensure the dir exists for SaveConnections inside the call.
	if err := os.MkdirAll(filepath.Dir(cm.filePath), 0755); err != nil {
		t.Fatalf("setup MkdirAll: %v", err)
	}
	return cm
}
