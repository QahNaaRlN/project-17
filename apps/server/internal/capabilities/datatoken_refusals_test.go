package capabilities

import (
	"context"
	"testing"

	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// STU-375/376/380/381: drafts of both kinds refuse bad keys, contracts,
// placements and params; a draft does not change IR.
func TestSTU375_381DataTokenCreateRefusals(t *testing.T) {
	f := setup(t)
	f.doc = f.promoPage("/data")
	g := githubFixture{fixture: f}
	create := func(base func() Create, change func(*Create)) error {
		p := base()
		change(&p)
		_, e := g.call(f.a, "create-capability-request", p, "")
		return e
	}
	data, token := f.dataSourceCreate, f.tokenCreate
	expectCode(t, create(data, func(p *Create) { p.Key = "Recommendations" }), "CAPABILITY_REQUEST_INVALID")
	expectCode(t, create(data, func(p *Create) { p.Placement = &Placement{NodeID: "n_box"} }), "CAPABILITY_REQUEST_INVALID")
	expectCode(t, create(data, func(p *Create) { p.ProposedContract = raw(object{"params": object{}}) }), "CAPABILITY_CONTRACT_INVALID")
	expectCode(t, create(data, func(p *Create) {
		p.ProposedContract = raw(object{"result": object{"type": "text", "content": true}})
	}), "CAPABILITY_CONTRACT_INVALID")
	expectCode(t, create(data, func(p *Create) { p.InitialProps = raw(object{}) }), "CAPABILITY_PROPS_INVALID")
	expectCode(t, create(data, func(p *Create) { p.InitialProps = raw(object{"limit": object{"lit": "four"}}) }), "CAPABILITY_PROPS_INVALID")
	expectCode(t, create(data, func(p *Create) { p.InitialProps = raw(object{"limit": object{"lit": 4}, "x": "$content.y"}) }), "CAPABILITY_PROPS_INVALID")
	expectCode(t, create(data, func(p *Create) { p.Placement = &Placement{DataSourceKey: "content"} }), "CAPABILITY_PLACEMENT_INVALID")
	expectCode(t, create(token, func(p *Create) { p.Key = "typography.big" }), "CAPABILITY_REQUEST_INVALID")
	expectCode(t, create(token, func(p *Create) { p.ProposedContract = raw(object{"category": "colors", "value": "#fff"}) }), "CAPABILITY_CONTRACT_INVALID")
	expectCode(t, create(token, func(p *Create) { p.ProposedContract = raw(object{"category": "spacing", "value": "huge"}) }), "CAPABILITY_CONTRACT_INVALID")
	expectCode(t, create(token, func(p *Create) { p.Placement = &Placement{NodeID: "n_box", Property: "background"} }), "CAPABILITY_PLACEMENT_INVALID")
	expectCode(t, create(token, func(p *Create) { p.Placement = &Placement{NodeID: "n_none", Property: "padding"} }), "CAPABILITY_PLACEMENT_INVALID")
	expectCode(t, create(token, func(p *Create) { p.Placement = &Placement{NodeID: "n_box", Property: "padding", Breakpoint: "md"} }), "CAPABILITY_PLACEMENT_INVALID")
	before := f.seq()
	if r := must(f.e, f.a, "create-capability-request", f.dataSourceCreate()); r.Request["kind"] != KindDataSource || f.seq() != before {
		t.Fatal("STU-381 a draft keeps the page", r.Request)
	}
	expectCode(t, create(data, func(*Create) {}), "CAPABILITY_KEY_TAKEN")
	if r := must(f.e, f.a, "create-capability-request", f.tokenCreate()); r.Request["kind"] != KindToken || r.Request["event"] != "padding" {
		t.Fatal("STU-383 the token anchor path", r.Request)
	}
}

// STU-384/385: another Change Set joins with its own key; a manual source
// marker or a copied placeholder without anchor is refused; detach removes it.
func TestSTU384_385DataTokenInstances(t *testing.T) {
	f := dataSourceSetup(t)
	r := f.current()
	var cs changes.Changeset
	f.e.Must(f.a, "create-changeset", object{"title": "second", "targets": []string{"staging"}}, &cs)
	var made changes.ApplyResult
	f.e.Must(f.a, "apply-operations", object{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{object{"type": "document.create", "payload": object{"kind": "page", "path": "/second", "root": object{"id": "n_box", "type": "Box"}}}}}, &made)
	doc := made.Operations[0].Target
	apply := func(op object) error {
		op["target"] = doc
		c, _ := f.e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: cs.ID})
		return f.e.Do(f.a, "apply-operations", object{"changesetId": cs.ID, "expectedSeq": c.Seq, "operations": []any{op}}, nil)
	}
	expectCode(t, apply(object{"type": "document.setDataSource", "payload": object{"key": "copy", "use": object{"source": "store.recommendations", "pending": r.ID.String(), "params": object{"limit": object{"lit": 1}}}}}), "CAPABILITY_PENDING_INVALID")
	p := AttachRequest{RequestID: r.ID, ExpectedRequestRevision: f.revision(), ChangesetID: cs.ID, ExpectedSeq: f.seq(cs.ID), DocumentID: doc, ExpectedManifestHash: f.hash, Placement: &Placement{DataSourceKey: "related"}, InitialProps: raw(object{"limit": object{"lit": 2}})}
	f.command(f.a, "attach-capability-request", p)
	joined := anchorFor(f.anchors(), cs.ID)
	if joined.NodeID != "related" || joined.AnchorKind != "source" {
		t.Fatal("STU-384 the joined page key", joined)
	}
	f.command(f.a, "detach-capability-request", AnchorAction{RequestID: r.ID, AnchorID: joined.ID, ExpectedAnchorRevision: joined.Revision, ExpectedSeq: f.seq(cs.ID)})
	if anchorFor(f.anchors(), cs.ID).State != "detached" {
		t.Fatal("STU-384 detached")
	}
	d, _ := changes.GetDocument(context.Background(), f.e.Q, f.a.ProjectID, doc, &cs.ID)
	if obj(parse(d.Body)["dataSources"])["related"] != nil {
		t.Fatal("STU-384 detach removes the placeholder")
	}
}

// STU-390: cms-system may only drop the source marker or write the token
// name of a fulfilled request; nothing else.
func TestSTU390SystemRuleForDataAndTokens(t *testing.T) {
	ctx := context.Background()
	try := func(f *fulfillmentFixture, op string, payload object) error {
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
	d := dataSourceSetup(t)
	drop := object{"key": "recommended", "use": object{"source": "store.recommendations", "params": object{"limit": object{"lit": 4}}}}
	expectCode(t, try(d, "document.setDataSource", drop), "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN")
	d.e.Exec(`UPDATE capability_requests SET status='fulfilled'`)
	other := object{"key": "recommended", "use": object{"source": "store.recommendations", "params": object{"limit": object{"lit": 9}}}}
	expectCode(t, try(d, "document.setDataSource", other), "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN")
	expectCode(t, try(d, "document.setDataSource", object{"key": "recommended", "use": nil}), "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN")
	if e := try(d, "document.setDataSource", drop); e != nil {
		t.Fatal("STU-390 the exact marker drop", e)
	}
	k := tokenSetup(t)
	name := object{"nodeId": "n_box", "set": object{"padding": "huge"}}
	expectCode(t, try(k, "node.setDesign", name), "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN")
	k.e.Exec(`UPDATE capability_requests SET status='fulfilled'`)
	expectCode(t, try(k, "node.setDesign", object{"nodeId": "n_box", "set": object{"padding": "md"}}), "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN")
	expectCode(t, try(k, "node.setDesign", object{"nodeId": "n_box", "set": object{"padding": "huge", "gap": "huge"}}), "CAPABILITY_SYSTEM_OPERATION_FORBIDDEN")
	m := parse([]byte(cmstest.DefaultManifest))
	m["tokens"] = object{"spacing": object{"md": "16px", "huge": "48px"}}
	k.e.ActivateManifest("staging", raw(m))
	if e := try(k, "node.setDesign", name); e != nil {
		t.Fatal("STU-390 the exact token name", e)
	}
}

// STU-389: shapes, not bounds, decide data source compatibility.
func TestSTU389DataSourceShapes(t *testing.T) {
	r := store.CapabilityRequest{Kind: KindDataSource, ProposedContract: raw(recommendationsContract())}
	real := func(result object, params object) object {
		return object{"params": params, "result": result}
	}
	limit := object{"limit": object{"type": "number", "integer": true, "min": 1, "max": 64, "required": true}}
	list := func(fields object) object {
		return object{"type": "list", "max": 64, "of": object{"type": "object", "fields": fields}}
	}
	if !dataSourceCompatible(r, real(list(object{"title": object{"type": "text", "maxLength": 2000}, "price": object{"type": "number"}}), limit)) {
		t.Fatal("bounds and extra fields are compatible")
	}
	for name, actual := range map[string]object{
		"missing":     nil,
		"no title":    real(list(object{"price": object{"type": "number"}}), limit),
		"object":      real(object{"type": "object", "fields": object{}}, limit),
		"param type":  real(list(object{"title": object{"type": "string"}}), object{"limit": object{"type": "string"}}),
		"extra param": real(list(object{"title": object{"type": "string"}}), object{"limit": object{"type": "number"}, "region": object{"type": "string", "required": true}}),
		"paginated":   {"params": limit, "result": list(object{"title": object{"type": "string"}}), "paginated": true},
	} {
		if dataSourceCompatible(r, actual) {
			t.Error("incompatible", name)
		}
	}
	enum := object{"type": "enum", "values": []any{"a", "b"}}
	if !shapeCompatible(enum, object{"type": "enum", "values": []any{"a", "b", "c"}}, 0) || shapeCompatible(enum, object{"type": "enum", "values": []any{"a"}}, 0) {
		t.Error("enum values")
	}
	ref := object{"type": "reference", "schema": "Product"}
	if !shapeCompatible(ref, ref, 0) || shapeCompatible(ref, object{"type": "reference", "schema": "Other"}, 0) || shapeCompatible(ref, ref, 9) {
		t.Error("reference schema and depth")
	}
}
