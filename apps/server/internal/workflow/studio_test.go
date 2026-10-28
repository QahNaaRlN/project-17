package workflow_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// STU-070/072: actions reflect state, actual actor rights, authorship and human identity.
// PUB-005: the reviewer sees diff, checks, reason and source.
func TestStudioReviewActionsAndObjects(t *testing.T) {
	e := setup(t)
	cs, doc := e.Draft(e.Admin, "v1")
	get := func(actor auth.Actor) workflow.StudioReview {
		t.Helper()
		r, err := workflow.GetStudioReview(context.Background(), e.Q, actor, cs)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := get(e.Admin)
	if !r.CanEditBehavior || !r.CanEditCondition {
		t.Fatal("missing operation flags", r)
	}
	behaviorOnly := e.Admin
	behaviorOnly.Rights = auth.RightSet{auth.DesignRead: true, auth.BehaviorUse: true}
	if r := get(behaviorOnly); r.CanEdit || !r.CanEditBehavior || r.CanEditCondition {
		t.Fatal("behavior-only flags", r)
	}
	if r.Actions["submit"] != nil || r.Actions["publish"] == nil || len(r.Objects) != 1 || r.Objects[0].ID != doc || r.Objects[0].Kind != "page" {
		t.Fatal(r)
	}
	raw, _ := json.Marshal(r.Objects)
	if strings.Contains(string(raw), "nodes") || strings.Contains(string(raw), "v1") {
		t.Fatal(string(raw))
	}
	outsider := e.Human("outsider", auth.ContentPublish)
	if r := get(outsider); r.CanEditBehavior || r.CanEditCondition {
		t.Fatal("non-owner flags", r)
	}
	if reason := get(outsider).Actions["submit"]; reason == nil || *reason != "CHANGESET_NOT_OWNER" {
		t.Fatal(reason)
	}
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)
	if r := get(e.Admin); r.CanEditBehavior || r.CanEditCondition {
		t.Fatal("submitted edit flags", r)
	}
	if reason := get(e.Admin).Actions["approve"]; reason == nil || *reason != "APPROVAL_FORBIDDEN_ACTOR" {
		t.Fatal(reason)
	}
	r = get(outsider)
	if r.Actions["approve"] != nil || r.Actions["request-changes"] != nil {
		t.Fatal(r)
	}
	denied := e.Human("denied", auth.ContentRead)
	if reason := get(denied).Actions["approve"]; reason == nil || *reason != "FORBIDDEN" {
		t.Fatal(reason)
	}
	humanOwner := e.Admin
	humanOwner.Kind = auth.ActorHuman
	if reason := get(humanOwner).Actions["approve"]; reason == nil || *reason != "APPROVAL_SELF" {
		t.Fatal(reason)
	}
	e.Must(outsider, "request-changes", map[string]any{"changesetId": cs, "expectedSeq": 1, "comment": "revise"}, nil)
	r = get(e.Admin)
	if r.Actions["reopen"] != nil || len(r.History) != 1 || r.History[0].Comment == nil {
		t.Fatal(r)
	}
	e.Must(e.Admin, "reopen-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)
	if r = get(e.Admin); r.Changeset.State != "open" || r.History[0].Valid {
		t.Fatal(r)
	}
	_, err := workflow.GetStudioReview(context.Background(), e.Q, e.Admin, uuid.New())
	if cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
}

// STU-255: capability entry points require current owner rights and an open CS.
func TestStudioCapabilityPolicyRequiresCurrentOwnerRights(t *testing.T) {
	e := setup(t)
	owner := e.Human("capability owner", auth.ContentWrite, auth.DesignCompose, auth.DesignRead, auth.CapabilityRequest)
	cs, _ := e.Draft(owner, "v1")
	read := func(actor auth.Actor) workflow.StudioReview {
		t.Helper()
		view, err := workflow.GetStudioReview(context.Background(), e.Q, actor, cs)
		if err != nil {
			t.Fatal(err)
		}
		return view
	}
	if p := read(owner).CapabilityPolicy; p["defaultMode"] != "SYSTEM" || p["maxMode"] != "SYSTEM" {
		t.Fatal(p)
	}
	e.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"DESIGN","maxMode":"CODE"}' WHERE id=$1`, owner.ProjectID)
	if p := read(owner).CapabilityPolicy; p["defaultMode"] != "DESIGN" || p["maxMode"] != "CODE" {
		t.Fatal(p)
	}
	revoked := owner
	revoked.Rights = auth.RightSet{auth.DesignRead: true}
	if read(revoked).CapabilityPolicy != nil {
		t.Fatal("revoked capability right exposed policy")
	}
	outsider := e.Human("capability outsider", auth.DesignRead, auth.CapabilityRequest)
	if read(outsider).CapabilityPolicy != nil {
		t.Fatal("another owner's policy exposed")
	}
	e.Exec(`UPDATE changesets SET state='in_review' WHERE id=$1`, cs)
	if read(owner).CapabilityPolicy != nil {
		t.Fatal("closed editor exposed capability controls")
	}
}

// STU-074: decisions based on an old editor seq cannot mutate the review or approvals.
func TestStudioReviewRejectsStaleDecisionSeq(t *testing.T) {
	e := setup(t)
	cs, _ := e.Draft(e.Admin, "v1")
	reviewer := e.Human("reviewer", auth.ContentPublish)
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)
	for _, action := range []string{"approve-changeset", "request-changes", "reopen-changeset"} {
		actor := reviewer
		if action == "reopen-changeset" {
			actor = e.Admin
		}
		payload := map[string]any{"changesetId": cs, "expectedSeq": 0}
		if action != "reopen-changeset" {
			payload["comment"] = "revise"
		}
		if err := e.Do(actor, action, payload, nil); cmstest.Code(err) != "CHANGESET_SEQ_CONFLICT" {
			t.Fatal(action, err)
		}
	}
	r, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, cs)
	if err != nil || r.Changeset.State != "in_review" || len(r.History) != 0 {
		t.Fatal(r, err)
	}
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)
}

// STU-070/071: empty, conflicting and non-reviewable work cannot offer a submission.
func TestStudioReviewUnavailableSubmission(t *testing.T) {
	e := setup(t)
	var cs changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "empty", "targets": []string{"staging"}}, &cs)
	read := func() workflow.StudioReview {
		r, err := workflow.GetStudioReview(context.Background(), e.Q, e.Admin, cs.ID)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := read(); r.Actions["submit"] == nil || *r.Actions["submit"] != "CHANGESET_EMPTY" {
		t.Fatal(r)
	}
	e.Exec("UPDATE changesets SET has_conflicts=true WHERE id=$1", cs.ID)
	if r := read(); r.Actions["submit"] == nil || *r.Actions["submit"] != "CHANGESET_HAS_CONFLICTS" {
		t.Fatal(r)
	}
	e.Exec("UPDATE changesets SET state='approved', has_conflicts=false WHERE id=$1", cs.ID)
	if r := read(); r.Actions["publish"] != nil || r.Actions["reopen"] != nil {
		t.Fatal(r)
	}
}

// STU-070: a read-only snapshot neither mixes a concurrent submission nor blocks it.
func TestStudioReviewSnapshotDuringSubmission(t *testing.T) {
	e := setup(t)
	cs, _ := e.Draft(e.Admin, "v1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tx, err := e.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := store.New(tx)
	before, err := workflow.GetStudioReview(ctx, q, e.Admin, cs)
	if err != nil || before.Changeset.State != "open" {
		t.Fatal(before, err)
	}
	if _, err := submit(e, cs, 1); err != nil {
		t.Fatal(err)
	}
	after, err := workflow.GetStudioReview(ctx, q, e.Admin, cs)
	if err != nil || after.Changeset.State != "open" || len(after.Checks) != 0 {
		t.Fatal(after, err)
	}
	latest, err := workflow.GetStudioReview(ctx, e.Q, e.Admin, cs)
	if err != nil || latest.Changeset.State != "in_review" || len(latest.Checks) != 2 {
		t.Fatal(latest, err)
	}
}
