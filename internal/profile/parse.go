package profile

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"

	pprofprofile "github.com/google/pprof/profile"
)

const (
	maxEncodedBytes      int64 = 64 << 20
	maxDecompressedBytes int64 = 256 << 20
)

// Parse decodes and validates a goroutineleak profile.
func Parse(source string, r io.Reader) (Snapshot, error) {
	return parseWithLimits(source, r, maxEncodedBytes, maxDecompressedBytes)
}

func parseWithLimits(source string, r io.Reader, encodedLimit, decompressedLimit int64) (Snapshot, error) {
	if r == nil {
		return Snapshot{}, fmt.Errorf("read encoded profile: nil reader")
	}

	encoded, tooLarge, err := readBounded(r, encodedLimit)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read encoded profile: %w", err)
	}
	if tooLarge {
		return Snapshot{}, fmt.Errorf("encoded profile exceeds %d-byte limit", encodedLimit)
	}
	if len(encoded) == 0 {
		return Snapshot{}, fmt.Errorf("empty profile input")
	}

	data := encoded
	if isGzip(encoded) {
		gz, err := gzip.NewReader(bytes.NewReader(encoded))
		if err != nil {
			return Snapshot{}, fmt.Errorf("decompress profile: %w", err)
		}

		data, tooLarge, err = readBounded(gz, decompressedLimit)
		closeErr := gz.Close()
		if err != nil {
			return Snapshot{}, fmt.Errorf("decompress profile: %w", err)
		}
		if tooLarge {
			return Snapshot{}, fmt.Errorf("decompressed profile exceeds %d-byte limit", decompressedLimit)
		}
		if closeErr != nil {
			return Snapshot{}, fmt.Errorf("decompress profile: %w", closeErr)
		}
		if len(data) == 0 {
			return Snapshot{}, fmt.Errorf("empty profile input")
		}
	}
	if isGzip(data) {
		return Snapshot{}, fmt.Errorf("nested gzip profile is unsupported")
	}

	p, err := pprofprofile.ParseData(data)
	if err != nil {
		return Snapshot{}, fmt.Errorf("decode profile: %w", err)
	}

	snapshot, err := convertProfile(source, p)
	if err != nil {
		return Snapshot{}, fmt.Errorf("validate profile: %w", err)
	}
	return snapshot, nil
}

func readBounded(r io.Reader, limit int64) ([]byte, bool, error) {
	if limit < 0 {
		return nil, false, fmt.Errorf("invalid negative size limit")
	}

	lr := &io.LimitedReader{R: r, N: limit + 1}
	data, err := io.ReadAll(lr)
	if err != nil {
		return nil, false, err
	}
	return data, int64(len(data)) > limit, nil
}

func isGzip(data []byte) bool {
	return len(data) >= 2 && data[0] == 0x1f && data[1] == 0x8b
}
