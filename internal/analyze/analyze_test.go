package analyze

import (
	"reflect"
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
