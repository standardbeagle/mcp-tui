package transports

import (
	"strings"
	"testing"
)

func TestFirstNonJSONLine(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		wantLine string
		wantOK   bool
	}{
		{"banner before the first message", "Acme support desk listening on stdio\n{\"jsonrpc\":\"2.0\"}\n", "Acme support desk listening on stdio", true},
		{"malformed response", "{\"jsonrpc\": \"2.0\", \"id\": 1, \"result\": {\n", `{"jsonrpc": "2.0", "id": 1, "result": {`, true},
		{"only JSON-RPC messages", "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n\n", "", false},
		{"line still being written", "{\"jsonrpc\":\"2.0\",\"id\":1,", "", false},
		{"nothing written", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, ok := firstNonJSONLine([]byte(tt.stdout))
			if line != tt.wantLine || ok != tt.wantOK {
				t.Errorf("firstNonJSONLine() = %q, %v; want %q, %v", line, ok, tt.wantLine, tt.wantOK)
			}
		})
	}
}

func TestFirstNonJSONLineShortensLongLines(t *testing.T) {
	line, ok := firstNonJSONLine([]byte(strings.Repeat("x", 500) + "\n"))
	if !ok || len([]rune(line)) != maxQuotedLineLength+1 || !strings.HasSuffix(line, "…") {
		t.Errorf("firstNonJSONLine() = %d runes, %v; want %d runes ending in an ellipsis", len([]rune(line)), ok, maxQuotedLineLength+1)
	}
}

func TestStdoutPrefixKeepsOnlyTheStart(t *testing.T) {
	var p stdoutPrefix
	chunk := []byte(strings.Repeat("a", stdoutPrefixLimit-10))
	if n, err := p.Write(chunk); n != len(chunk) || err != nil {
		t.Fatalf("Write() = %d, %v", n, err)
	}
	if n, err := p.Write([]byte(strings.Repeat("b", 100))); n != 100 || err != nil {
		t.Fatalf("Write() past the limit = %d, %v; want every byte reported written", n, err)
	}
	if got := len(p.Bytes()); got != stdoutPrefixLimit {
		t.Errorf("kept %d bytes, want %d", got, stdoutPrefixLimit)
	}
}
