package transports

import (
	"reflect"
	"testing"
	"time"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
)

func TestFromConnectionConfigPreservesHeadersAndEnvironment(t *testing.T) {
	conn := &configPkg.ConnectionConfig{
		Type:    configPkg.TransportStdio,
		Command: "node",
		Args:    []string{"server.js"},
		Headers: map[string]string{
			"Authorization": "Bearer token",
		},
		Environment: map[string]string{
			"API_KEY": "secret",
		},
		MCPMethodHeaders: true,
	}

	got := FromConnectionConfig(conn, true, 5*time.Second)

	if !reflect.DeepEqual(got.StaticHeaders, conn.Headers) {
		t.Fatalf("StaticHeaders = %#v; want %#v", got.StaticHeaders, conn.Headers)
	}
	if !reflect.DeepEqual(got.Environment, conn.Environment) {
		t.Fatalf("Environment = %#v; want %#v", got.Environment, conn.Environment)
	}
	if !got.MCPMethodHeaders {
		t.Fatalf("MCPMethodHeaders = false; want true")
	}
}
