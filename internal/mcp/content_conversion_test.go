package mcp

import (
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestConvertContentPreservesPromptContentTypes(t *testing.T) {
	t.Run("text", func(t *testing.T) {
		got := convertContent(&officialMCP.TextContent{Text: "hello"})
		require.Equal(t, Content{Type: "text", Text: "hello"}, got)
	})

	t.Run("image", func(t *testing.T) {
		got := convertContent(&officialMCP.ImageContent{Data: []byte("aGVsbG8="), MIMEType: "image/png"})
		require.Equal(t, Content{Type: "image", Data: "aGVsbG8=", MimeType: "image/png"}, got)
	})

	t.Run("embedded resource", func(t *testing.T) {
		got := convertContent(&officialMCP.EmbeddedResource{
			Resource: &officialMCP.ResourceContents{URI: "file:///tmp/example.txt"},
		})
		require.Equal(t, &ResourceReference{Type: "embedded", URI: "file:///tmp/example.txt"}, got.Resource)
		require.Equal(t, "resource", got.Type)
	})
}
