package validation

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"time"
)

// RecordDiagnostics belongs to the same transaction as the check and its effects.
func (c *Context) RecordDiagnostics(ctx context.Context, cs store.Changeset, problems map[string][]ir.Diagnostic) error {
	if problems == nil {
		problems = map[string][]ir.Diagnostic{}
	}
	raw, err := json.Marshal(problems)
	if err != nil {
		return err
	}
	return c.q.RecordManifestDiagnostics(ctx, store.RecordManifestDiagnosticsParams{ChangesetID: cs.ID, ProjectID: c.ProjectID, EnvironmentID: c.Environment.ID, ManifestID: c.ManifestID, CheckedSeq: cs.Seq, Diagnostics: raw})
}

type EnvironmentDiagnostics struct {
	Environment  string                     `json:"environment"`
	ManifestHash *string                    `json:"manifestHash"`
	Documents    map[string][]ir.Diagnostic `json:"documents"`
	CheckedAt    *time.Time                 `json:"checkedAt"`
	Stale        bool                       `json:"stale"`
}

func Diagnostics(ctx context.Context, q *store.Queries, project, cs uuid.UUID) ([]EnvironmentDiagnostics, bool, error) {
	rows, err := q.ChangesetManifestDiagnostics(ctx, store.ChangesetManifestDiagnosticsParams{ProjectID: project, ID: cs})
	if err != nil {
		return nil, false, err
	}
	out := make([]EnvironmentDiagnostics, 0, len(rows))
	attention := false
	for _, r := range rows {
		documents := map[string][]ir.Diagnostic{}
		if len(r.Diagnostics) > 0 {
			if err := json.Unmarshal(r.Diagnostics, &documents); err != nil {
				return nil, false, err
			}
		}
		attention = attention || r.Stale || HasErrors(documents)
		out = append(out, EnvironmentDiagnostics{r.Environment, r.ManifestHash, documents, r.CheckedAt, r.Stale})
	}
	return out, attention, nil
}
