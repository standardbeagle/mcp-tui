package shell

import (
	"slices"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// sh reads each quoted word back as the original string.
func TestQuote_ShReadsTheWordBack(t *testing.T) {
	words := []string{
		"plain", "", "$HOME", "${PATH}", "`id`", "$(whoami)", "it's", `say "hi"`,
		`back\slash`, "line1\nline2", "tab\there", "a b", "*", "~user", "#hash", "'", "''",
	}
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = Quote(w)
	}
	if got := testutil.ShWords(t, strings.Join(quoted, " ")); !slices.Equal(got, words) {
		t.Errorf("sh read\n%q\nwant\n%q", got, words)
	}
}
