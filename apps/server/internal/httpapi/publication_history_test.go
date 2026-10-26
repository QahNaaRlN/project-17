package httpapi_test

import (
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestPublicationHistoryQueriesAndGuardedRollback(t *testing.T) {
	e := setup(t)
	e.command(t, "set-approval-policy", `{"low":0,"medium":0,"high":0}`)
	cs := e.command(t, "create-changeset", `{"title":"History","targets":["staging"]}`)["id"].(string)
	result := e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/history","root":{"id":"n_root","type":"Box","name":"History"}}}]}`)
	doc := result["operations"].([]any)[0].(map[string]any)["target"].(string)
	e.command(t, "submit-changeset", `{"changesetId":"`+cs+`","expectedSeq":1}`)
	p := e.command(t, "publish", `{"changesetId":"`+cs+`","environment":"staging"}`)["id"].(string)
	root := "/api/v1/publication-history"
	r, v := e.do(t, "GET", root+"?environment=staging&limit=1", "", e.authed(nil))
	if r.StatusCode != 200 || len(v["items"].([]any)) != 1 {
		t.Fatal(v)
	}
	r, v = e.do(t, "GET", root+"/"+p+"?environment=staging", "", e.authed(nil))
	if r.StatusCode != 200 || v["complete"] != true {
		t.Fatal(v)
	}
	r, v = e.do(t, "GET", root+"/"+p+"/rollback-preview?environment=staging", "", e.authed(nil))
	if r.StatusCode != 200 || v["canRollback"] != true {
		t.Fatal(v)
	}
	snapshot := v["snapshot"].(string)
	detail := root + "/" + p + "/rollback-preview/diff?objectId=" + doc + "&snapshot=" + snapshot + "&limit=1"
	r, v = e.do(t, "GET", detail, "", e.authed(nil))
	if r.StatusCode != 200 {
		t.Fatal(v)
	}
	historical := root + "/" + p + "/diff?objectId=" + doc + "&limit=100"
	r, v = e.do(t, "GET", historical, "", e.authed(nil))
	if r.StatusCode != 200 {
		t.Fatal(v)
	}
	for _, bad := range []string{root, root + "?environment=staging&x=1", root + "?environment=staging&limit=bad", root + "?environment=staging&limit=101", root + "?environment=staging&cursor=" + strings.Repeat("x", 2049), root + "/bad", root + "/" + p + "?environment=production", root + "/" + uuid.NewString(), detail + "&objectId=" + doc, strings.Replace(detail, doc, "bad", 1), strings.Replace(detail, "limit=1", "limit=bad", 1)} {
		r, v = e.do(t, "GET", bad, "", e.authed(nil))
		if r.StatusCode < 400 {
			t.Fatal(bad, v)
		}
	}
	body := `{"reason":"Cancel publication after review","payload":{"publicationId":"` + p + `","resetHead":false,"expectedRollbackSnapshot":"` + snapshot + `"}}`
	for i := 0; i < 2; i++ {
		r, v = e.do(t, "POST", "/api/v1/commands/rollback", body, e.authed(map[string]string{"Idempotency-Key": "history-rollback"}))
		if r.StatusCode != 200 {
			t.Fatal(v)
		}
	}
	r, v = e.do(t, "GET", detail, "", e.authed(nil))
	expectProblem(t, r, v, 409, "ROLLBACK_SNAPSHOT_STALE")
	r, v = e.do(t, "GET", historical, "", e.authed(nil))
	if r.StatusCode != 200 {
		t.Fatal(v)
	}
}

// STU-462, 463, 466: promote preview, its diff and the guarded command over HTTP.
func TestPublicationHistoryPromotePreview(t *testing.T) {
	e := setup(t)
	e.command(t, "set-approval-policy", `{"low":0,"medium":0,"high":0}`)
	cs := e.command(t, "create-changeset", `{"title":"Promote","targets":["staging"]}`)["id"].(string)
	result := e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/promote","root":{"id":"n_root","type":"Box","name":"Promote"}}}]}`)
	doc := result["operations"].([]any)[0].(map[string]any)["target"].(string)
	e.command(t, "submit-changeset", `{"changesetId":"`+cs+`","expectedSeq":1}`)
	p := e.command(t, "publish", `{"changesetId":"`+cs+`","environment":"staging"}`)["id"].(string)
	preview := "/api/v1/publication-history/" + p + "/promote-preview?environment=staging&toEnvironment=production"
	r, v := e.do(t, "GET", preview, "", e.authed(nil))
	if r.StatusCode != 200 || v["canPromote"] != true || v["toEnvironment"] != "production" {
		t.Fatal(v)
	}
	snapshot := v["snapshot"].(string)
	detail := "/api/v1/publication-history/" + p + "/promote-preview/diff?toEnvironment=production&objectId=" + doc + "&snapshot=" + snapshot + "&limit=1"
	r, v = e.do(t, "GET", detail, "", e.authed(nil))
	if r.StatusCode != 200 {
		t.Fatal(v)
	}
	for _, bad := range []string{strings.Replace(preview, "&toEnvironment=production", "", 1), preview + "&toEnvironment=staging", strings.Replace(preview, "production", strings.Repeat("p", 101), 1), strings.Replace(preview, "production", "missing", 1), strings.Replace(detail, "toEnvironment", "environment", 1)} {
		r, v = e.do(t, "GET", bad, "", e.authed(nil))
		if r.StatusCode < 400 {
			t.Fatal(bad, v)
		}
	}
	body := `{"reason":"Release","payload":{"publicationId":"` + p + `","toEnvironment":"production","expectedPromoteSnapshot":"` + snapshot + `"}}`
	r, v = e.do(t, "POST", "/api/v1/commands/promote", body, e.authed(map[string]string{"Idempotency-Key": "history-promote"}))
	if r.StatusCode != 200 || v["result"].(map[string]any)["kind"] != "promote" {
		t.Fatal(v)
	}
	r, v = e.do(t, "GET", detail, "", e.authed(nil))
	expectProblem(t, r, v, 409, "PROMOTE_SNAPSHOT_STALE")
}
