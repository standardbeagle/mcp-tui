package oauth

import (
	"context"
	"sync"
	"time"
)

// signInWaitLimit bounds how long the authorization-code flow waits for the
// user to finish signing in, since that wait no longer counts against the
// connect timeout (WithTimeoutExcludingSignIn).
const signInWaitLimit = 5 * time.Minute

type signInClockKey struct{}

// WithTimeoutExcludingSignIn is context.WithTimeout for a connect that may
// include a browser sign-in: the time the OAuth flow spends waiting for the
// user is not counted, so --timeout bounds the network, not how fast a
// person types a password. When the running time is used up the context ends
// with context.DeadlineExceeded, as a WithTimeout context would. It reports
// no Deadline, since the expiry moves while a sign-in is under way.
func WithTimeoutExcludingSignIn(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	c := &signInClock{Context: parent, done: make(chan struct{}), remaining: timeout, started: time.Now()}
	// Under the lock: a short timeout can fire before the assignment lands.
	c.mu.Lock()
	c.timer = time.AfterFunc(timeout, func() { c.end(context.DeadlineExceeded) })
	c.mu.Unlock()
	stopFollowing := context.AfterFunc(parent, func() { c.end(parent.Err()) })
	return c, func() {
		stopFollowing()
		c.end(context.Canceled)
	}
}

// signInClock is the context WithTimeoutExcludingSignIn returns. The
// LocalServerFetcher finds it through Value and stops it for the sign-in.
type signInClock struct {
	context.Context // the parent, for values

	done chan struct{}

	mu        sync.Mutex
	err       error
	timer     *time.Timer
	remaining time.Duration // running time left when the timer was last started
	started   time.Time     // when the timer was last started
	signingIn int           // sign-ins under way (the clock is stopped while > 0)
}

func (c *signInClock) Done() <-chan struct{} { return c.done }

func (c *signInClock) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *signInClock) Deadline() (time.Time, bool) { return time.Time{}, false }

func (c *signInClock) Value(key any) any {
	if key == (signInClockKey{}) {
		return c
	}
	return c.Context.Value(key)
}

func (c *signInClock) end(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return
	}
	c.err = err
	c.timer.Stop()
	close(c.done)
}

func (c *signInClock) signInStarted() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.signingIn++
	if c.signingIn > 1 || c.err != nil {
		return
	}
	if c.timer.Stop() {
		c.remaining -= time.Since(c.started)
	}
}

func (c *signInClock) signInFinished() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.signingIn--
	if c.signingIn > 0 || c.err != nil {
		return
	}
	c.started = time.Now()
	c.timer = time.AfterFunc(max(c.remaining, 0), func() { c.end(context.DeadlineExceeded) })
}

// pauseForSignIn stops ctx's connect clock, when it has one, until the
// returned func is called.
func pauseForSignIn(ctx context.Context) (resume func()) {
	clock, ok := ctx.Value(signInClockKey{}).(*signInClock)
	if !ok {
		return func() {}
	}
	clock.signInStarted()
	return clock.signInFinished
}
