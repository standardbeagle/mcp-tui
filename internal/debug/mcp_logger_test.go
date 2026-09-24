package debug

import (
	"fmt"
	"strings"
	"sync"
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

// Connections first log to the Messages tab concurrently; they must all get
// the same logger, without a data race on its lazy creation.
func TestGetMCPLogger_ConcurrentFirstUse(t *testing.T) {
	prev := globalMCPLogger.Swap(nil)
	t.Cleanup(func() { globalMCPLogger.Store(prev) })

	const callers = 16
	got := make(chan *MCPLogger, callers)
	var start sync.WaitGroup
	start.Add(1)
	for range callers {
		go func() {
			start.Wait()
			got <- GetMCPLogger()
		}()
	}
	start.Done()
	first := <-got
	for range callers - 1 {
		if l := <-got; l != first {
			t.Fatal("concurrent first calls created different loggers; messages logged to the others are lost")
		}
	}
}
