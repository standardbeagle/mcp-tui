package notifications

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// previewMaxLen is the cap on the inline preview string. Wide enough to
// show a uri or progress message without wrapping, narrow enough to fit
// next to a timestamp + type label in an 80-column terminal.
const previewMaxLen = 80

// FromRequest builds an Entry from a JSON-RPC method name plus the typed
// Request the SDK passed to the receiving middleware. We extract Level (for
// message), generate a one-line Preview, and stash the raw params under Raw
// so the user can copy the full JSON if needed.
//
// Returns ("", false) for any method that is not one of the seven captured
// notification types — the middleware should drop those silently to avoid
// polluting the stream with request/response traffic.
func FromRequest(method string, req officialMCP.Request, now time.Time) (Entry, bool) {
	t, ok := FromMethod(method)
	if !ok {
		return Entry{}, false
	}
	e := Entry{
		Time:   now,
		Type:   t,
		Method: method,
	}
	if req != nil {
		params := req.GetParams()
		e.Raw = params
		e.Level, e.Preview = describeParams(t, params)
	}
	return e, true
}

// describeParams returns (level, preview) for the given typed params. The
// SDK's typed structs are pointer types in the requests.go aliases, so we
// type-assert against the concrete pointer and read fields directly. For
// list_changed types (no fields) we return an empty preview — the type
// label alone is enough info, and a forced "(no params)" suffix would just
// be visual noise. Falls back to a JSON-marshaled summary when we don't
// recognize the params type, so unknown future fields still render legibly.
func describeParams(t Type, params officialMCP.Params) (level string, preview string) {
	if params == nil {
		return "", ""
	}
	switch t {
	case TypeMessage:
		if p, ok := params.(*officialMCP.LoggingMessageParams); ok && p != nil {
			return string(p.Level), formatLoggingData(p.Logger, p.Data)
		}
	case TypeProgress:
		if p, ok := params.(*officialMCP.ProgressNotificationParams); ok && p != nil {
			return "", formatProgress(p)
		}
	case TypeResourcesUpdated:
		if p, ok := params.(*officialMCP.ResourceUpdatedNotificationParams); ok && p != nil {
			return "", "uri=" + p.URI
		}
	case TypeCancelled:
		if p, ok := params.(*officialMCP.CancelledParams); ok && p != nil {
			return "", formatCancelled(p)
		}
	case TypeToolsListChanged, TypePromptsListChanged, TypeResourcesListChanged:
		// list_changed has no useful fields; preview is empty intentionally.
		return "", ""
	}
	// Fallback: marshal whatever the SDK gave us. This preserves forward-
	// compatibility — when the SDK adds a new field we still render it
	// rather than silently dropping it.
	return "", marshalSummary(params)
}

// formatLoggingData turns the logging data field into a one-line preview.
// LoggingMessageParams.Data is `any` (per spec, may be string, object,
// json.RawMessage, etc.). We marshal it to JSON and truncate so the preview
// stays single-line even if the server logged a verbose object.
func formatLoggingData(logger string, data any) string {
	var b strings.Builder
	if logger != "" {
		b.WriteString(logger)
		b.WriteString(": ")
	}
	b.WriteString(stringifyData(data))
	return truncate(b.String(), previewMaxLen)
}

// stringifyData renders the LoggingMessageParams.Data field. Strings are
// passed through; everything else gets JSON-marshaled. We strip surrounding
// quotes from json-encoded strings so a string log message reads naturally
// in the preview.
func stringifyData(data any) string {
	if data == nil {
		return ""
	}
	if s, ok := data.(string); ok {
		return s
	}
	if rm, ok := data.(json.RawMessage); ok {
		// Try to render a string raw message as bare text; otherwise pass
		// through as compact JSON.
		var s string
		if err := json.Unmarshal(rm, &s); err == nil {
			return s
		}
		return string(rm)
	}
	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Sprintf("<%T>", data)
	}
	return string(b)
}

// formatProgress renders the progress fields. Total is optional (zero means
// unknown per spec); we show "n/?" in that case so it's clear the bar is
// uncapped rather than at zero of zero.
func formatProgress(p *officialMCP.ProgressNotificationParams) string {
	var b strings.Builder
	if p.ProgressToken != nil {
		fmt.Fprintf(&b, "token=%v ", p.ProgressToken)
	}
	if p.Total > 0 {
		fmt.Fprintf(&b, "%g/%g", p.Progress, p.Total)
	} else {
		fmt.Fprintf(&b, "%g/?", p.Progress)
	}
	if p.Message != "" {
		b.WriteString(" \"")
		b.WriteString(p.Message)
		b.WriteString("\"")
	}
	return truncate(b.String(), previewMaxLen)
}

// formatCancelled renders requestId + reason in a compact form.
func formatCancelled(p *officialMCP.CancelledParams) string {
	var b strings.Builder
	if p.RequestID != nil {
		fmt.Fprintf(&b, "requestId=%v", p.RequestID)
	}
	if p.Reason != "" {
		if b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString("reason=\"")
		b.WriteString(p.Reason)
		b.WriteString("\"")
	}
	return truncate(b.String(), previewMaxLen)
}

// marshalSummary marshals an arbitrary value to compact JSON, truncating to
// previewMaxLen. Used as a defensive fallback when describeParams is fed a
// type we don't recognize.
func marshalSummary(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("<%T>", v)
	}
	return truncate(string(b), previewMaxLen)
}

// truncate shortens s to at most n characters, appending "..." when cut.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
