package profile

import (
	"bytes"
	"compress/gzip"
	"math"
	"reflect"
	"strings"
	"testing"

	pprofprofile "github.com/google/pprof/profile"
)

func TestParseGzipAndUncompressed(t *testing.T) {
	want := Snapshot{
		Source: "synthetic.pprof",
		Leaks: []Leak{{
			Count: 7,
			Stack: []Frame{
				{Function: "runtime.gopark", File: "/usr/local/go/src/runtime/proc.go", Line: 460},
				{Function: "github.com/acme/worker.receive", File: "/src/worker.go", Line: 41, Inlined: true},
				{Function: "github.com/acme/worker.(*Pool).run", File: "/src/worker.go", Line: 87},
			},
			Labels: LabelSet{"tenant": {"alpha", "beta"}},
		}},
	}

	for _, compressed := range []bool{true, false} {
		name := "uncompressed"
		if compressed {
			name = "gzip"
		}
		t.Run(name, func(t *testing.T) {
			data := writeProfile(t, syntheticProfile(7), compressed)
			got, err := Parse("synthetic.pprof", bytes.NewReader(data))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Parse() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestParseUsesGoroutineLeakSampleTypeIndex(t *testing.T) {
	p := syntheticProfile(0)
	p.SampleType = []*pprofprofile.ValueType{
		{Type: "alloc_space", Unit: "bytes"},
		{Type: "goroutineleak", Unit: "count"},
	}
	p.Sample[0].Value = []int64{999, 7}

	got, err := parseWrittenProfile(t, "index.pprof", p)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Leaks[0].Count != 7 {
		t.Fatalf("Count = %d, want 7", got.Leaks[0].Count)
	}
}

func TestParseRejectsInvalidSampleTypes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*pprofprofile.Profile)
	}{
		{
			name: "missing",
			mutate: func(p *pprofprofile.Profile) {
				p.SampleType = nil
				p.Sample = nil
			},
		},
		{
			name: "wrong",
			mutate: func(p *pprofprofile.Profile) {
				p.SampleType[0] = &pprofprofile.ValueType{Type: "goroutine", Unit: "count"}
			},
		},
		{
			name: "multiple",
			mutate: func(p *pprofprofile.Profile) {
				p.SampleType = append(p.SampleType, &pprofprofile.ValueType{Type: "goroutineleak", Unit: "count"})
				p.Sample[0].Value = append(p.Sample[0].Value, 7)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := syntheticProfile(7)
			test.mutate(p)
			_, err := parseWrittenProfile(t, "type.pprof", p)
			if err == nil || !strings.Contains(err.Error(), "sample type") {
				t.Fatalf("Parse() error = %v, want sample type error", err)
			}
		})
	}
}

func TestParseRejectsIncompleteSampleValues(t *testing.T) {
	p := syntheticProfile(7)
	p.SampleType = append(p.SampleType, &pprofprofile.ValueType{Type: "alloc_space", Unit: "bytes"})

	_, err := parseWrittenProfile(t, "values.pprof", p)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("Parse() error = %v, want incomplete values error", err)
	}
}

func TestParseRejectsNonzeroDuration(t *testing.T) {
	p := syntheticProfile(7)
	p.DurationNanos = 1

	_, err := parseWrittenProfile(t, "delta.pprof", p)
	if err == nil || !strings.Contains(err.Error(), "duration must be zero") {
		t.Fatalf("Parse() error = %v, want duration error", err)
	}
}

func TestParseCountValidation(t *testing.T) {
	t.Run("negative", func(t *testing.T) {
		_, err := parseWrittenProfile(t, "negative.pprof", syntheticProfile(-1))
		if err == nil || !strings.Contains(err.Error(), "negative") {
			t.Fatalf("Parse() error = %v, want negative count error", err)
		}
	})

	t.Run("zero ignored", func(t *testing.T) {
		p := syntheticProfile(0)
		p.Sample[0].Location = nil
		got, err := parseWrittenProfile(t, "zero.pprof", p)
		if err != nil {
			t.Fatalf("Parse() error = %v", err)
		}
		if got.Leaks == nil || len(got.Leaks) != 0 {
			t.Fatalf("Leaks = %#v, want non-nil empty slice", got.Leaks)
		}
	})

	t.Run("overflow", func(t *testing.T) {
		p := syntheticProfile(math.MaxInt64)
		p.Sample = append(p.Sample, &pprofprofile.Sample{
			Location: p.Sample[0].Location,
			Value:    []int64{1},
		})
		_, err := parseWrittenProfile(t, "overflow.pprof", p)
		if err == nil || !strings.Contains(err.Error(), "overflows") {
			t.Fatalf("Parse() error = %v, want overflow error", err)
		}
	})
}

func TestParseRejectsNumericLabels(t *testing.T) {
	p := syntheticProfile(7)
	p.Sample[0].NumLabel = map[string][]int64{"request_bytes": {128}}
	p.Sample[0].NumUnit = map[string][]string{"request_bytes": {"bytes"}}

	_, err := parseWrittenProfile(t, "numeric-label.pprof", p)
	if err == nil || !strings.Contains(err.Error(), "numeric labels") {
		t.Fatalf("Parse() error = %v, want numeric label error", err)
	}
}

func TestParseRejectsUnsymbolizedPositiveSamples(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*pprofprofile.Profile)
		wantError string
	}{
		{
			name: "missing line",
			mutate: func(p *pprofprofile.Profile) {
				p.Location[0].Address = 0x1234
				p.Location[0].Line = nil
			},
			wantError: "missing line",
		},
		{
			name: "missing function",
			mutate: func(p *pprofprofile.Profile) {
				p.Location[0].Line[0].Function = nil
			},
			wantError: "nil function",
		},
		{
			name: "empty function name",
			mutate: func(p *pprofprofile.Profile) {
				p.Function[0].Name = ""
			},
			wantError: "missing function name",
		},
		{
			name: "empty logical stack",
			mutate: func(p *pprofprofile.Profile) {
				p.Sample[0].Location = nil
			},
			wantError: "empty logical stack",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := syntheticProfile(7)
			test.mutate(p)
			_, err := parseWrittenProfile(t, "unsymbolized.pprof", p)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Parse() error = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func TestParseMalformedAndEmptyInput(t *testing.T) {
	tests := []struct {
		name      string
		data      []byte
		wantError string
	}{
		{name: "empty", data: nil, wantError: "empty profile input"},
		{name: "malformed", data: []byte("not a pprof profile"), wantError: "decode profile"},
		{name: "truncated gzip", data: []byte{0x1f, 0x8b}, wantError: "decompress profile"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse("invalid.pprof", bytes.NewReader(test.data))
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Parse() error = %v, want error containing %q", err, test.wantError)
			}
		})
	}
}

func TestParseValidEmptyProfile(t *testing.T) {
	p := &pprofprofile.Profile{
		SampleType: []*pprofprofile.ValueType{{Type: "goroutineleak", Unit: "count"}},
	}

	got, err := parseWrittenProfile(t, "empty.pprof", p)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got.Source != "empty.pprof" || got.Leaks == nil || len(got.Leaks) != 0 {
		t.Fatalf("Parse() = %#v, want named snapshot with non-nil empty leaks", got)
	}
}

func TestParseSizeLimits(t *testing.T) {
	if maxEncodedBytes != 64<<20 {
		t.Fatalf("maxEncodedBytes = %d, want %d", maxEncodedBytes, int64(64<<20))
	}
	if maxDecompressedBytes != 256<<20 {
		t.Fatalf("maxDecompressedBytes = %d, want %d", maxDecompressedBytes, int64(256<<20))
	}

	p := syntheticProfile(7)
	compressed := writeProfile(t, p, true)
	uncompressed := writeProfile(t, p, false)

	t.Run("encoded exact boundary", func(t *testing.T) {
		if _, err := parseWithLimits("boundary.pprof", bytes.NewReader(uncompressed), int64(len(uncompressed)), maxDecompressedBytes); err != nil {
			t.Fatalf("parseWithLimits() error = %v", err)
		}
	})

	t.Run("encoded over limit", func(t *testing.T) {
		_, err := parseWithLimits("large.pprof", bytes.NewReader(uncompressed), int64(len(uncompressed)-1), maxDecompressedBytes)
		if err == nil || !strings.Contains(err.Error(), "encoded profile exceeds") {
			t.Fatalf("parseWithLimits() error = %v, want encoded limit error", err)
		}
	})

	t.Run("decompressed exact boundary", func(t *testing.T) {
		if _, err := parseWithLimits("boundary.pprof", bytes.NewReader(compressed), int64(len(compressed)), int64(len(uncompressed))); err != nil {
			t.Fatalf("parseWithLimits() error = %v", err)
		}
	})

	t.Run("decompressed over limit", func(t *testing.T) {
		_, err := parseWithLimits("bomb.pprof", bytes.NewReader(compressed), int64(len(compressed)), int64(len(uncompressed)-1))
		if err == nil || !strings.Contains(err.Error(), "decompressed profile exceeds") {
			t.Fatalf("parseWithLimits() error = %v, want decompressed limit error", err)
		}
	})

	t.Run("high expansion gzip", func(t *testing.T) {
		bomb := gzipData(t, bytes.Repeat([]byte{0}, 4096))
		_, err := parseWithLimits("bomb.pprof", bytes.NewReader(bomb), int64(len(bomb)), 1024)
		if err == nil || !strings.Contains(err.Error(), "decompressed profile exceeds") {
			t.Fatalf("parseWithLimits() error = %v, want decompressed limit error", err)
		}
	})

	t.Run("nested gzip", func(t *testing.T) {
		nested := gzipData(t, compressed)
		_, err := parseWithLimits("nested.pprof", bytes.NewReader(nested), int64(len(nested)), int64(len(compressed)))
		if err == nil || !strings.Contains(err.Error(), "nested gzip") {
			t.Fatalf("parseWithLimits() error = %v, want nested gzip error", err)
		}
	})
}

func gzipData(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatalf("write gzip data: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}
	return buf.Bytes()
}

func TestConvertProfileDeepCopiesModel(t *testing.T) {
	p := syntheticProfile(7)
	got, err := convertProfile("deep-copy.pprof", p)
	if err != nil {
		t.Fatalf("convertProfile() error = %v", err)
	}

	p.Sample[0].Value[0] = 99
	p.Sample[0].Label["tenant"][0] = "changed"
	p.Sample[0].Label["new"] = []string{"value"}
	p.Function[0].Name = "changed.function"
	p.Function[0].Filename = "/changed.go"
	p.Location[0].Line[0].Line = 999
	p.Sample[0].Location = nil

	if got.Leaks[0].Count != 7 {
		t.Fatalf("Count = %d, want 7", got.Leaks[0].Count)
	}
	if got.Leaks[0].Labels["tenant"][0] != "alpha" || len(got.Leaks[0].Labels) != 1 {
		t.Fatalf("Labels mutated through source profile: %#v", got.Leaks[0].Labels)
	}
	if got.Leaks[0].Stack[0] != (Frame{Function: "runtime.gopark", File: "/usr/local/go/src/runtime/proc.go", Line: 460}) {
		t.Fatalf("Frame mutated through source profile: %#v", got.Leaks[0].Stack[0])
	}
}
