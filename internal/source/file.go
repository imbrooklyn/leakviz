package source

import (
	"fmt"
	"io"
	"os"
)

func (l Loader) openStdin() (Input, error) {
	if l.Stdin == nil {
		return Input{}, fmt.Errorf("open stdin source: nil reader")
	}

	return Input{
		Kind:        KindStdin,
		DisplayName: "-",
		Reader:      io.NopCloser(l.Stdin),
	}, nil
}

func openFile(target string) (Input, error) {
	file, err := os.Open(target)
	if err != nil {
		return Input{}, fmt.Errorf("open file source %q: %w", target, err)
	}

	return Input{
		Kind:        KindFile,
		DisplayName: target,
		Reader:      file,
	}, nil
}
