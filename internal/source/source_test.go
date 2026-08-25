package source

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOpenFileSuccessErrorAndClose(t *testing.T) {
	contents := []byte("serialized profile")
	path := filepath.Join(t.TempDir(), "snapshot.pprof")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	input, err := (Loader{}).Open(context.Background(), path)
	if err != nil {
		t.Fatalf("Open(file): %v", err)
	}
	if input.Kind != KindFile || input.DisplayName != path {
		t.Fatalf("Open(file) metadata = {%q, %q}, want {%q, %q}", input.Kind, input.DisplayName, KindFile, path)
	}
	got, err := io.ReadAll(input.Reader)
	if err != nil {
		t.Fatalf("ReadAll(file): %v", err)
	}
	if !bytes.Equal(got, contents) {
		t.Fatalf("ReadAll(file) = %q, want %q", got, contents)
	}
	if err := input.Reader.Close(); err != nil {
		t.Fatalf("Close(file): %v", err)
	}
	if _, err := input.Reader.Read(make([]byte, 1)); err == nil {
		t.Fatal("Read(closed file) succeeded, want error")
	}

	missing := filepath.Join(t.TempDir(), "missing.pprof")
	_, err = (Loader{}).Open(context.Background(), missing)
	if err == nil {
		t.Fatal("Open(missing file) succeeded, want error")
	}
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "open file source") {
		t.Fatalf("Open(missing file) error = %q", err)
	}
}

func TestOpenStdinUsesInjectedReaderAndNoOpClose(t *testing.T) {
	stdin := &closeTrackingReader{Reader: strings.NewReader("stdin profile")}
	input, err := (Loader{Stdin: stdin}).Open(context.Background(), "-")
	if err != nil {
		t.Fatalf("Open(stdin): %v", err)
	}
	if input.Kind != KindStdin || input.DisplayName != "-" {
		t.Fatalf("Open(stdin) metadata = {%q, %q}, want {%q, %q}", input.Kind, input.DisplayName, KindStdin, "-")
	}
	got, err := io.ReadAll(input.Reader)
	if err != nil {
		t.Fatalf("ReadAll(stdin): %v", err)
	}
	if got := string(got); got != "stdin profile" {
		t.Fatalf("ReadAll(stdin) = %q, want %q", got, "stdin profile")
	}
	if err := input.Reader.Close(); err != nil {
		t.Fatalf("Close(stdin): %v", err)
	}
	if stdin.closed {
		t.Fatal("Close(stdin input) closed the injected reader")
	}

	if _, err := (Loader{}).Open(context.Background(), "-"); err == nil {
		t.Fatal("Open(nil stdin) succeeded, want error")
	}
}

func TestSameNameFileRequiresExplicitPath(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile("localhost:6060", []byte("same-name file"), 0o600); err != nil {
		t.Fatalf("WriteFile(same-name): %v", err)
	}

	var requests atomic.Int64
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("HTTP body")),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}

	fileInput, err := (Loader{Client: client}).Open(context.Background(), "./localhost:6060")
	if err != nil {
		t.Fatalf("Open(explicit same-name file): %v", err)
	}
	fileContents, err := io.ReadAll(fileInput.Reader)
	if err != nil {
		t.Fatalf("ReadAll(explicit same-name file): %v", err)
	}
	if err := fileInput.Reader.Close(); err != nil {
		t.Fatalf("Close(explicit same-name file): %v", err)
	}
	if fileInput.Kind != KindFile || string(fileContents) != "same-name file" || requests.Load() != 0 {
		t.Fatalf("explicit same-name result = {%q, %q, requests=%d}", fileInput.Kind, fileContents, requests.Load())
	}

	httpInput, err := (Loader{Client: client}).Open(context.Background(), "localhost:6060")
	if err != nil {
		t.Fatalf("Open(bare same-name target): %v", err)
	}
	if err := httpInput.Reader.Close(); err != nil {
		t.Fatalf("Close(bare same-name HTTP body): %v", err)
	}
	if httpInput.Kind != KindHTTP || httpInput.DisplayName != "localhost:6060" || requests.Load() != 1 {
		t.Fatalf("bare same-name result = {%q, %q, requests=%d}", httpInput.Kind, httpInput.DisplayName, requests.Load())
	}
}

func TestTargetResolutionOrder(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   Kind
	}{
		{name: "stdin", target: "-", want: KindStdin},
		{name: "HTTP", target: "http://example.test", want: KindHTTP},
		{name: "HTTPS", target: "https://example.test/profile", want: KindHTTP},
		{name: "uppercase scheme", target: "HTTP://example.test", want: KindHTTP},
		{name: "localhost", target: "localhost:6060", want: KindHTTP},
		{name: "IPv4", target: "127.0.0.1:6060", want: KindHTTP},
		{name: "IPv6", target: "[::1]:6060", want: KindHTTP},
		{name: "same-name file", target: "./localhost:6060", want: KindFile},
		{name: "absolute same-name file", target: "/tmp/localhost:6060", want: KindFile},
		{name: "Windows path", target: `C:\profiles\snapshot.pprof`, want: KindFile},
		{name: "plain file", target: "snapshot.pprof", want: KindFile},
		{name: "unsupported scheme is a file", target: "ftp://example.test/profile", want: KindFile},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolvedKind(test.target); got != test.want {
				t.Fatalf("resolvedKind(%q) = %q, want %q", test.target, got, test.want)
			}
		})
	}
}

func TestOpenHTTPResolutionAndPathMatrix(t *testing.T) {
	tests := []struct {
		name        string
		target      string
		wantURL     string
		wantDisplay string
	}{
		{
			name:        "localhost",
			target:      "localhost:6060",
			wantURL:     "http://localhost:6060" + goroutineLeakPath,
			wantDisplay: "localhost:6060",
		},
		{
			name:        "IPv4",
			target:      "127.0.0.1:6060",
			wantURL:     "http://127.0.0.1:6060" + goroutineLeakPath,
			wantDisplay: "127.0.0.1:6060",
		},
		{
			name:        "IPv6",
			target:      "[::1]:6060",
			wantURL:     "http://[::1]:6060" + goroutineLeakPath,
			wantDisplay: "[::1]:6060",
		},
		{
			name:        "HTTP empty path",
			target:      "http://example.test",
			wantURL:     "http://example.test" + goroutineLeakPath,
			wantDisplay: "http://example.test",
		},
		{
			name:        "HTTP root path",
			target:      "http://example.test/",
			wantURL:     "http://example.test" + goroutineLeakPath,
			wantDisplay: "http://example.test/",
		},
		{
			name:        "HTTPS custom path",
			target:      "https://example.test/proxy/leaks?debug=0&token=request-secret#fragment-secret",
			wantURL:     "https://example.test/proxy/leaks?debug=0&token=request-secret",
			wantDisplay: "https://example.test/proxy/leaks",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requestedURL string
			body := &trackingBody{Reader: bytes.NewReader([]byte{0x1f, 0x8b, 0x08})}
			client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				requestedURL = request.URL.String()
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       body,
					Header:     make(http.Header),
					Request:    request,
				}, nil
			})}

			input, err := (Loader{Client: client}).Open(context.Background(), test.target)
			if err != nil {
				t.Fatalf("Open(%q): %v", test.target, err)
			}
			if requestedURL != test.wantURL {
				t.Fatalf("request URL = %q, want %q", requestedURL, test.wantURL)
			}
			if input.Kind != KindHTTP || input.DisplayName != test.wantDisplay {
				t.Fatalf("HTTP metadata = {%q, %q}, want {%q, %q}", input.Kind, input.DisplayName, KindHTTP, test.wantDisplay)
			}
			if body.closed {
				t.Fatal("successful HTTP body was closed before caller ownership")
			}
			if err := input.Reader.Close(); err != nil {
				t.Fatalf("Close(HTTP body): %v", err)
			}
			if !body.closed {
				t.Fatal("caller Close did not close successful HTTP body")
			}
		})
	}
}

func TestOpenHTTPAndHTTPSBinaryBodyWithHTTPTest(t *testing.T) {
	binaryBody := []byte{0x1f, 0x8b, 0x08, 0x00, 0xff, 0x00}

	for _, useTLS := range []bool{false, true} {
		name := "HTTP"
		if useTLS {
			name = "HTTPS"
		}
		t.Run(name, func(t *testing.T) {
			requested := make(chan string, 1)
			handler := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requested <- request.URL.RequestURI()
				writer.Header().Set("Content-Type", "application/octet-stream")
				_, _ = writer.Write(binaryBody)
			})

			var server *httptest.Server
			if useTLS {
				server = httptest.NewTLSServer(handler)
			} else {
				server = httptest.NewServer(handler)
			}
			defer server.Close()

			input, err := (Loader{Client: server.Client()}).Open(context.Background(), server.URL)
			if err != nil {
				t.Fatalf("Open(%s): %v", name, err)
			}
			defer input.Reader.Close()
			got, err := io.ReadAll(input.Reader)
			if err != nil {
				t.Fatalf("ReadAll(%s): %v", name, err)
			}
			if !bytes.Equal(got, binaryBody) {
				t.Fatalf("ReadAll(%s) = %v, want %v", name, got, binaryBody)
			}
			if gotPath := <-requested; gotPath != goroutineLeakPath {
				t.Fatalf("%s request URI = %q, want %q", name, gotPath, goroutineLeakPath)
			}
		})
	}
}

func TestOpenHTTPRejectsDebugAndSecondsBeforeRequest(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{name: "debug one", target: "http://example.test?debug=1", want: "debug query must be zero"},
		{name: "debug negative", target: "http://example.test?debug=-1", want: "debug query must be zero"},
		{name: "debug duplicate", target: "http://example.test?debug=0&debug=2", want: "debug query must be zero"},
		{name: "debug invalid", target: "http://example.test?debug=text", want: "debug query must be zero"},
		{name: "seconds value", target: "http://example.test?seconds=5", want: "seconds query is unsupported"},
		{name: "seconds empty", target: "http://example.test?seconds=", want: "seconds query is unsupported"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var requests atomic.Int64
			client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				requests.Add(1)
				return nil, errors.New("unexpected request")
			})}
			_, err := (Loader{Client: client}).Open(context.Background(), test.target)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Open(%q) error = %v, want %q", test.target, err, test.want)
			}
			if strings.Contains(err.Error(), "?") || requests.Load() != 0 {
				t.Fatalf("Open(%q) error/request count = %q/%d", test.target, err, requests.Load())
			}
		})
	}
}

func TestOpenHTTPNon2xxClosesBodyWithoutLeakingIt(t *testing.T) {
	body := &trackingBody{Reader: strings.NewReader("response-body-secret")}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTeapot,
			Status:     "418 response-body-secret",
			Body:       body,
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
	target := "http://user:password@example.test/custom?token=query-secret#fragment-secret"

	_, err := (Loader{Client: client}).Open(context.Background(), target)
	if err == nil {
		t.Fatal("Open(non-2xx) succeeded, want error")
	}
	if !body.closed {
		t.Fatal("Open(non-2xx) did not close response body")
	}
	if !strings.Contains(err.Error(), "418 I'm a teapot") {
		t.Fatalf("Open(non-2xx) error = %q, want canonical status", err)
	}
	assertOmitsSecrets(t, err.Error(), "user", "password", "query-secret", "fragment-secret", "response-body-secret")
}

func TestOpenHTTPSuccessSanitizesDisplayName(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("profile")),
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})}
	target := "http://display-user:display-password@example.test/custom?token=display-token#display-fragment"

	input, err := (Loader{Client: client}).Open(context.Background(), target)
	if err != nil {
		t.Fatalf("Open(sanitized display): %v", err)
	}
	defer input.Reader.Close()
	if input.DisplayName != "http://example.test/custom" {
		t.Fatalf("sanitized DisplayName = %q, want %q", input.DisplayName, "http://example.test/custom")
	}
	assertOmitsSecrets(t, input.DisplayName, "display-user", "display-password", "display-token", "display-fragment")
}

func TestOpenHTTPTimeoutIsSanitized(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	defer server.Close()

	client := server.Client()
	client.Timeout = 100 * time.Millisecond
	target := server.URL + "/custom?token=timeout-secret#timeout-fragment"
	_, err := (Loader{Client: client}).Open(context.Background(), target)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("timeout request did not reach the test server")
	}
	if err == nil {
		t.Fatal("Open(timeout) succeeded, want error")
	}
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("Open(timeout) error = %q", err)
	}
	assertOmitsSecrets(t, err.Error(), "timeout-secret", "timeout-fragment")
}

func TestOpenHTTPRedirectSuccessKeepsOriginalDisplayName(t *testing.T) {
	finalRequest := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/start":
			http.Redirect(writer, request, "/final?token=redirect-secret#redirect-fragment", http.StatusFound)
		case "/final":
			finalRequest <- request.URL.RequestURI()
			_, _ = writer.Write([]byte("binary profile"))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	target := server.URL + "/start?token=original-secret#original-fragment"
	input, err := (Loader{Client: server.Client()}).Open(context.Background(), target)
	if err != nil {
		t.Fatalf("Open(redirect): %v", err)
	}
	defer input.Reader.Close()
	if input.DisplayName != server.URL+"/start" {
		t.Fatalf("redirect DisplayName = %q, want %q", input.DisplayName, server.URL+"/start")
	}
	if got := <-finalRequest; got != "/final?token=redirect-secret" {
		t.Fatalf("redirect final RequestURI = %q", got)
	}
}

func TestOpenHTTPRedirectErrorSanitizesOriginalFinalAndPolicyError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		final := strings.Replace(serverURL(request), "://", "://redirect-user:redirect-password@", 1)
		http.Redirect(writer, request, final+"/final?token=final-secret#final-fragment", http.StatusFound)
	}))
	defer server.Close()

	client := server.Client()
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("policy-error-secret")
	}
	target := strings.Replace(server.URL, "://", "://original-user:original-password@", 1) +
		"/start?token=original-secret#original-fragment"

	_, err := (Loader{Client: client}).Open(context.Background(), target)
	if err == nil {
		t.Fatal("Open(rejected redirect) succeeded, want error")
	}
	if !strings.Contains(err.Error(), "HTTP redirect failed") || !strings.Contains(err.Error(), server.URL+"/final") {
		t.Fatalf("Open(rejected redirect) error = %q", err)
	}
	assertOmitsSecrets(t, err.Error(),
		"original-user", "original-password", "original-secret", "original-fragment",
		"redirect-user", "redirect-password", "final-secret", "final-fragment", "policy-error-secret",
	)
}

func TestOpenHTTPDefaultRedirectLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var step int
		_, _ = fmt.Sscanf(strings.TrimPrefix(request.URL.Path, "/"), "%d", &step)
		http.Redirect(writer, request, fmt.Sprintf("/%d?token=redirect-limit-secret", step+1), http.StatusFound)
	}))
	defer server.Close()

	_, err := (Loader{}).Open(context.Background(), server.URL+"/0?token=original-limit-secret")
	if err == nil || !strings.Contains(err.Error(), "HTTP redirect failed") {
		t.Fatalf("Open(default redirect limit) error = %v", err)
	}
	assertOmitsSecrets(t, err.Error(), "redirect-limit-secret", "original-limit-secret")
}

func TestOpenHTTPTransportErrorIsSanitized(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport-error-secret token=transport-token")
	})}
	target := "http://request-user:request-password@example.test/custom?token=request-token#request-fragment"

	_, err := (Loader{Client: client}).Open(context.Background(), target)
	if err == nil || !strings.Contains(err.Error(), "HTTP request failed") {
		t.Fatalf("Open(transport error) error = %v", err)
	}
	assertOmitsSecrets(t, err.Error(),
		"request-user", "request-password", "request-token", "request-fragment",
		"transport-error-secret", "transport-token",
	)
}

func resolvedKind(target string) Kind {
	switch {
	case target == "-":
		return KindStdin
	case hasHTTPScheme(target), isBareHostPort(target):
		return KindHTTP
	default:
		return KindFile
	}
}

func assertOmitsSecrets(t *testing.T, value string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(value, secret) {
			t.Errorf("value %q contains secret %q", value, secret)
		}
	}
}

func serverURL(request *http.Request) string {
	return "http://" + request.Host
}

type closeTrackingReader struct {
	io.Reader
	closed bool
}

func (reader *closeTrackingReader) Close() error {
	reader.closed = true
	return nil
}

type trackingBody struct {
	io.Reader
	closed bool
}

func (body *trackingBody) Close() error {
	body.closed = true
	return nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}
