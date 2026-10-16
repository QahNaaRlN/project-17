package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"strings"
	"testing"
)

const primaryManifest = `{"manifestVersion":"1.0","app":{"id":"test","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"breakpoints":{},"tokens":{},"schemas":{"Article":{"version":1,"fields":{"title":{"type":"text"},"cover":{"type":"asset","assetKind":"image"}}}}}`

// STU-205/210/211/212/216: unavailable dependencies produce safe, scoped failures.
func TestPrimaryResolverFailuresSTU205210211212216(t *testing.T) {
	for _, kind := range []string{"route", "invalid-id", "absent", "kind", "deleted", "schema", "version", "missing-version", "unknown-schema", "asset", "asset-kind", "head"} {
		t.Run(kind, func(t *testing.T) {
			e := cmstest.New(t)
			e.ActivateManifest("staging", []byte(primaryManifest))
			env, err := e.Q.GetEnvironmentByName(context.Background(), store.GetEnvironmentByNameParams{ProjectID: e.Admin.ProjectID, Name: "staging"})
			if err != nil {
				t.Fatal(err)
			}
			id := e.ContentFixture("entity", "Article", map[string]any{"title": "Visible", "private": "private unused"}, "staging")
			declaration := map[string]any{"schema": "Article", "resolve": map[string]any{"by": "entity", "entityId": id.String()}}
			expected := "CONTENT_REFERENCE_UNPUBLISHED"
			switch kind {
			case "route":
				declaration["resolve"] = map[string]any{"by": "routeParam", "param": "slug"}
				expected = "BINDING_PATH_UNRESOLVED"
			case "invalid-id":
				declaration["resolve"] = map[string]any{"by": "entity", "entityId": "invalid"}
			case "absent":
				declaration["resolve"] = map[string]any{"by": "entity", "entityId": uuid.New().String()}
			case "kind":
				e.Exec(`UPDATE objects SET kind='asset',schema_name=NULL WHERE id=$1`, id)
				expected = "CONTENT_REFERENCE_SCHEMA"
			case "deleted":
				e.Exec(`UPDATE object_versions SET deleted=true WHERE object_id=$1`, id)
				expected = "CONTENT_REFERENCE_SCHEMA"
			case "schema":
				declaration["schema"] = "Unknown"
				expected = "CONTENT_REFERENCE_SCHEMA"
			case "version":
				e.Exec(`UPDATE object_versions SET schema_version=2 WHERE object_id=$1`, id)
				expected = "SCHEMA_UPCAST_REQUIRED"
			case "missing-version":
				e.Exec(`UPDATE object_versions SET schema_version=NULL WHERE object_id=$1`, id)
				expected = "SCHEMA_UPCAST_REQUIRED"
			case "unknown-schema":
				e.Exec(`UPDATE objects SET schema_name='Unknown' WHERE id=$1`, id)
				declaration["schema"] = "Unknown"
				expected = "SCHEMA_UPCAST_REQUIRED"
			case "asset", "asset-kind":
				asset := uuid.New()
				if kind == "asset-kind" {
					asset = e.ContentFixture("entity", "Article", map[string]any{"title": "Wrong kind"}, "staging")
				}
				raw, _ := json.Marshal(map[string]any{"title": "Visible", "cover": map[string]any{"assetId": asset.String()}})
				e.Exec(`UPDATE object_versions SET body=$2 WHERE object_id=$1`, id, raw)
			case "head":
				e.Exec(`DELETE FROM published_pointers WHERE object_id=$1`, id)
			}
			raw, _ := json.Marshal(map[string]any{"content": declaration, "nodes": map[string]any{"n_root": map[string]any{"bindings": map[string]any{"text": "$content.title", "src": "$content.cover"}}}})
			page := &Page{Document: raw, Data: map[string]any{}}
			a := Access{ProjectID: e.Admin.ProjectID, EnvironmentID: env.ID, Environment: "staging"}
			if err := resolvePrimary(context.Background(), e.Q, a, page, ""); err != nil {
				t.Fatal(err)
			}
			found := false
			for _, d := range page.Diagnostics {
				found = found || string(d.Code) == expected
			}
			if !found {
				t.Fatalf("missing %s: %+v", expected, page.Diagnostics)
			}
			encoded, _ := json.Marshal(page)
			if strings.Contains(string(encoded), "private unused") {
				t.Fatal("private data leaked")
			}
			if kind == "head" {
				a.Draft = true
				next := &Page{Document: raw, Data: map[string]any{}}
				if err := resolvePrimary(context.Background(), e.Q, a, next, ""); err != nil || next.Data["content"].(map[string]any)["title"] != "Visible" {
					t.Fatal(next, err)
				}
			}
		})
	}
}

// STU-210/216: infrastructure errors must not masquerade as absent public content.
func TestPrimaryResolverReadFaultsSTU210216(t *testing.T) {
	for _, table := range []string{"projects", "objects", "manifests"} {
		t.Run(table, func(t *testing.T) {
			e := cmstest.New(t)
			e.ActivateManifest("staging", []byte(primaryManifest))
			env, err := e.Q.GetEnvironmentByName(context.Background(), store.GetEnvironmentByNameParams{ProjectID: e.Admin.ProjectID, Name: "staging"})
			if err != nil {
				t.Fatal(err)
			}
			id := e.ContentFixture("entity", "Article", map[string]any{"title": "Visible"}, "staging")
			a := Access{ProjectID: e.Admin.ProjectID, EnvironmentID: env.ID, Environment: "staging"}
			if table == "projects" {
				if err := resolvePrimary(context.Background(), e.Q, a, &Page{Document: []byte(`[]`), Data: map[string]any{}}, ""); err == nil {
					t.Fatal("invalid document accepted")
				}
			}
			raw, _ := json.Marshal(map[string]any{"content": map[string]any{"schema": "Article", "resolve": map[string]any{"by": "entity", "entityId": id}}})
			e.Break(table, "")
			cmstest.ExpectDBError(t, resolvePrimary(context.Background(), e.Q, a, &Page{Document: raw, Data: map[string]any{}}, ""))
		})
	}
}
