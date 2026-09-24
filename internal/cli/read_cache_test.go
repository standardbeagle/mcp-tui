package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/standardbeagle/mcp-tui/internal/mcp"
)

var deployLogRead = &mcp.ReadResourceResult{
	Contents: []mcp.ResourceContents{{URI: "file:///var/log/deploy.log", Text: "deployed v2.4.0"}},
	Cache:    &mcp.ReadCacheInfo{TTLMs: 60_000, CacheScope: "private", FromCache: true},
}

// TestWriteReadCache pins the `resource get` text line saying how the SDK
// served the read, and that reads without cache info print nothing.
func TestWriteReadCache(t *testing.T) {
	var buf bytes.Buffer
	writeReadCache(&buf, deployLogRead.Cache)
	if want := "\nCache: cached · ttl 1m0s · private\n"; buf.String() != want {
		t.Errorf("writeReadCache = %q, want %q", buf.String(), want)
	}
	buf.Reset()
	writeReadCache(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("writeReadCache(nil) = %q, want nothing", buf.String())
	}
}

// TestResourceReadOutput_Cache pins that `resource get --format json`
// carries the cache state when the session has one, and omits it otherwise.
func TestResourceReadOutput_Cache(t *testing.T) {
	doc, err := json.Marshal(resourceReadOutput("file:///var/log/deploy.log", deployLogRead))
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Cache *mcp.ReadCacheInfo `json:"cache"`
	}
	if err = json.Unmarshal(doc, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Cache == nil || *decoded.Cache != *deployLogRead.Cache {
		t.Errorf("JSON = %s, want cache %+v", doc, *deployLogRead.Cache)
	}
	legacy, err := json.Marshal(resourceReadOutput("file:///var/log/deploy.log",
		&mcp.ReadResourceResult{Contents: deployLogRead.Contents}))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacy), `"cache"`) {
		t.Errorf("JSON without cache info = %s, want no cache field", legacy)
	}
}
