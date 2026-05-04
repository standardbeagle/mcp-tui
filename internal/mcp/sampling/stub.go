package sampling

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// defaultStubModel is the model name reported when a stub reply is generated
// without an explicit model field. Servers expect a non-empty string.
const defaultStubModel = "mcp-tui-stub"

// NewTextStubHandler returns a Handler that always replies with the given
// plain-text content. It is used by the CLI flag `--sampling-stub`.
//
// The reply role is always "assistant" and stopReason is "endTurn", which is
// the natural choice for a static canned response.
func NewTextStubHandler(text string) Handler {
	return HandlerFunc(func(_ context.Context, _ *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
		return &officialMCP.CreateMessageResult{
			Content: &officialMCP.TextContent{
				Text: text,
			},
			Model:      defaultStubModel,
			Role:       officialMCP.Role("assistant"),
			StopReason: "endTurn",
		}, nil
	})
}

// stubFileSchema is the JSON shape accepted by NewFileStubHandler. All fields
// are optional except Text. Callers can override Model/Role/StopReason and
// choose between text and image content.
type stubFileSchema struct {
	Text       string `json:"text,omitempty"`
	Model      string `json:"model,omitempty"`
	Role       string `json:"role,omitempty"`
	StopReason string `json:"stopReason,omitempty"`

	// Image, if non-empty, switches the reply to image content. The string is
	// base64-encoded image data.
	Image    string `json:"imageData,omitempty"`
	MIMEType string `json:"mimeType,omitempty"`
}

// NewFileStubHandler reads a JSON reply template from the given path and
// returns a Handler that always replies with that content. It is used by the
// CLI flag `--sampling-stub-file`.
//
// The file is read once at construction time; subsequent edits do not affect
// running clients. This is intentional — CI runs read the file at start.
func NewFileStubHandler(path string) (Handler, error) {
	if path == "" {
		return nil, fmt.Errorf("sampling stub file path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sampling stub file %q: %w", path, err)
	}

	var spec stubFileSchema
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse sampling stub file %q: %w", path, err)
	}
	if spec.Text == "" && spec.Image == "" {
		return nil, fmt.Errorf("sampling stub file %q must set either \"text\" or \"imageData\"", path)
	}
	if spec.Text != "" && spec.Image != "" {
		return nil, fmt.Errorf("sampling stub file %q must not set both \"text\" and \"imageData\"", path)
	}

	model := spec.Model
	if model == "" {
		model = defaultStubModel
	}
	role := spec.Role
	if role == "" {
		role = "assistant"
	}
	stop := spec.StopReason
	if stop == "" {
		stop = "endTurn"
	}

	var content officialMCP.Content
	if spec.Image != "" {
		mime := spec.MIMEType
		if mime == "" {
			mime = "image/png"
		}
		content = &officialMCP.ImageContent{
			Data:     []byte(spec.Image),
			MIMEType: mime,
		}
	} else {
		content = &officialMCP.TextContent{Text: spec.Text}
	}

	return HandlerFunc(func(_ context.Context, _ *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
		return &officialMCP.CreateMessageResult{
			Content:    content,
			Model:      model,
			Role:       officialMCP.Role(role),
			StopReason: stop,
		}, nil
	}), nil
}
