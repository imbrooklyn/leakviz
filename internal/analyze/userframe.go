package analyze

import (
	"strings"

	"github.com/imbrooklyn/leakviz/internal/profile"
)

func selectUserFrame(stack []profile.Frame, opts Options) *profile.Frame {
	if opts.AppPrefix != "" {
		for _, frame := range stack {
			if frame.Function != "" && matchesPackageBoundary(frame.Function, opts.AppPrefix) {
				return cloneFrame(frame)
			}
		}
	}

	for _, frame := range stack {
		if frame.Function != "" && !isDefaultPlumbing(frame.Function) {
			return cloneFrame(frame)
		}
	}
	return nil
}

func matchesPackageBoundary(function, prefix string) bool {
	if function == prefix {
		return true
	}
	if !strings.HasPrefix(function, prefix) || len(function) == len(prefix) {
		return false
	}
	next := function[len(prefix)]
	return next == '.' || next == '/'
}

func isDefaultPlumbing(function string) bool {
	return matchesPackageBoundary(function, "runtime") ||
		matchesPackageBoundary(function, "internal/runtime") ||
		matchesPackageBoundary(function, "sync") ||
		matchesPackageBoundary(function, "internal/sync")
}

func cloneFrame(frame profile.Frame) *profile.Frame {
	cloned := frame
	cloned.Function = strings.Clone(frame.Function)
	cloned.File = strings.Clone(frame.File)
	return &cloned
}
