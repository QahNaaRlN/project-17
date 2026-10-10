package validation

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/policydoc"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// DraftWarnings rejects L5–L6 errors and returns incomplete L4 bindings/actions as warnings.
// The caller has locked the project's environments before its CS/object locks.
func DraftWarnings(ctx context.Context, q *store.Queries, project, cs uuid.UUID, targets []string, docs []Document) (map[string]map[string][]ir.Diagnostic, error) {
	change, err := q.GetChangeset(ctx, store.GetChangesetParams{ID: cs, ProjectID: project})
	if err != nil {
		return nil, err
	}
	var candidate *store.ValidationCandidateRow
	if change.Kind == "schema" {
		row, err := q.ValidationCandidate(ctx, store.ValidationCandidateParams{ProjectID: project, ChangesetID: cs})
		// Registration may follow schema editing; project policy still applies.
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			candidate = &row
		}
	}
	envs, err := q.ListEnvironments(ctx, project)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string][]ir.Diagnostic{}
	fatal := map[string][]ir.Diagnostic{}
	settings, err := q.GetProjectSettings(ctx, project)
	if err != nil {
		return nil, err
	}
	projectPolicy, err := decode(settings)
	if err != nil {
		return nil, err
	}
	for _, d := range docs {
		doc, err := decode(d.Body)
		if err != nil {
			return nil, err
		}
		if ir.ValidateDocument(doc).Valid {
			r := policydoc.Validate(doc, nil, policydoc.Options{Project: projectPolicy})
			if !r.Valid {
				fatal[d.ObjectID.String()] = r.Diagnostics
			}
		}
	}
	if err := RequireValid(fatal); err != nil {
		return nil, err
	}
	if change.Kind == "schema" && candidate == nil {
		return nil, nil
	}
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
		c.Draft = true
		for _, d := range docs {
			c.Overrides[d.ObjectID] = d
		}
		for _, d := range docs {
			r, err := c.Validate(ctx, d)
			if err != nil {
				return nil, err
			}
			for _, diag := range r.Diagnostics {
				if IsPolicyDiagnostic(diag) {
					fatal[e.Name+":"+d.ObjectID.String()] = append(fatal[e.Name+":"+d.ObjectID.String()], diag)
					continue
				}
				if strings.HasPrefix(string(diag.Code), "CONTENT_") || strings.HasPrefix(string(diag.Code), "SCHEMA_") {
					if diag.Code != "CONTENT_REQUIRED" && diag.Code != "CONTENT_REFERENCE_UNPUBLISHED" {
						fatal[e.Name+":"+d.ObjectID.String()] = append(fatal[e.Name+":"+d.ObjectID.String()], diag)
						continue
					}
				}
				if !strings.HasPrefix(string(diag.Code), "CONTENT_") && !strings.HasPrefix(string(diag.Code), "BINDING_") && !strings.HasPrefix(string(diag.Code), "ACTION_") && !strings.HasPrefix(string(diag.Code), "A11Y_") {
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
	return out, RequireValid(fatal)
}

func IsPolicyDiagnostic(d ir.Diagnostic) bool {
	if strings.HasPrefix(string(d.Code), "POLICY_") || strings.HasPrefix(string(d.Code), "DESIGN_") {
		return true
	}
	if nested, ok := d.Params["diagnostic"].(ir.Diagnostic); ok {
		return IsPolicyDiagnostic(nested)
	}
	return false
}
