package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/imbrooklyn/leakviz/internal/analyze"
	"github.com/imbrooklyn/leakviz/internal/diff"
)

// WriteDiffText writes the deterministic plain-text form of a diff result.
func WriteDiffText(w io.Writer, result diff.Result) error {
	if w == nil {
		return fmt.Errorf("write text diff report: nil writer")
	}

	var output strings.Builder
	output.WriteString("LEAKVIZ DIFF\n")
	fmt.Fprintf(&output, "Before: %s (total=%d)\n", escapeTextScalar(displaySource(result.BeforeSource)), result.BeforeTotal)
	fmt.Fprintf(&output, "After: %s (total=%d)\n", escapeTextScalar(displaySource(result.AfterSource)), result.AfterTotal)
	fmt.Fprintf(&output, "Changes: %d\n", len(result.Changes))

	for index, change := range result.Changes {
		output.WriteByte('\n')
		writeTextChange(&output, index+1, change)
	}

	rendered := output.String()
	written, err := io.WriteString(w, rendered)
	if err != nil {
		return fmt.Errorf("write text diff report: %w", err)
	}
	if written != len(rendered) {
		return fmt.Errorf("write text diff report: %w", io.ErrShortWrite)
	}
	return nil
}

func writeTextChange(output *strings.Builder, number int, change diff.Change) {
	fmt.Fprintf(output, "Change %d\n", number)
	fmt.Fprintf(output, "  Status: %s\n", escapeTextScalar(string(change.Status)))
	fmt.Fprintf(output, "  Semantic fingerprint: %s\n", escapeTextScalar(change.SemanticFingerprint))
	fmt.Fprintf(output, "  Before count: %d\n", change.BeforeCount)
	fmt.Fprintf(output, "  After count: %d\n", change.AfterCount)
	fmt.Fprintf(output, "  Delta: %s\n", textDelta(change.Delta))

	representative := representativeGroup(change)
	output.WriteString("  Representative: ")
	if representative == nil || representative.UserFrame == nil || representative.UserFrame.Function == "" {
		output.WriteString("-\n")
	} else {
		fmt.Fprintf(
			output,
			"%s (%s:%d)\n",
			escapeTextScalar(shortFunction(representative.UserFrame.Function)),
			escapeTextScalar(displayBase(representative.UserFrame.File)),
			representative.UserFrame.Line,
		)
	}

	exactFingerprint := "-"
	if representative != nil && representative.ExactFingerprint != "" {
		exactFingerprint = representative.ExactFingerprint
	}
	fmt.Fprintf(output, "  Representative exact fingerprint: %s\n", escapeTextScalar(exactFingerprint))
	fmt.Fprintf(output, "  Before sites: %d\n", len(change.BeforeGroups))
	fmt.Fprintf(output, "  After sites: %d\n", len(change.AfterGroups))
}

func representativeGroup(change diff.Change) *analyze.Group {
	groups := change.AfterGroups
	if change.Status == diff.StatusResolved {
		groups = change.BeforeGroups
	}
	if len(groups) == 0 {
		return nil
	}
	return &groups[0]
}

func textDelta(delta int64) string {
	if delta > 0 {
		return fmt.Sprintf("+%d", delta)
	}
	return fmt.Sprintf("%d", delta)
}
