package validation_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func published(e *cmstest.Env, id uuid.UUID) validation.Document {
	var d validation.Document
	if err := e.Pool.QueryRow(context.Background(), `SELECT v.id, v.body FROM published_pointers pp JOIN object_versions v ON v.id = pp.version_id
		JOIN environments e ON e.id = pp.environment_id WHERE e.name = 'staging' AND e.project_id = $1 AND pp.object_id = $2`, e.Admin.ProjectID, id).Scan(&d.VersionID, &d.Body); err != nil {
		e.T.Fatal(err)
	}
	d.ObjectID = id
	return d
}

func check(e *cmstest.Env, cs *uuid.UUID, docs ...validation.Document) (map[string][]ir.Diagnostic, error) {
	c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", cs)
	if err != nil {
		e.T.Fatal(err)
	}
	return c.CheckPublication(context.Background(), docs)
}

func recordBasis(e *cmstest.Env) {
	c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", nil)
	if err != nil {
		e.T.Fatal(err)
	}
	if err := c.RecordBasis(context.Background()); err != nil {
		e.T.Fatal(err)
	}
}

// STU-562…566: only the batch and its dependents are checked while the basis holds; a new
// basis or an affected set over the limit checks the whole environment.
func TestIncrementalChecksDependentsOnly(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	component := create(e, cs, 0, "component", map[string]any{"type": "Box"})
	page := create(e, cs, 1, "page", ref(component, "live"))
	submit(e, cs, 2, "staging")
	publish(e, cs) // records the basis
	// Imported content outside the checks: an entity of an unknown schema.
	broken := e.ContentFixture("entity", "Unknown", map[string]any{}, "staging")

	t.Setenv(validation.CrossCheckEnv, "0")
	if problems, err := check(e, nil, published(e, page)); err != nil || validation.HasErrors(problems) {
		t.Fatal(problems, err)
	}
	edit := e.Edit(e.Admin, component, "x")
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root,type}','"Missing"') WHERE changeset_id=$1`, edit)
	problems, err := check(e, &edit, doc(e, edit, component))
	if err != nil || len(problems[page.String()]) == 0 || len(problems[component.String()]) == 0 || len(problems[broken.String()]) != 0 {
		t.Fatal(problems, err)
	}
	// The full check would see the broken entity: the cross-check reports the difference.
	t.Setenv(validation.CrossCheckEnv, "1")
	if _, err := check(e, nil, published(e, page)); err == nil || !strings.Contains(err.Error(), "cross-check") {
		t.Fatal(err)
	}
	t.Setenv(validation.CrossCheckEnv, "0")
	// Over the limit the check falls back to the whole environment.
	restore := validation.SetMaxAffected(0)
	if problems, err := check(e, &edit, doc(e, edit, component)); err != nil || len(problems[broken.String()]) == 0 {
		t.Fatal(problems, err)
	}
	c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", nil)
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := validation.PublishedDependents(context.Background(), e.Q, e.Admin.ProjectID, c.Environment.ID, nil, []uuid.UUID{component}, false); err != nil || len(rows) != 3 {
		t.Fatal(len(rows), err)
	}
	restore()
	if rows, err := validation.PublishedDependents(context.Background(), e.Q, e.Admin.ProjectID, c.Environment.ID, nil, []uuid.UUID{component}, false); err != nil || len(rows) != 1 || rows[0].ObjectID != page {
		t.Fatal(rows, err)
	}
	// Changed project settings change the basis: the check is full again.
	e.Exec(`UPDATE projects SET settings = settings || '{"note":"basis"}' WHERE id = $1`, e.Admin.ProjectID)
	if problems, err := check(e, nil, published(e, page)); err != nil || len(problems[broken.String()]) == 0 {
		t.Fatal(problems, err)
	}
}

// STU-563: a page pinning an old component version depends on what that version uses, even
// when the component's published version no longer does.
func TestIncrementalPinnedComponentVersion(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	inner := create(e, cs, 0, "component", map[string]any{"type": "Box"})
	wrapper := create(e, cs, 1, "component", ref(inner, "live"))
	submit(e, cs, 2, "staging")
	publish(e, cs)
	// The wrapper's v2 drops the inner component; a page pins v1.
	next := e.Edit(e.Admin, wrapper, "v2")
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root}','{"id":"n_root","type":"Box"}') WHERE changeset_id=$1`, next)
	pin := create(e, next, 1, "page", ref(wrapper, 1))
	submit(e, next, 2, "staging")
	publish(e, next)

	edit := e.Edit(e.Admin, inner, "broken")
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root,type}','"Missing"') WHERE changeset_id=$1`, edit)
	problems, err := check(e, &edit, doc(e, edit, inner))
	if err != nil || len(problems[pin.String()]) == 0 {
		t.Fatal(problems, err)
	}
}

// STU-565: singleton and unique compare the batch with every entity of its schema.
func TestIncrementalConstraints(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	var app map[string]any
	if err := json.Unmarshal([]byte(cmstest.DefaultManifest), &app); err != nil {
		t.Fatal(err)
	}
	app["schemas"] = map[string]any{
		"Article": map[string]any{"version": 1, "singleton": true, "fields": map[string]any{"slug": map[string]any{"type": "string", "unique": true}, "title": map[string]any{"type": "text", "localized": true, "unique": true}}},
		"Note":    map[string]any{"version": 1, "fields": map[string]any{"text": map[string]any{"type": "string"}}},
	}
	raw, _ := json.Marshal(app)
	e.ActivateManifest("staging", raw)
	e.ContentFixture("entity", "Article", map[string]any{"slug": "same", "title": map[string]any{"ru": "Одинаково"}}, "staging")
	recordBasis(e)
	entity := func(schema string, body map[string]any) validation.Document {
		id := e.ContentFixture("entity", schema, body)
		var d validation.Document
		// A new entity of the batch: its version is not the head until published.
		if err := e.Pool.QueryRow(ctx, `UPDATE objects o SET head_version_id = NULL FROM objects b
			WHERE o.id = $1 AND b.id = o.id RETURNING b.head_version_id`, id).Scan(&d.VersionID); err != nil {
			t.Fatal(err)
		}
		d.ObjectID = id
		d.Body, _ = json.Marshal(body)
		return d
	}
	codes := func(problems map[string][]ir.Diagnostic, id uuid.UUID) map[string]bool {
		out := map[string]bool{}
		for _, d := range problems[id.String()] {
			out[string(d.Code)] = true
		}
		return out
	}
	twin := entity("Article", map[string]any{"slug": "same", "title": map[string]any{"ru": "Одинаково"}})
	for _, publishedOnly := range []bool{false, true} {
		c, err := validation.Load(ctx, e.Q, e.Admin.ProjectID, "staging", nil)
		if err != nil {
			t.Fatal(err)
		}
		c.PublishedOnly = publishedOnly
		problems, err := c.CheckPublication(ctx, []validation.Document{twin})
		if got := codes(problems, twin.ObjectID); err != nil || !got["CONTENT_SINGLETON"] || !got["CONTENT_UNIQUE"] {
			t.Fatal(publishedOnly, problems, err)
		}
	}
	// A different localized value does not collide; the schema is still a singleton.
	other := entity("Article", map[string]any{"slug": "other", "title": map[string]any{"ru": "Иначе"}})
	problems, err := check(e, nil, other)
	if got := codes(problems, other.ObjectID); err != nil || !got["CONTENT_SINGLETON"] || got["CONTENT_UNIQUE"] {
		t.Fatal(problems, err)
	}
	note := entity("Note", map[string]any{"text": "free"})
	if problems, err := check(e, nil, note); err != nil || validation.HasErrors(problems) {
		t.Fatal(problems, err)
	}
	removed := twin
	removed.Deleted = true
	c, err := validation.Load(ctx, e.Q, e.Admin.ProjectID, "staging", nil)
	if err != nil {
		t.Fatal(err)
	}
	c.PublishedOnly = true
	if problems, err := c.CheckPublication(ctx, []validation.Document{removed}); err != nil || validation.HasErrors(problems) {
		t.Fatal(problems, err)
	}
	if _, err := check(e, nil, validation.Document{ObjectID: uuid.New()}); err == nil {
		t.Fatal("missing identity hidden")
	}
}

// STU-567/568: the dependents helper and the generic cross-check.
func TestCrossChecked(t *testing.T) {
	t.Setenv(validation.CrossCheckEnv, "1")
	if r, err := validation.CrossChecked(func(all bool) (int, error) { return 1, nil }); err != nil || r != 1 {
		t.Fatal(r, err)
	}
	if _, err := validation.CrossChecked(func(all bool) (bool, error) { return all, nil }); err == nil {
		t.Fatal("difference hidden")
	}
	failure := errors.New("storage")
	if _, err := validation.CrossChecked(func(all bool) (int, error) {
		if all {
			return 0, failure
		}
		return 1, nil
	}); !errors.Is(err, failure) {
		t.Fatal(err)
	}
}
