package validation_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

const designContract = `{"manifestVersion":"1.0","app":{"id":"test","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"breakpoints":{"md":768},"tokens":{"spacing":{"sm":"8px"},"colors":{"primary":"#112233"}},"components":{"Native":{"props":{},"events":{}}}}`

func designEnv(t *testing.T) *cmstest.Env {
	e := setup(t)
	for _, env := range []string{"staging", "production"} {
		e.ActivateManifest(env, []byte(designContract))
	}
	e.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"SYSTEM","maxMode":"CODE"}' WHERE id=$1`, e.Admin.ProjectID)
	return e
}
func operation(id uuid.UUID, typ string, payload any) map[string]any {
	return map[string]any{"type": typ, "target": id, "payload": payload}
}
func applyPolicy(e *cmstest.Env, actor auth.Actor, cs uuid.UUID, seq int, ops ...map[string]any) error {
	return e.Do(actor, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": seq, "operations": ops}, nil)
}

type snapshot struct {
	body       []byte
	cert       bool
	seq, count int
}

func state(e *cmstest.Env, cs, id uuid.UUID) snapshot {
	d := doc(e, cs, id)
	v, err := e.Q.GetVersion(context.Background(), d.VersionID)
	if err != nil {
		e.T.Fatal(err)
	}
	s := snapshot{body: d.Body, cert: v.Certified}
	if err := e.Pool.QueryRow(context.Background(), `SELECT seq,(SELECT count(*) FROM operations WHERE changeset_id=$1) FROM changesets WHERE id=$1`, cs).Scan(&s.seq, &s.count); err != nil {
		e.T.Fatal(err)
	}
	return s
}
func unchanged(t *testing.T, a, b snapshot) {
	t.Helper()
	if !bytes.Equal(a.body, b.body) || a.cert != b.cert || a.seq != b.seq || a.count != b.count {
		t.Fatalf("atomicity violated: %+v -> %+v", a, b)
	}
}

// DS-020/CHG-011: a late L5 failure rolls back earlier edits, journal and seq.
func TestDesignBatchAtomicityAndDowngrade(t *testing.T) {
	e := designEnv(t)
	cs := newCS(e)
	id := create(e, cs, 0, "page", map[string]any{"type": "Box"})
	before := state(e, cs, id)
	err := applyPolicy(e, e.Admin, cs, 1, operation(id, "node.rename", map[string]any{"nodeId": "n_root", "name": "changed"}), operation(id, "node.setDesign", map[string]any{"nodeId": "n_root", "set": map[string]any{"color": "missing"}}))
	expectDiagnostic(t, err, "DESIGN_VALUE_INVALID")
	unchanged(t, before, state(e, cs, id))
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 1, "operations": []any{operation(id, "document.setPolicy", map[string]any{"policy": map[string]any{"mode": "FREE"}}), operation(id, "node.setDesign", map[string]any{"nodeId": "n_root", "set": map[string]any{"padding": map[string]any{"raw": "20px"}}})}}, nil)
	before = state(e, cs, id)
	err = applyPolicy(e, e.Admin, cs, 3, operation(id, "document.setPolicy", map[string]any{"policy": map[string]any{"mode": "SYSTEM"}}))
	expectDiagnostic(t, err, "DESIGN_VALUE_INVALID")
	unchanged(t, before, state(e, cs, id))
	err = applyPolicy(e, e.Admin, cs, 3, operation(id, "document.setPolicy", map[string]any{"policy": map[string]any{"mode": "SYSTEM"}}), operation(id, "node.setDesign", map[string]any{"nodeId": "n_root", "set": map[string]any{"padding": "sm"}}))
	if err != nil {
		t.Fatal(err)
	}
	// Undo would reintroduce raw in SYSTEM and must also be atomic.
	before = state(e, cs, id)
	err = e.Do(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": 5}, nil)
	expectDiagnostic(t, err, "DESIGN_VALUE_INVALID")
	unchanged(t, before, state(e, cs, id))
}

// IR locked/CHG-011: editing content is allowed; moving a protected subtree is guarded.
func TestLockedRightsAndUndo(t *testing.T) {
	e := designEnv(t)
	cs := newCS(e)
	id := create(e, cs, 0, "page", map[string]any{"type": "Box", "children": []any{map[string]any{"id": "n_lock", "type": "Box", "locked": true, "children": []any{map[string]any{"id": "n_child", "type": "Text"}}}, map[string]any{"id": "n_other", "type": "Box"}}})
	limited := e.Admin
	limited.Rights = auth.RightSet{auth.DesignCompose: true}
	before := state(e, cs, id)
	err := applyPolicy(e, limited, cs, 1, operation(id, "node.move", map[string]any{"nodeId": "n_lock", "parentId": "n_other"}))
	if cmstest.Code(err) != "OPERATION_INVALID" {
		t.Fatal(err)
	}
	unchanged(t, before, state(e, cs, id))
	if err := applyPolicy(e, limited, cs, 1, operation(id, "node.setProps", map[string]any{"nodeId": "n_child", "set": map[string]any{"text": "content"}})); err != nil {
		t.Fatal(err)
	}
	if err := applyPolicy(e, e.Admin, cs, 2, operation(id, "node.setLocked", map[string]any{"nodeId": "n_lock", "locked": false})); err != nil {
		t.Fatal(err)
	}
	before = state(e, cs, id)
	err = e.Do(limited, "undo", map[string]any{"changesetId": cs, "expectedSeq": 3}, nil)
	if cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	unchanged(t, before, state(e, cs, id))
	// Inserting a newly locked zone requires the management right too.
	err = applyPolicy(e, limited, cs, 3, operation(id, "node.insert", map[string]any{"parentId": "n_root", "subtree": map[string]any{"type": "Box", "zone": map[string]any{"id": "new", "mode": "SYSTEM", "locked": true}}}))
	if cmstest.Code(err) != "OPERATION_INVALID" {
		t.Fatal(err)
	}
	unchanged(t, before, state(e, cs, id))
}

// DS-021/IR-063: certification is trusted per version and removed by edits/undo.
func TestCertificationAndExactVersions(t *testing.T) {
	e := designEnv(t)
	cs := newCS(e)
	component := create(e, cs, 0, "component", map[string]any{"type": "Box"})
	limited := e.Admin
	limited.Rights = auth.RightSet{auth.DesignCompose: true}
	err := applyPolicy(e, limited, cs, 1, operation(component, "component.certify", map[string]any{"certified": true}))
	if cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	if err := applyPolicy(e, e.Admin, cs, 1, operation(component, "component.certify", map[string]any{"certified": true})); err != nil {
		t.Fatal(err)
	}
	if !state(e, cs, component).cert {
		t.Fatal("not certified")
	}
	var result changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 2, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "policy": map[string]any{"mode": "STRICT"}, "root": map[string]any{"id": "n_root", "type": "Composed", "ref": map[string]any{"component": component.String(), "version": "live"}}}}}}, &result)
	page := result.Operations[0].Target
	submit(e, cs, 3, "staging")
	publish(e, cs)
	next := newCS(e)
	if err := applyPolicy(e, e.Admin, next, 0, operation(component, "node.rename", map[string]any{"nodeId": "n_root", "name": "new"})); err != nil {
		t.Fatal(err)
	}
	if state(e, next, component).cert {
		t.Fatal("stale certification")
	}
	e.Must(e.Admin, "undo", map[string]any{"changesetId": next, "expectedSeq": 1}, nil)
	if state(e, next, component).cert {
		t.Fatal("undo restored certification")
	}
	r := submit(e, next, 2, "staging")
	raw, _ := json.Marshal(r)
	if r.Changeset.State != "failed" || !strings.Contains(string(raw), "POLICY_COMPONENT_MODE_REQUIRED") {
		t.Fatal(string(raw))
	}
	// The pinned published version retains certification after changes to head/working.
	published, err := e.Q.ValidationPublishedVersions(context.Background(), store.ValidationPublishedVersionsParams{ProjectID: e.Admin.ProjectID, ID: envID(e, "staging")})
	if err != nil {
		t.Fatal(err)
	}
	var compVersion int32
	for _, v := range published {
		if v.ObjectID == component {
			stored, _ := e.Q.GetVersion(context.Background(), v.VersionID)
			compVersion = *stored.Number
			if !stored.Certified {
				t.Fatal("published certificate lost")
			}
		}
	}
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{nodes,n_root,ref,version}',to_jsonb($2::int)) WHERE object_id=$1 AND state='committed'`, page, compVersion)
	c, err := validation.Load(context.Background(), e.Q, e.Admin.ProjectID, "staging", &next)
	if err != nil {
		t.Fatal(err)
	}
	problems, err := c.CheckPublication(context.Background(), []validation.Document{doc(e, next, component)})
	if err != nil || len(problems) > 0 {
		t.Fatal(problems, err)
	}
}
func envID(e *cmstest.Env, name string) uuid.UUID {
	env, err := e.Q.GetEnvironmentByName(context.Background(), store.GetEnvironmentByNameParams{ProjectID: e.Admin.ProjectID, Name: name})
	if err != nil {
		e.T.Fatal(err)
	}
	return env.ID
}

// PUB-020/MF-023: changing an environment's tokens invalidates approved content.
func TestDesignImpactAndPublicationRecheck(t *testing.T) {
	e := designEnv(t)
	cs := newCS(e)
	id := create(e, cs, 0, "page", map[string]any{"type": "Text", "design": map[string]any{"color": "primary"}})
	submit(e, cs, 1, "staging")
	changed := strings.Replace(designContract, `"colors":{"primary":"#112233"}`, `"colors":{}`, 1)
	var proposed any
	_ = json.Unmarshal([]byte(changed), &proposed)
	impacts, err := validation.AnalyzeImpact(context.Background(), e.Q, e.Admin.ProjectID, "staging", proposed)
	if err != nil || len(impacts) == 0 {
		t.Fatal(impacts, err)
	}
	e.ActivateManifest("staging", []byte(changed))
	before := state(e, cs, id)
	err = e.Do(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
	expectDiagnostic(t, err, "DESIGN_VALUE_INVALID")
	unchanged(t, before, state(e, cs, id))
	var count int
	e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM published_pointers WHERE object_id=$1`, id).Scan(&count)
	if count != 0 {
		t.Fatal("published invalid design")
	}
}

// Bootstrap and unbound schema drafts must still enforce the project cap.
func TestPolicyWithoutManifest(t *testing.T) {
	e := designEnv(t)
	e.Exec(`UPDATE environments SET active_manifest_id=NULL WHERE project_id=$1`, e.Admin.ProjectID)
	e.Exec(`UPDATE projects SET settings='{}' WHERE id=$1`, e.Admin.ProjectID)
	cs := newCS(e)
	e.Exec(`UPDATE changesets SET kind='schema' WHERE id=$1`, cs)
	err := e.Do(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "policy": map[string]any{"mode": "FREE"}, "root": map[string]any{"type": "Box"}}}}}, nil)
	expectDiagnostic(t, err, "POLICY_MODE_EXCEEDS_PARENT")
	var count int
	e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM changeset_objects WHERE changeset_id=$1`, cs).Scan(&count)
	if count != 0 {
		t.Fatal("created invalid document")
	}
}

// CHG-040/DS-020: rebase uses the new base's locks and checks the final design.
func TestPolicyRebaseAtomicity(t *testing.T) {
	e := designEnv(t)
	base := newCS(e)
	id := create(e, base, 0, "page", map[string]any{"type": "Box"})
	submit(e, base, 1, "staging")
	publish(e, base)
	old := newCS(e)
	if err := applyPolicy(e, e.Admin, old, 0, operation(id, "node.insert", map[string]any{"parentId": "n_root", "subtree": map[string]any{"type": "Text"}})); err != nil {
		t.Fatal(err)
	}
	lock := newCS(e)
	if err := applyPolicy(e, e.Admin, lock, 0, operation(id, "node.setLocked", map[string]any{"nodeId": "n_root", "locked": true})); err != nil {
		t.Fatal(err)
	}
	submit(e, lock, 1, "staging")
	publish(e, lock)
	limited := e.Admin
	limited.Rights = auth.RightSet{auth.DesignCompose: true}
	before := state(e, old, id)
	err := e.Do(limited, "rebase-changeset", map[string]any{"changesetId": old, "expectedSeq": 1}, nil)
	if cmstest.Code(err) != "OPERATION_INVALID" {
		t.Fatal(err)
	}
	unchanged(t, before, state(e, old, id))
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": old, "expectedSeq": 1}, nil)
	// A changed token contract also blocks open-CS rebase, even without submission.
	color := newCS(e)
	if err := applyPolicy(e, e.Admin, color, 0, operation(id, "node.setDesign", map[string]any{"nodeId": "n_root", "set": map[string]any{"color": "primary"}})); err != nil {
		t.Fatal(err)
	}
	newer := newCS(e)
	if err := applyPolicy(e, e.Admin, newer, 0, operation(id, "node.rename", map[string]any{"nodeId": "n_root", "name": "head"})); err != nil {
		t.Fatal(err)
	}
	submit(e, newer, 1, "staging")
	publish(e, newer)
	e.ActivateManifest("staging", []byte(strings.Replace(designContract, `"colors":{"primary":"#112233"}`, `"colors":{}`, 1)))
	before = state(e, color, id)
	err = e.Do(e.Admin, "rebase-changeset", map[string]any{"changesetId": color, "expectedSeq": 1}, nil)
	expectDiagnostic(t, err, "DESIGN_VALUE_INVALID")
	unchanged(t, before, state(e, color, id))
}

// DS-021: neither open meta nor page targets can fabricate trusted certification.
func TestCertificationPayloadAndForgery(t *testing.T) {
	e := designEnv(t)
	cs := newCS(e)
	id := create(e, cs, 0, "component", map[string]any{"type": "Box"})
	for _, payload := range []any{map[string]any{}, map[string]any{"certified": "true"}, map[string]any{"certified": true, "extra": true}} {
		before := state(e, cs, id)
		if err := applyPolicy(e, e.Admin, cs, 1, operation(id, "component.certify", payload)); cmstest.Code(err) != "OPERATION_INVALID" {
			t.Fatal(err)
		}
		unchanged(t, before, state(e, cs, id))
	}
	page := create(e, cs, 1, "page", map[string]any{"type": "Box"})
	if err := applyPolicy(e, e.Admin, cs, 2, operation(page, "component.certify", map[string]any{"certified": true})); cmstest.Code(err) != "OPERATION_INVALID" {
		t.Fatal(err)
	}
	e.Exec(`UPDATE object_versions SET body=jsonb_set(body,'{meta}','{"certified":true}') WHERE changeset_id=$1 AND object_id=$2`, cs, id)
	err := e.Do(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 2, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "policy": map[string]any{"mode": "STRICT"}, "root": ref(id, "live")}}}}, nil)
	expectDiagnostic(t, err, "POLICY_COMPONENT_MODE_REQUIRED")
	// Editing then certifying is valid in one atomic package.
	if err := applyPolicy(e, e.Admin, cs, 2, operation(id, "node.rename", map[string]any{"nodeId": "n_root", "name": "reviewed"}), operation(id, "component.certify", map[string]any{"certified": true})); err != nil {
		t.Fatal(err)
	}
	if !state(e, cs, id).cert {
		t.Fatal("certificate not saved")
	}
	e.Must(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": 4}, nil)
	if state(e, cs, id).cert {
		t.Fatal("undo certify")
	}
}

// DS-021/CHG-040: a certificate cannot silently move onto an unreviewed base.
func TestCertificateRebaseNeedsConfirmation(t *testing.T) {
	e := designEnv(t)
	base := newCS(e)
	id := create(e, base, 0, "component", map[string]any{"type": "Box"})
	submit(e, base, 1, "staging")
	publish(e, base)
	old := newCS(e)
	var applied changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": old, "expectedSeq": 0, "operations": []any{operation(id, "component.certify", map[string]any{"certified": true})}}, &applied)
	newer := newCS(e)
	if err := applyPolicy(e, e.Admin, newer, 0, operation(id, "node.rename", map[string]any{"nodeId": "n_root", "name": "changed base"})); err != nil {
		t.Fatal(err)
	}
	submit(e, newer, 1, "staging")
	publish(e, newer)
	var outcome workflow.RebaseOutcome
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": old, "expectedSeq": 1}, &outcome)
	if len(outcome.Conflicts) != 1 || outcome.Conflicts[0].Code != "BEFORE_MISMATCH" {
		t.Fatal(outcome)
	}
	limited := e.Admin
	limited.Rights = auth.RightSet{auth.DesignCompose: true}
	err := e.Do(limited, "rebase-changeset", map[string]any{"changesetId": old, "expectedSeq": 1, "resolutions": []any{map[string]any{"operationId": applied.Operations[0].ID, "choice": "mine"}}}, nil)
	if cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": old, "expectedSeq": 1, "resolutions": []any{map[string]any{"operationId": applied.Operations[0].ID, "choice": "mine"}}}, nil)
	if !state(e, old, id).cert {
		t.Fatal("confirmed certificate lost")
	}
	// Rebase dropping certification leaves the new head's trusted value unchanged.
	drop := newCS(e)
	var r changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": drop, "expectedSeq": 0, "operations": []any{operation(id, "component.certify", map[string]any{"certified": true})}}, &r)
	next := newCS(e)
	if err := applyPolicy(e, e.Admin, next, 0, operation(id, "node.rename", map[string]any{"nodeId": "n_root", "name": "another base"})); err != nil {
		t.Fatal(err)
	}
	submit(e, next, 1, "staging")
	publish(e, next)
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": drop, "expectedSeq": 1, "resolutions": []any{map[string]any{"operationId": r.Operations[0].ID, "choice": "theirs"}}}, nil)
}

// PUB-020/DS-021: approval rechecks L5 and binds the trusted certificate flag.
func TestDesignAndCertificateApprovalRecheck(t *testing.T) {
	e := designEnv(t)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 1, "medium": 1, "high": 1}, nil)
	reviewer := e.Human("design-reviewer", auth.ContentPublish)
	cs := newCS(e)
	id := create(e, cs, 0, "component", map[string]any{"type": "Box", "design": map[string]any{"color": "primary"}})
	submit(e, cs, 1, "staging")
	before := state(e, cs, id)
	e.ActivateManifest("staging", []byte(strings.Replace(designContract, `"colors":{"primary":"#112233"}`, `"colors":{}`, 1)))
	err := e.Do(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil)
	raw, _ := json.Marshal(err)
	if cmstest.Code(err) != "VALIDATION_FAILED" || !strings.Contains(string(raw), "DESIGN_VALUE_INVALID") {
		t.Fatal(err)
	}
	unchanged(t, before, state(e, cs, id))
	e.ActivateManifest("staging", []byte(designContract))
	e.Exec(`UPDATE object_versions SET certified=true WHERE object_id=$1 AND changeset_id=$2`, id, cs)
	err = e.Do(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil)
	if cmstest.Code(err) != "CHANGESET_CONTENT_CHANGED" {
		t.Fatal(err)
	}
	var count int
	if err := e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM approvals WHERE changeset_id=$1`, cs).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("recorded invalid approval")
	}
}
