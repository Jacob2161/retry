# retry

[![Go Reference](https://pkg.go.dev/badge/github.com/jacob2161/retry.svg)](https://pkg.go.dev/github.com/jacob2161/retry)
[![CI](https://github.com/jacob2161/retry/actions/workflows/ci.yml/badge.svg)](https://github.com/jacob2161/retry/actions/workflows/ci.yml)

Retry an operation with jittered exponential backoff, or retry a request a server refused with 429
for as long as each Retry-After asks, within a bound. Standard library only.

```bash
go get github.com/jacob2161/retry
```

## Usage

```go
err := retry.Backoff(ctx, &retry.BackoffArgs{
	Operation: func() error { return send(ctx) },
	Transient: isTransient, // nil retries nothing
	OnRetry: func(attempt int, err error, delay time.Duration) {
		log.Printf("attempt %d failed: %v; retrying in %s", attempt, err, delay)
	},
})
```

- The operation always runs once, even on a cancelled context; the context is checked only while
  waiting.
- Jitter is one-sided, spreading a delay d across [d, 1.5d): waiting a little longer is always safe,
  waiting less is what stampedes a recovering service.
- `RateLimited` never resends a request that failed any other way than 429, since it may have taken
  effect.
- There is no total time limit of its own: give it a context with a deadline.

Requires Go 1.27. Licensed under the MIT license.
