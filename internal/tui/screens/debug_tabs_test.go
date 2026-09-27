package screens

import "testing"

// The debug screen's selected tab had the main screen's old bright white
// on blue, as unreadable in Catppuccin Mocha; it takes the same reverse
// video.
func TestDebugScreen_SelectedTabIsReverseVideo(t *testing.T) {
	ds := NewDebugScreen()
	assertSelectedTabReverseVideo(t, ds.renderTabs, "General (")
}
