package errors

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStdoutNotJSONRPCErrorTellsLogLineFromBrokenMessage(t *testing.T) {
	logLine := (&StdoutNotJSONRPCError{Command: "node", Line: "Server listening on stdio"}).Error()
	assert.Contains(t, logLine, "stderr")

	broken := (&StdoutNotJSONRPCError{Command: "node", Line: `{"jsonrpc": "2.0", "id": 1, "result": undefined}`}).Error()
	assert.Contains(t, broken, "malformed JSON-RPC message")
	assert.NotContains(t, broken, "stderr")
}
