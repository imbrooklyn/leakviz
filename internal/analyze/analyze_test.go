package analyze

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/imbrooklyn/leakviz/internal/profile"
)

func TestClassifyChanReceive(t *testing.T) {
	stack := chanReceiveStack()
	got := classify(stack)
	want := Blocker{
		Kind:             BlockerChanReceive,
		EvidenceFunction: "runtime.chanrecv1",
	}
	if got != want {
		t.Fatalf("classify() = %#v, want %#v", got, want)
	}
	userFrame := selectUserFrame(stack, Options{})
	if userFrame == nil || userFrame.Function != "github.com/acme/worker.(*Pool).run" {
		t.Fatalf("selectUserFrame() = %#v, want vertical-slice user frame", userFrame)
	}

	for _, symbol := range []string{"runtime.chanrecv", "runtime.chanrecv1"} {
		t.Run(symbol, func(t *testing.T) {
			got := classify([]profile.Frame{{Function: symbol}})
			if got.Kind != BlockerChanReceive || got.EvidenceFunction != symbol {
				t.Fatalf("classify(%q) = %#v", symbol, got)
			}
		})
	}
}

func TestClassifyUsesOnlyEnabledExactSymbols(t *testing.T) {
	tests := []string{
		"runtime.chanrecv2",
		"runtime.chanrecv.func1",
		"github.com/acme/runtime.chanrecv",
		"github.com/acme/worker.chanrecv1",
		"runtime.chansend",
		"runtime.selectgo",
		"sync.(*Mutex).Lock",
	}
	for _, function := range tests {
		t.Run(function, func(t *testing.T) {
			got := classify([]profile.Frame{{Function: function}})
			if got != (Blocker{Kind: BlockerUnknown}) {
				t.Fatalf("classify(%q) = %#v, want unknown", function, got)
			}
		})
	}
}

func TestSelectUserFrame(t *testing.T) {
	t.Run("app prefix package boundary", func(t *testing.T) {
		stack := []profile.Frame{
			{Function: "runtime.gopark", File: "/runtime/proc.go", Line: 1},
			{Function: "github.com/acme/service2.Run", File: "/src/service2.go", Line: 2},
			{Function: "github.com/acme/service/worker.Run", File: "/src/worker.go", Line: 3, Inlined: true},
			{Function: "github.com/acme/service.Main", File: "/src/main.go", Line: 4},
		}
		got := selectUserFrame(stack, Options{AppPrefix: "github.com/acme/service"})
		if got == nil || got.Function != "github.com/acme/service/worker.Run" {
			t.Fatalf("selectUserFrame() = %#v, want first package-boundary match", got)
		}

		stack[2].Function = "changed"
		stack[2].File = "changed"
		if got.Function != "github.com/acme/service/worker.Run" || got.File != "/src/worker.go" {
			t.Fatalf("selected frame aliases input stack: %#v", got)
		}
	})

	t.Run("missing app prefix falls back", func(t *testing.T) {
		stack := []profile.Frame{
			{Function: "runtime.gopark"},
			{Function: "github.com/acme/default.Run", File: "/src/default.go", Line: 8},
		}
		got := selectUserFrame(stack, Options{AppPrefix: "github.com/missing"})
		if got == nil || got.Function != "github.com/acme/default.Run" {
			t.Fatalf("selectUserFrame() = %#v, want default fallback", got)
		}
	})

	t.Run("default skips plumbing", func(t *testing.T) {
		stack := []profile.Frame{
			{Function: "runtime.gopark"},
			{Function: "internal/runtime/maps.get"},
			{Function: "sync.(*Mutex).Lock"},
			{Function: "internal/sync.(*Mutex).lockSlow"},
			{Function: "github.com/acme/worker.Run", File: "/src/worker.go", Line: 9},
		}
		got := selectUserFrame(stack, Options{})
		if got == nil || got.Function != "github.com/acme/worker.Run" {
			t.Fatalf("selectUserFrame() = %#v, want first non-plumbing frame", got)
		}
	})

	t.Run("no user frame", func(t *testing.T) {
		stack := []profile.Frame{
			{Function: "runtime.gopark"},
			{Function: "sync.(*Cond).Wait"},
		}
		if got := selectUserFrame(stack, Options{}); got != nil {
			t.Fatalf("selectUserFrame() = %#v, want nil", got)
		}
	})
}

func TestMatchesPackageBoundary(t *testing.T) {
	tests := []struct {
		name     string
		function string
		prefix   string
		want     bool
	}{
		{name: "package function", function: "github.com/acme/service.Run", prefix: "github.com/acme/service", want: true},
		{name: "subpackage", function: "github.com/acme/service/worker.Run", prefix: "github.com/acme/service", want: true},
		{name: "method", function: "github.com/acme/service.(*Worker).Run", prefix: "github.com/acme/service", want: true},
		{name: "exact", function: "github.com/acme/service", prefix: "github.com/acme/service", want: true},
		{name: "similar package", function: "github.com/acme/service2.Run", prefix: "github.com/acme/service", want: false},
		{name: "partial segment", function: "github.com/acme/services/worker.Run", prefix: "github.com/acme/service", want: false},
		{name: "empty prefix", function: "github.com/acme/service.Run", prefix: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesPackageBoundary(test.function, test.prefix); got != test.want {
				t.Fatalf("matchesPackageBoundary(%q, %q) = %t, want %t", test.function, test.prefix, got, test.want)
			}
		})
	}
}

func TestNormalizeStack(t *testing.T) {
	t.Run("vertical slice", func(t *testing.T) {
		got := normalizeStack(chanReceiveStack())
		want := []string{"github.com/acme/worker.(*Pool).run"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("normalizeStack() = %#v, want %#v", got, want)
		}
	})

	t.Run("fixed plumbing set", func(t *testing.T) {
		plumbing := []string{
			"runtime.gopark",
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
			"sync.(*WaitGroup).Wait",
		}
		stack := make([]profile.Frame, len(plumbing))
		for i, function := range plumbing {
			stack[i].Function = function
		}
		got := normalizeStack(stack)
		if got == nil || len(got) != 0 {
			t.Fatalf("normalizeStack() = %#v, want non-nil empty slice", got)
		}
	})

	t.Run("generic closure inline and exact matching", func(t *testing.T) {
		stack := []profile.Frame{
			{Function: "runtime.chanrecv", Inlined: true},
			{Function: "github.com/acme/pipeline.Map[int]", Inlined: true},
			{Function: "github.com/acme/pipeline.Map[int].func1"},
			{Function: "runtime.chanrecv2"},
			{Function: "例/包.函数"},
		}
		want := []string{
			"github.com/acme/pipeline.Map[int]",
			"github.com/acme/pipeline.Map[int].func1",
			"runtime.chanrecv2",
			"例/包.函数",
		}
		if got := normalizeStack(stack); !reflect.DeepEqual(got, want) {
			t.Fatalf("normalizeStack() = %#v, want %#v", got, want)
		}
	})

	t.Run("empty", func(t *testing.T) {
		got := normalizeStack(nil)
		if got == nil || len(got) != 0 {
			t.Fatalf("normalizeStack(nil) = %#v, want non-nil empty slice", got)
		}
	})
}

func TestAppPrefixOnlyAffectsUserFrame(t *testing.T) {
	stack := []profile.Frame{
		{Function: "runtime.gopark", File: "/runtime/proc.go", Line: 1},
		{Function: "github.com/other/library.Wait", File: "/other/wait.go", Line: 2},
		{Function: "github.com/acme/service.Run", File: "/app/run.go", Line: 3},
	}
	type result struct {
		userFrame           *profile.Frame
		exactFingerprint    string
		semanticFingerprint string
	}
	analyzeStack := func(opts Options) result {
		exact, semantic := fingerprintsForTest(t, stack, classify(stack).Kind)
		return result{
			userFrame:           selectUserFrame(stack, opts),
			exactFingerprint:    exact,
			semanticFingerprint: semantic,
		}
	}
	defaultResult := analyzeStack(Options{})
	appResult := analyzeStack(Options{AppPrefix: "github.com/acme/service"})
	if defaultResult.userFrame == nil || appResult.userFrame == nil || defaultResult.userFrame.Function == appResult.userFrame.Function {
		t.Fatalf("UserFrame choices = %#v and %#v, want distinct frames", defaultResult.userFrame, appResult.userFrame)
	}
	if defaultResult.exactFingerprint != appResult.exactFingerprint || defaultResult.semanticFingerprint != appResult.semanticFingerprint {
		t.Fatalf("AppPrefix changed fingerprints: %#v != %#v", defaultResult, appResult)
	}
}

func chanReceiveStack() []profile.Frame {
	return []profile.Frame{
		{Function: "runtime.gopark", File: "/usr/local/go/src/runtime/proc.go", Line: 460},
		{Function: "runtime.chanrecv", File: "/usr/local/go/src/runtime/chan.go", Line: 664},
		{Function: "runtime.chanrecv1", File: "/usr/local/go/src/runtime/chan.go", Line: 506},
		{Function: "github.com/acme/worker.(*Pool).run", File: "/src/worker.go", Line: 87},
	}
}

func TestAnalyzeGroupsExactStacksAndAggregatesLabels(t *testing.T) {
	stack := chanReceiveStack()
	snapshot := profile.Snapshot{
		Source: "snapshot.pprof",
		Leaks: []profile.Leak{
			{
				Count: 10,
				Stack: stack,
				Labels: profile.LabelSet{
					"empty":  nil,
					"region": {"west"},
					"tenant": {"a", "a", "b"},
				},
			},
			{
				Count: 12,
				Stack: stack,
				Labels: profile.LabelSet{
					"tenant": {"d", "b", "c", "b"},
				},
			},
			{Count: 5, Stack: stack},
		},
	}

	got, err := Analyze(snapshot, Options{})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	userFrame := profile.Frame{
		Function: "github.com/acme/worker.(*Pool).run",
		File:     "/src/worker.go",
		Line:     87,
	}
	want := Analysis{
		Source: "snapshot.pprof",
		Total:  27,
		Groups: []Group{
			{
				Count:               27,
				ExactFingerprint:    "sha256:f9539207a187cde6a8b712278dce01252a531e54a17866d3e80f9ba9f94685bc",
				SemanticFingerprint: "sha256:8b77b951c4b986fa99dda710146d0e116cff968e21945bd11cfc8c289ba87d0c",
				Blocker: Blocker{
					Kind:             BlockerChanReceive,
					EvidenceFunction: "runtime.chanrecv1",
				},
				Stack:     chanReceiveStack(),
				UserFrame: &userFrame,
				Labels: []LabelKeySummary{
					{Key: "empty", Present: 10, Missing: 17, Values: []LabelValueCount{}},
					{
						Key:     "region",
						Present: 10,
						Missing: 17,
						Values:  []LabelValueCount{{Value: "west", Count: 10}},
					},
					{
						Key:     "tenant",
						Present: 22,
						Missing: 5,
						Values: []LabelValueCount{
							{Value: "b", Count: 22},
							{Value: "c", Count: 12},
							{Value: "d", Count: 12},
							{Value: "a", Count: 10},
						},
					},
				},
				Findings: []Finding{},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Analyze() = %#v, want %#v", got, want)
	}

	// Analysis owns its stack and selected frame rather than aliasing the input.
	snapshot.Leaks[0].Stack[3].Function = "changed"
	snapshot.Leaks[0].Stack[3].File = "changed"
	if got.Groups[0].Stack[3].Function != userFrame.Function || got.Groups[0].UserFrame.Function != userFrame.Function {
		t.Fatalf("Analyze() output aliases its input: %#v", got.Groups[0])
	}
}

func TestAnalyzeKeepsSameSemanticDifferentSitesSeparate(t *testing.T) {
	base := chanReceiveStack()
	differentLine := cloneStack(base)
	differentLine[len(differentLine)-1].Line = 120
	differentFile := cloneStack(base)
	differentFile[len(differentFile)-1].File = "/src/other.go"

	analysis, err := Analyze(profile.Snapshot{
		Leaks: []profile.Leak{
			{Count: 2, Stack: differentLine},
			{Count: 3, Stack: base},
			{Count: 4, Stack: differentFile},
		},
	}, Options{})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if analysis.Total != 9 || len(analysis.Groups) != 3 {
		t.Fatalf("Analyze() total/groups = %d/%d, want 9/3", analysis.Total, len(analysis.Groups))
	}

	exact := make(map[string]struct{})
	semantic := make(map[string]struct{})
	for _, group := range analysis.Groups {
		exact[group.ExactFingerprint] = struct{}{}
		semantic[group.SemanticFingerprint] = struct{}{}
	}
	if len(exact) != 3 {
		t.Fatalf("exact fingerprint count = %d, want 3", len(exact))
	}
	if len(semantic) != 1 {
		t.Fatalf("semantic fingerprint count = %d, want 1", len(semantic))
	}
}

func TestAnalyzeIsDeterministicAcrossShuffledInputs(t *testing.T) {
	chanStack := chanReceiveStack()
	inlineChanStack := cloneStack(chanStack)
	inlineChanStack[len(inlineChanStack)-1].Inlined = true
	unknownStack := []profile.Frame{{Function: "github.com/acme/other.Wait", File: "/src/other.go", Line: 9}}
	leaks := []profile.Leak{
		{Count: 4, Stack: chanStack, Labels: profile.LabelSet{"tenant": {"b", "a"}}},
		{Count: 1, Stack: inlineChanStack, Labels: profile.LabelSet{"tenant": {"a"}, "zone": {"2"}}},
		{Count: 5, Stack: unknownStack, Labels: profile.LabelSet{"tenant": {"z"}}},
		{Count: 2, Stack: []profile.Frame{}, Labels: profile.LabelSet{"zone": {"1"}}},
	}
	orders := [][]int{
		{0, 1, 2, 3},
		{3, 2, 1, 0},
		{1, 3, 0, 2},
		{2, 0, 3, 1},
	}

	var want Analysis
	for orderIndex, order := range orders {
		shuffled := make([]profile.Leak, 0, len(order))
		for _, leakIndex := range order {
			shuffled = append(shuffled, leaks[leakIndex])
		}
		got, err := Analyze(profile.Snapshot{Source: "shuffle.pprof", Leaks: shuffled}, Options{})
		if err != nil {
			t.Fatalf("Analyze(order %d) error = %v", orderIndex, err)
		}
		if orderIndex == 0 {
			want = got
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("Analyze(order %d) = %#v, want %#v", orderIndex, got, want)
		}
	}
	if len(want.Groups) != 3 {
		t.Fatalf("group count = %d, want 3", len(want.Groups))
	}
	if want.Groups[0].Blocker.Kind != BlockerChanReceive || want.Groups[1].Blocker.Kind != BlockerUnknown {
		t.Fatalf("equal-count blocker order = %#v, want chan_receive before unknown", want.Groups[:2])
	}
	if want.Groups[0].Stack[len(want.Groups[0].Stack)-1].Inlined {
		t.Fatalf("canonical exact-group stack depends on shuffled inline evidence: %#v", want.Groups[0].Stack)
	}
}

func TestGroupOrderingComparator(t *testing.T) {
	frame := func(function, file string, line int64, inlined bool) *profile.Frame {
		return &profile.Frame{Function: function, File: file, Line: line, Inlined: inlined}
	}
	tests := []struct {
		name  string
		left  Group
		right Group
	}{
		{
			name:  "count descending",
			left:  Group{Count: 2, Blocker: Blocker{Kind: BlockerUnknown}, ExactFingerprint: "z"},
			right: Group{Count: 1, Blocker: Blocker{Kind: BlockerChanReceive}, ExactFingerprint: "a"},
		},
		{
			name:  "blocker rank",
			left:  Group{Count: 1, Blocker: Blocker{Kind: BlockerChanReceive}, ExactFingerprint: "z"},
			right: Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, ExactFingerprint: "a"},
		},
		{
			name:  "non-nil user frame",
			left:  Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, UserFrame: frame("z", "z", 9, false), ExactFingerprint: "z"},
			right: Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, ExactFingerprint: "a"},
		},
		{
			name:  "user function",
			left:  Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, UserFrame: frame("a", "z", 9, false), ExactFingerprint: "z"},
			right: Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, UserFrame: frame("b", "a", 1, false), ExactFingerprint: "a"},
		},
		{
			name:  "user file",
			left:  Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, UserFrame: frame("f", "a", 9, false), ExactFingerprint: "z"},
			right: Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, UserFrame: frame("f", "b", 1, false), ExactFingerprint: "a"},
		},
		{
			name:  "user line",
			left:  Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, UserFrame: frame("f", "p", 1, false), ExactFingerprint: "z"},
			right: Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, UserFrame: frame("f", "p", 2, false), ExactFingerprint: "a"},
		},
		{
			name:  "exact fingerprint tie breaker",
			left:  Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, UserFrame: frame("f", "p", 1, true), ExactFingerprint: "a"},
			right: Group{Count: 1, Blocker: Blocker{Kind: BlockerUnknown}, UserFrame: frame("f", "p", 1, false), ExactFingerprint: "b"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if !groupLess(test.left, test.right) {
				t.Fatalf("groupLess(left, right) = false")
			}
			if groupLess(test.right, test.left) {
				t.Fatalf("groupLess(right, left) = true")
			}
		})
	}

	rankedKinds := []BlockerKind{
		BlockerChanReceive,
		BlockerChanSend,
		BlockerSelect,
		BlockerMutex,
		BlockerRWMutex,
		BlockerCond,
		BlockerWaitGroup,
		BlockerUnknown,
	}
	for index, kind := range rankedKinds {
		if got := blockerKindRank(kind); got != index {
			t.Fatalf("blockerKindRank(%q) = %d, want %d", kind, got, index)
		}
	}
}

func TestNormalizeFindingsOrdersAndDeduplicates(t *testing.T) {
	got := normalizeFindings([]Finding{
		{Kind: FindingInspect, Code: "a", Message: "inspect"},
		{Kind: FindingDetected, Code: "z", Message: "detected z"},
		{Kind: FindingPossibleCause, Code: "a", Message: "possible"},
		{Kind: FindingDetected, Code: "a", Message: "message 2"},
		{Kind: FindingDetected, Code: "a", Message: "message 1"},
		{Kind: FindingDetected, Code: "a", Message: "message 1"},
	})
	want := []Finding{
		{Kind: FindingDetected, Code: "a", Message: "message 1"},
		{Kind: FindingDetected, Code: "a", Message: "message 2"},
		{Kind: FindingDetected, Code: "z", Message: "detected z"},
		{Kind: FindingPossibleCause, Code: "a", Message: "possible"},
		{Kind: FindingInspect, Code: "a", Message: "inspect"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeFindings() = %#v, want %#v", got, want)
	}
	empty := normalizeFindings(nil)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("normalizeFindings(nil) = %#v, want non-nil empty slice", empty)
	}
}

func TestAnalyzeCanonicalEmptySlices(t *testing.T) {
	empty, err := Analyze(profile.Snapshot{Source: "empty.pprof"}, Options{})
	if err != nil {
		t.Fatalf("Analyze(empty) error = %v", err)
	}
	if empty.Source != "empty.pprof" || empty.Total != 0 || empty.Groups == nil || len(empty.Groups) != 0 {
		t.Fatalf("Analyze(empty) = %#v, want source and non-nil empty groups", empty)
	}

	withEmptyStack, err := Analyze(profile.Snapshot{
		Leaks: []profile.Leak{{Count: 1, Stack: nil}},
	}, Options{})
	if err != nil {
		t.Fatalf("Analyze(empty stack) error = %v", err)
	}
	if len(withEmptyStack.Groups) != 1 {
		t.Fatalf("Analyze(empty stack) group count = %d, want 1", len(withEmptyStack.Groups))
	}
	group := withEmptyStack.Groups[0]
	if group.Stack == nil || group.Labels == nil || group.Findings == nil {
		t.Fatalf("Analyze(empty stack) contains nil slices: %#v", group)
	}
	if group.UserFrame != nil {
		t.Fatalf("Analyze(empty stack) UserFrame = %#v, want nil", group.UserFrame)
	}
}

func TestAnalyzeCheckedArithmetic(t *testing.T) {
	t.Run("total overflow returns zero analysis", func(t *testing.T) {
		got, err := Analyze(profile.Snapshot{
			Source: "secret-source.pprof",
			Leaks: []profile.Leak{
				{Count: math.MaxInt64, Stack: chanReceiveStack()},
				{Count: 1, Stack: chanReceiveStack()},
			},
		}, Options{})
		if !errors.Is(err, errArithmeticOverflow) {
			t.Fatalf("Analyze() error = %v, want arithmetic overflow", err)
		}
		if !reflect.DeepEqual(got, Analysis{}) {
			t.Fatalf("Analyze() = %#v, want zero analysis on error", got)
		}
		if strings.Contains(err.Error(), "secret-source") {
			t.Fatalf("Analyze() error exposes source: %v", err)
		}
	})

	t.Run("label presence overflow", func(t *testing.T) {
		accumulators := map[string]*labelAccumulator{
			"secret-key": {present: math.MaxInt64, values: make(map[string]int64)},
		}
		err := accumulateLabels(accumulators, profile.LabelSet{"secret-key": nil}, 1)
		if !errors.Is(err, errArithmeticOverflow) {
			t.Fatalf("accumulateLabels() error = %v, want arithmetic overflow", err)
		}
		if strings.Contains(err.Error(), "secret-key") {
			t.Fatalf("accumulateLabels() error exposes label key: %v", err)
		}
	})

	t.Run("label value overflow", func(t *testing.T) {
		accumulators := map[string]*labelAccumulator{
			"key": {values: map[string]int64{"secret-value": math.MaxInt64}},
		}
		err := accumulateLabels(accumulators, profile.LabelSet{"key": {"secret-value"}}, 1)
		if !errors.Is(err, errArithmeticOverflow) {
			t.Fatalf("accumulateLabels() error = %v, want arithmetic overflow", err)
		}
		if strings.Contains(err.Error(), "secret-value") {
			t.Fatalf("accumulateLabels() error exposes label value: %v", err)
		}
	})
}

func TestAnalyzeCountInvariants(t *testing.T) {
	t.Run("zero count ignored", func(t *testing.T) {
		got, err := Analyze(profile.Snapshot{
			Leaks: []profile.Leak{{Count: 0, Stack: chanReceiveStack(), Labels: profile.LabelSet{"ignored": {"value"}}}},
		}, Options{})
		if err != nil {
			t.Fatalf("Analyze() error = %v", err)
		}
		if got.Total != 0 || got.Groups == nil || len(got.Groups) != 0 {
			t.Fatalf("Analyze() = %#v, want canonical empty analysis", got)
		}
	})

	t.Run("negative count rejected", func(t *testing.T) {
		got, err := Analyze(profile.Snapshot{Leaks: []profile.Leak{{Count: -1}}}, Options{})
		if err == nil || !strings.Contains(err.Error(), "negative count") {
			t.Fatalf("Analyze() error = %v, want negative count rejection", err)
		}
		if !reflect.DeepEqual(got, Analysis{}) {
			t.Fatalf("Analyze() = %#v, want zero analysis on error", got)
		}
	})
}
