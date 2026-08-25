// Package source acquires profile byte streams from files, stdin, and HTTP.
package source

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
)

// Kind identifies how an input was acquired.
type Kind string

const (
	KindStdin Kind = "stdin"
	KindFile  Kind = "file"
	KindHTTP  Kind = "http"
)

// Input is an acquired byte stream. The caller must close Reader.
type Input struct {
	Kind        Kind
	DisplayName string
	Reader      io.ReadCloser
}

// Loader acquires profile byte streams using injected process and HTTP inputs.
type Loader struct {
	Stdin  io.Reader
	Client *http.Client
}

// Open resolves target and acquires its byte stream.
func (l Loader) Open(ctx context.Context, target string) (Input, error) {
	switch {
	case target == "-":
		return l.openStdin()
	case hasHTTPScheme(target):
		return l.openHTTP(ctx, target, false)
	case isBareHostPort(target):
		return l.openHTTP(ctx, "http://"+target, true)
	default:
		return openFile(target)
	}
}

func hasHTTPScheme(target string) bool {
	separator := strings.Index(target, "://")
	if separator < 0 {
		return false
	}

	scheme := target[:separator]
	return strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https")
}

func isBareHostPort(target string) bool {
	// Path separators make the target an explicitly qualified file name.
	if strings.ContainsAny(target, `/\`) {
		return false
	}

	_, _, err := net.SplitHostPort(target)
	if err != nil {
		return false
	}

	return true
}
