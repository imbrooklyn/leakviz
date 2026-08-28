package report

import (
	"bytes"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode"

	pprofprofile "github.com/google/pprof/profile"

	"github.com/imbrooklyn/leakviz/internal/analyze"
	"github.com/imbrooklyn/leakviz/internal/profile"
)

func TestWriteTextFullPipelineGolden(t *testing.T) {
	want := readGolden(t, "chan_receive.txt")
	orders := [][]int{{0, 1}, {1, 0}}
	for orderIndex, order := range orders {
		analysis, got := renderSerializedProfile(t, "localhost:6060", chanReceiveProfile(order))
		if got != want {
			t.Fatalf("WriteText(order %d) mismatch\ngot:\n%s\nwant:\n%s", orderIndex, got, want)
		}
		_, repeated := renderSerializedProfile(t, "localhost:6060", chanReceiveProfile(order))
		if repeated != got {
			t.Fatalf("WriteText(order %d) changed across identical runs", orderIndex)
		}
		if analysis.Total != 22 || len(analysis.Groups) != 1 || analysis.Groups[0].Count != 22 {
			t.Fatalf("full pipeline analysis = %#v, want one 22-count group", analysis)
		}
		for _, finding := range analysis.Groups[0].Findings {
			if finding.Kind != analyze.FindingDetected {
				t.Fatalf("chan_receive finding = %#v, want detected facts only", finding)
			}
		}
	}
}

func TestWriteTextAllBlockersGolden(t *testing.T) {
	analysis, got := renderSerializedProfile(t, "all-blockers.pprof", allBlockersProfile())
	if want := readGolden(t, "all_blockers.txt"); got != want {
		t.Fatalf("WriteText(all blockers) mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}

	wantKinds := []analyze.BlockerKind{
		analyze.BlockerChanReceive,
		analyze.BlockerChanSend,
		analyze.BlockerSelect,
		analyze.BlockerMutex,
		analyze.BlockerRWMutex,
		analyze.BlockerCond,
		analyze.BlockerWaitGroup,
		analyze.BlockerUnknown,
	}
	if analysis.Total != int64(len(wantKinds)) || len(analysis.Groups) != len(wantKinds) {
		t.Fatalf("all-blocker analysis totals = total %d, groups %d, want %d of each", analysis.Total, len(analysis.Groups), len(wantKinds))
	}
	for index, wantKind := range wantKinds {
		group := analysis.Groups[index]
		if group.Blocker.Kind != wantKind {
			t.Fatalf("group %d blocker = %q, want %q", index, group.Blocker.Kind, wantKind)
		}
		for _, finding := range group.Findings {
			if finding.Kind == analyze.FindingPossibleCause {
				t.Fatalf("blocker %q produced an unsupported possible cause: %#v", wantKind, finding)
			}
			if strings.Contains(finding.Message, "Fix:") || strings.Contains(finding.Message, "close(") {
				t.Fatalf("blocker %q produced an automatic repair instruction: %#v", wantKind, finding)
			}
		}
	}
}

func TestWriteTextEmptyGolden(t *testing.T) {
	_, got := renderSerializedProfile(t, "empty.pprof", &pprofprofile.Profile{
		SampleType: []*pprofprofile.ValueType{{Type: "goroutineleak", Unit: "count"}},
	})
	if want := readGolden(t, "empty.txt"); got != want {
		t.Fatalf("WriteText(empty) mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestWriteTextUnknownGolden(t *testing.T) {
	analysis, got := renderSerializedProfile(t, "unknown.pprof", unknownProfile())
	if want := readGolden(t, "unknown.txt"); got != want {
		t.Fatalf("WriteText(unknown) mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
	if len(analysis.Groups) != 1 || analysis.Groups[0].UserFrame != nil {
		t.Fatalf("unknown analysis user frame = %#v, want nil", analysis.Groups)
	}
	wantFindings := []analyze.Finding{
		{
			Kind:    analyze.FindingDetected,
			Code:    "runtime_permanent_block",
			Message: "Runtime reported this goroutine as permanently blocked.",
		},
		{
			Kind:    analyze.FindingInspect,
			Code:    "unknown_blocker",
			Message: "No supported blocking primitive was identified; inspect the retained stack.",
		},
	}
	if !reflect.DeepEqual(analysis.Groups[0].Findings, wantFindings) {
		t.Fatalf("unknown findings = %#v, want %#v", analysis.Groups[0].Findings, wantFindings)
	}
}

func TestWriteTextInlineUserLocation(t *testing.T) {
	analysis, got := renderSerializedProfile(t, "/tmp/profiles/inline.pprof", inlineProfile())
	if len(analysis.Groups) != 1 {
		t.Fatalf("group count = %d, want 1", len(analysis.Groups))
	}
	group := analysis.Groups[0]
	if group.UserFrame == nil || group.UserFrame.Function != "github.com/acme/worker.receive" || !group.UserFrame.Inlined {
		t.Fatalf("UserFrame = %#v, want inline callee", group.UserFrame)
	}
	if len(group.Stack) != 4 || !group.Stack[2].Inlined || group.Stack[3].Inlined {
		t.Fatalf("inline stack = %#v, want inline callee then outer caller", group.Stack)
	}
	wantLines := []string{
		"Source: inline.pprof\n",
		"  User frame: worker.receive (worker.go:41)\n",
		"    - worker.receive (worker.go:41) [inlined]\n",
		"    - worker.(*Pool).run (worker.go:87)\n",
	}
	previous := -1
	for _, line := range wantLines {
		index := strings.Index(got, line)
		if index < 0 {
			t.Fatalf("WriteText(inline) missing %q in:\n%s", line, got)
		}
		if index <= previous {
			t.Fatalf("WriteText(inline) line %q is out of order", line)
		}
		previous = index
	}
}

func TestEscapeTextScalar(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "printable UTF-8", value: "plain 雪🙂", want: "plain 雪🙂"},
		{name: "backslashes", value: `C:\temp\profile`, want: `C:\\temp\\profile`},
		{
			name:  "controls",
			value: "line\nreturn\rcolumn\tend\x00\x1b\x7f\u0085\u2028\u2029\u202e\U0001d173",
			want:  `line\nreturn\rcolumn\tend\x00\x1b\x7f\u0085\u2028\u2029\u202e\U0001d173`,
		},
		{name: "invalid UTF-8", value: string([]byte{'a', 0xff, 0xc0, 'b'}), want: `a\xff\xc0b`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := escapeTextScalar(test.value); got != test.want {
				t.Fatalf("escapeTextScalar(%q) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}

func TestWriteTextEscapesSerializedProfileFields(t *testing.T) {
	input := chanReceiveProfile([]int{0})
	input.Function[3].Name = "github.com/acme/worker.run\n\x1b[31m\\tail"
	input.Function[3].Filename = "/src/worker\r\t\x1b[2J.go"
	input.Sample[0].Label = map[string][]string{
		"tenant\n\x1b": {"value\r\t\\path"},
	}

	source := "https://example.test/profile\n\x1b[2J" + string([]byte{0xff})
	analysis, got := renderSerializedProfile(t, source, input)
	for _, want := range []string{
		`Source: https://example.test/profile\n\x1b[2J\xff`,
		`  User frame: worker.run\n\x1b[31m\\tail (worker\r\t\x1b[2J.go:87)`,
		`    - tenant\n\x1b: present=10 missing=0`,
		`      - value\r\t\\path: 10`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("WriteText() missing escaped profile fragment %q:\n%s", want, got)
		}
	}
	assertTextContainsNoUnsafeControl(t, got)
	if repeated := renderAnalysis(t, analysis); repeated != got {
		t.Fatalf("WriteText() changed across identical escaped analysis runs")
	}
}

func TestWriteTextEscapesAllDynamicGroupScalars(t *testing.T) {
	userFrame := profile.Frame{Function: "example.test/user\nframe", File: "/src/user\r.go", Line: 7}
	analysis := analyze.Analysis{
		Source: "source.pprof",
		Total:  1,
		Groups: []analyze.Group{{
			Count:               1,
			ExactFingerprint:    "exact\n\x1b",
			SemanticFingerprint: "semantic\r\\value",
			Blocker: analyze.Blocker{
				Kind:             analyze.BlockerKind("custom\tblocker"),
				EvidenceFunction: "example.test/evidence\nfunction",
			},
			UserFrame: &userFrame,
			Stack:     []profile.Frame{userFrame},
			Labels: []analyze.LabelKeySummary{{
				Key:     "key\u2028value",
				Present: 1,
				Values:  []analyze.LabelValueCount{{Value: "label\u202evalue", Count: 1}},
			}},
			Findings: []analyze.Finding{{
				Kind:    analyze.FindingKind("custom\nkind"),
				Code:    "code\x00value",
				Message: "message\x1b[2J",
			}},
		}},
	}

	got := renderAnalysis(t, analysis)
	for _, want := range []string{
		`  Exact fingerprint: exact\n\x1b`,
		`  Semantic fingerprint: semantic\r\\value`,
		`  Blocker: custom\tblocker`,
		`  Evidence: evidence\nfunction`,
		`  User frame: user\nframe (user\r.go:7)`,
		`    - key\u2028value: present=1 missing=0`,
		`      - label\u202evalue: 1`,
		`    - custom\nkind code\x00value: message\x1b[2J`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("WriteText() missing escaped group fragment %q:\n%s", want, got)
		}
	}
	assertTextContainsNoUnsafeControl(t, got)
}

func TestTextDisplayHelpers(t *testing.T) {
	baseTests := []struct {
		name string
		path string
		want string
	}{
		{name: "Unix", path: "/var/tmp/snapshot.pprof", want: "snapshot.pprof"},
		{name: "Windows", path: `C:\profiles\snapshot.pprof`, want: "snapshot.pprof"},
		{name: "mixed", path: `C:\profiles/archive/snapshot.pprof`, want: "snapshot.pprof"},
		{name: "plain", path: "snapshot.pprof", want: "snapshot.pprof"},
		{name: "empty", path: "", want: "?"},
	}
	for _, test := range baseTests {
		t.Run("base "+test.name, func(t *testing.T) {
			if got := displayBase(test.path); got != test.want {
				t.Fatalf("displayBase(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}

	sourceTests := []struct {
		source string
		want   string
	}{
		{source: "/var/tmp/snapshot.pprof", want: "snapshot.pprof"},
		{source: `C:\profiles\snapshot.pprof`, want: "snapshot.pprof"},
		{source: "https://example.test/custom/profile", want: "https://example.test/custom/profile"},
		{source: "localhost:6060", want: "localhost:6060"},
		{source: "-", want: "-"},
		{source: "", want: "?"},
	}
	for _, test := range sourceTests {
		if got := displaySource(test.source); got != test.want {
			t.Errorf("displaySource(%q) = %q, want %q", test.source, got, test.want)
		}
		rendered := renderAnalysis(t, analyze.Analysis{Source: test.source, Groups: []analyze.Group{}})
		if !strings.Contains(rendered, "Source: "+test.want+"\n") {
			t.Errorf("WriteText(Source %q) did not render %q:\n%s", test.source, test.want, rendered)
		}
	}

	functionTests := []struct {
		function string
		want     string
	}{
		{function: "github.com/acme/worker.(*Pool).run", want: "worker.(*Pool).run"},
		{function: "github.com/acme/pipeline.Map[int].func1", want: "pipeline.Map[int].func1"},
		{function: "runtime.gopark", want: "runtime.gopark"},
		{function: "", want: ""},
	}
	for _, test := range functionTests {
		if got := shortFunction(test.function); got != test.want {
			t.Errorf("shortFunction(%q) = %q, want %q", test.function, got, test.want)
		}
	}
}

func TestWriteTextFindingKindsAndEmptyCollections(t *testing.T) {
	t.Run("finding kinds", func(t *testing.T) {
		analysis := analyze.Analysis{
			Source: "findings.pprof",
			Total:  1,
			Groups: []analyze.Group{{
				Count:               1,
				ExactFingerprint:    "exact",
				SemanticFingerprint: "semantic",
				Blocker:             analyze.Blocker{Kind: analyze.BlockerUnknown},
				Stack:               []profile.Frame{{Function: "pkg.wait", File: "/src/wait.go", Line: 1}},
				Findings: []analyze.Finding{
					{Kind: analyze.FindingDetected, Code: "fact", Message: "Observed fact."},
					{Kind: analyze.FindingPossibleCause, Code: "hypothesis", Message: "This may indicate contention."},
					{Kind: analyze.FindingInspect, Code: "next_step", Message: "Inspect the retained stack."},
				},
			}},
		}
		got := renderAnalysis(t, analysis)
		want := []string{
			"    - DETECTED fact: Observed fact.\n",
			"    - POSSIBLE_CAUSE hypothesis: This may indicate contention.\n",
			"    - INSPECT next_step: Inspect the retained stack.\n",
		}
		for _, line := range want {
			if !strings.Contains(got, line) {
				t.Fatalf("WriteText() missing %q in:\n%s", line, got)
			}
		}
	})

	t.Run("empty collections", func(t *testing.T) {
		analysis := analyze.Analysis{
			Source: "none.pprof",
			Total:  1,
			Groups: []analyze.Group{{
				Count:               1,
				ExactFingerprint:    "exact",
				SemanticFingerprint: "semantic",
				Blocker:             analyze.Blocker{Kind: analyze.BlockerUnknown},
				Stack:               []profile.Frame{{Function: "runtime.mystery", File: "", Line: 1}},
			}},
		}
		got := renderAnalysis(t, analysis)
		if !strings.Contains(got, "  Evidence: -\n  User frame: -\n") {
			t.Fatalf("WriteText() missing unknown placeholders:\n%s", got)
		}
		if !strings.Contains(got, "  Labels: none\n  Findings: none\n") {
			t.Fatalf("WriteText() missing canonical empty collections:\n%s", got)
		}
		if !strings.Contains(got, "    - runtime.mystery (?:1)\n") {
			t.Fatalf("WriteText() missing empty-path placeholder:\n%s", got)
		}
	})
}

func TestWriteTextWriterErrors(t *testing.T) {
	sentinel := errors.New("writer failed")
	if err := WriteText(errorWriter{err: sentinel}, analyze.Analysis{}); !errors.Is(err, sentinel) {
		t.Fatalf("WriteText(error writer) error = %v, want wrapped sentinel", err)
	}
	if err := WriteText(shortWriter{}, analyze.Analysis{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("WriteText(short writer) error = %v, want io.ErrShortWrite", err)
	}
	if err := WriteText(nil, analyze.Analysis{}); err == nil || !strings.Contains(err.Error(), "nil writer") {
		t.Fatalf("WriteText(nil) error = %v, want nil writer error", err)
	}
}

type errorWriter struct {
	err error
}

func (writer errorWriter) Write([]byte) (int, error) {
	return 0, writer.err
}

type shortWriter struct{}

func (shortWriter) Write(data []byte) (int, error) {
	return len(data) - 1, nil
}

func renderSerializedProfile(t *testing.T, source string, input *pprofprofile.Profile) (analyze.Analysis, string) {
	t.Helper()
	var encoded bytes.Buffer
	if err := input.Write(&encoded); err != nil {
		t.Fatalf("serialize synthetic profile: %v", err)
	}
	data := encoded.Bytes()
	if len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		t.Fatalf("Profile.Write output is not gzip data")
	}

	snapshot, err := profile.Parse(source, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	analysis, err := analyze.Analyze(snapshot, analyze.Options{})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	return analysis, renderAnalysis(t, analysis)
}

func renderAnalysis(t *testing.T, analysis analyze.Analysis) string {
	t.Helper()
	var output bytes.Buffer
	if err := WriteText(&output, analysis); err != nil {
		t.Fatalf("WriteText() error = %v", err)
	}
	return output.String()
}

func assertTextContainsNoUnsafeControl(t *testing.T, value string) {
	t.Helper()
	for offset, r := range value {
		if r == '\n' {
			continue
		}
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			t.Fatalf("text report contains unsafe control U+%04X at byte offset %d", r, offset)
		}
	}
}

func readGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("../../testdata/golden/" + name)
	if err != nil {
		t.Fatalf("read golden %q: %v", name, err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Fatalf("golden %q must end with one LF", name)
	}
	return string(data)
}

func chanReceiveProfile(order []int) *pprofprofile.Profile {
	functions := []*pprofprofile.Function{
		{ID: 1, Name: "runtime.gopark", Filename: "/usr/local/go/src/runtime/proc.go"},
		{ID: 2, Name: "runtime.chanrecv", Filename: "/usr/local/go/src/runtime/chan.go"},
		{ID: 3, Name: "runtime.chanrecv1", Filename: "/usr/local/go/src/runtime/chan.go"},
		{ID: 4, Name: "github.com/acme/worker.(*Pool).run", Filename: "/src/worker.go"},
	}
	locations := []*pprofprofile.Location{
		{ID: 1, Line: []pprofprofile.Line{{Function: functions[0], Line: 460}}},
		{ID: 2, Line: []pprofprofile.Line{{Function: functions[1], Line: 664}}},
		{ID: 3, Line: []pprofprofile.Line{{Function: functions[2], Line: 506}}},
		{ID: 4, Line: []pprofprofile.Line{{Function: functions[3], Line: 87}}},
	}
	type sampleSpec struct {
		count  int64
		tenant string
	}
	specs := []sampleSpec{{count: 10, tenant: "a"}, {count: 12, tenant: "b"}}
	samples := make([]*pprofprofile.Sample, 0, len(order))
	for _, index := range order {
		spec := specs[index]
		samples = append(samples, &pprofprofile.Sample{
			Location: locations,
			Value:    []int64{spec.count},
			Label:    map[string][]string{"tenant": {spec.tenant}},
		})
	}
	return &pprofprofile.Profile{
		SampleType: []*pprofprofile.ValueType{{Type: "goroutineleak", Unit: "count"}},
		Sample:     samples,
		Location:   locations,
		Function:   functions,
	}
}

func allBlockersProfile() *pprofprofile.Profile {
	type frameSpec struct {
		function string
		file     string
		line     int64
	}
	type sampleSpec struct {
		frames []frameSpec
	}

	runtimeFrame := frameSpec{
		function: "runtime.gopark",
		file:     "/usr/local/go/src/runtime/proc.go",
		line:     460,
	}
	specs := []sampleSpec{
		{frames: []frameSpec{
			runtimeFrame,
			{function: "sync.runtime_notifyListWait", file: "/usr/local/go/src/runtime/sema.go", line: 590},
			{function: "sync.(*Cond).Wait", file: "/usr/local/go/src/sync/cond.go", line: 71},
			{function: "github.com/acme/worker.waitCond", file: "/src/worker/cond.go", line: 31},
		}},
		{frames: []frameSpec{
			runtimeFrame,
			{function: "runtime.semacquire1", file: "/usr/local/go/src/runtime/sema.go", line: 192},
			{function: "sync.runtime_SemacquireWaitGroup", file: "/usr/local/go/src/runtime/sema.go", line: 110},
			{function: "sync.(*WaitGroup).Wait", file: "/usr/local/go/src/sync/waitgroup.go", line: 216},
			{function: "github.com/acme/worker.waitGroup", file: "/src/worker/waitgroup.go", line: 43},
		}},
		{frames: []frameSpec{
			runtimeFrame,
			{function: "runtime.semacquire1", file: "/usr/local/go/src/runtime/sema.go", line: 192},
			{function: "sync.runtime_SemacquireRWMutex", file: "/usr/local/go/src/runtime/sema.go", line: 105},
			{function: "sync.(*RWMutex).Lock", file: "/usr/local/go/src/sync/rwmutex.go", line: 151},
			{function: "github.com/acme/worker.lockShared", file: "/src/worker/rwmutex.go", line: 55},
		}},
		{frames: []frameSpec{
			runtimeFrame,
			{function: "runtime.semacquire1", file: "/usr/local/go/src/runtime/sema.go", line: 192},
			{function: "internal/sync.runtime_SemacquireMutex", file: "/usr/local/go/src/runtime/sema.go", line: 95},
			{function: "internal/sync.(*Mutex).lockSlow", file: "/usr/local/go/src/internal/sync/mutex.go", line: 149},
			{function: "sync.(*Mutex).Lock", file: "/usr/local/go/src/sync/mutex.go", line: 46},
			{function: "github.com/acme/worker.lock", file: "/src/worker/mutex.go", line: 67},
		}},
		{frames: []frameSpec{
			runtimeFrame,
			{function: "runtime.selectgo", file: "/usr/local/go/src/runtime/select.go", line: 122},
			{function: "github.com/acme/worker.waitSelect", file: "/src/worker/select.go", line: 79},
		}},
		{frames: []frameSpec{
			runtimeFrame,
			{function: "runtime.chansend", file: "/usr/local/go/src/runtime/chan.go", line: 171},
			{function: "runtime.chansend1", file: "/usr/local/go/src/runtime/chan.go", line: 156},
			{function: "github.com/acme/worker.send", file: "/src/worker/send.go", line: 91},
		}},
		{frames: []frameSpec{
			runtimeFrame,
			{function: "runtime.chanrecv", file: "/usr/local/go/src/runtime/chan.go", line: 664},
			{function: "runtime.chanrecv1", file: "/usr/local/go/src/runtime/chan.go", line: 506},
			{function: "github.com/acme/worker.receive", file: "/src/worker/receive.go", line: 103},
		}},
		{frames: []frameSpec{
			runtimeFrame,
			{function: "github.com/acme/worker.waitUnknown", file: "/src/worker/unknown.go", line: 115},
		}},
	}

	result := &pprofprofile.Profile{
		SampleType: []*pprofprofile.ValueType{{Type: "goroutineleak", Unit: "count"}},
		Sample:     make([]*pprofprofile.Sample, 0, len(specs)),
		Location:   make([]*pprofprofile.Location, 0),
		Function:   make([]*pprofprofile.Function, 0),
	}
	var functionID uint64
	var locationID uint64
	for _, spec := range specs {
		locations := make([]*pprofprofile.Location, 0, len(spec.frames))
		for _, frame := range spec.frames {
			functionID++
			function := &pprofprofile.Function{
				ID:       functionID,
				Name:     frame.function,
				Filename: frame.file,
			}
			locationID++
			location := &pprofprofile.Location{
				ID:   locationID,
				Line: []pprofprofile.Line{{Function: function, Line: frame.line}},
			}
			result.Function = append(result.Function, function)
			result.Location = append(result.Location, location)
			locations = append(locations, location)
		}
		result.Sample = append(result.Sample, &pprofprofile.Sample{
			Location: locations,
			Value:    []int64{1},
		})
	}
	return result
}

func unknownProfile() *pprofprofile.Profile {
	function := &pprofprofile.Function{
		ID:       1,
		Name:     "runtime.mystery",
		Filename: "/usr/local/go/src/runtime/mystery.go",
	}
	location := &pprofprofile.Location{
		ID:   1,
		Line: []pprofprofile.Line{{Function: function, Line: 1}},
	}
	return &pprofprofile.Profile{
		SampleType: []*pprofprofile.ValueType{{Type: "goroutineleak", Unit: "count"}},
		Sample: []*pprofprofile.Sample{{
			Location: []*pprofprofile.Location{location},
			Value:    []int64{1},
		}},
		Location: []*pprofprofile.Location{location},
		Function: []*pprofprofile.Function{function},
	}
}

func inlineProfile() *pprofprofile.Profile {
	functions := []*pprofprofile.Function{
		{ID: 1, Name: "runtime.gopark", Filename: "/usr/local/go/src/runtime/proc.go"},
		{ID: 2, Name: "runtime.chanrecv1", Filename: "/usr/local/go/src/runtime/chan.go"},
		{ID: 3, Name: "github.com/acme/worker.receive", Filename: "/src/worker.go"},
		{ID: 4, Name: "github.com/acme/worker.(*Pool).run", Filename: "/src/worker.go"},
	}
	locations := []*pprofprofile.Location{
		{ID: 1, Line: []pprofprofile.Line{{Function: functions[0], Line: 460}}},
		{ID: 2, Line: []pprofprofile.Line{{Function: functions[1], Line: 506}}},
		{ID: 3, Line: []pprofprofile.Line{
			{Function: functions[2], Line: 41},
			{Function: functions[3], Line: 87},
		}},
	}
	return &pprofprofile.Profile{
		SampleType: []*pprofprofile.ValueType{{Type: "goroutineleak", Unit: "count"}},
		Sample: []*pprofprofile.Sample{{
			Location: locations,
			Value:    []int64{1},
		}},
		Location: locations,
		Function: functions,
	}
}
