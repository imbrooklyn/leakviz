package analyze

import "github.com/imbrooklyn/leakviz/internal/profile"

// Options controls presentation-only analysis choices.
type Options struct {
	AppPrefix string
}

// Analysis contains the deterministic exact-stack groups for one snapshot.
type Analysis struct {
	Source string
	Total  int64
	Groups []Group
}

// BlockerKind identifies a blocking primitive.
type BlockerKind string

const (
	BlockerUnknown     BlockerKind = "unknown"
	BlockerChanReceive BlockerKind = "chan_receive"
	BlockerChanSend    BlockerKind = "chan_send"
	BlockerSelect      BlockerKind = "select"
	BlockerMutex       BlockerKind = "mutex"
	BlockerRWMutex     BlockerKind = "rwmutex"
	BlockerCond        BlockerKind = "cond"
	BlockerWaitGroup   BlockerKind = "waitgroup"
)

// Blocker records the classified primitive and the exact matching symbol.
type Blocker struct {
	Kind             BlockerKind
	EvidenceFunction string
}

// Group aggregates samples with the same exact stack fingerprint.
type Group struct {
	Count               int64
	ExactFingerprint    string
	SemanticFingerprint string
	Blocker             Blocker
	Stack               []profile.Frame
	UserFrame           *profile.Frame
	Labels              []LabelKeySummary
	Findings            []Finding
}

// LabelKeySummary records weighted presence, absence, and values for one key.
type LabelKeySummary struct {
	Key     string
	Present int64
	Missing int64
	Values  []LabelValueCount
}

// LabelValueCount records the weighted count for one label value.
type LabelValueCount struct {
	Value string
	Count int64
}

// FindingKind identifies the evidence strength of an analysis finding.
type FindingKind string

const (
	FindingDetected      FindingKind = "detected"
	FindingPossibleCause FindingKind = "possible_cause"
	FindingInspect       FindingKind = "inspect"
)

// Finding is a stable machine-readable analysis observation.
type Finding struct {
	Kind    FindingKind
	Code    string
	Message string
}
