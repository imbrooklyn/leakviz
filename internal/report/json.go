package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/imbrooklyn/leakviz/internal/analyze"
)

const (
	schemaVersion      = 1
	fingerprintVersion = 1
)

type analysisJSON struct {
	SchemaVersion      int         `json:"schema_version"`
	Report             string      `json:"report"`
	FingerprintVersion int         `json:"fingerprint_version"`
	Source             string      `json:"source"`
	Total              int64       `json:"total"`
	Groups             []groupJSON `json:"groups"`
}

type groupJSON struct {
	ExactFingerprint    string         `json:"exact_fingerprint"`
	SemanticFingerprint string         `json:"semantic_fingerprint"`
	Count               int64          `json:"count"`
	Blocker             blockerJSON    `json:"blocker"`
	UserFrame           *frameJSON     `json:"user_frame"`
	Stack               []frameJSON    `json:"stack"`
	Labels              []labelKeyJSON `json:"labels"`
	Findings            []findingJSON  `json:"findings"`
}

type blockerJSON struct {
	Kind             string `json:"kind"`
	EvidenceFunction string `json:"evidence_function"`
}

type frameJSON struct {
	Function string `json:"function"`
	File     string `json:"file"`
	Line     int64  `json:"line"`
	Inlined  bool   `json:"inlined"`
}

type labelKeyJSON struct {
	Key     string           `json:"key"`
	Present int64            `json:"present"`
	Missing int64            `json:"missing"`
	Values  []labelValueJSON `json:"values"`
}

type labelValueJSON struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

type findingJSON struct {
	Kind    string `json:"kind"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes the deterministic JSON schema v1 form of an analysis.
func WriteJSON(w io.Writer, analysis analyze.Analysis) error {
	if w == nil {
		return fmt.Errorf("write JSON report: nil writer")
	}

	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(newAnalysisJSON(analysis)); err != nil {
		return fmt.Errorf("encode JSON report: %w", err)
	}

	written, err := w.Write(output.Bytes())
	if err != nil {
		return fmt.Errorf("write JSON report: %w", err)
	}
	if written != output.Len() {
		return fmt.Errorf("write JSON report: %w", io.ErrShortWrite)
	}
	return nil
}

func newAnalysisJSON(analysis analyze.Analysis) analysisJSON {
	groups := make([]groupJSON, len(analysis.Groups))
	for index, group := range analysis.Groups {
		groups[index] = newGroupJSON(group)
	}
	return analysisJSON{
		SchemaVersion:      schemaVersion,
		Report:             "analysis",
		FingerprintVersion: fingerprintVersion,
		Source:             analysis.Source,
		Total:              analysis.Total,
		Groups:             groups,
	}
}

func newGroupJSON(group analyze.Group) groupJSON {
	stack := make([]frameJSON, len(group.Stack))
	for index, frame := range group.Stack {
		stack[index] = newFrameJSON(frame.Function, frame.File, frame.Line, frame.Inlined)
	}

	labels := make([]labelKeyJSON, len(group.Labels))
	for index, label := range group.Labels {
		values := make([]labelValueJSON, len(label.Values))
		for valueIndex, value := range label.Values {
			values[valueIndex] = labelValueJSON{Value: value.Value, Count: value.Count}
		}
		labels[index] = labelKeyJSON{
			Key:     label.Key,
			Present: label.Present,
			Missing: label.Missing,
			Values:  values,
		}
	}

	findings := make([]findingJSON, len(group.Findings))
	for index, finding := range group.Findings {
		findings[index] = findingJSON{
			Kind:    string(finding.Kind),
			Code:    finding.Code,
			Message: finding.Message,
		}
	}

	var userFrame *frameJSON
	if group.UserFrame != nil {
		converted := newFrameJSON(
			group.UserFrame.Function,
			group.UserFrame.File,
			group.UserFrame.Line,
			group.UserFrame.Inlined,
		)
		userFrame = &converted
	}

	return groupJSON{
		ExactFingerprint:    group.ExactFingerprint,
		SemanticFingerprint: group.SemanticFingerprint,
		Count:               group.Count,
		Blocker: blockerJSON{
			Kind:             string(group.Blocker.Kind),
			EvidenceFunction: group.Blocker.EvidenceFunction,
		},
		UserFrame: userFrame,
		Stack:     stack,
		Labels:    labels,
		Findings:  findings,
	}
}

func newFrameJSON(function, file string, line int64, inlined bool) frameJSON {
	return frameJSON{
		Function: function,
		File:     file,
		Line:     line,
		Inlined:  inlined,
	}
}
