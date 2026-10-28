package workflow_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// renderSetup enables stages 8–9 with application addresses and returns the runner.
func renderSetup(t *testing.T) (*cmstest.Env, auth.Actor) {
	t.Helper()
	e := cmstest.New(t, workflow.Register, publishing.Register)
	e.Exec(`UPDATE project_render_checks SET enabled = true WHERE project_id = $1`, e.Admin.ProjectID)
	for _, env := range []string{"staging", "production"} {
		e.Must(e.Admin, "set-environment-app-url", map[string]any{"environment": env, "appUrl": "http://127.0.0.1:18099"}, nil)
	}
	res, err := projects.CheckRunnerToken(context.Background(), e.Pool, "store", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	runner, err := auth.Authenticate(context.Background(), e.Q, "Bearer "+res.Token)
	if err != nil {
		t.Fatal(err)
	}
	// The same actor is reused by a second token.
	again, err := projects.CheckRunnerToken(context.Background(), e.Pool, "store", time.Hour)
	if err != nil || again.ActorID != res.ActorID {
		t.Fatal(again, err)
	}
	return e, runner
}

// routedPage creates a Change Set with one routed page and returns it with its seq.
func routedPage(e *cmstest.Env, path string) (cs, doc uuid.UUID) {
	e.T.Helper()
	var c changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": path, "targets": []string{"staging"}}, &c)
	var res changes.ApplyResult
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": c.ID, "expectedSeq": 0, "operations": []any{
		map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": path, "root": map[string]any{"id": "n_root", "type": "Box"}}},
	}}, &res)
	return c.ID, res.Operations[0].Target
}

func claim(t *testing.T, e *cmstest.Env, runner auth.Actor) *workflow.LeasedCheck {
	t.Helper()
	var out workflow.ClaimedCheck
	e.Must(runner, "claim-check", map[string]any{}, &out)
	return out.Check
}

func complete(e *cmstest.Env, runner auth.Actor, c *workflow.LeasedCheck, status string) (workflow.CompletedCheck, error) {
	var out workflow.CompletedCheck
	err := e.Do(runner, "complete-check", map[string]any{"checkId": c.ID, "leaseId": c.LeaseID, "status": status,
		"details": map[string]any{"pages": []any{map[string]any{"objectId": c.Pages[0].ObjectID, "path": c.Pages[0].Path, "status": 200}}}}, &out)
	return out, err
}

func changesetState(t *testing.T, e *cmstest.Env, cs uuid.UUID) store.Changeset {
	t.Helper()
	c, err := e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ID: cs, ProjectID: e.Admin.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// STU-480, 483, 485, 486, 484: queue, lease, complete, review and publish.
func TestRenderChecksLeaseCompleteAndPublish(t *testing.T) {
	e, runner := renderSetup(t)
	cs, doc := routedPage(e, "/render")
	r, err := submit(e, cs, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Changeset.State != "checking" || len(r.Checks) != 6 {
		t.Fatalf("%+v", r)
	}
	for _, c := range r.Checks[2:4] {
		if c.Environment != "staging" || c.Status != "queued" || !c.Blocking {
			t.Fatalf("%+v", c)
		}
	}
	if err := workflow.RequireRenderChecks(context.Background(), e.Q, changesetState(t, e, cs), "staging"); cmstest.Code(err) != "CHECKS_PENDING" {
		t.Fatal(err)
	}
	if err := e.Do(e.Admin, "claim-check", map[string]any{}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	first := claim(t, e, runner)
	if first == nil || first.Stage != workflow.StagePreview || first.AppURL != "http://127.0.0.1:18099" || first.PreviewToken == "" ||
		len(first.Pages) != 1 || first.Pages[0].ObjectID != doc || first.Pages[0].Path != "/render" || first.ChangesetID != cs {
		t.Fatalf("%+v", first)
	}
	if _, err := complete(e, runner, &workflow.LeasedCheck{ID: first.ID, LeaseID: uuid.New(), Pages: first.Pages}, "passed"); cmstest.Code(err) != "CHECK_LEASE_LOST" {
		t.Fatal(err)
	}
	if _, err := complete(e, runner, first, "skipped"); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatal(err)
	}
	if err := e.Do(runner, "complete-check", map[string]any{"checkId": first.ID, "leaseId": first.LeaseID, "status": "passed", "details": map[string]any{}}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatal(err)
	}
	if err := e.Do(runner, "complete-check", map[string]any{"checkId": uuid.New(), "leaseId": first.LeaseID, "status": "passed", "details": map[string]any{"pages": []any{}}}, nil); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	done, err := complete(e, runner, first, "passed")
	if err != nil || done.ChangesetState != "checking" {
		t.Fatal(done, err)
	}
	if _, err := complete(e, runner, first, "passed"); cmstest.Code(err) != "CHECK_LEASE_LOST" {
		t.Fatal(err)
	}
	second := claim(t, e, runner)
	if second == nil || second.Stage != workflow.StageAxe {
		t.Fatalf("%+v", second)
	}
	if done, err = complete(e, runner, second, "warning"); err != nil || done.ChangesetState != "in_review" {
		t.Fatal(done, err)
	}
	if empty := claim(t, e, runner); empty != nil {
		t.Fatalf("%+v", empty)
	}
	detail, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, cs)
	if err != nil || len(detail.Checks) != 6 {
		t.Fatal(detail, err)
	}
	reviewer := e.Human("Reviewer", auth.ContentPublish, auth.DesignRead, auth.ContentRead)
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil)
	if err := e.Do(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil); err != nil {
		t.Fatal(err)
	}
}

// STU-483, 484, 481, 482: a blocking failure, a missing address and a page-less change.
func TestRenderChecksFailureAppURLAndNoPages(t *testing.T) {
	e, runner := renderSetup(t)
	cs, _ := routedPage(e, "/broken")
	if _, err := submit(e, cs, 1); err != nil {
		t.Fatal(err)
	}
	c := claim(t, e, runner)
	if done, err := complete(e, runner, c, "failed"); err != nil || done.ChangesetState != "failed" {
		t.Fatal(done, err)
	}
	if err := workflow.RequireRenderChecks(context.Background(), e.Q, changesetState(t, e, cs), "staging"); cmstest.Code(err) != "CHECKS_FAILED" {
		t.Fatal(err)
	}
	// A page without a route renders nothing: the stages are skipped and do not block.
	plain, _ := e.Draft(e.Admin, "plain")
	if r, err := submit(e, plain, 1); err != nil || r.Changeset.State != "in_review" {
		t.Fatal(r, err)
	}
	if err := workflow.RequireRenderChecks(context.Background(), e.Q, changesetState(t, e, plain), "staging"); err != nil {
		t.Fatal(err)
	}
	if err := workflow.RequireRenderChecks(context.Background(), e.Q, changesetState(t, e, plain), "qa"); cmstest.Code(err) != "CHECKS_PENDING" {
		t.Fatal(err)
	}
	e.Exec("UPDATE environments SET app_url = NULL WHERE project_id = $1", e.Admin.ProjectID)
	missing, _ := routedPage(e, "/missing")
	r, err := submit(e, missing, 1)
	if err != nil || r.Changeset.State != "failed" {
		t.Fatal(r, err)
	}
	var details map[string]any
	_ = json.Unmarshal(r.Checks[2].Details, &details)
	if details["reason"] != "CHECK_APP_URL_MISSING" {
		t.Fatal(details)
	}
	for _, bad := range []map[string]any{{"environment": "staging", "appUrl": "ftp://x"}, {"environment": "staging", "appUrl": "https://u:p@x"}, {"environment": "nope", "appUrl": "https://x"}} {
		if err := e.Do(e.Admin, "set-environment-app-url", bad, nil); err == nil {
			t.Fatal(bad)
		}
	}
}

// STU-487, 495: lease expiry, retries, the runner timeout, stale queue and reopen.
func TestRenderChecksLeaseExpiryAndReopen(t *testing.T) {
	e, runner := renderSetup(t)
	ctx := context.Background()
	cs, _ := routedPage(e, "/lease")
	if _, err := submit(e, cs, 1); err != nil {
		t.Fatal(err)
	}
	var c *workflow.LeasedCheck
	for attempt := 1; attempt <= 3; attempt++ {
		c = claim(t, e, runner)
		if c == nil || c.Stage != workflow.StagePreview {
			t.Fatalf("attempt %d: %+v", attempt, c)
		}
		e.Exec("UPDATE checks SET lease_expires_at = now() - interval '1 second' WHERE id = $1", c.ID)
		if err := workflow.ExpireRenderLeases(ctx, e.Pool); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := complete(e, runner, c, "passed"); cmstest.Code(err) != "CHECK_LEASE_LOST" {
		t.Fatal(err)
	}
	if got := changesetState(t, e, cs); got.State != "failed" {
		t.Fatal(got.State)
	}
	review, err := workflow.GetReview(ctx, e.Q, e.Admin.ProjectID, cs)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range review.Checks {
		if check.Stage == workflow.StagePreview {
			var details map[string]any
			_ = json.Unmarshal(check.Details, &details)
			if check.Status != "failed" || details["reason"] != "CHECK_RUNNER_TIMEOUT" {
				t.Fatal(check, details)
			}
		}
	}

	other, _ := routedPage(e, "/reopen")
	if _, err := submit(e, other, 1); err != nil {
		t.Fatal(err)
	}
	e.Exec("UPDATE checks SET created_at = now() - interval '1 hour' WHERE changeset_id = $1", other)
	stale, err := workflow.GetReview(ctx, e.Q, e.Admin.ProjectID, other)
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range stale.Checks {
		if check.Stale != (check.Stage == workflow.StagePreview || check.Stage == workflow.StageAxe) {
			t.Fatal(check)
		}
	}
	leased := claim(t, e, runner)
	if leased == nil || leased.ChangesetID != other {
		t.Fatalf("%+v", leased)
	}
	e.Must(e.Admin, "reopen-changeset", map[string]any{"changesetId": other}, nil)
	if _, err := complete(e, runner, leased, "passed"); cmstest.Code(err) != "CHECK_LEASE_LOST" {
		t.Fatal(err)
	}
	if empty := claim(t, e, runner); empty != nil {
		t.Fatalf("%+v", empty)
	}
	// A still valid lease is left alone by the expiry job.
	if _, err := submit(e, other, int(changesetState(t, e, other).Seq)); err != nil {
		t.Fatal(err)
	}
	if valid := claim(t, e, runner); valid == nil {
		t.Fatal("no check after resubmission")
	}
	if err := workflow.ExpireRenderLeases(ctx, e.Pool); err != nil {
		t.Fatal(err)
	}
}
