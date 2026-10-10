package httpapi_test

import (
	"context"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"strings"
	"testing"
)

// CNT-043/API-033/040: published and draft entity/asset reads respect environment, rights and cache boundaries.
func TestContentHTTP(t *testing.T) {
	e := setup(t)
	actor, err := auth.Authenticate(context.Background(), store.New(e.pool), "Bearer "+e.token)
	if err != nil {
		t.Fatal(err)
	}
	fixture := cmstest.Env{T: t, Pool: e.pool, Q: store.New(e.pool), Admin: actor}
	raw := strings.TrimSuffix(cmstest.DefaultManifest, "}") + `,"schemas":{"Article":{"version":1,"fields":{"title":{"type":"text"}}}}}`
	fixture.ActivateManifest("staging", []byte(raw))
	fixture.ActivateManifest("production", []byte(raw))
	id := fixture.ContentFixture("entity", "Article", map[string]any{"title": "Published"}, "staging")
	asset := fixture.ContentFixture("asset", "", map[string]any{"assetKind": "image", "fileHash": "test", "title": "Picture"}, "staging")
	key := e.command(t, "create-delivery-key", `{"environment":"staging","name":"content"}`)["key"].(string)
	for _, v := range []struct{ kind, id string }{{"entity", id.String()}, {"asset", asset.String()}} {
		path := "/delivery/v1/store/staging/" + v.kind + "/" + v.id
		resp, body := e.do(t, "GET", path, "", bearer(key))
		if resp.StatusCode != 200 || body["id"] != v.id || resp.Header.Get("ETag") == "" {
			t.Fatal(resp.StatusCode, body)
		}
		h := bearer(key)
		h["If-None-Match"] = resp.Header.Get("ETag")
		resp, _ = e.do(t, "GET", path, "", h)
		if resp.StatusCode != 304 {
			t.Fatal(resp.StatusCode)
		}
		wrong := "entity"
		if v.kind == "entity" {
			wrong = "asset"
		}
		resp, body = e.do(t, "GET", "/delivery/v1/store/staging/"+wrong+"/"+v.id, "", bearer(key))
		expectProblem(t, resp, body, 404, "NOT_FOUND")
		resp, body = e.do(t, "GET", "/delivery/v1/store/staging/"+v.kind+"/bad", "", bearer(key))
		expectProblem(t, resp, body, 400, "PARAM_INVALID")
		kind := "entities"
		if v.kind == "asset" {
			kind = "assets"
		}
		for _, suffix := range []string{"", "?environment=staging"} {
			resp, body = e.do(t, "GET", "/api/v1/"+kind+"/"+v.id+suffix, "", e.authed(nil))
			if resp.StatusCode != 200 {
				t.Fatal(resp.StatusCode, body)
			}
		}
		resp, body = e.do(t, "GET", "/api/v1/"+kind+"/bad", "", e.authed(nil))
		expectProblem(t, resp, body, 400, "PARAM_INVALID")
		resp, body = e.do(t, "GET", "/api/v1/"+kind+"/"+v.id+"?changesetId=bad", "", e.authed(nil))
		expectProblem(t, resp, body, 400, "PARAM_INVALID")
		resp, body = e.do(t, "GET", "/api/v1/"+kind+"/"+v.id+"?environment=production", "", e.authed(nil))
		expectProblem(t, resp, body, 404, "NOT_FOUND")
	}
	cs := e.command(t, "create-changeset", `{"title":"draft"}`)["id"].(string)
	e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"entity.setFields","target":"`+id.String()+`","payload":{"set":{"title":"Draft"}}}]}`)
	token := e.command(t, "create-preview-token", `{"environment":"staging","changesetId":"`+cs+`"}`)["token"].(string)
	resp, body := e.do(t, "GET", "/delivery/v1/store/staging/entity/"+id.String()+"?changesetId="+cs, "", map[string]string{"Authorization": "Preview " + token})
	if resp.StatusCode != 200 || body["data"].(map[string]any)["title"] != "Draft" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(resp.StatusCode, body)
	}
	resp, body = e.do(t, "GET", "/api/v1/entities/"+id.String()+"?changesetId="+cs, "", e.authed(nil))
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode, body)
	}
	for _, path := range []string{"/api/v1/assets/" + id.String(), "/api/v1/entities/" + asset.String(), "/api/v1/entities/" + id.String() + "?changesetId=" + uuid.NewString(), "/api/v1/entities/" + id.String() + "?environment=missing"} {
		resp, body = e.do(t, "GET", path, "", e.authed(nil))
		expectProblem(t, resp, body, 404, "NOT_FOUND")
	}
	fixture.Exec(`UPDATE object_versions SET schema_version=2 WHERE object_id=$1 AND state='working'`, id)
	resp, body = e.do(t, "GET", "/api/v1/entities/"+id.String()+"?changesetId="+cs, "", e.authed(nil))
	expectProblem(t, resp, body, 409, "SCHEMA_VERSION_AHEAD")
	fixture.Exec(`UPDATE object_versions SET schema_version=NULL WHERE object_id=$1 AND state='working'`, id)
	resp, body = e.do(t, "GET", "/api/v1/entities/"+id.String()+"?changesetId="+cs, "", e.authed(nil))
	expectProblem(t, resp, body, 409, "SCHEMA_UPCAST_REQUIRED")
	fixture.ActivateManifest("production", []byte(strings.Replace(raw, `"version":1`, `"version":2`, 1)))
	headToken := e.command(t, "create-preview-token", `{"environment":"production"}`)["token"].(string)
	resp, body = e.do(t, "GET", "/delivery/v1/store/production/entity/"+id.String(), "", map[string]string{"Authorization": "Preview " + headToken})
	expectProblem(t, resp, body, 409, "SCHEMA_UPCAST_REQUIRED")
	fixture.Exec(`UPDATE roles SET capabilities=ARRAY['design.read']`)
	for _, kind := range []string{"entities", "assets"} {
		resp, body = e.do(t, "GET", "/api/v1/"+kind+"/"+id.String(), "", e.authed(nil))
		expectProblem(t, resp, body, 403, "FORBIDDEN")
	}
}
