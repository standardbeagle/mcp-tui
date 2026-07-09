package mcp

import (
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

func requireLocalListener(t *testing.T) {
	t.Helper()
	testutil.RequireLocalListener(t)
}
