# Changelog

This module follows semantic versioning. Before v1, a minor version may
change the API; each such change says what callers must do.

## v0.2.1 (2026-11-28)

- CI runs every step in bash, so the Windows runners read
  `-modfile=tools/go.mod` whole. No API change.
- The README says the module requires Go 1.27.

## v0.2.0 (2026-11-28)

- Requires Go 1.27, the newest Go release; Go 1.26 is no longer
  supported.
- CI runs staticcheck and govulncheck at the versions pinned in
  `tools/go.mod`, a module of its own that importers never see.

## v0.1.1 (2026-11-27)

v0.1.2 tags the same commit.

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
