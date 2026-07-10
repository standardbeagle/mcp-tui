package mcp

import (
	"context"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/capabilities"
	"github.com/standardbeagle/mcp-tui/internal/mcp/elicitation"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
	"github.com/standardbeagle/mcp-tui/internal/mcp/oauth"
	"github.com/standardbeagle/mcp-tui/internal/mcp/sampling"
)

// Service provides high-level MCP operations
type Service interface {
	// Connection management
	Connect(ctx context.Context, config *config.ConnectionConfig) error
	Disconnect() error
	IsConnected() bool
	SetDebugMode(debug bool)

	// SetSamplingHandler installs a handler for server-initiated
	// sampling/createMessage requests. It must be called before Connect; the
	// SDK reads the handler at client construction time. Pass nil to clear.
	SetSamplingHandler(handler sampling.Handler)

	// SetElicitationHandler installs a handler for server-initiated
	// elicitation/create requests. It must be called before Connect; the
	// SDK reads the handler at client construction time. Pass nil to clear.
	SetElicitationHandler(handler elicitation.Handler)

	// SetInitialRoots installs the initial set of roots advertised to the
	// server. Must be called before Connect — the SDK seeds the client's
	// roots feature set at construction time, so installing later has no
	// effect on already-running sessions. Pass nil or an empty slice to
	// advertise no roots (the default).
	SetInitialRoots(roots []*officialMCP.Root)

	// AddRoots appends the given roots to the client and (if connected)
	// fires a roots/list_changed notification so the server can re-fetch.
	// Safe to call before Connect — the roots are accumulated and seeded
	// at connect time.
	AddRoots(roots ...*officialMCP.Root)

	// RemoveRoots removes roots with the given URIs from the client and
	// (if connected) fires a roots/list_changed notification. URIs that
	// do not match any current root are silently ignored.
	RemoveRoots(uris ...string)

	// ListRoots returns a snapshot of the current roots. The slice is a
	// copy and may be mutated by the caller without affecting the service.
	ListRoots() []*officialMCP.Root

	// GetOAuthHandler returns the OAuth handler that was wired into the
	// transport at Connect time, or nil if the connection did not use
	// OAuth. The TUI status indicator reads Status() from this handler;
	// the Re-authenticate keybinding calls Reauthenticate() on it.
	GetOAuthHandler() *oauth.Handler

	// Tool operations
	ListTools(ctx context.Context) ([]Tool, error)
	CallTool(ctx context.Context, req CallToolRequest) (*CallToolResult, error)

	// Resource operations
	ListResources(ctx context.Context) ([]Resource, error)
	// ListResourceTemplates returns the URI-template descriptions surfaced by
	// resources/templates/list. Servers that lack the resources capability
	// or never registered any templates return an empty slice without error
	// — callers should render an empty section rather than treating absence
	// as a failure.
	ListResourceTemplates(ctx context.Context) ([]ResourceTemplate, error)
	ReadResource(ctx context.Context, uri string) ([]ResourceContents, error)

	// Prompt operations
	ListPrompts(ctx context.Context) ([]Prompt, error)
	GetPrompt(ctx context.Context, req GetPromptRequest) (*GetPromptResult, error)

	// Complete sends a completion/complete request to the server. The
	// request's Ref identifies a prompt or resource template, and ArgumentName
	// + ArgumentValue describe the variable being typed and its prefix. The
	// server returns a deterministic ordered list of suggestions; an empty
	// list (with err == nil) is a normal "no matches" outcome.
	Complete(ctx context.Context, req CompleteRequest) (*CompleteResult, error)

	// Server info
	GetServerInfo() *ServerInfo

	// GetCapabilitiesSnapshot returns the negotiated server + client
	// capabilities captured at the most recent successful Connect. Returns
	// nil when the service has never connected. The snapshot is read-only
	// — callers may marshal it to JSON or render it in the TUI but must
	// not mutate it.
	GetCapabilitiesSnapshot() *capabilities.Snapshot

	// NotificationStream returns the per-service ring buffer of captured
	// server-to-client notifications. The returned stream is shared across
	// all callers (UI tab, CLI flag, tests) and is safe for concurrent use.
	// Lazy-initialized so even tests that bypass Connect see a non-nil
	// stream and can append fixture entries.
	NotificationStream() *notifications.Stream

	// AddNotificationObserver registers a callback that fires once per
	// captured Entry, in addition to the entry being appended to the ring
	// buffer. Callbacks must return quickly — they run on the SDK's
	// receiving goroutine. Pass nil to make this a no-op.
	AddNotificationObserver(fn func(notifications.Entry))

	// Connection health and monitoring
	GetConnectionHealth() map[string]interface{}
	ConfigureReconnection(maxAttempts int, delay time.Duration)
	ConfigureHealthCheck(interval time.Duration)

	// Error handling and diagnostics
	GetErrorStatistics() map[string]interface{}
	GetErrorReport() map[string]interface{}
	ResetErrorStatistics()

	// Event tracing and debugging
	GetTracingStatistics() map[string]interface{}
	GetRecentEvents(count int) interface{}
	ExportEvents() ([]byte, error)
	// ExportReplayScript translates the recorded client→server requests into
	// an equivalent `mcp-tui` CLI shell script for automation replay.
	ExportReplayScript() (string, error)
	ClearEvents()

	// Configuration management
	GetConfiguration() map[string]interface{}
	UpdateConfiguration(config map[string]interface{}) error

	// Connection state and diagnostics
	GetConnectionDisplayMessage() string
	GetServerDiagnosticMessage() string
	GetConnectionConfig() *config.ConnectionConfig
}

// SchemaError captures schema parsing/validation issues
type SchemaError struct {
	Message   string                 `json:"message"`
	RawSchema string                 `json:"rawSchema,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
}

// ToolAnnotations holds the MCP-spec annotation hints surfaced by a tool.
//
// The fields use *bool for hints that have non-default values per the MCP
// spec (DestructiveHint defaults to true, OpenWorldHint defaults to true);
// nil means "the server did not advertise a value" so callers can apply the
// spec defaults. ReadOnlyHint and IdempotentHint default to false, so a
// missing value is indistinguishable from an explicit false — matching the
// SDK's representation.
type ToolAnnotations struct {
	// Title is a human-friendly title for the tool. UI surfaces should prefer
	// the top-level Tool.Title field, falling back to this Annotations.Title,
	// then to Tool.Name (per MCP 2025-06-18 §tools).
	Title string `json:"title,omitempty"`

	// ReadOnlyHint indicates the tool does not modify its environment.
	ReadOnlyHint bool `json:"readOnlyHint,omitempty"`

	// DestructiveHint indicates the tool may perform destructive updates.
	// nil = server did not advertise (spec default: true when ReadOnlyHint=false).
	DestructiveHint *bool `json:"destructiveHint,omitempty"`

	// IdempotentHint indicates repeated calls with the same arguments are safe.
	IdempotentHint bool `json:"idempotentHint,omitempty"`

	// OpenWorldHint indicates the tool interacts with an open world of
	// external entities. nil = server did not advertise (spec default: true).
	OpenWorldHint *bool `json:"openWorldHint,omitempty"`
}

// Tool represents an MCP tool
type Tool struct {
	Name        string                 `json:"name"`
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	Icons       []officialMCP.Icon     `json:"icons,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
	// OutputSchema is the JSON Schema describing the structured result this
	// tool returns when callers consume `structuredContent` from CallToolResult.
	// Optional per the MCP 2025-06-18 spec — nil means the server did not
	// advertise a schema and validation is a no-op. Stored as map[string]any
	// rather than *jsonschema.Schema so the schema-decode failure (if any) is
	// captured in SchemaError without forcing the validator dependency on every
	// consumer of the Tool struct.
	OutputSchema map[string]interface{} `json:"outputSchema,omitempty"`
	Annotations  *ToolAnnotations       `json:"annotations,omitempty"`
	SchemaError  *SchemaError           `json:"schemaError,omitempty"`
}

// HasSchemaError returns true if the tool has a schema parsing error
func (t Tool) HasSchemaError() bool {
	return t.SchemaError != nil
}

// DisplayName returns the user-facing label for the tool. Per MCP 2025-06-18
// the precedence is: Tool.Title (top-level) → Annotations.Title → Tool.Name.
func (t Tool) DisplayName() string {
	if t.Title != "" {
		return t.Title
	}
	if t.Annotations != nil && t.Annotations.Title != "" {
		return t.Annotations.Title
	}
	return t.Name
}

// IsDestructive reports whether the tool is flagged as destructive.
//
// Resolution rules:
//   - ReadOnlyHint=true ⇒ never destructive (the destructiveHint is meaningless
//     when readOnlyHint is true per the MCP spec).
//   - Annotations missing or DestructiveHint nil ⇒ NOT destructive. mcp-tui
//     deliberately diverges from the spec's "default true" so existing
//     unannotated tools keep their no-prompt behaviour; servers must opt in
//     by advertising destructiveHint=true to trigger a confirm gate.
//   - Otherwise the advertised hint is honoured.
func (t Tool) IsDestructive() bool {
	if t.Annotations == nil {
		return false
	}
	if t.Annotations.ReadOnlyHint {
		return false
	}
	if t.Annotations.DestructiveHint == nil {
		return false
	}
	return *t.Annotations.DestructiveHint
}

// IsReadOnly reports whether the tool advertises readOnlyHint=true.
func (t Tool) IsReadOnly() bool {
	return t.Annotations != nil && t.Annotations.ReadOnlyHint
}

// IsIdempotent reports whether the tool advertises idempotentHint=true.
// Per the MCP spec, idempotentHint is meaningful only when readOnlyHint=false;
// a read-only tool is implicitly idempotent so we still surface a true hint
// only when the server explicitly set it.
func (t Tool) IsIdempotent() bool {
	return t.Annotations != nil && t.Annotations.IdempotentHint
}

// IsOpenWorld reports whether the tool advertises openWorldHint=true. Returns
// false when the hint is absent rather than the spec default of true: the badge
// is informational, not safety-critical, and showing it for every unannotated
// tool would create badge noise.
func (t Tool) IsOpenWorld() bool {
	if t.Annotations == nil || t.Annotations.OpenWorldHint == nil {
		return false
	}
	return *t.Annotations.OpenWorldHint
}

// BadgeString returns a compact uncolored badge string for the tool's
// annotations, e.g. "[D][I]". Returns an empty string when the tool has no
// surfaceable hints. Order is fixed so list rendering stays stable across runs:
// destructive → readOnly → idempotent → openWorld. R and D are mutually
// exclusive (IsDestructive suppresses on read-only) so we render at most one
// of them.
func (t Tool) BadgeString() string {
	var b []byte
	switch {
	case t.IsDestructive():
		b = append(b, "[D]"...)
	case t.IsReadOnly():
		b = append(b, "[R]"...)
	}
	if t.IsIdempotent() {
		b = append(b, "[I]"...)
	}
	if t.IsOpenWorld() {
		b = append(b, "[O]"...)
	}
	return string(b)
}

// Resource represents an MCP resource
type Resource struct {
	URI         string             `json:"uri"`
	Name        string             `json:"name,omitempty"`
	Title       string             `json:"title,omitempty"`
	Description string             `json:"description,omitempty"`
	MimeType    string             `json:"mimeType,omitempty"`
	Icons       []officialMCP.Icon `json:"icons,omitempty"`
}

func (r Resource) DisplayName() string {
	if r.Title != "" {
		return r.Title
	}
	if r.Name != "" {
		return r.Name
	}
	return r.URI
}

// ResourceTemplate represents an MCP resource URI template (RFC 6570) returned
// by resources/templates/list. Unlike Resource, the URITemplate field carries
// `{var}` placeholders that callers expand at read time. Title falls back to
// Name for display, matching the MCP 2025-06-18 §resources spec.
type ResourceTemplate struct {
	URITemplate string             `json:"uriTemplate"`
	Name        string             `json:"name,omitempty"`
	Title       string             `json:"title,omitempty"`
	Description string             `json:"description,omitempty"`
	MimeType    string             `json:"mimeType,omitempty"`
	Icons       []officialMCP.Icon `json:"icons,omitempty"`
}

// DisplayName returns the user-facing label for the template, preferring
// Title (added in 2025-06-18) over Name to match the Resource spec, with the
// raw URI template as a final fallback so the row never renders blank.
func (t ResourceTemplate) DisplayName() string {
	if t.Title != "" {
		return t.Title
	}
	if t.Name != "" {
		return t.Name
	}
	return t.URITemplate
}

// CompleteRequest carries the parameters for a completion/complete call. The
// reference identifies what is being completed (a prompt argument or a resource
// URI template variable), ArgumentName is the variable being typed, and
// ArgumentValue is the prefix typed so far. Context.Arguments lets the caller
// supply previously-resolved variables so the server can scope suggestions
// (per MCP 2025-06-18 §completion).
type CompleteRequest struct {
	// Ref must have Type "ref/prompt" with Name set, or "ref/resource" with
	// URI set to a URI template. Mismatched fields are rejected by the SDK
	// before the request is sent.
	Ref CompleteReference `json:"ref"`
	// ArgumentName is the prompt-argument or template-variable name.
	ArgumentName string `json:"argumentName"`
	// ArgumentValue is the value typed so far. Empty string requests
	// suggestions for an empty prefix.
	ArgumentValue string `json:"argumentValue"`
	// ContextArguments supplies previously-resolved values so the server can
	// scope its suggestions. nil is allowed.
	ContextArguments map[string]string `json:"contextArguments,omitempty"`
}

// CompleteReference targets either a prompt or a resource template. Exactly
// one of Name/URI must be populated; mcp-tui constructs these via the
// PromptRef and ResourceRef helpers.
type CompleteReference struct {
	// Type must be "ref/prompt" or "ref/resource".
	Type string `json:"type"`
	// Name is set for ref/prompt. URI is set for ref/resource (the raw URI
	// template string).
	Name string `json:"name,omitempty"`
	URI  string `json:"uri,omitempty"`
}

// PromptRef constructs a ref/prompt CompleteReference for the named prompt.
func PromptRef(name string) CompleteReference {
	return CompleteReference{Type: "ref/prompt", Name: name}
}

// ResourceRef constructs a ref/resource CompleteReference targeting a URI
// template. The caller passes the raw template (e.g. "users://{userId}").
func ResourceRef(uriTemplate string) CompleteReference {
	return CompleteReference{Type: "ref/resource", URI: uriTemplate}
}

// CompleteResult carries the suggestions returned by the server. HasMore
// signals truncated results; Total is the server's claimed total when known
// (zero means unspecified).
type CompleteResult struct {
	Values  []string `json:"values"`
	HasMore bool     `json:"hasMore,omitempty"`
	Total   int      `json:"total,omitempty"`
}

// ResourceContents represents the contents of a resource
type ResourceContents struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

// Prompt represents an MCP prompt
type Prompt struct {
	Name        string                 `json:"name"`
	Title       string                 `json:"title,omitempty"`
	Description string                 `json:"description,omitempty"`
	Arguments   map[string]interface{} `json:"arguments,omitempty"`
	Icons       []officialMCP.Icon     `json:"icons,omitempty"`
}

func (p Prompt) DisplayName() string {
	if p.Title != "" {
		return p.Title
	}
	return p.Name
}

// PromptMessage represents a message in a prompt
type PromptMessage struct {
	Role    string    `json:"role"`
	Content []Content `json:"content"`
}

// Content represents various types of content
type Content struct {
	Type     string             `json:"type"`
	Text     string             `json:"text,omitempty"`
	Data     string             `json:"data,omitempty"`
	MimeType string             `json:"mimeType,omitempty"`
	Resource *ResourceReference `json:"resource,omitempty"`
}

// ResourceReference represents a reference to a resource
type ResourceReference struct {
	Type        string             `json:"type"`
	URI         string             `json:"uri"`
	Name        string             `json:"name,omitempty"`
	Title       string             `json:"title,omitempty"`
	Description string             `json:"description,omitempty"`
	MimeType    string             `json:"mimeType,omitempty"`
	Size        *int64             `json:"size,omitempty"`
	Icons       []officialMCP.Icon `json:"icons,omitempty"`
}

// CallToolRequest represents a tool call request
type CallToolRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// CallToolResult represents a tool call result
type CallToolResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
	// StructuredContent carries the optional structured payload from MCP
	// 2025-06-18 tool results. nil when the server did not return one. The
	// service layer normalises it to a JSON-friendly Go value (map/slice/
	// scalar) so consumers do not need the SDK type to render or validate it.
	StructuredContent any `json:"structuredContent,omitempty"`
	// OutputViolations lists schema-validation failures found by comparing
	// StructuredContent against the calling Tool's OutputSchema. Empty (or
	// nil) when validation passes, when no outputSchema was advertised, or
	// when the tool returned no structured payload alongside no schema.
	// Populated server-side by the service layer at CallTool time so every
	// surface (TUI banner, CLI stderr warning, --strict-output exit code)
	// reads the same authoritative slice.
	OutputViolations []string `json:"outputViolations,omitempty"`
}

// GetPromptRequest represents a prompt request
type GetPromptRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// GetPromptResult represents a prompt result
type GetPromptResult struct {
	Description string          `json:"description,omitempty"`
	Messages    []PromptMessage `json:"messages"`
}

// ServerInfo holds server information
type ServerInfo struct {
	Name            string                 `json:"name"`
	Version         string                 `json:"version"`
	ProtocolVersion string                 `json:"protocolVersion"`
	Capabilities    map[string]interface{} `json:"capabilities"`
	Connected       bool                   `json:"connected"`
}
