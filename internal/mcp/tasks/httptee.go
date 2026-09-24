package tasks

import (
	"bytes"
	"io"
	"mime"
	"net/http"
)

// teeRoundTripper serves the link on HTTP transports, whose connection the
// link cannot wrap: it stamps Mcp-Name on requests the link routes, and lets
// the link observe every JSON-RPC message in a response body as the SDK
// reads it. It never changes a byte the SDK sees.
type teeRoundTripper struct {
	base http.RoundTripper
	link *Link
}

func (t *teeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if name := routingName(req.Context()); name != "" && req.Header.Get("Mcp-Name") == "" {
		req = req.Clone(req.Context())
		req.Header.Set("Mcp-Name", name)
	}
	resp, err := t.base.RoundTrip(req)
	if err != nil || resp.Body == nil {
		return resp, err
	}
	mediaType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch mediaType {
	case "application/json":
		resp.Body = &jsonTee{body: resp.Body, link: t.link}
	case "text/event-stream":
		resp.Body = &sseTee{body: resp.Body, link: t.link}
	}
	return resp, nil
}

// jsonTee hands a JSON body to the link once the SDK read all of it.
type jsonTee struct {
	body io.ReadCloser
	link *Link
	buf  bytes.Buffer
	done bool
}

func (j *jsonTee) Read(p []byte) (int, error) {
	n, err := j.body.Read(p)
	j.buf.Write(p[:n])
	if err == io.EOF && !j.done {
		j.done = true
		j.link.observeBatch(j.buf.Bytes())
	}
	return n, err
}

func (j *jsonTee) Close() error { return j.body.Close() }

// sseTee hands each server-sent event's data to the link as the SDK reads
// the stream.
type sseTee struct {
	body  io.ReadCloser
	link  *Link
	line  []byte
	event bytes.Buffer
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
		if s.event.Len() > 0 {
			s.link.observeBatch(s.event.Bytes())
			s.event.Reset()
		}
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
