package capabilities

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// STU-336/337/351: payloads, reasons, rights and states of the fulfillment
// commands are checked before anything changes.
func TestSTU336_351FulfillmentCommandGuards(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	id := f.githubFixture.request.ID
	original := anchorFor(f.anchors(), f.cs)
	retry := AnchorAction{RequestID: id, AnchorID: original.ID, ExpectedAnchorRevision: original.Revision}
	_, e := f.call(f.a, "attach-capability-request", AttachRequest{}, "")
	expectCode(t, e, "CAPABILITY_REQUEST_INVALID")
	_, e = f.call(f.a, "set-capability-fulfillment-args", retry, "")
	expectCode(t, e, "CAPABILITY_REQUEST_INVALID")
	_, e = f.call(f.engineer, "check-capability-fulfillment", CheckFulfillment{}, "")
	expectCode(t, e, "CAPABILITY_REQUEST_INVALID")
	_, e = f.call(f.e.Human("viewer", auth.DesignRead), "check-capability-fulfillment", CheckFulfillment{RequestID: id}, "")
	expectCode(t, e, "FORBIDDEN")
	// A reason is required for every change of a group.
	cs2, doc2 := f.page("second")
	env, _ := f.e.Q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: f.a.ProjectID, Name: "staging"})
	m, _ := f.e.Q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: f.a.ProjectID, ID: *env.ActiveManifestID})
	expectCode(t, f.e.Do(f.a, "attach-capability-request", AttachRequest{RequestID: id, ExpectedRequestRevision: f.revision(), ChangesetID: cs2, ExpectedSeq: f.seq(cs2), DocumentID: doc2, NodeID: "n_root", Event: "click", ExpectedManifestHash: m.Hash}, nil), "REASON_REQUIRED")
	expectCode(t, f.e.Do(f.a, "retry-capability-fulfillment", retry, nil), "REASON_REQUIRED")
	unknown := retry
	unknown.RequestID = uuid.New()
	_, e = f.call(f.a, "retry-capability-fulfillment", unknown, "")
	expectCode(t, e, "NOT_FOUND")
	// A detached anchor is no longer counted or shown.
	f.attach(cs2, doc2)
	joined := anchorFor(f.anchors(), cs2)
	f.command(f.a, "detach-capability-request", AnchorAction{RequestID: id, AnchorID: joined.ID, ExpectedAnchorRevision: joined.Revision, ExpectedSeq: f.seq(cs2)})
	if v, e := FulfillmentGet(ctx, f.e.Q, f.a, id); e != nil || v["anchorCount"] != 1 {
		t.Fatal("STU-351 detached anchors are hidden", v, e)
	}
	// Merged observations queue a check only for an environment with a manifest.
	l := f.row()
	if e := mergedCheck(ctx, f.e.Q, l, gh.Observation{State: "open"}); e != nil {
		t.Fatal(e)
	}
	missing := l
	missing.RequestID = uuid.New()
	if e := mergedCheck(ctx, f.e.Q, missing, gh.Observation{State: "merged"}); e == nil {
		t.Fatal("unknown request")
	}
	f.e.Exec(`UPDATE environments SET active_manifest_id=NULL WHERE name='staging'`)
	if e := mergedCheck(ctx, f.e.Q, l, gh.Observation{State: "merged"}); e != nil {
		t.Fatal(e)
	}
	_, e = f.call(f.engineer, "check-capability-fulfillment", CheckFulfillment{RequestID: id}, "")
	expectCode(t, e, "CAPABILITY_ENVIRONMENT_INVALID")
	// A draft has no fulfillment to show; a terminal one cannot be checked.
	f.e.Exec(`UPDATE capability_requests SET status='draft'`)
	if _, e := FulfillmentGet(ctx, f.e.Q, f.a, id); cmstest.Code(e) != "NOT_FOUND" {
		t.Fatal("draft", e)
	}
	f.e.Exec(`UPDATE capability_requests SET status='rejected'`)
	_, e = f.call(f.engineer, "check-capability-fulfillment", CheckFulfillment{RequestID: id}, "")
	expectCode(t, e, "CAPABILITY_STATE_INVALID")
}

// STU-340: the worker's River entry point, its clock and the bounded retry.
func TestSTU340FulfillmentWorkerRetries(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	f.svc.Now = time.Now
	if e := (&FulfillmentWorker{Service: f.svc}).Work(ctx, nil); e != nil {
		t.Fatal(e)
	}
	if (FulfillmentArgs{}).Kind() != "capability_fulfillment" || (FulfillmentArgs{}).InsertOpts().MaxAttempts != 1 {
		t.Fatal("periodic tick options")
	}
	f.merged()
	f.deploy(mergeSHA, object{"value": object{"type": "string"}})
	f.e.Exec(`UPDATE capability_fulfillment_jobs SET attempts=2`)
	f.e.Break("capability_github_links", "")
	f.run()
	var open int
	f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM capability_fulfillment_jobs WHERE state<>'done'`).Scan(&open)
	if open != 0 || f.current().Status == "fulfilled" {
		t.Fatal("STU-340 the third failed attempt ends the job", open)
	}
}

// STU-340: fresh facts are requested through the GitHub queue.
func TestSTU340StaleObservationQueuesGithubJob(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	queue, e := jobs.NewInserter(f.e.Pool)
	if e != nil {
		t.Fatal(e)
	}
	f.svc.Queue = queue
	f.merged()
	f.e.Exec(`UPDATE capability_github_observations SET fresh_until=now()-interval '1 minute'`)
	f.e.Exec(`UPDATE capability_github_links SET refresh_after=now()-interval '1 minute', state='idle'`)
	f.deploy(mergeSHA, object{"value": object{"type": "string"}})
	f.run()
	var queued int
	f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind=$1`, (GithubArgs{}).Kind()).Scan(&queued)
	if f.row().State != "queued" || queued == 0 {
		t.Fatal("STU-340 refresh is queued", f.row().State, queued)
	}
	// The same reason again leaves the request untouched.
	before := f.current()
	f.command(f.engineer, "check-capability-fulfillment", CheckFulfillment{RequestID: before.ID})
	f.run()
	if after := f.current(); after.FulfillmentReason != before.FulfillmentReason || after.Revision != before.Revision {
		t.Fatal("repeated reason", after.FulfillmentReason)
	}
}

// STU-342: a refused group names its failing operation.
func TestSTU342OperationIndex(t *testing.T) {
	if operationIndex(errors.New("plain")) != 0 {
		t.Fatal("plain error")
	}
	if operationIndex(commandbus.NewError(409, "X", "x", "x").WithParams(map[string]any{"operationIndex": 1})) != 1 {
		t.Fatal("indexed error")
	}
}

// STU-345: changes of unchanged events, non-marker chains and the anchor
// lookup failure are refused for cms-system; people get explicit marker errors.
func TestSTU345SystemRuleEdges(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	apply := func(a auth.Actor, event string, handlers any) error {
		tx, e := f.e.Pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		q := store.New(tx)
		cs, _ := q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
		if a.ID == uuid.Nil {
			a, _ = systemActor(ctx, q, f.a.ProjectID, cs.OwnerID)
		}
		if _, e = changes.ApplyInTx(ctx, tx, a, f.cs, cs.Seq, nil); e == nil {
			t.Fatal("an empty group is invalid")
		}
		_, e = changes.ApplyInTx(ctx, tx, a, f.cs, cs.Seq, []changes.OperationInput{{Type: "node.setBehavior", Target: &f.doc, Payload: raw(object{"nodeId": "n_root", "event": event, "handlers": handlers})}})
		if e == nil {
			e = tx.Commit(ctx)
		}
		return e
	}
	system := auth.Actor{}
	scroll := []any{object{"action": "scrollTo", "args": object{"target": object{"lit": "n_root"}}}}
	if e := apply(f.a, "hover", scroll); e != nil {
		t.Fatal(e)
	}
	f.e.Exec(`UPDATE capability_requests SET status='fulfilled'`)
	expectCode(t, apply(system, "hover", []any{object{"action": "store.favorite"}}), "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN")
	expectCode(t, apply(f.a, "focus", []any{object{"action": "pending:" + f.githubFixture.request.ID.String()}}), "CAPABILITY_PENDING_INVALID")
	f.e.Break("capability_request_anchors", "")
	if e := apply(system, "click", []any{object{"action": "store.favorite"}}); e == nil || cmstest.Code(e) != "" {
		t.Fatal("anchor lookup failure is a storage error", e)
	}
	if e := apply(f.a, "focus", []any{object{"action": "pending:" + f.githubFixture.request.ID.String()}}); e == nil || cmstest.Code(e) != "" {
		t.Fatal("marker lookup failure is a storage error", e)
	}
}

// STU-339/341: a disabled configuration falls back to manual resolve, and a
// decision for a request changed meanwhile is dropped.
func TestSTU339_341ConfigAndRaceGuards(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	f.merged()
	f.e.Exec(`UPDATE capability_github_configs SET enabled=false`)
	f.deploy(mergeSHA, object{"value": object{"type": "string"}})
	f.run()
	if r := f.current(); r.Status == "fulfilled" || r.FulfillmentReason != "no_github_link" {
		t.Fatal("STU-339 a disabled configuration does not fulfil", r.FulfillmentReason)
	}
	env, _ := f.e.Q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: f.a.ProjectID, Name: "staging"})
	m, _ := f.e.Q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: f.a.ProjectID, ID: *env.ActiveManifestID})
	stale := f.current()
	stale.Revision--
	if e := f.svc.record(ctx, stale, m, decision{reason: "pr_not_merged"}); e != nil || f.current().FulfillmentReason != "no_github_link" {
		t.Fatal("STU-341 a changed request is not overwritten", e, f.current().FulfillmentReason)
	}
	gone := f.current()
	gone.ID = uuid.New()
	if e := f.svc.record(ctx, gone, m, decision{reason: "pr_not_merged"}); e == nil {
		t.Fatal("unknown request")
	}
}

// STU-342/343: a group the pipeline refuses marks its anchors; anchors of
// requests not yet fulfilled in the same Change Set are left alone.
func TestSTU342RefusedGroupAndUnfulfilledAnchors(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	f.e.Exec(`UPDATE capability_request_anchors SET state='queued', next_at=now()`)
	if e := f.svc.Replace(ctx, f.a.ProjectID, f.cs); e != nil {
		t.Fatal(e)
	}
	if a := anchorFor(f.anchors(), f.cs); a.State != "queued" || f.clickOf(f.cs, f.doc)["action"] == "store.favorite" {
		t.Fatal("STU-343 an unfulfilled request is not replaced", a.State)
	}
	f.e.Exec(`UPDATE capability_requests SET status='fulfilled'`)
	f.e.Exec(`UPDATE capability_request_anchors SET fulfillment_args='{"action":"store.favorite","args":"not-an-object"}'`)
	f.activate(object{"value": object{"type": "string"}})
	if e := f.svc.Run(ctx); e != nil {
		t.Fatal(e)
	}
	if a := anchorFor(f.anchors(), f.cs); a.State != "invalid_args" || f.clickOf(f.cs, f.doc)["action"] == "store.favorite" {
		t.Fatal("STU-342 a refused operation marks its anchor", a.State)
	}
}

// STU-337/351: reads of a request whose page is gone, and storage failures
// of the reads that decide visibility.
func TestSTU337_351ReadsOfAGonePage(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	id := f.githubFixture.request.ID
	f.e.Exec(`UPDATE capability_requests SET status='fulfilled'`)
	if v, e := Get(ctx, f.e.Q, f.a, id); e != nil || v["fulfillmentReverted"] != true {
		t.Fatal("the marker is still in place", v, e)
	}
	f.e.Exec(`UPDATE capability_request_anchors SET state='replaced'`)
	f.e.Exec(`DELETE FROM changeset_objects WHERE changeset_id=$1 AND object_id=$2`, f.cs, f.doc)
	if v, e := Get(ctx, f.e.Q, f.a, id); e != nil || v["anchorChanged"] != true || v["stale"] != true {
		t.Fatal("a gone page is an anchor change", v, e)
	}
	if v, e := FulfillmentGet(ctx, f.e.Q, f.a, id); e != nil || obj(arr(v["anchors"])[0])["state"] != "replaced" {
		t.Fatal("a replaced anchor of a gone page stays replaced", v, e)
	}
	carol := f.e.Human("carol", auth.DesignRead, auth.BehaviorUse, auth.CapabilityRequest)
	f.e.Break("capability_github_links", "")
	if _, e := Get(ctx, f.e.Q, f.engineer, id); e == nil {
		t.Fatal("link failure hidden")
	}
	f.e.Break("capability_request_anchors", "")
	if _, e := Get(ctx, f.e.Q, carol, id); e == nil || cmstest.Code(e) != "" {
		t.Fatal("anchor ownership failure hidden", e)
	}
}

// STU-273/335/341: refusals of request updates, and storage failures of the
// anchor bookkeeping in create, cancel and manual resolve.
func TestSTU335RequestAnchorBookkeeping(t *testing.T) {
	f := fulfillmentSetup(t)
	id := f.githubFixture.request.ID
	update := func(a auth.Actor, p Update) error {
		_, e := f.call(a, "update-capability-request", p, "")
		return e
	}
	expectCode(t, update(f.a, Update{ID: uuid.New(), ExpectedRevision: 1, Transition: "cancelled"}), "NOT_FOUND")
	expectCode(t, update(f.e.Human("lead", auth.CapabilityRequest, auth.DesignRead, auth.BehaviorUse, auth.CodePropose), Update{ID: id, ExpectedRevision: f.revision(), Transition: "cancelled"}), "NOT_FOUND")
	for _, s := range []string{"accepted", "in_progress"} {
		f.command(f.engineer, "update-capability-request", Update{ID: id, ExpectedRevision: f.revision(), Transition: s})
	}
	f.e.Exec(`UPDATE capability_requests SET external_refs='["a","b","c","d","e","f","g","h"]'`)
	expectCode(t, update(f.engineer, Update{ID: id, ExpectedRevision: f.revision(), Transition: "pr_open", PRURL: "https://example.test/repo/pull/2"}), "CAPABILITY_REQUEST_INVALID")
	// Manual resolve and cancel report a failed anchor update.
	hash := f.activate(object{"value": object{"type": "string"}})
	f.e.Break("capability_request_anchors", "UPDATE")
	if _, e := f.call(f.a, "resolve-capability-request", Action{ID: id, ExpectedRevision: f.revision(), ExpectedSeq: f.seq(f.cs), ExpectedManifestHash: hash, Handlers: raw([]any{object{"action": "store.favorite", "args": object{"value": object{"lit": "x"}}}})}, ""); e == nil || f.current().Status == "fulfilled" {
		t.Fatal("resolve hid the anchor failure", e)
	}
	if e := update(f.a, Update{ID: id, ExpectedRevision: f.revision(), Transition: "cancelled"}); e == nil || f.current().Status == "cancelled" {
		t.Fatal("cancel hid the anchor failure", e)
	}
	// Creating a request records its original anchor.
	g := setup(t)
	g.e.Break("capability_request_anchors", "INSERT")
	p := Create{Draft: Draft{Title: "Favorite", Description: "Save a favorite", ProposedContract: raw(object{"args": object{}}), Acceptance: []string{"An explicit click saves the choice"}}, Environment: "staging", ExpectedManifestHash: g.hash, ChangesetID: g.cs, ExpectedSeq: 1, DocumentID: g.doc, NodeID: "n_root", Event: "click", Key: "store.favorite"}
	if _, e := (githubFixture{fixture: g}).call(g.a, "create-capability-request", p, ""); e == nil || cmstest.Code(e) != "" {
		t.Fatal("create hid the anchor failure", e)
	}
}
