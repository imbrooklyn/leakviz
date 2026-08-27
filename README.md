# leakviz

`leakviz` is a small, read-only command-line tool for inspecting Go 1.27
`goroutineleak` profiles. It turns runtime evidence about permanently blocked
goroutines into deterministic text or JSON reports, and it can compare two
snapshots without modifying the target application or the profile.

## Requirements

- Go 1.27.x
- A binary `goroutineleak` pprof snapshot from a Go 1.27 application

## Install or build

To install the tagged v0.1.0 module release when it is available:

```bash
go install github.com/imbrooklyn/leakviz/cmd/leakviz@v0.1.0
```

To build the current source checkout:

```bash
mkdir -p bin
go build -o ./bin/leakviz ./cmd/leakviz
./bin/leakviz --version
```

A source-checkout build reports `leakviz devel`. A build installed from the
`v0.1.0` module tag reports `leakviz v0.1.0` through Go build information.

## Analyze a snapshot

### HTTP

A bare `HOST:PORT` or an HTTP(S) URL with an empty or root path uses
`/debug/pprof/goroutineleak`:

```bash
leakviz localhost:6060
leakviz https://service.example
```

An explicit non-root URL path is requested as written:

```bash
leakviz https://service.example/internal/profiles/goroutineleak
```

### File

```bash
leakviz ./leak.pprof
```

### Standard input

```bash
cat ./leak.pprof | leakviz -
```

## Output and analysis options

Flags use Go's standard flags-before-operands order.

| Option | Behavior |
| --- | --- |
| `--json` | Write deterministic JSON schema v1 instead of text. |
| `--app prefix` | Prefer the first user frame in the package or module prefix. This changes display selection only, not fingerprints. |
| `--timeout duration` | Set the HTTP request timeout; the default is `30s` and the value must be greater than zero. |

```bash
leakviz --json ./leak.pprof
leakviz --app github.com/acme/service ./leak.pprof
leakviz --timeout 2m localhost:6060
```

Text and JSON reports include stack evidence, exact and semantic fingerprints,
counts, blocker classification, user-frame selection, labels, and findings.
Unknown blockers remain reportable and are not assigned a guessed cause.

## Compare snapshots

`diff` compares exact snapshot groups through semantic fingerprint buckets, so
a source line move can still match while every exact site remains available in
JSON output.

```bash
leakviz diff ./before.pprof ./after.pprof
leakviz diff --json ./before.pprof ./after.pprof
```

Diff statuses are `NEW`, `INCREASED`, `DECREASED`, `RESOLVED`, and `UNCHANGED`.
At most one diff input may be standard input. Each HTTP input receives an
independent timeout. Finding leaks or an increase does not change the success
exit code; operational errors return `1`, and usage errors return `2`.

## Interpretation and limits

### Runtime evidence and false negatives

An entry in a Go 1.27 `goroutineleak` profile means the runtime determined that
the goroutine cannot become unblocked. This is strong evidence of permanent
blocking, but it does not prove the application's root cause.

The runtime's decision depends on garbage-collector reachability. A synchronization
object that remains reachable from a global variable or a runnable goroutine can
keep a real leak out of the profile. An empty report therefore does not prove
that the application has no goroutine leaks.

### Sensitive labels

Profile labels can contain tenant names, identifiers, or other sensitive data.
`leakviz` includes label keys and values in text and JSON reports by default.
Review and protect report files before sharing them. The tool does not upload
profiles, start a server, or send telemetry.

### Input scope

v0.1 accepts gzip-compressed or uncompressed binary pprof data containing
exactly one `goroutineleak/count` sample type. It rejects ordinary `goroutine`
profiles, delta profiles, and unsymbolized positive-count samples rather than
guessing leak or symbol information.

## v0.1 non-goals

v0.1 does not provide:

- symbolization;
- root-cause proof;
- automatic fixes;
- watch, polling, or daemon modes;
- a plugin or rule DSL;
- configuration-file or environment-variable settings;
- a public Go library API;
- automatic migration across fingerprint versions; or
- leak inference from ordinary goroutine profiles.

## License

Licensed under the [Apache License 2.0](LICENSE).
