package conform

import (
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

// conform's --tool-args reach the seterror-content probe as well as
// tools.call.isError, with the same conversion.
func TestVerifyTarget_CarriesToolArguments(t *testing.T) {
	convert := func(*mcp.Tool, []string) (map[string]any, error) { return nil, nil }
	r := NewRunner(&Target{Command: "billing-server", ToolName: "void_invoice",
		ToolArgs: []string{"invoice_id=INV-2201"}, ToolArguments: convert})

	vt := r.verifyTarget()
	if vt.ToolName != "void_invoice" || vt.Command != "billing-server" {
		t.Errorf("target = %+v", vt)
	}
	if len(vt.ToolArgPairs) != 1 || vt.ToolArgPairs[0] != "invoice_id=INV-2201" {
		t.Errorf("ToolArgPairs = %q", vt.ToolArgPairs)
	}
	if vt.ToolArguments == nil {
		t.Error("ToolArguments conversion not passed to the probe")
	}
}
