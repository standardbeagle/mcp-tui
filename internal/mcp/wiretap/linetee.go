package wiretap

import (
	"bytes"
	"io"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerOutput is a transport that can show the tap the raw bytes its server
// writes: stdio, where the SDK's decoder fails the connection on a message
// that is not JSON-RPC 2.0 before the tap sees any message. newWriter is
// called once per connection for the writer to copy that connection's
// output to.
type ServerOutput interface {
	TeeServerOutput(newWriter func() io.Writer)
}

// lineTee reads a stdio server's output as the MCP stdio framing defines
// it, one message per line, and reports each line that does not decode as
// JSON-RPC 2.0. Lines that do reach the observers through the connection.
type lineTee struct {
	tap      *Tap
	line     []byte
	overflow bool // the line outgrew the SDK's frame limit, which fails it
}

func (l *lineTee) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		chunk := p
		if i >= 0 {
			chunk = p[:i]
		}
		if !l.overflow {
			if len(l.line)+len(chunk) > officialMCP.DefaultMaxLineLength {
				l.overflow, l.line = true, l.line[:0]
			} else {
				l.line = append(l.line, chunk...)
			}
		}
		if i < 0 {
			break
		}
		if !l.overflow {
			l.checkLine()
		}
		l.line, l.overflow = l.line[:0], false
		p = p[i+1:]
	}
	return n, nil
}

func (l *lineTee) checkLine() {
	for _, raw := range splitBatch(l.line) {
		if _, err := jsonrpc.DecodeMessage(raw); err != nil {
			l.tap.malformed(bytes.Clone(raw), err)
		}
	}
}
