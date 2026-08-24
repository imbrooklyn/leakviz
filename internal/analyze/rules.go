package analyze

import "github.com/imbrooklyn/leakviz/internal/profile"

type blockerRule struct {
	kind    BlockerKind
	symbols []string
}

// Rules and symbols are ordered from highest to lowest priority.
var blockerRules = [...]blockerRule{
	{
		kind: BlockerChanReceive,
		symbols: []string{
			"runtime.chanrecv1",
			"runtime.chanrecv",
		},
	},
}

func classify(stack []profile.Frame) Blocker {
	for _, rule := range blockerRules {
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
	return Blocker{Kind: BlockerUnknown}
}
