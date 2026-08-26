// Package cli connects command-line arguments to the leak analysis pipeline.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/imbrooklyn/leakviz/internal/analyze"
	"github.com/imbrooklyn/leakviz/internal/diff"
	"github.com/imbrooklyn/leakviz/internal/profile"
	"github.com/imbrooklyn/leakviz/internal/report"
	"github.com/imbrooklyn/leakviz/internal/source"
)

const (
	exitSuccess     = 0
	exitOperational = 1
	exitUsage       = 2

	defaultTimeout = 30 * time.Second
)

const analyzeUsage = `Usage:
  leakviz [flags] <source>

Sources:
  -                 Read a profile from standard input.
  PATH              Read a profile file.
  URL               Read an HTTP or HTTPS endpoint.
  HOST:PORT         Read the goroutineleak endpoint over HTTP.

Flags:
  --json            Write JSON schema v1 instead of text.
  --app prefix      Prefer a user frame in a package or module.
  --timeout value   HTTP request timeout (default 30s).
  --version         Print the version and exit.
  -h, --help        Print this help.

The operand "diff" is reserved; use "./diff" to analyze a file with that name.
`

const diffUsage = `Usage:
  leakviz diff [flags] <before> <after>

Sources:
  -                 Read one profile from standard input.
  PATH              Read a profile file.
  URL               Read an HTTP or HTTPS endpoint.
  HOST:PORT         Read the goroutineleak endpoint over HTTP.

Flags:
  --json            Write JSON schema v1 instead of text.
  --app prefix      Prefer a user frame in a package or module.
  --timeout value   Timeout for each HTTP request (default 30s).
  -h, --help        Print this help.

At most one source may use standard input.
`

// version may be injected with -ldflags -X. Development builds fall back to
// build information and then to the stable "devel" marker.
var version string

// Run executes one leakviz invocation and returns its process exit code.
func Run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "diff" {
		return runDiff(ctx, args[1:], stdin, stdout, stderr)
	}
	return runAnalyze(ctx, args, stdin, stdout, stderr)
}

func runAnalyze(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("leakviz", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}

	var appPrefix string
	timeout := defaultTimeout
	var jsonOutput bool
	var showVersion bool
	var shortHelp bool
	var longHelp bool

	flags.BoolVar(&jsonOutput, "json", false, "write JSON schema v1")
	flags.StringVar(&appPrefix, "app", "", "prefer a user frame in a package or module")
	flags.DurationVar(&timeout, "timeout", defaultTimeout, "HTTP request timeout")
	flags.BoolVar(&showVersion, "version", false, "print the version and exit")
	flags.BoolVar(&shortHelp, "h", false, "print help")
	flags.BoolVar(&longHelp, "help", false, "print help")

	if err := flags.Parse(args); err != nil {
		return usageFailure(stderr, "%v", err)
	}

	if shortHelp || longHelp {
		if len(args) != 1 || flags.NArg() != 0 || showVersion {
			return usageFailure(stderr, "help cannot be combined with other arguments")
		}
		if err := writeString(stdout, analyzeUsage); err != nil {
			return operationalFailure(stderr, "write help: %v", err)
		}
		return exitSuccess
	}

	if showVersion {
		if len(args) != 1 || flags.NArg() != 0 {
			return usageFailure(stderr, "version cannot be combined with other arguments")
		}
		if err := writeString(stdout, "leakviz "+currentVersion()+"\n"); err != nil {
			return operationalFailure(stderr, "write version: %v", err)
		}
		return exitSuccess
	}

	if timeout <= 0 {
		return usageFailure(stderr, "timeout must be greater than zero")
	}
	if flags.NArg() != 1 {
		return usageFailure(stderr, "expected exactly one source operand")
	}
	if ctx == nil {
		return operationalFailure(stderr, "run analysis: nil context")
	}

	analysis, err := analyzeTarget(ctx, flags.Arg(0), appPrefix, timeout, stdin)
	if err != nil {
		return operationalFailure(stderr, "%v", err)
	}
	writeReport := report.WriteText
	if jsonOutput {
		writeReport = report.WriteJSON
	}
	if err := writeReport(stdout, analysis); err != nil {
		return operationalFailure(stderr, "%v", err)
	}

	return exitSuccess
}

func runDiff(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("leakviz diff", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.Usage = func() {}

	var jsonOutput bool
	var appPrefix string
	timeout := defaultTimeout
	var shortHelp bool
	var longHelp bool

	flags.BoolVar(&jsonOutput, "json", false, "write JSON schema v1")
	flags.StringVar(&appPrefix, "app", "", "prefer a user frame in a package or module")
	flags.DurationVar(&timeout, "timeout", defaultTimeout, "timeout for each HTTP request")
	flags.BoolVar(&shortHelp, "h", false, "print help")
	flags.BoolVar(&longHelp, "help", false, "print help")

	if err := flags.Parse(args); err != nil {
		return diffUsageFailure(stderr, "%v", err)
	}
	if shortHelp || longHelp {
		if len(args) != 1 || flags.NArg() != 0 {
			return diffUsageFailure(stderr, "help cannot be combined with other arguments")
		}
		if err := writeString(stdout, diffUsage); err != nil {
			return operationalFailure(stderr, "write help: %v", err)
		}
		return exitSuccess
	}
	if timeout <= 0 {
		return diffUsageFailure(stderr, "timeout must be greater than zero")
	}
	if flags.NArg() != 2 {
		return diffUsageFailure(stderr, "expected exactly two source operands")
	}
	beforeTarget, afterTarget := flags.Arg(0), flags.Arg(1)
	if beforeTarget == "-" && afterTarget == "-" {
		return diffUsageFailure(stderr, "before and after cannot both use standard input")
	}
	if ctx == nil {
		return operationalFailure(stderr, "run diff: nil context")
	}

	before, err := analyzeTarget(ctx, beforeTarget, appPrefix, timeout, stdin)
	if err != nil {
		return operationalFailure(stderr, "analyze before source: %v", err)
	}
	after, err := analyzeTarget(ctx, afterTarget, appPrefix, timeout, stdin)
	if err != nil {
		return operationalFailure(stderr, "analyze after source: %v", err)
	}
	result, err := diff.Compare(before, after)
	if err != nil {
		return operationalFailure(stderr, "compare analyses: %v", err)
	}

	writeReport := report.WriteDiffText
	if jsonOutput {
		writeReport = report.WriteDiffJSON
	}
	if err := writeReport(stdout, result); err != nil {
		return operationalFailure(stderr, "%v", err)
	}
	return exitSuccess
}

func analyzeTarget(ctx context.Context, target, appPrefix string, timeout time.Duration, stdin io.Reader) (analyze.Analysis, error) {
	requestContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	loader := source.Loader{
		Stdin:  stdin,
		Client: &http.Client{Timeout: timeout},
	}
	input, err := loader.Open(requestContext, target)
	if err != nil {
		return analyze.Analysis{}, err
	}
	defer input.Reader.Close()

	snapshot, err := profile.Parse(input.DisplayName, input.Reader)
	if err != nil {
		return analyze.Analysis{}, fmt.Errorf("parse profile: %w", err)
	}
	analysis, err := analyze.Analyze(snapshot, analyze.Options{AppPrefix: appPrefix})
	if err != nil {
		return analyze.Analysis{}, fmt.Errorf("analyze profile: %w", err)
	}
	return analysis, nil
}

func currentVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if buildVersion := buildInfoVersion(info); buildVersion != "" {
			return buildVersion
		}
	}
	return "devel"
}

func buildInfoVersion(info *debug.BuildInfo) string {
	if info == nil || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return ""
	}

	// A source-tree build may carry a Go-generated pseudo-version (and, for a
	// dirty tree, a +dirty suffix). It is still a development build, while a
	// module-version install has Main.Version without local VCS settings.
	for _, setting := range info.Settings {
		if setting.Key == "vcs.modified" {
			return ""
		}
	}
	return info.Main.Version
}

func usageFailure(stderr io.Writer, format string, args ...any) int {
	return usageFailureWith(stderr, analyzeUsage, format, args...)
}

func diffUsageFailure(stderr io.Writer, format string, args ...any) int {
	return usageFailureWith(stderr, diffUsage, format, args...)
}

func usageFailureWith(stderr io.Writer, usage, format string, args ...any) int {
	_ = writeString(stderr, usage)
	message := strings.NewReplacer("\r", " ", "\n", " ").Replace(fmt.Sprintf(format, args...))
	_ = writeString(stderr, "Error: "+message+"\n")
	return exitUsage
}

func operationalFailure(stderr io.Writer, format string, args ...any) int {
	message := strings.NewReplacer("\r", " ", "\n", " ").Replace(fmt.Sprintf(format, args...))
	_ = writeString(stderr, "Error: "+message+"\n")
	return exitOperational
}

func writeString(writer io.Writer, value string) error {
	if writer == nil {
		return fmt.Errorf("nil writer")
	}
	written, err := io.WriteString(writer, value)
	if err != nil {
		return err
	}
	if written != len(value) {
		return io.ErrShortWrite
	}
	return nil
}
