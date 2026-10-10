package validation_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

const bindingContract = `{"manifestVersion":"1.0","app":{"id":"test","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"breakpoints":{},"tokens":{},"components":{"Native":{"props":{"title":{"type":"string"}},"events":{"click":{}}}},"dataSources":{"test.load":{"params":{},"result":{"type":"object","fields":{"title":{"type":"text"}}}}},"actions":{"test.run":{"args":{"enabled":{"type":"boolean","required":true}}}}}`

func bindingDoc(e *cmstest.Env, cs uuid.UUID, seq int) uuid.UUID {
	id := create(e, cs, seq, "page", map[string]any{"type": "Native", "bindings": map[string]any{"title": "$data.info.title"}})
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{dataSources}','{"info":{"source":"test.load","params":{}}}') WHERE changeset_id=$1 AND object_id=$2`, cs, id)
	return id
}
func expectDiagnostic(t *testing.T, err error, code string) {
	t.Helper()
	raw, _ := json.Marshal(err)
	if cmstest.Code(err) != "VALIDATION_FAILED" || !strings.Contains(string(raw), code) {
		t.Fatalf("expected %s: %s", code, raw)
	}
}

// IR-042/052/053: incomplete edits and undo return warnings; submission blocks them.
func TestDraftBindingWarningsAndUndo(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	e.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"CODE","maxMode":"CODE"}' WHERE id=$1`, e.Admin.ProjectID)
	var result changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "root": map[string]any{"id": "n_root", "type": "Button", "bindings": map[string]any{"label": "$item.title"}, "on": map[string]any{"click": map[string]any{"action": "pending:0192f1c7-4b1e-7c2b-9d10-3b5f2a9e4c11"}}}}}}}, &result)
	id := result.Operations[0].Target
	for _, env := range []string{"staging", "production"} {
		ds := result.Warnings[env][id.String()]
		if !has(ir.Result{Diagnostics: ds}, "BINDING_SCOPE_UNAVAILABLE") || !has(ir.Result{Diagnostics: ds}, "ACTION_PENDING_CAPABILITY") {
			t.Fatal(result)
		}
		for _, d := range ds {
			if d.Severity != ir.SeverityWarning {
				t.Fatal(d)
			}
		}
	}
	e.Exec(`UPDATE changesets SET targets=ARRAY['staging'] WHERE id=$1`, cs)
	result = changes.ApplyResult{}
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{map[string]any{"type": "node.setBinding", "target": id, "payload": map[string]any{"nodeId": "n_root", "prop": "label", "binding": "$context.locale"}}, map[string]any{"type": "node.setBehavior", "target": id, "payload": map[string]any{"nodeId": "n_root", "event": "click", "handlers": nil}}}}, &result)
	if len(result.Warnings) != 0 {
		t.Fatal(result)
	}
	result = changes.ApplyResult{}
	e.Must(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": 3}, &result)
	if len(result.Warnings["production"]) > 0 || !has(ir.Result{Diagnostics: result.Warnings["staging"][id.String()]}, "ACTION_PENDING_CAPABILITY") {
		t.Fatal(result)
	}
	review := submit(e, cs, 4, "staging")
	raw, _ := json.Marshal(review.Checks)
	if review.Changeset.State != "failed" || !strings.Contains(string(raw), "ACTION_PENDING_CAPABILITY") {
		t.Fatal(review)
	}
	current, _ := e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ID: cs, ProjectID: e.Admin.ProjectID})
	if current.State != "failed" || current.Seq != 4 {
		t.Fatal(current)
	}
}

// IR-042/CHG-034/PUB-020: impact and publication recheck data-source result paths.
func TestBindingImpactAndAtomicPublication(t *testing.T) {
	e := setup(t)
	e.ActivateManifest("staging", []byte(bindingContract))
	cs := newCS(e)
	id := bindingDoc(e, cs, 0)
	if r := validate(e, cs, id, "staging"); !r.Valid {
		t.Fatal(r)
	}
	submit(e, cs, 1, "staging")
	changed := strings.Replace(bindingContract, `"title":{"type":"text"}`, `"other":{"type":"text"}`, 1)
	var proposed any
	_ = json.Unmarshal([]byte(changed), &proposed)
	impacts, err := validation.AnalyzeImpact(context.Background(), e.Q, e.Admin.ProjectID, "staging", proposed)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, i := range impacts {
		if i.ObjectID == id && has(ir.Result{Diagnostics: i.Diagnostics}, "BINDING_PATH_UNRESOLVED") {
			found = true
		}
	}
	if !found {
		t.Fatal(impacts)
	}
	e.ActivateManifest("staging", []byte(changed))
	before := doc(e, cs, id)
	expectDiagnostic(t, e.Do(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil), "BINDING_PATH_UNRESOLVED")
	v, _ := e.Q.GetVersion(context.Background(), before.VersionID)
	obj, _ := e.Q.GetObject(context.Background(), store.GetObjectParams{ID: id, ProjectID: e.Admin.ProjectID})
	if string(before.Body) != string(v.Body) || v.State != "working" || obj.HeadVersionID != nil {
		t.Fatal("validation changed content")
	}
	for _, table := range []string{"publications", "published_pointers", "routes", "river_job"} {
		var n int
		if err := e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s %d %v", table, n, err)
		}
	}
	e.ActivateManifest("staging", []byte(bindingContract))
	publish(e, cs)
}

// CHG-034/IR-052: approval cannot record a decision after an action contract changes.
func TestActionApprovalRechecks(t *testing.T) {
	e := setup(t)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 1, "high": 2}, nil)
	e.ActivateManifest("staging", []byte(bindingContract))
	cs := newCS(e)
	create(e, cs, 0, "page", map[string]any{"type": "Native", "on": map[string]any{"click": map[string]any{"action": "test.run", "args": map[string]any{"enabled": map[string]any{"lit": true}}}}})
	submit(e, cs, 1, "staging")
	e.ActivateManifest("staging", []byte(strings.Replace(bindingContract, `"enabled":{"type":"boolean"`, `"enabled":{"type":"number"`, 1)))
	reviewer := e.Human("reviewer", auth.ContentPublish)
	expectDiagnostic(t, e.Do(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil), "ACTION_ARGS_INVALID")
	approvals, err := e.Q.ListApprovals(context.Background(), cs)
	if err != nil || len(approvals) != 0 {
		t.Fatal(approvals, err)
	}
}

// IR-052: page action targets resolve in the same project and prospective environment.
func TestActionPageResolution(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	target := create(e, cs, 0, "page", map[string]any{"type": "Box"})
	e.Exec(`UPDATE object_versions SET path='/products/:slug' WHERE changeset_id=$1 AND object_id=$2`, cs, target)
	page := create(e, cs, 1, "page", map[string]any{"type": "Button", "on": map[string]any{"click": map[string]any{"action": "navigate", "args": map[string]any{"to": map[string]any{"kind": "page", "page": target.String(), "params": map[string]any{"slug": "$context.locale"}}}}}})
	if r := validate(e, cs, page, "staging"); !r.Valid {
		t.Fatal(r)
	}
	candidate, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &cs)
	if err != nil {
		t.Fatal(err)
	}
	if problems, err := candidate.CheckPublication(context.Background(), []validation.Document{doc(e, cs, target), doc(e, cs, page)}); err != nil || len(problems) > 0 {
		t.Fatal(problems, err)
	}
	candidate.Overrides[target] = validation.Document{Body: []byte(`{"kind":"component"}`)}
	if r, err := candidate.Validate(context.Background(), doc(e, cs, page)); err != nil || !has(r, "ACTION_ARGS_INVALID") {
		t.Fatal(r, err)
	}
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root,on,click,args,to,page}',to_jsonb($2::text)) WHERE object_id=$1`, page, uuid.New().String())
	if r := validate(e, cs, page, "staging"); !has(r, "ACTION_ARGS_INVALID") {
		t.Fatal(r)
	}
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root,on,click,args,to,page}',to_jsonb($2::text)) WHERE object_id=$1`, page, target.String())
	e.Break("published_pointers", "")
	// A target outside the CS requires a database read; errors must propagate, not pass.
	d := doc(e, cs, page)
	c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Validate(context.Background(), d)
	cmstest.ExpectDBError(t, err)
}

// Draft diagnostics are optional until a manifest is active; target selection stays scoped.
func TestDraftNotReady(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	e.Exec("UPDATE environments SET active_manifest_id=NULL")
	id := create(e, cs, 0, "page", map[string]any{"type": "Text", "bindings": map[string]any{"text": "$item.title"}})
	if r := validate(e, cs, id, "staging"); !has(r, "MANIFEST_NOT_READY") {
		t.Fatal(r)
	}
}

// MF-027/IR-042: schema edits use the linked candidate even before first activation.
func TestSchemaDraftWarningsUseCandidate(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	id := create(e, cs, 0, "page", map[string]any{"type": "Native"})
	e.Exec("UPDATE changesets SET kind='schema' WHERE id=$1", cs)
	var result changes.ApplyResult
	payload := map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{map[string]any{"type": "node.setBinding", "target": id, "payload": map[string]any{"nodeId": "n_root", "prop": "title", "binding": "$props.missing"}}}}
	e.Must(e.Admin, "apply-operations", payload, &result)
	if len(result.Warnings) > 0 {
		t.Fatal(result)
	}
	candidate := e.ActivateManifest("unused", []byte(bindingContract))
	e.Exec("UPDATE environments SET active_manifest_id=NULL WHERE name='staging'")
	env, _ := e.Q.GetEnvironmentByName(context.Background(), store.GetEnvironmentByNameParams{ProjectID: e.Admin.ProjectID, Name: "staging"})
	e.Exec("INSERT INTO changeset_manifest_candidates(changeset_id,project_id,environment_id,manifest_id) VALUES($1,$2,$3,$4)", cs, e.Admin.ProjectID, env.ID, candidate)
	payload["expectedSeq"] = 2
	result = changes.ApplyResult{}
	e.Must(e.Admin, "apply-operations", payload, &result)
	if len(result.Warnings) != 1 || !has(ir.Result{Diagnostics: result.Warnings["staging"][id.String()]}, "BINDING_SCOPE_UNAVAILABLE") {
		t.Fatal(result)
	}
}
