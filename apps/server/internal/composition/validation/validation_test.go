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
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

const contract = `{"manifestVersion":"1.0","app":{"id":"test","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"breakpoints":{},"tokens":{},"components":{"Native":{"props":{"title":{"type":"string"}}}}}`

func setup(t *testing.T) *cmstest.Env {
	e := cmstest.New(t, workflow.Register, publishing.Register)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	return e
}
func newCS(e *cmstest.Env) uuid.UUID {
	var cs changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "validation"}, &cs)
	return cs.ID
}
func create(e *cmstest.Env, cs uuid.UUID, seq int, kind string, node map[string]any) uuid.UUID {
	var r changes.ApplyResult
	node["id"] = "n_root"
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": seq, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": kind, "root": node}}}}, &r)
	return r.Operations[0].Target
}
func ref(id uuid.UUID, version any) map[string]any {
	return map[string]any{"type": "Composed", "ref": map[string]any{"component": id.String(), "version": version}}
}
func submit(e *cmstest.Env, cs uuid.UUID, seq int, target string) workflow.Review {
	var r workflow.Review
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": seq, "targets": []string{target}}, &r)
	return r
}
func publish(e *cmstest.Env, cs uuid.UUID) {
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
}
func has(r ir.Result, code string) bool {
	for _, d := range r.Diagnostics {
		if string(d.Code) == code {
			return true
		}
	}
	return false
}
func doc(e *cmstest.Env, cs, id uuid.UUID) validation.Document {
	d, err := e.Q.GetWorkingDocument(context.Background(), store.GetWorkingDocumentParams{ChangesetID: cs, ObjectID: id, ProjectID: e.Admin.ProjectID})
	if err != nil {
		e.T.Fatal(err)
	}
	return validation.Document{ObjectID: id, VersionID: d.VersionID, Body: d.Body}
}
func validate(e *cmstest.Env, cs, id uuid.UUID, environment string) ir.Result {
	c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, environment, &cs)
	if err != nil {
		e.T.Fatal(err)
	}
	r, err := c.Validate(context.Background(), doc(e, cs, id))
	if err != nil {
		e.T.Fatal(err)
	}
	return r
}

// MF-003/MF-023/CHG-034/PUB-020: a changed contract blocks approved content without writes.
func TestEnvironmentContractsAndAtomicFailure(t *testing.T) {
	e := setup(t)
	e.ActivateManifest("staging", []byte(contract))
	cs := newCS(e)
	id := create(e, cs, 0, "page", map[string]any{"type": "Native", "props": map[string]any{"title": "ok"}})
	if r := validate(e, cs, id, "staging"); !r.Valid {
		t.Fatal(r)
	}
	if r := validate(e, cs, id, "production"); !has(r, "TYPE_UNKNOWN") {
		t.Fatal(r)
	}
	if r := submit(e, cs, 1, "staging"); r.Changeset.State != "approved" {
		t.Fatal(r)
	}
	e.Exec("UPDATE environments SET active_manifest_id=NULL WHERE name='staging'")
	before := doc(e, cs, id)
	err := e.Do(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
	encoded, _ := json.Marshal(err)
	if cmstest.Code(err) != "VALIDATION_FAILED" || !strings.Contains(string(encoded), "MANIFEST_NOT_READY") {
		t.Fatalf("%v", err)
	}
	obj, _ := e.Q.GetObject(context.Background(), store.GetObjectParams{ID: id, ProjectID: e.Admin.ProjectID})
	v, _ := e.Q.GetVersion(context.Background(), before.VersionID)
	if obj.HeadVersionID != nil || v.State != "working" || string(before.Body) != string(v.Body) {
		t.Fatal("failed validation changed content")
	}
	for _, table := range []string{"publications", "published_pointers", "routes", "river_job"} {
		var n int
		if err := e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s %d %v", table, n, err)
		}
	}
	current, _ := e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ID: cs, ProjectID: e.Admin.ProjectID})
	if current.State != "approved" {
		t.Fatal(current)
	}
	e.ActivateManifest("staging", []byte(contract))
	publish(e, cs)
}

// IR-061/062, PUB-010: same-CS components, exact numbered versions and transitive cycles.
func TestComposedResolution(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	component := create(e, cs, 0, "component", map[string]any{"type": "Box"})
	page := create(e, cs, 1, "page", ref(component, "live"))
	if r := validate(e, cs, page, "staging"); !r.Valid {
		t.Fatal(r)
	}
	if r := submit(e, cs, 2, "staging"); r.Changeset.State != "approved" {
		t.Fatal(r)
	}
	publish(e, cs)
	other := newCS(e)
	live := create(e, other, 0, "page", ref(component, "live"))
	pinned := create(e, other, 1, "page", ref(component, 1))
	if r := validate(e, other, live, "production"); !has(r, "COMPONENT_NOT_FOUND") {
		t.Fatal(r)
	}
	if r := validate(e, other, pinned, "production"); !r.Valid {
		t.Fatal(r)
	}
	// A draft in another CS is invisible, but the same CS repair takes precedence.
	edit := e.Edit(e.Admin, component, "new")
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root,type}','"Missing"') WHERE changeset_id=$1`, edit)
	if r := validate(e, other, live, "staging"); !r.Valid {
		t.Fatal(r)
	}
	if r := validate(e, edit, component, "staging"); !has(r, "TYPE_UNKNOWN") {
		t.Fatal(r)
	}
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root}', $2::jsonb) WHERE changeset_id=$1`, edit, string(marshal(ref(component, "live"), true)))
	if r := validate(e, edit, component, "staging"); !has(r, "COMPONENT_CYCLE") {
		t.Fatal(r)
	}
	// Wrong project and page objects cannot be resolved as components.
	for _, target := range []uuid.UUID{page, uuid.New()} {
		broken := newCS(e)
		id := create(e, broken, 0, "page", ref(target, "live"))
		if r := validate(e, broken, id, "staging"); !has(r, "COMPONENT_NOT_FOUND") {
			t.Fatal(r)
		}
	}
}
func marshal(node map[string]any, root bool) []byte {
	if root {
		node["id"] = "n_root"
	}
	b, _ := json.Marshal(node)
	return b
}

// PUB-010/MF-023: consumers are checked transitively; repairs in the publication batch count.
func TestLiveConsumerAndBatchRepair(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	a := create(e, cs, 0, "component", map[string]any{"type": "Box"})
	b := create(e, cs, 1, "component", ref(a, "live"))
	page := create(e, cs, 2, "page", ref(b, "live"))
	submit(e, cs, 3, "staging")
	publish(e, cs)
	edit := e.Edit(e.Admin, a, "change")
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{inputs}','{"title":{"type":"text","required":true,"content":true}}') WHERE changeset_id=$1`, edit)
	r := submit(e, edit, 1, "staging")
	if r.Changeset.State != "failed" || !strings.Contains(string(r.Checks[0].Details), page.String()) {
		t.Fatal(r)
	}
	e.Must(e.Admin, "reopen-changeset", map[string]any{"changesetId": edit}, nil)
	// Bring dependent component into the same CS and supply the new required input.
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": edit, "expectedSeq": 1, "operations": []any{map[string]any{"type": "node.rename", "target": b, "payload": map[string]any{"nodeId": "n_root", "name": "repair"}}}}, nil)
	e.Exec(`UPDATE object_versions SET body=jsonb_set(jsonb_set(body,'{nodes,n_root,bindings}','{"title":"$local.t_fixed"}'),'{localContent}','{"t_fixed":{"type":"text","value":{"ru":"fixed"}}}') WHERE changeset_id=$1 AND object_id=$2`, edit, b)
	if r := submit(e, edit, 2, "staging"); r.Changeset.State != "approved" {
		t.Fatal(r)
	}
	publish(e, edit)
}

// MF-020/023: manifest impact includes a native use behind two Composed instances.
func TestTransitiveManifestImpact(t *testing.T) {
	e := setup(t)
	e.ActivateManifest("staging", []byte(contract))
	cs := newCS(e)
	a := create(e, cs, 0, "component", map[string]any{"type": "Native", "props": map[string]any{"title": "text"}})
	b := create(e, cs, 1, "component", ref(a, "live"))
	p := create(e, cs, 2, "page", ref(b, "live"))
	submit(e, cs, 3, "staging")
	publish(e, cs)
	var proposed any
	_ = json.Unmarshal([]byte(strings.Replace(contract, `"type":"string"`, `"type":"number"`, 1)), &proposed)
	rows, err := validation.AnalyzeImpact(context.Background(), e.Q, e.Admin.ProjectID, "staging", proposed)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, row := range rows {
		if row.ObjectID == p {
			found[row.Stage] = true
			if len(row.Changes) == 0 || len(row.Diagnostics) == 0 {
				t.Fatal(row)
			}
		}
	}
	if !found["head"] || !found["published"] {
		t.Fatal(rows)
	}
}

// MF-027/PUB-004: schema candidate validation can bootstrap a NULL environment;
// content cannot publish under that candidate until atomic activation is implemented.
func TestCandidateBinding(t *testing.T) {
	e := setup(t)
	candidate := e.ActivateManifest("unused", []byte(contract))
	cs := newCS(e)
	id := create(e, cs, 0, "page", map[string]any{"type": "Native", "props": map[string]any{"title": "text"}})
	e.Exec("UPDATE changesets SET kind='schema' WHERE id=$1", cs)
	e.Exec("UPDATE environments SET active_manifest_id=NULL WHERE name='staging'")
	env, _ := e.Q.GetEnvironmentByName(context.Background(), store.GetEnvironmentByNameParams{ProjectID: e.Admin.ProjectID, Name: "staging"})
	e.Exec("INSERT INTO changeset_manifest_candidates(changeset_id,project_id,environment_id,manifest_id) VALUES($1,$2,$3,$4)", cs, e.Admin.ProjectID, env.ID, candidate)
	if r := validate(e, cs, id, "staging"); !r.Valid {
		t.Fatal(r)
	}
	if r := submit(e, cs, 1, "staging"); r.Changeset.State != "approved" {
		t.Fatal(r)
	}
	if err := e.Do(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil); cmstest.Code(err) != "SCHEMA_PUBLICATION_NOT_READY" {
		t.Fatal(err)
	}
	if _, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "production", &cs); cmstest.Code(err) != "MANIFEST_CANDIDATE_MISMATCH" {
		t.Fatal(err)
	}
	e.ActivateManifest("staging", []byte(contract))
	if _, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &cs); cmstest.Code(err) != "MANIFEST_CANDIDATE_MISMATCH" {
		t.Fatal(err)
	}
	if _, err := e.Pool.Exec(context.Background(), "DELETE FROM changeset_manifest_candidates WHERE changeset_id=$1", cs); err == nil {
		t.Fatal("mutable binding")
	}
}

// CHG-034: an approval rechecks the current environment before recording a decision.
func TestApprovalRechecksManifest(t *testing.T) {
	e := setup(t)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 1, "high": 2}, nil)
	e.ActivateManifest("staging", []byte(contract))
	cs := newCS(e)
	create(e, cs, 0, "page", map[string]any{"type": "Native"})
	submit(e, cs, 1, "staging")
	e.Exec("UPDATE environments SET active_manifest_id=NULL WHERE name='staging'")
	reviewer := e.Human("reviewer", auth.ContentPublish)
	if err := e.Do(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatal(err)
	}
	approvals, err := e.Q.ListApprovals(context.Background(), cs)
	if err != nil || len(approvals) != 0 {
		t.Fatal(approvals, err)
	}
}

// PUB-020: promotion uses the destination contract and leaves pointers untouched on failure.
func TestPromotionRechecksDestination(t *testing.T) {
	e := setup(t)
	e.ActivateManifest("staging", []byte(contract))
	cs := newCS(e)
	create(e, cs, 0, "page", map[string]any{"type": "Native"})
	submit(e, cs, 1, "staging")
	var p publishing.Publication
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, &p)
	if err := e.Do(e.Admin, "promote", map[string]any{"publicationId": p.ID, "toEnvironment": "production"}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatal(err)
	}
	e.ActivateManifest("production", []byte(contract))
	e.Must(e.Admin, "promote", map[string]any{"publicationId": p.ID, "toEnvironment": "production"}, nil)
}
