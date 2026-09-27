//go:build unix

package clipboard

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// The helper order is atotto/clipboard's, which mcp-tui used before.
func TestChooseHelper(t *testing.T) {
	for _, tc := range []struct {
		name      string
		goos      string
		wayland   bool
		installed []string
		wantCopy  string
	}{
		{"macOS", "darwin", false, []string{"pbcopy", "pbpaste", "xclip"}, "pbcopy"},
		{"wayland", "linux", true, []string{"wl-copy", "wl-paste", "xclip"}, "wl-copy"},
		{"wayland without wl-paste", "linux", true, []string{"wl-copy", "xclip"}, "xclip"},
		{"X without wayland", "linux", false, []string{"wl-copy", "wl-paste", "xclip", "xsel"}, "xclip"},
		{"xsel", "linux", false, []string{"xsel"}, "xsel"},
		{"termux", "linux", false, []string{"termux-clipboard-set", "termux-clipboard-get"}, "termux-clipboard-set"},
		{"WSL without X", "linux", false, []string{"clip.exe", "powershell.exe"}, "clip.exe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installed := func(name string) bool { return slices.Contains(tc.installed, name) }
			h, err := chooseHelper(tc.goos, tc.wayland, installed)
			if err != nil {
				t.Fatal(err)
			}
			if h.copy[0] != tc.wantCopy {
				t.Errorf("copy helper = %v, want %s", h.copy, tc.wantCopy)
			}
		})
	}

	if _, err := chooseHelper("linux", false, func(string) bool { return false }); err == nil {
		t.Error("no helper installed: want an error")
	}
}

// A helper that never exits is killed at the deadline instead of leaking.
func TestRunHelper_KillsAHungHelper(t *testing.T) {
	command, args := testutil.ServerSleeps(t, 60)
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := runHelper(ctx, append([]string{command}, args...), "text", false)

	if err == nil {
		t.Fatal("hung helper reported success")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("runHelper returned after %s, want soon after the 300ms deadline", elapsed)
	}
}
