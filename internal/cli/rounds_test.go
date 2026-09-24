package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

var deployRound = []mcp.RoundSummary{{
	Round: 1, Method: "tools/call", HasRequestState: true, DurationMs: 3.2,
	InputRequests: []mcp.InputExchange{{Key: "confirm", Kind: "elicitation", Response: "accept"}},
}}

// TestWriteRoundTrace_Silent keeps single-round calls free of an empty
// round section.
func TestWriteRoundTrace_Silent(t *testing.T) {
	var buf bytes.Buffer
	writeRoundTrace(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("writeRoundTrace(nil) wrote %q, want nothing", buf.String())
	}
}

// TestWriteRoundTrace_ListsRounds pins the section header and one line per
// round and input request.
func TestWriteRoundTrace_ListsRounds(t *testing.T) {
	var buf bytes.Buffer
	writeRoundTrace(&buf, deployRound)
	want := "\nInput rounds (SEP-2322):\n" +
		"  round 1 · tools/call · 3.2ms · request state: yes\n" +
		"    confirm: elicitation → accept\n"
	if buf.String() != want {
		t.Errorf("writeRoundTrace =\n%q\nwant\n%q", buf.String(), want)
	}
}

// TestResourceReadOutput_Rounds pins the `resource read --format json`
// shape: rounds appear only when the read took input rounds, like the
// omitempty rounds field of tool and prompt results.
func TestResourceReadOutput_Rounds(t *testing.T) {
	contents := []mcp.ResourceContents{{URI: "file:///var/log/deploy.log", Text: "deploy ok"}}

	plain, err := json.Marshal(resourceReadOutput("file:///var/log/deploy.log", &mcp.ReadResourceResult{Contents: contents}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(plain), `"rounds"`) {
		t.Errorf("single-round read JSON = %s, want no rounds field", plain)
	}

	withRounds, err := json.Marshal(resourceReadOutput("file:///var/log/deploy.log",
		&mcp.ReadResourceResult{Contents: contents, Rounds: deployRound}))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Count  int                `json:"count"`
		Rounds []mcp.RoundSummary `json:"rounds"`
	}
	if err := json.Unmarshal(withRounds, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Count != 1 || len(decoded.Rounds) != 1 || decoded.Rounds[0].InputRequests[0].Key != "confirm" {
		t.Errorf("multi-round read JSON = %s, want count 1 and the confirm round", withRounds)
	}
}
