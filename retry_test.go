package retry

import (
	"context"
	"errors"
	"math"
	"net/http"
	"testing"
	"time"
)

func TestBackoffRetriesTransientUntilSuccess(t *testing.T) {
	attempts := 0
	retries := 0
	err := Backoff(t.Context(), &BackoffArgs{
		Operation: func() error {
			attempts++
			if attempts < 3 {
				return errors.New("not yet")
			}
			return nil
		},
		Transient:    func(error) bool { return true },
		InitialDelay: time.Millisecond,
		MaxDelay:     2 * time.Millisecond,
		OnRetry:      func(int, error, time.Duration) { retries++ },
	})
	if err != nil {
		t.Fatalf("Backoff: %v", err)
	}
	if attempts != 3 || retries != 2 {
		t.Fatalf("attempts = %d, retries = %d, want 3 and 2", attempts, retries)
	}
}

func TestBackoffReturnsPermanentErrorImmediately(t *testing.T) {
	permanent := errors.New("denied")
	attempts := 0
	err := Backoff(t.Context(), &BackoffArgs{
		Operation: func() error {
			attempts++
			return permanent
		},
		Transient:    func(error) bool { return false },
		InitialDelay: time.Hour,
		MaxDelay:     time.Hour,
	})
	if !errors.Is(err, permanent) {
		t.Fatalf("Backoff error = %v, want the operation's own error", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestBackoffStopsOnContextDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	err := Backoff(ctx, &BackoffArgs{
		Operation:    func() error { return errors.New("still down") },
		Transient:    func(error) bool { return true },
		InitialDelay: 5 * time.Millisecond,
		MaxDelay:     10 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Backoff error = %v, want the context deadline", err)
	}
}

func TestTransientHTTP(t *testing.T) {
	for _, statusCode := range []int{
		http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		if !TransientHTTP(statusCode) {
			t.Errorf("TransientHTTP(%d) = false, want true", statusCode)
		}
	}
	for _, statusCode := range []int{
		http.StatusOK,
		http.StatusBadRequest,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusConflict,
		http.StatusNotImplemented,
	} {
		if TransientHTTP(statusCode) {
			t.Errorf("TransientHTTP(%d) = true, want false", statusCode)
		}
	}
}

func TestJitterSpreadsForward(t *testing.T) {
	jitterCases := []struct {
		name  string
		delay time.Duration
		low   time.Duration
		high  time.Duration // exclusive, except where low == high
	}{
		{name: "zero", delay: 0, low: 0, high: 0},
		{name: "negative", delay: -1, low: 0, high: 0},
		{name: "one nanosecond", delay: 1, low: 1, high: 1},
		{name: "three nanoseconds", delay: 3, low: 3, high: 4},
		{name: "one second", delay: time.Second, low: time.Second, high: 1500 * time.Millisecond},
		{name: "longest", delay: math.MaxInt64, low: math.MaxInt64, high: math.MaxInt64},
	}
	for _, jitterCase := range jitterCases {
		t.Run(jitterCase.name, func(t *testing.T) {
			for range 200 {
				jittered := Jitter(jitterCase.delay)
				if jitterCase.low == jitterCase.high {
					if jittered != jitterCase.low {
						t.Fatalf(
							"Jitter(%d) = %d, want %d",
							jitterCase.delay,
							jittered,
							jitterCase.low,
						)
					}
					continue
				}
				if jittered < jitterCase.low || jittered >= jitterCase.high {
					t.Fatalf(
						"Jitter(%d) = %d, outside [%d, %d)",
						jitterCase.delay,
						jittered,
						jitterCase.low,
						jitterCase.high,
					)
				}
			}
		})
	}
}

func TestBackoffDeniesMissingOperation(t *testing.T) {
	if err := Backoff(t.Context(), nil); err == nil {
		t.Fatal("Backoff(nil) returned nil, want an error")
	}
	if err := Backoff(t.Context(), &BackoffArgs{}); err == nil {
		t.Fatal("Backoff without an operation returned nil, want an error")
	}
}

func TestBackoffTriesOnceOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	attempts := 0
	err := Backoff(ctx, &BackoffArgs{
		Operation: func() error {
			attempts++
			return errors.New("down")
		},
		Transient: func(error) bool { return true },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Backoff error = %v, want context.Canceled", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestBackoffDefaultsZeroDelays(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var firstDelay time.Duration
	err := Backoff(ctx, &BackoffArgs{
		Operation: func() error { return errors.New("down") },
		Transient: func(error) bool { return true },
		OnRetry: func(_ int, _ error, delay time.Duration) {
			firstDelay = delay
			cancel()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Backoff error = %v, want context.Canceled", err)
	}
	if firstDelay < DefaultInitialDelay || firstDelay >= DefaultInitialDelay*3/2 {
		t.Fatalf("first delay = %s, want within [%s, 1.5x)", firstDelay, DefaultInitialDelay)
	}
}

func TestDoubledDelaySaturates(t *testing.T) {
	doublingCases := []struct {
		delay, maxDelay, want time.Duration
	}{
		{delay: time.Second, maxDelay: 30 * time.Second, want: 2 * time.Second},
		{delay: 20 * time.Second, maxDelay: 30 * time.Second, want: 30 * time.Second},
		{delay: 15 * time.Second, maxDelay: 30 * time.Second, want: 30 * time.Second},
		{delay: math.MaxInt64/2 + 1, maxDelay: math.MaxInt64, want: math.MaxInt64},
		{delay: math.MaxInt64, maxDelay: math.MaxInt64, want: math.MaxInt64},
	}
	for _, doublingCase := range doublingCases {
		if got := doubledDelay(
			doublingCase.delay,
			doublingCase.maxDelay,
		); got != doublingCase.want {
			t.Errorf(
				"doubledDelay(%d, %d) = %d, want %d",
				doublingCase.delay,
				doublingCase.maxDelay,
				got,
				doublingCase.want,
			)
		}
	}
}

// errRateLimited stands for a server's 429 Too Many Requests refusal.
var errRateLimited = errors.New("429 too many requests")

// rateLimitedRetryAfter classifies errRateLimited as rate limited with no
// Retry-After.
func rateLimitedRetryAfter(err error) (time.Duration, bool) {
	return 0, errors.Is(err, errRateLimited)
}

// TestRateLimited retries a rate-limited request until it is answered, gives
// up on one still refused when the wait runs out, and never sends again a
// request that failed another way.
func TestRateLimited(t *testing.T) {
	attempts := 0
	err := RateLimited(t.Context(), &RateLimitedArgs{
		Operation: func() error {
			attempts++
			if attempts < 3 {
				return errRateLimited
			}
			return nil
		},
		RetryAfter: rateLimitedRetryAfter,
		Wait:       time.Second,
		Delay:      time.Millisecond,
	})
	if err != nil || attempts != 3 {
		t.Fatalf("after two refusals: %v in %d attempts, want success in 3", err, attempts)
	}

	attempts = 0
	err = RateLimited(t.Context(), &RateLimitedArgs{
		Operation:  func() error { attempts++; return errRateLimited },
		RetryAfter: rateLimitedRetryAfter,
		Wait:       50 * time.Millisecond,
		Delay:      10 * time.Millisecond,
	})
	if !errors.Is(err, errRateLimited) || attempts < 2 || attempts > 6 {
		t.Fatalf("past the wait: %v in %d attempts, want the refusal after a few", err, attempts)
	}

	dropped := errors.New("connection reset by peer")
	attempts = 0
	err = RateLimited(t.Context(), &RateLimitedArgs{
		Operation:  func() error { attempts++; return dropped },
		RetryAfter: rateLimitedRetryAfter,
		Delay:      time.Millisecond,
	})
	if !errors.Is(err, dropped) || attempts != 1 {
		t.Fatalf("a dropped connection: %v in %d attempts, want it once", err, attempts)
	}
}

// TestRateLimitedHugeRetryAfter checks that a Retry-After too large to add
// to the elapsed time ends the call instead of overflowing past the bound.
func TestRateLimitedHugeRetryAfter(t *testing.T) {
	attempts := 0
	err := RateLimited(t.Context(), &RateLimitedArgs{
		Operation: func() error {
			attempts++
			return errRateLimited
		},
		RetryAfter: func(error) (time.Duration, bool) { return time.Duration(math.MaxInt64), true },
	})
	if !errors.Is(err, errRateLimited) || attempts != 1 {
		t.Fatalf("a huge Retry-After: %d attempts, %v", attempts, err)
	}
}

// TestTransientHTTPBounds checks the edges of the retryable class.
func TestTransientHTTPBounds(t *testing.T) {
	for statusCode, want := range map[int]bool{499: false, 500: true, 501: false, 599: true, 600: false, 999: false} {
		if got := TransientHTTP(statusCode); got != want {
			t.Errorf("TransientHTTP(%d) = %v, want %v", statusCode, got, want)
		}
	}
}
