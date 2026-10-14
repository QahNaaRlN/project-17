package httpapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestStudioDiffSnapshotAndIdempotentPublish(t *testing.T) {
	e := setup(t)
	e.command(t, "set-approval-policy", `{"low":0,"medium":0,"high":0}`)
	cs := e.command(t, "create-changeset", `{"title":"Compare","targets":["staging"]}`)["id"].(string)
	result := e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/diff","root":{"id":"n_root","type":"Box","name":"Before"}}}]}`)
	doc := result["operations"].([]any)[0].(map[string]any)["target"].(string)
	route := "/api/v1/changesets/" + cs + "/diff?environment=staging"
	r, index := e.do(t, "GET", route, "", e.authed(nil))
	if r.StatusCode != 200 || index["complete"] != true || len(index["objects"].([]any)) != 1 {
		t.Fatal(index)
	}
	snapshot := index["snapshot"].(string)
	details := fmt.Sprintf("/api/v1/changesets/%s/diff/%s?environment=staging&baseline=base&snapshot=%s&limit=1", cs, doc, snapshot)
	r, page := e.do(t, "GET", details, "", e.authed(nil))
	if r.StatusCode != 200 || len(page["items"].([]any)) != 1 || page["nextCursor"] == nil {
		t.Fatal(page)
	}
	for cursor := page["nextCursor"]; cursor != nil; cursor = page["nextCursor"] {
		r, page = e.do(t, "GET", details+"&after="+cursor.(string), "", e.authed(nil))
		if r.StatusCode != 200 {
			t.Fatal(page)
		}
	}
	for _, bad := range []string{route + "&x=1", strings.Split(route, "?")[0], strings.Replace(details, "limit=1", "limit=bad", 1), strings.Replace(details, doc, "bad", 1), details + "&after=" + strings.Repeat("x", 2049), route + "&environment=staging", "/api/v1/changesets/bad/diff?environment=staging", details + "&after=bad", strings.Replace(details, "baseline=base", "baseline=wrong", 1)} {
		r, p := e.do(t, "GET", bad, "", e.authed(nil))
		if r.StatusCode < 400 {
			t.Fatal(bad, p)
		}
	}
	r, p := e.do(t, "GET", strings.Replace(details, doc, uuid.NewString(), 1), "", e.authed(nil))
	expectProblem(t, r, p, 404, "NOT_FOUND")
	e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":1,"operations":[{"type":"node.rename","target":"`+doc+`","payload":{"nodeId":"n_root","name":"After"}}]}`)
	r, p = e.do(t, "GET", details, "", e.authed(nil))
	expectProblem(t, r, p, 409, "DIFF_SNAPSHOT_STALE")
	e.command(t, "submit-changeset", `{"changesetId":"`+cs+`","expectedSeq":2}`)
	payload := func(s string) string {
		return `{"payload":{"changesetId":"` + cs + `","environment":"staging","expectedSeq":2,"expectedDiffSnapshot":"` + s + `"}}`
	}
	r, p = e.do(t, "POST", "/api/v1/commands/publish", payload(snapshot), e.authed(map[string]string{"Idempotency-Key": "stale-diff"}))
	expectProblem(t, r, p, 409, "DIFF_SNAPSHOT_STALE")
	_, index = e.do(t, "GET", route, "", e.authed(nil))
	snapshot = index["snapshot"].(string)
	var publication any
	for i := 0; i < 2; i++ {
		r, p = e.do(t, "POST", "/api/v1/commands/publish", payload(snapshot), e.authed(map[string]string{"Idempotency-Key": "same-diff-publish"}))
		if r.StatusCode != 200 {
			t.Fatal(p)
		}
		id := p["result"].(map[string]any)["id"]
		if i == 0 {
			publication = id
		} else if id != publication {
			t.Fatal(p)
		}
	}
	r, p = e.do(t, "GET", route, "", e.authed(nil))
	expectProblem(t, r, p, 409, "DIFF_STATE_UNSUPPORTED")
	raw, _ := json.Marshal(p)
	if strings.Contains(string(raw), "Before") {
		t.Fatal(string(raw))
	}
}
func TestStudioDiffStorageFaults(t *testing.T) {
	for _, table := range []string{"changesets", "changeset_objects", "object_versions", "published_pointers", "environments", "projects"} {
		t.Run(table, func(t *testing.T) {
			e := setup(t)
			cs := e.command(t, "create-changeset", `{"title":"Compare","targets":["staging"]}`)["id"].(string)
			e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/diff","root":{"id":"n_root","type":"Box"}}}]}`)
			if _, err := e.pool.Exec(context.Background(), `ALTER TABLE `+table+` RENAME TO `+table+`_broken`); err != nil {
				t.Fatal(err)
			}
			r, p := e.do(t, "GET", "/api/v1/changesets/"+cs+"/diff?environment=staging", "", e.authed(nil))
			if r.StatusCode < 400 || p["objects"] != nil {
				t.Fatal(p)
			}
		})
	}
}
