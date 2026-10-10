package validation

import (
	"context"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

type Impact struct {
	ObjectID    uuid.UUID         `json:"objectId"`
	VersionID   uuid.UUID         `json:"versionId"`
	Stage       string            `json:"stage"`
	ChangesetID *uuid.UUID        `json:"changesetId,omitempty"`
	Changes     []manifest.Change `json:"changes"`
	Diagnostics []ir.Diagnostic   `json:"diagnostics"`
}

// AnalyzeImpact checks separate head/published/working versions against a proposed
// manifest. Transitive Composed usage participates even if the page has no direct use.
func AnalyzeImpact(ctx context.Context, q *store.Queries, project uuid.UUID, environment string, proposed any) ([]Impact, error) {
	rows, err := q.ManifestImpactVersions(ctx, store.ManifestImpactVersionsParams{ProjectID: project, Environment: environment})
	if err != nil {
		return nil, err
	}
	c, err := Load(ctx, q, project, environment, nil)
	if err != nil {
		return nil, err
	}
	active := c.app
	changes := manifest.Diff(active, proposed)
	out := []Impact{}
	for _, row := range rows {
		c.ChangesetID = row.ChangesetID
		c.UseHead = row.Stage != "published"
		c.Overrides = map[uuid.UUID]Document{}
		uses := []manifest.Usage{}
		seen := map[uuid.UUID]bool{}
		var collect func(Document, int) error
		collect = func(d Document, depth int) error {
			if seen[d.VersionID] || depth > 128 {
				return nil
			}
			seen[d.VersionID] = true
			doc, err := decode(d.Body)
			if err != nil {
				return err
			}
			uses = append(uses, manifest.DocumentUses(doc, active)...)
			nodes := object(doc["nodes"])
			keys := sortedKeys(nodes)
			for _, id := range keys {
				node := object(nodes[id])
				if node["type"] != "Composed" {
					continue
				}
				child, ok, err := c.resolve(ctx, object(node["ref"]))
				if err != nil {
					return err
				}
				if ok {
					if err := collect(child, depth+1); err != nil {
						return err
					}
				}
			}
			return nil
		}
		d := Document{row.ObjectID, row.VersionID, row.Body}
		if err := collect(d, 0); err != nil {
			return nil, err
		}
		used := manifest.UsedChanges(changes, uses)
		c.app = proposed
		r, err := c.Validate(ctx, d)
		c.app = active
		if err != nil {
			return nil, err
		}
		if len(used) > 0 || !r.Valid {
			out = append(out, Impact{row.ObjectID, row.VersionID, row.Stage, row.ChangesetID, used, r.Diagnostics})
		}
	}
	return out, nil
}
