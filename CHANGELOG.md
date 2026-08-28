# Changelog

This file records notable user-visible changes to `leakviz`.

## v0.1.1

See the [v0.1.1 release notes](docs/releases/v0.1.1.md) for installation and
compatibility details.

### Fixed

- Use an absolute, tag-pinned README link that resolves correctly from the
  hosted GitHub Release page.

### Compatibility

- This is a documentation-only patch. CLI behavior, JSON schema v1,
  fingerprints, dependencies, and supported inputs are unchanged from
  v0.1.0.

## v0.1.0

See the [v0.1.0 release notes](docs/releases/v0.1.0.md) for usage and important
limitations.

### Added

- Read-only analysis of Go 1.27 binary `goroutineleak` pprof snapshots from
  HTTP(S), local files, or standard input.
- Deterministic text reports and JSON schema v1 with stack, count, blocker,
  fingerprint, label, user-frame, and finding data.
- Exact grouping within a snapshot and semantic comparison across two
  snapshots.
- Diff output with `NEW`, `INCREASED`, `DECREASED`, `RESOLVED`, and
  `UNCHANGED` statuses while retaining every exact site in JSON.
- `--app`, `--timeout`, `--json`, help, and version support using standard
  flags-before-operands syntax.

### Safety and compatibility

- Text reports render profile-derived control characters and invalid UTF-8
  bytes as visible escape sequences; JSON uses standard string escaping.
- Reports preserve profile labels, which may contain sensitive data.
- Runtime reachability can produce false negatives; an empty report does not
  prove that an application has no goroutine leaks.
- Ordinary goroutine profiles, delta profiles, symbolization, root-cause
  proof, and automatic fixes are outside the v0.1 scope.
