package httpapi_test

import (
	"fmt"
	"github.com/google/uuid"
	"net/url"
	"strings"
	"testing"
)

func TestWorkQueueSearchAndPagination(t *testing.T) {
	e := setup(t)
	actor := workspaceActor(t, e)
	// Empty work is intentionally visible, and searching must precede LIMIT.
	for i := 0; i < 105; i++ {
		workspaceDraft(t, e, actor, "open", "standard", "staging")
	}
	oldest := workspaceDraft(t, e, actor, "open", "standard", "staging")
	workspaceExec(t, e, "UPDATE changesets SET title='Точная_% работа',created_at=now()-interval '1 day' WHERE id=$1", oldest)
	for _, query := range []string{"точная", "_%", "  ТОЧНАЯ  "} {
		resp, body := e.do(t, "GET", "/api/v1/work-queue?environment=staging&q="+url.QueryEscape(query), "", e.authed(nil))
		if resp.StatusCode != 200 {
			t.Fatal(body)
		}
		items := body["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"] != oldest.String() || items[0].(map[string]any)["objectCount"] != float64(0) {
			t.Fatal(body)
		}
	}
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		path := "/api/v1/work-queue?environment=staging&limit=50"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		resp, body := e.do(t, "GET", path, "", e.authed(nil))
		if resp.StatusCode != 200 {
			t.Fatal(body)
		}
		for _, raw := range body["items"].([]any) {
			id := raw.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatal("duplicate", id)
			}
			seen[id] = true
		}
		pages++
		if pages > 4 {
			t.Fatal("cursor loop")
		}
		if body["nextCursor"] == nil {
			break
		}
		cursor = body["nextCursor"].(string)
		for _, suffix := range []string{"&q=changed", "&kind=entity", "&ownership=mine", "&state=open", "&limit=10"} {
			// Use a context with exactly one limit.
			changed := "/api/v1/work-queue?environment=staging&cursor=" + url.QueryEscape(cursor) + suffix
			resp, b := e.do(t, "GET", changed, "", e.authed(nil))
			expectProblem(t, resp, b, 400, "WORK_QUEUE_CURSOR_INVALID")
		}
	}
	if len(seen) != 106 || pages != 3 {
		t.Fatalf("%d/%d", len(seen), pages)
	}
	resp, body := e.do(t, "GET", "/api/v1/work-queue/"+oldest.String()+"?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["objects"].([]any)) != 0 {
		t.Fatal(body)
	}
}

func TestWorkQueueKindsPathsAndRights(t *testing.T) {
	e := setup(t)
	cs := e.command(t, "create-changeset", `{"title":"Mixed","targets":["staging"]}`)["id"].(string)
	for i, path := range []string{"/first", "/second_~"} {
		e.command(t, "apply-operations", fmt.Sprintf(`{"changesetId":%q,"expectedSeq":%d,"operations":[{"type":"document.create","payload":{"kind":"page","path":%q,"root":{"id":"n_root","type":"Box"}}}]}`, cs, i, path))
	}
	for _, query := range []string{"kind=page", "q=second_~", "state=open&ownership=mine"} {
		resp, body := e.do(t, "GET", "/api/v1/work-queue?environment=staging&"+query, "", e.authed(nil))
		if resp.StatusCode != 200 {
			t.Fatal(body)
		}
		items := body["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["objectCount"] != float64(2) {
			t.Fatal(body)
		}
	}
	resp, body := e.do(t, "GET", "/api/v1/work-queue/"+cs+"?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["objects"].([]any)) != 2 {
		t.Fatal(body)
	}
	// A content-only token cannot infer a document CS from a matching path or ID.
	workspaceExec(t, e, `UPDATE api_tokens SET scopes=ARRAY['content.read']::text[]`)
	for _, query := range []string{"", "&kind=page", "&q=second"} {
		resp, b := e.do(t, "GET", "/api/v1/work-queue?environment=staging"+query, "", e.authed(nil))
		if resp.StatusCode != 200 || len(b["items"].([]any)) != 0 {
			t.Fatal(b)
		}
	}
	for _, path := range []string{"/work-queue/" + cs + "?environment=staging", "/changesets/" + cs + "/review", "/changesets/" + cs + "/history"} {
		resp, b := e.do(t, "GET", "/api/v1"+path, "", e.authed(nil))
		if resp.StatusCode != 404 {
			t.Fatalf("%s %d %v", path, resp.StatusCode, b)
		}
	}
	resp, body = e.do(t, "GET", "/api/v1/manifest?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 {
		t.Fatal(body)
	}
}

func TestWorkQueueInvalidArgumentsAndHiddenDraft(t *testing.T) {
	e := setup(t)
	a := workspaceActor(t, e)
	foreign := a
	foreign.ID = uuid.New()
	workspaceExec(t, e, `INSERT INTO actors(id,kind,project_id,display_name) VALUES($1,'service',$2,'Other')`, foreign.ID, a.ProjectID)
	id := workspaceDraft(t, e, foreign, "open", "standard", "staging")
	for _, path := range []string{"/work-queue/" + id.String() + "?environment=staging", "/changesets/" + id.String() + "/review", "/changesets/" + id.String() + "/history"} {
		resp, b := e.do(t, "GET", "/api/v1"+path, "", e.authed(nil))
		if resp.StatusCode != 404 {
			t.Fatalf("%s %d %v", path, resp.StatusCode, b)
		}
	}
	for _, query := range []string{"", "environment=staging&state=merged", "environment=staging&ownership=bad", "environment=staging&kind=document", "environment=staging&limit=0", "environment=staging&limit=101", "environment=staging&q=x&q=y", "environment=staging&ownerId=x", "environment=staging&q=%00", "environment=staging&q=%FF", "environment=staging&q=" + strings.Repeat("a", 201)} {
		resp, b := e.do(t, "GET", "/api/v1/work-queue?"+query, "", e.authed(nil))
		expectProblem(t, resp, b, 400, "PARAM_INVALID")
	}
	for _, query := range []string{"cursor=", "cursor=bad", "cursor=" + strings.Repeat("a", 2049)} {
		resp, b := e.do(t, "GET", "/api/v1/work-queue?environment=staging&"+query, "", e.authed(nil))
		expectProblem(t, resp, b, 400, "WORK_QUEUE_CURSOR_INVALID")
	}
	resp, b := e.do(t, "GET", "/api/v1/work-queue?environment=missing", "", e.authed(nil))
	expectProblem(t, resp, b, 404, "NOT_FOUND")
}
