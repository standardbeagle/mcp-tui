package screens_test

import (
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/tui/screens"
)

// fakeRootsService is a minimal in-memory implementation of
// screens.RootsEditorService used to drive the overlay in tests without
// pulling in the real mcp.Service.
type fakeRootsService struct {
	mu     sync.Mutex
	roots  []*officialMCP.Root
	addLog [][]*officialMCP.Root
	rmLog  [][]string
}

func (f *fakeRootsService) ListRoots() []*officialMCP.Root {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*officialMCP.Root, len(f.roots))
	copy(out, f.roots)
	return out
}

func (f *fakeRootsService) AddRoots(rs ...*officialMCP.Root) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]*officialMCP.Root(nil), rs...)
	f.addLog = append(f.addLog, cp)
	for _, r := range rs {
		// Replace same-URI entry, mirroring the real SDK semantics.
		replaced := false
		for i, existing := range f.roots {
			if existing != nil && existing.URI == r.URI {
				f.roots[i] = r
				replaced = true
				break
			}
		}
		if !replaced {
			f.roots = append(f.roots, r)
		}
	}
}

func (f *fakeRootsService) RemoveRoots(uris ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := append([]string(nil), uris...)
	f.rmLog = append(f.rmLog, cp)
	uriSet := map[string]struct{}{}
	for _, u := range uris {
		uriSet[u] = struct{}{}
	}
	out := f.roots[:0]
	for _, r := range f.roots {
		if r == nil {
			continue
		}
		if _, drop := uriSet[r.URI]; drop {
			continue
		}
		out = append(out, r)
	}
	f.roots = out
}

// keyMsg is a small helper that constructs a tea.KeyMsg from a single rune
// (or special key string). bubbletea's tea.KeyMsg is just []rune for runes
// and a Type for special keys, so we lean on the existing string-based key
// matching by encoding everything as runes when possible.
func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// TestRootsScreen_AddRootViaForm walks the full add flow: open the editor,
// press 'a' to begin a new entry, type a name and a path, save, and verify
// the service saw one AddRoots call with the correct Root.
func TestRootsScreen_AddRootViaForm(t *testing.T) {
	svc := &fakeRootsService{}
	screen := screens.NewRootsScreen(svc)

	// Begin add.
	model, _ := screen.Update(keyMsg("a"))
	rs, ok := model.(*screens.RootsScreen)
	if !ok {
		t.Fatalf("Update returned %T, want *RootsScreen", model)
	}

	// Type "home" into the name field, one rune at a time.
	for _, r := range "home" {
		next, _ := rs.Update(keyMsg(string(r)))
		rs = next.(*screens.RootsScreen)
	}

	// Tab to the path field.
	next, _ := rs.Update(keyMsg("tab"))
	rs = next.(*screens.RootsScreen)

	// Type the path.
	for _, r := range "/tmp/x" {
		next, _ := rs.Update(keyMsg(string(r)))
		rs = next.(*screens.RootsScreen)
	}

	// Save.
	next, _ = rs.Update(keyMsg("ctrl+s"))
	rs = next.(*screens.RootsScreen)

	if len(svc.addLog) != 1 {
		t.Fatalf("addLog has %d entries, want 1", len(svc.addLog))
	}
	if len(svc.addLog[0]) != 1 {
		t.Fatalf("first AddRoots call has %d roots, want 1", len(svc.addLog[0]))
	}
	got := svc.addLog[0][0]
	if got.Name != "home" {
		t.Errorf("Name = %q, want %q", got.Name, "home")
	}
	if got.URI != "file:///tmp/x" {
		t.Errorf("URI = %q, want %q", got.URI, "file:///tmp/x")
	}
}

// TestRootsScreen_DeleteRoot opens the editor with a pre-populated root,
// presses 'd' on the only entry, and verifies RemoveRoots was called with
// that URI.
func TestRootsScreen_DeleteRoot(t *testing.T) {
	svc := &fakeRootsService{
		roots: []*officialMCP.Root{
			{Name: "home", URI: "file:///tmp/home"},
		},
	}
	screen := screens.NewRootsScreen(svc)

	// Cursor starts at 0; press 'd' to delete.
	model, _ := screen.Update(keyMsg("d"))
	if _, ok := model.(*screens.RootsScreen); !ok {
		t.Fatalf("Update returned %T, want *RootsScreen", model)
	}

	if len(svc.rmLog) != 1 {
		t.Fatalf("rmLog has %d entries, want 1", len(svc.rmLog))
	}
	if len(svc.rmLog[0]) != 1 || svc.rmLog[0][0] != "file:///tmp/home" {
		t.Errorf("RemoveRoots called with %v, want [file:///tmp/home]", svc.rmLog[0])
	}
}

// TestRootsScreen_EscClosesOverlay verifies that Esc in list mode emits a
// BackMsg, which the screen manager uses to close the overlay.
func TestRootsScreen_EscClosesOverlay(t *testing.T) {
	svc := &fakeRootsService{}
	screen := screens.NewRootsScreen(svc)

	_, cmd := screen.Update(keyMsg("esc"))
	if cmd == nil {
		t.Fatalf("Esc returned nil cmd, want a BackMsg-emitting cmd")
	}
	msg := cmd()
	if _, ok := msg.(screens.BackMsg); !ok {
		t.Errorf("cmd produced %T, want BackMsg", msg)
	}
}

// TestRootsScreen_EditPreservesOldURIWhenUnchanged verifies that if the user
// edits a root and saves without changing the URI, the screen does NOT call
// RemoveRoots (which would otherwise produce a spurious list_changed
// notification for unchanged entries).
func TestRootsScreen_EditPreservesOldURIWhenUnchanged(t *testing.T) {
	svc := &fakeRootsService{
		roots: []*officialMCP.Root{
			{Name: "home", URI: "file:///tmp/home"},
		},
	}
	screen := screens.NewRootsScreen(svc)

	// Press 'e' to edit the only entry.
	model, _ := screen.Update(keyMsg("e"))
	rs := model.(*screens.RootsScreen)

	// Press Ctrl+S immediately to save without changes.
	next, _ := rs.Update(keyMsg("ctrl+s"))
	_ = next.(*screens.RootsScreen)

	if len(svc.rmLog) != 0 {
		t.Errorf("RemoveRoots called %d times for unchanged edit, want 0", len(svc.rmLog))
	}
	// AddRoots is still called (the SDK contract is "replace same-URI"),
	// so the saved label propagates even when the URI is unchanged.
	if len(svc.addLog) != 1 {
		t.Errorf("AddRoots called %d times, want 1", len(svc.addLog))
	}
}

// TestRootsScreen_ViewMentionsKeyHelp confirms that the list-mode help line
// includes the basic key shortcuts so users have a discoverable surface.
// This is a smoke test for the View() output.
func TestRootsScreen_ViewMentionsKeyHelp(t *testing.T) {
	svc := &fakeRootsService{}
	screen := screens.NewRootsScreen(svc)
	// Set a window size so wrapInBorder produces a useful render.
	model, _ := screen.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	rs := model.(*screens.RootsScreen)

	out := rs.View()
	for _, want := range []string{"add", "edit", "delete"} {
		if !strings.Contains(strings.ToLower(out), want) {
			t.Errorf("View() missing %q in help line:\n%s", want, out)
		}
	}
}
