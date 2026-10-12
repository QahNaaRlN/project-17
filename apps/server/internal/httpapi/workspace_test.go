package httpapi_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// Fixtures model imported committed pages and working drafts; all user writes use Command API.
func workspaceExec(t *testing.T, e env, sql string, args ...any) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}
func workspaceActor(t *testing.T, e env) auth.Actor {
	t.Helper()
	actor, err := auth.Authenticate(context.Background(), store.New(e.pool), "Bearer "+e.token)
	if err != nil {
		t.Fatal(err)
	}
	return actor
}
func workspacePage(t *testing.T, e env, actor auth.Actor, path, kind string) uuid.UUID {
	t.Helper()
	id, version := uuid.New(), uuid.New()
	workspaceExec(t, e, `INSERT INTO objects(id,project_id,kind,doc_kind) VALUES($1,$2,'document',$3)`, id, actor.ProjectID, kind)
	workspaceExec(t, e, `INSERT INTO object_versions(id,project_id,object_id,number,state,ir_version,path,body,body_hash,created_by) VALUES($1,$2,$3,1,'committed','1.0',$4,'{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box","name":"Home"}}}',decode(repeat('00',32),'hex'),$5)`, version, actor.ProjectID, id, path, actor.ID)
	workspaceExec(t, e, `UPDATE objects SET head_version_id=$1,head_path=$2 WHERE id=$3`, version, path, id)
	return id
}
func workspaceDraft(t *testing.T, e env, actor auth.Actor, state, kind, target string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	workspaceExec(t, e, `INSERT INTO changesets(id,project_id,kind,title,owner_id,state,targets) VALUES($1,$2,$3,'Draft',$4,$5,ARRAY[$6]::text[])`, id, actor.ProjectID, kind, actor.ID, state, target)
	return id
}
func TestStudioWorkspaceIsolation(t *testing.T) {
	// STU-040/041: filtering precedes pagination and uses the authenticated owner/project.
	e := setup(t)
	actor := workspaceActor(t, e)
	head := workspacePage(t, e, actor, "/home", "page")
	workspacePage(t, e, actor, "/component", "component")
	deleted := workspacePage(t, e, actor, "/deleted", "page")
	workspaceExec(t, e, `UPDATE objects SET deleted_at=now() WHERE id=$1`, deleted)
	own := workspaceDraft(t, e, actor, "open", "standard", "staging")
	for _, state := range []string{"checking", "failed", "in_review", "changes_requested", "approved", "merged", "abandoned"} {
		workspaceDraft(t, e, actor, state, "standard", "staging")
	}
	workspaceDraft(t, e, actor, "open", "schema", "staging")
	workspaceDraft(t, e, actor, "open", "standard", "production")
	otherOwner := actor
	otherOwner.ID = uuid.New()
	workspaceExec(t, e, `INSERT INTO actors(id,kind,project_id,display_name) VALUES($1,'service',$2,'Other')`, otherOwner.ID, actor.ProjectID)
	workspaceDraft(t, e, otherOwner, "open", "standard", "staging")
	bootstrap, err := projects.Bootstrap(context.Background(), e.pool, "other", "Other", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	otherProject, err := auth.Authenticate(context.Background(), store.New(e.pool), "Bearer "+bootstrap.Token)
	if err != nil {
		t.Fatal(err)
	}
	workspacePage(t, e, otherProject, "/foreign", "page")
	workspaceDraft(t, e, otherProject, "open", "standard", "staging")
	resp, body := e.do(t, "GET", "/api/v1/pages?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 1 || body["items"].([]any)[0].(map[string]any)["id"] != head.String() {
		t.Fatalf("pages: %d %v", resp.StatusCode, body)
	}
	item := body["items"].([]any)[0].(map[string]any)
	if item["hasHead"] != true || item["title"] != "Home" || item["body"] != nil {
		t.Fatal(item)
	}
	resp, body = e.do(t, "GET", "/api/v1/working-drafts?environment=staging&documentId="+head.String(), "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 1 || body["items"].([]any)[0].(map[string]any)["id"] != own.String() {
		t.Fatalf("drafts: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(t, "GET", "/api/v1/working-drafts?environment=staging&documentId="+deleted.String(), "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 0 {
		t.Fatal(body)
	}
}
func TestStudioWorkspaceDraftOnly(t *testing.T) {
	// STU-040/041: a page without head is visible only in its own suitable CS.
	e := setup(t)
	own := e.command(t, "create-changeset", `{"title":"Own","targets":["staging"]}`)["id"].(string)
	result := e.command(t, "apply-operations", `{"changesetId":"`+own+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","path":"/draft","root":{"id":"n_root","type":"Box","name":"Draft page"}}}]}`)
	id := result["operations"].([]any)[0].(map[string]any)["target"].(string)
	empty := e.command(t, "create-changeset", `{"title":"Empty","targets":["staging"]}`)["id"].(string)
	resp, body := e.do(t, "GET", "/api/v1/pages?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 1 || body["items"].([]any)[0].(map[string]any)["hasHead"] != false {
		t.Fatalf("%d %v", resp.StatusCode, body)
	}
	resp, body = e.do(t, "GET", "/api/v1/working-drafts?environment=staging&documentId="+id, "", e.authed(nil))
	items := body["items"].([]any)
	if resp.StatusCode != 200 || len(items) != 1 || items[0].(map[string]any)["id"] != own {
		t.Fatal(body)
	}
	if items[0].(map[string]any)["id"] == empty {
		t.Fatal("empty CS cannot read unmerged page")
	}
	actor := workspaceActor(t, e)
	other := uuid.New()
	workspaceExec(t, e, `INSERT INTO actors(id,kind,project_id,display_name) VALUES($1,'service',$2,'Other')`, other, actor.ProjectID)
	workspaceExec(t, e, `UPDATE changesets SET owner_id=$1 WHERE id=$2`, other, own)
	resp, body = e.do(t, "GET", "/api/v1/pages?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 0 {
		t.Fatal("foreign working page leaked", body)
	}
	workspaceExec(t, e, `UPDATE changesets SET owner_id=$1 WHERE id=$2`, actor.ID, own)
	workspaceExec(t, e, `UPDATE changesets SET state='abandoned' WHERE id=$1`, own)
	resp, body = e.do(t, "GET", "/api/v1/pages?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 0 {
		t.Fatal(body)
	}
}
func TestStudioWorkspacePagination(t *testing.T) {
	// STU-044: both lists expose every item once, including beyond legacy LIMIT 100.
	e := setup(t)
	actor := workspaceActor(t, e)
	var head uuid.UUID
	for i := 0; i < 103; i++ {
		head = workspacePage(t, e, actor, fmt.Sprintf("/page-%d", i), "page")
		workspaceDraft(t, e, actor, "open", "standard", "staging")
	}
	for _, path := range []string{"/api/v1/pages?environment=staging", "/api/v1/working-drafts?environment=staging&documentId=" + head.String()} {
		cursor := ""
		seen := map[string]bool{}
		pages := 0
		for {
			url := path
			if cursor != "" {
				url += "&after=" + cursor
			}
			resp, body := e.do(t, "GET", url, "", e.authed(nil))
			if resp.StatusCode != 200 {
				t.Fatal(body)
			}
			items := body["items"].([]any)
			if len(items) > 50 {
				t.Fatal("unbounded list")
			}
			pages++
			for _, raw := range items {
				id := raw.(map[string]any)["id"].(string)
				if seen[id] {
					t.Fatal("duplicate", id)
				}
				seen[id] = true
			}
			if body["nextCursor"] == nil {
				break
			}
			cursor = body["nextCursor"].(string)
			if pages > 3 {
				t.Fatal("cursor loop")
			}
		}
		if len(seen) != 103 || pages != 3 {
			t.Fatalf("%s: %d items/%d pages", path, len(seen), pages)
		}
	}
}
func TestStudioWorkspaceErrors(t *testing.T) {
	// STU-001/044: auth, permissions, invalid and duplicate parameters, storage failures.
	e := setup(t)
	for _, path := range []string{"/pages", "/working-drafts"} {
		resp, body := e.do(t, "GET", "/api/v1"+path+"?environment=staging", "", nil)
		expectProblem(t, resp, body, 401, "UNAUTHENTICATED")
	}
	for _, query := range []string{"", "?environment=missing", "?environment=schema", "?environment=staging&after=bad", "?environment=staging&after=", "?environment=staging&environment=staging", "?environment=staging&ownerId=me", "?environment=staging&documentId=x"} {
		resp, body := e.do(t, "GET", "/api/v1/pages"+query, "", e.authed(nil))
		expectProblem(t, resp, body, 400, "PARAM_INVALID")
	}
	for _, query := range []string{"?environment=staging", "?environment=staging&documentId=bad", "?environment=staging&documentId=x&documentId=y"} {
		resp, body := e.do(t, "GET", "/api/v1/working-drafts"+query, "", e.authed(nil))
		expectProblem(t, resp, body, 400, "PARAM_INVALID")
	}
	workspaceExec(t, e, `UPDATE api_tokens SET scopes=ARRAY['content.read']::text[]`)
	for _, path := range []string{"/pages", "/working-drafts"} {
		resp, body := e.do(t, "GET", "/api/v1"+path+"?environment=staging", "", e.authed(nil))
		expectProblem(t, resp, body, 403, "FORBIDDEN")
	}
}
func TestStudioWorkspaceStorageErrors(t *testing.T) {
	e := setup(t)
	workspaceExec(t, e, `ALTER TABLE objects RENAME TO objects_broken`)
	for _, path := range []string{"/pages?environment=staging", "/working-drafts?environment=staging&documentId=" + uuid.NewString()} {
		resp, body := e.do(t, "GET", "/api/v1"+path, "", e.authed(nil))
		expectProblem(t, resp, body, 500, "INTERNAL")
	}
	workspaceExec(t, e, `ALTER TABLE environments RENAME TO environments_broken`)
	resp, body := e.do(t, "GET", "/api/v1/pages?environment=staging", "", e.authed(nil))
	expectProblem(t, resp, body, 500, "INTERNAL")
}

func TestStudioDraftCreationIdempotency(t *testing.T) {
	// STU-042/043: command retries create one owned CS, without changing a page/head/version.
	e := setup(t)
	actor := workspaceActor(t, e)
	head := workspacePage(t, e, actor, "/home", "page")
	body := `{"payload":{"title":"New workspace draft","targets":["staging"]},"reason":"Studio: create page draft"}`
	var id string
	for i := 0; i < 2; i++ {
		resp, out := e.do(t, "POST", "/api/v1/commands/create-changeset", body, e.authed(map[string]string{"Idempotency-Key": "same-create-key"}))
		if resp.StatusCode != 200 {
			t.Fatal(out)
		}
		result := out["result"].(map[string]any)
		if result["ownerId"] != actor.ID.String() || result["seq"] != float64(0) || result["targets"].([]any)[0] != "staging" {
			t.Fatal(result)
		}
		if i == 0 {
			id = result["id"].(string)
		} else if result["id"] != id {
			t.Fatal("created duplicate", out)
		}
	}
	var csCount, versionCount int
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM changesets`).Scan(&csCount); err != nil {
		t.Fatal(err)
	}
	if err := e.pool.QueryRow(context.Background(), `SELECT count(*) FROM object_versions`).Scan(&versionCount); err != nil {
		t.Fatal(err)
	}
	if csCount != 1 || versionCount != 1 {
		t.Fatalf("cs=%d versions=%d", csCount, versionCount)
	}
	resp, out := e.do(t, "GET", "/api/v1/documents/"+head.String()+"?changesetId="+id, "", e.authed(nil))
	if resp.StatusCode != 200 || out["path"] != "/home" || out["state"] != "committed" {
		t.Fatal(out)
	}
}
