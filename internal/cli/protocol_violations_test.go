package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp"
	mcperrors "github.com/standardbeagle/mcp-tui/internal/mcp/errors"
	"github.com/standardbeagle/mcp-tui/internal/mcp/protocolwatch"
	"github.com/standardbeagle/mcp-tui/internal/testutil"
)

// connectOutOfOrderServer connects to test-servers/out-of-order-server.js,
// which sends a bare "initialized" notification before its initialize
// response and a tools/list response under an id no request carries.
func connectOutOfOrderServer(t *testing.T) mcp.Service {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("node is required for the out-of-order server: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "test-servers", "out-of-order-server.js"))
	if err != nil {
		t.Fatal(err)
	}
	svc := mcp.NewService()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := svc.Connect(ctx, &config.ConnectionConfig{
		Type: config.TransportStdio, Command: node, Args: []string{script}, ProtocolVersion: "2025-11-25",
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	return svc
}

// Text mode names each violation on stderr once the command is done:
// after the output, whether or not the command succeeded.
func TestCloseClient_ReportsProtocolViolations(t *testing.T) {
	tc := NewToolCommand()
	tc.service = connectOutOfOrderServer(t)
	stderr := captureStderr(t, func() {
		if err := tc.CloseClient(); err != nil {
			t.Errorf("CloseClient: %v", err)
		}
	})
	want := `⚠ protocol: server sent notification "initialized", which MCP does not define (did you mean notifications/initialized?)`
	if !strings.Contains(stderr, want+"\n") {
		t.Errorf("stderr = %q, want the line %q", stderr, want)
	}
}

// A stdio banner fails the handshake with an error that quotes the line; the
// watcher's finding for that same line would only repeat it. Findings about
// other messages, and the finding when the error is about something else,
// are still reported.
func TestReportableProtocolViolations_DropsTheLineTheErrorQuotes(t *testing.T) {
	const banner = "Acme support desk listening on stdio"
	bannerFinding := protocolwatch.Violation{Kind: protocolwatch.KindMalformed,
		Message: "server sent a message that is not JSON-RPC 2.0: it is not JSON", Raw: banner}
	otherLine := protocolwatch.Violation{Kind: protocolwatch.KindMalformed,
		Message: "server sent a message that is not JSON-RPC 2.0: it is not JSON", Raw: "Loaded 42 tickets"}
	undefined := protocolwatch.Violation{Kind: protocolwatch.KindUndefinedMethod, Method: "initialized",
		Message: `server sent notification "initialized", which MCP does not define`}
	all := []protocolwatch.Violation{bannerFinding, otherLine, undefined}
	bannerErr := fmt.Errorf("failed to connect to MCP server: %w",
		&mcperrors.StdoutNotJSONRPCError{Command: "acme-desk", Line: banner})

	got := reportableProtocolViolations(all, bannerErr)
	if want := []protocolwatch.Violation{otherLine, undefined}; !slices.Equal(got, want) {
		t.Errorf("with the banner error: got %+v, want %+v", got, want)
	}
	if got := reportableProtocolViolations(all, fmt.Errorf("tool %q not found on the server", "close_ticket")); !slices.Equal(got, all) {
		t.Errorf("with an unrelated error: got %+v, want all %+v", got, all)
	}
	if got := reportableProtocolViolations(all, nil); !slices.Equal(got, all) {
		t.Errorf("without an error: got %+v, want all %+v", got, all)
	}
}

// The error quotes at most a shortened line, the finding a differently
// shortened one; a long banner is still recognized as the same line.
func TestReportableProtocolViolations_MatchesShortenedLines(t *testing.T) {
	long := strings.Repeat("Acme support desk: loading plugin ", 20)
	finding := protocolwatch.Violation{Kind: protocolwatch.KindMalformed, Raw: long[:300] + "…"}
	err := &mcperrors.StdoutNotJSONRPCError{Command: "acme-desk", Line: long[:200] + "…"}
	if got := reportableProtocolViolations([]protocolwatch.Violation{finding}, err); len(got) != 0 {
		t.Errorf("got %+v, want the finding dropped", got)
	}
}

func TestCloseClient_QuietInPorcelainAndJSON(t *testing.T) {
	for _, tt := range []struct {
		name      string
		porcelain bool
		format    OutputFormat
	}{
		{"porcelain", true, OutputFormatText},
		{"json", false, OutputFormatJSON},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tc := NewToolCommand()
			tc.service = connectOutOfOrderServer(t)
			tc.porcelain, tc.outputFormat = tt.porcelain, tt.format
			stderr := captureStderr(t, func() {
				if err := tc.CloseClient(); err != nil {
					t.Errorf("CloseClient: %v", err)
				}
			})
			if strings.Contains(stderr, "protocol:") {
				t.Errorf("stderr = %q, want no protocol report", stderr)
			}
		})
	}
}

// serveStrayMessages serves a 2025-11-25 streamable HTTP server that
// answers tools/list on an SSE stream carrying, before the real response,
// a bare "initialized" notification and a response under an id no request
// carries.
func serveStrayMessages(t *testing.T) string {
	t.Helper()
	return testutil.ServeStreamableHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		msg, err := jsonrpc.DecodeMessage(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req, ok := msg.(*jsonrpc.Request)
		if !ok || !req.IsCall() {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		id, err := json.Marshal(req.ID.Raw())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25","capabilities":{"tools":{}},"serverInfo":{"name":"inventory","version":"2.0.1"}}}`, id)
		case "tools/list":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"initialized\",\"params\":{}}\n\n"+
				"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":9%s,\"result\":{\"tools\":[]}}\n\n"+
				"event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"tools\":[{\"name\":\"count_stock\",\"inputSchema\":{\"type\":\"object\"}}]}}\n\n", id, id)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"error":{"code":-32601,"message":"Method not found"}}`, id)
		}
	}))
}

// JSON output carries the violations in its object envelope.
func TestToolList_JSONCarriesProtocolViolations(t *testing.T) {
	svc := mcp.NewService()
	if err := svc.Connect(context.Background(), &config.ConnectionConfig{
		Type: config.TransportStreamableHTTP, URL: serveStrayMessages(t), ProtocolVersion: "2025-11-25",
	}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = svc.Disconnect() })
	stdout, _ := runToolList(t, svc, "--format", "json")
	var doc struct {
		ProtocolViolations []protocolwatch.Violation `json:"protocolViolations"`
	}
	if err := json.Unmarshal([]byte(stdout), &doc); err != nil {
		t.Fatalf("json output: %v\n%s", err, stdout)
	}
	kinds := map[protocolwatch.Kind]bool{}
	for _, v := range doc.ProtocolViolations {
		kinds[v.Kind] = true
	}
	if !kinds[protocolwatch.KindUndefinedMethod] || !kinds[protocolwatch.KindUnknownID] {
		t.Errorf("protocolViolations = %+v, want the undefined notification and the unknown id", doc.ProtocolViolations)
	}
}
