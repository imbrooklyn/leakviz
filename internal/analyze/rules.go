package analyze

import "github.com/imbrooklyn/leakviz/internal/profile"

type blockerRule struct {
	kind     BlockerKind
	symbols  []string
	fallback bool
}

// Rules and symbols are ordered from highest to lowest priority.
var blockerRules = [...]blockerRule{
	{
		kind:    BlockerCond,
		symbols: []string{"sync.(*Cond).Wait"},
	},
	{
		kind:    BlockerWaitGroup,
		symbols: []string{"sync.(*WaitGroup).Wait"},
	},
	{
		kind: BlockerRWMutex,
		symbols: []string{
			"sync.(*RWMutex).Lock",
			"sync.(*RWMutex).RLock",
			"sync.runtime_SemacquireRWMutex",
			"sync.runtime_SemacquireRWMutexR",
		},
	},
	{
		kind: BlockerMutex,
		symbols: []string{
			"sync.(*Mutex).Lock",
			"internal/sync.(*Mutex).lockSlow",
		},
	},
	{
		kind: BlockerSelect,
		symbols: []string{
			"runtime.selectgo",
			"runtime.block",
		},
	},
	{
		kind: BlockerChanSend,
		symbols: []string{
			"runtime.chansend1",
			"runtime.chansend",
		},
	},
	{
		kind: BlockerChanReceive,
		symbols: []string{
			"runtime.chanrecv1",
			"runtime.chanrecv",
		},
	},
	{
		kind:     BlockerUnknown,
		fallback: true,
	},
}

func classify(stack []profile.Frame) Blocker {
	for _, rule := range blockerRules {
		if rule.fallback {
			return Blocker{Kind: rule.kind}
		}
		for _, symbol := range rule.symbols {
			for _, frame := range stack {
				if frame.Function == symbol {
					return Blocker{
						Kind:             rule.kind,
						EvidenceFunction: frame.Function,
					}
				}
			}
		}
	}
	// Keep a defensive fallback if the table is accidentally incomplete.
	return Blocker{Kind: BlockerUnknown}
}
