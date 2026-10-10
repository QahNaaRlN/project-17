package validation_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"slices"
	"strings"
	"testing"
)

const contrastContract = `{"manifestVersion":"1.0","app":{"id":"test","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"breakpoints":{},"tokens":{"colors":{"foreground":"#000000","background":"#ffffff"}}}`

func pubState(e *cmstest.Env) string {
	var s string
	err := e.Pool.QueryRow(context.Background(), `SELECT jsonb_build_object('objects',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM objects o),'versions',(SELECT jsonb_agg(to_jsonb(v) ORDER BY id) FROM object_versions v),'pointers',(SELECT jsonb_agg(to_jsonb(p) ORDER BY object_id,environment_id) FROM published_pointers p),'publications',(SELECT jsonb_agg(to_jsonb(p) ORDER BY id) FROM publications p),'changesets',(SELECT jsonb_agg(to_jsonb(c) ORDER BY id) FROM changesets c),'operations',(SELECT count(*) FROM operations),'jobs',(SELECT count(*) FROM river_job))::text`).Scan(&s)
	if err != nil {
		e.T.Fatal(err)
	}
	return s
}

// 03 §6 / CHG-011: draft and undo retain soft A11Y diagnostics; submit blocks errors.
func TestA11YDraftUndoSubmit(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	var result changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": 0, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "root": map[string]any{"id": "n_root", "type": "Image"}}}}}, &result)
	raw, _ := json.Marshal(result)
	if !strings.Contains(string(raw), "A11Y_IMAGE_ALT_MISSING") {
		t.Fatal(string(raw))
	}
	id := result.Operations[0].Target
	if err := applyPolicy(e, e.Admin, cs, 1, operation(id, "node.setProps", map[string]any{"nodeId": "n_root", "set": map[string]any{"decorative": true}})); err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": 2}, nil)
	if r := submit(e, cs, 3, "staging"); r.Changeset.State != "failed" || !strings.Contains(string(r.Checks[0].Details), "A11Y_IMAGE_ALT_MISSING") {
		t.Fatal(r)
	}
}

// PUB-020 / 03 §6: environmental contrast changes block an approved publication atomically.
func TestA11YPublishApprovalImpactAndPromotion(t *testing.T) {
	e := setup(t)
	e.ActivateManifest("staging", []byte(contrastContract))
	e.ActivateManifest("production", []byte(contrastContract))
	cs := newCS(e)
	id := create(e, cs, 0, "page", map[string]any{"type": "Box", "design": map[string]any{"background": "background"}, "children": []any{map[string]any{"type": "Text", "design": map[string]any{"color": "foreground"}}}})
	submit(e, cs, 1, "staging")
	bad := strings.Replace(contrastContract, "#000000", "#ffffff", 1)
	e.ActivateManifest("staging", []byte(bad))
	before := pubState(e)
	expectDiagnostic(t, e.Do(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil), "A11Y_CONTRAST")
	if before != pubState(e) {
		t.Fatal("publication changed data")
	}
	proposed := map[string]any{}
	_ = json.Unmarshal([]byte(bad), &proposed)
	impacts, err := validation.AnalyzeImpact(context.Background(), e.Q, e.Admin.ProjectID, "staging", proposed)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, impact := range impacts {
		if impact.ObjectID == id {
			for _, d := range impact.Diagnostics {
				found = found || d.Code == "A11Y_CONTRAST"
			}
		}
	}
	if !found {
		t.Fatal(impacts)
	}
	e.ActivateManifest("staging", []byte(contrastContract))
	var pub struct{ ID uuid.UUID }
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, &pub)
	e.ActivateManifest("production", []byte(bad))
	before = pubState(e)
	expectDiagnostic(t, e.Do(e.Admin, "promote", map[string]any{"publicationId": pub.ID, "toEnvironment": "production"}, nil), "A11Y_CONTRAST")
	if before != pubState(e) {
		t.Fatal("promotion changed data")
	}
	// Approval rechecks L7 and records no decision on failure.
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 1, "high": 2}, nil)
	cs = newCS(e)
	create(e, cs, 0, "page", map[string]any{"type": "Box", "design": map[string]any{"background": "background"}, "children": []any{map[string]any{"type": "Text", "design": map[string]any{"color": "foreground"}}}})
	submit(e, cs, 1, "staging")
	e.ActivateManifest("staging", []byte(bad))
	reviewer := e.Human("contrast-review", auth.ContentPublish)
	expectDiagnostic(t, e.Do(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil), "A11Y_CONTRAST")
	approvals, err := e.Q.ListApprovals(context.Background(), cs)
	if err != nil || len(approvals) != 0 {
		t.Fatal(approvals, err)
	}
}

// 03 §6: warning-only diagnostics never block submission or publication.
func TestA11YWarningsPublish(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	create(e, cs, 0, "page", map[string]any{"type": "Box", "children": []any{map[string]any{"type": "Heading", "props": map[string]any{"level": 1}}, map[string]any{"type": "Heading", "props": map[string]any{"level": 3}}}})
	r := submit(e, cs, 1, "staging")
	if r.Changeset.State != "approved" || !strings.Contains(string(r.Checks[0].Details), "A11Y_HEADING_ORDER") {
		t.Fatal(r)
	}
	publish(e, cs)
}
func approveZone(e *cmstest.Env, actor auth.Actor, cs uuid.UUID) workflow.Review {
	var r workflow.Review
	e.Must(actor, "approve-changeset", map[string]any{"changesetId": cs}, &r)
	return r
}
func zoneNode(role string) map[string]any {
	return map[string]any{"type": "Box", "zone": map[string]any{"id": "zone", "mode": "SYSTEM", "requiredApprovalRole": role}, "children": []any{map[string]any{"id": "n_child", "type": "Text"}}}
}

// PUB-001/002/003: count and required roles are independent, from current project membership.
func TestZoneApprovalCoverageAndRevocation(t *testing.T) {
	for _, revocation := range []string{"role", "disabled", "capability"} {
		t.Run(revocation, func(t *testing.T) {
			e := setup(t)
			cs := newCS(e)
			create(e, cs, 0, "page", zoneNode("role-designer"))
			r := submit(e, cs, 1, "staging")
			if r.RequiredApprovals != 1 || r.Changeset.State != "in_review" || !slices.Equal(r.MissingRequiredRoles, []string{"role-designer"}) {
				t.Fatal(r)
			}
			wrong := e.Human("publisher", auth.ContentPublish)
			r = approveZone(e, wrong, cs)
			if r.Changeset.State != "in_review" || r.Approvals != 1 || len(r.MissingRequiredRoles) != 1 {
				t.Fatal(r)
			}
			right := e.Human("designer", auth.ContentPublish)
			r = approveZone(e, right, cs)
			if r.Changeset.State != "approved" || r.Approvals != 2 || len(r.MissingRequiredRoles) != 0 {
				t.Fatal(r)
			}
			detail, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, cs)
			if err != nil || len(detail.RequiredRoles) != 1 || len(detail.MissingRequiredRoles) != 0 {
				t.Fatal(detail, err)
			}
			switch revocation {
			case "role":
				e.Exec("DELETE FROM role_bindings WHERE actor_id=$1", right.ID)
			case "disabled":
				e.Exec("UPDATE actors SET disabled_at=now() WHERE id=$1", right.ID)
			case "capability":
				e.Exec("UPDATE roles SET capabilities=ARRAY[]::text[] WHERE name='role-designer'")
			}
			before := pubState(e)
			err = e.Do(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
			if cmstest.Code(err) != "APPROVAL_ROLES_MISSING" {
				t.Fatal(err)
			}
			if before != pubState(e) {
				t.Fatal("publication changed data")
			}
			detail, err = workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, cs)
			if err != nil || len(detail.MissingRequiredRoles) != 1 || detail.Approvals != 1 {
				t.Fatal(detail, err)
			}
		})
	}
}

// PUB-001: a single independent human can cover multiple project roles; STRICT sets high risk.
func TestZoneMultipleRolesStrictAndRemoval(t *testing.T) {
	e := setup(t)
	native := strings.TrimSuffix(cmstest.DefaultManifest, "}") + `,"components":{"Native":{"container":true,"props":{}}}}`
	for _, env := range []string{"staging", "production"} {
		e.ActivateManifest(env, []byte(native))
	}
	reviewer := e.Human("designer", auth.ContentPublish)
	role, err := e.Q.CreateRole(context.Background(), store.CreateRoleParams{ID: uuid.New(), ProjectID: e.Admin.ProjectID, Name: "owner", Capabilities: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Q.BindRole(context.Background(), store.BindRoleParams{ProjectID: e.Admin.ProjectID, ActorID: reviewer.ID, RoleID: role.ID}); err != nil {
		t.Fatal(err)
	}
	cs := newCS(e)
	n := zoneNode("role-designer")
	n["type"] = "Native"
	n["zone"] = map[string]any{"id": "ownerZone", "requiredApprovalRole": "owner", "mode": "STRICT"}
	nested := zoneNode("role-designer")
	nested["type"] = "Native"
	delete(nested, "children")
	nested["zone"].(map[string]any)["mode"] = "STRICT"
	n["children"] = []any{nested}
	id := create(e, cs, 0, "page", n)
	r := submit(e, cs, 1, "staging")
	if *r.Risk != "high" || len(r.RequiredRoles) != 2 {
		t.Fatal(r)
	}
	if r = approveZone(e, reviewer, cs); r.Changeset.State != "approved" {
		t.Fatal(r)
	}
	publish(e, cs)
	cs = newCS(e)
	if err := applyPolicy(e, e.Admin, cs, 0, operation(id, "node.setZone", map[string]any{"nodeId": "n_root", "zone": nil})); err != nil {
		t.Fatal(err)
	}
	r = submit(e, cs, 1, "staging")
	if !slices.Contains(r.RequiredRoles, "owner") || *r.Risk != "high" {
		t.Fatal(r)
	}
}

// PUB-001/IR-041: only live dependencies affect zone approvals in the selected environment.
func TestZoneLiveConsumers(t *testing.T) {
	for _, version := range []any{"live", 1} {
		t.Run(strings.TrimSpace(string(mustMarshal(version))), func(t *testing.T) {
			e := setup(t)
			cs := newCS(e)
			component := create(e, cs, 0, "component", map[string]any{"type": "Text"})
			submit(e, cs, 1, "staging")
			publish(e, cs)
			cs = newCS(e)
			n := ref(component, version)
			n["zone"] = map[string]any{"id": "consumerZone", "mode": "SYSTEM", "requiredApprovalRole": "role-consumer"}
			create(e, cs, 0, "page", n)
			submit(e, cs, 1, "staging")
			reviewer := e.Human("consumer", auth.ContentPublish)
			approveZone(e, reviewer, cs)
			publish(e, cs)
			cs = newCS(e)
			if err := applyPolicy(e, e.Admin, cs, 0, operation(component, "node.rename", map[string]any{"nodeId": "n_root", "name": "updated"})); err != nil {
				t.Fatal(err)
			}
			r := submit(e, cs, 1, "staging")
			if slices.Contains(r.RequiredRoles, "role-consumer") != (version == "live") {
				t.Fatal(r)
			}
		})
	}
}
func mustMarshal(v any) []byte { b, _ := json.Marshal(v); return b }

// PUB-001/020: exact pinned wrappers retain internal zones around changed live descendants.
func TestZonePinnedHistoricalWrapperAndPromotion(t *testing.T) {
	e := setup(t)
	reviewer := e.Human("historical", auth.ContentPublish)
	cs := newCS(e)
	leaf := create(e, cs, 0, "component", map[string]any{"type": "Text"})
	submit(e, cs, 1, "staging")
	publish(e, cs)
	cs = newCS(e)
	wrapperNode := ref(leaf, "live")
	wrapperNode["zone"] = map[string]any{"id": "historical", "mode": "SYSTEM", "requiredApprovalRole": "role-historical"}
	wrapper := create(e, cs, 0, "component", wrapperNode)
	submit(e, cs, 1, "staging")
	approveZone(e, reviewer, cs)
	publish(e, cs)
	cs = newCS(e)
	if err := applyPolicy(e, e.Admin, cs, 0, operation(wrapper, "node.setZone", map[string]any{"nodeId": "n_root", "zone": nil})); err != nil {
		t.Fatal(err)
	}
	submit(e, cs, 1, "staging")
	approveZone(e, reviewer, cs)
	publish(e, cs)
	cs = newCS(e)
	create(e, cs, 0, "page", ref(wrapper, 1))
	r := submit(e, cs, 1, "staging")
	if !slices.Contains(r.RequiredRoles, "role-historical") {
		t.Fatal(r)
	}
	approveZone(e, reviewer, cs)
	publish(e, cs)
	cs = newCS(e)
	if err := applyPolicy(e, e.Admin, cs, 0, operation(leaf, "node.rename", map[string]any{"nodeId": "n_root", "name": "changed"})); err != nil {
		t.Fatal(err)
	}
	r = submit(e, cs, 1, "staging")
	if !slices.Contains(r.RequiredRoles, "role-historical") {
		t.Fatal(r)
	}
	approveZone(e, reviewer, cs)
	var pub struct{ ID uuid.UUID }
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, &pub)
	// Promotion of a previously approved protected page also rechecks current membership.
	protectedCS := newCS(e)
	create(e, protectedCS, 0, "page", zoneNode("role-historical"))
	submit(e, protectedCS, 1, "staging")
	approveZone(e, reviewer, protectedCS)
	e.Must(e.Admin, "publish", map[string]any{"changesetId": protectedCS, "environment": "staging"}, &pub)
	e.Exec("DELETE FROM role_bindings WHERE actor_id=$1", reviewer.ID)
	before := pubState(e)
	err := e.Do(e.Admin, "promote", map[string]any{"publicationId": pub.ID, "toEnvironment": "production"}, nil)
	if cmstest.Code(err) != "APPROVAL_ROLES_MISSING" || before != pubState(e) {
		t.Fatal(err)
	}
}

// PUB-001: request-changes keeps role requirements visible without approving the Change Set.
func TestZoneRequestChanges(t *testing.T) {
	e := setup(t)
	cs := newCS(e)
	create(e, cs, 0, "page", zoneNode("role-reviewer"))
	submit(e, cs, 1, "staging")
	reviewer := e.Human("reviewer", auth.ContentPublish)
	var r workflow.Review
	e.Must(reviewer, "request-changes", map[string]any{"changesetId": cs, "comment": "revise"}, &r)
	if r.Changeset.State != "changes_requested" || len(r.RequiredRoles) != 1 || len(r.MissingRequiredRoles) != 1 {
		t.Fatal(r)
	}
}

// PUB-001: a foreign role bound with a mismatched project never supplies capabilities or coverage.
func TestZoneRoleProjectBoundary(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	other, err := e.Q.CreateProject(ctx, store.CreateProjectParams{ID: uuid.New(), Slug: "other", Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := e.Q.CreateRole(ctx, store.CreateRoleParams{ID: uuid.New(), ProjectID: other.ID, Name: "role-required", Capabilities: []string{string(auth.DesignCompose)}})
	if err != nil {
		t.Fatal(err)
	}
	reviewer := e.Human("wrong", auth.ContentPublish)
	e.Exec("INSERT INTO role_bindings(project_id,actor_id,role_id) VALUES ($1,$2,$3)", e.Admin.ProjectID, reviewer.ID, foreign.ID)
	caps, err := e.Q.ActorCapabilities(ctx, store.ActorCapabilitiesParams{ProjectID: e.Admin.ProjectID, ActorID: reviewer.ID})
	if err != nil || slices.Contains(caps, string(auth.DesignCompose)) {
		t.Fatal(caps, err)
	}
	cs := newCS(e)
	create(e, cs, 0, "page", zoneNode("role-required"))
	submit(e, cs, 1, "staging")
	r := approveZone(e, reviewer, cs)
	if r.Changeset.State != "in_review" || !slices.Equal(r.MissingRequiredRoles, []string{"role-required"}) {
		t.Fatal(r)
	}
}
