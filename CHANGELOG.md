# Changelog

This module follows semantic versioning. Before v1, a minor version may
change the API; each such change says what callers must do.

## Unreleased

- `DefaultInitialDelay` and `DefaultMaxDelay` each have their own doc
  comment. No API change.
- CI also checks formatting, `go mod tidy -diff`, and `govulncheck`.

## v0.1.0 (2026-11-27)

First release.

- `Backoff` retries an operation with jittered exponential backoff until it
  succeeds, the context ends, or `Transient` rejects the error.
- `RateLimited` retries a request refused with 429 Too Many Requests for as
  long as each Retry-After asks, within a total bound, and never resends a
  request that failed another way.
- `Jitter` spreads a delay forward across [d, 1.5d), and `TransientHTTP`
  reports whether a status code is worth retrying.
