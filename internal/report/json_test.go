package report

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	pprofprofile "github.com/google/pprof/profile"

	"github.com/imbrooklyn/leakviz/internal/analyze"
	"github.com/imbrooklyn/leakviz/internal/profile"
)

func TestWriteJSONFullPipelineGolden(t *testing.T) {
	want := readGolden(t, "analysis.json")
	orders := [][]int{{0, 1}, {1, 0}}
	for orderIndex, order := range orders {
		analysis, _ := renderSerializedProfile(t, "localhost:6060", chanReceiveProfile(order))
		got := renderJSON(t, analysis)
		if got != want {
			t.Fatalf("WriteJSON(order %d) mismatch\ngot:\n%s\nwant:\n%s", orderIndex, got, want)
		}
		if repeated := renderJSON(t, analysis); repeated != got {
			t.Fatalf("WriteJSON(order %d) changed across identical runs", orderIndex)
		}
	}
}

func TestWriteJSONEmptyGolden(t *testing.T) {
	analysis, _ := renderSerializedProfile(t, "empty.pprof", &pprofprofile.Profile{
		SampleType: []*pprofprofile.ValueType{{Type: "goroutineleak", Unit: "count"}},
	})
	if got, want := renderJSON(t, analysis), readGolden(t, "analysis_empty.json"); got != want {
		t.Fatalf("WriteJSON(empty) mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteJSONUnknownAndNilUserFrameGolden(t *testing.T) {
	analysis, _ := renderSerializedProfile(t, "unknown.pprof", unknownProfile())
	if got, want := renderJSON(t, analysis), readGolden(t, "analysis_unknown.json"); got != want {
		t.Fatalf("WriteJSON(unknown) mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteJSONLabelsMissingAndMultiValue(t *testing.T) {
	analysis := analyze.Analysis{
		Source: "labels.pprof",
		Total:  3,
		Groups: []analyze.Group{{
			ExactFingerprint:    "sha256:exact",
			SemanticFingerprint: "sha256:semantic",
			Count:               3,
			Blocker: analyze.Blocker{
				Kind:             analyze.BlockerChanReceive,
				EvidenceFunction: "runtime.chanrecv1",
			},
			Stack: []profile.Frame{{Function: "runtime.chanrecv1", File: "/src/chan.go", Line: 1}},
			Labels: []analyze.LabelKeySummary{{
				Key:     "tenant",
				Present: 2,
				Missing: 1,
				Values: []analyze.LabelValueCount{
					{Value: "a", Count: 2},
					{Value: "b", Count: 2},
				},
			}},
		}},
	}

	got := renderJSON(t, analysis)
	want := `      "labels": [
        {
          "key": "tenant",
          "present": 2,
          "missing": 1,
          "values": [
            {
              "value": "a",
              "count": 2
            },
            {
              "value": "b",
              "count": 2
            }
          ]
        }
      ],
      "findings": []`
	if !strings.Contains(got, want) {
		t.Fatalf("WriteJSON(labels) missing exact summary\ngot:\n%s\nwant fragment:\n%s", got, want)
	}
}

func TestWriteJSONUnicodeAndDefaultHTMLEscaping(t *testing.T) {
	analysis := analyze.Analysis{
		Source: "快照<&>.pprof",
		Total:  1,
		Groups: []analyze.Group{{
			ExactFingerprint:    "sha256:例<&>",
			SemanticFingerprint: "sha256:语义",
			Count:               1,
			Blocker:             analyze.Blocker{Kind: analyze.BlockerUnknown},
			Stack: []profile.Frame{{
				Function: "example.com/例.<wait>&",
				File:     "/源/\u2028.go",
				Line:     7,
			}},
			Labels: []analyze.LabelKeySummary{{
				Key:     "租户",
				Present: 1,
				Values:  []analyze.LabelValueCount{{Value: "雪&<>", Count: 1}},
			}},
			Findings: []analyze.Finding{{
				Kind:    analyze.FindingInspect,
				Code:    "检查",
				Message: "Inspect <stack> & labels.",
			}},
		}},
	}

	got := renderJSON(t, analysis)
	for _, want := range []string{
		`"source": "快照\u003c\u0026\u003e.pprof"`,
		`"exact_fingerprint": "sha256:例\u003c\u0026\u003e"`,
		`"function": "example.com/例.\u003cwait\u003e\u0026"`,
		`"file": "/源/\u2028.go"`,
		`"value": "雪\u0026\u003c\u003e"`,
		`"message": "Inspect \u003cstack\u003e \u0026 labels."`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("WriteJSON() missing escaped Unicode/HTML fragment %q:\n%s", want, got)
		}
	}
}

func TestWriteJSONWriterErrors(t *testing.T) {
	sentinel := errors.New("writer failed")
	if err := WriteJSON(errorWriter{err: sentinel}, analyze.Analysis{}); !errors.Is(err, sentinel) {
		t.Fatalf("WriteJSON(error writer) error = %v, want wrapped sentinel", err)
	}
	if err := WriteJSON(shortWriter{}, analyze.Analysis{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("WriteJSON(short writer) error = %v, want io.ErrShortWrite", err)
	}
	if err := WriteJSON(nil, analyze.Analysis{}); err == nil || !strings.Contains(err.Error(), "nil writer") {
		t.Fatalf("WriteJSON(nil) error = %v, want nil writer error", err)
	}
}

func TestJSONDomainModelsHaveNoTags(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(analyze.Analysis{}),
		reflect.TypeOf(analyze.Group{}),
		reflect.TypeOf(analyze.Blocker{}),
		reflect.TypeOf(analyze.LabelKeySummary{}),
		reflect.TypeOf(analyze.LabelValueCount{}),
		reflect.TypeOf(analyze.Finding{}),
		reflect.TypeOf(profile.Frame{}),
	}
	for _, modelType := range types {
		for index := 0; index < modelType.NumField(); index++ {
			field := modelType.Field(index)
			if field.Tag.Get("json") != "" {
				t.Fatalf("domain field %s.%s has JSON tag %q", modelType, field.Name, field.Tag.Get("json"))
			}
		}
	}
}

func renderJSON(t *testing.T, analysis analyze.Analysis) string {
	t.Helper()
	var output bytes.Buffer
	if err := WriteJSON(&output, analysis); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	return output.String()
}
