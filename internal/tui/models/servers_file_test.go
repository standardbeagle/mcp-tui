package models

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/config"
)

// tempUserConfig points the user's home at a temp directory (so neither the
// developer's own config nor a Claude Desktop config leaks in) and returns
// ~/.config/mcp-tui inside it.
func tempUserConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	// Project-local configs are read from the working directory.
	t.Chdir(t.TempDir())
	dir := filepath.Join(home, ".config", "mcp-tui")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func loadManager(t *testing.T) *ConnectionsManager {
	t.Helper()
	cm := NewConnectionsManager()
	if err := cm.LoadConnections(); err != nil {
		t.Fatalf("LoadConnections: %v", err)
	}
	return cm
}

// The example the configuration guide published at first: an array of
// servers keyed by name, without a version, in servers.json.
const arrayServersJSON = `{
  "servers": [
    {
      "name": "everything",
      "transport": "stdio",
      "command": "npx",
      "args": ["@modelcontextprotocol/server-everything", "stdio"],
      "description": "Reference test server"
    },
    {
      "name": "remote",
      "transport": "http",
      "url": "https://my-mcp.example.com/mcp",
      "headers": { "Authorization": "Bearer ${MCP_TUI_TEST_TOKEN}" }
    }
  ]
}`

// arrayServersList is arrayServersJSON from its "servers" array to the end.
var arrayServersList = arrayServersJSON[strings.IndexByte(arrayServersJSON, '['):]

const singleRemoteJSON = `{"servers": {"remote": {
  "transport": "http",
  "url": "https://my-mcp.example.com/mcp",
  "headers": {"Authorization": "Bearer ${MCP_TUI_TEST_TOKEN}"}
}}}`

func TestServersJSONArrayFormLoads(t *testing.T) {
	dir := tempUserConfig(t)
	writeFile(t, filepath.Join(dir, ServersFileName), arrayServersJSON)

	cm := loadManager(t)
	if got := cm.ServerIDs(); strings.Join(got, ",") != "everything,remote" {
		t.Fatalf("servers = %v, want everything, remote", got)
	}
	remote := cm.GetConnections()["remote"]
	if remote.Transport != config.TransportHTTP || remote.URL != "https://my-mcp.example.com/mcp" {
		t.Errorf("remote = %s %q", remote.Transport, remote.URL)
	}
	if remote.Name != "remote" {
		t.Errorf("remote display name = %q, want its own name, remote", remote.Name)
	}
	if cm.ShouldAutoConnect() {
		t.Error("two servers and no defaultServer must show the picker, not auto-connect")
	}
}

func TestServersJSONSingleServerAutoConnectsWithExpandedHeaders(t *testing.T) {
	dir := tempUserConfig(t)
	writeFile(t, filepath.Join(dir, ServersFileName), singleRemoteJSON)
	t.Setenv("MCP_TUI_TEST_TOKEN", "s3cret")

	cm := loadManager(t)
	if !cm.ShouldAutoConnect() {
		t.Fatal("a single configured server must auto-connect")
	}
	entry := cm.GetAutoConnectEntry()
	if entry == nil || entry.ID != "remote" {
		t.Fatalf("auto-connect entry = %+v, want remote", entry)
	}
	if entry.Transport != config.TransportHTTP {
		t.Errorf("transport = %s, want http", entry.Transport)
	}
	conn, err := entry.ResolveConnectionConfig()
	if err != nil {
		t.Fatalf("ResolveConnectionConfig: %v", err)
	}
	if got := conn.Headers["Authorization"]; got != "Bearer s3cret" {
		t.Errorf("Authorization = %q, want Bearer s3cret", got)
	}
	if entry.Headers["Authorization"] != "Bearer ${MCP_TUI_TEST_TOKEN}" {
		t.Error("resolving must not write the secret back into the entry")
	}
}

func TestResolveConnectionConfigRefusesUnsetVariable(t *testing.T) {
	entry := &ConnectionEntry{
		ID: "remote", Transport: config.TransportHTTP, URL: "https://x/mcp",
		Headers: map[string]string{"Authorization": "Bearer ${MCP_TUI_UNSET_TOKEN}"},
	}
	_, err := entry.ResolveConnectionConfig()
	if err == nil || !strings.Contains(err.Error(), `server "remote": header Authorization: ${MCP_TUI_UNSET_TOKEN} is not set`) {
		t.Fatalf("err = %v", err)
	}
}

func TestServersJSONDefaultServerAutoConnects(t *testing.T) {
	dir := tempUserConfig(t)
	writeFile(t, filepath.Join(dir, ServersFileName),
		`{"defaultServer": "remote", "servers": `+arrayServersList)

	cm := loadManager(t)
	if entry := cm.GetAutoConnectEntry(); !cm.ShouldAutoConnect() || entry == nil || entry.ID != "remote" {
		t.Fatalf("defaultServer remote not auto-connected: %+v", entry)
	}
}

func TestServersJSONOverridesSavedEntryAndKeepsHistory(t *testing.T) {
	dir := tempUserConfig(t)
	writeFile(t, filepath.Join(dir, ConnectionsFileName), `{"version": "1.0", "servers": {
	  "remote": {"id": "remote", "name": "Remote", "transport": "http", "url": "https://old/mcp",
	             "success": true, "lastSeenVersion": "2025-11-25"},
	  "manual": {"id": "manual", "name": "Manual", "transport": "stdio", "command": "node"}
	}}`)
	writeFile(t, filepath.Join(dir, ServersFileName), singleRemoteJSON)

	cm := loadManager(t)
	remote := cm.GetConnections()["remote"]
	if remote.URL != "https://my-mcp.example.com/mcp" {
		t.Errorf("url = %q: servers.json must win over the saved copy", remote.URL)
	}
	if !remote.Success || remote.LastSeenVersion != "2025-11-25" {
		t.Errorf("saved history lost: success=%v version=%q", remote.Success, remote.LastSeenVersion)
	}
	if cm.GetConnections()["manual"] == nil {
		t.Error("a server saved from the connection screen must still be listed")
	}
}

// Taking a server, or the defaultServer, out of servers.json takes it out of
// effect, although connections.json was saved while it was there.
func TestServersJSONRemovalIsNotUndoneBySavedCopy(t *testing.T) {
	dir := tempUserConfig(t)
	serversPath := filepath.Join(dir, ServersFileName)
	writeFile(t, serversPath,
		`{"defaultServer": "remote", "servers": `+arrayServersList)

	cm := loadManager(t)
	cm.UpdateLastUsedWithVersion("remote", true, "2026-07-28") // saves connections.json

	saved, err := os.ReadFile(filepath.Join(dir, ConnectionsFileName))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk ConnectionsConfig
	if err := json.Unmarshal(saved, &onDisk); err != nil {
		t.Fatal(err)
	}
	if onDisk.DefaultServer != "" {
		t.Errorf("servers.json's defaultServer was saved into connections.json: %q", onDisk.DefaultServer)
	}

	writeFile(t, serversPath, `{"servers": [{"name": "everything", "command": "npx"}]}`)
	cm = loadManager(t)
	if got := cm.ServerIDs(); strings.Join(got, ",") != "everything" {
		t.Fatalf("servers = %v, want only everything", got)
	}
	if entry := cm.GetAutoConnectEntry(); entry == nil || entry.ID != "everything" {
		t.Fatalf("auto-connect entry = %+v, want the one remaining server", entry)
	}
}

func TestMalformedServersJSONIsReported(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
	}{
		{"not json", `{"servers": [`, "not valid JSON"},
		{"servers is a string", `{"servers": "x"}`, `"servers" must be an object`},
		{"no name", `{"servers": [{"command": "npx"}]}`, `servers[0]: needs an "id" or a "name"`},
		{"duplicate", `{"servers": [{"name": "a", "command": "x"}, {"name": "a", "command": "y"}]}`, `a second server named "a"`},
		{"unknown transport", `{"servers": {"a": {"transport": "carrier-pigeon", "url": "x"}}}`, `unknown transport "carrier-pigeon"`},
		{"http without url", `{"servers": {"a": {"transport": "http"}}}`, `needs a "url"`},
		{"stdio without command", `{"servers": {"a": {}}}`, `needs a "command"`},
		{"bad default", `{"defaultServer": "b", "servers": {"a": {"command": "x"}}}`, `defaultServer "b" is not one of the servers`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := tempUserConfig(t)
			writeFile(t, filepath.Join(dir, ServersFileName), tc.content)
			err := NewConnectionsManager().LoadConnections()
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), ServersFileName) {
				t.Fatalf("err = %v, want one naming servers.json and %q", err, tc.want)
			}
		})
	}
}

func TestNativeTransportSpellings(t *testing.T) {
	cfg, err := ParseNativeConfig([]byte(`{"servers": {
	  "a": {"url": "https://a/mcp"},
	  "b": {"type": "sse", "url": "https://b/sse"},
	  "c": {"transport": "streamable-http", "url": "https://c/mcp"},
	  "d": {"command": "node"}
	}}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]config.TransportType{
		"a": config.TransportHTTP, "b": config.TransportSSE,
		"c": config.TransportStreamableHTTP, "d": config.TransportStdio,
	}
	for id, transport := range want {
		if got := cfg.Servers[id].Transport; got != transport {
			t.Errorf("%s transport = %s, want %s", id, got, transport)
		}
	}
}

func TestFindServer(t *testing.T) {
	dir := tempUserConfig(t)
	writeFile(t, filepath.Join(dir, ServersFileName), arrayServersJSON)
	cm := loadManager(t)

	for _, name := range []string{"remote", "REMOTE", "Remote"} {
		if entry, err := cm.FindServer(name); err != nil || entry.ID != "remote" {
			t.Errorf("FindServer(%q) = %+v, %v", name, entry, err)
		}
	}
	if _, err := cm.FindServer("missing"); err == nil || !strings.Contains(err.Error(), "configured servers: everything, remote") {
		t.Errorf("FindServer(missing) err = %v", err)
	}
}

func TestDiscoveryReportsServersJSONAsNative(t *testing.T) {
	dir := tempUserConfig(t)
	writeFile(t, filepath.Join(dir, ServersFileName), singleRemoteJSON)

	cm := NewConnectionsManager()
	var found *DiscoveredConfigFile
	for _, f := range cm.DiscoverConfigFiles() {
		if f.Name == ServersFileName {
			found = f
		}
	}
	if found == nil {
		t.Fatal("servers.json not discovered")
	}
	if found.Format != FormatMCPTUI || found.ServerCount != 1 || found.Servers[0].Transport != "http" {
		t.Errorf("discovered = %+v", found)
	}
}
