package wiretap

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// teeRoundTripper serves the tap on HTTP transports, whose connection it
// cannot wrap: it lets the observers stamp requests, shows them each
// JSON-RPC message in a request body before it goes out, and each one in a
// response body as the SDK reads it. It never changes a byte the SDK sees.
type teeRoundTripper struct {
	base http.RoundTripper
	tap  *Tap
}

func (t *teeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	for _, o := range t.tap.observers {
		if s, ok := o.(RequestStamper); ok {
			req = s.StampRequest(req)
		}
	}
	if req.Method == http.MethodPost {
		t.observeRequestBody(req)
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp.Body == nil {
		return resp, err
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		// The SDK rejects the body too: no JSON-RPC in it to observe.
		return resp, nil
	}
	// Outside 2xx the SDK only looks for a JSON-RPC error in the body; a
	// body that is not one (an OAuth error, say) is no protocol violation.
	in := inbound{tap: t.tap, reportMalformed: resp.StatusCode >= 200 && resp.StatusCode < 300}
	switch mediaType {
	case "application/json":
		resp.Body = &jsonTee{body: resp.Body, in: in}
	case "text/event-stream":
		resp.Body = &sseTee{body: resp.Body, in: in}
	}
	return resp, nil
}

// observeRequestBody shows the messages in a POST body to the observers,
// reading a copy so the request itself is untouched. The SDK builds its
// bodies from byte slices, so GetBody is always set.
func (t *teeRoundTripper) observeRequestBody(req *http.Request) {
	if req.GetBody == nil {
		return
	}
	body, err := req.GetBody()
	if err != nil {
		return
	}
	data, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		return
	}
	for _, raw := range splitBatch(data) {
		if msg, err := jsonrpc.DecodeMessage(raw); err == nil {
			t.tap.sent(msg)
		}
	}
}

// inbound decodes server message bodies for the observers.
type inbound struct {
	tap             *Tap
	reportMalformed bool
}

// observe handles an HTTP body or event that holds one message or a batch.
func (in inbound) observe(data []byte) {
	for _, raw := range splitBatch(data) {
		msg, err := jsonrpc.DecodeMessage(raw)
		if err != nil {
			if in.reportMalformed {
				in.tap.malformed(raw, err)
			}
			continue
		}
		in.tap.received(msg)
	}
}

// splitBatch returns the messages of a body: the elements of a JSON array,
// or the body itself.
func splitBatch(data []byte) [][]byte {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil
	}
	if data[0] == '[' {
		var batch []json.RawMessage
		if json.Unmarshal(data, &batch) == nil {
			out := make([][]byte, len(batch))
			for i, m := range batch {
				out[i] = m
			}
			return out
		}
	}
	return [][]byte{data}
}

// jsonTee hands a JSON body to the observers once the SDK read all of it.
type jsonTee struct {
	body io.ReadCloser
	in   inbound
	buf  bytes.Buffer
	done bool
}

func (j *jsonTee) Read(p []byte) (int, error) {
	n, err := j.body.Read(p)
	j.buf.Write(p[:n])
	if err == io.EOF && !j.done {
		j.done = true
		j.in.observe(j.buf.Bytes())
	}
	return n, err
}

func (j *jsonTee) Close() error { return j.body.Close() }

// sseTee hands each server-sent message event's data to the observers as
// the SDK reads the stream.
type sseTee struct {
	body      io.ReadCloser
	in        inbound
	line      []byte
	eventType string
	event     bytes.Buffer
}

func (s *sseTee) Read(p []byte) (int, error) {
	n, err := s.body.Read(p)
	s.scan(p[:n])
	return n, err
}

func (s *sseTee) Close() error { return s.body.Close() }

// scan feeds chunk through the SSE line grammar: data lines accumulate into
// the event, a blank line dispatches it, other fields are ignored.
func (s *sseTee) scan(chunk []byte) {
	for len(chunk) > 0 {
		i := bytes.IndexByte(chunk, '\n')
		if i < 0 {
			s.line = append(s.line, chunk...)
			return
		}
		s.line = append(s.line, chunk[:i]...)
		chunk = chunk[i+1:]
		s.endLine(bytes.TrimSuffix(s.line, []byte("\r")))
		s.line = s.line[:0]
	}
}

func (s *sseTee) endLine(line []byte) {
	if len(line) == 0 {
		// Only message events carry JSON-RPC; the legacy SSE transport's
		// first event, endpoint, carries a URL.
		if s.event.Len() > 0 && (s.eventType == "" || s.eventType == "message") {
			s.in.observe(s.event.Bytes())
		}
		s.event.Reset()
		s.eventType = ""
		return
	}
	if name, ok := bytes.CutPrefix(line, []byte("event:")); ok {
		s.eventType = string(bytes.TrimPrefix(name, []byte(" ")))
		return
	}
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return
	}
	if s.event.Len() > 0 {
		s.event.WriteByte('\n')
	}
	s.event.Write(bytes.TrimPrefix(data, []byte(" ")))
}
