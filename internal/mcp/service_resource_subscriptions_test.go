package mcp

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	officialMCP "github.com/modelcontextprotocol/go-sdk/mcp"

	configPkg "github.com/standardbeagle/mcp-tui/internal/config"
	"github.com/standardbeagle/mcp-tui/internal/mcp/notifications"
)

const (
	buildLogURI  = "file:///var/log/ci/build-4711.log"
	ciServerName = "ci-server"
	plainText    = "text/plain"
)

// newBuildLogServer serves one resource, a CI build log. When subscribable
// it declares resources.subscribe and reports each unsubscribe on
// unsubscribed.
func newBuildLogServer(subscribable bool, unsubscribed chan<- string) *officialMCP.Server {
	var opts *officialMCP.ServerOptions
	if subscribable {
		opts = &officialMCP.ServerOptions{
			SubscribeHandler: func(context.Context, *officialMCP.SubscribeRequest) error { return nil },
			UnsubscribeHandler: func(_ context.Context, req *officialMCP.UnsubscribeRequest) error {
				unsubscribed <- req.Params.URI
				return nil
			},
		}
	}
	server := officialMCP.NewServer(&officialMCP.Implementation{Name: ciServerName, Version: "3.1.0"}, opts)
	server.AddResource(&officialMCP.Resource{URI: buildLogURI, Name: "build-4711.log", MIMEType: plainText},
		func(context.Context, *officialMCP.ReadResourceRequest) (*officialMCP.ReadResourceResult, error) {
			return &officialMCP.ReadResourceResult{Contents: []*officialMCP.ResourceContents{
				{URI: buildLogURI, MIMEType: plainText, Text: "step 3/7: go test ./..."},
			}}, nil
		})
	return server
}

func negotiatedOrLatest(pinned string) string {
	if pinned == "" {
		return officialMCP.SupportedProtocolVersions()[0]
	}
	return pinned
}

// TestService_ResourceSubscription_DeliversUpdatesUntilUnsubscribed: on
// 2026-07-28 Subscribe opens a per-URI subscriptions/listen stream
// (SEP-2575), before it sends resources/subscribe. Either way the service
// reports the URI as subscribed, routes notifications/resources/updated into
// the notification stream, and Unsubscribe releases the server-side
// subscription.
func TestService_ResourceSubscription_DeliversUpdatesUntilUnsubscribed(t *testing.T) {
	for _, pinned := range []string{"", legacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			unsubscribed := make(chan string, 1)
			server := newBuildLogServer(true, unsubscribed)
			svc := NewService().(*service)
			updated := make(chan string, 4)
			svc.AddNotificationObserver(func(e notifications.Entry) {
				if e.Type == notifications.TypeResourcesUpdated {
					updated <- e.Raw.(*officialMCP.ResourceUpdatedNotificationParams).URI
				}
			})
			connectInMemory(t, server, svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: ciServerName, ProtocolVersion: pinned,
			})
			if got, want := svc.GetServerInfo().ProtocolVersion, negotiatedOrLatest(pinned); got != want {
				t.Fatalf("negotiated %q, want %q", got, want)
			}

			ctx := context.Background()
			if err := svc.SubscribeResource(ctx, buildLogURI); err != nil {
				t.Fatalf("SubscribeResource: %v", err)
			}
			if got := svc.ResourceSubscriptions(); !slices.Equal(got, []string{buildLogURI}) {
				t.Errorf("ResourceSubscriptions = %v, want [%s]", got, buildLogURI)
			}
			if err := server.ResourceUpdated(ctx, &officialMCP.ResourceUpdatedNotificationParams{URI: buildLogURI}); err != nil {
				t.Fatal(err)
			}
			select {
			case uri := <-updated:
				if uri != buildLogURI {
					t.Errorf("update for %q, want %q", uri, buildLogURI)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no notifications/resources/updated after subscribing")
			}

			if err := svc.UnsubscribeResource(ctx, buildLogURI); err != nil {
				t.Fatalf("UnsubscribeResource: %v", err)
			}
			if got := svc.ResourceSubscriptions(); len(got) != 0 {
				t.Errorf("ResourceSubscriptions after unsubscribe = %v, want none", got)
			}
			select {
			case uri := <-unsubscribed:
				if uri != buildLogURI {
					t.Errorf("server unsubscribed %q, want %q", uri, buildLogURI)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("server never saw the unsubscribe")
			}
		})
	}
}

// TestService_ResourceSubscription_RejectedWithoutServerCapability: a server
// that does not declare resources.subscribe would silently never notify (on
// 2026-07-28 it acknowledges the listen stream without the URI), so the
// service refuses up front with a named error.
func TestService_ResourceSubscription_RejectedWithoutServerCapability(t *testing.T) {
	for _, pinned := range []string{"", legacyProtocolVersion} {
		t.Run("pin="+pinned, func(t *testing.T) {
			svc := NewService().(*service)
			connectInMemory(t, newBuildLogServer(false, nil), svc, &configPkg.ConnectionConfig{
				Type: configPkg.TransportStdio, Command: ciServerName, ProtocolVersion: pinned,
			})
			if got, want := svc.GetServerInfo().ProtocolVersion, negotiatedOrLatest(pinned); got != want {
				t.Fatalf("negotiated %q, want %q", got, want)
			}
			err := svc.SubscribeResource(context.Background(), buildLogURI)
			if !errors.Is(err, ErrResourceSubscribeUnsupported) {
				t.Fatalf("SubscribeResource error = %v, want ErrResourceSubscribeUnsupported", err)
			}
			if got := svc.ResourceSubscriptions(); len(got) != 0 {
				t.Errorf("ResourceSubscriptions = %v, want none", got)
			}
		})
	}
}
