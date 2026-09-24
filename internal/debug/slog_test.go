package debug

import (
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"testing"
)

func TestSlogHandler_ForwardsLevelComponentAttrsAndGroups(t *testing.T) {
	read, stop := Capture(LogLevelDebug)
	defer stop()

	logger := slog.New(NewSlogHandler("sdk")).With("session", "s-19").WithGroup("rpc")
	logger.Warn("keepalive ping failed", "method", "ping", slog.Group("peer", "addr", "127.0.0.1:7400"))

	out := read()
	for _, want := range []string{"WARN [sdk] keepalive ping failed", "session=s-19", "rpc.method=ping", "rpc.peer.addr=127.0.0.1:7400"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSlogHandler_MapsEverySlogLevel(t *testing.T) {
	read, stop := Capture(LogLevelDebug)
	defer stop()

	logger := slog.New(NewSlogHandler("sdk"))
	logger.Debug("d-line")
	logger.Info("i-line")
	logger.Warn("w-line")
	logger.Error("e-line")

	out := read()
	for _, want := range []string{"DEBUG [sdk] d-line", "INFO [sdk] i-line", "WARN [sdk] w-line", "ERROR [sdk] e-line"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSlogHandler_EnabledFollowsGlobalLevel(t *testing.T) {
	_, stop := Capture(LogLevelWarn)
	defer stop()

	h := NewSlogHandler("sdk")
	if h.Enabled(t.Context(), slog.LevelInfo) {
		t.Error("Info enabled while global level is Warn")
	}
	if !h.Enabled(t.Context(), slog.LevelError) {
		t.Error("Error disabled while global level is Warn")
	}
}

func TestSlogHandler_RedactsCredentialAttrsAndErrorURLs(t *testing.T) {
	read, stop := Capture(LogLevelDebug)
	defer stop()

	urlErr := &url.Error{Op: "Post", URL: "https://as.example/token?code=ac-66d0e1", Err: errors.New("EOF")}
	slog.New(NewSlogHandler("sdk")).Error("token exchange failed",
		"refresh_token", "rt-a91c2f", "error", urlErr)

	out := read()
	for _, secret := range []string{"rt-a91c2f", "ac-66d0e1"} {
		if strings.Contains(out, secret) {
			t.Errorf("SDK log leaked %q:\n%s", secret, out)
		}
	}
}
