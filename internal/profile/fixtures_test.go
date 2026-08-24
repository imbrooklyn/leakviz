package profile

import (
	"bytes"
	"testing"

	pprofprofile "github.com/google/pprof/profile"
)

func syntheticProfile(count int64) *pprofprofile.Profile {
	leafFunction := &pprofprofile.Function{
		ID:       1,
		Name:     "runtime.gopark",
		Filename: "/usr/local/go/src/runtime/proc.go",
	}
	inlineFunction := &pprofprofile.Function{
		ID:       2,
		Name:     "github.com/acme/worker.receive",
		Filename: "/src/worker.go",
	}
	outerFunction := &pprofprofile.Function{
		ID:       3,
		Name:     "github.com/acme/worker.(*Pool).run",
		Filename: "/src/worker.go",
	}
	leafLocation := &pprofprofile.Location{
		ID: 1,
		Line: []pprofprofile.Line{{
			Function: leafFunction,
			Line:     460,
		}},
	}
	callerLocation := &pprofprofile.Location{
		ID: 2,
		Line: []pprofprofile.Line{
			{Function: inlineFunction, Line: 41},
			{Function: outerFunction, Line: 87},
		},
	}
	sample := &pprofprofile.Sample{
		Location: []*pprofprofile.Location{leafLocation, callerLocation},
		Value:    []int64{count},
		Label: map[string][]string{
			"tenant": {"alpha", "beta"},
		},
	}
	return &pprofprofile.Profile{
		SampleType: []*pprofprofile.ValueType{{Type: "goroutineleak", Unit: "count"}},
		Sample:     []*pprofprofile.Sample{sample},
		Location:   []*pprofprofile.Location{leafLocation, callerLocation},
		Function:   []*pprofprofile.Function{leafFunction, inlineFunction, outerFunction},
	}
}

func writeProfile(t *testing.T, p *pprofprofile.Profile, compressed bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	var err error
	if compressed {
		err = p.Write(&buf)
	} else {
		err = p.WriteUncompressed(&buf)
	}
	if err != nil {
		t.Fatalf("serialize synthetic profile: %v", err)
	}
	return buf.Bytes()
}

func parseWrittenProfile(t *testing.T, source string, p *pprofprofile.Profile) (Snapshot, error) {
	t.Helper()
	return Parse(source, bytes.NewReader(writeProfile(t, p, true)))
}
