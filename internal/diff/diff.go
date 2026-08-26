// Package diff compares analyzed leak snapshots by semantic identity.
package diff

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/imbrooklyn/leakviz/internal/analyze"
)

// Status describes how one semantic leak bucket changed.
type Status string

const (
	StatusNew       Status = "NEW"
	StatusIncreased Status = "INCREASED"
	StatusDecreased Status = "DECREASED"
	StatusResolved  Status = "RESOLVED"
	StatusUnchanged Status = "UNCHANGED"
)

// Result contains the deterministic semantic changes between two analyses.
type Result struct {
	BeforeSource string
	AfterSource  string
	BeforeTotal  int64
	AfterTotal   int64
	Changes      []Change
}

// Change retains every exact site in one semantic bucket on both sides.
type Change struct {
	Status              Status
	SemanticFingerprint string
	BeforeCount         int64
	AfterCount          int64
	Delta               int64
	BeforeGroups        []analyze.Group
	AfterGroups         []analyze.Group
}

type bucket struct {
	count  int64
	groups []analyze.Group
}

// Compare rebuckets exact groups by semantic fingerprint and compares counts.
func Compare(before, after analyze.Analysis) (Result, error) {
	beforeBuckets, err := buildBuckets("before", before.Groups)
	if err != nil {
		return Result{}, err
	}
	afterBuckets, err := buildBuckets("after", after.Groups)
	if err != nil {
		return Result{}, err
	}

	semanticFingerprints := make([]string, 0, len(beforeBuckets)+len(afterBuckets))
	seen := make(map[string]struct{}, len(beforeBuckets)+len(afterBuckets))
	for fingerprint := range beforeBuckets {
		seen[fingerprint] = struct{}{}
		semanticFingerprints = append(semanticFingerprints, fingerprint)
	}
	for fingerprint := range afterBuckets {
		if _, exists := seen[fingerprint]; exists {
			continue
		}
		semanticFingerprints = append(semanticFingerprints, fingerprint)
	}
	sort.Strings(semanticFingerprints)

	changes := make([]Change, 0, len(semanticFingerprints))
	for _, fingerprint := range semanticFingerprints {
		beforeBucket, beforeExists := beforeBuckets[fingerprint]
		afterBucket, afterExists := afterBuckets[fingerprint]
		delta, err := checkedDelta(beforeBucket.count, afterBucket.count)
		if err != nil {
			return Result{}, fmt.Errorf("compare semantic bucket %q: %w", fingerprint, err)
		}

		changes = append(changes, Change{
			Status:              changeStatus(beforeExists, afterExists, beforeBucket.count, afterBucket.count),
			SemanticFingerprint: strings.Clone(fingerprint),
			BeforeCount:         beforeBucket.count,
			AfterCount:          afterBucket.count,
			Delta:               delta,
			BeforeGroups:        cloneGroupSlice(beforeBucket.groups),
			AfterGroups:         cloneGroupSlice(afterBucket.groups),
		})
	}
	sortChanges(changes)

	return Result{
		BeforeSource: strings.Clone(before.Source),
		AfterSource:  strings.Clone(after.Source),
		BeforeTotal:  before.Total,
		AfterTotal:   after.Total,
		Changes:      changes,
	}, nil
}

func buildBuckets(side string, groups []analyze.Group) (map[string]bucket, error) {
	buckets := make(map[string]bucket)
	for _, group := range groups {
		if group.Count <= 0 {
			return nil, fmt.Errorf("compare %s groups: group count must be positive", side)
		}

		current := buckets[group.SemanticFingerprint]
		count, err := checkedAdd(current.count, group.Count)
		if err != nil {
			return nil, fmt.Errorf("compare %s semantic bucket %q: %w", side, group.SemanticFingerprint, err)
		}
		current.count = count
		current.groups = append(current.groups, group)
		buckets[group.SemanticFingerprint] = current
	}

	for fingerprint, current := range buckets {
		sort.Slice(current.groups, func(i, j int) bool {
			if current.groups[i].Count != current.groups[j].Count {
				return current.groups[i].Count > current.groups[j].Count
			}
			return current.groups[i].ExactFingerprint < current.groups[j].ExactFingerprint
		})
		buckets[fingerprint] = current
	}
	return buckets, nil
}

func checkedAdd(left, right int64) (int64, error) {
	if left < 0 || right < 0 {
		return 0, errors.New("negative count")
	}
	if left > math.MaxInt64-right {
		return 0, errors.New("arithmetic overflow")
	}
	return left + right, nil
}

func checkedDelta(before, after int64) (int64, error) {
	if before < 0 || after < 0 {
		return 0, errors.New("negative count")
	}
	if after >= before {
		return after - before, nil
	}
	return -(before - after), nil
}

func changeStatus(beforeExists, afterExists bool, beforeCount, afterCount int64) Status {
	switch {
	case !beforeExists && afterExists:
		return StatusNew
	case beforeExists && !afterExists:
		return StatusResolved
	case afterCount > beforeCount:
		return StatusIncreased
	case afterCount < beforeCount:
		return StatusDecreased
	default:
		return StatusUnchanged
	}
}

func cloneGroupSlice(groups []analyze.Group) []analyze.Group {
	cloned := make([]analyze.Group, len(groups))
	copy(cloned, groups)
	return cloned
}

func sortChanges(changes []Change) {
	sort.Slice(changes, func(i, j int) bool {
		left, right := changes[i], changes[j]
		leftRank, rightRank := statusRank(left.Status), statusRank(right.Status)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if left.Status != right.Status {
			return left.Status < right.Status
		}
		leftMagnitude, rightMagnitude := deltaMagnitude(left.Delta), deltaMagnitude(right.Delta)
		if leftMagnitude != rightMagnitude {
			return leftMagnitude > rightMagnitude
		}
		leftMaximum := max(left.BeforeCount, left.AfterCount)
		rightMaximum := max(right.BeforeCount, right.AfterCount)
		if leftMaximum != rightMaximum {
			return leftMaximum > rightMaximum
		}
		return left.SemanticFingerprint < right.SemanticFingerprint
	})
}

func statusRank(status Status) int {
	switch status {
	case StatusNew:
		return 0
	case StatusIncreased:
		return 1
	case StatusDecreased:
		return 2
	case StatusResolved:
		return 3
	case StatusUnchanged:
		return 4
	default:
		return 5
	}
}

func deltaMagnitude(delta int64) uint64 {
	if delta >= 0 {
		return uint64(delta)
	}
	return uint64(-(delta + 1)) + 1
}
