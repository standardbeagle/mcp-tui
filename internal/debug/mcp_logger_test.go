package debug

import (
	"fmt"
	"strings"
	"testing"
)

// TestMCPLogger_RedactsURLsInPayloads: the MCP Messages tab shows each
// entry's raw JSON and parsed params/result verbatim, so a credential inside
// a URL must be masked before the entry is stored, while ordinary argument
// values (even under names like "state") stay readable.
func TestMCPLogger_RedactsURLsInPayloads(t *testing.T) {
	const secret = "dev-code-SECRET"
	ml := NewMCPLogger(10)
	ml.LogOutgoing(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"open","arguments":{"state":"draft","url":"https://h.example/cb?code=`+secret+`"}}}`, nil)
	ml.LogIncoming(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"visit https://h.example/d?code=`+secret+`"}]}}`, nil)
	ml.LogIncoming(`not json https://h.example/d?code=`+secret, nil)

	for _, e := range ml.GetEntries() {
		shown := e.RawMessage + e.GetFormattedJSON() + fmt.Sprintf("%v%v", e.Params, e.Result)
		if strings.Contains(shown, secret) {
			t.Errorf("entry leaks the code: %s", shown)
		}
	}
	if !strings.Contains(ml.GetEntries()[0].RawMessage, `"state":"draft"`) {
		t.Errorf("ordinary argument state was masked: %s", ml.GetEntries()[0].RawMessage)
	}
}
