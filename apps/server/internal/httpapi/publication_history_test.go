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
