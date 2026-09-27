package protocol

import "strings"

// lifetime is the span of protocol versions in which one side may send a
// method, as a request or a notification.
type lifetime struct {
	request bool
	since   string // first version that defines it
	until   string // first version without it; "" while it is still defined
}

func (l lifetime) in(version string) bool {
	return version == "" || (version >= l.since && (l.until == "" || version < l.until))
}

// The MCP versions mcp-tui speaks, as the bounds of each lifetime below.
const (
	v20241105 = "2024-11-05"
	v20250618 = "2025-06-18"
	v20251125 = "2025-11-25"
)

// serverSends is every method an MCP server may send a client, in the spec
// versions 2024-11-05 through 2026-07-28 and the extensions mcp-tui speaks
// (the 2026-07-28 tasks extension, subscriptions/listen). From 2026-07-28 a
// server sends no requests at all: input requests ride results (SEP-2322)
// and ping is gone (SEP-2575).
var serverSends = map[string]lifetime{
	"ping":                   {request: true, since: v20241105, until: StatelessVersion},
	"sampling/createMessage": {request: true, since: v20241105, until: StatelessVersion},
	"roots/list":             {request: true, since: v20241105, until: StatelessVersion},
	"elicitation/create":     {request: true, since: v20250618, until: StatelessVersion},
	// 2025-11-25 experimental tasks: the server polls a task the client
	// runs for a sampling or elicitation request.
	"tasks/get":    {request: true, since: v20251125, until: StatelessVersion},
	"tasks/result": {request: true, since: v20251125, until: StatelessVersion},
	"tasks/list":   {request: true, since: v20251125, until: StatelessVersion},
	"tasks/cancel": {request: true, since: v20251125, until: StatelessVersion},

	"notifications/cancelled":                  {since: v20241105},
	"notifications/progress":                   {since: v20241105},
	"notifications/message":                    {since: v20241105},
	"notifications/resources/updated":          {since: v20241105},
	"notifications/resources/list_changed":     {since: v20241105},
	"notifications/tools/list_changed":         {since: v20241105},
	"notifications/prompts/list_changed":       {since: v20241105},
	"notifications/elicitation/complete":       {since: v20251125, until: StatelessVersion},
	"notifications/tasks/status":               {since: v20251125, until: StatelessVersion},
	"notifications/tasks":                      {since: StatelessVersion},
	"notifications/subscriptions/acknowledged": {since: StatelessVersion},
}

// clientCapabilityFor maps each server request a client capability invites
// to that capability's name in the client's initialize request.
var clientCapabilityFor = map[string]string{
	"sampling/createMessage": "sampling",
	"roots/list":             "roots",
	"elicitation/create":     "elicitation",
}

// ClientCapabilityFor returns the client capability that invites the server
// request method, or "" when no capability does.
func ClientCapabilityFor(method string) string {
	return clientCapabilityFor[method]
}

// clientSends is every method an MCP client may send a server; the watcher
// uses it to tell a server that sent a client's method from one that sent
// no MCP method at all.
var clientSends = map[string]lifetime{
	"initialize":               {request: true, since: v20241105, until: StatelessVersion},
	"server/discover":          {request: true, since: StatelessVersion},
	"ping":                     {request: true, since: v20241105, until: StatelessVersion},
	"tools/list":               {request: true, since: v20241105},
	"tools/call":               {request: true, since: v20241105},
	"resources/list":           {request: true, since: v20241105},
	"resources/templates/list": {request: true, since: v20241105},
	"resources/read":           {request: true, since: v20241105},
	"resources/subscribe":      {request: true, since: v20241105},
	"resources/unsubscribe":    {request: true, since: v20241105},
	"prompts/list":             {request: true, since: v20241105},
	"prompts/get":              {request: true, since: v20241105},
	"completion/complete":      {request: true, since: v20241105},
	"logging/setLevel":         {request: true, since: v20241105, until: StatelessVersion},
	"subscriptions/listen":     {request: true, since: StatelessVersion},
	"tasks/get":                {request: true, since: v20251125},
	"tasks/result":             {request: true, since: v20251125},
	"tasks/list":               {request: true, since: v20251125},
	"tasks/cancel":             {request: true, since: v20251125},
	"tasks/update":             {request: true, since: StatelessVersion},

	"notifications/initialized":        {since: v20241105, until: StatelessVersion},
	"notifications/cancelled":          {since: v20241105},
	"notifications/progress":           {since: v20241105},
	"notifications/roots/list_changed": {since: v20241105, until: StatelessVersion},
	"notifications/tasks/status":       {since: v20251125, until: StatelessVersion},
}

// MethodProblem is what is wrong with a method a server sent.
type MethodProblem int

const (
	// MethodOK: the version defines the method for servers to send, as sent.
	MethodOK MethodProblem = iota
	// MethodUndefined: no MCP version defines the method.
	MethodUndefined
	// MethodClientOnly: MCP defines the method only for clients to send.
	MethodClientOnly
	// MethodWrongKind: a request sent as a notification, or the reverse.
	MethodWrongKind
	// MethodNotInVersion: servers may send the method, but not in this
	// version.
	MethodNotInVersion
)

// CheckServerMethod reports whether a server may send method, as a request
// (request true) or a notification, on protocol version; an empty version
// accepts a method any version defines. For an undefined method it also
// returns the defined server method it most likely meant, or "".
func CheckServerMethod(version, method string, request bool) (MethodProblem, string) {
	l, ok := serverSends[method]
	switch {
	case !ok && isClientMethod(method):
		return MethodClientOnly, ""
	case !ok:
		return MethodUndefined, likelyMeant(method)
	case l.request != request:
		return MethodWrongKind, ""
	case !l.in(version):
		return MethodNotInVersion, ""
	}
	return MethodOK, ""
}

func isClientMethod(method string) bool {
	_, ok := clientSends[method]
	return ok
}

// likelyMeant is the MCP method a server most likely meant by an undefined
// one: the same name with the notifications/ prefix it dropped.
func likelyMeant(method string) string {
	if strings.HasPrefix(method, "notifications/") {
		return ""
	}
	prefixed := "notifications/" + method
	if _, ok := serverSends[prefixed]; ok || isClientMethod(prefixed) {
		return prefixed
	}
	return ""
}
