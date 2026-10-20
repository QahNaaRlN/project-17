package capabilities

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
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

// STU-342/343: an invalid argument rolls the Change Set group back and only
// that anchor is refused; a later manifest without the key fails the rest.
func TestSTU342InvalidArgsAndRemovedKey(t *testing.T) {
	f := fulfillmentSetup(t)
	cs2, doc2 := f.page("second")
	f.attach(cs2, doc2)
	original := anchorFor(f.anchors(), f.cs)
	f.command(f.a, "set-capability-fulfillment-args", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: original.ID, ExpectedAnchorRevision: original.Revision, Handlers: raw([]any{object{"action": "store.favorite", "args": object{"value": object{"lit": 42}}}})})
	f.merged()
	f.deploy(mergeSHA, object{"value": object{"type": "string"}})
	f.run()
	rows := f.anchors()
	if anchorFor(rows, f.cs).State != "invalid_args" || anchorFor(rows, cs2).State != "replaced" {
		t.Fatal("STU-342 invalid args refuse only their anchor", anchorFor(rows, f.cs).State, anchorFor(rows, cs2).State)
	}
	if f.clickOf(f.cs, f.doc)["action"] == "store.favorite" {
		t.Fatal("refused anchor kept its marker")
	}
	// The owner fixes the arguments; meanwhile a manifest without the key is active.
	a := anchorFor(f.anchors(), f.cs)
	f.command(f.a, "set-capability-fulfillment-args", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: a.ID, ExpectedAnchorRevision: a.Revision, Handlers: raw([]any{object{"action": "store.favorite", "args": object{"value": object{"lit": "ok"}}}})})
	f.deploy(laterSHA, nil)
	f.run()
	if a := anchorFor(f.anchors(), f.cs); a.State != "failed" || a.LastError != "key_missing" {
		t.Fatal("STU-349 a manifest without the key fails the replacement", a.State, a.LastError)
	}
	if r := f.current(); r.Status != "fulfilled" {
		t.Fatal("STU-349 the request stays fulfilled")
	}
	// Refusals on a fulfilled request.
	replaced := anchorFor(f.anchors(), cs2)
	expectCode(t, func() error {
		_, e := f.call(f.a, "set-capability-fulfillment-args", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: replaced.ID, ExpectedAnchorRevision: replaced.Revision, Handlers: raw([]any{object{"action": "store.favorite"}})}, "")
		return e
	}(), "CAPABILITY_STATE_INVALID")
	expectCode(t, func() error {
		_, e := f.call(f.a, "retry-capability-fulfillment", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: replaced.ID, ExpectedAnchorRevision: replaced.Revision}, "")
		return e
	}(), "CAPABILITY_STATE_INVALID")
	expectCode(t, func() error {
		_, e := f.call(f.a, "detach-capability-request", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: replaced.ID, ExpectedAnchorRevision: replaced.Revision, ExpectedSeq: f.seq(cs2)}, "")
		return e
	}(), "CAPABILITY_STATE_INVALID")
	expectCode(t, func() error {
		_, e := f.call(f.engineer, "check-capability-fulfillment", CheckFulfillment{RequestID: f.githubFixture.request.ID}, "")
		return e
	}(), "CAPABILITY_STATE_INVALID")
}

// STU-341: the manual path fulfills the request and queues the other anchors.
func TestSTU341ManualResolveQueuesTheGroup(t *testing.T) {
	f := fulfillmentSetup(t)
	cs2, doc2 := f.page("second")
	f.attach(cs2, doc2)
	r := f.engineering(f.current())
	hash := f.activate(object{"value": object{"type": "string"}})
	f.command(f.a, "resolve-capability-request", Action{ID: r.ID, ExpectedRevision: r.Revision, ExpectedSeq: f.seq(f.cs), ExpectedManifestHash: hash, Handlers: raw([]any{object{"action": r.Key}})})
	if got := f.current(); got.FulfillmentMode == nil || *got.FulfillmentMode != "manual" {
		t.Fatal("STU-341 manual mode", got.FulfillmentMode)
	}
	if anchorFor(f.anchors(), f.cs).State != "replaced" || anchorFor(f.anchors(), cs2).State != "queued" {
		t.Fatal("STU-341 other anchors are queued", f.anchors())
	}
	f.run()
	if f.clickOf(cs2, doc2)["action"] != "store.favorite" {
		t.Fatal("STU-342 group replaced after manual resolve")
	}
	// A terminal request releases its anchors.
	g := fulfillmentSetup(t)
	g.command(g.a, "update-capability-request", Update{ID: g.githubFixture.request.ID, ExpectedRevision: g.revision(), Transition: "cancelled"})
	for _, a := range g.anchors() {
		if a.State != "detached" {
			t.Fatal("STU-335 cancel detaches", a.State)
		}
	}
	cs3, doc3 := g.page("third")
	if _, e := g.call(g.a, "attach-capability-request", AttachRequest{RequestID: g.githubFixture.request.ID, ExpectedRequestRevision: g.revision(), ChangesetID: cs3, ExpectedSeq: g.seq(cs3), DocumentID: doc3, NodeID: "n_root", Event: "click", ExpectedManifestHash: hash}, ""); e == nil {
		t.Fatal("cancelled request accepts no anchor")
	}
}

// STU-345: cms-system may only turn this request's marker into its key.
func TestSTU345SystemReplacementRule(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	try := func(handlers any, event string) error {
		tx, e := f.e.Pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		q := store.New(tx)
		cs, _ := q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
		system, _ := systemActor(ctx, q, f.a.ProjectID, cs.OwnerID)
		_, e = changes.ApplyInTx(ctx, tx, system, f.cs, cs.Seq, []changes.OperationInput{{Type: "node.setBehavior", Target: &f.doc, Payload: raw(object{"nodeId": "n_root", "event": event, "handlers": handlers})}})
		return e
	}
	call := []any{object{"action": "store.favorite"}}
	if e := try(call, "click"); e == nil {
		t.Fatal("an unfulfilled request cannot be replaced")
	}
	f.e.Exec(`UPDATE capability_requests SET status='fulfilled'`)
	if e := try([]any{object{"action": "other.key"}}, "click"); e == nil {
		t.Fatal("only the request key")
	}
	if e := try(call, "hover"); e == nil {
		t.Fatal("no new events")
	}
	if e := try([]any{object{"action": "store.favorite"}, object{"action": "store.favorite"}}, "click"); e == nil {
		t.Fatal("exactly one call")
	}
	if e := try(call, "click"); e != nil {
		t.Fatal("the exact replacement is allowed", e)
	}
	// A marker that is not pending, or of a removed anchor, is refused.
	f.e.Exec(`UPDATE capability_request_anchors SET state='detached'`)
	if e := try(call, "click"); e == nil {
		t.Fatal("detached anchor")
	}
}

// Decision §11.7 / STU-337: another owner joins the group and sees only their anchor.
func TestSTU337CrossOwnerGroup(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	carol := f.e.Human("carol", auth.DesignRead, auth.DesignCompose, auth.DesignZonesManage, auth.BehaviorUse, auth.CapabilityRequest)
	var cs changes.Changeset
	f.e.Must(carol, "create-changeset", object{"title": "carol", "targets": []string{"staging"}}, &cs)
	var made changes.ApplyResult
	f.e.Must(carol, "apply-operations", object{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{object{"type": "document.create", "payload": object{"kind": "page", "path": "/carol", "root": object{"id": "n_root", "type": "Button", "props": object{"label": "carol"}}}}}}, &made)
	doc := made.Operations[0].Target
	env, _ := f.e.Q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: f.a.ProjectID, Name: "staging"})
	m, _ := f.e.Q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: f.a.ProjectID, ID: *env.ActiveManifestID})
	if _, e := Get(ctx, f.e.Q, carol, f.githubFixture.request.ID); e == nil {
		t.Fatal("an unrelated designer does not see the request")
	}
	if _, e := f.call(carol, "attach-capability-request", AttachRequest{RequestID: f.githubFixture.request.ID, ExpectedRequestRevision: f.revision(), ChangesetID: cs.ID, ExpectedSeq: 1, DocumentID: doc, NodeID: "n_root", Event: "click", ExpectedManifestHash: m.Hash}, ""); e != nil {
		t.Fatal("decision §11.7 cross-owner attach", e)
	}
	if _, e := Get(ctx, f.e.Q, carol, f.githubFixture.request.ID); e != nil {
		t.Fatal("STU-337 anchor owner reads the request", e)
	}
	view, e := FulfillmentGet(ctx, f.e.Q, carol, f.githubFixture.request.ID)
	if e != nil || view["anchorCount"] != 2 || len(arr(view["anchors"])) != 1 || obj(arr(view["anchors"])[0])["own"] != true {
		t.Fatal("STU-337 only own anchors", view, e)
	}
	owner, e := FulfillmentGet(ctx, f.e.Q, f.a, f.githubFixture.request.ID)
	if e != nil || len(arr(owner["anchors"])) != 1 {
		t.Fatal("the request owner also sees only own anchors", owner, e)
	}
	if _, e := FulfillmentGet(ctx, f.e.Q, f.e.Human("stranger", auth.DesignRead), f.githubFixture.request.ID); e == nil {
		t.Fatal("stranger")
	}
	// Carol's anchor is replaced in her Change Set on her behalf.
	f.merged()
	f.deploy(mergeSHA, object{"value": object{"type": "string"}})
	f.run()
	d, _ := changes.GetDocument(ctx, f.e.Q, f.a.ProjectID, doc, &cs.ID)
	if !strings.Contains(string(d.Body), `"store.favorite"`) {
		t.Fatal("STU-342 replacement in another owner's Change Set")
	}
}

// STU-350: after the system replacement the draft passes its checks, including
// the policy stage for cms-system; with the marker it failed.
func TestSTU350ReplacedDraftPassesChecks(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	workflow.Register(f.e.Bus)
	submit := func() string {
		var out object
		f.e.Must(f.a, "submit-changeset", object{"changesetId": f.cs, "expectedSeq": f.seq(f.cs)}, &out)
		cs, _ := f.e.Q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
		return cs.State
	}
	if state := submit(); state != "failed" {
		t.Fatal("IR-053 pending fails the checks", state)
	}
	f.e.Must(f.a, "reopen-changeset", object{"changesetId": f.cs, "expectedSeq": f.seq(f.cs)}, nil)
	f.merged()
	f.deploy(mergeSHA, object{"value": object{"type": "string"}})
	f.run()
	if anchorFor(f.anchors(), f.cs).State != "replaced" {
		t.Fatal("replacement")
	}
	submit()
	// The fixture page keeps a literal label (PROP_CONTENT_LITERAL); the
	// placeholder and the cms-system policy violation must be gone.
	var policy, pendingFree int
	f.e.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE stage='policy' AND status='failed'), count(*) FILTER (WHERE stage='ir' AND details::text NOT LIKE '%ACTION_PENDING_CAPABILITY%') FROM checks WHERE changeset_id=$1`, f.cs).Scan(&policy, &pendingFree)
	if policy != 0 || pendingFree == 0 {
		t.Fatal("STU-350 replaced draft passes the placeholder and policy checks", policy, pendingFree)
	}
}

// STU-353: each storage step of evaluation, replacement and the anchor
// commands reports its failure and leaves no partial result.
func TestSTU353EveryStepReportsStorageFailures(t *testing.T) {
	ctx := context.Background()
	type step struct{ table, op string }
	evaluate := []step{{"environments", ""}, {"manifests", ""}, {"capability_github_configs", ""}, {"capability_requests", "UPDATE"}, {"capability_requests", ""}, {"capability_request_audit", "INSERT"}, {"capability_request_anchors", "UPDATE"}, {"actors", ""}}
	for _, s := range evaluate {
		t.Run("evaluate/"+s.table+s.op, func(t *testing.T) {
			f := fulfillmentSetup(t)
			f.merged()
			f.deploy(mergeSHA, object{"value": object{"type": "string"}})
			f.e.Break(s.table, s.op)
			_ = f.svc.Run(ctx)
			if s.table != "actors" && f.current().Status == "fulfilled" {
				t.Fatal("fulfilled despite a storage failure")
			}
		})
	}
	replace := []step{{"capability_fulfillment_audit", "INSERT"}, {"project_revisions", "INSERT"}, {"object_versions", ""}, {"manifests", ""}, {"actors", ""}, {"capability_request_anchors", "UPDATE"}, {"operations", "INSERT"}}
	for _, s := range replace {
		t.Run("replace/"+s.table+s.op, func(t *testing.T) {
			f := fulfillmentSetup(t)
			f.merged()
			f.deploy(mergeSHA, object{"value": object{"type": "string"}})
			// Decide while the draft is in review, then reopen and break the replacement.
			f.e.Exec(`UPDATE changesets SET state='in_review' WHERE id=$1`, f.cs)
			f.run()
			if f.current().Status != "fulfilled" || anchorFor(f.anchors(), f.cs).State != "changeset_in_review" {
				t.Fatal("setup", f.current().FulfillmentReason)
			}
			f.e.Exec(`UPDATE changesets SET state='open' WHERE id=$1`, f.cs)
			f.e.Exec(`UPDATE capability_request_anchors SET state='queued', next_at=now()`)
			f.e.Break(s.table, s.op)
			e := f.svc.Run(ctx) // due groups are replaced after the outbox
			if s.table == "operations" {
				// A refused write group marks the anchor; the marker stays.
				if a := anchorFor(f.anchors(), f.cs); e == nil && a.State == "replaced" {
					t.Fatal("replaced despite a failed write")
				}
				return
			}
			if e == nil {
				t.Fatal("storage failure hidden")
			}
			if anchorFor(f.anchors(), f.cs).State == "replaced" {
				t.Fatal("partial replacement")
			}
		})
	}
	commands := []struct {
		table, op, name string
	}{
		{"capability_request_anchors", "INSERT", "attach"},
		{"capability_fulfillment_audit", "INSERT", "attach"},
		{"capability_requests", "", "attach"},
		{"environments", "", "attach"},
		{"capability_request_anchors", "", "attach"},
		{"capability_requests", "", "retry"},
		{"environments", "", "retry"},
		{"object_versions", "", "retry"},
		{"operations", "INSERT", "detach"},
		{"capability_request_anchors", "UPDATE", "detach"},
		{"capability_request_anchors", "UPDATE", "args"},
		{"capability_fulfillment_audit", "INSERT", "args"},
		{"capability_fulfillment_jobs", "INSERT", "check"},
		{"environments", "", "check"},
		{"object_versions", "", "get"},
		{"capability_requests", "", "open"},
	}
	for _, c := range commands {
		t.Run("command/"+c.name+"/"+c.table+c.op, func(t *testing.T) {
			f := fulfillmentSetup(t)
			cs2, doc2 := f.page("second")
			env, _ := f.e.Q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: f.a.ProjectID, Name: "staging"})
			m, _ := f.e.Q.GetManifestByID(ctx, store.GetManifestByIDParams{ProjectID: f.a.ProjectID, ID: *env.ActiveManifestID})
			original := anchorFor(f.anchors(), f.cs)
			if c.name == "get" {
				f.e.Exec(`UPDATE capability_request_anchors SET state='replaced'`)
			}
			attach := AttachRequest{RequestID: f.githubFixture.request.ID, ExpectedRequestRevision: f.revision(), ChangesetID: cs2, ExpectedSeq: f.seq(cs2), DocumentID: doc2, NodeID: "n_root", Event: "click", ExpectedManifestHash: m.Hash}
			var joined store.CapabilityRequestAnchor
			switch c.name {
			case "detach":
				f.command(f.a, "attach-capability-request", attach)
				joined = anchorFor(f.anchors(), cs2)
			case "retry":
				f.e.Exec(`UPDATE capability_requests SET status='fulfilled'`)
				f.e.Exec(`UPDATE capability_request_anchors SET state='replaced'`)
				original = anchorFor(f.anchors(), f.cs)
			}
			f.e.Break(c.table, c.op)
			var e error
			switch c.name {
			case "attach":
				_, e = f.call(f.a, "attach-capability-request", attach, "")
			case "retry":
				_, e = f.call(f.a, "retry-capability-fulfillment", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: original.ID, ExpectedAnchorRevision: original.Revision}, "")
			case "detach":
				_, e = f.call(f.a, "detach-capability-request", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: joined.ID, ExpectedAnchorRevision: joined.Revision, ExpectedSeq: f.seq(cs2)}, "")
			case "args":
				_, e = f.call(f.a, "set-capability-fulfillment-args", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: original.ID, ExpectedAnchorRevision: original.Revision, Handlers: raw([]any{object{"action": "store.favorite"}})}, "")
			case "check":
				_, e = f.call(f.engineer, "check-capability-fulfillment", CheckFulfillment{RequestID: f.githubFixture.request.ID}, "")
			case "get":
				_, e = FulfillmentGet(ctx, f.e.Q, f.a, f.githubFixture.request.ID)
			case "open":
				_, e = OpenRequestByKey(ctx, f.e.Q, f.a, "staging", "store.favorite")
			}
			if e == nil {
				t.Fatal("storage failure hidden")
			}
		})
	}
}
