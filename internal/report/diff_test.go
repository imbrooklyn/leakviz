package report

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/imbrooklyn/leakviz/internal/analyze"
	analysisdiff "github.com/imbrooklyn/leakviz/internal/diff"
	"github.com/imbrooklyn/leakviz/internal/profile"
)

func TestWriteDiffTextMultiSiteGolden(t *testing.T) {
	result := normativeMultiSiteDiff(t)
	want := readGolden(t, "diff.txt")
	if got := renderDiffText(t, result); got != want {
		t.Fatalf("WriteDiffText() mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
	if repeated := renderDiffText(t, result); repeated != want {
		t.Fatalf("WriteDiffText() changed across identical runs")
	}
}

func TestWriteDiffJSONMultiSiteGolden(t *testing.T) {
	result := normativeMultiSiteDiff(t)
	want := readGolden(t, "diff.json")
	if got := renderDiffJSON(t, result); got != want {
		t.Fatalf("WriteDiffJSON() mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
	if repeated := renderDiffJSON(t, result); repeated != want {
		t.Fatalf("WriteDiffJSON() changed across identical runs")
	}
}

func TestWriteDiffAllStatusesGoldens(t *testing.T) {
	result := allStatusDiff()
	if got, want := renderDiffText(t, result), readGolden(t, "diff_statuses.txt"); got != want {
		t.Fatalf("WriteDiffText(statuses) mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
	if got, want := renderDiffJSON(t, result), readGolden(t, "diff_statuses.json"); got != want {
		t.Fatalf("WriteDiffJSON(statuses) mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteDiffEmptyGoldens(t *testing.T) {
	result := analysisdiff.Result{
		BeforeSource: "/tmp/before.pprof",
		AfterSource:  `C:\profiles\after.pprof`,
		Changes:      []analysisdiff.Change{},
	}
	if got, want := renderDiffText(t, result), readGolden(t, "diff_empty.txt"); got != want {
		t.Fatalf("WriteDiffText(empty) mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
	if got, want := renderDiffJSON(t, result), readGolden(t, "diff_empty.json"); got != want {
		t.Fatalf("WriteDiffJSON(empty) mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteDiffRepresentativeSelectionAndPaths(t *testing.T) {
	afterFrame := profile.Frame{Function: "example.com/worker.after", File: `C:\src\after.go`, Line: 22}
	beforeFrame := profile.Frame{Function: "example.com/worker.before", File: "/src/before.go", Line: 11}
	result := analysisdiff.Result{
		BeforeSource: "before.pprof",
		AfterSource:  "after.pprof",
		Changes: []analysisdiff.Change{
			{
				Status:       analysisdiff.StatusIncreased,
				BeforeGroups: []analyze.Group{},
				AfterGroups: []analyze.Group{{
					ExactFingerprint: "exact-after",
					UserFrame:        &afterFrame,
				}},
			},
			{
				Status: analysisdiff.StatusResolved,
				BeforeGroups: []analyze.Group{{
					ExactFingerprint: "exact-before",
					UserFrame:        &beforeFrame,
				}},
				AfterGroups: []analyze.Group{},
			},
			{
				Status:       analysisdiff.StatusNew,
				BeforeGroups: []analyze.Group{},
				AfterGroups:  []analyze.Group{{ExactFingerprint: "exact-no-frame"}},
			},
		},
	}

	got := renderDiffText(t, result)
	for _, want := range []string{
		"  Representative: worker.after (after.go:22)\n  Representative exact fingerprint: exact-after\n",
		"  Representative: worker.before (before.go:11)\n  Representative exact fingerprint: exact-before\n",
		"  Representative: -\n  Representative exact fingerprint: exact-no-frame\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("WriteDiffText() missing representative fragment %q:\n%s", want, got)
		}
	}
}

func TestWriteDiffTextEscapesDynamicScalars(t *testing.T) {
	userFrame := profile.Frame{
		Function: "example.test/diff\n\x1b[31m\\frame",
		File:     "/src/diff\r\t.go",
		Line:     22,
	}
	result := analysisdiff.Result{
		BeforeSource: "https://example.test/before\n\x1b[2J",
		AfterSource:  "/tmp/after\r\x1b.pprof",
		Changes: []analysisdiff.Change{{
			Status:              analysisdiff.Status("NEW\n\x1b"),
			SemanticFingerprint: "semantic\r\\value",
			AfterCount:          1,
			Delta:               1,
			AfterGroups: []analyze.Group{{
				ExactFingerprint: "exact\u2028value",
				UserFrame:        &userFrame,
			}},
		}},
	}

	got := renderDiffText(t, result)
	for _, want := range []string{
		`Before: https://example.test/before\n\x1b[2J (total=0)`,
		`After: after\r\x1b.pprof (total=0)`,
		`  Status: NEW\n\x1b`,
		`  Semantic fingerprint: semantic\r\\value`,
		`  Representative: diff\n\x1b[31m\\frame (diff\r\t.go:22)`,
		`  Representative exact fingerprint: exact\u2028value`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("WriteDiffText() missing escaped fragment %q:\n%s", want, got)
		}
	}
	assertTextContainsNoUnsafeControl(t, got)
}

func TestWriteDiffJSONUnicodeHTMLEscapingAndNilUserFrame(t *testing.T) {
	result := analysisdiff.Result{
		BeforeSource: "前<追踪>&.pprof",
		AfterSource:  "后.pprof",
		Changes: []analysisdiff.Change{{
			Status:              analysisdiff.StatusNew,
			SemanticFingerprint: "sha256:语义<&>",
			AfterCount:          1,
			Delta:               1,
			BeforeGroups:        []analyze.Group{},
			AfterGroups: []analyze.Group{{
				ExactFingerprint:    "sha256:位置<&>",
				SemanticFingerprint: "sha256:语义<&>",
				Count:               1,
				Blocker:             analyze.Blocker{Kind: analyze.BlockerUnknown},
				Stack:               []profile.Frame{},
				Labels:              []analyze.LabelKeySummary{},
				Findings:            []analyze.Finding{},
			}},
		}},
	}

	got := renderDiffJSON(t, result)
	for _, want := range []string{
		`"source": "前\u003c追踪\u003e\u0026.pprof"`,
		`"semantic_fingerprint": "sha256:语义\u003c\u0026\u003e"`,
		`"exact_fingerprint": "sha256:位置\u003c\u0026\u003e"`,
		`"user_frame": null`,
		`"stack": []`,
		`"labels": []`,
		`"findings": []`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("WriteDiffJSON() missing %q:\n%s", want, got)
		}
	}
}

func TestWriteDiffWriterErrors(t *testing.T) {
	sentinel := errors.New("writer failed")
	tests := []struct {
		name  string
		write func(io.Writer, analysisdiff.Result) error
	}{
		{name: "text", write: WriteDiffText},
		{name: "JSON", write: WriteDiffJSON},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.write(errorWriter{err: sentinel}, analysisdiff.Result{}); !errors.Is(err, sentinel) {
				t.Fatalf("error writer error = %v, want wrapped sentinel", err)
			}
			if err := test.write(shortWriter{}, analysisdiff.Result{}); !errors.Is(err, io.ErrShortWrite) {
				t.Fatalf("short writer error = %v, want io.ErrShortWrite", err)
			}
			if err := test.write(nil, analysisdiff.Result{}); err == nil || !strings.Contains(err.Error(), "nil writer") {
				t.Fatalf("nil writer error = %v, want nil writer error", err)
			}
		})
	}
}

func TestDiffDomainModelsHaveNoJSONTags(t *testing.T) {
	for _, modelType := range []reflect.Type{
		reflect.TypeOf(analysisdiff.Result{}),
		reflect.TypeOf(analysisdiff.Change{}),
	} {
		for index := 0; index < modelType.NumField(); index++ {
			field := modelType.Field(index)
			if field.Tag.Get("json") != "" {
				t.Fatalf("domain field %s.%s has JSON tag %q", modelType, field.Name, field.Tag.Get("json"))
			}
		}
	}
}

func normativeMultiSiteDiff(t *testing.T) analysisdiff.Result {
	t.Helper()
	const semantic = "sha256:8b77b951c4b986fa99dda710146d0e116cff968e21945bd11cfc8c289ba87d0c"
	before := analyze.Analysis{
		Source: "before.pprof",
		Total:  10,
		Groups: []analyze.Group{
			normativeDiffGroup(semantic, "sha256:b77d3c10d7bfa0320e061a9691d414806298d844d46d88581283c65d33f9003b", 4, 80),
			normativeDiffGroup(semantic, "sha256:f9539207a187cde6a8b712278dce01252a531e54a17866d3e80f9ba9f94685bc", 6, 87),
		},
	}
	after := analyze.Analysis{
		Source: "after.pprof",
		Total:  12,
		Groups: []analyze.Group{
			normativeDiffGroup(semantic, "sha256:6a9fa2b2f763527c0dbfbbb3e94e7fdbd1578fe891dca4ed58d9b362ef86e994", 12, 92),
		},
	}
	result, err := analysisdiff.Compare(before, after)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	return result
}

func normativeDiffGroup(semantic, exact string, count, line int64) analyze.Group {
	stack := []profile.Frame{
		{Function: "runtime.gopark", File: "/usr/local/go/src/runtime/proc.go", Line: 460},
		{Function: "runtime.chanrecv", File: "/usr/local/go/src/runtime/chan.go", Line: 664},
		{Function: "runtime.chanrecv1", File: "/usr/local/go/src/runtime/chan.go", Line: 506},
		{Function: "github.com/acme/worker.(*Pool).run", File: "/src/worker.go", Line: line},
	}
	userFrame := stack[len(stack)-1]
	return analyze.Group{
		ExactFingerprint:    exact,
		SemanticFingerprint: semantic,
		Count:               count,
		Blocker: analyze.Blocker{
			Kind:             analyze.BlockerChanReceive,
			EvidenceFunction: "runtime.chanrecv1",
		},
		UserFrame: &userFrame,
		Stack:     stack,
		Labels: []analyze.LabelKeySummary{{
			Key:     "tenant",
			Present: count,
			Values:  []analyze.LabelValueCount{{Value: "a", Count: count}},
		}},
		Findings: []analyze.Finding{
			{
				Kind:    analyze.FindingDetected,
				Code:    "blocking_primitive",
				Message: "Blocking primitive: chan_receive (runtime.chanrecv1).",
			},
			{
				Kind:    analyze.FindingDetected,
				Code:    "runtime_permanent_block",
				Message: "Runtime reported this goroutine as permanently blocked.",
			},
		},
	}
}

func allStatusDiff() analysisdiff.Result {
	emptyGroups := func() []analyze.Group { return []analyze.Group{} }
	return analysisdiff.Result{
		BeforeSource: `C:\profiles\before.pprof`,
		AfterSource:  "/tmp/after.pprof",
		BeforeTotal:  16,
		AfterTotal:   15,
		Changes: []analysisdiff.Change{
			{Status: analysisdiff.StatusNew, SemanticFingerprint: "semantic-new", AfterCount: 4, Delta: 4, BeforeGroups: emptyGroups(), AfterGroups: emptyGroups()},
			{Status: analysisdiff.StatusIncreased, SemanticFingerprint: "semantic-increased", BeforeCount: 2, AfterCount: 5, Delta: 3, BeforeGroups: emptyGroups(), AfterGroups: emptyGroups()},
			{Status: analysisdiff.StatusDecreased, SemanticFingerprint: "semantic-decreased", BeforeCount: 5, AfterCount: 2, Delta: -3, BeforeGroups: emptyGroups(), AfterGroups: emptyGroups()},
			{Status: analysisdiff.StatusResolved, SemanticFingerprint: "semantic-resolved", BeforeCount: 4, Delta: -4, BeforeGroups: emptyGroups(), AfterGroups: emptyGroups()},
			{Status: analysisdiff.StatusUnchanged, SemanticFingerprint: "semantic-unchanged", BeforeCount: 5, AfterCount: 5, BeforeGroups: emptyGroups(), AfterGroups: emptyGroups()},
		},
	}
}

func renderDiffText(t *testing.T, result analysisdiff.Result) string {
	t.Helper()
	var output bytes.Buffer
	if err := WriteDiffText(&output, result); err != nil {
		t.Fatalf("WriteDiffText() error = %v", err)
	}
	return output.String()
}

func renderDiffJSON(t *testing.T, result analysisdiff.Result) string {
	t.Helper()
	var output bytes.Buffer
	if err := WriteDiffJSON(&output, result); err != nil {
		t.Fatalf("WriteDiffJSON() error = %v", err)
	}
	return output.String()
}
