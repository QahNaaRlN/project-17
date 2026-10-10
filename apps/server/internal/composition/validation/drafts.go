package validation

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// DraftWarnings checks edited documents without blocking incomplete bindings or actions.
// The caller has locked the project's environments before its CS/object locks.
func DraftWarnings(ctx context.Context, q *store.Queries, project, cs uuid.UUID, targets []string, docs []Document) (map[string]map[string][]ir.Diagnostic, error) {
	change, err := q.GetChangeset(ctx, store.GetChangesetParams{ID: cs, ProjectID: project})
	if err != nil {
		return nil, err
	}
	var candidate *store.ValidationCandidateRow
	if change.Kind == "schema" {
		row, err := q.ValidationCandidate(ctx, store.ValidationCandidateParams{ProjectID: project, ChangesetID: cs})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		} // registration may follow editing
		if err != nil {
			return nil, err
		}
		candidate = &row
	}
	envs, err := q.ListEnvironments(ctx, project)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string][]ir.Diagnostic{}
	for _, e := range envs {
		if candidate != nil && e.ID != candidate.EnvironmentID {
			continue
		}
		if len(targets) > 0 {
			selected := false
			for _, t := range targets {
				if t == e.Name {
					selected = true
				}
			}
			if !selected {
				continue
			}
		}
		if e.ActiveManifestID == nil && candidate == nil {
			continue
		}
		c, err := Load(ctx, q, project, e.Name, &cs)
		if err != nil {
			return nil, err
		}
		for _, d := range docs {
			c.Overrides[d.ObjectID] = d
		}
		for _, d := range docs {
			r, err := c.Validate(ctx, d)
			if err != nil {
				return nil, err
			}
			for _, diag := range r.Diagnostics {
				if !strings.HasPrefix(string(diag.Code), "BINDING_") && !strings.HasPrefix(string(diag.Code), "ACTION_") {
					continue
				}
				diag.Severity = ir.SeverityWarning
				if out[e.Name] == nil {
					out[e.Name] = map[string][]ir.Diagnostic{}
				}
				out[e.Name][d.ObjectID.String()] = append(out[e.Name][d.ObjectID.String()], diag)
			}
		}
	}
	return out, nil
}
