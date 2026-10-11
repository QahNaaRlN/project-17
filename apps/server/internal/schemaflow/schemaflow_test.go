package schemaflow_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/schemaflow"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { pgtest.Main(m) }
func setup(t *testing.T) *cmstest.Env {
	e := cmstest.New(t, manifestregistry.Register, workflow.Register, publishing.Register)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 1}, nil)
	return e
}
func manifest(version, field string) string {
	return strings.TrimSuffix(cmstest.DefaultManifest, "}") + `,"schemas":{"Product":{"version":` + version + `,"fields":{"title":{"type":"` + field + `"}}}}}`
}
func register(e *cmstest.Env, raw, env string) manifestregistry.Registration {
	var r manifestregistry.Registration
	e.Must(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: json.RawMessage(raw), Environment: env}, &r)
	return r
}
func author(e *cmstest.Env) auth.Actor {
	return e.Human("owner", auth.SchemaApply, auth.DesignCompose, auth.ContentPublish, auth.SchemaRead, auth.DesignRead)
}
func claim(e *cmstest.Env, actor auth.Actor, id uuid.UUID) store.Changeset {
	cs, err := e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: e.Admin.ProjectID, ID: id})
	if err != nil {
		e.T.Fatal(err)
	}
	e.Must(actor, "claim-schema-changeset", map[string]any{"changesetId": id, "expectedSeq": cs.Seq}, &cs)
	return cs
}
func review(e *cmstest.Env, owner auth.Actor, id uuid.UUID) workflow.Review {
	cs, err := e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: e.Admin.ProjectID, ID: id})
	if err != nil {
		e.T.Fatal(err)
	}
	targets := cs.Targets
	if len(targets) == 0 {
		targets = []string{"staging"}
	}
	var r workflow.Review
	e.Must(owner, "submit-changeset", map[string]any{"changesetId": id, "expectedSeq": cs.Seq, "targets": targets}, &r)
	return r
}
func approve(e *cmstest.Env, id uuid.UUID) auth.Actor {
	a := e.Human("reviewer", auth.ContentPublish)
	e.Must(a, "approve-changeset", map[string]any{"changesetId": id}, nil)
	return a
}
func publish(e *cmstest.Env, owner auth.Actor, id uuid.UUID) publishing.Publication {
	var p publishing.Publication
	e.Must(owner, "publish", map[string]any{"changesetId": id, "environment": "staging"}, &p)
	return p
}
func detail(e *cmstest.Env, id uuid.UUID) changes.ChangesetDetail {
	d, err := changes.GetChangeset(context.Background(), e.Q, e.Admin.ProjectID, id)
	if err != nil {
		e.T.Fatal(err)
	}
	return d
}
func count(e *cmstest.Env, table string) int {
	var n int
	if err := e.Pool.QueryRow(context.Background(), "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		e.T.Fatal(err)
	}
	return n
}
func active(e *cmstest.Env) *uuid.UUID {
	env, err := e.Q.GetManifestEnvironment(context.Background(), store.GetManifestEnvironmentParams{ProjectID: e.Admin.ProjectID, Name: "staging"})
	if err != nil {
		e.T.Fatal(err)
	}
	return env.ActiveManifestID
}

// CNT-021, MF-003/027, PUB-020/033: first activation, audit, content and rollback form one transaction.
func TestFirstActivationAndJointRollback(t *testing.T) {
	e := setup(t)
	e.Exec("UPDATE environments SET active_manifest_id=NULL WHERE name='staging'")
	r := register(e, manifest("1", "text"), "staging")
	id := *r.ChangesetID
	owner := author(e)
	cs := claim(e, owner, id)
	if cs.Seq != 1 || cs.OwnerID != owner.ID || count(e, "schema_changeset_claims") != 1 {
		t.Fatal(cs)
	}
	ops, err := changes.ListOperations(context.Background(), e.Q, e.Admin.ProjectID, id, 0)
	if err != nil || len(ops) != 1 || ops[0].SchemaTarget == nil || ops[0].SchemaTarget.SchemaName != "Product" || ops[0].Target != uuid.Nil {
		t.Fatal(ops, err)
	}
	encoded, _ := json.Marshal(ops[0])
	var decoded changes.Operation
	if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.SchemaTarget == nil {
		t.Fatal(decoded, err)
	}
	objectJSON := []byte(`{"target":"` + uuid.NewString() + `"}`)
	if err := json.Unmarshal(objectJSON, &decoded); err != nil || decoded.SchemaTarget != nil || decoded.Target == uuid.Nil {
		t.Fatal(decoded, err)
	}
	if err := json.Unmarshal([]byte(`{"target":false}`), &decoded); err == nil {
		t.Fatal("invalid operation target accepted")
	}
	if err := json.Unmarshal([]byte(`[`), &decoded); err == nil {
		t.Fatal("invalid operation JSON accepted")
	}
	if !strings.Contains(string(encoded), `"target":{"kind":"schema","schemaName":"Product"}`) {
		t.Fatal(string(encoded))
	}
	var result changes.ApplyResult
	e.Must(owner, "apply-operations", map[string]any{"changesetId": id, "expectedSeq": 1, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": "/schema", "root": map[string]any{"id": "n_root", "type": "Box"}}}}}, &result)
	doc := result.Operations[0].Target
	rv := review(e, owner, id)
	if rv.Changeset.State != "in_review" || rv.Risk == nil || *rv.Risk != "high" {
		t.Fatal(rv)
	}
	if active(e) != nil || count(e, "schema_versions") != 0 {
		t.Fatal("activated before agreement")
	}
	approve(e, id)
	p := publish(e, owner, id)
	if !p.ManifestChanged || p.PreviousManifestID != nil || p.CurrentManifestID == nil || active(e) == nil || *active(e) != *p.CurrentManifestID || count(e, "schema_versions") != 1 || count(e, "published_pointers") != 1 {
		t.Fatal(p)
	}
	historical, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, id)
	if err != nil || historical.Changeset.State != "merged" || len(historical.History) != 1 {
		t.Fatal(historical, err)
	}
	if detail(e, id).NeedsAttention {
		t.Fatal("merged schema CS requires attention")
	}
	stored, err := publishing.GetPublication(context.Background(), e.Q, e.Admin.ProjectID, p.ID)
	if err != nil || !stored.ManifestChanged {
		t.Fatal(stored, err)
	}
	list, err := publishing.ListPublications(context.Background(), e.Q, e.Admin.ProjectID, nil)
	if err != nil || !list[0].ManifestChanged {
		t.Fatal(list, err)
	}
	var back publishing.Publication
	e.Must(owner, "rollback", map[string]any{"publicationId": p.ID}, &back)
	if _, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, id); err != nil {
		t.Fatal(err)
	}
	if !back.ManifestChanged || back.CurrentManifestID != nil || active(e) != nil || count(e, "published_pointers") != 0 || count(e, "schema_versions") != 1 {
		t.Fatal(back)
	}
	o, err := e.Q.GetObject(context.Background(), store.GetObjectParams{ProjectID: e.Admin.ProjectID, ID: doc})
	if err != nil || o.HeadVersionID != nil {
		t.Fatal(o, err)
	}
	if count(e, "object_versions") != 1 || count(e, "river_job") != 2 {
		t.Fatal("rollback created versions or lost outbox")
	}
	// Rollback itself can be reversed without deleting immutable schemas or making versions.
	e.Must(owner, "rollback", map[string]any{"publicationId": back.ID}, nil)
	if active(e) == nil || count(e, "object_versions") != 1 {
		t.Fatal("rollback reversal")
	}
}

// CHG-010/011, CNT-021: explicit schema targets bind to server-computed candidate bodies.
func TestSchemaOperationsAndClaimBoundaries(t *testing.T) {
	e := setup(t)
	r := register(e, manifest("1", "text"), "staging")
	id := *r.ChangesetID
	if cmstest.Code(e.Do(e.Admin, "claim-schema-changeset", map[string]any{"changesetId": id, "expectedSeq": 1}, nil)) != "SCHEMA_CLAIM_HUMAN_REQUIRED" {
		t.Fatal("service claimed human ownership")
	}
	noRight := e.Human("without-right", auth.ContentPublish)
	if cmstest.Code(e.Do(noRight, "claim-schema-changeset", map[string]any{"changesetId": id, "expectedSeq": 1}, nil)) != "FORBIDDEN" {
		t.Fatal("claim bypassed schema.apply")
	}
	owner := author(e)
	if cmstest.Code(e.Do(owner, "claim-schema-changeset", map[string]any{"changesetId": id, "expectedSeq": 0}, nil)) != "CHANGESET_SEQ_CONFLICT" {
		t.Fatal("stale claim")
	}
	if cmstest.Code(e.Do(owner, "claim-schema-changeset", map[string]any{"changesetId": uuid.New(), "expectedSeq": 0}, nil)) != "NOT_FOUND" {
		t.Fatal("unknown claim")
	}
	claim(e, owner, id)
	claim(e, owner, id)
	another := e.Human("another", auth.SchemaApply)
	if cmstest.Code(e.Do(another, "claim-schema-changeset", map[string]any{"changesetId": id, "expectedSeq": 1}, nil)) != "CHANGESET_NOT_OWNER" {
		t.Fatal("stole human ownership")
	}
	target := map[string]any{"kind": "schema", "schemaName": "Product"}
	for _, bad := range []map[string]any{
		{"type": "schema.apply", "target": uuid.New(), "payload": map[string]any{"schema": "Product", "version": 1}},
		{"type": "schema.apply", "target": target, "payload": map[string]any{"schema": "Other", "version": 1}},
		{"type": "schema.apply", "target": target, "payload": map[string]any{"schema": "Product", "version": 2}},
		{"type": "schema.apply", "target": target, "payload": map[string]any{"schema": "Product", "version": 1, "body": map[string]any{}}},
		{"type": "node.rename", "target": target, "payload": map[string]any{}},
	} {
		if err := e.Do(owner, "apply-operations", map[string]any{"changesetId": id, "expectedSeq": 1, "operations": []any{bad}}, nil); err == nil {
			t.Fatal(bad)
		}
	}
	var result changes.ApplyResult
	e.Must(owner, "apply-operations", map[string]any{"changesetId": id, "expectedSeq": 1, "operations": []any{map[string]any{"type": "schema.apply", "target": target, "payload": map[string]any{"schema": "Product", "version": 1}}}}, &result)
	if result.Changeset.Seq != 2 || result.Operations[0].SchemaTarget == nil || count(e, "schema_versions") != 0 {
		t.Fatal(result)
	}
	if err := schemaflow.Verify(context.Background(), e.Q, e.Admin.ProjectID, id); err != nil {
		t.Fatal(err)
	}
	if cmstest.Code(e.Do(owner, "undo", map[string]any{"changesetId": id, "expectedSeq": 2}, nil)) != "UNDO_NOT_SUPPORTED" {
		t.Fatal("candidate journal unexpectedly undoable")
	}
}

// MF-027, PUB-004/020: stale base, revoked permission and stale agreement reject without writes.
func TestPublicationRechecks(t *testing.T) {
	for _, mode := range []string{"base", "owner-right", "publisher-right", "reviewer-right", "policy"} {
		t.Run(mode, func(t *testing.T) {
			e := setup(t)
			r := register(e, manifest("1", "text"), "staging")
			id := *r.ChangesetID
			owner := author(e)
			claim(e, owner, id)
			review(e, owner, id)
			reviewer := approve(e, id)
			code := "FORBIDDEN"
			switch mode {
			case "base":
				register(e, strings.Replace(cmstest.DefaultManifest, "1.0.0", "1.0.1", 1), "staging")
				code = "MANIFEST_BASE_CHANGED"
			case "owner-right":
				e.Exec("UPDATE roles SET capabilities=ARRAY['content.publish'] WHERE name='role-owner'")
			case "publisher-right":
				owner.Rights = auth.RightSet{auth.ContentPublish: true}
			case "reviewer-right":
				e.Exec("UPDATE roles SET capabilities='{}' WHERE name='role-reviewer'")
				_ = reviewer
				code = "APPROVAL_ROLES_MISSING"
			case "policy":
				e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 2}, nil)
				code = "APPROVAL_ROLES_MISSING"
			}
			before := active(e)
			err := e.Do(owner, "publish", map[string]any{"changesetId": id, "environment": "staging"}, nil)
			if cmstest.Code(err) != code || count(e, "schema_versions") != 0 || count(e, "publications") != 0 || count(e, "river_job") != 0 || *active(e) != *before || detail(e, id).State != "approved" {
				t.Fatal(err)
			}
		})
	}
}

// MF-027/PUB-033: normal activation supersedes schema rollback; schemas are environment-specific.
func TestSupersededRollbackAndSchemaRemoval(t *testing.T) {
	e := setup(t)
	r := register(e, manifest("1", "text"), "staging")
	owner := author(e)
	claim(e, owner, *r.ChangesetID)
	review(e, owner, *r.ChangesetID)
	approve(e, *r.ChangesetID)
	p := publish(e, owner, *r.ChangesetID)
	if cmstest.Code(e.Do(owner, "promote", map[string]any{"publicationId": p.ID, "toEnvironment": "production"}, nil)) != "PROMOTE_SCHEMA_NOT_SUPPORTED" {
		t.Fatal("environment binding bypassed")
	}
	changed := strings.Replace(manifest("1", "text"), "1.0.0", "1.0.1", 1)
	register(e, changed, "staging")
	if cmstest.Code(e.Do(owner, "rollback", map[string]any{"publicationId": p.ID}, nil)) != "ROLLBACK_SUPERSEDED" {
		t.Fatal("superseded manifest rolled back")
	}
	removed := register(e, cmstest.DefaultManifest, "staging")
	claim(e, owner, *removed.ChangesetID)
	review(e, owner, *removed.ChangesetID)
	reviewer := e.Human("reviewer-2", auth.ContentPublish)
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": *removed.ChangesetID}, nil)
	removal := publish(e, owner, *removed.ChangesetID)
	if removal.PreviousManifestID == nil || removal.CurrentManifestID == nil || count(e, "schema_versions") != 1 {
		t.Fatal(removal)
	}
	e.Must(owner, "rollback", map[string]any{"publicationId": removal.ID}, nil)
	if *active(e) != *removal.PreviousManifestID {
		t.Fatal("schema removal rollback")
	}
}

// MF-027/CNT-002: versions are rechecked at publication even across distinct environments.
func TestConcurrentSchemaVersionConflict(t *testing.T) {
	e := setup(t)
	owner := author(e)
	r := register(e, manifest("1", "text"), "staging")
	claim(e, owner, *r.ChangesetID)
	review(e, owner, *r.ChangesetID)
	reviewer := approve(e, *r.ChangesetID)
	other := register(e, manifest("1", "number"), "production")
	claim(e, owner, *other.ChangesetID)
	review(e, owner, *other.ChangesetID)
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": *other.ChangesetID}, nil)
	e.Must(owner, "publish", map[string]any{"changesetId": *other.ChangesetID, "environment": "production"}, nil)
	if cmstest.Code(e.Do(owner, "publish", map[string]any{"changesetId": *r.ChangesetID, "environment": "staging"}, nil)) != "SCHEMA_VERSION_CONFLICT" || count(e, "publications") != 1 {
		t.Fatal("conflicting schema version published")
	}
}

// PUB-020/033: every late-write failure rolls back manifest, schema registry, CS, pointers and outbox.
func TestAtomicWriteFailures(t *testing.T) {
	for _, table := range []string{"schema_versions", "publications", "environments", "published_pointers", "changesets", "river_job", "changeset_manifest_diagnostics"} {
		t.Run(table, func(t *testing.T) {
			e := setup(t)
			r := register(e, manifest("1", "text"), "staging")
			owner := author(e)
			claim(e, owner, *r.ChangesetID)
			cs := detail(e, *r.ChangesetID)
			e.Must(owner, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": cs.Seq, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "root": map[string]any{"id": "n_root", "type": "Box"}}}}}, nil)
			review(e, owner, cs.ID)
			approve(e, cs.ID)
			before := *active(e)
			op := "INSERT"
			if table == "environments" || table == "changesets" {
				op = "UPDATE"
			}
			e.Break(table, op)
			cmstest.ExpectDBError(t, e.Do(owner, "publish", map[string]any{"changesetId": cs.ID, "environment": "staging"}, nil))
			if *active(e) != before || count(e, "schema_versions") != 0 || count(e, "publications") != 0 || count(e, "published_pointers") != 0 || count(e, "river_job") != 0 {
				t.Fatal("partial publication")
			}
		})
	}
	for _, table := range []string{"environments", "publications", "river_job"} {
		t.Run("rollback-"+table, func(t *testing.T) {
			e := setup(t)
			r := register(e, manifest("1", "text"), "staging")
			owner := author(e)
			claim(e, owner, *r.ChangesetID)
			review(e, owner, *r.ChangesetID)
			approve(e, *r.ChangesetID)
			p := publish(e, owner, *r.ChangesetID)
			before := *active(e)
			op := "INSERT"
			if table == "environments" {
				op = "UPDATE"
			}
			e.Break(table, op)
			cmstest.ExpectDBError(t, e.Do(owner, "rollback", map[string]any{"publicationId": p.ID}, nil))
			if *active(e) != before || count(e, "publications") != 1 || count(e, "river_job") != 1 {
				t.Fatal("partial rollback")
			}
		})
	}
}

// CNT-022/MF-027/PUB-033: broken published bindings must be repaired under the candidate in the same CS.
func TestSchemaAndBindingRepairTogether(t *testing.T) {
	e := setup(t)
	owner := e.Human("repair", auth.SchemaApply, auth.DesignCompose, auth.ContentWrite, auth.ContentPublish, auth.SchemaRead, auth.DesignRead)
	r := register(e, manifest("1", "text"), "staging")
	claim(e, owner, *r.ChangesetID)
	review(e, owner, *r.ChangesetID)
	a := approve(e, *r.ChangesetID)
	publish(e, owner, *r.ChangesetID)
	var cs changes.Changeset
	e.Must(owner, "create-changeset", map[string]any{"title": "binding"}, &cs)
	var entity changes.ApplyResult
	e.Must(owner, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{map[string]any{"type": "entity.create", "payload": map[string]any{"schema": "Product", "environment": "staging", "data": map[string]any{"title": "Product"}}}}}, &entity)
	entityID := entity.Operations[0].Target
	var created changes.ApplyResult
	e.Must(owner, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "operations": []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "content": map[string]any{"schema": "Product", "resolve": map[string]any{"by": "entity", "entityId": entityID.String()}}, "root": map[string]any{"id": "n_root", "type": "Text", "bindings": map[string]any{"text": "$content.title"}}}}}}, &created)
	initialReview := review(e, owner, cs.ID)
	if initialReview.Changeset.State != "approved" {
		raw, _ := json.Marshal(initialReview.Checks)
		t.Fatal(string(raw))
	}
	publish(e, owner, cs.ID)
	previous := *active(e)
	r = register(e, strings.Replace(manifest("2", "text"), `"title"`, `"name"`, 1), "staging")
	claim(e, owner, *r.ChangesetID)
	rv := review(e, owner, *r.ChangesetID)
	if rv.Changeset.State != "failed" || *active(e) != previous {
		t.Fatal(rv)
	}
	e.Must(owner, "reopen-changeset", map[string]any{"changesetId": *r.ChangesetID}, nil)
	e.Must(owner, "apply-operations", map[string]any{"changesetId": *r.ChangesetID, "expectedSeq": 1, "operations": []any{
		map[string]any{"type": "node.setBinding", "target": created.Operations[0].Target, "payload": map[string]any{"nodeId": "n_root", "prop": "text", "binding": "$content.name"}},
		map[string]any{"type": "node.rename", "target": created.Operations[0].Target, "payload": map[string]any{"nodeId": "n_root", "name": "fixed"}},
		map[string]any{"type": "entity.setFields", "target": entityID, "payload": map[string]any{"set": map[string]any{"name": "Product"}, "unset": []string{"title"}}},
	}}, nil)
	fixed := review(e, owner, *r.ChangesetID)
	if fixed.Changeset.State != "in_review" {
		raw, _ := json.Marshal(fixed.Checks)
		t.Fatal(string(raw))
	}
	e.Must(a, "approve-changeset", map[string]any{"changesetId": *r.ChangesetID}, nil)
	p := publish(e, owner, *r.ChangesetID)
	if *active(e) == previous || len(p.Items) != 2 || count(e, "schema_versions") != 2 {
		t.Fatal(p)
	}
	if _, err := changes.GetContent(context.Background(), e.Q, e.Admin.ProjectID, entityID, "staging", r.ChangesetID); err != nil {
		t.Fatal("merged content history", err)
	}
	e.Must(owner, "rollback", map[string]any{"publicationId": p.ID}, nil)
	if _, err := changes.GetContent(context.Background(), e.Q, e.Admin.ProjectID, entityID, "staging", r.ChangesetID); err != nil {
		t.Fatal("rolled back content history", err)
	}
	if *active(e) != previous || count(e, "schema_versions") != 2 {
		t.Fatal("joint rollback failed")
	}
}

// CNT-021: previously registered empty candidates acquire their schema journal on human claim.
func TestLegacyClaimAndInvalidPlans(t *testing.T) {
	e := setup(t)
	r := register(e, manifest("1", "text"), "staging")
	id := *r.ChangesetID
	owner := author(e)
	e.Exec("DELETE FROM operations WHERE changeset_id=$1", id)
	e.Exec("UPDATE changesets SET seq=0 WHERE id=$1", id)
	cs := claim(e, owner, id)
	if cs.Seq != 1 || count(e, "operations") != 1 {
		t.Fatal(cs)
	}
	for _, sql := range []string{`UPDATE operations SET payload='{"schema":"Missing","version":1}' WHERE changeset_id=$1`, `UPDATE operations SET payload='{"schema":"Product","version":2}' WHERE changeset_id=$1`, `UPDATE operations SET payload='[]' WHERE changeset_id=$1`, `UPDATE operations SET status='dropped' WHERE changeset_id=$1`} {
		e.Exec(sql, id)
		if cmstest.Code(schemaflow.Verify(context.Background(), e.Q, e.Admin.ProjectID, id)) != "SCHEMA_PLAN_MISMATCH" {
			t.Fatal("corrupt plan accepted")
		}
	}
	if _, _, err := schemaflow.Plan(context.Background(), e.Q, e.Admin.ProjectID, uuid.New()); cmstest.Code(err) != "MANIFEST_CANDIDATE_REQUIRED" {
		t.Fatal(err)
	}
	ordinary, _ := e.Draft(owner, "ordinary")
	if cmstest.Code(e.Do(owner, "claim-schema-changeset", map[string]any{"changesetId": ordinary, "expectedSeq": 1}, nil)) != "CHANGESET_STATE_INVALID" {
		t.Fatal("ordinary claimed")
	}
}

// CHG-011/MF-027: storage failures are surfaced without partial journal/ownership changes.
func TestSchemaReadAndPreparationFailures(t *testing.T) {
	for _, table := range []string{"operations", "changesets", "environments", "changeset_manifest_candidates", "manifests", "actors", "schema_changeset_claims"} {
		t.Run("claim-"+table, func(t *testing.T) {
			e := setup(t)
			r := register(e, manifest("1", "text"), "staging")
			owner := author(e)
			e.Break(table, "")
			cmstest.ExpectDBError(t, e.Do(owner, "claim-schema-changeset", map[string]any{"changesetId": *r.ChangesetID, "expectedSeq": 1}, nil))
		})
	}
	for _, table := range []string{"operations", "changeset_manifest_candidates", "manifests"} {
		t.Run("verify-"+table, func(t *testing.T) {
			e := setup(t)
			r := register(e, manifest("1", "text"), "staging")
			e.Break(table, "")
			cmstest.ExpectDBError(t, schemaflow.Verify(context.Background(), e.Q, e.Admin.ProjectID, *r.ChangesetID))
		})
	}
	for _, tc := range []struct{ table, op string }{{"operations", "INSERT"}, {"changesets", "UPDATE"}} {
		t.Run("prepare-"+tc.table, func(t *testing.T) {
			e := setup(t)
			before := count(e, "manifests")
			e.Break(tc.table, tc.op)
			cmstest.ExpectDBError(t, e.Do(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: json.RawMessage(manifest("1", "text")), Environment: "staging"}, nil))
			if count(e, "manifests") != before || count(e, "changeset_manifest_candidates") != 0 {
				t.Fatal("partial candidate")
			}
		})
	}
}

// PUB-033: rolling back a contract must not strand subsequently published documents.
func TestRollbackContractConsumers(t *testing.T) {
	e := setup(t)
	e.Exec("UPDATE environments SET active_manifest_id=NULL WHERE name='staging'")
	r := register(e, manifest("1", "text"), "staging")
	owner := author(e)
	claim(e, owner, *r.ChangesetID)
	review(e, owner, *r.ChangesetID)
	approve(e, *r.ChangesetID)
	p := publish(e, owner, *r.ChangesetID)
	cs, _ := e.Draft(owner, "later")
	review(e, owner, cs)
	publish(e, owner, cs)
	before := *active(e)
	if cmstest.Code(e.Do(owner, "rollback", map[string]any{"publicationId": p.ID}, nil)) != "VALIDATION_FAILED" || *active(e) != before || count(e, "publications") != 2 || count(e, "published_pointers") != 1 {
		t.Fatal("rollback stranded published content")
	}
}

// CNT-002: identical immutable versions can be activated in another standard environment.
func TestKnownVersionReuseAndClaimFailureRollback(t *testing.T) {
	e := setup(t)
	r := register(e, manifest("1", "text"), "staging")
	owner := author(e)
	claim(e, owner, *r.ChangesetID)
	review(e, owner, *r.ChangesetID)
	a := approve(e, *r.ChangesetID)
	publish(e, owner, *r.ChangesetID)
	other := register(e, manifest("1", "text"), "production")
	claim(e, owner, *other.ChangesetID)
	review(e, owner, *other.ChangesetID)
	e.Must(a, "approve-changeset", map[string]any{"changesetId": *other.ChangesetID}, nil)
	e.Must(owner, "publish", map[string]any{"changesetId": *other.ChangesetID, "environment": "production"}, nil)
	if count(e, "schema_versions") != 1 {
		t.Fatal("immutable version duplicated")
	}
	candidate := register(e, manifest("2", "text"), "staging")
	e.Break("changesets", "UPDATE")
	cmstest.ExpectDBError(t, e.Do(owner, "claim-schema-changeset", map[string]any{"changesetId": *candidate.ChangesetID, "expectedSeq": 1}, nil))
	if count(e, "schema_changeset_claims") != 2 || detail(e, *candidate.ChangesetID).OwnerID != e.Admin.ID {
		t.Fatal("claim audit committed without owner")
	}
}

// CHG-010/PUB-033: a schema downgrade cannot erase the new journal and publication audit.
func TestMigrationDownPreservesSchemaAudit(t *testing.T) {
	e := setup(t)
	r := register(e, manifest("1", "text"), "staging")
	if err := postgres.Migrate(context.Background(), e.Pool, "down", slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	} // remove the empty asset upload migration first
	if err := postgres.Migrate(context.Background(), e.Pool, "down", slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	} // remove the empty content migration first
	err := postgres.Migrate(context.Background(), e.Pool, "down", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err == nil || !strings.Contains(err.Error(), "schema publication data prevents downgrade") {
		t.Fatal(err)
	}
	if count(e, "operations") != 1 {
		t.Fatal("migration removed journal")
	}
	if err := schemaflow.Verify(context.Background(), e.Q, e.Admin.ProjectID, *r.ChangesetID); err != nil {
		t.Fatal(err)
	}
}
