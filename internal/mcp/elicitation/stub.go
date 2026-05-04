package elicitation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubReply is the on-disk / on-CLI shape of a canned elicitation reply.
//
// Two equivalent surface forms are accepted:
//
//  1. A bare JSON object — its keys are treated as the form Content map and
//     Action defaults to "accept". This is the common case for happy-path
//     stubs because the form values are exactly what the server expects.
//
//  2. A JSON object with at least one of the reserved keys "_action" or
//     "_content" set. The reserved key "_action" overrides the default
//     "accept" Action; "_content" overrides the entire content map. This
//     form is needed to test decline/cancel paths and to disambiguate
//     stubs whose form fields happen to collide with reserved names.
//
// The reserved-key prefix is "_" rather than "$" or a runtime-magic suffix
// because JSON Schema explicitly reserves "$"-prefixed names for keywords;
// servers that legitimately ask for a field literally named "_action" can
// route around this by using the reserved-key form with "_content" carrying
// the actual content map.
type stubReply struct {
	Action  string         `json:"_action,omitempty"`
	Content map[string]any `json:"_content,omitempty"`
}

// validActions are the Actions allowed by the MCP elicitation spec.
var validActions = map[string]struct{}{
	"accept":  {},
	"decline": {},
	"cancel":  {},
}

// parseStubJSON decodes a canned elicitation reply. The accepted shapes are
// described on stubReply. An empty body is rejected because a stub that
// resolves to "no Action and no Content" would be indistinguishable from a
// silent no-op handler — the caller almost certainly meant something
// specific.
func parseStubJSON(data []byte) (action string, content map[string]any, err error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return "", nil, fmt.Errorf("elicitation stub body is empty")
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(trimmed), &raw); err != nil {
		return "", nil, fmt.Errorf("parse elicitation stub JSON: %w", err)
	}

	// Detect the reserved-key form first so a stub using `_content` is not
	// also interpreted as a literal form value named `_content`.
	hasReserved := false
	if _, ok := raw["_action"]; ok {
		hasReserved = true
	}
	if _, ok := raw["_content"]; ok {
		hasReserved = true
	}

	if hasReserved {
		var spec stubReply
		if err := json.Unmarshal([]byte(trimmed), &spec); err != nil {
			return "", nil, fmt.Errorf("parse elicitation stub JSON (reserved-key form): %w", err)
		}
		action = spec.Action
		content = spec.Content
	} else {
		action = "accept"
		content = raw
	}

	if action == "" {
		action = "accept"
	}
	if _, ok := validActions[action]; !ok {
		return "", nil, fmt.Errorf("elicitation stub: invalid _action %q (must be accept, decline, or cancel)", action)
	}
	if action != "accept" {
		// The MCP spec says Content is only meaningful for "accept" replies;
		// non-accept replies must not carry content. The SDK does not enforce
		// this server-side but downstream tests rely on the wire shape, so we
		// drop the content here to mirror the SDK's TestElicitationNoValidation
		// expectation (cancel/decline carry empty content).
		content = nil
	} else if content == nil {
		// An accept with no content is legal (the schema may have no required
		// fields); use an empty map rather than nil so callers that range
		// over the result don't have to nil-check.
		content = map[string]any{}
	}
	return action, content, nil
}

// NewJSONStubHandler returns a Handler that always replies with the supplied
// JSON content. It is used by the CLI flag `--elicit-stub <json>`.
//
// The string is parsed once at construction time; subsequent edits to the
// caller's buffer have no effect on the returned handler.
func NewJSONStubHandler(jsonBody string) (Handler, error) {
	action, content, err := parseStubJSON([]byte(jsonBody))
	if err != nil {
		return nil, err
	}
	return HandlerFunc(func(_ context.Context, _ *officialMCP.ElicitRequest) (*officialMCP.ElicitResult, error) {
		return &officialMCP.ElicitResult{Action: action, Content: content}, nil
	}), nil
}

// NewFileStubHandler reads a JSON reply from the given path and returns a
// Handler that always replies with that content. It is used by the CLI flag
// `--elicit-stub-file <path>`.
//
// The file is read once at construction time; subsequent edits do not affect
// running clients. This is intentional — CI runs read the file at start.
func NewFileStubHandler(path string) (Handler, error) {
	if path == "" {
		return nil, fmt.Errorf("elicitation stub file path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read elicitation stub file %q: %w", path, err)
	}
	action, content, err := parseStubJSON(data)
	if err != nil {
		return nil, fmt.Errorf("elicitation stub file %q: %w", path, err)
	}
	return HandlerFunc(func(_ context.Context, _ *officialMCP.ElicitRequest) (*officialMCP.ElicitResult, error) {
		return &officialMCP.ElicitResult{Action: action, Content: content}, nil
	}), nil
}
