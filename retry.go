// Package retry retries operations with jittered exponential backoff, and
// retries requests a server refused with 429 Too Many Requests for as long as
// each refusal's Retry-After asks, within a bound.
//
// It uses only the standard library. A total time limit is the caller's
// context deadline; this package adds none of its own.
package retry

import (
	"context"
	"errors"
	"math"
	mathrand "math/rand/v2"
	"net/http"
	"time"
)

// Default delays for a Backoff call whose BackoffArgs leave them zero.
const (
	DefaultInitialDelay = 100 * time.Millisecond
	DefaultMaxDelay     = 30 * time.Second
)

// BackoffArgs configures a single Backoff call.
type BackoffArgs struct {
	// Operation is the work to attempt. It runs at least once. Required.
	Operation func() error
	// Transient decides whether an error earns another attempt. An error it
	// rejects is returned immediately; nil retries nothing.
	Transient func(error) bool
	// InitialDelay is the wait before the second attempt. It doubles after
	// every retry. Zero or negative uses DefaultInitialDelay.
	InitialDelay time.Duration
	// MaxDelay caps the doubling. Zero or negative uses DefaultMaxDelay; a
	// MaxDelay below InitialDelay caps the first wait too.
	MaxDelay time.Duration
	// OnRetry, when set, runs once per retry with the attempt number
	// (1-based), the error that earned it, and the delay before the next
	// attempt. Use it to log what is happening during an outage.
	OnRetry func(attempt int, err error, delay time.Duration)
}

// Backoff runs args.Operation until it returns nil, ctx is cancelled, or the
// operation returns an error args.Transient classifies as not retryable.
//
// The operation always runs once, even on a context that is already
// cancelled; ctx is checked only while waiting between attempts, and a
// cancelled wait returns ctx.Err().
//
// Between attempts: jittered exponential backoff from InitialDelay,
// doubling, capped at MaxDelay. There is no total-time cap of its own; a
// caller that wants one gives Backoff a context with a deadline.
func Backoff(ctx context.Context, args *BackoffArgs) error {
	if args == nil || args.Operation == nil {
		return errors.New("no operation")
	}
	initialDelay := args.InitialDelay
	if initialDelay <= 0 {
		initialDelay = DefaultInitialDelay
	}
	maxDelay := args.MaxDelay
	if maxDelay <= 0 {
		maxDelay = DefaultMaxDelay
	}
	attempt := 0
	delay := min(initialDelay, maxDelay)
	for {
		err := args.Operation()
		if err == nil {
			return nil
		}
		if args.Transient == nil || !args.Transient(err) {
			return err
		}
		attempt++
		jittered := Jitter(delay)
		if args.OnRetry != nil {
			args.OnRetry(attempt, err, jittered)
		}
		timer := time.NewTimer(jittered)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		delay = doubledDelay(delay, maxDelay)
	}
}

// doubledDelay doubles delay up to maxDelay without overflowing.
func doubledDelay(delay, maxDelay time.Duration) time.Duration {
	if delay > maxDelay/2 {
		return maxDelay
	}
	return delay * 2
}

// Jitter spreads d across [d, d+⌊d/2⌋) so a herd of clients reconnecting to
// a recovering service lands across a window instead of all hitting at the
// same instant. The spread is one-sided on purpose: waiting a little longer
// than the backoff asked for is always safe, waiting less is what stampedes.
// A d of 0 or less returns 0, a d too short to spread (1 ns) returns d, and
// the sum saturates at the longest Duration.
func Jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	spread := int64(d) / 2
	if spread == 0 {
		return d
	}
	jitter := mathrand.Int64N(spread)
	if int64(d) > math.MaxInt64-jitter {
		return time.Duration(math.MaxInt64)
	}
	return d + time.Duration(jitter)
}

// TransientHTTP reports whether an HTTP status code is in the retryable
// class: 500 to 599 except 501 Not Implemented, 408, and 429. Everything else
// is an answer, not a stumble.
func TransientHTTP(statusCode int) bool {
	switch statusCode {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	if statusCode >= 500 && statusCode < 600 && statusCode != http.StatusNotImplemented {
		return true
	}
	return false
}

// DefaultRateLimitWait is the default bound on the time RateLimited spends
// retrying a request the server refused with 429 Too Many Requests.
const DefaultRateLimitWait = 30 * time.Second

// DefaultRateLimitDelay is the default wait after a refusal that names no
// Retry-After.
const DefaultRateLimitDelay = time.Second

// RateLimitedArgs configures a single RateLimited call.
type RateLimitedArgs struct {
	// Operation is the request. It runs at least once. Required.
	Operation func() error
	// RetryAfter reports whether an error is the server's 429 Too Many
	// Requests refusal, and the wait its Retry-After asks for, 0 when it names none.
	// Any other error, a dropped connection included, ends the call.
	// Required.
	RetryAfter func(error) (time.Duration, bool)
	// Wait bounds the time spent retrying: a retry whose wait would end
	// past it is not made. Zero or negative uses DefaultRateLimitWait.
	Wait time.Duration
	// Delay is the wait when a refusal names no Retry-After. Zero or
	// negative uses DefaultRateLimitDelay.
	Delay time.Duration
}

// RateLimited runs args.Operation until it is not refused as rate limited,
// waiting as each refusal's Retry-After asks, jittered, for at most
// args.Wait in all; it then returns the last refusal. A request that failed
// any other way is never sent again, since it may have taken effect.
func RateLimited(ctx context.Context, args *RateLimitedArgs) error {
	if args == nil || args.Operation == nil || args.RetryAfter == nil {
		return errors.New("no operation")
	}
	wait := args.Wait
	if wait <= 0 {
		wait = DefaultRateLimitWait
	}
	fallback := args.Delay
	if fallback <= 0 {
		fallback = DefaultRateLimitDelay
	}
	started := time.Now()
	for {
		err := args.Operation()
		if err == nil {
			return nil
		}
		delay, limited := args.RetryAfter(err)
		if !limited {
			return err
		}
		if delay <= 0 {
			delay = fallback
		}
		delay = Jitter(delay)
		// Compared against what is left, so a huge Retry-After cannot
		// overflow the sum and slip under the bound.
		if remaining := wait - time.Since(started); remaining <= 0 || delay > remaining {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errors.Join(err, ctx.Err())
		case <-timer.C:
		}
	}
}
