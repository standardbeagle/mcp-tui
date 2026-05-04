package sampling

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

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

// ParseToolUseSpec parses the `--sampling-tool-use` flag value of the form
// "<tool_name>:<json_args>". Only the first colon is treated as the separator;
// subsequent colons inside the JSON object are preserved verbatim.
//
// Returns an error if the spec is missing the colon or has an empty tool name.
// JSON validity is checked by NewToolUseStubHandler, not here, so callers can
// reuse this helper for any "name:json" syntax they expose.
func ParseToolUseSpec(spec string) (name, argsJSON string, err error) {
	idx := -1
	for i, r := range spec {
		if r == ':' {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", "", fmt.Errorf("sampling tool-use spec %q must be of the form <tool_name>:<json_args>", spec)
	}
	name = spec[:idx]
	argsJSON = spec[idx+1:]
	if name == "" {
		return "", "", fmt.Errorf("sampling tool-use spec %q has empty tool name", spec)
	}
	return name, argsJSON, nil
}

// toolUseStubHandler is a Handler that always replies with a single
// ToolUseContent block. It implements both Handler (for the legacy single-
// content variant) and WithToolsHandler (for the array-content variant).
//
// It is used by the CLI flag `--sampling-tool-use`, which lets test scripts
// canned-reply with a tool-use block to drive an agentic round-trip without
// human interaction.
type toolUseStubHandler struct {
	name  string
	input map[string]any
	id    string
	model string
}

// NewToolUseStubHandler returns a handler that replies to every sampling
// request with a single tool_use block invoking toolName with argsJSON. The
// reply role is "assistant", stopReason is "toolUse", and a deterministic
// tool-use ID is generated from the tool name (sufficient for stub usage —
// tests can match on the prefix).
//
// argsJSON must be either empty (treated as `{}`) or a valid JSON object.
// Arrays, scalars, and malformed JSON are rejected because the MCP schema
// requires tool_use Input to be a JSON object.
func NewToolUseStubHandler(toolName, argsJSON string) (WithToolsHandler, error) {
	if toolName == "" {
		return nil, fmt.Errorf("sampling tool-use stub: tool name must not be empty")
	}

	input := map[string]any{}
	trimmed := strings.TrimSpace(argsJSON)
	if trimmed != "" {
		// Decoding into a generic any first lets us reject non-object JSON
		// (arrays, numbers) with a clearer error than json.Unmarshal would
		// produce when the destination type doesn't match.
		var raw any
		if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
			return nil, fmt.Errorf("sampling tool-use stub: parse args JSON: %w", err)
		}
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("sampling tool-use stub: args JSON must be an object, got %T", raw)
		}
		input = obj
	}

	return &toolUseStubHandler{
		name:  toolName,
		input: input,
		id:    "stub-" + toolName,
		model: defaultStubModel,
	}, nil
}

// HandleCreateMessage replies with the tool_use block as a single Content. The
// MCP SDK accepts ToolUseContent in CreateMessageResult only when the server
// uses the WithTools variant; basic CreateMessage does not include tools, so
// in practice this code path is rarely exercised by real servers. It exists
// to satisfy the Handler interface so the stub can flow through the same
// wiring code as text/file stubs.
func (h *toolUseStubHandler) HandleCreateMessage(_ context.Context, _ *officialMCP.CreateMessageRequest) (*officialMCP.CreateMessageResult, error) {
	return &officialMCP.CreateMessageResult{
		Content: &officialMCP.ToolUseContent{
			ID:    h.id,
			Name:  h.name,
			Input: h.input,
		},
		Model:      h.model,
		Role:       officialMCP.Role("assistant"),
		StopReason: "toolUse",
	}, nil
}

// HandleCreateMessageWithTools replies with the canned tool_use block. This
// is the path real servers will hit when they request sampling-with-tools.
func (h *toolUseStubHandler) HandleCreateMessageWithTools(_ context.Context, _ *officialMCP.CreateMessageWithToolsRequest) (*officialMCP.CreateMessageWithToolsResult, error) {
	return &officialMCP.CreateMessageWithToolsResult{
		Content: []officialMCP.Content{
			&officialMCP.ToolUseContent{
				ID:    h.id,
				Name:  h.name,
				Input: h.input,
			},
		},
		Model:      h.model,
		Role:       officialMCP.Role("assistant"),
		StopReason: "toolUse",
	}, nil
}
