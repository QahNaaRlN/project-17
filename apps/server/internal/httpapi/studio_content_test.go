package httpapi_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func studioContentFixture(t *testing.T, e env) cmstest.Env {
	t.Helper()
	f := cmstest.Env{T: t, Pool: e.pool, Q: store.New(e.pool), Admin: workspaceActor(t, e)}
	var manifest map[string]any
	if err := json.Unmarshal([]byte(cmstest.DefaultManifest), &manifest); err != nil {
		t.Fatal(err)
	}
	manifest["schemas"] = map[string]any{"Article": map[string]any{"version": 1, "fields": map[string]any{"title": map[string]any{"type": "text", "localized": true}, "body": map[string]any{"type": "richText"}}}, "Other": map[string]any{"version": 1, "fields": map[string]any{"title": map[string]any{"type": "text"}}}}
	raw, _ := json.Marshal(manifest)
	f.ActivateManifest("staging", raw)
	return f
}
func TestStudioContentCatalog(t *testing.T) {
	// STU-100/101: exact working/head base, schema filtering, tombstones and keyset pagination.
	e := setup(t)
	f := studioContentFixture(t, e)
	cs := e.command(t, "create-changeset", `{"title":"content","targets":["staging"]}`)["id"].(string)
	hiddenCS := e.command(t, "create-changeset", `{"title":"other","targets":["staging"]}`)["id"].(string)
	e.command(t, "apply-operations", `{"changesetId":"`+hiddenCS+`","expectedSeq":0,"operations":[{"type":"entity.create","payload":{"schema":"Article","environment":"staging","data":{"title":{"ru":"Hidden"}}}}]}`)
	ids := map[string]bool{}
	for i := 0; i < 52; i++ {
		id := f.ContentFixture("entity", "Article", map[string]any{"title": map[string]any{"ru": fmt.Sprint(i)}}, "staging")
		ids[id.String()] = true
	}
	f.ContentFixture("entity", "Other", map[string]any{"title": "other schema"}, "staging")
	asset := f.ContentFixture("asset", "", map[string]any{"assetKind": "image", "fileHash": "test", "mimeType": "image/png", "title": "Picture"}, "staging")
	first := ""
	for id := range ids {
		first = id
		break
	}
	e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"group":true,"operations":[{"type":"entity.setFields","target":"`+first+`","payload":{"set":{"title.ru":"Working","title.en":"Keep"}}}]}`)
	base := "/api/v1/content-catalog?environment=staging&changesetId=" + cs
	r, data := e.do(t, "GET", base+"&kind=entity&schema=Article", "", e.authed(nil))
	if r.StatusCode != 200 || len(data["items"].([]any)) != 50 || data["nextCursor"] == nil {
		t.Fatal(r.StatusCode, data)
	}
	for _, item := range data["items"].([]any) {
		delete(ids, item.(map[string]any)["id"].(string))
	}
	r, page := e.do(t, "GET", base+"&kind=entity&schema=Article&after="+data["nextCursor"].(string), "", e.authed(nil))
	if r.StatusCode != 200 || len(page["items"].([]any)) != 2 || page["nextCursor"] != nil {
		t.Fatal(r.StatusCode, page)
	}
	for _, item := range page["items"].([]any) {
		delete(ids, item.(map[string]any)["id"].(string))
	}
	if len(ids) != 0 {
		t.Fatal(ids)
	}
	record := "/api/v1/content/" + first + "?environment=staging&changesetId=" + cs
	r, data = e.do(t, "GET", record, "", e.authed(nil))
	if r.StatusCode != 200 || data["data"].(map[string]any)["title"].(map[string]any)["ru"] != "Working" {
		t.Fatal(r.StatusCode, data)
	}
	r, data = e.do(t, "GET", base+"&kind=asset", "", e.authed(nil))
	if r.StatusCode != 200 || data["items"].([]any)[0].(map[string]any)["id"] != asset.String() {
		t.Fatal(r.StatusCode, data)
	}
	e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":1,"operations":[{"type":"entity.delete","target":"`+first+`","payload":{}}]}`)
	r, data = e.do(t, "GET", record, "", e.authed(nil))
	if r.StatusCode != 200 || data["deleted"] != true {
		t.Fatal(r.StatusCode, data)
	}
	e.command(t, "undo", `{"changesetId":"`+cs+`","expectedSeq":2}`)
	r, data = e.do(t, "GET", record, "", e.authed(nil))
	if r.StatusCode != 200 || data["deleted"] != false {
		t.Fatal(r.StatusCode, data)
	}
	for _, suffix := range []string{"&unknown=1", "&environment=staging", "&after=bad", "&after=", "&kind=bad", "&kind=asset&schema=Article"} {
		path := base + suffix
		if suffix != "&kind=bad" && suffix != "&kind=asset&schema=Article" {
			path += "&kind=entity"
		}
		r, data = e.do(t, "GET", path, "", e.authed(nil))
		expectProblem(t, r, data, 400, "PARAM_INVALID")
	}
	for _, path := range []string{"/api/v1/content-catalog?environment=staging&kind=entity", "/api/v1/content-catalog?environment=production&changesetId=" + cs + "&kind=entity", record + "&after=" + uuid.NewString()} {
		r, data = e.do(t, "GET", path, "", e.authed(nil))
		expectProblem(t, r, data, 400, "PARAM_INVALID")
	}
	r, data = e.do(t, "GET", "/api/v1/content-catalog?environment=staging&changesetId="+uuid.NewString()+"&kind=entity", "", e.authed(nil))
	expectProblem(t, r, data, 404, "NOT_FOUND")
	r, data = e.do(t, "GET", "/api/v1/content/"+uuid.NewString()+"?environment=staging&changesetId="+cs, "", e.authed(nil))
	expectProblem(t, r, data, 404, "NOT_FOUND")
	// Catalog exposes only the declared locale contract, never all project settings.
	workspaceExec(t, e, `UPDATE projects SET settings='{"locales":["kk"],"defaultLocale":"kk","private":"hidden"}' WHERE id=$1`, f.Admin.ProjectID)
	r, data = e.do(t, "GET", base+"&kind=entity", "", e.authed(nil))
	if r.StatusCode != 200 || data["defaultLocale"] != "kk" || data["locales"].([]any)[0] != "kk" || data["private"] != nil {
		t.Fatal(r.StatusCode, data)
	}
	workspaceExec(t, e, `UPDATE roles SET capabilities=ARRAY['design.read']`)
	r, data = e.do(t, "GET", base+"&kind=entity", "", e.authed(nil))
	expectProblem(t, r, data, 403, "FORBIDDEN")
}
func TestStudioContentAtomicHistory(t *testing.T) {
	// STU-101/102/104: localized RichText and metadata are one real group, exact undo/redo and atomic rejection.
	e := setup(t)
	f := studioContentFixture(t, e)
	id := f.ContentFixture("entity", "Article", map[string]any{"title": map[string]any{"ru": "Before", "en": "Keep"}}, "staging")
	asset := f.ContentFixture("asset", "", map[string]any{"assetKind": "image", "fileHash": "original", "title": "Picture"}, "staging")
	cs := e.command(t, "create-changeset", `{"title":"content","targets":["staging"]}`)["id"].(string)
	body := `{"changesetId":"` + cs + `","expectedSeq":0,"group":true,"operations":[{"type":"entity.setFields","target":"` + id.String() + `","payload":{"set":{"title.ru":"After","body":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Rich","marks":[{"type":"bold"}]}]}]}}}},{"type":"asset.updateMeta","target":"` + asset.String() + `","payload":{"set":{"alt.ru":"Изображение"}}}]}`
	e.command(t, "apply-operations", body)
	path := func(id uuid.UUID) string {
		return "/api/v1/content/" + id.String() + "?environment=staging&changesetId=" + cs
	}
	r, data := e.do(t, "GET", path(id), "", e.authed(nil))
	title := data["data"].(map[string]any)["title"].(map[string]any)
	if r.StatusCode != 200 || title["ru"] != "After" || title["en"] != "Keep" {
		t.Fatal(r.StatusCode, data)
	}
	_, data = e.do(t, "GET", path(asset), "", e.authed(nil))
	if data["data"].(map[string]any)["fileHash"] != "original" {
		t.Fatal(data)
	}
	e.command(t, "undo", `{"changesetId":"`+cs+`","expectedSeq":2}`)
	_, data = e.do(t, "GET", path(id), "", e.authed(nil))
	if data["data"].(map[string]any)["title"].(map[string]any)["ru"] != "Before" {
		t.Fatal(data)
	}
	e.command(t, "redo", `{"changesetId":"`+cs+`","expectedSeq":4}`)
	_, data = e.do(t, "GET", path(id), "", e.authed(nil))
	if data["data"].(map[string]any)["title"].(map[string]any)["ru"] != "After" {
		t.Fatal(data)
	}
	r, data = e.do(t, "POST", "/api/v1/commands/apply-operations", `{"payload":{"changesetId":"`+cs+`","expectedSeq":6,"group":true,"operations":[{"type":"entity.setFields","target":"`+id.String()+`","payload":{"set":{"title.ru":"rollback"}}},{"type":"asset.updateMeta","target":"`+asset.String()+`","payload":{"set":{"fileHash":"forged"}}}]}}`, e.authed(map[string]string{"Idempotency-Key": "content-invalid"}))
	if r.StatusCode < 400 {
		t.Fatal(data)
	}
	_, data = e.do(t, "GET", path(id), "", e.authed(nil))
	if data["data"].(map[string]any)["title"].(map[string]any)["ru"] != "After" {
		t.Fatal(data)
	}
	var seq int
	if err := e.pool.QueryRow(context.Background(), "SELECT seq FROM changesets WHERE id=$1", cs).Scan(&seq); err != nil || seq != 6 {
		t.Fatal(seq, err)
	}
}

func TestStudioContentVersionPrecondition(t *testing.T) {
	// STU-104: publishing in another CS cannot overwrite a form read at an older head.
	e := setup(t)
	f := studioContentFixture(t, e)
	id := f.ContentFixture("entity", "Article", map[string]any{"title": map[string]any{"ru": "Before"}}, "staging")
	cs := e.command(t, "create-changeset", `{"title":"content","targets":["staging"]}`)["id"].(string)
	path := "/api/v1/content/" + id.String() + "?environment=staging&changesetId=" + cs
	_, original := e.do(t, "GET", path, "", e.authed(nil))
	oldVersion := original["versionId"].(string)
	newVersion := uuid.New()
	publishedBody := []byte(`{"title":{"ru":"Published"}}`)
	publishedHash := sha256.Sum256(publishedBody)
	f.Exec(`INSERT INTO object_versions(id,project_id,object_id,number,state,schema_version,body,body_hash,created_by)
		SELECT $1,project_id,object_id,2,'committed',schema_version,$3,$4,created_by FROM object_versions WHERE id=$2`, newVersion, oldVersion, publishedBody, publishedHash[:])
	f.Exec(`UPDATE objects SET head_version_id=$1 WHERE id=$2`, newVersion, id)
	command := func(seq int, operations string, key string) (int, map[string]any) {
		body := fmt.Sprintf(`{"payload":{"changesetId":"%s","expectedSeq":%d,"group":true,"operations":[%s]}}`, cs, seq, operations)
		r, data := e.do(t, "POST", "/api/v1/commands/apply-operations", body, e.authed(map[string]string{"Idempotency-Key": key}))
		return r.StatusCode, data
	}
	op := func(version, title string) string {
		return fmt.Sprintf(`{"type":"entity.setFields","target":"%s","expectedVersionId":"%s","payload":{"set":{"title.ru":"%s"}}}`, id, version, title)
	}
	status, data := command(0, op(newVersion.String(), "Partial")+","+op(oldVersion, "Stale"), "stale-head")
	if status != 409 || data["code"] != "CONTENT_VERSION_CONFLICT" {
		t.Fatal(status, data)
	}
	_, data = e.do(t, "GET", path, "", e.authed(nil))
	if data["versionId"] != newVersion.String() || data["data"].(map[string]any)["title"].(map[string]any)["ru"] != "Published" {
		t.Fatal(data)
	}
	var seq, working int
	if err := e.pool.QueryRow(context.Background(), `SELECT seq,(SELECT count(*) FROM changeset_objects WHERE changeset_id=$1) FROM changesets WHERE id=$1`, cs).Scan(&seq, &working); err != nil || seq != 0 || working != 0 {
		t.Fatal(seq, working, err)
	}
	// Two operations compare the same initial source, not a newly allocated working ID.
	status, data = command(0, op(newVersion.String(), "One")+","+op(newVersion.String(), "Two"), "fresh-head")
	if status != 200 {
		t.Fatal(status, data)
	}
	_, data = e.do(t, "GET", path, "", e.authed(nil))
	workingVersion := data["versionId"].(string)
	status, data = command(2, op(workingVersion, "Three"), "working-version")
	if status != 200 {
		t.Fatal(status, data)
	}
	e.command(t, "undo", `{"changesetId":"`+cs+`","expectedSeq":3}`)
	_, data = e.do(t, "GET", path, "", e.authed(nil))
	if data["data"].(map[string]any)["title"].(map[string]any)["ru"] != "Two" {
		t.Fatal(data)
	}
	for i, operation := range []string{
		fmt.Sprintf(`{"type":"entity.create","expectedVersionId":"%s","payload":{"schema":"Article","environment":"staging","data":{}}}`, workingVersion),
		fmt.Sprintf(`{"type":"entity.setFields","target":"%s","expectedVersionId":"%s","payload":{"set":{"title.ru":"Invalid"}}}`, id, uuid.Nil),
	} {
		status, data = command(4, operation, fmt.Sprintf("invalid-precondition-%d", i))
		if status < 400 {
			t.Fatal(status, data)
		}
	}
}
