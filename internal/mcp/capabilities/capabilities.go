// Package capabilities provides a stable, JSON-friendly view of the negotiated
// MCP capabilities for both the connected server and the local client.
//
// Why this exists: the SDK's *officialMCP.InitializeResult and
// *officialMCP.ClientCapabilities are excellent for runtime use but inconvenient
// to render in a TUI tab or marshal as a CLI subcommand result because:
//   - the SDK types embed unexported helpers and version-bridging fields
//     (RootsV2, clientCapabilitiesV2);
//   - a missing-but-known capability (e.g. server does not advertise tools)
//     becomes a nil pointer that is easy to mis-render as "absent" vs
//     "unsupported";
//   - the extensions map (SEP-2133, SDK v1.4+) is the load-bearing field for
//     this feature and deserves an explicit, named slot in the snapshot.
//
// Snapshot is the externally-visible, marshal-stable representation. All known
// capability fields are present as pointers so the JSON output preserves the
// distinction between "supported" (non-nil) and "not supported" (nil/omitted).
// The renderers (TUI Capabilities tab, capabilities CLI subcommand) consume
// this type — they never see SDK internals directly.
package capabilities

import (
	"encoding/json"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Implementation mirrors officialMCP.Implementation but is JSON-stable for our
// own marshaling. We re-declare the type so tests don't break if the SDK adds
// a new field that happens to have the same JSON tag.
type Implementation struct {
	Name       string             `json:"name"`
	Title      string             `json:"title,omitempty"`
	Version    string             `json:"version"`
	WebsiteURL string             `json:"websiteUrl,omitempty"`
	Icons      []officialMCP.Icon `json:"icons,omitempty"`
}

// ServerCaps is the snapshot of *officialMCP.ServerCapabilities. Pointer fields
// are nil when the server did not advertise the capability — that distinction
// is observable in JSON output (omitempty on nil pointers omits the key).
type ServerCaps struct {
	Logging      *officialMCP.LoggingCapabilities    `json:"logging,omitempty"`
	Prompts      *officialMCP.PromptCapabilities     `json:"prompts,omitempty"`
	Resources    *officialMCP.ResourceCapabilities   `json:"resources,omitempty"`
	Tools        *officialMCP.ToolCapabilities       `json:"tools,omitempty"`
	Completions  *officialMCP.CompletionCapabilities `json:"completions,omitempty"`
	Experimental map[string]interface{}              `json:"experimental,omitempty"`
	Extensions   map[string]interface{}              `json:"extensions,omitempty"`
}

// ClientCaps is the snapshot of *officialMCP.ClientCapabilities. We store
// RootsV2 (the corrected pointer-typed roots field per SDK #607) rather than
// the deprecated value-type Roots — when the client advertises roots
// support, RootsV2 is non-nil.
type ClientCaps struct {
	Roots        *officialMCP.RootCapabilities        `json:"roots,omitempty"`
	Sampling     *officialMCP.SamplingCapabilities    `json:"sampling,omitempty"`
	Elicitation  *officialMCP.ElicitationCapabilities `json:"elicitation,omitempty"`
	Experimental map[string]interface{}               `json:"experimental,omitempty"`
	Extensions   map[string]interface{}               `json:"extensions,omitempty"`
}

// Snapshot is the full negotiated state captured at initialize time. Both the
// server side (Server*) and the client side (Client*) are present so a single
// JSON document or a single TUI tab can show the entire handshake.
//
// ProtocolVersion is the version the *server* responded with — per the spec,
// the server may answer with a different version than the client requested.
type Snapshot struct {
	ProtocolVersion string          `json:"protocolVersion"`
	ServerInfo      *Implementation `json:"serverInfo,omitempty"`
	ServerCaps      *ServerCaps     `json:"serverCaps,omitempty"`
	Instructions    string          `json:"instructions,omitempty"`
	ClientInfo      *Implementation `json:"clientInfo,omitempty"`
	ClientCaps      *ClientCaps     `json:"clientCaps,omitempty"`
}

// FromInitializeResult lifts an *officialMCP.InitializeResult plus the
// client-side counterparts into a Snapshot. Nil inputs produce nil sub-fields,
// which marshal as absent keys (omitempty). The function is pure so tests can
// drive it with hand-built fixtures.
//
// The clientImpl/clientCaps arguments come from our service: clientImpl is the
// mcp-tui Implementation we send during initialize, and clientCaps is what we
// computed via DeriveClientCapabilities.
func FromInitializeResult(
	res *officialMCP.InitializeResult,
	clientImpl *officialMCP.Implementation,
	clientCaps *officialMCP.ClientCapabilities,
) *Snapshot {
	snap := &Snapshot{}
	if res != nil {
		snap.ProtocolVersion = res.ProtocolVersion
		snap.Instructions = res.Instructions
		if res.ServerInfo != nil {
			snap.ServerInfo = implementationFrom(res.ServerInfo)
		}
		if res.Capabilities != nil {
			snap.ServerCaps = serverCapsFrom(res.Capabilities)
		}
	}
	if clientImpl != nil {
		snap.ClientInfo = implementationFrom(clientImpl)
	}
	if clientCaps != nil {
		snap.ClientCaps = clientCapsFrom(clientCaps)
	}
	return snap
}

// implementationFrom copies a *officialMCP.Implementation into our local type.
// We don't share the underlying Icon slice — copying keeps the snapshot
// independent of any later SDK mutation.
func implementationFrom(impl *officialMCP.Implementation) *Implementation {
	cp := &Implementation{
		Name:       impl.Name,
		Title:      impl.Title,
		Version:    impl.Version,
		WebsiteURL: impl.WebsiteURL,
	}
	if len(impl.Icons) > 0 {
		cp.Icons = append([]officialMCP.Icon(nil), impl.Icons...)
	}
	return cp
}

// serverCapsFrom copies the SDK ServerCapabilities into our snapshot type.
// Pointer fields are forwarded as-is when present (small structs; the SDK
// never mutates them after returning the InitializeResult).
func serverCapsFrom(caps *officialMCP.ServerCapabilities) *ServerCaps {
	out := &ServerCaps{
		Logging:     caps.Logging,
		Prompts:     caps.Prompts,
		Resources:   caps.Resources,
		Tools:       caps.Tools,
		Completions: caps.Completions,
	}
	if len(caps.Experimental) > 0 {
		out.Experimental = anyMapToInterface(caps.Experimental)
	}
	if len(caps.Extensions) > 0 {
		out.Extensions = anyMapToInterface(caps.Extensions)
	}
	return out
}

// clientCapsFrom copies the SDK ClientCapabilities into our snapshot type.
// We use RootsV2 (the modern pointer-valued field) when present and fall back
// to the deprecated Roots struct otherwise — older SDK call sites or older
// servers may have populated only the legacy field.
func clientCapsFrom(caps *officialMCP.ClientCapabilities) *ClientCaps {
	out := &ClientCaps{
		Sampling:    caps.Sampling,
		Elicitation: caps.Elicitation,
	}
	if caps.RootsV2 != nil {
		// Copy by value so later mutation in the SDK doesn't leak in.
		rv := *caps.RootsV2
		out.Roots = &rv
	} else if caps.Roots.ListChanged {
		// Legacy struct — only meaningful when ListChanged is set, since the
		// zero value indicates "client did not advertise roots".
		out.Roots = &officialMCP.RootCapabilities{ListChanged: caps.Roots.ListChanged}
	}
	if len(caps.Experimental) > 0 {
		out.Experimental = anyMapToInterface(caps.Experimental)
	}
	if len(caps.Extensions) > 0 {
		out.Extensions = anyMapToInterface(caps.Extensions)
	}
	return out
}

// anyMapToInterface converts the SDK's map[string]any to map[string]interface{}.
// They are the same type in modern Go but the explicit conversion keeps the
// public API stable even if Go ever divergences the aliases.
func anyMapToInterface(m map[string]any) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// MarshalJSON ensures deterministic key ordering. Go's encoding/json
// already sorts keys for map[string]interface{} at every nesting level, so
// we round-trip through a generic representation: struct fields become
// alphabetical map keys, and the inner Experimental/Extensions maps are
// sorted automatically by the standard library. The result is byte-stable
// across runs without needing a hand-rolled marshaller.
//
// Determinism matters because users may diff capability dumps across server
// versions to detect feature changes — unstable output would produce noisy
// diffs.
func (s *Snapshot) MarshalJSON() ([]byte, error) {
	type alias Snapshot
	if s == nil {
		return []byte("null"), nil
	}
	raw, err := json.Marshal((*alias)(s))
	if err != nil {
		return nil, err
	}
	var generic map[string]interface{}
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(generic)
}

// DeriveClientCapabilities replicates the SDK's client.capabilities() logic
// from our known service state. We need this because the SDK does not expose
// the negotiated client capabilities after Connect — they are computed inside
// Client.Connect from ClientOptions and immediately sent to the server.
//
// Inputs mirror the three knobs we expose on the service:
//   - hasSampling          — true when SetSamplingHandler was called with a non-nil value
//   - samplingTools        — true when the handler also implements WithToolsHandler
//   - hasElicitation       — true when SetElicitationHandler was called with a non-nil value
//   - protocolVersion      — the negotiated server-confirmed protocol version
//   - rootsListChanged     — always true for our client (we always pass roots through SetInitialRoots/AddRoots)
//
// The SDK's default behavior is to advertise roots:{listChanged:true} unless
// ClientOptions.Capabilities is explicitly set, which mcp-tui never does.
// Sampling and elicitation are then layered on top when the matching handler
// is registered. The 2025-11-25 protocol version adds form-elicitation, which
// the SDK auto-fills on newer connections.
//
// This function is the single source of truth for our client capabilities so
// the snapshot matches what we actually sent on the wire.
func DeriveClientCapabilities(
	hasSampling bool,
	samplingTools bool,
	hasElicitation bool,
	protocolVersion string,
	rootsListChanged bool,
) *officialMCP.ClientCapabilities {
	caps := &officialMCP.ClientCapabilities{
		RootsV2: &officialMCP.RootCapabilities{ListChanged: rootsListChanged},
	}
	// Mirror the SDK's backward-compatibility sync.
	caps.Roots.ListChanged = rootsListChanged

	if hasSampling {
		caps.Sampling = &officialMCP.SamplingCapabilities{}
		if samplingTools {
			caps.Sampling.Tools = &officialMCP.SamplingToolsCapabilities{}
		}
	}

	if hasElicitation {
		caps.Elicitation = &officialMCP.ElicitationCapabilities{}
		// Form elicitation was added in 2025-11-25; the SDK auto-fills it for
		// newer protocol versions and the empty {} is treated equivalently on
		// older servers (they ignore unknown sub-fields).
		if protocolVersion >= "2025-11-25" {
			caps.Elicitation.Form = &officialMCP.FormElicitationCapabilities{}
		}
	}

	return caps
}
