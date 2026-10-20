package capabilities

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// STU-336/337/344/346: every anchor command refusal is explicit.
func TestSTU336_346AnchorCommandRefusals(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	cs2, doc2 := f.page("second")
	attach := func(p AttachRequest) error {
		_, e := f.call(f.a, "attach-capability-request", p, "")
		return e
	}
	env, _ := f.e.Q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: f.a.ProjectID, Name: "staging"})
	m, _ := f.e.Q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: f.a.ProjectID, ID: *env.ActiveManifestID})
	good := AttachRequest{RequestID: f.githubFixture.request.ID, ExpectedRequestRevision: f.revision(), ChangesetID: cs2, ExpectedSeq: f.seq(cs2), DocumentID: doc2, NodeID: "n_root", Event: "click", ExpectedManifestHash: m.Hash}
	stale := good
	stale.ExpectedRequestRevision = 99
	expectCode(t, attach(stale), "CAPABILITY_REVISION_CONFLICT")
	unknown := good
	unknown.RequestID = uuid.New()
	expectCode(t, attach(unknown), "NOT_FOUND")
	if _, e := f.call(f.engineer, "attach-capability-request", good, ""); e == nil {
		t.Fatal("STU-336 attach needs design rights")
	}
	// An existing chain needs explicit confirmation.
	f.e.Must(f.a, "apply-operations", object{"changesetId": cs2, "expectedSeq": f.seq(cs2), "operations": []any{object{"type": "node.setBehavior", "target": doc2, "payload": object{"nodeId": "n_root", "event": "click", "handlers": []any{object{"action": "scrollTo", "args": object{"target": object{"lit": "n_root"}}}}}}}}, nil)
	good.ExpectedSeq = f.seq(cs2)
	expectCode(t, attach(good), "CAPABILITY_REPLACE_CONFIRM_REQUIRED")
	good.ConfirmReplaceExisting = true
	view := f.command(f.a, "attach-capability-request", good)
	anchor := obj(view["anchor"])
	id := uuid.MustParse(anchor["id"].(string))
	rev := int64(anchor["revision"].(float64))

	action := func(name string, p AnchorAction) error {
		_, e := f.call(f.a, name, p, "")
		return e
	}
	base := AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: id, ExpectedAnchorRevision: rev}
	wrong := base
	wrong.ExpectedAnchorRevision = 99
	expectCode(t, action("retry-capability-fulfillment", wrong), "CAPABILITY_REVISION_CONFLICT")
	expectCode(t, action("retry-capability-fulfillment", base), "CAPABILITY_STATE_INVALID")
	args := base
	args.Handlers = raw([]any{object{"action": "other"}})
	expectCode(t, action("set-capability-fulfillment-args", args), "CAPABILITY_FULFILLMENT_ARGS_INVALID")
	foreign := base
	foreign.AnchorID = uuid.New()
	expectCode(t, action("retry-capability-fulfillment", foreign), "NOT_FOUND")
	if _, e := f.call(f.engineer, "retry-capability-fulfillment", base, ""); e == nil {
		t.Fatal("STU-337 only the anchor owner manages it")
	}
	original := anchorFor(f.anchors(), f.cs)
	expectCode(t, action("detach-capability-request", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: original.ID, ExpectedAnchorRevision: original.Revision, ExpectedSeq: f.seq(f.cs)}), "CAPABILITY_STATE_INVALID")
	detach := base
	detach.ExpectedSeq = f.seq(cs2)
	f.command(f.a, "detach-capability-request", detach)
	if anchorFor(f.anchors(), cs2).State != "detached" || f.clickOf2(cs2, doc2) != nil {
		t.Fatal("STU-337 detach removes the marker")
	}
	again := detach
	again.ExpectedAnchorRevision = anchorFor(f.anchors(), cs2).Revision
	expectCode(t, action("detach-capability-request", again), "CAPABILITY_STATE_INVALID")
	// Limits of a group (STU-335).
	f.e.Exec(`INSERT INTO capability_request_anchors (id, project_id, request_id, changeset_id, document_id, node_id, event, owner_id)
		SELECT gen_random_uuid(), project_id, request_id, changeset_id, document_id, 'n_fake_' || g, event, owner_id
		FROM capability_request_anchors, generate_series(1, 50) g WHERE original`)
	good.ExpectedSeq = f.seq(cs2)
	good.ExpectedRequestRevision = f.revision()
	expectCode(t, attach(good), "CAPABILITY_ANCHOR_LIMIT")
	// Lookup of an open request for attachment.
	if v, e := OpenRequestByKey(ctx, f.e.Q, f.a, "staging", "store.favorite"); e != nil || v["id"] != f.githubFixture.request.ID {
		t.Fatal(v, e)
	}
	if _, e := OpenRequestByKey(ctx, f.e.Q, f.a, "staging", "missing.key"); e == nil {
		t.Fatal("unknown key")
	}
	if _, e := OpenRequestByKey(ctx, f.e.Q, f.engineer, "staging", "store.favorite"); e == nil {
		t.Fatal("engineer cannot attach")
	}
}

// clickOf2 returns the click chain or nil when the event is gone.
func (f *fulfillmentFixture) clickOf2(cs, doc uuid.UUID) any {
	f.e.T.Helper()
	ctx := context.Background()
	rows, _ := f.e.Q.ChangesetWorkingVersions(ctx, cs)
	for _, v := range rows {
		if v.ObjectID == doc {
			return obj(obj(obj(parse(v.Body)["nodes"])["n_root"])["on"])["click"]
		}
	}
	return nil
}

// STU-353: storage failures are reported and never become a fulfillment.
func TestSTU353FulfillmentStorageFailures(t *testing.T) {
	ctx := context.Background()
	for _, table := range []string{"capability_fulfillment_jobs", "capability_github_links", "capability_github_observations", "code_index_uploads", "capability_fulfillment_audit", "capability_request_anchors"} {
		t.Run(table, func(t *testing.T) {
			f := fulfillmentSetup(t)
			f.merged()
			f.deploy(mergeSHA, object{"value": object{"type": "string"}})
			f.e.Break(table, "")
			if e := f.svc.Run(ctx); e == nil && table != "capability_fulfillment_audit" && !f.retried() {
				t.Fatal("storage failure hidden")
			}
			if r := f.current(); r.Status == "fulfilled" && f.clickOf(f.cs, f.doc)["action"] == "store.favorite" && table != "capability_fulfillment_audit" {
				t.Fatal("partial fulfillment")
			}
		})
	}
	t.Run("replacement audit", func(t *testing.T) {
		f := fulfillmentSetup(t)
		f.merged()
		f.deploy(mergeSHA, object{"value": object{"type": "string"}})
		f.e.Break("capability_fulfillment_audit", "INSERT")
		if e := f.svc.Run(ctx); e == nil && !f.retried() {
			t.Fatal("audit failure hidden")
		}
		if f.current().Status == "fulfilled" {
			t.Fatal("decision committed without its audit")
		}
	})
	t.Run("queries", func(t *testing.T) {
		f := fulfillmentSetup(t)
		f.e.Break("capability_request_anchors", "")
		if _, e := FulfillmentGet(ctx, f.e.Q, f.a, f.githubFixture.request.ID); e == nil {
			t.Fatal("anchors failure hidden")
		}
		if _, e := f.call(f.engineer, "check-capability-fulfillment", CheckFulfillment{RequestID: f.githubFixture.request.ID}, ""); e != nil && !strings.Contains(e.Error(), "") {
			t.Fatal(e)
		}
	})
	t.Run("stale refresh", func(t *testing.T) {
		f := fulfillmentSetup(t)
		f.merged()
		f.e.Exec(`UPDATE capability_github_observations SET fresh_until=now()-interval '1 minute'`)
		f.e.Exec(`UPDATE capability_github_links SET refresh_after=now()-interval '1 minute', state='idle'`)
		f.deploy(mergeSHA, object{"value": object{"type": "string"}})
		f.run()
		if l := f.row(); l.State != "queued" || f.current().FulfillmentReason != "observation_stale" {
			t.Fatal("STU-340 a stale observation requests fresh facts", l.State, f.current().FulfillmentReason)
		}
		f.sync()
		f.run()
		if f.current().Status != "fulfilled" {
			t.Fatal("decision §11.8 the merged observation re-evaluates", f.current().FulfillmentReason)
		}
	})
}

// retried reports a job put back with a temporary error (STU-340 bounded retry).
func (f *fulfillmentFixture) retried() bool {
	var n int
	f.e.Pool.QueryRow(context.Background(), `SELECT count(*) FROM capability_fulfillment_jobs WHERE state='queued' AND last_error='temporary' AND attempts=1`).Scan(&n)
	return n > 0
}
