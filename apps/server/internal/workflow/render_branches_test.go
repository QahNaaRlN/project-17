package workflow_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/previewtoken"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// runAll completes every queued stage 8–9 check with status.
func runAll(t *testing.T, e *cmstest.Env, runner auth.Actor, status string) {
	t.Helper()
	for c := claim(t, e, runner); c != nil; c = claim(t, e, runner) {
		if _, err := complete(e, runner, c, status); err != nil {
			t.Fatal(err)
		}
	}
}

// STU-483, 480, 482: no approvals needed, low risk, the page limit and the rebase requeue.
func TestRenderChecksApprovedLowRiskLimitAndRebase(t *testing.T) {
	e, runner := renderSetup(t)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	cs, doc := routedPage(e, "/approved")
	if _, err := submit(e, cs, 1); err != nil {
		t.Fatal(err)
	}
	runAll(t, e, runner, "passed")
	if got := changesetState(t, e, cs); got.State != "approved" {
		t.Fatal(got.State)
	}
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)

	// Local content of an existing page is low risk: stages 8–9 are skipped.
	var low changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "low", "targets": []string{"staging"}}, &low)
	if err := e.Apply(e.Admin, low.ID, 0, doc, cmstest.Op("localContent.set", map[string]any{"key": "t_label", "type": "text", "value": map[string]any{"ru": "x"}})); err != nil {
		t.Fatal(err)
	}
	r, err := submit(e, low.ID, 1)
	if err != nil || *r.Risk != workflow.RiskLow || r.Changeset.State != "approved" {
		t.Fatal(r, err)
	}
	var details map[string]any
	_ = json.Unmarshal(r.Checks[len(r.Checks)-1].Details, &details)
	if details["reason"] != "RISK_LOW" {
		t.Fatal(details)
	}

	// Two concurrent edits: publishing one makes the other rebase and check again.
	edit := func(op map[string]any) uuid.UUID {
		c := change(e, doc, op)
		if _, err := submit(e, c, 1); err != nil {
			t.Fatal(err)
		}
		runAll(t, e, runner, "passed")
		return c
	}
	first := edit(rename("first"))
	second := edit(cmstest.Op("node.insert", map[string]any{"parentId": "n_root", "subtree": map[string]any{"id": "n_new", "type": "Box"}}))
	e.Must(e.Admin, "publish", map[string]any{"changesetId": first, "environment": "staging"}, nil)
	out, err := rebase(e, second, int(changesetState(t, e, second).Seq))
	if err != nil {
		t.Fatal(out, err)
	}
	if got := changesetState(t, e, second); got.State != "checking" {
		t.Fatal(got.State)
	}

	// More than 50 pages are not rendered.
	var many changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "many", "targets": []string{"staging"}}, &many)
	ops := []any{}
	for i := 0; i < 51; i++ {
		ops = append(ops, map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": fmt.Sprintf("/many-%d", i), "root": map[string]any{"id": "n_root", "type": "Box"}}})
	}
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": many.ID, "expectedSeq": 0, "operations": ops}, nil)
	if r, err := submit(e, many.ID, 51); err != nil || r.Changeset.State != "failed" {
		t.Fatal(r, err)
	}
}

// STU-485, 486, 488: runner identity, a removed address and malformed results.
func TestRenderChecksRunnerGuards(t *testing.T) {
	e, runner := renderSetup(t)
	ctx := context.Background()
	human := e.Human("Runner impostor", auth.ChecksRun)
	if err := e.Do(human, "claim-check", map[string]any{}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	cs, _ := routedPage(e, "/guards")
	if _, err := submit(e, cs, 1); err != nil {
		t.Fatal(err)
	}
	c := claim(t, e, runner)
	for _, details := range []any{[]any{}, "x", nil} {
		if err := e.Do(runner, "complete-check", map[string]any{"checkId": c.ID, "leaseId": c.LeaseID, "status": "passed", "details": details}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Fatal(details, err)
		}
	}
	e.Exec("UPDATE environments SET app_url = NULL WHERE project_id = $1", e.Admin.ProjectID)
	if err := e.Do(runner, "claim-check", map[string]any{}, nil); cmstest.Code(err) != "CHECK_APP_URL_MISSING" {
		t.Fatal(err)
	}
	if _, err := previewtoken.IssueForChangeset(ctx, e.Q, "store", "missing", cs, runner.ID, time.Now()); err == nil {
		t.Fatal("token for an unknown environment")
	}
	// The periodic job body runs through the worker as well.
	if (workflow.RenderLeaseArgs{}).Kind() != "render_check_leases" {
		t.Fatal("job kind")
	}
	if err := (&workflow.RenderLeaseWorker{Pool: e.Pool}).Work(ctx, nil); err != nil {
		t.Fatal(err)
	}
}

// Storage failures surface as errors, never as decided checks.
func TestRenderChecksStorageFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("queue insert", func(t *testing.T) {
		e, _ := renderSetup(t)
		cs, _ := routedPage(e, "/insert")
		e.Break("checks", "INSERT")
		_, err := submit(e, cs, 1)
		cmstest.ExpectDBError(t, err)
	})
	t.Run("switch", func(t *testing.T) {
		e, _ := renderSetup(t)
		cs, _ := routedPage(e, "/switch")
		e.Break("project_render_checks", "")
		_, err := submit(e, cs, 1)
		cmstest.ExpectDBError(t, err)
		cmstest.ExpectDBError(t, workflow.RequireRenderChecks(ctx, e.Q, changesetState(t, e, cs), "staging"))
	})
	t.Run("pages", func(t *testing.T) {
		e, _ := renderSetup(t)
		cs, _ := routedPage(e, "/pages")
		e.Break("published_pointers", "")
		_, err := submit(e, cs, 1)
		cmstest.ExpectDBError(t, err)
	})
	t.Run("leases", func(t *testing.T) {
		e, runner := renderSetup(t)
		cs, _ := routedPage(e, "/leases")
		if _, err := submit(e, cs, 1); err != nil {
			t.Fatal(err)
		}
		c := claim(t, e, runner)
		e.Exec("UPDATE checks SET lease_expires_at = now() - interval '1 second' WHERE id = $1", c.ID)
		e.Break("project_revisions", "INSERT")
		cmstest.ExpectDBError(t, workflow.ExpireRenderLeases(ctx, e.Pool))
		e.Break("checks", "")
		cmstest.ExpectDBError(t, workflow.ExpireRenderLeases(ctx, e.Pool))
	})
	t.Run("claim", func(t *testing.T) {
		e, runner := renderSetup(t)
		cs, _ := routedPage(e, "/claim")
		if _, err := submit(e, cs, 1); err != nil {
			t.Fatal(err)
		}
		e.Break("checks", "UPDATE")
		cmstest.ExpectDBError(t, e.Do(runner, "claim-check", map[string]any{}, nil))
	})
	t.Run("finish", func(t *testing.T) {
		e, runner := renderSetup(t)
		cs, _ := routedPage(e, "/finish")
		if _, err := submit(e, cs, 1); err != nil {
			t.Fatal(err)
		}
		c := claim(t, e, runner)
		e.Break("checks", "UPDATE")
		_, err := complete(e, runner, c, "passed")
		cmstest.ExpectDBError(t, err)
		// The lease is expired directly in the past, then requeueing hits the same fault.
		e.Exec("DROP TRIGGER cms_test_fail ON checks")
		e.Exec("UPDATE checks SET lease_expires_at = now() - interval '1 second' WHERE id = $1", c.ID)
		e.Break("checks", "UPDATE")
		cmstest.ExpectDBError(t, workflow.ExpireRenderLeases(ctx, e.Pool))
	})
	t.Run("lease environment", func(t *testing.T) {
		e, runner := renderSetup(t)
		cs, _ := routedPage(e, "/environment")
		if _, err := submit(e, cs, 1); err != nil {
			t.Fatal(err)
		}
		e.Break("environments", "")
		cmstest.ExpectDBError(t, e.Do(runner, "claim-check", map[string]any{}, nil))
	})
	t.Run("complete", func(t *testing.T) {
		e, runner := renderSetup(t)
		cs, _ := routedPage(e, "/complete")
		if _, err := submit(e, cs, 1); err != nil {
			t.Fatal(err)
		}
		c := claim(t, e, runner)
		e.Break("changesets", "UPDATE")
		_, err := complete(e, runner, c, "failed")
		cmstest.ExpectDBError(t, err)
	})
}
