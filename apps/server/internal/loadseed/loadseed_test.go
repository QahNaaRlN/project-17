package loadseed_test

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/loadseed"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

// STU-542, 556: generated pages are valid IR within NFR-012, and generation is deterministic.
func TestDocumentsAndEntities(t *testing.T) {
	a, b := rand.New(rand.NewPCG(1, 1)), rand.New(rand.NewPCG(1, 1))
	for i := 0; i < 200; i++ {
		doc := loadseed.Document(a)
		if !reflect.DeepEqual(doc, loadseed.Document(b)) {
			t.Fatal("not deterministic")
		}
		raw, _ := json.Marshal(doc)
		if r := ir.ValidateJSON(raw); !r.Valid {
			t.Fatal(r.Diagnostics)
		}
		if n := len(doc["nodes"].(map[string]any)); n < 20 || n > 300 {
			t.Fatal(n)
		}
	}
	if !reflect.DeepEqual(loadseed.Entity(a), loadseed.Entity(b)) {
		t.Fatal("entities")
	}
	if loadseed.Slug(3) != "load-03" || loadseed.PagePath(7) != "/p/7" ||
		loadseed.EntityID(2, 255).String() != "00000000-0000-4000-8000-0002000000ff" ||
		loadseed.DocumentID(2, 1).String() != "00000000-0001-4000-8000-000200000001" {
		t.Fatal("identifiers")
	}
	if p := loadseed.Profile(); p.Projects != 20 || p.Entities != 100_000 || p.Documents != 10_000 || p.Editors != 50 {
		t.Fatal(p)
	}
}

// STU-542, 556: seeded content is published as a publication leaves it and is isolated per project.
func TestRun(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewDB(t)
	cfg := loadseed.Config{Projects: 2, Entities: 30, Documents: 12, Editors: 3, Seed: 7, TokenTTL: 3600e9}
	var lines int
	res, err := loadseed.Run(ctx, pool, cfg, func(string, ...any) { lines++ })
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Projects) != 2 || len(res.Editors) != 3 || lines != 2 || res.Projects[1].Slug != "load-02" {
		t.Fatal(res)
	}
	q := store.New(pool)
	for _, p := range res.Projects {
		access, err := delivery.Authenticate(ctx, q, "Bearer "+p.DeliveryKey)
		if err != nil || access.ProjectSlug != p.Slug || access.Environment != "staging" {
			t.Fatal(access, err)
		}
		page, err := delivery.GetPage(ctx, q, access, loadseed.PagePath(11))
		if err != nil || page.Page.ObjectID != loadseed.DocumentID(p.Index, 11) {
			t.Fatal(page.Page, err)
		}
		routes, err := delivery.GetRoutes(ctx, q, access)
		if err != nil || len(routes) != 12 {
			t.Fatal(len(routes), err)
		}
		entity, err := changes.GetContent(ctx, q, access.ProjectID, loadseed.EntityID(p.Index, 29), "staging", nil, false)
		if err != nil || entity.Kind != "entity" {
			t.Fatal(entity, err)
		}
		// Another project's objects are not visible through this key.
		other := 3 - p.Index
		if _, err := changes.GetContent(ctx, q, access.ProjectID, loadseed.EntityID(other, 0), "staging", nil, false); err == nil {
			t.Fatal("isolation")
		}
		if _, err := auth.Authenticate(ctx, q, "Bearer "+p.AdminToken); err != nil {
			t.Fatal(err)
		}
	}
	editor, err := auth.Authenticate(ctx, q, "Bearer "+res.Editors[0])
	if err != nil || !editor.Rights.Has(auth.ContentWrite) || editor.Rights.Has(auth.ProjectAdmin) {
		t.Fatal(editor.Rights, err)
	}
	// A second run with the same slugs fails instead of mixing data.
	if _, err := loadseed.Run(ctx, pool, cfg, func(string, ...any) {}); err == nil {
		t.Fatal("rerun")
	}
}

// A storage failure at any step stops the seed with an error.
func TestRunStorageFailures(t *testing.T) {
	cfg := loadseed.Config{Projects: 1, Entities: 2, Documents: 2, Editors: 1, Seed: 1, TokenTTL: 3600e9}
	for _, c := range []struct{ table, op string }{
		{"manifests", "INSERT"}, {"environments", "UPDATE"}, {"project_render_checks", "INSERT"}, {"publications", "INSERT"},
		{"objects", "INSERT"}, {"object_versions", "INSERT"}, {"published_pointers", "INSERT"}, {"routes", "INSERT"},
		{"objects", "UPDATE"}, {"delivery_keys", "INSERT"},
	} {
		t.Run(c.table+" "+c.op, func(t *testing.T) {
			pool := pgtest.NewDB(t)
			if _, err := pool.Exec(context.Background(), `CREATE FUNCTION cms_test_fail() RETURNS trigger LANGUAGE plpgsql AS
				$$BEGIN RAISE EXCEPTION 'сбой, внесённый тестом'; END$$;
				CREATE TRIGGER cms_test_fail BEFORE `+c.op+` ON `+c.table+` FOR EACH STATEMENT EXECUTE FUNCTION cms_test_fail()`); err != nil {
				t.Fatal(err)
			}
			if _, err := loadseed.Run(context.Background(), pool, cfg, func(string, ...any) {}); err == nil {
				t.Fatal("error expected")
			}
		})
	}
}
