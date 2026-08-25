package source

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const goroutineLeakPath = "/debug/pprof/goroutineleak"

var (
	errInvalidHTTPURL   = errors.New("invalid HTTP URL")
	errInvalidHTTPQuery = errors.New("invalid HTTP query")
	errInvalidDebug     = errors.New("debug query must be zero")
	errSecondsQuery     = errors.New("seconds query is unsupported")
	errNilContext       = errors.New("nil context")
	errHTTPRequest      = errors.New("HTTP request failed")
	errHTTPRedirect     = errors.New("HTTP redirect failed")
	errHTTPBody         = errors.New("HTTP response has no body")
)

func (l Loader) openHTTP(ctx context.Context, target string, bareHostPort bool) (Input, error) {
	displayName := sanitizeHTTPDisplay(target, bareHostPort)
	requestURL, err := parseHTTPURL(target)
	if err != nil {
		return Input{}, fmt.Errorf("open HTTP source %q: %w", displayName, err)
	}
	if ctx == nil {
		return Input{}, fmt.Errorf("open HTTP source %q: %w", displayName, errNilContext)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return Input{}, fmt.Errorf("open HTTP source %q: %w", displayName, errInvalidHTTPURL)
	}

	client := l.Client
	if client == nil {
		client = &http.Client{}
	}

	response, err := client.Do(request)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		return Input{}, sanitizedRequestError(displayName, request.URL, response, err)
	}
	if response.Body == nil {
		return Input{}, fmt.Errorf("open HTTP source %q: %w", displayName, errHTTPBody)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = response.Body.Close()
		return Input{}, fmt.Errorf(
			"open HTTP source %q: HTTP status %s",
			displayName,
			safeHTTPStatus(response.StatusCode),
		)
	}

	return Input{
		Kind:        KindHTTP,
		DisplayName: displayName,
		Reader:      response.Body,
	}, nil
}

func parseHTTPURL(target string) (*url.URL, error) {
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" {
		return nil, errInvalidHTTPURL
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return nil, errInvalidHTTPURL
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)

	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return nil, errInvalidHTTPQuery
	}
	if _, exists := query["seconds"]; exists {
		return nil, errSecondsQuery
	}
	for _, value := range query["debug"] {
		debug, err := strconv.ParseInt(value, 10, 64)
		if err != nil || debug != 0 {
			return nil, errInvalidDebug
		}
	}

	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = goroutineLeakPath
		parsed.RawPath = ""
	}
	parsed.Fragment = ""
	parsed.RawFragment = ""
	return parsed, nil
}

func sanitizedRequestError(displayName string, requested *url.URL, response *http.Response, requestErr error) error {
	if isTimeout(requestErr) {
		return fmt.Errorf("open HTTP source %q: HTTP request timed out: %w", displayName, context.DeadlineExceeded)
	}
	if errors.Is(requestErr, context.Canceled) {
		return fmt.Errorf("open HTTP source %q: HTTP request canceled: %w", displayName, context.Canceled)
	}

	requestedDisplay := sanitizeURL(requested)
	finalDisplay := requestErrorURL(requestErr)
	redirected := false
	if finalDisplay != "" && finalDisplay != requestedDisplay {
		redirected = true
	}
	if response != nil {
		redirected = true
		if finalDisplay == "" && response.Request != nil {
			finalDisplay = sanitizeURL(response.Request.URL)
		}
	}

	cause := errHTTPRequest
	if redirected {
		cause = errHTTPRedirect
	}
	if redirected && finalDisplay != "" {
		return fmt.Errorf("open HTTP source %q at final URL %q: %w", displayName, finalDisplay, cause)
	}
	return fmt.Errorf("open HTTP source %q: %w", displayName, cause)
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func requestErrorURL(err error) string {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) || urlErr.URL == "" {
		return ""
	}
	return sanitizeHTTPDisplay(urlErr.URL, false)
}

func safeHTTPStatus(code int) string {
	status := strconv.Itoa(code)
	if text := http.StatusText(code); text != "" {
		status += " " + text
	}
	return status
}

func sanitizeHTTPDisplay(target string, bareHostPort bool) string {
	parsed, err := url.Parse(target)
	if err == nil {
		if bareHostPort {
			return parsed.Host
		}
		return sanitizeURL(parsed)
	}

	redacted := redactMalformedURL(target)
	if bareHostPort {
		redacted = strings.TrimPrefix(redacted, "http://")
	}
	return redacted
}

func sanitizeURL(input *url.URL) string {
	if input == nil {
		return ""
	}

	safe := *input
	safe.User = nil
	safe.RawQuery = ""
	safe.ForceQuery = false
	safe.Fragment = ""
	safe.RawFragment = ""
	return safe.String()
}

func redactMalformedURL(target string) string {
	redacted := target
	if index := strings.IndexAny(redacted, "?#"); index >= 0 {
		redacted = redacted[:index]
	}

	scheme := strings.Index(redacted, "://")
	if scheme < 0 {
		return redacted
	}
	authorityStart := scheme + len("://")
	authorityEnd := len(redacted)
	if slash := strings.IndexByte(redacted[authorityStart:], '/'); slash >= 0 {
		authorityEnd = authorityStart + slash
	}
	authority := redacted[authorityStart:authorityEnd]
	if at := strings.LastIndexByte(authority, '@'); at >= 0 {
		redacted = redacted[:authorityStart] + authority[at+1:] + redacted[authorityEnd:]
	}
	return redacted
}
