package capabilities

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// activateComponent activates a staging manifest with the given components.
func (f *fixture) activateComponent(components object) string {
	m := parse([]byte(cmstest.DefaultManifest))
	m["components"] = components
	f.e.ActivateManifest("staging", raw(m))
	h, _ := manifest.Hash(m)
	return h
}

// STU-365: the pack names the placement, initial props and similar components.
func TestSTU365ComponentPack(t *testing.T) {
	f := setup(t)
	f.doc = f.promoPage("/promo")
	r := must(f.e, f.a, "create-capability-request", f.componentCreate())
	row, _ := f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: uuid.MustParse(r.Request["id"].(string))})
	out := must(f.e, f.a, "submit-capability-request", Action{ID: row.ID, ExpectedRevision: 1, ExpectedSeq: f.seq(), ExpectedManifestHash: f.hash})
	pack, e := f.e.Q.GetCapabilityPack(context.Background(), store.GetCapabilityPackParams{ProjectID: f.a.ProjectID, RequestID: row.ID, Revision: 2})
	if e != nil || out.Pack == nil {
		t.Fatal(e)
	}
	body := parse(pack.Body)
	anchor := obj(body["anchor"])
	if obj(anchor["placement"])["parentId"] != "n_box" || obj(anchor["initialProps"])["title"] != "Sale" || anchor["event"] != nil || obj(body["request"])["kind"] != KindComponent {
		t.Fatal("STU-365 pack anchor", anchor)
	}
	if got := similarComponents(object{"components": object{"PromoCard": object{"props": object{"title": object{}}}, "Footer": object{"props": object{"tone": object{}}}, "Hero": object{"props": object{}}, "Prompt": object{}}}, row, 5); len(got) != 3 || got[0] != "PromoCard" {
		t.Fatal("STU-365 similar components are ranked", got)
	}
}

// STU-368/369: manual resolve checks compatibility and the instance props
// against the real component, then queues the other instances.
func TestSTU368_369ComponentManualResolve(t *testing.T) {
	f := componentSetup(t)
	r := f.engineering(f.current())
	resolve := func(hash string, handlers []any) error {
		p := Action{ID: r.ID, ExpectedRevision: r.Revision, ExpectedSeq: f.seq(f.cs), ExpectedManifestHash: hash}
		if handlers != nil {
			p.Handlers = raw(handlers)
		}
		_, e := f.call(f.a, "resolve-capability-request", p, "")
		return e
	}
	expectCode(t, resolve(f.hash, nil), "CAPABILITY_NOT_AVAILABLE")
	hash := f.activateComponent(object{"PromoBanner": object{"props": object{"title": object{"type": "string", "required": true}}}})
	expectCode(t, resolve(hash, nil), "CAPABILITY_CONTRACT_MISMATCH")
	hash = f.activateComponent(object{"PromoBanner": object{"container": true, "props": object{"title": object{"type": "string", "required": true}, "tone": object{"type": "enum", "values": []any{"plain", "accent"}}}}})
	expectCode(t, resolve(hash, []any{object{"action": "PromoBanner"}}), "CAPABILITY_HANDLERS_INVALID")
	if e := resolve(hash, nil); e != nil {
		t.Fatal("STU-369 manual resolve", e)
	}
	if got := f.current(); got.Status != "fulfilled" || got.FulfillmentMode == nil || *got.FulfillmentMode != "manual" {
		t.Fatal("STU-369 fulfilled manually", got.Status)
	}
	if n := f.node(f.cs, f.doc, r.NodeID); n["pending"] != nil {
		t.Fatal("STU-370 marker cleared", n)
	}
	if v, e := Get(context.Background(), f.e.Q, f.a, r.ID); e != nil || v["fulfillmentReverted"] != false {
		t.Fatal("fulfilled instance", v, e)
	}
}

// STU-370: cms-system clears only the marker of a fulfilled component request.
func TestSTU370SystemClearsOnlyFulfilledMarkers(t *testing.T) {
	f := componentSetup(t)
	ctx := context.Background()
	r := f.current()
	try := func(op string, payload object) error {
		tx, e := f.e.Pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		q := store.New(tx)
		cs, _ := q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
		system, _ := systemActor(ctx, q, f.a.ProjectID, cs.OwnerID)
		_, e = changes.ApplyInTx(ctx, tx, system, f.cs, cs.Seq, []changes.OperationInput{{Type: op, Target: &f.doc, Payload: raw(payload)}})
		return e
	}
	clear := object{"nodeId": r.NodeID}
	expectCode(t, try("node.clearPending", clear), "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN")
	expectCode(t, try("node.setProps", object{"nodeId": r.NodeID, "set": object{"tone": "plain"}}), "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN")
	f.e.Exec(`UPDATE capability_requests SET status='fulfilled'`)
	if e := try("node.clearPending", clear); e != nil {
		t.Fatal("STU-370 the exact clearing is allowed", e)
	}
	f.e.Exec(`UPDATE capability_request_anchors SET state='detached'`)
	expectCode(t, try("node.clearPending", clear), "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN")
}

// STU-367: a compatible component of the PR manifest renders in that
// preview only; the stored draft keeps the placeholder.
func TestSTU367ComponentPreview(t *testing.T) {
	f := previewSetupWith(t, githubSetupWith(t, (*fixture).createComponent))
	ctx := context.Background()
	f.createEnv()
	p := f.buildWith(shaA, func(m object) { m["components"] = object{"PromoBanner": realPromo()} })
	f.register(p)
	if f.state() != PreviewReady {
		t.Fatal("STU-367 ready", f.state())
	}
	node := func(doc object) object { return obj(obj(doc["nodes"])[f.request.NodeID]) }
	if n := node(f.project(f.environment().ID)); n["pending"] != nil || n["type"] != "PromoBanner" {
		t.Fatal("STU-367 the preview renders the real component", n)
	}
	if node(parse(f.body()))["pending"] != f.request.ID.String() {
		t.Fatal("STU-367 the stored draft keeps the placeholder")
	}
	cs, e := ChangesetPreview(ctx, f.e.Q, f.a, f.cs, f.env)
	if e != nil || obj(arr(cs["substitutions"])[0])["substituted"] != true {
		t.Fatal("STU-367 Change Set preview", cs, e)
	}
}
