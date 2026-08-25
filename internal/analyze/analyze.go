package analyze

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/imbrooklyn/leakviz/internal/profile"
)

var errArithmeticOverflow = errors.New("arithmetic overflow")

type groupAccumulator struct {
	group  Group
	labels map[string]*labelAccumulator
}

type labelAccumulator struct {
	present int64
	values  map[string]int64
}

// Analyze groups one snapshot by exact stack identity and summarizes its labels.
func Analyze(snapshot profile.Snapshot, opts Options) (Analysis, error) {
	analysis := Analysis{
		Source: strings.Clone(snapshot.Source),
		Groups: make([]Group, 0),
	}
	groups := make(map[string]*groupAccumulator)

	for _, leak := range snapshot.Leaks {
		if leak.Count < 0 {
			return Analysis{}, errors.New("analyze leak: negative count unsupported")
		}
		if leak.Count == 0 {
			continue
		}

		total, err := checkedAdd(analysis.Total, leak.Count)
		if err != nil {
			return Analysis{}, fmt.Errorf("analyze total: %w", err)
		}
		analysis.Total = total

		exact, err := exactFingerprint(leak.Stack)
		if err != nil {
			return Analysis{}, fmt.Errorf("analyze exact fingerprint: %w", err)
		}
		accumulator, ok := groups[exact]
		if !ok {
			blocker := classify(leak.Stack)
			semantic, err := semanticFingerprint(blocker.Kind, normalizeStack(leak.Stack))
			if err != nil {
				return Analysis{}, fmt.Errorf("analyze semantic fingerprint: %w", err)
			}
			accumulator = &groupAccumulator{
				group: Group{
					ExactFingerprint:    exact,
					SemanticFingerprint: semantic,
					Blocker: Blocker{
						Kind:             blocker.Kind,
						EvidenceFunction: strings.Clone(blocker.EvidenceFunction),
					},
					Stack:     cloneStack(leak.Stack),
					UserFrame: selectUserFrame(leak.Stack, opts),
					Labels:    make([]LabelKeySummary, 0),
					Findings:  make([]Finding, 0),
				},
				labels: make(map[string]*labelAccumulator),
			}
			groups[exact] = accumulator
		} else if stackLess(leak.Stack, accumulator.group.Stack) {
			// Inlined is evidence, not exact identity. Choose an input stack by a
			// complete order so shuffled equivalent samples have one representative.
			accumulator.group.Stack = cloneStack(leak.Stack)
			accumulator.group.UserFrame = selectUserFrame(leak.Stack, opts)
		}

		groupCount, err := checkedAdd(accumulator.group.Count, leak.Count)
		if err != nil {
			return Analysis{}, fmt.Errorf("analyze group count: %w", err)
		}
		accumulator.group.Count = groupCount
		if err := accumulateLabels(accumulator.labels, leak.Labels, leak.Count); err != nil {
			return Analysis{}, fmt.Errorf("analyze labels: %w", err)
		}
	}

	exactFingerprints := make([]string, 0, len(groups))
	for exact := range groups {
		exactFingerprints = append(exactFingerprints, exact)
	}
	sort.Strings(exactFingerprints)

	var groupedTotal int64
	for _, exact := range exactFingerprints {
		accumulator := groups[exact]
		labels, err := summarizeLabels(accumulator.labels, accumulator.group.Count)
		if err != nil {
			return Analysis{}, fmt.Errorf("analyze label summaries: %w", err)
		}
		accumulator.group.Labels = labels
		accumulator.group.Findings = normalizeFindings(accumulator.group.Findings)

		groupedTotal, err = checkedAdd(groupedTotal, accumulator.group.Count)
		if err != nil {
			return Analysis{}, fmt.Errorf("analyze grouped total: %w", err)
		}
		analysis.Groups = append(analysis.Groups, accumulator.group)
	}
	if groupedTotal != analysis.Total {
		return Analysis{}, errors.New("analyze grouped total does not match snapshot total")
	}

	sortGroups(analysis.Groups)
	return analysis, nil
}

func accumulateLabels(accumulators map[string]*labelAccumulator, labels profile.LabelSet, count int64) error {
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		accumulator, ok := accumulators[key]
		if !ok {
			accumulator = &labelAccumulator{values: make(map[string]int64)}
			accumulators[key] = accumulator
		}

		present, err := checkedAdd(accumulator.present, count)
		if err != nil {
			return fmt.Errorf("add label presence: %w", err)
		}
		accumulator.present = present

		uniqueValues := make(map[string]struct{}, len(labels[key]))
		for _, value := range labels[key] {
			uniqueValues[value] = struct{}{}
		}
		values := make([]string, 0, len(uniqueValues))
		for value := range uniqueValues {
			values = append(values, value)
		}
		sort.Strings(values)
		for _, value := range values {
			valueCount, err := checkedAdd(accumulator.values[value], count)
			if err != nil {
				return fmt.Errorf("add label value: %w", err)
			}
			accumulator.values[value] = valueCount
		}
	}
	return nil
}

func summarizeLabels(accumulators map[string]*labelAccumulator, groupCount int64) ([]LabelKeySummary, error) {
	keys := make([]string, 0, len(accumulators))
	for key := range accumulators {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	summaries := make([]LabelKeySummary, 0, len(keys))
	for _, key := range keys {
		accumulator := accumulators[key]
		if accumulator.present < 0 || accumulator.present > groupCount {
			return nil, errors.New("label presence exceeds group count")
		}

		values := make([]LabelValueCount, 0, len(accumulator.values))
		for value, count := range accumulator.values {
			values = append(values, LabelValueCount{
				Value: strings.Clone(value),
				Count: count,
			})
		}
		sort.Slice(values, func(i, j int) bool {
			if values[i].Count != values[j].Count {
				return values[i].Count > values[j].Count
			}
			return values[i].Value < values[j].Value
		})

		summaries = append(summaries, LabelKeySummary{
			Key:     strings.Clone(key),
			Present: accumulator.present,
			Missing: groupCount - accumulator.present,
			Values:  values,
		})
	}
	return summaries, nil
}

func checkedAdd(left, right int64) (int64, error) {
	if left < 0 || right < 0 {
		return 0, errors.New("negative arithmetic operand")
	}
	if left > math.MaxInt64-right {
		return 0, errArithmeticOverflow
	}
	return left + right, nil
}

func cloneStack(stack []profile.Frame) []profile.Frame {
	cloned := make([]profile.Frame, len(stack))
	for index, frame := range stack {
		cloned[index] = profile.Frame{
			Function: strings.Clone(frame.Function),
			File:     strings.Clone(frame.File),
			Line:     frame.Line,
			Inlined:  frame.Inlined,
		}
	}
	return cloned
}

func stackLess(left, right []profile.Frame) bool {
	limit := min(len(left), len(right))
	for index := 0; index < limit; index++ {
		if left[index].Function != right[index].Function {
			return left[index].Function < right[index].Function
		}
		if left[index].File != right[index].File {
			return left[index].File < right[index].File
		}
		if left[index].Line != right[index].Line {
			return left[index].Line < right[index].Line
		}
		if left[index].Inlined != right[index].Inlined {
			return !left[index].Inlined
		}
	}
	return len(left) < len(right)
}

func sortGroups(groups []Group) {
	sort.Slice(groups, func(i, j int) bool {
		return groupLess(groups[i], groups[j])
	})
}

func groupLess(left, right Group) bool {
	if left.Count != right.Count {
		return left.Count > right.Count
	}
	leftBlockerRank, rightBlockerRank := blockerKindRank(left.Blocker.Kind), blockerKindRank(right.Blocker.Kind)
	if leftBlockerRank != rightBlockerRank {
		return leftBlockerRank < rightBlockerRank
	}
	if left.Blocker.Kind != right.Blocker.Kind {
		return left.Blocker.Kind < right.Blocker.Kind
	}
	if (left.UserFrame == nil) != (right.UserFrame == nil) {
		return left.UserFrame != nil
	}
	if left.UserFrame != nil {
		if left.UserFrame.Function != right.UserFrame.Function {
			return left.UserFrame.Function < right.UserFrame.Function
		}
		if left.UserFrame.File != right.UserFrame.File {
			return left.UserFrame.File < right.UserFrame.File
		}
		if left.UserFrame.Line != right.UserFrame.Line {
			return left.UserFrame.Line < right.UserFrame.Line
		}
	}
	return left.ExactFingerprint < right.ExactFingerprint
}

func blockerKindRank(kind BlockerKind) int {
	switch kind {
	case BlockerChanReceive:
		return 0
	case BlockerChanSend:
		return 1
	case BlockerSelect:
		return 2
	case BlockerMutex:
		return 3
	case BlockerRWMutex:
		return 4
	case BlockerCond:
		return 5
	case BlockerWaitGroup:
		return 6
	case BlockerUnknown:
		return 7
	default:
		return 8
	}
}

func normalizeFindings(findings []Finding) []Finding {
	normalized := make([]Finding, len(findings))
	for index, finding := range findings {
		normalized[index] = Finding{
			Kind:    finding.Kind,
			Code:    strings.Clone(finding.Code),
			Message: strings.Clone(finding.Message),
		}
	}
	sort.Slice(normalized, func(i, j int) bool {
		left, right := normalized[i], normalized[j]
		leftRank, rightRank := findingKindRank(left.Kind), findingKindRank(right.Kind)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		if left.Code != right.Code {
			return left.Code < right.Code
		}
		return left.Message < right.Message
	})

	deduplicated := make([]Finding, 0, len(normalized))
	for _, finding := range normalized {
		if len(deduplicated) == 0 || finding != deduplicated[len(deduplicated)-1] {
			deduplicated = append(deduplicated, finding)
		}
	}
	return deduplicated
}

func findingKindRank(kind FindingKind) int {
	switch kind {
	case FindingDetected:
		return 0
	case FindingPossibleCause:
		return 1
	case FindingInspect:
		return 2
	default:
		return 3
	}
}
