package changes

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// RequirePrimaryRead repeats the content gate for whole-CS comparison and decisions,
// including removal of a declaration and clients that omit a diff snapshot.
func RequirePrimaryRead(ctx context.Context, q *store.Queries, actor auth.Actor, cs uuid.UUID) error {
	if actor.Rights.Has(auth.ContentRead) {
		return nil
	}
	rows, err := q.ChangesetOperationActors(ctx, cs)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if row.Type == ops.DocumentSetContent {
			return commandbus.Require(actor, auth.ContentRead)
		}
	}
	versions, err := q.ChangesetWorkingVersions(ctx, cs)
	if err != nil {
		return err
	}
	for _, row := range versions {
		var body map[string]json.RawMessage
		if err := json.Unmarshal(row.Body, &body); err != nil {
			return err
		}
		if len(body["content"]) > 0 {
			return commandbus.Require(actor, auth.ContentRead)
		}
	}
	return nil
}

// Check the complete atomic group, including undo and redo, after persistence in
// the caller's transaction. Any failure rolls the entire command back.
func (s *session) validatePrimaryEdits() error {
	envs, err := s.q.ListEnvironments(s.ctx, s.actor.ProjectID)
	if err != nil {
		return err
	}
	for _, env := range envs {
		if len(s.cs.Targets) > 0 {
			selected := false
			for _, name := range s.cs.Targets {
				selected = selected || name == env.Name
			}
			if !selected {
				continue
			}
		}
		for id, d := range s.docs {
			if !d.primaryChanged {
				continue
			}
			c, err := validation.Load(s.ctx, s.q, s.actor.ProjectID, env.Name, &s.cs.ID)
			if err != nil {
				return err
			}
			c.UseHead = true
			for objectID, doc := range s.docs {
				raw, _ := encodeBody(doc.body)
				c.Overrides[objectID] = validation.Document{ObjectID: objectID, VersionID: doc.versionID, Body: raw, Deleted: doc.deleted, Path: doc.path}
			}
			if err := c.RequirePrimaryEdit(s.ctx, id, d.body, d.path); err != nil {
				return err
			}
		}
	}
	return nil
}
