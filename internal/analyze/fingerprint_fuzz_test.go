package analyze

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"

	"github.com/imbrooklyn/leakviz/internal/profile"
)

func TestFingerprintGoldenVectors(t *testing.T) {
	tests := []struct {
		name         string
		preimage     func() ([]byte, error)
		fingerprint  func() (string, error)
		wantPreimage string
		wantDigest   string
	}{
		{
			name: "exact four frames",
			preimage: func() ([]byte, error) {
				return exactPreimage(chanReceiveStack())
			},
			fingerprint: func() (string, error) {
				return exactFingerprint(chanReceiveStack())
			},
			wantPreimage: "00000000000000106c65616b76697a2d65786163742d76310000000000000004000000000000000e72756e74696d652e676f7061726b00000000000000212f7573722f6c6f63616c2f676f2f7372632f72756e74696d652f70726f632e676f00000000000001cc000000000000001072756e74696d652e6368616e7265637600000000000000212f7573722f6c6f63616c2f676f2f7372632f72756e74696d652f6368616e2e676f0000000000000298000000000000001172756e74696d652e6368616e726563763100000000000000212f7573722f6c6f63616c2f676f2f7372632f72756e74696d652f6368616e2e676f00000000000001fa00000000000000226769746875622e636f6d2f61636d652f776f726b65722e282a506f6f6c292e72756e000000000000000e2f7372632f776f726b65722e676f0000000000000057",
			wantDigest:   "sha256:f9539207a187cde6a8b712278dce01252a531e54a17866d3e80f9ba9f94685bc",
		},
		{
			name: "semantic retained function",
			preimage: func() ([]byte, error) {
				return semanticPreimage(BlockerChanReceive, []string{"github.com/acme/worker.(*Pool).run"})
			},
			fingerprint: func() (string, error) {
				return semanticFingerprint(BlockerChanReceive, []string{"github.com/acme/worker.(*Pool).run"})
			},
			wantPreimage: "00000000000000136c65616b76697a2d73656d616e7469632d7631000000000000000c6368616e5f72656365697665000000000000000100000000000000226769746875622e636f6d2f61636d652f776f726b65722e282a506f6f6c292e72756e",
			wantDigest:   "sha256:8b77b951c4b986fa99dda710146d0e116cff968e21945bd11cfc8c289ba87d0c",
		},
		{
			name: "exact Unicode empty file and signed lines",
			preimage: func() ([]byte, error) {
				return exactPreimage([]profile.Frame{
					{Function: "例/函数", File: "", Line: -1},
					{Function: "pkg.fn", File: `C:\src\x.go`, Line: 0},
				})
			},
			fingerprint: func() (string, error) {
				return exactFingerprint([]profile.Frame{
					{Function: "例/函数", File: "", Line: -1},
					{Function: "pkg.fn", File: `C:\src\x.go`, Line: 0},
				})
			},
			wantPreimage: "00000000000000106c65616b76697a2d65786163742d76310000000000000002000000000000000ae4be8b2fe587bde695b00000000000000000ffffffffffffffff0000000000000006706b672e666e000000000000000b433a5c7372635c782e676f0000000000000000",
			wantDigest:   "sha256:2c29919e1f569902e06ef6f4d34949c690b06f0e781f06d1e0a704c900605f03",
		},
		{
			name: "semantic empty sentinel",
			preimage: func() ([]byte, error) {
				return semanticPreimage(BlockerUnknown, nil)
			},
			fingerprint: func() (string, error) {
				return semanticFingerprint(BlockerUnknown, nil)
			},
			wantPreimage: "00000000000000136c65616b76697a2d73656d616e7469632d76310000000000000007756e6b6e6f776e000000000000000000000000000000196c65616b76697a2d656d7074792d73656d616e7469632d7631",
			wantDigest:   "sha256:1e3699ba678ac5a2a671cfd6e4256627abcf168642ac7cadb0a51b6f6a75e1c3",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			preimage, err := test.preimage()
			if err != nil {
				t.Fatalf("preimage error = %v", err)
			}
			if got := hex.EncodeToString(preimage); got != test.wantPreimage {
				t.Fatalf("preimage = %s, want %s", got, test.wantPreimage)
			}
			fingerprint, err := test.fingerprint()
			if err != nil {
				t.Fatalf("fingerprint error = %v", err)
			}
			if fingerprint != test.wantDigest {
				t.Fatalf("fingerprint = %q, want %q", fingerprint, test.wantDigest)
			}
		})
	}
}

func TestExactFingerprintBoundaries(t *testing.T) {
	tests := []struct {
		name  string
		left  []profile.Frame
		right []profile.Frame
	}{
		{
			name:  "field boundary",
			left:  []profile.Frame{{Function: "ab", File: "c"}},
			right: []profile.Frame{{Function: "a", File: "bc"}},
		},
		{
			name:  "frame count",
			left:  []profile.Frame{{Function: "a", File: "b"}},
			right: []profile.Frame{{Function: "a"}, {Function: "b"}},
		},
		{
			name:  "Unicode normalization forms",
			left:  []profile.Frame{{Function: "pkg.é"}},
			right: []profile.Frame{{Function: "pkg.e\u0301"}},
		},
		{
			name:  "inline logical function order",
			left:  []profile.Frame{{Function: "pkg.first", Inlined: true}, {Function: "pkg.second"}},
			right: []profile.Frame{{Function: "pkg.second", Inlined: true}, {Function: "pkg.first"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			leftPreimage, err := exactPreimage(test.left)
			if err != nil {
				t.Fatal(err)
			}
			rightPreimage, err := exactPreimage(test.right)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(leftPreimage, rightPreimage) {
				t.Fatalf("distinct boundaries produced the same preimage: %x", leftPreimage)
			}
			leftFingerprint, err := exactFingerprint(test.left)
			if err != nil {
				t.Fatal(err)
			}
			rightFingerprint, err := exactFingerprint(test.right)
			if err != nil {
				t.Fatal(err)
			}
			if leftFingerprint == rightFingerprint {
				t.Fatalf("distinct preimages produced the same fingerprint %q", leftFingerprint)
			}
		})
	}
}

func TestFingerprintIdentitySemantics(t *testing.T) {
	base := chanReceiveStack()
	moved := append([]profile.Frame(nil), base...)
	moved[len(moved)-1].File = "/new/worker.go"
	moved[len(moved)-1].Line = 92

	baseExact, baseSemantic := fingerprintsForTest(t, base, BlockerChanReceive)
	movedExact, movedSemantic := fingerprintsForTest(t, moved, BlockerChanReceive)
	if baseExact == movedExact {
		t.Fatalf("file/line change did not change exact fingerprint %q", baseExact)
	}
	if baseSemantic != movedSemantic {
		t.Fatalf("file/line change changed semantic fingerprint: %q != %q", baseSemantic, movedSemantic)
	}

	changedFunction := append([]profile.Frame(nil), base...)
	changedFunction[len(changedFunction)-1].Function = "github.com/acme/worker.(*Pool).run.func1"
	_, changedFunctionSemantic := fingerprintsForTest(t, changedFunction, BlockerChanReceive)
	if baseSemantic == changedFunctionSemantic {
		t.Fatalf("function change did not change semantic fingerprint %q", baseSemantic)
	}

	_, changedBlockerSemantic := fingerprintsForTest(t, base, BlockerUnknown)
	if baseSemantic == changedBlockerSemantic {
		t.Fatalf("blocker change did not change semantic fingerprint %q", baseSemantic)
	}

	nearPlumbing := append([]profile.Frame(nil), base...)
	nearPlumbing[1].Function = "runtime.chanrecv2"
	_, nearPlumbingSemantic := fingerprintsForTest(t, nearPlumbing, BlockerChanReceive)
	if baseSemantic == nearPlumbingSemantic {
		t.Fatalf("near-plumbing function was removed from semantic identity")
	}
}

func TestFingerprintIgnoresNonIdentityFields(t *testing.T) {
	left := profile.Leak{
		Stack:  chanReceiveStack(),
		Labels: profile.LabelSet{"tenant": {"left"}},
	}
	right := profile.Leak{
		Stack:  append([]profile.Frame(nil), left.Stack...),
		Labels: profile.LabelSet{"tenant": {"right"}, "extra": {"value"}},
	}
	for i := range right.Stack {
		right.Stack[i].Inlined = !right.Stack[i].Inlined
	}

	leftExact, leftSemantic := fingerprintsForTest(t, left.Stack, BlockerChanReceive)
	rightExact, rightSemantic := fingerprintsForTest(t, right.Stack, BlockerChanReceive)
	if leftExact != rightExact || leftSemantic != rightSemantic {
		t.Fatalf("labels or inline markers changed identity: exact %q/%q semantic %q/%q", leftExact, rightExact, leftSemantic, rightSemantic)
	}
}

func TestSemanticFingerprintPreservesGenericClosureAndOrder(t *testing.T) {
	functions := []string{
		"github.com/acme/pipeline.Map[int]",
		"github.com/acme/pipeline.Map[int].func1",
		"例/包.函数[string]",
		"",
	}
	forward, err := semanticFingerprint(BlockerUnknown, functions)
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := semanticFingerprint(BlockerUnknown, []string{functions[3], functions[2], functions[1], functions[0]})
	if err != nil {
		t.Fatal(err)
	}
	if forward == reversed {
		t.Fatalf("function order did not affect semantic fingerprint %q", forward)
	}
	empty, err := semanticFingerprint(BlockerUnknown, nil)
	if err != nil {
		t.Fatal(err)
	}
	oneEmpty, err := semanticFingerprint(BlockerUnknown, []string{""})
	if err != nil {
		t.Fatal(err)
	}
	if empty == oneEmpty {
		t.Fatalf("empty sentinel collided with one empty function")
	}
}

func TestCheckedLengthRejectsNegative(t *testing.T) {
	if _, err := checkedLength(-1); err == nil {
		t.Fatal("checkedLength(-1) returned nil error")
	}
}

func FuzzFingerprint(f *testing.F) {
	seeds := []struct {
		function string
		file     string
		line     int64
		caller   string
	}{
		{function: "", file: "", line: 0, caller: ""},
		{function: "runtime.gopark", file: "/usr/local/go/src/runtime/proc.go", line: 460, caller: "runtime.chanrecv"},
		{function: "例/函数", file: "", line: -1, caller: "pkg.fn"},
		{function: "pkg.é", file: `C:\src\x.go`, line: 0, caller: "pkg.e\u0301"},
		{function: "github.com/acme/pipeline.Map[int]", file: "/src/map.go", line: 87, caller: "github.com/acme/pipeline.Map[int].func1"},
		{function: "ab", file: "c", line: 1, caller: "a.bc"},
		{function: strings.Repeat("long", 256), file: strings.Repeat("/path", 256), line: 1<<63 - 1, caller: "pkg.long.func1"},
	}
	for _, seed := range seeds {
		f.Add(seed.function, seed.file, seed.line, seed.caller)
	}

	f.Fuzz(func(t *testing.T, function, file string, line int64, caller string) {
		stack := []profile.Frame{
			{Function: function, File: file, Line: line, Inlined: true},
			{Function: caller, File: "", Line: 0},
		}
		exactFirst, semanticFirst := fingerprintsForTest(t, stack, BlockerChanReceive)
		exactSecond, semanticSecond := fingerprintsForTest(t, stack, BlockerChanReceive)
		if exactFirst != exactSecond || semanticFirst != semanticSecond {
			t.Fatalf("fingerprints are nondeterministic: exact %q/%q semantic %q/%q", exactFirst, exactSecond, semanticFirst, semanticSecond)
		}
		assertFullSHA256(t, exactFirst)
		assertFullSHA256(t, semanticFirst)

		withoutInline := append([]profile.Frame(nil), stack...)
		withoutInline[0].Inlined = false
		exactWithoutInline, semanticWithoutInline := fingerprintsForTest(t, withoutInline, BlockerChanReceive)
		if exactFirst != exactWithoutInline || semanticFirst != semanticWithoutInline {
			t.Fatalf("inline marker changed identity")
		}

		moved := append([]profile.Frame(nil), stack...)
		moved[0].File += "\x00"
		moved[0].Line ^= 1
		movedPreimage, err := exactPreimage(moved)
		if err != nil {
			t.Fatal(err)
		}
		originalPreimage, err := exactPreimage(stack)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(originalPreimage, movedPreimage) {
			t.Fatal("file/line mutation did not change exact preimage")
		}
		_, movedSemantic := fingerprintsForTest(t, moved, BlockerChanReceive)
		if semanticFirst != movedSemantic {
			t.Fatalf("file/line mutation changed semantic identity: %q != %q", semanticFirst, movedSemantic)
		}

		leftExactBoundary := []profile.Frame{{Function: function + "a", File: file}}
		rightExactBoundary := []profile.Frame{{Function: function, File: "a" + file}}
		assertDistinctExactBoundaries(t, leftExactBoundary, rightExactBoundary)

		leftSemanticBoundary := []string{function + "a", caller}
		rightSemanticBoundary := []string{function, "a" + caller}
		assertDistinctSemanticBoundaries(t, leftSemanticBoundary, rightSemanticBoundary)
	})
}

func FuzzNormalizeStack(f *testing.F) {
	seeds := []struct {
		first  string
		second string
		third  string
		inline bool
	}{
		{first: "runtime.gopark", second: "runtime.chanrecv", third: "github.com/acme/worker.Run"},
		{first: "runtime.block", second: "runtime.selectgo", third: "runtime.chanrecv1"},
		{first: "runtime.chansend", second: "runtime.chansend1", third: "runtime.semacquire1"},
		{first: "sync.runtime_Semacquire", second: "sync.runtime_SemacquireWaitGroup", third: "sync.runtime_SemacquireRWMutex"},
		{first: "sync.runtime_SemacquireRWMutexR", second: "sync.runtime_notifyListWait", third: "internal/sync.runtime_SemacquireMutex"},
		{first: "internal/sync.(*Mutex).lockSlow", second: "sync.(*Mutex).Lock", third: "sync.(*RWMutex).Lock"},
		{first: "sync.(*RWMutex).RLock", second: "sync.(*Cond).Wait", third: "sync.(*WaitGroup).Wait"},
		{first: "sync.(*Mutex).Lock", second: "github.com/acme/pipeline.Map[int]", third: "github.com/acme/pipeline.Map[int].func1", inline: true},
		{first: "runtime.chanrecv2", second: "例/包.函数", third: "", inline: true},
		{first: "pkg.é", second: "pkg.e\u0301", third: "pkg.func1"},
		{first: "", second: "", third: ""},
	}
	for _, seed := range seeds {
		f.Add(seed.first, seed.second, seed.third, seed.inline)
	}

	f.Fuzz(func(t *testing.T, first, second, third string, inline bool) {
		stack := []profile.Frame{
			{Function: first, Inlined: inline},
			{Function: second, Inlined: !inline},
			{Function: third, Inlined: inline},
		}
		original := append([]profile.Frame(nil), stack...)
		want := make([]string, 0, len(stack))
		for _, frame := range stack {
			if !isSemanticPlumbing(frame.Function) {
				want = append(want, frame.Function)
			}
		}
		firstResult := normalizeStack(stack)
		secondResult := normalizeStack(stack)
		if !reflect.DeepEqual(firstResult, want) || !reflect.DeepEqual(secondResult, want) {
			t.Fatalf("normalizeStack() = %#v and %#v, want %#v", firstResult, secondResult, want)
		}
		if !reflect.DeepEqual(stack, original) {
			t.Fatalf("normalizeStack mutated input: %#v != %#v", stack, original)
		}
		firstFingerprint, err := semanticFingerprint(BlockerUnknown, firstResult)
		if err != nil {
			t.Fatal(err)
		}
		secondFingerprint, err := semanticFingerprint(BlockerUnknown, secondResult)
		if err != nil {
			t.Fatal(err)
		}
		if firstFingerprint != secondFingerprint {
			t.Fatalf("normalized fingerprints are nondeterministic: %q != %q", firstFingerprint, secondFingerprint)
		}
	})
}

func assertDistinctExactBoundaries(t *testing.T, left, right []profile.Frame) {
	t.Helper()
	leftPreimage, err := exactPreimage(left)
	if err != nil {
		t.Fatal(err)
	}
	rightPreimage, err := exactPreimage(right)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(leftPreimage, rightPreimage) {
		t.Fatalf("distinct exact field boundaries produced the same preimage: %x", leftPreimage)
	}
	leftFingerprint, err := exactFingerprint(left)
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := exactFingerprint(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint == rightFingerprint {
		t.Fatalf("distinct exact field boundaries produced the same fingerprint %q", leftFingerprint)
	}
}

func assertDistinctSemanticBoundaries(t *testing.T, left, right []string) {
	t.Helper()
	leftPreimage, err := semanticPreimage(BlockerUnknown, left)
	if err != nil {
		t.Fatal(err)
	}
	rightPreimage, err := semanticPreimage(BlockerUnknown, right)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(leftPreimage, rightPreimage) {
		t.Fatalf("distinct semantic frame boundaries produced the same preimage: %x", leftPreimage)
	}
	leftFingerprint, err := semanticFingerprint(BlockerUnknown, left)
	if err != nil {
		t.Fatal(err)
	}
	rightFingerprint, err := semanticFingerprint(BlockerUnknown, right)
	if err != nil {
		t.Fatal(err)
	}
	if leftFingerprint == rightFingerprint {
		t.Fatalf("distinct semantic frame boundaries produced the same fingerprint %q", leftFingerprint)
	}
}

func fingerprintsForTest(t *testing.T, stack []profile.Frame, blocker BlockerKind) (string, string) {
	t.Helper()
	exact, err := exactFingerprint(stack)
	if err != nil {
		t.Fatalf("exactFingerprint() error = %v", err)
	}
	semantic, err := semanticFingerprint(blocker, normalizeStack(stack))
	if err != nil {
		t.Fatalf("semanticFingerprint() error = %v", err)
	}
	return exact, semantic
}

func assertFullSHA256(t *testing.T, fingerprint string) {
	t.Helper()
	const prefix = "sha256:"
	if !strings.HasPrefix(fingerprint, prefix) || len(fingerprint) != len(prefix)+sha256HexLength {
		t.Fatalf("fingerprint %q is not a full SHA-256 value", fingerprint)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(fingerprint, prefix)); err != nil {
		t.Fatalf("fingerprint %q has invalid lowercase hex: %v", fingerprint, err)
	}
	if fingerprint != strings.ToLower(fingerprint) {
		t.Fatalf("fingerprint %q is not lowercase", fingerprint)
	}
}

const sha256HexLength = 64
