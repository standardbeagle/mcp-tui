package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/tui/screens"
)

// withServersJSON gives the run a home whose ~/.config/mcp-tui/servers.json
// holds content, and an empty working directory.
func withServersJSON(t *testing.T, content string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	t.Chdir(t.TempDir())
	dir := filepath.Join(home, ".config", "mcp-tui")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "servers.json"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const oneRemoteServer = `{"servers": [{"name": "intranet", "url": "http://127.0.0.1:1/mcp",
  "headers": {"Authorization": "Bearer ${MCP_TUI_TEST_TOKEN}"}}]}`

func TestScreenManagerAutoConnectsToTheOnlyServer(t *testing.T) {
	withServersJSON(t, oneRemoteServer)
	t.Setenv("MCP_TUI_TEST_TOKEN", "s3cret")

	sm := NewScreenManager(config.Default(), nil)
	if _, ok := sm.currentScreen.(*screens.MainScreen); !ok {
		t.Fatalf("start screen = %T, want *screens.MainScreen (auto-connect)", sm.currentScreen)
	}
}

func TestScreenManagerShowsPickerForSeveralServers(t *testing.T) {
	withServersJSON(t, `{"servers": [{"name": "a", "command": "node"}, {"name": "b", "command": "node"}]}`)

	sm := NewScreenManager(config.Default(), nil)
	if _, ok := sm.currentScreen.(*screens.ConnectionScreen); !ok {
		t.Fatalf("start screen = %T, want *screens.ConnectionScreen", sm.currentScreen)
	}
}

// An unset ${VAR} stops the auto-connect and is shown, rather than sending
// the request without its credentials.
func TestScreenManagerReportsUnresolvableAutoConnect(t *testing.T) {
	withServersJSON(t, oneRemoteServer)
	t.Setenv("MCP_TUI_TEST_TOKEN", "")

	sm := NewScreenManager(config.Default(), nil)
	cs, ok := sm.currentScreen.(*screens.ConnectionScreen)
	if !ok {
		t.Fatalf("start screen = %T, want *screens.ConnectionScreen", sm.currentScreen)
	}
	if view := cs.View(); !containsAll(view, "MCP_TUI_TEST_TOKEN", "not set") {
		t.Errorf("connection screen does not say why it did not connect:\n%s", view)
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
