package retry

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

var errCause = errors.New("cause")

func TestDoValuesRetriesUntilSuccess(t *testing.T) {
	calls := 0
	got, err := Backoff{MaxAttempts: 5}.DoValues(t.Context(), func(context.Context) (string, error) {
		calls++
		if calls < 3 {
			return "", Again(errCause)
		}
		return "ok", nil
	})
	if err != nil || got != "ok" || calls != 3 {
		t.Fatalf("got (%q, %v) after %d calls, want (\"ok\", nil) after 3", got, err, calls)
	}
}

func TestDoStopsOnUnmarkedError(t *testing.T) {
	calls := 0
	err := Backoff{MaxAttempts: 5}.Do(t.Context(), func(context.Context) error {
		calls++
		return errCause
	})
	if err != errCause || calls != 1 { //nolint:errorlint // checking the exact error
		t.Fatalf("got %v after %d calls, want errCause after 1", err, calls)
	}
}

func TestDoReturnsCauseWhenOutOfAttempts(t *testing.T) {
	calls := 0
	err := Backoff{MaxAttempts: 3}.Do(t.Context(), func(context.Context) error {
		calls++
		return Again(errCause)
	})
	if err != errCause || calls != 3 { //nolint:errorlint // the Again marker must be stripped
		t.Fatalf("got %v after %d calls, want errCause after 3", err, calls)
	}
}

func TestDoZeroValueCallsOnce(t *testing.T) {
	calls := 0
	_ = Backoff{}.Do(t.Context(), func(context.Context) error {
		calls++
		return Again(errCause)
	})
	if calls != 1 {
		t.Fatalf("got %d calls, want 1", calls)
	}
}

func TestDoStopsWhenContextDone(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	err := Backoff{MaxAttempts: 3, InitialDelay: time.Hour}.Do(ctx, func(context.Context) error {
		calls++
		cancel()
		return Again(errCause)
	})
	if err != errCause || calls != 1 { //nolint:errorlint // the Again marker must be stripped
		t.Fatalf("got %v after %d calls, want errCause after 1", err, calls)
	}
}

func TestDefaultBackoffWaitsBetweenAttempts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		_ = DefaultBackoff.Do(t.Context(), func(context.Context) error {
			return Again(errCause)
		})
		if took := time.Since(start); took < 7500*time.Millisecond || took > 15*time.Second {
			t.Fatalf("took %v, want between 7.5s and 15s", took)
		}
	})
}

func TestAgainNilIsNil(t *testing.T) {
	if err := Again(nil); err != nil {
		t.Fatalf("Again(nil) = %v, want nil", err)
	}
}

func TestNextDelayDoublesUpToMax(t *testing.T) {
	b := Backoff{MaxDelay: 3 * time.Second}
	if got := b.nextDelay(time.Second); got != 2*time.Second {
		t.Errorf("nextDelay(1s) = %v, want 2s", got)
	}
	if got := b.nextDelay(2 * time.Second); got != 3*time.Second {
		t.Errorf("nextDelay(2s) = %v, want 3s", got)
	}
}
