package identity_test

import (
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"testing"
)

// STU-083: filtering precedes pagination; own failed work survives while foreign open work stays hidden.
func TestWorkQueueScopesAndPagination(t *testing.T) {
	f := setup(t)
	cookies, info := f.login(t, "alice")
	role := f.grant(t, info.User.ID, "reader", auth.DesignRead, auth.ContentRead)
	bob := f.e.Human("bob", auth.DesignRead)
	object, version := uuid.New(), uuid.New()
	f.e.Exec(`INSERT INTO objects(id,project_id,kind,doc_kind) VALUES($1,$2,'document','page')`, object, f.e.Admin.ProjectID)
	f.e.Exec(`INSERT INTO object_versions(id,project_id,object_id,number,state,ir_version,path,body,body_hash,created_by) VALUES($1,$2,$3,1,'committed','1.0','/queue','{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box"}}}',decode(repeat('00',32),'hex'),$4)`, version, f.e.Admin.ProjectID, object, info.User.ID)
	seed := func(owner uuid.UUID, state, target string) {
		cs := uuid.New()
		f.e.Exec(`INSERT INTO changesets(id,project_id,kind,title,owner_id,state,targets) VALUES($1,$2,'standard','Work',$3,$4,ARRAY[$5]::text[])`, cs, f.e.Admin.ProjectID, owner, state, target)
		f.e.Exec(`INSERT INTO changeset_objects(changeset_id,object_id,working_version_id) VALUES($1,$2,$3)`, cs, object, version)
	}
	for _, state := range []string{"open", "failed", "changes_requested", "in_review", "approved"} {
		seed(info.User.ID, state, "staging")
	}
	for _, state := range []string{"open", "failed", "changes_requested", "in_review", "approved", "merged", "abandoned"} {
		seed(bob.ID, state, "staging")
	}
	seed(info.User.ID, "open", "production")
	seed(info.User.ID, "merged", "staging")
	_, data := f.request(t, "GET", "/api/v1/work-items?environment=staging", cookies, "", "")
	if len(data["items"].([]any)) != 5 {
		t.Fatal(data)
	}
	f.e.Exec(`UPDATE roles SET capabilities=ARRAY['design.read','content.read','content.publish'] WHERE id=$1`, role)
	_, data = f.request(t, "GET", "/api/v1/work-items?environment=staging", cookies, "", "")
	if len(data["items"].([]any)) != 7 {
		t.Fatal(data)
	}
	for i := 0; i < 47; i++ {
		seed(info.User.ID, "open", "staging")
	}
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		path := "/api/v1/work-items?environment=staging"
		if cursor != "" {
			path += "&after=" + cursor
		}
		r, d := f.request(t, "GET", path, cookies, "", "")
		if r.StatusCode != 200 {
			t.Fatal(d)
		}
		items := d["items"].([]any)
		if len(items) > 50 {
			t.Fatal("unbounded queue")
		}
		pages++
		for _, raw := range items {
			item := raw.(map[string]any)
			id := item["id"].(string)
			if seen[id] {
				t.Fatal("duplicate cursor item")
			}
			seen[id] = true
			if item["body"] != nil {
				t.Fatal("body leaked")
			}
		}
		if d["nextCursor"] == nil {
			break
		}
		cursor = d["nextCursor"].(string)
	}
	if len(seen) != 54 || pages != 2 {
		t.Fatal(len(seen), pages)
	}
	for _, query := range []string{"environment=missing", "environment=staging&after=bad", "environment=staging&ownerId=" + bob.ID.String(), "environment=staging&environment=production"} {
		r, _ := f.request(t, "GET", "/api/v1/work-items?"+query, cookies, "", "")
		if r.StatusCode != 400 {
			t.Fatal(query, r.StatusCode)
		}
	}
	f.e.Exec(`ALTER TABLE changesets RENAME TO queue_unavailable`)
	r, _ := f.request(t, "GET", "/api/v1/work-items?environment=staging", cookies, "", "")
	if r.StatusCode != 500 {
		t.Fatal("database failure hidden", r.StatusCode)
	}
}
