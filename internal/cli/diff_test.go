package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	pprofprofile "github.com/google/pprof/profile"
)

func TestRunDiffArgvContract(t *testing.T) {
	beforePath := writeCLIProfile(t, "before.pprof", diffProfileBytes(t, 2, 40, "a"))
	afterPath := writeCLIProfile(t, "after.pprof", diffProfileBytes(t, 3, 41, "b"))

	tests := []struct {
		name      string
		args      []string
		wantCode  int
		wantError string
		wantJSON  bool
	}{
		{
			name:     "canonical flags before operands",
			args:     []string{"diff", "--app=example.com/chosen", "--timeout=1s", beforePath, afterPath},
			wantCode: exitSuccess,
		},
		{
			name:     "standard single dash spellings",
			args:     []string{"diff", "-app", "example.com/chosen", "-timeout=1s", beforePath, afterPath},
			wantCode: exitSuccess,
		},
		{
			name:     "double dash terminates flags",
			args:     []string{"diff", "--", beforePath, afterPath},
			wantCode: exitSuccess,
		},
		{
			name:     "canonical JSON flag",
			args:     []string{"diff", "--json", beforePath, afterPath},
			wantCode: exitSuccess,
			wantJSON: true,
		},
		{
			name:     "standard single dash JSON spelling",
			args:     []string{"diff", "-json", beforePath, afterPath},
			wantCode: exitSuccess,
			wantJSON: true,
		},
		{
			name:      "trailing JSON rejected",
			args:      []string{"diff", beforePath, afterPath, "--json"},
			wantCode:  exitUsage,
			wantError: "expected exactly two source operands",
		},
		{
			name:      "trailing app rejected",
			args:      []string{"diff", beforePath, afterPath, "--app", "example.com/chosen"},
			wantCode:  exitUsage,
			wantError: "expected exactly two source operands",
		},
		{
			name:      "trailing timeout rejected",
			args:      []string{"diff", beforePath, afterPath, "--timeout=1s"},
			wantCode:  exitUsage,
			wantError: "expected exactly two source operands",
		},
		{
			name:      "unknown flag",
			args:      []string{"diff", "--unknown", beforePath, afterPath},
			wantCode:  exitUsage,
			wantError: "flag provided but not defined: -unknown",
		},
		{
			name:      "version not supported",
			args:      []string{"diff", "--version", beforePath, afterPath},
			wantCode:  exitUsage,
			wantError: "flag provided but not defined: -version",
		},
		{
			name:      "missing operands",
			args:      []string{"diff"},
			wantCode:  exitUsage,
			wantError: "expected exactly two source operands",
		},
		{
			name:      "one operand",
			args:      []string{"diff", beforePath},
			wantCode:  exitUsage,
			wantError: "expected exactly two source operands",
		},
		{
			name:      "extra operand",
			args:      []string{"diff", beforePath, afterPath, afterPath},
			wantCode:  exitUsage,
			wantError: "expected exactly two source operands",
		},
		{
			name:      "zero timeout",
			args:      []string{"diff", "--timeout=0", beforePath, afterPath},
			wantCode:  exitUsage,
			wantError: "timeout must be greater than zero",
		},
		{
			name:      "negative timeout",
			args:      []string{"diff", "--timeout=-1s", beforePath, afterPath},
			wantCode:  exitUsage,
			wantError: "timeout must be greater than zero",
		},
		{
			name:      "invalid timeout",
			args:      []string{"diff", "--timeout=soon", beforePath, afterPath},
			wantCode:  exitUsage,
			wantError: "invalid value",
		},
		{
			name:      "overflow timeout",
			args:      []string{"diff", "--timeout=999999999999999999999h", beforePath, afterPath},
			wantCode:  exitUsage,
			wantError: "invalid value",
		},
		{
			name:      "help with operands",
			args:      []string{"diff", "--help", beforePath, afterPath},
			wantCode:  exitUsage,
			wantError: "help cannot be combined",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := invokeCLI(t, test.args, nil, nil)
			if code != test.wantCode {
				t.Fatalf("Run() code = %d, want %d; stdout=%q stderr=%q", code, test.wantCode, stdout, stderr)
			}
			if test.wantCode == exitSuccess {
				wantPrefix := "LEAKVIZ DIFF\n"
				if test.wantJSON {
					wantPrefix = "{\n  \"schema_version\": 1,\n  \"report\": \"diff\",\n"
				}
				if stderr != "" || !strings.HasPrefix(stdout, wantPrefix) {
					t.Fatalf("success streams: stdout=%q stderr=%q", stdout, stderr)
				}
				return
			}
			if stdout != "" {
				t.Fatalf("usage failure stdout = %q, want empty", stdout)
			}
			if !strings.HasPrefix(stderr, diffUsage) || !strings.Contains(stderr, "Error: "+test.wantError) {
				t.Fatalf("usage failure stderr = %q, want diff usage and error containing %q", stderr, test.wantError)
			}
		})
	}

	stdout, stderr, code := invokeCLI(t, []string{"--json", "diff", beforePath, afterPath}, nil, nil)
	if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, analyzeUsage) || !strings.Contains(stderr, "expected exactly one source operand") {
		t.Fatalf("top-level flag dispatch = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestRunDiffHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		t.Run(arg, func(t *testing.T) {
			stdout, stderr, code := invokeCLI(t, []string{"diff", arg}, nil, nil)
			if code != exitSuccess || stdout != diffUsage || stderr != "" {
				t.Fatalf("Run(diff %s) = code %d, stdout %q, stderr %q", arg, code, stdout, stderr)
			}
		})
	}
}

func TestRunDiffFileStdinHTTPCombinations(t *testing.T) {
	beforeData := diffProfileBytes(t, 2, 40, "a")
	afterData := diffProfileBytes(t, 5, 41, "b")
	beforePath := writeCLIProfile(t, "before.pprof", beforeData)
	afterPath := writeCLIProfile(t, "after.pprof", afterData)

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/octet-stream")
		if strings.Contains(request.URL.Path, "before") {
			_, _ = writer.Write(beforeData)
			return
		}
		_, _ = writer.Write(afterData)
	}))
	defer server.Close()

	tests := []struct {
		name  string
		args  []string
		stdin io.Reader
	}{
		{name: "file file", args: []string{"diff", beforePath, afterPath}},
		{name: "stdin file", args: []string{"diff", "-", afterPath}, stdin: bytes.NewReader(beforeData)},
		{name: "file stdin", args: []string{"diff", beforePath, "-"}, stdin: bytes.NewReader(afterData)},
		{name: "stdin HTTP", args: []string{"diff", "--timeout=1s", "-", server.URL + "/after"}, stdin: bytes.NewReader(beforeData)},
		{name: "HTTP stdin", args: []string{"diff", "--timeout=1s", server.URL + "/before", "-"}, stdin: bytes.NewReader(afterData)},
		{name: "HTTP file", args: []string{"diff", "--timeout=1s", server.URL + "/before", afterPath}},
		{name: "file HTTP", args: []string{"diff", "--timeout=1s", beforePath, server.URL + "/after"}},
		{name: "HTTP HTTP", args: []string{"diff", "--timeout=1s", server.URL + "/before", server.URL + "/after"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, code := invokeCLI(t, test.args, test.stdin, nil)
			assertSuccessfulDiff(t, code, stdout, stderr)
			for _, want := range []string{
				"  Status: INCREASED\n",
				"  Before count: 2\n",
				"  After count: 5\n",
				"  Delta: +3\n",
			} {
				if !strings.Contains(stdout, want) {
					t.Fatalf("diff stdout missing %q:\n%s", want, stdout)
				}
			}
		})
	}
}

func TestRunDiffRejectsDoubleStdinBeforeRead(t *testing.T) {
	reader := &trackingReader{}
	stdout, stderr, code := invokeCLI(t, []string{"diff", "-", "-"}, reader, nil)
	if code != exitUsage || stdout != "" || !strings.HasPrefix(stderr, diffUsage) || !strings.Contains(stderr, "before and after cannot both use standard input") {
		t.Fatalf("double stdin = code %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	if reader.reads != 0 {
		t.Fatalf("double stdin read count = %d, want 0", reader.reads)
	}
}

func TestRunDiffHTTPReadsSequentiallyWithIndependentTimeouts(t *testing.T) {
	beforeData := diffProfileBytes(t, 2, 40, "a")
	afterData := diffProfileBytes(t, 3, 41, "b")
	var mu sync.Mutex
	var requestOrder []string
	active := 0
	concurrent := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		requestOrder = append(requestOrder, request.URL.Path)
		active++
		if active > 1 {
			concurrent = true
		}
		mu.Unlock()

		time.Sleep(300 * time.Millisecond)
		if request.URL.Path == "/before" {
			_, _ = writer.Write(beforeData)
		} else {
			_, _ = writer.Write(afterData)
		}

		mu.Lock()
		active--
		mu.Unlock()
	}))
	defer server.Close()

	started := time.Now()
	stdout, stderr, code := invokeCLI(
		t,
		[]string{"diff", "--timeout=500ms", server.URL + "/before", server.URL + "/after"},
		nil,
		nil,
	)
	elapsed := time.Since(started)
	assertSuccessfulDiff(t, code, stdout, stderr)
	mu.Lock()
	defer mu.Unlock()
	if concurrent {
		t.Fatal("HTTP sources were fetched concurrently")
	}
	if strings.Join(requestOrder, ",") != "/before,/after" {
		t.Fatalf("HTTP request order = %v, want before then after", requestOrder)
	}
	if elapsed < 550*time.Millisecond {
		t.Fatalf("HTTP diff completed in %v, want evidence of sequential reads", elapsed)
	}
}

func TestRunDiffAppPreference(t *testing.T) {
	profilePath := writeCLIProfile(t, "app.pprof", cliProfileBytes(t, "goroutineleak"))
	defaultOutput, defaultError, defaultCode := invokeCLI(t, []string{"diff", profilePath, profilePath}, nil, nil)
	assertSuccessfulDiff(t, defaultCode, defaultOutput, defaultError)
	if !strings.Contains(defaultOutput, "  Representative: default.wait (default.go:30)\n") {
		t.Fatalf("default diff missing default representative:\n%s", defaultOutput)
	}

	appOutput, appError, appCode := invokeCLI(
		t,
		[]string{"diff", "--app", "example.com/chosen", profilePath, profilePath},
		nil,
		nil,
	)
	assertSuccessfulDiff(t, appCode, appOutput, appError)
	if !strings.Contains(appOutput, "  Representative: chosen.Run (chosen.go:42)\n") {
		t.Fatalf("--app diff missing preferred representative:\n%s", appOutput)
	}
}

func TestRunDiffOperationalFailuresAndWriters(t *testing.T) {
	validPath := writeCLIProfile(t, "valid.pprof", diffProfileBytes(t, 2, 40, "a"))
	wrongPath := writeCLIProfile(t, "wrong.pprof", cliProfileBytes(t, "goroutine"))

	t.Run("before profile", func(t *testing.T) {
		stdout, stderr, code := invokeCLI(t, []string{"diff", wrongPath, validPath}, nil, nil)
		assertOperationalFailure(t, code, stdout, stderr, "analyze before source: parse profile")
	})
	t.Run("after profile", func(t *testing.T) {
		stdout, stderr, code := invokeCLI(t, []string{"diff", validPath, wrongPath}, nil, nil)
		assertOperationalFailure(t, code, stdout, stderr, "analyze after source: parse profile")
	})
	t.Run("nil context", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := Run(nil, []string{"diff", validPath, validPath}, nil, &stdout, &stderr)
		assertOperationalFailure(t, code, stdout.String(), stderr.String(), "run diff: nil context")
	})

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "text writer", args: []string{"diff", validPath, validPath}, want: "write text diff report: writer failed"},
		{name: "JSON writer", args: []string{"diff", "--json", validPath, validPath}, want: "write JSON diff report: writer failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stderr bytes.Buffer
			code := Run(context.Background(), test.args, nil, failingWriter{err: errors.New("writer failed")}, &stderr)
			if code != exitOperational || !strings.Contains(stderr.String(), test.want) || strings.Count(stderr.String(), "\n") != 1 {
				t.Fatalf("writer failure = code %d, stderr %q", code, stderr.String())
			}
		})
	}
}

func diffProfileBytes(t *testing.T, count, line int64, tenant string) []byte {
	t.Helper()
	functions := []*pprofprofile.Function{
		{ID: 1, Name: "runtime.gopark", Filename: "/usr/local/go/src/runtime/proc.go"},
		{ID: 2, Name: "runtime.chanrecv1", Filename: "/usr/local/go/src/runtime/chan.go"},
		{ID: 3, Name: "example.com/worker.wait", Filename: "/src/worker.go"},
	}
	locations := []*pprofprofile.Location{
		{ID: 1, Line: []pprofprofile.Line{{Function: functions[0], Line: 460}}},
		{ID: 2, Line: []pprofprofile.Line{{Function: functions[1], Line: 639}}},
		{ID: 3, Line: []pprofprofile.Line{{Function: functions[2], Line: line}}},
	}
	profile := &pprofprofile.Profile{
		SampleType: []*pprofprofile.ValueType{{Type: "goroutineleak", Unit: "count"}},
		Sample: []*pprofprofile.Sample{{
			Location: locations,
			Value:    []int64{count},
			Label:    map[string][]string{"tenant": {tenant}},
		}},
		Location: locations,
		Function: functions,
	}
	var output bytes.Buffer
	if err := profile.Write(&output); err != nil {
		t.Fatalf("serialize diff CLI profile: %v", err)
	}
	return output.Bytes()
}

func assertSuccessfulDiff(t *testing.T, code int, stdout, stderr string) {
	t.Helper()
	if code != exitSuccess || stderr != "" {
		t.Fatalf("diff = code %d, stderr %q, stdout %q", code, stderr, stdout)
	}
	for _, want := range []string{
		"LEAKVIZ DIFF\n",
		"Changes: 1\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("diff stdout missing %q:\n%s", want, stdout)
		}
	}
}

type trackingReader struct {
	reads int
}

func (reader *trackingReader) Read([]byte) (int, error) {
	reader.reads++
	return 0, io.EOF
}
