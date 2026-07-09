package errors

import (
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Transports wrap their failures before they reach the classifier. A bare type
// assertion on the outermost error misses every wrapped one, which pushed real
// connection failures into CategoryUnknown -- marking them unrecoverable and
// silently disabling session reconnection.
func TestClassifyWrappedNetworkErrors(t *testing.T) {
	tests := []struct {
		name            string
		err             error
		wantCategory    ErrorCategory
		wantRecoverable bool
	}{
		{
			name:            "wrapped ECONNREFUSED",
			err:             fmt.Errorf("dialing server: %w", syscall.ECONNREFUSED),
			wantCategory:    CategoryConnection,
			wantRecoverable: true,
		},
		{
			name:            "wrapped ECONNRESET",
			err:             fmt.Errorf("reading response: %w", syscall.ECONNRESET),
			wantCategory:    CategoryConnection,
			wantRecoverable: true,
		},
		{
			name:            "wrapped EPIPE",
			err:             fmt.Errorf("writing request: %w", syscall.EPIPE),
			wantCategory:    CategoryTransport,
			wantRecoverable: true, // transport errors mentioning a pipe are retried
		},
		{
			name:            "wrapped net.OpError dial",
			err:             fmt.Errorf("connect: %w", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}),
			wantCategory:    CategoryConnection,
			wantRecoverable: true,
		},
		{
			name:            "wrapped DNS not found",
			err:             fmt.Errorf("resolve: %w", &net.DNSError{Err: "no such host", IsNotFound: true}),
			wantCategory:    CategoryConnection,
			wantRecoverable: true,
		},
		{
			name:            "plain-string connection reset",
			err:             fmt.Errorf("read tcp: connection reset by peer"),
			wantCategory:    CategoryConnection,
			wantRecoverable: true,
		},
		{
			name:            "plain-string connection refused",
			err:             fmt.Errorf("connection refused"),
			wantCategory:    CategoryConnection,
			wantRecoverable: true,
		},
	}

	classifier := NewErrorClassifier()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			category, _ := classifier.analyzeError(tt.err)
			assert.Equal(t, tt.wantCategory, category, "category for %v", tt.err)
			assert.Equal(t, tt.wantRecoverable, classifier.isRecoverable(tt.err, category),
				"a wrapped network failure must stay recoverable so reconnection runs")
		})
	}
}

// A stdio server that exits during the MCP handshake reports "connection
// closed" / "broken pipe". Those must not be mistaken for transient network
// faults: retrying a misconfigured server is pointless, and the user needs the
// protocol failure reported instead.
func TestServerExitDuringHandshakeIsNotATransientConnectionError(t *testing.T) {
	classifier := NewErrorClassifier()

	for _, msg := range []string{
		"connection closed",
		"broken pipe",
	} {
		category, _ := classifier.analyzeError(fmt.Errorf("%s", msg))
		assert.NotEqual(t, CategoryConnection, category,
			"%q must not be classified as a transient connection fault", msg)
	}
}

// Errors that need user intervention must not be retried.
func TestNonRecoverableCategoriesStayNonRecoverable(t *testing.T) {
	classifier := NewErrorClassifier()

	for _, category := range []ErrorCategory{
		CategoryAuthentication,
		CategoryClientConfig,
		CategoryValidation,
		CategoryProtocol,
		CategorySerialization,
		CategoryServerStartup,
	} {
		assert.False(t, classifier.isRecoverable(fmt.Errorf("boom"), category),
			"category %s must not be retried", category)
	}
}
