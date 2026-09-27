// Package protocolwatch checks the JSON-RPC traffic a server sends against
// JSON-RPC 2.0 and the negotiated MCP version, and reports what the SDK
// would silently drop: responses to no request, second responses, methods
// MCP does not define, and messages that are not JSON-RPC at all.
//
// A Watcher is a wiretap.Observer. It sees the client's requests too, to
// know which ids are outstanding. Responses arriving out of order are
// legal JSON-RPC and reported as information, never as a violation.
package protocolwatch

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/standardbeagle/mcp-tui/internal/mcp/protocol"
	"github.com/standardbeagle/mcp-tui/internal/redact"
)

// Kind classifies a violation.
type Kind string

const (
	KindUnknownID         Kind = "unknown-response-id"
	KindDuplicateResponse Kind = "duplicate-response"
	KindUndefinedMethod   Kind = "undefined-method"
	KindMalformed         Kind = "malformed-message"
)

// Violation is one message the server sent in breach of JSON-RPC 2.0 or of
// the negotiated MCP version.
type Violation struct {
	Kind    Kind   `json:"kind"`
	Method  string `json:"method,omitempty"`
	ID      string `json:"id,omitempty"` // the wire id as JSON: 7 or "a7"
	Message string `json:"message"`
	Raw     string `json:"raw"` // the message, redacted and shortened
}

// Ordering records a response that overtook an earlier request's: legal in
// JSON-RPC, worth seeing when a server's replies look mixed up.
type Ordering struct {
	ID              string
	Method          string
	OvertakenID     string
	OvertakenMethod string
	Message         string
}

// rawLimit bounds the message text a violation carries.
const rawLimit = 300

// answeredLimit bounds how many answered ids are kept to recognize a
// second response; a duplicate arrives right after the first.
const answeredLimit = 1024

// streamMethods are requests that stay open for the whole session, so
// every other response overtakes them by design.
var streamMethods = map[string]bool{"subscriptions/listen": true}

type outstandingRequest struct {
	method string
	seq    uint64
}

// Watcher checks one service's traffic. Its hooks run on the goroutine that
// read or wrote the message, outside the watcher's lock.
type Watcher struct {
	onViolation func(Violation)
	onOrdering  func(Ordering)

	mu            sync.Mutex
	version       string
	seq           uint64
	outstanding   map[string]outstandingRequest
	answered      map[string]string // id → method
	answeredOrder []string
	violations    []Violation
}

// New returns a watcher calling onViolation and onOrdering (either may be
// nil) for each finding.
func New(onViolation func(Violation), onOrdering func(Ordering)) *Watcher {
	w := &Watcher{onViolation: onViolation, onOrdering: onOrdering}
	w.forgetIDs()
	return w
}

// SetProtocolVersion sets the version methods are checked against; until
// it is set, a method any version defines passes.
func (w *Watcher) SetProtocolVersion(version string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.version = version
}

// Violations returns every violation since New or the last Reset.
func (w *Watcher) Violations() []Violation {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]Violation(nil), w.violations...)
}

// Reset forgets everything: violations, ids and the protocol version. Call
// it when the service connects anew.
func (w *Watcher) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.version = ""
	w.violations = nil
	w.forgetIDs()
}

func (w *Watcher) forgetIDs() {
	w.outstanding = map[string]outstandingRequest{}
	w.answered = map[string]string{}
	w.answeredOrder = nil
}

// Connected starts a new connection, whose requests are numbered afresh.
func (w *Watcher) Connected(officialMCP.Connection) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.forgetIDs()
}

// Sent records each request the client sends as outstanding.
func (w *Watcher) Sent(msg jsonrpc.Message) {
	req, ok := msg.(*jsonrpc.Request)
	if !ok || !req.IsCall() {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	w.outstanding[idText(req.ID)] = outstandingRequest{method: req.Method, seq: w.seq}
}

// Received checks each message from the server. It never withholds one.
func (w *Watcher) Received(msg jsonrpc.Message) bool {
	var v *Violation
	var o *Ordering
	switch m := msg.(type) {
	case *jsonrpc.Request:
		v = w.checkMethod(m)
	case *jsonrpc.Response:
		v, o = w.checkResponse(m)
	}
	if v != nil {
		v.Raw = snippet(msg)
		w.report(*v)
	}
	if o != nil && w.onOrdering != nil {
		w.onOrdering(*o)
	}
	return false
}

// Malformed reports a server message that did not decode as JSON-RPC 2.0,
// except an error answering a request the server could not identify,
// which JSON-RPC sends with id null.
func (w *Watcher) Malformed(raw []byte, err error) {
	var probe struct {
		Version *string         `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Method  json.RawMessage `json:"method"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	reason := err.Error()
	switch {
	case json.Unmarshal(raw, &probe) != nil:
		reason = "it is not JSON"
	case probe.Version == nil || *probe.Version != "2.0":
		reason = `it lacks "jsonrpc": "2.0"`
	case string(probe.ID) == "null" && probe.Method == nil && probe.Error != nil && probe.Result == nil:
		return
	}
	w.report(Violation{
		Kind:    KindMalformed,
		Message: "server sent a message that is not JSON-RPC 2.0: " + reason,
		Raw:     shorten(redactRaw(raw)),
	})
}

func (w *Watcher) report(v Violation) {
	w.mu.Lock()
	w.violations = append(w.violations, v)
	w.mu.Unlock()
	if w.onViolation != nil {
		w.onViolation(v)
	}
}

func (w *Watcher) checkMethod(req *jsonrpc.Request) *Violation {
	w.mu.Lock()
	version := w.version
	w.mu.Unlock()
	kind := "notification"
	if req.IsCall() {
		kind = "request"
	}
	problem, meant := protocol.CheckServerMethod(version, req.Method, req.IsCall())
	var text string
	switch problem {
	case protocol.MethodOK:
		return nil
	case protocol.MethodUndefined:
		text = fmt.Sprintf("server sent %s %q, which MCP does not define", kind, req.Method)
		if meant != "" {
			text += fmt.Sprintf(" (did you mean %s?)", meant)
		}
	case protocol.MethodClientOnly:
		text = fmt.Sprintf("server sent %s %q, which MCP defines only for clients to send", kind, req.Method)
	case protocol.MethodWrongKind:
		defined := "request"
		if req.IsCall() {
			defined = "notification"
		}
		text = fmt.Sprintf("server sent %q as a %s; MCP defines it as a %s", req.Method, kind, defined)
	case protocol.MethodNotInVersion:
		text = fmt.Sprintf("server sent %s %q, which MCP %s does not let servers send", kind, req.Method, version)
	}
	v := &Violation{Kind: KindUndefinedMethod, Method: req.Method, Message: text}
	if req.IsCall() {
		v.ID = idText(req.ID)
	}
	return v
}

func (w *Watcher) checkResponse(resp *jsonrpc.Response) (*Violation, *Ordering) {
	key := idText(resp.ID)
	w.mu.Lock()
	req, isOutstanding := w.outstanding[key]
	answeredMethod, wasAnswered := w.answered[key]
	var overtaken *Ordering
	if isOutstanding {
		delete(w.outstanding, key)
		w.remember(key, req.method)
		overtaken = w.overtaken(key, req)
	}
	w.mu.Unlock()

	switch {
	case isOutstanding && resp.Result != nil && resp.Error != nil:
		return &Violation{Kind: KindMalformed, Method: req.method, ID: key,
			Message: fmt.Sprintf("server sent a response to request %s (%s) with both result and error", key, req.method)}, overtaken
	case isOutstanding && resp.Result == nil && resp.Error == nil:
		return &Violation{Kind: KindMalformed, Method: req.method, ID: key,
			Message: fmt.Sprintf("server sent a response to request %s (%s) with neither result nor error", key, req.method)}, overtaken
	case isOutstanding:
		return nil, overtaken
	case wasAnswered:
		return &Violation{Kind: KindDuplicateResponse, Method: answeredMethod, ID: key,
			Message: fmt.Sprintf("server sent a second response to request %s (%s)", key, answeredMethod)}, nil
	default:
		return &Violation{Kind: KindUnknownID, ID: key,
			Message: fmt.Sprintf("server sent a response with id %s that matches no request", key)}, nil
	}
}

// remember keeps key as answered, dropping the oldest beyond answeredLimit.
// Callers hold w.mu.
func (w *Watcher) remember(key, method string) {
	w.answered[key] = method
	w.answeredOrder = append(w.answeredOrder, key)
	if len(w.answeredOrder) > answeredLimit {
		delete(w.answered, w.answeredOrder[0])
		w.answeredOrder = w.answeredOrder[1:]
	}
}

// overtaken returns the earliest request, sent before req and still
// outstanding, that the response to req overtook. Callers hold w.mu.
func (w *Watcher) overtaken(key string, req outstandingRequest) *Ordering {
	var earliestKey string
	var earliest outstandingRequest
	for k, other := range w.outstanding {
		if other.seq < req.seq && !streamMethods[other.method] && (earliestKey == "" || other.seq < earliest.seq) {
			earliestKey, earliest = k, other
		}
	}
	if earliestKey == "" {
		return nil
	}
	return &Ordering{
		ID: key, Method: req.method, OvertakenID: earliestKey, OvertakenMethod: earliest.method,
		Message: fmt.Sprintf("response to #%s (%s) arrived before #%s (%s), which was sent first",
			key, req.method, earliestKey, earliest.method),
	}
}

// idText is a JSON-RPC id as it appears on the wire: 7 or "a7", so a
// string id never matches a number.
func idText(id jsonrpc.ID) string {
	switch v := id.Raw().(type) {
	case int64:
		return strconv.FormatInt(v, 10)
	case string:
		return strconv.Quote(v)
	}
	return "null"
}

func snippet(msg jsonrpc.Message) string {
	raw, err := jsonrpc.EncodeMessage(msg)
	if err != nil {
		return fmt.Sprintf("[unencodable message: %v]", err)
	}
	return shorten(redactRaw(raw))
}

// redactRaw masks credentials in a message as the Messages log does: in
// URLs inside strings and in credential-named _meta entries.
func redactRaw(raw []byte) string {
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return redact.Text(string(raw))
	}
	out, err := json.Marshal(redact.Payload(doc))
	if err != nil {
		return fmt.Sprintf("[unencodable message, %d bytes]", len(raw))
	}
	return string(out)
}

func shorten(s string) string {
	if len(s) <= rawLimit {
		return s
	}
	return s[:rawLimit] + "…"
}
