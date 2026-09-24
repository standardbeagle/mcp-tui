package debug

import "testing"

// TestProtocolErrorCode pins the names given to the JSON-RPC error codes
// MCP defines or re-purposes, including the 2026-07-28 SEP-2575 codes and
// resource-not-found's move from -32002 to -32602 (SEP-2164).
func TestProtocolErrorCode(t *testing.T) {
	for _, tc := range []struct {
		code   int64
		method string
		want   ErrorCode
	}{
		{-32602, "resources/read", ErrorCodeResourceNotFound},
		{-32002, "resources/read", ErrorCodeResourceNotFound},
		{-32602, "tools/call", ErrorCodeInvalidParams},
		{-32020, "tools/call", ErrorCodeHeaderMismatch},
		{-32021, "tools/list", ErrorCodeMissingClientCapabilities},
		{-32022, "server/discover", ErrorCodeUnsupportedVersion},
		{-32042, "tools/call", ErrorCodeURLElicitationRequired},
		{-32603, "tools/call", ""},
	} {
		if got := ProtocolErrorCode(tc.code, tc.method); got != tc.want {
			t.Errorf("ProtocolErrorCode(%d, %s) = %q, want %q", tc.code, tc.method, got, tc.want)
		}
	}
}
