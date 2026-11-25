// Package retry calls a function again with exponential backoff
// for as long as it returns errors marked with [Again].
package retry

import (
	"context"
	"math/rand/v2"
	"time"
)

// Again marks err as worth retrying. Return it directly from the function
// passed to [Backoff.Do] or [Backoff.DoValues]. Again(nil) is nil.
func Again(err error) error {
	if err == nil {
		return nil
	}
	return &againError{err: err}
}

type againError struct {
	err error
}

func (e *againError) Error() string { return e.err.Error() }
func (e *againError) Unwrap() error { return e.err }

// DefaultBackoff is a sensible backoff for calls to other services.
var DefaultBackoff = Backoff{
	MaxAttempts:  5,
	InitialDelay: time.Second,
	MaxDelay:     10 * time.Second,
}

// Backoff decides how often and how long to wait between attempts.
// The zero value attempts once.
type Backoff struct {
	MaxAttempts  int
	InitialDelay time.Duration
	MaxDelay     time.Duration
}

// Do calls fn until it returns an error not marked with [Again],
// it has made MaxAttempts calls, or ctx is done while waiting.
// It returns fn's last error, without the [Again] marker.
func (b Backoff) Do(ctx context.Context, fn func(context.Context) error) error {
	_, err := b.DoValues(ctx, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}

// DoValues is like [Backoff.Do], but also returns the value from fn's last call.
func (b Backoff) DoValues[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	var value T
	var err error
	delay := b.InitialDelay
	for attempt := range max(1, b.MaxAttempts) {
		if attempt > 0 {
			if !wait(ctx, withJitter(delay)) {
				break
			}
			delay = b.nextDelay(delay)
		}
		value, err = fn(ctx)
		again, ok := err.(*againError) //nolint:errorlint // like fs.SkipDir, the marker must be returned as is
		if !ok {
			return value, err
		}
		err = again.err
	}
	return value, err
}

func (b Backoff) nextDelay(delay time.Duration) time.Duration {
	if b.MaxDelay > 0 {
		return min(delay*2, b.MaxDelay)
	}
	return delay * 2
}

// withJitter shortens d by a random amount, so that clients
// failing at the same time don't retry in lockstep.
func withJitter(d time.Duration) time.Duration {
	half := d / 2
	return half + rand.N(d-half+1)
}

func wait(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
