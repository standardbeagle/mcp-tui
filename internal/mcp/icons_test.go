package mcp

import (
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDescribeIcon_ListsEveryField(t *testing.T) {
	got := DescribeIcon(officialMCP.Icon{
		Source: "https://cdn.example.com/icons/deploy.svg", MIMEType: "image/svg+xml",
		Sizes: []string{"any"}, Theme: "dark",
	})
	want := "https://cdn.example.com/icons/deploy.svg (image/svg+xml, sizes any, theme dark)"
	if got != want {
		t.Errorf("DescribeIcon = %q, want %q", got, want)
	}
	if got := DescribeIcon(officialMCP.Icon{Source: "https://cdn.example.com/a.png"}); got != "https://cdn.example.com/a.png" {
		t.Errorf("DescribeIcon without optional fields = %q", got)
	}
}

// A data: URI can be megabytes of base64; show its header and size only.
func TestDescribeIcon_SummarizesDataURIs(t *testing.T) {
	src := "data:image/png;base64," + strings.Repeat("iVBORw0KGgo", 400)
	got := DescribeIcon(officialMCP.Icon{Source: src, Sizes: []string{"48x48", "96x96"}})
	if strings.Contains(got, "iVBORw0KGgo") || !strings.Contains(got, "data:image/png;base64,") ||
		!strings.Contains(got, "4400 bytes") || !strings.Contains(got, "sizes 48x48 96x96") {
		t.Errorf("DescribeIcon(data URI) = %q", got)
	}
}
