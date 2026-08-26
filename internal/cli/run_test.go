package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pprofprofile "github.com/google/pprof/profile"
)

func TestRunFullPipelineSources(t *testing.T) {
	data := cliProfileBytes(t, "goroutineleak")
	profilePath := writeCLIProfile(t, "snapshot.pprof", data)

	t.Run("file", func(t *testing.T) {
		stdout, stderr, code := invokeCLI(t, []string{profilePath}, strings.NewReader("unused"), nil)
		assertSuccessfulReport(t, code, stdout, stderr, "snapshot.pprof")
	})

	t.Run("stdin", func(t *testing.T) {
		stdout, stderr, code := invokeCLI(t, []string{"-"}, bytes.NewReader(data), nil)
		assertSuccessfulReport(t, code, stdout, stderr, "-")
	})

	t.Run("HTTP", func(t *testing.T) {
		requestedPath := make(chan string, 1)
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			requestedPath <- request.URL.Path
			writer.Header().Set("Content-Type", "application/octet-stream")
			_, _ = writer.Write(data)
		}))
		defer server.Close()

		stdout, stderr, code := invokeCLI(t, []string{"--timeout=1s", server.URL}, nil, nil)
		assertSuccessfulReport(t, code, stdout, stderr, server.URL)
		if got := <-requestedPath; got != "/debug/pprof/goroutineleak" {
			t.Fatalf("HTTP request path = %q, want goroutineleak endpoint", got)
		}
	})
}

func TestRunAppPreference(t *testing.T) {
	profilePath := writeCLIProfile(t, "app.pprof", cliProfileBytes(t, "goroutineleak"))

	defaultOutput, defaultError, defaultCode := invokeCLI(t, []string{profilePath}, nil, nil)
	assertSuccessfulReport(t, defaultCode, defaultOutput, defaultError, "app.pprof")
	if !strings.Contains(defaultOutput, "  User frame: default.wait (default.go:30)\n") {
		t.Fatalf("default output missing default user frame:\n%s", defaultOutput)
	}

	appOutput, appError, appCode := invokeCLI(
		t,
		[]string{"--app", "example.com/chosen", profilePath},
		nil,
		nil,
	)
	assertSuccessfulReport(t, appCode, appOutput, appError, "app.pprof")
	if !strings.Contains(appOutput, "  User frame: chosen.Run (chosen.go:42)\n") {
		t.Fatalf("--app output missing preferred user frame:\n%s", appOutput)
	}
}

func TestRunAnalyzeArgvContract(t *testing.T) {
	profilePath := writeCLIProfile(t, "argv.pprof", cliProfileBytes(t, "goroutineleak"))

	tests := []struct {
		name      string
		args      []string
		wantCode  int
		wantError string
		wantJSON  bool
	}{
		{
			name:     "canonical flags before operand",
			args:     []string{"--app=example.com/chosen", "--timeout=1s", profilePath},
			wantCode: exitSuccess,
		},
		{
			name:     "standard single dash spellings",
			args:     []string{"-app", "example.com/chosen", "-timeout=1s", profilePath},
			wantCode: exitSuccess,
		},
		{
			name:      "trailing app rejected",
			args:      []string{profilePath, "--app", "example.com/chosen"},
			wantCode:  exitUsage,
			wantError: "expected exactly one source operand",
		},
		{
			name:      "trailing timeout rejected",
			args:      []string{profilePath, "--timeout=1s"},
			wantCode:  exitUsage,
			wantError: "expected exactly one source operand",
		},
		{
			name:     "canonical JSON flag",
			args:     []string{"--json", profilePath},
			wantCode: exitSuccess,
			wantJSON: true,
		},
		{
			name:     "standard single dash JSON spelling",
			args:     []string{"-json", profilePath},
			wantCode: exitSuccess,
			wantJSON: true,
		},
		{
			name:      "unknown flag",
			args:      []string{"--unknown", profilePath},
			wantCode:  exitUsage,
			wantError: "flag provided but not defined: -unknown",
		},
		{
			name:      "missing operand",
			wantCode:  exitUsage,
			wantError: "expected exactly one source operand",
		},
		{
			name:      "extra operand",
			args:      []string{profilePath, profilePath},
			wantCode:  exitUsage,
			wantError: "expected exactly one source operand",
		},
		{
			name:      "top-level flags do not dispatch diff",
			args:      []string{"--app=example.com/chosen", "diff", "before.pprof", "after.pprof"},
			wantCode:  exitUsage,
			wantError: "expected exactly one source operand",
		},
		{
			name:      "zero timeout",
			args:      []string{"--timeout=0", profilePath},
			wantCode:  exitUsage,
			wantError: "timeout must be greater than zero",
		},
		{
			name:      "negative timeout",
			args:      []string{"--timeout=-1s", profilePath},
			wantCode:  exitUsage,
			wantError: "timeout must be greater than zero",
		},
		{
			name:      "invalid timeout",
			args:      []string{"--timeout=soon", profilePath},
			wantCode:  exitUsage,
			wantError: "invalid value",
		},
		{
			name:      "overflow timeout",
			args:      []string{"--timeout=999999999999999999999h", profilePath},
			wantCode:  exitUsage,
			wantError: "invalid value",
		},
		{
			name:      "help with operand",
			args:      []string{"--help", profilePath},
			wantCode:  exitUsage,
			wantError: "help cannot be combined",
		},
		{
			name:      "version with operand",
			args:      []string{"--version", profilePath},
			wantCode:  exitUsage,
			wantError: "version cannot be combined",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := invokeCLI(t, test.args, nil, nil)
			if code != test.wantCode {
				t.Fatalf("Run() code = %d, want %d; stdout=%q stderr=%q", code, test.wantCode, stdout, stderr)
			}
			if test.wantCode == exitSuccess {
				wantPrefix := "LEAKVIZ ANALYSIS\n"
				if test.wantJSON {
					wantPrefix = "{\n  \"schema_version\": 1,\n  \"report\": \"analysis\",\n"
				}
				if stderr != "" || !strings.HasPrefix(stdout, wantPrefix) {
					t.Fatalf("success streams: stdout=%q stderr=%q", stdout, stderr)
				}
				return
			}
			if stdout != "" {
				t.Fatalf("usage failure stdout = %q, want empty", stdout)
			}
			if !strings.HasPrefix(stderr, analyzeUsage) || !strings.Contains(stderr, "Error: "+test.wantError) {
				t.Fatalf("usage failure stderr = %q, want usage and error containing %q", stderr, test.wantError)
			}
		})
	}
}

func TestRunHelpAndVersion(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			stdout, stderr, code := invokeCLI(t, []string{arg}, nil, nil)
			if code != exitSuccess || stdout != analyzeUsage || stderr != "" {
				t.Fatalf("Run(%q) = code %d, stdout %q, stderr %q", arg, code, stdout, stderr)
			}
		})
	}

	oldVersion := version
	version = "v0.1.0-test"
	t.Cleanup(func() { version = oldVersion })
	for _, arg := range []string{"--version", "-version"} {
		t.Run(arg, func(t *testing.T) {
			stdout, stderr, code := invokeCLI(t, []string{arg}, nil, nil)
			if code != exitSuccess || stdout != "leakviz v0.1.0-test\n" || stderr != "" {
				t.Fatalf("Run(%q) = code %d, stdout %q, stderr %q", arg, code, stdout, stderr)
			}
		})
	}

	stdout, stderr, code := invokeCLI(t, []string{"--help", "--version"}, nil, nil)
	if code != exitUsage || stdout != "" || !strings.Contains(stderr, "help cannot be combined") {
		t.Fatalf("help/version conflict = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestBuildInfoVersion(t *testing.T) {
	tests := []struct {
		name string
		info *debug.BuildInfo
		want string
	}{
		{name: "missing"},
		{name: "devel", info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}},
		{name: "module version", info: &debug.BuildInfo{Main: debug.Module{Version: "v0.1.0"}}, want: "v0.1.0"},
		{
			name: "local clean source tree",
			info: &debug.BuildInfo{
				Main:     debug.Module{Version: "v0.0.0-20260825020421-d30156fad1b8"},
				Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "false"}},
			},
		},
		{
			name: "local dirty source tree",
			info: &debug.BuildInfo{
				Main:     debug.Module{Version: "v0.0.0-20260825020421-d30156fad1b8+dirty"},
				Settings: []debug.BuildSetting{{Key: "vcs.modified", Value: "true"}},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := buildInfoVersion(test.info); got != test.want {
				t.Fatalf("buildInfoVersion() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestRunDoubleDashAndDiffDispatch(t *testing.T) {
	data := cliProfileBytes(t, "goroutineleak")
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	if err := os.WriteFile("-snapshot.pprof", data, 0o600); err != nil {
		t.Fatalf("write dash profile: %v", err)
	}
	if err := os.WriteFile("diff", data, 0o600); err != nil {
		t.Fatalf("write diff profile: %v", err)
	}

	stdout, stderr, code := invokeCLI(t, []string{"--", "-snapshot.pprof"}, nil, nil)
	assertSuccessfulReport(t, code, stdout, stderr, "-snapshot.pprof")

	stdout, stderr, code = invokeCLI(t, []string{"diff"}, nil, nil)
	if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, diffUsage) || !strings.Contains(stderr, "expected exactly two source operands") {
		t.Fatalf("diff dispatch = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}

	stdout, stderr, code = invokeCLI(t, []string{"./diff"}, nil, nil)
	assertSuccessfulReport(t, code, stdout, stderr, "diff")
}

func TestRunTimeoutAndValidationBeforeOpen(t *testing.T) {
	data := cliProfileBytes(t, "goroutineleak")
	requestStarted := make(chan struct{})
	requestDone := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		close(requestStarted)
		<-request.Context().Done()
		close(requestDone)
	}))
	defer server.Close()

	stdout, stderr, code := invokeCLI(t, []string{"--timeout=50ms", server.URL}, nil, nil)
	if code != exitOperational || stdout != "" {
		t.Fatalf("timeout = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "HTTP request timed out") || strings.Count(stderr, "\n") != 1 {
		t.Fatalf("timeout stderr = %q, want one timeout line", stderr)
	}
	select {
	case <-requestStarted:
	default:
		t.Fatal("timeout request did not reach the server")
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("server request context was not canceled")
	}

	var requests atomic.Int64
	validationServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		_, _ = writer.Write(data)
	}))
	defer validationServer.Close()

	stdout, stderr, code = invokeCLI(t, []string{"--timeout=0", validationServer.URL}, nil, nil)
	if code != exitUsage || stdout != "" || requests.Load() != 0 {
		t.Fatalf("invalid timeout opened source: code %d, stdout %q, stderr %q, requests %d", code, stdout, stderr, requests.Load())
	}
}

func TestRunOperationalFailuresAndStreamSeparation(t *testing.T) {
	t.Run("JSON success", func(t *testing.T) {
		profilePath := writeCLIProfile(t, "json.pprof", cliProfileBytes(t, "goroutineleak"))
		stdout, stderr, code := invokeCLI(t, []string{"--json", profilePath}, nil, nil)
		if code != exitSuccess || stderr != "" {
			t.Fatalf("JSON success = code %d, stdout %q, stderr %q", code, stdout, stderr)
		}
		for _, want := range []string{
			"  \"schema_version\": 1,\n",
			"  \"report\": \"analysis\",\n",
			"  \"fingerprint_version\": 1,\n",
			"  \"source\": \"" + profilePath + "\",\n",
			"      \"kind\": \"chan_receive\",\n",
		} {
			if !strings.Contains(stdout, want) {
				t.Fatalf("JSON stdout missing %q:\n%s", want, stdout)
			}
		}
		if !strings.HasSuffix(stdout, "\n") {
			t.Fatalf("JSON stdout must end with LF: %q", stdout)
		}
	})

	t.Run("JSON profile error", func(t *testing.T) {
		path := writeCLIProfile(t, "wrong-json.pprof", cliProfileBytes(t, "goroutine"))
		stdout, stderr, code := invokeCLI(t, []string{"--json", path}, nil, nil)
		assertOperationalFailure(t, code, stdout, stderr, "parse profile")
	})

	t.Run("missing file", func(t *testing.T) {
		stdout, stderr, code := invokeCLI(t, []string{filepath.Join(t.TempDir(), "missing.pprof")}, nil, nil)
		assertOperationalFailure(t, code, stdout, stderr, "open file source")
	})

	t.Run("wrong profile type", func(t *testing.T) {
		path := writeCLIProfile(t, "wrong.pprof", cliProfileBytes(t, "goroutine"))
		stdout, stderr, code := invokeCLI(t, []string{path}, nil, nil)
		assertOperationalFailure(t, code, stdout, stderr, "parse profile")
		if !strings.Contains(stderr, "sample type") {
			t.Fatalf("wrong profile stderr = %q, want sample type detail", stderr)
		}
	})

	t.Run("HTTP status hides body and URL secrets", func(t *testing.T) {
		const bodySecret = "response-body-secret"
		const tokenSecret = "query-token-secret"
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(writer, bodySecret)
		}))
		defer server.Close()

		target := server.URL + "/custom/profile?token=" + tokenSecret + "#fragment-secret"
		stdout, stderr, code := invokeCLI(t, []string{target}, nil, nil)
		assertOperationalFailure(t, code, stdout, stderr, "HTTP status 503 Service Unavailable")
		for _, secret := range []string{bodySecret, tokenSecret, "fragment-secret"} {
			if strings.Contains(stderr, secret) {
				t.Fatalf("HTTP stderr leaked %q: %q", secret, stderr)
			}
		}
	})

	t.Run("report writer", func(t *testing.T) {
		profilePath := writeCLIProfile(t, "writer.pprof", cliProfileBytes(t, "goroutineleak"))
		var stderr bytes.Buffer
		code := Run(
			context.Background(),
			[]string{profilePath},
			nil,
			failingWriter{err: errors.New("writer failed")},
			&stderr,
		)
		if code != exitOperational || !strings.Contains(stderr.String(), "write text report: writer failed") || strings.Count(stderr.String(), "\n") != 1 {
			t.Fatalf("writer failure = code %d, stderr %q", code, stderr.String())
		}
	})

	t.Run("JSON report writer", func(t *testing.T) {
		profilePath := writeCLIProfile(t, "json-writer.pprof", cliProfileBytes(t, "goroutineleak"))
		var stderr bytes.Buffer
		code := Run(
			context.Background(),
			[]string{"--json", profilePath},
			nil,
			failingWriter{err: errors.New("writer failed")},
			&stderr,
		)
		if code != exitOperational || !strings.Contains(stderr.String(), "write JSON report: writer failed") || strings.Count(stderr.String(), "\n") != 1 {
			t.Fatalf("JSON writer failure = code %d, stderr %q", code, stderr.String())
		}
	})
}

func TestRunUsesIndependentFlagSets(t *testing.T) {
	profilePath := writeCLIProfile(t, "independent.pprof", cliProfileBytes(t, "goroutineleak"))

	_, _, firstCode := invokeCLI(t, []string{"--unknown"}, nil, nil)
	stdout, stderr, secondCode := invokeCLI(t, []string{profilePath}, nil, nil)
	if firstCode != exitUsage {
		t.Fatalf("first Run() code = %d, want %d", firstCode, exitUsage)
	}
	assertSuccessfulReport(t, secondCode, stdout, stderr, "independent.pprof")
}

func cliProfileBytes(t *testing.T, sampleType string) []byte {
	t.Helper()

	functions := []*pprofprofile.Function{
		{ID: 1, Name: "runtime.gopark", Filename: "/usr/local/go/src/runtime/proc.go"},
		{ID: 2, Name: "runtime.chanrecv1", Filename: "/usr/local/go/src/runtime/chan.go"},
		{ID: 3, Name: "example.com/default.wait", Filename: "/src/default.go"},
		{ID: 4, Name: "example.com/chosen.Run", Filename: "/src/chosen.go"},
	}
	locations := []*pprofprofile.Location{
		{ID: 1, Line: []pprofprofile.Line{{Function: functions[0], Line: 460}}},
		{ID: 2, Line: []pprofprofile.Line{{Function: functions[1], Line: 639}}},
		{ID: 3, Line: []pprofprofile.Line{{Function: functions[2], Line: 30}}},
		{ID: 4, Line: []pprofprofile.Line{{Function: functions[3], Line: 42}}},
	}
	profile := &pprofprofile.Profile{
		SampleType: []*pprofprofile.ValueType{{Type: sampleType, Unit: "count"}},
		Sample: []*pprofprofile.Sample{{
			Location: locations,
			Value:    []int64{2},
		}},
		Location: locations,
		Function: functions,
	}

	var output bytes.Buffer
	if err := profile.Write(&output); err != nil {
		t.Fatalf("serialize CLI profile: %v", err)
	}
	return output.Bytes()
}

func writeCLIProfile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write CLI profile: %v", err)
	}
	return path
}

func invokeCLI(t *testing.T, args []string, stdin io.Reader, stdout io.Writer) (string, string, int) {
	t.Helper()
	var stdoutBuffer bytes.Buffer
	if stdout == nil {
		stdout = &stdoutBuffer
	}
	var stderr bytes.Buffer
	code := Run(context.Background(), args, stdin, stdout, &stderr)
	return stdoutBuffer.String(), stderr.String(), code
}

func assertSuccessfulReport(t *testing.T, code int, stdout, stderr, sourceName string) {
	t.Helper()
	if code != exitSuccess {
		t.Fatalf("Run() code = %d, want %d; stderr=%q", code, exitSuccess, stderr)
	}
	if stderr != "" {
		t.Fatalf("success stderr = %q, want empty", stderr)
	}
	for _, want := range []string{
		"LEAKVIZ ANALYSIS\n",
		"Source: " + sourceName + "\n",
		"Total: 2\n",
		"  Blocker: chan_receive\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("success stdout missing %q:\n%s", want, stdout)
		}
	}
}

func assertOperationalFailure(t *testing.T, code int, stdout, stderr, want string) {
	t.Helper()
	if code != exitOperational {
		t.Fatalf("Run() code = %d, want %d; stdout=%q stderr=%q", code, exitOperational, stdout, stderr)
	}
	if stdout != "" {
		t.Fatalf("operational failure stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, want) || !strings.HasPrefix(stderr, "Error: ") || strings.Count(stderr, "\n") != 1 {
		t.Fatalf("operational stderr = %q, want one error line containing %q", stderr, want)
	}
}

type failingWriter struct {
	err error
}

func (writer failingWriter) Write([]byte) (int, error) {
	return 0, writer.err
}
