package oauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimeoutExcludingSignIn_ExpiresLikeATimeout(t *testing.T) {
	ctx, cancel := WithTimeoutExcludingSignIn(context.Background(), 30*time.Millisecond)
	defer cancel()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("the timeout never fired")
	}
	assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded, "callers classify an expired connect by DeadlineExceeded")
}

// Time the user spends signing in does not count against the timeout; the
// time before and after it does.
func TestTimeoutExcludingSignIn_PausesDuringSignIn(t *testing.T) {
	ctx, cancel := WithTimeoutExcludingSignIn(context.Background(), 60*time.Millisecond)
	defer cancel()
	clock, ok := ctx.Value(signInClockKey{}).(*signInClock)
	require.True(t, ok, "the context must carry the sign-in clock")

	clock.signInStarted()
	time.Sleep(150 * time.Millisecond)
	assert.NoError(t, ctx.Err(), "the sign-in outlasted the timeout but must not expire it")
	clock.signInFinished()

	select {
	case <-ctx.Done():
		assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	case <-time.After(time.Second):
		t.Fatal("the timeout did not resume after the sign-in")
	}
}

func TestTimeoutExcludingSignIn_FollowsParentAndCancel(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	ctx, cancel := WithTimeoutExcludingSignIn(parent, time.Minute)
	defer cancel()
	cancelParent()
	<-ctx.Done()
	assert.ErrorIs(t, ctx.Err(), context.Canceled)

	ctx2, cancel2 := WithTimeoutExcludingSignIn(context.Background(), time.Minute)
	cancel2()
	<-ctx2.Done()
	assert.ErrorIs(t, ctx2.Err(), context.Canceled)
}

// A context derived from it (as the SDK derives request contexts) still
// finds the clock and inherits the expiry.
func TestTimeoutExcludingSignIn_DerivedContexts(t *testing.T) {
	ctx, cancel := WithTimeoutExcludingSignIn(context.Background(), 30*time.Millisecond)
	defer cancel()
	derived, cancelDerived := context.WithCancel(ctx)
	defer cancelDerived()
	_, ok := derived.Value(signInClockKey{}).(*signInClock)
	assert.True(t, ok)
	<-derived.Done()
	assert.True(t, errors.Is(derived.Err(), context.DeadlineExceeded))
}

// The fetcher stops the clock while it waits for the browser redirect, so
// a sign-in longer than --timeout completes.
func TestLocalServerFetcher_SignInDoesNotCountAgainstTheTimeout(t *testing.T) {
	ctx, cancel := WithTimeoutExcludingSignIn(context.Background(), 50*time.Millisecond)
	defer cancel()
	f := newLocalServerFetcher("127.0.0.1", 0, nil)
	redirectURL := f.RedirectURL()
	require.NotEmpty(t, redirectURL)
	f.browserOpener = func(string) error {
		go func() {
			time.Sleep(200 * time.Millisecond) // the user types a password
			if resp, err := getCallback(redirectURL, "code-7f3a", callbackState); err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
	res, err := f.Fetch(ctx, &auth.AuthorizationArgs{URL: "https://auth.example/authorize?state=" + callbackState})
	require.NoError(t, err)
	assert.Equal(t, "code-7f3a", res.Code)
	assert.NoError(t, ctx.Err())
}
