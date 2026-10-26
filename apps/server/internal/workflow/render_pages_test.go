package workflow

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// STU-482: routed pages of the Change Set and published pages that use a changed live
// component, transitively through another component; pinned versions are not affected.
func TestRenderPagesFollowsLiveComponents(t *testing.T) {
	e := cmstest.New(t)
	ctx := context.Background()
	env, err := e.Q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: e.Admin.ProjectID, Name: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	body := func(kind string, refs ...map[string]any) []byte {
		nodes := map[string]any{"n_root": map[string]any{"id": "n_root", "type": "Box"}}
		for i, ref := range refs {
			nodes["n_ref"+string(rune('a'+i))] = map[string]any{"type": "Composed", "ref": ref}
		}
		raw, _ := json.Marshal(map[string]any{"kind": kind, "root": "n_root", "nodes": nodes})
		return raw
	}
	path := func(p string) *string { return &p }
	publication := uuid.New()
	e.Exec("INSERT INTO publications (id, project_id, environment_id, kind, actor_id) VALUES ($1, $2, $3, 'publish', $4)", publication, e.Admin.ProjectID, env.ID, e.Admin.ID)
	publish := func(kind string, raw []byte, route *string) uuid.UUID {
		id, version := uuid.New(), uuid.New()
		e.Exec("INSERT INTO objects (id, project_id, kind, doc_kind) VALUES ($1, $2, 'document', $3)", id, e.Admin.ProjectID, kind)
		e.Exec("INSERT INTO object_versions (id, project_id, object_id, number, state, body, body_hash, path, created_by) VALUES ($1, $2, $3, 1, 'committed', $4, $5, $6, $7)",
			version, e.Admin.ProjectID, id, raw, []byte{0}, route, e.Admin.ID)
		e.Exec("INSERT INTO published_pointers (environment_id, object_id, version_id, publication_id) VALUES ($1, $2, $3, $4)", env.ID, id, version, publication)
		return id
	}
	button := publish("component", body("component"), nil)
	card := publish("component", body("component", map[string]any{"component": button.String()}), nil)
	cardPage := publish("page", body("page", map[string]any{"component": card.String()}), path("/cards"))
	publish("page", body("page", map[string]any{"component": button.String(), "version": 1}), path("/pinned"))
	publish("page", body("page"), path("/other"))
	publish("page", body("page", map[string]any{"component": button.String()}), nil)

	versions := []store.ChangesetWorkingVersionsRow{
		{ObjectID: button, Body: body("component")},
		{ObjectID: uuid.New(), Body: body("page"), Path: path("/new")},
		{ObjectID: uuid.New(), Body: body("page")},
		{ObjectID: uuid.New(), Body: []byte("not json")},
		{ObjectID: uuid.New(), Body: body("page"), Path: path("/gone"), Deleted: true},
	}
	pages, err := renderPages(ctx, e.Q, e.Admin.ProjectID, env.ID, versions)
	if err != nil || len(pages) != 2 || pages[0].Path != "/cards" || pages[0].ObjectID != cardPage || pages[1].Path != "/new" {
		t.Fatal(pages, err)
	}
	// Without a changed component only the Change Set's own pages render.
	pages, err = renderPages(ctx, e.Q, e.Admin.ProjectID, env.ID, versions[1:])
	if err != nil || len(pages) != 1 || pages[0].Path != "/new" {
		t.Fatal(pages, err)
	}
}
