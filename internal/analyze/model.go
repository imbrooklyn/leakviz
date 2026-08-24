package analyze

// Options controls presentation-only analysis choices.
type Options struct {
	AppPrefix string
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
