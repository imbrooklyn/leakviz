package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/imbrooklyn/leakviz/internal/analyze"
)

// WriteText writes the deterministic plain-text form of an analysis.
func WriteText(w io.Writer, analysis analyze.Analysis) error {
	if w == nil {
		return fmt.Errorf("write text report: nil writer")
	}

	var output strings.Builder
	output.WriteString("LEAKVIZ ANALYSIS\n")
	fmt.Fprintf(&output, "Source: %s\n", escapeTextScalar(displaySource(analysis.Source)))
	fmt.Fprintf(&output, "Total: %d\n", analysis.Total)
	fmt.Fprintf(&output, "Groups: %d\n", len(analysis.Groups))

	for index, group := range analysis.Groups {
		output.WriteByte('\n')
		writeTextGroup(&output, index+1, group)
	}

	rendered := output.String()
	written, err := io.WriteString(w, rendered)
	if err != nil {
		return fmt.Errorf("write text report: %w", err)
	}
	if written != len(rendered) {
		return fmt.Errorf("write text report: %w", io.ErrShortWrite)
	}
	return nil
}

func writeTextGroup(output *strings.Builder, number int, group analyze.Group) {
	fmt.Fprintf(output, "Group %d\n", number)
	fmt.Fprintf(output, "  Count: %d\n", group.Count)
	fmt.Fprintf(output, "  Exact fingerprint: %s\n", escapeTextScalar(group.ExactFingerprint))
	fmt.Fprintf(output, "  Semantic fingerprint: %s\n", escapeTextScalar(group.SemanticFingerprint))
	fmt.Fprintf(output, "  Blocker: %s\n", escapeTextScalar(string(group.Blocker.Kind)))

	evidence := "-"
	if group.Blocker.EvidenceFunction != "" {
		evidence = shortFunction(group.Blocker.EvidenceFunction)
	}
	fmt.Fprintf(output, "  Evidence: %s\n", escapeTextScalar(evidence))

	output.WriteString("  User frame: ")
	if group.UserFrame == nil || group.UserFrame.Function == "" {
		output.WriteString("-\n")
	} else {
		fmt.Fprintf(
			output,
			"%s (%s:%d)\n",
			escapeTextScalar(shortFunction(group.UserFrame.Function)),
			escapeTextScalar(displayBase(group.UserFrame.File)),
			group.UserFrame.Line,
		)
	}

	output.WriteString("  Stack:\n")
	for _, frame := range group.Stack {
		fmt.Fprintf(
			output,
			"    - %s (%s:%d)",
			escapeTextScalar(shortFunction(frame.Function)),
			escapeTextScalar(displayBase(frame.File)),
			frame.Line,
		)
		if frame.Inlined {
			output.WriteString(" [inlined]")
		}
		output.WriteByte('\n')
	}

	if len(group.Labels) == 0 {
		output.WriteString("  Labels: none\n")
	} else {
		output.WriteString("  Labels:\n")
		for _, label := range group.Labels {
			fmt.Fprintf(
				output,
				"    - %s: present=%d missing=%d\n",
				escapeTextScalar(label.Key),
				label.Present,
				label.Missing,
			)
			for _, value := range label.Values {
				fmt.Fprintf(output, "      - %s: %d\n", escapeTextScalar(value.Value), value.Count)
			}
		}
	}

	if len(group.Findings) == 0 {
		output.WriteString("  Findings: none\n")
	} else {
		output.WriteString("  Findings:\n")
		for _, finding := range group.Findings {
			fmt.Fprintf(
				output,
				"    - %s %s: %s\n",
				escapeTextScalar(textFindingKind(finding.Kind)),
				escapeTextScalar(finding.Code),
				escapeTextScalar(finding.Message),
			)
		}
	}
}

func shortFunction(function string) string {
	separator := strings.LastIndexByte(function, '/')
	if separator < 0 {
		return function
	}
	return function[separator+1:]
}

func displaySource(source string) string {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return source
	}
	return displayBase(source)
}

func displayBase(path string) string {
	if path == "" {
		return "?"
	}
	lastSeparator := -1
	for index := 0; index < len(path); index++ {
		if path[index] == '/' || path[index] == '\\' {
			lastSeparator = index
		}
	}
	return path[lastSeparator+1:]
}

func textFindingKind(kind analyze.FindingKind) string {
	switch kind {
	case analyze.FindingDetected:
		return "DETECTED"
	case analyze.FindingPossibleCause:
		return "POSSIBLE_CAUSE"
	case analyze.FindingInspect:
		return "INSPECT"
	default:
		return string(kind)
	}
}
