package report

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/imbrooklyn/leakviz/internal/diff"
)

type diffJSON struct {
	SchemaVersion      int             `json:"schema_version"`
	Report             string          `json:"report"`
	FingerprintVersion int             `json:"fingerprint_version"`
	Before             snapshotRefJSON `json:"before"`
	After              snapshotRefJSON `json:"after"`
	Changes            []changeJSON    `json:"changes"`
}

type snapshotRefJSON struct {
	Source string `json:"source"`
	Total  int64  `json:"total"`
}

type changeJSON struct {
	Status              string      `json:"status"`
	SemanticFingerprint string      `json:"semantic_fingerprint"`
	BeforeCount         int64       `json:"before_count"`
	AfterCount          int64       `json:"after_count"`
	Delta               int64       `json:"delta"`
	BeforeGroups        []groupJSON `json:"before_groups"`
	AfterGroups         []groupJSON `json:"after_groups"`
}

// WriteDiffJSON writes the deterministic JSON schema v1 form of a diff result.
func WriteDiffJSON(w io.Writer, result diff.Result) error {
	if w == nil {
		return fmt.Errorf("write JSON diff report: nil writer")
	}

	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(newDiffJSON(result)); err != nil {
		return fmt.Errorf("encode JSON diff report: %w", err)
	}

	written, err := w.Write(output.Bytes())
	if err != nil {
		return fmt.Errorf("write JSON diff report: %w", err)
	}
	if written != output.Len() {
		return fmt.Errorf("write JSON diff report: %w", io.ErrShortWrite)
	}
	return nil
}

func newDiffJSON(result diff.Result) diffJSON {
	changes := make([]changeJSON, len(result.Changes))
	for index, change := range result.Changes {
		beforeGroups := make([]groupJSON, len(change.BeforeGroups))
		for groupIndex, group := range change.BeforeGroups {
			beforeGroups[groupIndex] = newGroupJSON(group)
		}
		afterGroups := make([]groupJSON, len(change.AfterGroups))
		for groupIndex, group := range change.AfterGroups {
			afterGroups[groupIndex] = newGroupJSON(group)
		}
		changes[index] = changeJSON{
			Status:              string(change.Status),
			SemanticFingerprint: change.SemanticFingerprint,
			BeforeCount:         change.BeforeCount,
			AfterCount:          change.AfterCount,
			Delta:               change.Delta,
			BeforeGroups:        beforeGroups,
			AfterGroups:         afterGroups,
		}
	}

	return diffJSON{
		SchemaVersion:      schemaVersion,
		Report:             "diff",
		FingerprintVersion: fingerprintVersion,
		Before: snapshotRefJSON{
			Source: result.BeforeSource,
			Total:  result.BeforeTotal,
		},
		After: snapshotRefJSON{
			Source: result.AfterSource,
			Total:  result.AfterTotal,
		},
		Changes: changes,
	}
}
