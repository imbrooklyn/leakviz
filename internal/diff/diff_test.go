package diff

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/imbrooklyn/leakviz/internal/analyze"
	"github.com/imbrooklyn/leakviz/internal/profile"
)

func TestCompareAllStatuses(t *testing.T) {
	before := analyze.Analysis{
		Source: "before.pprof",
		Total:  14,
		Groups: []analyze.Group{
			testGroup("sem-increased", "exact-increased-before", 2, 10),
			testGroup("sem-decreased", "exact-decreased-before", 5, 20),
			testGroup("sem-resolved", "exact-resolved", 4, 30),
			testGroup("sem-unchanged", "exact-unchanged-before", 3, 40),
		},
	}
	after := analyze.Analysis{
		Source: "after.pprof",
		Total:  15,
		Groups: []analyze.Group{
			testGroup("sem-new", "exact-new", 5, 50),
			testGroup("sem-increased", "exact-increased-after", 5, 11),
			testGroup("sem-decreased", "exact-decreased-after", 2, 21),
			testGroup("sem-unchanged", "exact-unchanged-after", 3, 41),
		},
	}

	result, err := Compare(before, after)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	want := []struct {
		status      Status
		fingerprint string
		before      int64
		after       int64
		delta       int64
	}{
		{StatusNew, "sem-new", 0, 5, 5},
		{StatusIncreased, "sem-increased", 2, 5, 3},
		{StatusDecreased, "sem-decreased", 5, 2, -3},
		{StatusResolved, "sem-resolved", 4, 0, -4},
		{StatusUnchanged, "sem-unchanged", 3, 3, 0},
	}
	if result.BeforeSource != before.Source || result.AfterSource != after.Source || result.BeforeTotal != 14 || result.AfterTotal != 15 {
		t.Fatalf("Compare() metadata = %#v", result)
	}
	if len(result.Changes) != len(want) {
		t.Fatalf("Compare() changes = %d, want %d", len(result.Changes), len(want))
	}
	for index, expected := range want {
		change := result.Changes[index]
		if change.Status != expected.status || change.SemanticFingerprint != expected.fingerprint || change.BeforeCount != expected.before || change.AfterCount != expected.after || change.Delta != expected.delta {
			t.Errorf("change %d = %#v, want status=%s fingerprint=%s counts=%d/%d delta=%d", index, change, expected.status, expected.fingerprint, expected.before, expected.after, expected.delta)
		}
		if change.BeforeGroups == nil || change.AfterGroups == nil {
			t.Errorf("change %d has nil group array", index)
		}
	}
}

func TestCompareLineMoveAndMultiSiteBucket(t *testing.T) {
	beforeGroups := []analyze.Group{
		testGroupWithLabel("semantic-worker", "exact-line-80", 4, 80, "a"),
		testGroupWithLabel("semantic-worker", "exact-line-87", 6, 87, "a"),
	}
	afterGroups := []analyze.Group{
		testGroupWithLabel("semantic-worker", "exact-line-92", 12, 92, "b"),
	}
	beforeOriginal := append([]analyze.Group(nil), beforeGroups...)

	result, err := Compare(
		analyze.Analysis{Source: "before.pprof", Total: 10, Groups: beforeGroups},
		analyze.Analysis{Source: "after.pprof", Total: 12, Groups: afterGroups},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if len(result.Changes) != 1 {
		t.Fatalf("changes = %#v, want one semantic bucket", result.Changes)
	}
	change := result.Changes[0]
	if change.Status != StatusIncreased || change.BeforeCount != 10 || change.AfterCount != 12 || change.Delta != 2 {
		t.Fatalf("multi-site change = %#v", change)
	}
	if len(change.BeforeGroups) != 2 || len(change.AfterGroups) != 1 {
		t.Fatalf("multi-site groups = before %d after %d", len(change.BeforeGroups), len(change.AfterGroups))
	}
	if change.BeforeGroups[0].ExactFingerprint != "exact-line-87" || change.BeforeGroups[1].ExactFingerprint != "exact-line-80" || change.AfterGroups[0].ExactFingerprint != "exact-line-92" {
		t.Fatalf("bucket group order/sites = before %#v after %#v", change.BeforeGroups, change.AfterGroups)
	}
	if change.BeforeGroups[0].UserFrame.Line != 87 || change.BeforeGroups[1].UserFrame.Line != 80 || change.AfterGroups[0].UserFrame.Line != 92 {
		t.Fatalf("line-move sites were not retained: %#v", change)
	}
	if !reflect.DeepEqual(beforeGroups, beforeOriginal) {
		t.Fatalf("Compare() rewrote snapshot grouping: got %#v want %#v", beforeGroups, beforeOriginal)
	}
}

func TestCompareRetainsLabelOnlyChanges(t *testing.T) {
	beforeGroup := testGroupWithLabel("semantic", "exact", 3, 10, "before")
	afterGroup := testGroupWithLabel("semantic", "exact", 3, 10, "after")
	result, err := Compare(
		analyze.Analysis{Groups: []analyze.Group{beforeGroup}},
		analyze.Analysis{Groups: []analyze.Group{afterGroup}},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	change := result.Changes[0]
	if change.Status != StatusUnchanged || change.Delta != 0 {
		t.Fatalf("label-only change status = %#v, want UNCHANGED", change)
	}
	if change.BeforeGroups[0].Labels[0].Values[0].Value != "before" || change.AfterGroups[0].Labels[0].Values[0].Value != "after" {
		t.Fatalf("label evidence was not retained: %#v", change)
	}
}

func TestCompareBlockerOrFunctionChangeDoesNotMatch(t *testing.T) {
	before := testGroup("semantic-chan-receive", "exact-before", 2, 10)
	before.Blocker.Kind = analyze.BlockerChanReceive
	after := testGroup("semantic-chan-send", "exact-after", 2, 10)
	after.Blocker.Kind = analyze.BlockerChanSend
	after.UserFrame.Function = "example.com/worker.send"

	result, err := Compare(
		analyze.Analysis{Groups: []analyze.Group{before}},
		analyze.Analysis{Groups: []analyze.Group{after}},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	if len(result.Changes) != 2 || result.Changes[0].Status != StatusNew || result.Changes[1].Status != StatusResolved {
		t.Fatalf("blocker/function change = %#v, want NEW then RESOLVED", result.Changes)
	}
}

func TestCompareEmptySides(t *testing.T) {
	t.Run("both empty", func(t *testing.T) {
		result, err := Compare(analyze.Analysis{}, analyze.Analysis{})
		if err != nil {
			t.Fatalf("Compare() error = %v", err)
		}
		if result.Changes == nil || len(result.Changes) != 0 {
			t.Fatalf("empty changes = %#v, want non-nil empty", result.Changes)
		}
	})

	t.Run("maximum deltas", func(t *testing.T) {
		newResult, err := Compare(analyze.Analysis{}, analyze.Analysis{Groups: []analyze.Group{testGroup("new", "new", math.MaxInt64, 1)}})
		if err != nil || newResult.Changes[0].Delta != math.MaxInt64 {
			t.Fatalf("maximum NEW delta = %#v, error %v", newResult, err)
		}
		resolvedResult, err := Compare(analyze.Analysis{Groups: []analyze.Group{testGroup("resolved", "resolved", math.MaxInt64, 1)}}, analyze.Analysis{})
		if err != nil || resolvedResult.Changes[0].Delta != -math.MaxInt64 {
			t.Fatalf("maximum RESOLVED delta = %#v, error %v", resolvedResult, err)
		}
	})
}

func TestCompareRejectsInvalidCountsAndOverflow(t *testing.T) {
	tests := []struct {
		name   string
		before []analyze.Group
		after  []analyze.Group
		want   string
	}{
		{
			name: "before overflow",
			before: []analyze.Group{
				testGroup("semantic", "a", math.MaxInt64, 1),
				testGroup("semantic", "b", 1, 2),
			},
			want: "before semantic bucket",
		},
		{
			name: "after overflow",
			after: []analyze.Group{
				testGroup("semantic", "a", math.MaxInt64, 1),
				testGroup("semantic", "b", 1, 2),
			},
			want: "after semantic bucket",
		},
		{
			name:   "zero group",
			before: []analyze.Group{testGroup("semantic", "zero", 0, 1)},
			want:   "must be positive",
		},
		{
			name:   "negative group",
			before: []analyze.Group{testGroup("semantic", "negative", -1, 1)},
			want:   "must be positive",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Compare(analyze.Analysis{Groups: test.before}, analyze.Analysis{Groups: test.after})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Compare() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCompareDeterministicOrdering(t *testing.T) {
	before := []analyze.Group{
		testGroup("d", "d-before", 5, 1),
		testGroup("b", "b-before", 10, 1),
		testGroup("e", "e-before", 5, 1),
		testGroup("a", "a-before", 1, 1),
		testGroup("c", "c-before", 5, 1),
	}
	after := []analyze.Group{
		testGroup("e", "e-after", 8, 1),
		testGroup("c", "c-after", 8, 1),
		testGroup("a", "a-after", 5, 1),
		testGroup("d", "d-after", 8, 1),
		testGroup("b", "b-after", 13, 1),
	}

	result, err := Compare(analyze.Analysis{Groups: before}, analyze.Analysis{Groups: after})
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	want := []string{"a", "b", "c", "d", "e"}
	for index, fingerprint := range want {
		if result.Changes[index].SemanticFingerprint != fingerprint {
			t.Fatalf("change order = %#v, want %v", result.Changes, want)
		}
	}

	reversedBefore := reverseGroups(before)
	reversedAfter := reverseGroups(after)
	repeated, err := Compare(analyze.Analysis{Groups: reversedBefore}, analyze.Analysis{Groups: reversedAfter})
	if err != nil {
		t.Fatalf("Compare(reversed) error = %v", err)
	}
	if !reflect.DeepEqual(result, repeated) {
		t.Fatalf("Compare() changed with shuffled inputs\nfirst: %#v\nsecond: %#v", result, repeated)
	}
}

func TestCompareEqualCountAndExactSiteTie(t *testing.T) {
	before := []analyze.Group{
		testGroup("semantic", "exact-z", 3, 30),
		testGroup("semantic", "exact-a", 3, 10),
	}
	after := []analyze.Group{
		testGroup("semantic", "exact-y", 2, 40),
		testGroup("semantic", "exact-b", 4, 20),
	}

	result, err := Compare(
		analyze.Analysis{Groups: before},
		analyze.Analysis{Groups: after},
	)
	if err != nil {
		t.Fatalf("Compare() error = %v", err)
	}
	change := result.Changes[0]
	if change.Status != StatusUnchanged || change.BeforeCount != 6 || change.AfterCount != 6 || change.Delta != 0 {
		t.Fatalf("equal semantic bucket = %#v, want UNCHANGED 6/6 delta 0", change)
	}
	if change.BeforeGroups[0].ExactFingerprint != "exact-a" || change.BeforeGroups[1].ExactFingerprint != "exact-z" {
		t.Fatalf("equal-count before site order = %#v, want exact fingerprint tie-break", change.BeforeGroups)
	}
	if change.AfterGroups[0].ExactFingerprint != "exact-b" || change.AfterGroups[1].ExactFingerprint != "exact-y" {
		t.Fatalf("after site order = %#v, want count then exact fingerprint", change.AfterGroups)
	}
}

func testGroup(semantic, exact string, count, line int64) analyze.Group {
	frame := profile.Frame{Function: "example.com/worker.wait", File: "/src/worker.go", Line: line}
	return analyze.Group{
		Count:               count,
		ExactFingerprint:    exact,
		SemanticFingerprint: semantic,
		Blocker: analyze.Blocker{
			Kind:             analyze.BlockerChanReceive,
			EvidenceFunction: "runtime.chanrecv1",
		},
		Stack:     []profile.Frame{frame},
		UserFrame: &frame,
		Labels:    []analyze.LabelKeySummary{},
		Findings:  []analyze.Finding{},
	}
}

func testGroupWithLabel(semantic, exact string, count, line int64, value string) analyze.Group {
	group := testGroup(semantic, exact, count, line)
	group.Labels = []analyze.LabelKeySummary{{
		Key:     "tenant",
		Present: count,
		Values:  []analyze.LabelValueCount{{Value: value, Count: count}},
	}}
	return group
}

func reverseGroups(groups []analyze.Group) []analyze.Group {
	reversed := make([]analyze.Group, len(groups))
	for index := range groups {
		reversed[len(groups)-1-index] = groups[index]
	}
	return reversed
}
