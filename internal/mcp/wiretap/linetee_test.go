package wiretap

import (
	"context"
	"io"
	"slices"
	"strings"
	"testing"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"
)

// rawStdio stands in for the stdio transport: a server whose stdout the
// test writes raw, with the tap's tee on it.
type rawStdio struct {
	stdout    io.Reader
	newWriter func() io.Writer
}

func (r *rawStdio) TeeServerOutput(newWriter func() io.Writer) { r.newWriter = newWriter }

func (r *rawStdio) Connect(ctx context.Context) (officialMCP.Connection, error) {
	t := &officialMCP.IOTransport{Reader: io.NopCloser(io.TeeReader(r.stdout, r.newWriter())), Writer: nopWriteCloser{}}
	return t.Connect(ctx)
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }

// On stdio the SDK's decoder fails the connection on a line that is not
// JSON-RPC 2.0 before the tap sees a message, so the tap reads the server's
// raw output line by line and reports each such line.
func TestTap_StdioLinesThatAreNotJSONRPC(t *testing.T) {
	rec := &recorder{}
	stdout := strings.NewReader("{\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n" +
		"\r\n" +
		"Weather server listening on stdio\n" +
		"{\"jsonrpc\":\"2.0\",\"result\":{}}\n")
	conn, err := New(rec).Transport(&rawStdio{stdout: stdout}).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for {
		if _, err := conn.Read(context.Background()); err != nil {
			break
		}
	}
	// The tee sees bytes as the SDK's decoder reads them, which may be
	// ahead of the messages it hands out, so only the set is fixed.
	want := []string{
		"connected",
		"malformed Weather server listening on stdio",
		`malformed {"jsonrpc":"2.0","result":{}}`,
		"received notifications/message",
	}
	got := rec.seen()
	slices.Sort(got)
	slices.Sort(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("observer saw %q, want %q", got, want)
	}
}

// A line split across writes is judged once it is complete.
func TestLineTee_JudgesCompleteLines(t *testing.T) {
	rec := &recorder{}
	tee := &lineTee{tap: New(rec)}
	for _, chunk := range []string{`{"id":4,`, `"result":{}}`, "\n{\"jsonrpc\":\"2.0\",\"id\":5,\"result\":{}}\n", `{"partial":`} {
		if n, err := tee.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	want := []string{`malformed {"id":4,"result":{}}`}
	if got := rec.seen(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("observer saw %q, want %q", got, want)
	}
}
