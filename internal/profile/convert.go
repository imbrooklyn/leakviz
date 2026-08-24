package profile

import (
	"fmt"
	"math"
	"strings"

	pprofprofile "github.com/google/pprof/profile"
)

func convertProfile(source string, p *pprofprofile.Profile) (Snapshot, error) {
	if p == nil {
		return Snapshot{}, fmt.Errorf("profile is nil")
	}
	if p.DurationNanos != 0 {
		return Snapshot{}, fmt.Errorf("profile duration must be zero; delta profiles are unsupported")
	}

	valueIndex, err := goroutineLeakValueIndex(p.SampleType)
	if err != nil {
		return Snapshot{}, err
	}

	leaks := make([]Leak, 0, len(p.Sample))
	var total int64
	for sampleIndex, sample := range p.Sample {
		if sample == nil {
			return Snapshot{}, fmt.Errorf("sample %d is nil", sampleIndex)
		}
		if len(sample.NumLabel) != 0 || len(sample.NumUnit) != 0 {
			return Snapshot{}, fmt.Errorf("sample %d contains unsupported numeric labels", sampleIndex)
		}
		if len(sample.Value) != len(p.SampleType) || valueIndex >= len(sample.Value) {
			return Snapshot{}, fmt.Errorf("sample %d has incomplete values for its sample types", sampleIndex)
		}

		count := sample.Value[valueIndex]
		if count < 0 {
			return Snapshot{}, fmt.Errorf("sample %d has negative goroutineleak count", sampleIndex)
		}
		if count == 0 {
			continue
		}
		if count > math.MaxInt64-total {
			return Snapshot{}, fmt.Errorf("goroutineleak count total overflows int64")
		}

		stack, err := convertStack(sampleIndex, sample.Location)
		if err != nil {
			return Snapshot{}, err
		}
		leaks = append(leaks, Leak{
			Count:  count,
			Stack:  stack,
			Labels: cloneLabels(sample.Label),
		})
		total += count
	}

	return Snapshot{
		Source: strings.Clone(source),
		Leaks:  leaks,
	}, nil
}

func goroutineLeakValueIndex(sampleTypes []*pprofprofile.ValueType) (int, error) {
	index := -1
	count := 0
	for i, sampleType := range sampleTypes {
		if sampleType != nil && sampleType.Type == "goroutineleak" && sampleType.Unit == "count" {
			index = i
			count++
		}
	}
	if count != 1 {
		return 0, fmt.Errorf("profile must contain exactly one goroutineleak/count sample type; found %d", count)
	}
	return index, nil
}

func convertStack(sampleIndex int, locations []*pprofprofile.Location) ([]Frame, error) {
	stack := make([]Frame, 0, len(locations))
	for locationIndex, location := range locations {
		if location == nil || len(location.Line) == 0 {
			return nil, fmt.Errorf("sample %d location %d is unsymbolized: missing line", sampleIndex, locationIndex)
		}
		for lineIndex, line := range location.Line {
			if line.Function == nil || line.Function.Name == "" {
				return nil, fmt.Errorf("sample %d location %d line %d is unsymbolized: missing function name", sampleIndex, locationIndex, lineIndex)
			}
			stack = append(stack, Frame{
				Function: strings.Clone(line.Function.Name),
				File:     strings.Clone(line.Function.Filename),
				Line:     line.Line,
				Inlined:  lineIndex < len(location.Line)-1,
			})
		}
	}
	if len(stack) == 0 {
		return nil, fmt.Errorf("sample %d has an empty logical stack", sampleIndex)
	}
	return stack, nil
}

func cloneLabels(labels map[string][]string) LabelSet {
	cloned := make(LabelSet, len(labels))
	for key, values := range labels {
		clonedValues := make([]string, len(values))
		for i, value := range values {
			clonedValues[i] = strings.Clone(value)
		}
		cloned[strings.Clone(key)] = clonedValues
	}
	return cloned
}
