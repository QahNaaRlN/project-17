package httpapi_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestPrimaryETagSTU210211215(t *testing.T) {
	e := setup(t)
	f := studioContentFixture(t, e)
	f.Exec(`UPDATE projects SET settings='{"locales":["ru","en"],"defaultLocale":"ru"}'`)
	source := f.ContentFixture("entity", "Article", map[string]any{"title": map[string]any{"ru": "Before", "en": "English"}}, "staging")
	e.command(t, "set-approval-policy", `{"low":0,"medium":0,"high":0}`)
	cs := e.command(t, "create-changeset", `{"title":"primary","targets":["staging"]}`)["id"].(string)
	res := e.command(t, "apply-operations", fmt.Sprintf(`{"changesetId":%q,"expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/primary","root":{"id":"n_root","type":"Heading","bindings":{"text":"$content.title"}}}}]}`, cs))
	pageID := res["operations"].([]any)[0].(map[string]any)["target"].(string)
	e.command(t, "apply-operations", fmt.Sprintf(`{"changesetId":%q,"expectedSeq":1,"operations":[{"type":"document.setContent","target":%q,"payload":{"content":{"schema":"Article","resolve":{"by":"entity","entityId":%q}}}}]}`, cs, pageID, source.String()))
	e.command(t, "submit-changeset", fmt.Sprintf(`{"changesetId":%q,"expectedSeq":2}`, cs))
	e.command(t, "publish", fmt.Sprintf(`{"changesetId":%q,"environment":"staging"}`, cs))
	key := e.command(t, "create-delivery-key", `{"environment":"staging","name":"primary"}`)["key"].(string)
	path := "/delivery/v1/store/staging/page?path=/primary&locale=ru"
	resp, before := e.do(t, "GET", path, "", bearer(key))
	if resp.StatusCode != 200 {
		t.Fatal(before)
	}
	tag := resp.Header.Get("ETag")
	pageVersion := before["page"].(map[string]any)["versionId"]
	h := bearer(key)
	h["If-None-Match"] = tag
	if response, _ := e.do(t, "GET", path, "", h); response.StatusCode != 304 {
		t.Fatal(response.StatusCode)
	}
	edit := e.command(t, "create-changeset", `{"title":"entity only","targets":["staging"]}`)["id"].(string)
	e.command(t, "apply-operations", fmt.Sprintf(`{"changesetId":%q,"expectedSeq":0,"operations":[{"type":"entity.setFields","target":%q,"payload":{"set":{"title.ru":"After"}}}]}`, edit, source.String()))
	response, still := e.do(t, "GET", path, "", h)
	if response.StatusCode != 304 {
		t.Fatal("draft leaked", still)
	}
	e.command(t, "submit-changeset", fmt.Sprintf(`{"changesetId":%q,"expectedSeq":1}`, edit))
	e.command(t, "publish", fmt.Sprintf(`{"changesetId":%q,"environment":"staging"}`, edit))
	response, after := e.do(t, "GET", path, "", h)
	if response.StatusCode != 200 || response.Header.Get("ETag") == tag || after["page"].(map[string]any)["versionId"] != pageVersion || after["data"].(map[string]any)["content"].(map[string]any)["title"] != "After" {
		t.Fatal(response.StatusCode, after)
	}
	response, _ = e.do(t, "GET", path[:len(path)-2]+"en", "", h)
	if response.StatusCode != 200 {
		t.Fatal("locale ETag reused")
	}
	// Query-side title search uses the same CS/head base with pagination.
	_, items := e.do(t, "GET", "/api/v1/content-catalog?environment=staging&changesetId="+edit+"&kind=entity&q=After", "", e.authed(nil))
	if len(items["items"].([]any)) != 1 {
		t.Fatal(items)
	}
	response, invalid := e.do(t, "GET", "/api/v1/content-catalog?environment=staging&kind=entity&q="+strings.Repeat("x", 201), "", e.authed(nil))
	if response.StatusCode != 400 || invalid["code"] != "PARAM_INVALID" {
		t.Fatal(response.StatusCode, invalid)
	}
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM operations WHERE changeset_id=$1`, edit).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
