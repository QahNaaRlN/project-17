package workflow_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// memoryArtifacts is an in-memory object store of check artifacts.
type memoryArtifacts struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (m *memoryArtifacts) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = b
	return err
}

func (m *memoryArtifacts) Open(_ context.Context, key string) (io.ReadCloser, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.objects[key]
	if !ok {
		return nil, 0, errors.New("no such object")
	}
	return io.NopCloser(bytes.NewReader(b)), int64(len(b)), nil
}

func (m *memoryArtifacts) Remove(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

// stagesSetup enables stages 8–11 with an application address; store may be nil.
func stagesSetup(t *testing.T, s workflow.ArtifactStore) (*cmstest.Env, auth.Actor) {
	t.Helper()
	register := []func(*commandbus.Bus){workflow.Register, publishing.Register}
	if s != nil {
		register = append(register, func(b *commandbus.Bus) { b.WithContext(workflow.WithArtifacts(s)) })
	}
	e := cmstest.New(t, register...)
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
	return e, runner
}

func stage(t *testing.T, checks []workflow.Check, name string) (workflow.Check, map[string]any) {
	t.Helper()
	for _, c := range checks {
		if c.Stage == name {
			var details map[string]any
			_ = json.Unmarshal(c.Details, &details)
			return c, details
		}
	}
	t.Fatalf("no %s check in %+v", name, checks)
	return workflow.Check{}, nil
}

func reviewChecks(t *testing.T, e *cmstest.Env, cs uuid.UUID) []workflow.Check {
	t.Helper()
	r, err := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, cs)
	if err != nil {
		t.Fatal(err)
	}
	return r.Checks
}

func completeWith(e *cmstest.Env, runner auth.Actor, c *workflow.LeasedCheck, status string, details map[string]any) (workflow.CompletedCheck, error) {
	var out workflow.CompletedCheck
	err := e.Do(runner, "complete-check", map[string]any{"checkId": c.ID, "leaseId": c.LeaseID, "status": status, "details": details}, &out)
	return out, err
}

// passRender completes the leased stage 8–9 checks of the queue.
func passRender(t *testing.T, e *cmstest.Env, runner auth.Actor) string {
	t.Helper()
	state := ""
	for range 2 {
		c := claim(t, e, runner)
		if c == nil || (c.Stage != workflow.StagePreview && c.Stage != workflow.StageAxe) {
			t.Fatalf("%+v", c)
		}
		done, err := complete(e, runner, c, "passed")
		if err != nil {
			t.Fatal(err)
		}
		state = done.ChangesetState
	}
	return state
}

// STU-570…575, 583, 585: stage 10 is queued without blocking, stores its screenshots
// under the lease, completes after review began and its artifacts expire.
func TestVisualStageArtifacts(t *testing.T) {
	ctx := context.Background()
	s := &memoryArtifacts{objects: map[string][]byte{}}
	e, runner := stagesSetup(t, s)
	cs, doc := routedPage(e, "/visual")
	r, err := submit(e, cs, 1)
	if err != nil || r.Changeset.State != "checking" {
		t.Fatal(r, err)
	}
	if c, _ := stage(t, r.Checks, workflow.StageVisual); c.Status != "queued" || c.Blocking {
		t.Fatalf("%+v", c)
	}
	if c, d := stage(t, r.Checks, workflow.StageLighthouse); c.Status != "skipped" || d["reason"] != "RISK_LOW" {
		t.Fatalf("%+v %v", c, d)
	}
	// Blocking checks are leased first; review starts while stage 10 still waits.
	if state := passRender(t, e, runner); state != "in_review" {
		t.Fatal(state)
	}
	if err := workflow.RequireRenderChecks(ctx, e.Q, changesetState(t, e, cs), "staging"); err != nil {
		t.Fatal(err)
	}
	visual := claim(t, e, runner)
	if visual == nil || visual.Stage != workflow.StageVisual || len(visual.Pages) != 1 || visual.Pages[0].ObjectID != doc || visual.Thresholds != nil {
		t.Fatalf("%+v", visual)
	}
	png := []byte("\x89PNG fake")
	put := func(a auth.Actor, s workflow.ArtifactStore, lease uuid.UUID, name, ct string, body []byte) error {
		return workflow.PutArtifact(ctx, e.Pool, s, a, visual.ID, lease, name, ct, body)
	}
	for _, bad := range []struct {
		err  error
		code string
	}{
		{put(e.Admin, s, visual.LeaseID, "a.png", "image/png", png), "FORBIDDEN"},
		{put(runner, nil, visual.LeaseID, "a.png", "image/png", png), "CHECK_ARTIFACTS_UNAVAILABLE"},
		{put(runner, s, visual.LeaseID, "A B.png", "image/gif", png), "VALIDATION_FAILED"},
		{put(runner, s, visual.LeaseID, "big.png", "image/png", make([]byte, workflow.MaxArtifactSize+1)), "CHECK_ARTIFACTS_LIMIT"},
		{put(runner, s, uuid.New(), "a.png", "image/png", png), "CHECK_LEASE_LOST"},
		{workflow.PutArtifact(ctx, e.Pool, s, runner, uuid.New(), visual.LeaseID, "a.png", "image/png", png), "NOT_FOUND"},
	} {
		if cmstest.Code(bad.err) != bad.code {
			t.Fatal(bad.code, bad.err)
		}
	}
	if err := put(runner, s, visual.LeaseID, "visual-375-after.png", "image/png", png); err != nil {
		t.Fatal(err)
	}
	var extended workflow.ExtendedLease
	e.Must(runner, "extend-check-lease", map[string]any{"checkId": visual.ID, "leaseId": visual.LeaseID}, &extended)
	if !extended.LeaseExpiresAt.After(time.Now()) {
		t.Fatal(extended)
	}
	for _, p := range []map[string]any{{"checkId": visual.ID, "leaseId": uuid.New()}, {"checkId": uuid.New(), "leaseId": visual.LeaseID}} {
		if err := e.Do(runner, "extend-check-lease", p, nil); cmstest.Code(err) != "CHECK_LEASE_LOST" && cmstest.Code(err) != "NOT_FOUND" {
			t.Fatal(err)
		}
	}
	if err := e.Do(e.Admin, "extend-check-lease", map[string]any{"checkId": visual.ID, "leaseId": visual.LeaseID}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	details := map[string]any{"pages": []any{map[string]any{"objectId": doc, "path": "/visual", "widths": []any{
		map[string]any{"width": 375, "baseline": "published", "changedRatio": 0.5,
			"artifacts": map[string]any{"after": "visual-375-after.png", "diff": "visual-375-diff.png"}},
	}}}, "truncated": false}
	if _, err := completeWith(e, runner, visual, "passed", details); cmstest.Code(err) != "CHECK_ARTIFACT_MISSING" {
		t.Fatal(err)
	}
	if err := put(runner, s, visual.LeaseID, "visual-375-diff.png", "image/png", png); err != nil {
		t.Fatal(err)
	}
	done, err := completeWith(e, runner, visual, "passed", details)
	if err != nil || done.ChangesetState != "in_review" {
		t.Fatal(done, err)
	}
	var stored []workflow.Artifact
	row, err := e.Q.LockRenderCheck(ctx, store.LockRenderCheckParams{ID: visual.ID, ProjectID: e.Admin.ProjectID})
	if err != nil || json.Unmarshal(row.Artifacts, &stored) != nil || len(stored) != 2 || stored[0].Name != "visual-375-after.png" || stored[0].Size != int64(len(png)) {
		t.Fatal(stored, err)
	}
	opened, err := workflow.OpenArtifact(ctx, e.Q, s, e.Admin.ProjectID, cs, visual.ID, "visual-375-diff.png")
	if err != nil || opened.ContentType != "image/png" || opened.Size != int64(len(png)) {
		t.Fatal(opened, err)
	}
	_ = opened.Body.Close()
	for _, missing := range []error{
		func() error {
			_, err := workflow.OpenArtifact(ctx, e.Q, s, e.Admin.ProjectID, cs, visual.ID, "nope.png")
			return err
		}(),
		func() error {
			_, err := workflow.OpenArtifact(ctx, e.Q, nil, e.Admin.ProjectID, cs, visual.ID, "visual-375-diff.png")
			return err
		}(),
		func() error {
			_, err := workflow.OpenArtifact(ctx, e.Q, s, e.Admin.ProjectID, uuid.New(), visual.ID, "visual-375-diff.png")
			return err
		}(),
	} {
		if cmstest.Code(missing) != "NOT_FOUND" {
			t.Fatal(missing)
		}
	}

	reviewer := e.Human("Reviewer", auth.ContentPublish, auth.DesignRead, auth.ContentRead)
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil)
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)

	// STU-583: nothing expires before the retention period; afterwards the objects go.
	if err := workflow.ExpireArtifacts(ctx, e.Pool, s, time.Now().Add(-workflow.ArtifactRetention)); err != nil || len(s.objects) != 2 {
		t.Fatal(err, len(s.objects))
	}
	if err := workflow.ExpireArtifacts(ctx, e.Pool, nil, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := workflow.ExpireArtifacts(ctx, e.Pool, s, time.Now().Add(time.Hour)); err != nil || len(s.objects) != 0 {
		t.Fatal(err, len(s.objects))
	}
	if _, err := workflow.OpenArtifact(ctx, e.Q, s, e.Admin.ProjectID, cs, visual.ID, "visual-375-diff.png"); cmstest.Code(err) != "CHECK_ARTIFACT_EXPIRED" {
		t.Fatal(err)
	}
}

// STU-571: a stage 10 check still queued at publication is skipped; stage 10 without
// storage, without an address and without pages is skipped with the reason.
func TestVisualStageSkips(t *testing.T) {
	ctx := context.Background()
	e, runner := stagesSetup(t, &memoryArtifacts{objects: map[string][]byte{}})
	cs, _ := routedPage(e, "/merged")
	if _, err := submit(e, cs, 1); err != nil {
		t.Fatal(err)
	}
	passRender(t, e, runner)
	reviewer := e.Human("Reviewer", auth.ContentPublish, auth.DesignRead, auth.ContentRead)
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil)
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
	var status, reason string
	if err := e.Pool.QueryRow(ctx, `SELECT status, details->>'reason' FROM checks WHERE changeset_id = $1 AND stage = 'visual'`, cs).Scan(&status, &reason); err != nil || status != "skipped" || reason != "CHANGESET_MERGED" {
		t.Fatal(status, reason, err)
	}
	if empty := claim(t, e, runner); empty != nil {
		t.Fatalf("%+v", empty)
	}

	plain, _ := e.Draft(e.Admin, "plain")
	r, err := submit(e, plain, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, d := stage(t, r.Checks, workflow.StageVisual); d["reason"] != "NO_PAGES" {
		t.Fatal(d)
	}
	e.Exec("UPDATE environments SET app_url = NULL WHERE project_id = $1", e.Admin.ProjectID)
	missing, _ := routedPage(e, "/missing")
	if r, err = submit(e, missing, 1); err != nil {
		t.Fatal(err)
	}
	if _, d := stage(t, r.Checks, workflow.StageVisual); d["reason"] != "CHECK_APP_URL_MISSING" {
		t.Fatal(d)
	}

	bare, runner := stagesSetup(t, nil)
	cs, _ = routedPage(bare, "/bare")
	if r, err = submit(bare, cs, 1); err != nil {
		t.Fatal(err)
	}
	if _, d := stage(t, r.Checks, workflow.StageVisual); d["reason"] != "CHECK_ARTIFACTS_UNAVAILABLE" {
		t.Fatal(d)
	}
	if state := passRender(t, bare, runner); state != "in_review" {
		t.Fatal(state)
	}
}

// strictPage creates a Change Set with a routed page in a STRICT zone (risk high, 06 §5.2)
// and extra routed pages.
func strictPage(e *cmstest.Env, path string, extra int) uuid.UUID {
	e.T.Helper()
	role, err := e.Q.CreateRole(context.Background(), store.CreateRoleParams{ID: uuid.New(), ProjectID: e.Admin.ProjectID, Name: "owner-" + path[1:], Capabilities: []string{}})
	if err != nil {
		e.T.Fatal(err)
	}
	native := strings.TrimSuffix(cmstest.DefaultManifest, "}") + `,"components":{"Native":{"container":true,"props":{}}}}`
	for _, env := range []string{"staging", "production"} {
		e.ActivateManifest(env, []byte(native))
	}
	var c changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": path, "targets": []string{"staging"}}, &c)
	ops := []any{map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": path, "root": map[string]any{
		"id": "n_root", "type": "Native", "zone": map[string]any{"id": "strict", "requiredApprovalRole": role.Name, "mode": "STRICT"}}}}}
	for i := range extra {
		ops = append(ops, map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "path": fmt.Sprintf("%s/p%d", path, i),
			"root": map[string]any{"id": "n_root", "type": "Box"}}})
	}
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": c.ID, "expectedSeq": 0, "operations": ops}, nil)
	return c.ID
}

// STU-579, 581, 582: stage 11 runs for high risk with the project thresholds, blocks by
// them and covers at most 10 pages.
func TestLighthouseStagePolicy(t *testing.T) {
	ctx := context.Background()
	e, runner := stagesSetup(t, nil)
	if p, err := workflow.LoadCheckPolicy(ctx, e.Q, e.Admin.ProjectID); err != nil || p != workflow.DefaultCheckPolicy {
		t.Fatal(p, err)
	}
	for _, bad := range []map[string]any{
		{"lighthouse": map[string]any{"performance": map[string]any{"min": 1.5}}},
		{"lighthouse": map[string]any{"accessibility": map[string]any{"min": -0.1}}},
	} {
		if err := e.Do(e.Admin, "set-check-policy", bad, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
			t.Fatal(err)
		}
	}
	if err := e.Do(runner, "set-check-policy", map[string]any{"lighthouse": map[string]any{}}, nil); err == nil {
		t.Fatal("runner set the policy")
	}

	cs := strictPage(e, "/strict", 10)
	r, err := submit(e, cs, 11)
	if err != nil || *r.Risk != "high" || r.Changeset.State != "checking" {
		t.Fatal(r, err)
	}
	lh, d := stage(t, r.Checks, workflow.StageLighthouse)
	if lh.Status != "queued" || !lh.Blocking || len(d["pages"].([]any)) != 10 || len(d["skipped"].([]any)) != 1 {
		t.Fatalf("%+v %v", lh, d)
	}
	passRender(t, e, runner)
	if got := changesetState(t, e, cs); got.State != "checking" {
		t.Fatal(got.State)
	}
	if err := workflow.RequireRenderChecks(ctx, e.Q, changesetState(t, e, cs), "staging"); cmstest.Code(err) != "CHECKS_PENDING" {
		t.Fatal(err)
	}
	leased := claim(t, e, runner)
	if leased == nil || leased.Stage != workflow.StageLighthouse || leased.Thresholds == nil || *leased.Thresholds != workflow.DefaultCheckPolicy.Lighthouse || len(leased.Pages) != 10 {
		t.Fatalf("%+v", leased)
	}
	done, err := completeWith(e, runner, leased, "failed", map[string]any{"pages": []any{map[string]any{"path": "/strict", "scores": map[string]any{"accessibility": 0.5}}}})
	if err != nil || done.ChangesetState != "failed" {
		t.Fatal(done, err)
	}
	if err := workflow.RequireRenderChecks(ctx, e.Q, changesetState(t, e, cs), "staging"); cmstest.Code(err) != "CHECKS_FAILED" {
		t.Fatal(err)
	}

	// Thresholds that only warn make stage 11 non-blocking: review starts after stages 8–9.
	relaxed := map[string]any{"lighthouse": map[string]any{"performance": map[string]any{"min": 0.4}, "accessibility": map[string]any{"min": 0.8}}}
	var policy workflow.CheckPolicy
	e.Must(e.Admin, "set-check-policy", relaxed, &policy)
	if p, err := workflow.LoadCheckPolicy(ctx, e.Q, e.Admin.ProjectID); err != nil || p != policy || p.Lighthouse.Blocking() || p.Lighthouse.Accessibility.Min != 0.8 {
		t.Fatal(p, err)
	}
	other := strictPage(e, "/relaxed", 0)
	if r, err = submit(e, other, 1); err != nil {
		t.Fatal(err)
	}
	if lh, _ := stage(t, r.Checks, workflow.StageLighthouse); lh.Blocking {
		t.Fatalf("%+v", lh)
	}
	if state := passRender(t, e, runner); state != "in_review" {
		t.Fatal(state)
	}
	if lh, _ := stage(t, reviewChecks(t, e, other), workflow.StageLighthouse); lh.Status != "queued" {
		t.Fatalf("%+v", lh)
	}
}

// failingArtifacts fails every object operation.
type failingArtifacts struct{ memoryArtifacts }

func (f *failingArtifacts) Open(context.Context, string) (io.ReadCloser, int64, error) {
	return nil, 0, errors.New("storage down")
}
func (f *failingArtifacts) Remove(context.Context, string) error { return errors.New("storage down") }

// STU-573, 575, 583, 585: limits, storage and database failures of stage 10–11 artifacts.
func TestStageArtifactFaults(t *testing.T) {
	ctx := context.Background()
	s := &memoryArtifacts{objects: map[string][]byte{}}
	e, runner := stagesSetup(t, s)
	cs, doc := routedPage(e, "/faults")
	if _, err := submit(e, cs, 1); err != nil {
		t.Fatal(err)
	}
	preview := claim(t, e, runner)
	if err := workflow.PutArtifact(ctx, e.Pool, s, runner, preview.ID, preview.LeaseID, "a.png", "image/png", nil); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatal(err)
	}
	if _, err := complete(e, runner, preview, "passed"); err != nil {
		t.Fatal(err)
	}
	if _, err := complete(e, runner, claim(t, e, runner), "passed"); err != nil {
		t.Fatal(err)
	}
	visual := claim(t, e, runner)
	e.Exec(`INSERT INTO check_artifacts (check_id, name, content_type, size, sha256)
	  SELECT $1, 'f' || i || '.png', 'image/png', 1, '\x00' FROM generate_series(1, 400) i`, visual.ID)
	if err := workflow.PutArtifact(ctx, e.Pool, s, runner, visual.ID, visual.LeaseID, "more.png", "image/png", []byte("x")); cmstest.Code(err) != "CHECK_ARTIFACTS_LIMIT" {
		t.Fatal(err)
	}
	e.Exec(`DELETE FROM check_artifacts WHERE check_id = $1`, visual.ID)
	if err := workflow.PutArtifact(ctx, e.Pool, s, runner, visual.ID, visual.LeaseID, "a.png", "image/png", []byte("x")); err != nil {
		t.Fatal(err)
	}
	details := map[string]any{"pages": []any{map[string]any{"objectId": doc, "path": "/faults", "report": "a.png"}}}
	if _, err := completeWith(e, runner, visual, "passed", details); err != nil {
		t.Fatal(err)
	}
	// A lost object answers a storage error; a failing store stops the retention batch.
	broken := &failingArtifacts{}
	if _, err := workflow.OpenArtifact(ctx, e.Q, broken, e.Admin.ProjectID, cs, visual.ID, "a.png"); err == nil || cmstest.Code(err) != "" {
		t.Fatal(err)
	}
	e.Exec(`UPDATE checks SET finished_at = now() - interval '100 days' WHERE id = $1`, visual.ID)
	e.Exec(`UPDATE changesets SET state = 'abandoned' WHERE id = $1`, cs)
	if err := workflow.ExpireArtifacts(ctx, e.Pool, broken, time.Now()); err == nil {
		t.Fatal("retention ignored a storage failure")
	}
	w := &workflow.ArtifactRetentionWorker{Pool: e.Pool, Store: s}
	if (workflow.ArtifactRetentionArgs{}).Kind() != "check_artifact_retention" || w.Work(ctx, nil) != nil || len(s.objects) != 0 {
		t.Fatal(s.objects)
	}

	for name, brk := range map[string]func(*cmstest.Env){
		"checks":    func(e *cmstest.Env) { e.Break("checks", "") },
		"artifacts": func(e *cmstest.Env) { e.Break("check_artifacts", "") },
	} {
		t.Run(name, func(t *testing.T) {
			e, runner := stagesSetup(t, s)
			cs, _ := routedPage(e, "/db")
			if _, err := submit(e, cs, 1); err != nil {
				t.Fatal(err)
			}
			passRender(t, e, runner)
			c := claim(t, e, runner)
			brk(e)
			cmstest.ExpectDBError(t, workflow.PutArtifact(ctx, e.Pool, s, runner, c.ID, c.LeaseID, "a.png", "image/png", []byte("x")))
			if name == "checks" {
				cmstest.ExpectDBError(t, e.Do(runner, "extend-check-lease", map[string]any{"checkId": c.ID, "leaseId": c.LeaseID}, nil))
			}
			_, err := workflow.OpenArtifact(ctx, e.Q, s, e.Admin.ProjectID, cs, c.ID, "a.png")
			cmstest.ExpectDBError(t, err)
			cmstest.ExpectDBError(t, workflow.ExpireArtifacts(ctx, e.Pool, s, time.Now()))
		})
	}
	for name, brk := range map[string]func(*cmstest.Env){
		"read":     func(e *cmstest.Env) { e.Break("projects", "") },
		"write":    func(e *cmstest.Env) { e.Break("projects", "UPDATE") },
		"settings": func(e *cmstest.Env) { e.Exec("UPDATE projects SET settings = '[]'") },
	} {
		t.Run("policy-"+name, func(t *testing.T) {
			e, _ := stagesSetup(t, nil)
			brk(e)
			cmstest.ExpectDBError(t, e.Do(e.Admin, "set-check-policy", map[string]any{"lighthouse": map[string]any{}}, nil))
		})
	}
	t.Run("submit-settings", func(t *testing.T) {
		e, _ := stagesSetup(t, nil)
		cs, _ := routedPage(e, "/settings")
		e.Exec("UPDATE projects SET settings = '[]'")
		cmstest.ExpectDBError(t, func() error { _, err := submit(e, cs, 1); return err }())
	})
}

// STU-579, 582: more than 50 pages fail stage 8 and skip stages 10–11; a missing address
// skips stage 11 of a high-risk change.
func TestStageSkipsForFailedRender(t *testing.T) {
	e, _ := stagesSetup(t, &memoryArtifacts{objects: map[string][]byte{}})
	cs := strictPage(e, "/many", 50)
	r, err := submit(e, cs, 51)
	if err != nil || r.Changeset.State != "failed" {
		t.Fatal(r, err)
	}
	for _, name := range []string{workflow.StageVisual, workflow.StageLighthouse} {
		if _, d := stage(t, r.Checks, name); d["reason"] != "PREVIEW_FAILED" {
			t.Fatal(name, d)
		}
	}
	e.Exec("UPDATE environments SET app_url = NULL WHERE project_id = $1", e.Admin.ProjectID)
	other := strictPage(e, "/noapp", 0)
	if r, err = submit(e, other, 1); err != nil {
		t.Fatal(err)
	}
	if _, d := stage(t, r.Checks, workflow.StageLighthouse); d["reason"] != "CHECK_APP_URL_MISSING" {
		t.Fatal(d)
	}
}
