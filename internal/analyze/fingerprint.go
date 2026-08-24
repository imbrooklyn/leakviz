package analyze

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"

	"github.com/imbrooklyn/leakviz/internal/profile"
)

const (
	exactFingerprintMarker    = "leakviz-exact-v1"
	semanticFingerprintMarker = "leakviz-semantic-v1"
	emptySemanticMarker       = "leakviz-empty-semantic-v1"
)

func exactFingerprint(stack []profile.Frame) (string, error) {
	preimage, err := exactPreimage(stack)
	if err != nil {
		return "", err
	}
	return hashPreimage(preimage), nil
}

func semanticFingerprint(blocker BlockerKind, functions []string) (string, error) {
	preimage, err := semanticPreimage(blocker, functions)
	if err != nil {
		return "", err
	}
	return hashPreimage(preimage), nil
}

func exactPreimage(stack []profile.Frame) ([]byte, error) {
	preimage, err := appendLengthPrefixed(nil, exactFingerprintMarker)
	if err != nil {
		return nil, err
	}
	preimage, err = appendCount(preimage, len(stack))
	if err != nil {
		return nil, fmt.Errorf("encode exact frame count: %w", err)
	}
	for frameIndex, frame := range stack {
		preimage, err = appendLengthPrefixed(preimage, frame.Function)
		if err != nil {
			return nil, fmt.Errorf("encode exact frame %d function: %w", frameIndex, err)
		}
		preimage, err = appendLengthPrefixed(preimage, frame.File)
		if err != nil {
			return nil, fmt.Errorf("encode exact frame %d file: %w", frameIndex, err)
		}
		preimage = appendUint64(preimage, uint64(frame.Line))
	}
	return preimage, nil
}

func semanticPreimage(blocker BlockerKind, functions []string) ([]byte, error) {
	preimage, err := appendLengthPrefixed(nil, semanticFingerprintMarker)
	if err != nil {
		return nil, err
	}
	preimage, err = appendLengthPrefixed(preimage, string(blocker))
	if err != nil {
		return nil, fmt.Errorf("encode semantic blocker: %w", err)
	}
	preimage, err = appendCount(preimage, len(functions))
	if err != nil {
		return nil, fmt.Errorf("encode semantic function count: %w", err)
	}
	if len(functions) == 0 {
		preimage, err = appendLengthPrefixed(preimage, emptySemanticMarker)
		if err != nil {
			return nil, fmt.Errorf("encode empty semantic marker: %w", err)
		}
		return preimage, nil
	}
	for functionIndex, function := range functions {
		preimage, err = appendLengthPrefixed(preimage, function)
		if err != nil {
			return nil, fmt.Errorf("encode semantic function %d: %w", functionIndex, err)
		}
	}
	return preimage, nil
}

func normalizeStack(stack []profile.Frame) []string {
	functions := make([]string, 0, len(stack))
	for _, frame := range stack {
		if !isSemanticPlumbing(frame.Function) {
			functions = append(functions, frame.Function)
		}
	}
	return functions
}

func isSemanticPlumbing(function string) bool {
	switch function {
	case "runtime.gopark",
		"runtime.block",
		"runtime.selectgo",
		"runtime.chanrecv",
		"runtime.chanrecv1",
		"runtime.chansend",
		"runtime.chansend1",
		"runtime.semacquire1",
		"sync.runtime_Semacquire",
		"sync.runtime_SemacquireWaitGroup",
		"sync.runtime_SemacquireRWMutex",
		"sync.runtime_SemacquireRWMutexR",
		"sync.runtime_notifyListWait",
		"internal/sync.runtime_SemacquireMutex",
		"internal/sync.(*Mutex).lockSlow",
		"sync.(*Mutex).Lock",
		"sync.(*RWMutex).Lock",
		"sync.(*RWMutex).RLock",
		"sync.(*Cond).Wait",
		"sync.(*WaitGroup).Wait":
		return true
	default:
		return false
	}
}

func appendLengthPrefixed(dst []byte, value string) ([]byte, error) {
	length, err := checkedLength(len(value))
	if err != nil {
		return nil, err
	}
	dst = appendUint64(dst, length)
	return append(dst, value...), nil
}

func appendCount(dst []byte, count int) ([]byte, error) {
	value, err := checkedLength(count)
	if err != nil {
		return nil, err
	}
	return appendUint64(dst, value), nil
}

func checkedLength(length int) (uint64, error) {
	if length < 0 {
		return 0, fmt.Errorf("negative length")
	}
	value := uint64(length)
	if int(value) != length {
		return 0, fmt.Errorf("length overflows uint64")
	}
	return value, nil
}

func appendUint64(dst []byte, value uint64) []byte {
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], value)
	return append(dst, encoded[:]...)
}

func hashPreimage(preimage []byte) string {
	digest := sha256.Sum256(preimage)
	return "sha256:" + hex.EncodeToString(digest[:])
}
