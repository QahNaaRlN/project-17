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
		if pages == 1 {
			// An insertion above the keyset boundary must not repeat or displace older rows.
			workspaceDraft(t, e, actor, "open", "standard", "staging")
		}
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

func TestWorkQueueLimitsNeverReturnPartialCard(t *testing.T) {
	e := setup(t)
	a := workspaceActor(t, e)
	id := workspaceDraft(t, e, a, "open", "standard", "staging")
	workspaceExec(t, e, `WITH objs AS (
 INSERT INTO objects(id,project_id,kind,doc_kind) SELECT gen_random_uuid(),$1,'document','page' FROM generate_series(1,1001) RETURNING id
 ), versions AS (
 INSERT INTO object_versions(id,project_id,object_id,state,ir_version,body,body_hash,created_by)
 SELECT gen_random_uuid(),$1,id,'working','1.0','{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box"}}}',decode(repeat('00',32),'hex'),$2 FROM objs RETURNING id,object_id
 ) INSERT INTO changeset_objects(changeset_id,object_id,working_version_id) SELECT $3,object_id,id FROM versions`, a.ProjectID, a.ID, id)
	resp, body := e.do(t, "GET", "/api/v1/work-queue?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 || body["items"].([]any)[0].(map[string]any)["objectCount"] != float64(1001) {
		t.Fatal(body)
	}
	for _, path := range []string{"/work-queue/" + id.String() + "?environment=staging", "/changesets/" + id.String(), "/changesets/" + id.String() + "/review", "/changesets/" + id.String() + "/history"} {
		resp, b := e.do(t, "GET", "/api/v1"+path, "", e.authed(nil))
		expectProblem(t, resp, b, 413, "WORK_QUEUE_LIMIT_EXCEEDED")
	}
	// Imported metadata cannot turn a successful bounded response into silent truncation.
	workspaceExec(t, e, "UPDATE actors SET display_name=repeat('x',4194305) WHERE id=$1", a.ID)
	resp, body = e.do(t, "GET", "/api/v1/work-queue?environment=staging&limit=1", "", e.authed(nil))
	expectProblem(t, resp, body, 413, "WORK_QUEUE_LIMIT_EXCEEDED")
}

func TestWorkQueueImportedKindsAndDeletedRoute(t *testing.T) {
	e := setup(t)
	a := workspaceActor(t, e)
	id := workspaceDraft(t, e, a, "open", "standard", "staging")
	page := workspacePage(t, e, a, "/previous-route", "page")
	workspaceExec(t, e, `WITH working AS (
 INSERT INTO object_versions(id,project_id,object_id,state,ir_version,path,body,body_hash,created_by,deleted)
 SELECT gen_random_uuid(),project_id,object_id,'working',ir_version,NULL,body,body_hash,$1,true FROM object_versions WHERE object_id=$2 RETURNING id
 ) INSERT INTO changeset_objects(changeset_id,object_id,base_version_id,working_version_id) SELECT $3,o.id,o.head_version_id,w.id FROM objects o,working w WHERE o.id=$2`, a.ID, page, id)
	for _, kind := range []string{"component", "entity", "asset"} {
		cs := workspaceDraft(t, e, a, "open", "standard", "staging")
		objectKind := kind
		var docKind, schema any
		if kind == "component" {
			objectKind = "document"
			docKind = "component"
		}
		if kind == "entity" {
			schema = "Article"
		}
		object, version := uuid.New(), uuid.New()
		workspaceExec(t, e, `INSERT INTO objects(id,project_id,kind,doc_kind,schema_name) VALUES($1,$2,$3,$4,$5)`, object, a.ProjectID, objectKind, docKind, schema)
		workspaceExec(t, e, `INSERT INTO object_versions(id,project_id,object_id,state,body,body_hash,created_by) VALUES($1,$2,$3,'working','{"privateStorageKey":"must-not-leak"}',decode(repeat('00',32),'hex'),$4)`, version, a.ProjectID, object, a.ID)
		workspaceExec(t, e, `INSERT INTO changeset_objects(changeset_id,object_id,working_version_id) VALUES($1,$2,$3)`, cs, object, version)
		resp, b := e.do(t, "GET", "/api/v1/work-queue?environment=staging&kind="+kind, "", e.authed(nil))
		if resp.StatusCode != 200 || len(b["items"].([]any)) != 1 {
			t.Fatal(b)
		}
		resp, b = e.do(t, "GET", "/api/v1/work-queue/"+cs.String()+"?environment=staging", "", e.authed(nil))
		if resp.StatusCode != 200 || len(b["objects"].([]any)) != 1 || strings.Contains(fmt.Sprint(b), "must-not-leak") {
			t.Fatal(b)
		}
	}
	resp, b := e.do(t, "GET", "/api/v1/work-queue?environment=staging&q=previous-route", "", e.authed(nil))
	if resp.StatusCode != 200 || len(b["items"].([]any)) != 1 || b["items"].([]any)[0].(map[string]any)["deletedCount"] != float64(1) {
		t.Fatal(b)
	}
	resp, b = e.do(t, "GET", "/api/v1/work-queue/"+id.String()+"?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 || b["objects"].([]any)[0].(map[string]any)["basePath"] != "/previous-route" {
		t.Fatal(b)
	}
	workspaceExec(t, e, `UPDATE api_tokens SET scopes=ARRAY['design.read']::text[]`)
	resp, b = e.do(t, "GET", "/api/v1/work-queue?environment=staging&kind=entity", "", e.authed(nil))
	if resp.StatusCode != 200 || len(b["items"].([]any)) != 0 {
		t.Fatal(b)
	}
	workspaceExec(t, e, `UPDATE api_tokens SET scopes=ARRAY['schema.read']::text[]`)
	resp, b = e.do(t, "GET", "/api/v1/work-queue?environment=staging", "", e.authed(nil))
	expectProblem(t, resp, b, 403, "FORBIDDEN")
}

func TestWorkQueueStorageFailuresAreNotEmptyResults(t *testing.T) {
	for _, column := range []string{"environment", "summary", "objects", "rights", "changeset"} {
		t.Run(column, func(t *testing.T) {
			e := setup(t)
			a := workspaceActor(t, e)
			id := workspaceDraft(t, e, a, "open", "standard", "staging")
			switch column {
			case "environment":
				workspaceExec(t, e, `ALTER TABLE environments RENAME TO environments_unavailable`)
			case "summary":
				workspaceExec(t, e, `ALTER TABLE object_versions RENAME TO versions_unavailable`)
			case "objects":
				workspaceExec(t, e, `ALTER TABLE object_versions DROP COLUMN body`)
			case "rights":
				// Authentication succeeds, but the subsequent read-only rights refresh fails.
				// The queue must never fall back to the initially authenticated capabilities.
				workspaceExec(t, e, `CREATE FUNCTION queue_capabilities(text[]) RETURNS text[] LANGUAGE plpgsql AS $$
    BEGIN IF current_setting('transaction_read_only')='on' THEN RAISE EXCEPTION 'rights unavailable'; END IF; RETURN $1; END $$;
    ALTER TABLE roles RENAME TO queue_roles;
    CREATE VIEW roles AS SELECT id,project_id,name,queue_capabilities(capabilities) AS capabilities FROM queue_roles;`)
			case "changeset":
				workspaceExec(t, e, `ALTER TABLE changesets RENAME TO changesets_unavailable`)
			}
			path := "/api/v1/work-queue?environment=staging"
			if column == "objects" {
				path = "/api/v1/work-queue/" + id.String() + "?environment=staging"
			}
			resp, b := e.do(t, "GET", path, "", e.authed(nil))
			if resp.StatusCode != 500 || b["items"] != nil || b["objects"] != nil {
				t.Fatalf("%d %v", resp.StatusCode, b)
			}
			if column == "objects" || column == "rights" || column == "changeset" {
				resp, b = e.do(t, "GET", "/api/v1/changesets/"+id.String()+"/review", "", e.authed(nil))
				if resp.StatusCode != 500 {
					t.Fatalf("review must fail closed: %d %v", resp.StatusCode, b)
				}
			}
		})
	}
}
