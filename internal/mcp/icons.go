package mcp

import (
	"fmt"
	"strings"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// DescribeIcon renders an icon (SEP-973) for display: its src, then mime
// type, sizes and theme when given. A data: URI is shortened to its header
// and payload size; the image is never fetched or decoded.
func DescribeIcon(icon officialMCP.Icon) string {
	src := icon.Source
	if header, payload, ok := strings.Cut(src, ","); ok && strings.HasPrefix(src, "data:") {
		src = fmt.Sprintf("%s,… (%d bytes)", header, len(payload))
	}
	var details []string
	if icon.MIMEType != "" {
		details = append(details, icon.MIMEType)
	}
	if len(icon.Sizes) > 0 {
		details = append(details, "sizes "+strings.Join(icon.Sizes, " "))
	}
	if icon.Theme != "" {
		details = append(details, "theme "+string(icon.Theme))
	}
	if len(details) == 0 {
		return src
	}
	return src + " (" + strings.Join(details, ", ") + ")"
}
