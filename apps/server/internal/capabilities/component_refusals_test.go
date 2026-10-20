package capabilities

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

// STU-355/356/359/360: component drafts refuse bad keys, contracts,
// placements and initial props; a draft does not change IR.
func TestSTU355_360ComponentCreateRefusals(t *testing.T) {
	f := setup(t)
	f.doc = f.promoPage("/promo")
	create := func(change func(*Create)) error {
		p := f.componentCreate()
		change(&p)
		_, e := (githubFixture{fixture: f}).call(f.a, "create-capability-request", p, "")
		return e
	}
	expectCode(t, create(func(p *Create) { p.Key = "promo" }), "CAPABILITY_REQUEST_INVALID")
	expectCode(t, create(func(p *Create) { p.NodeID, p.Event = "n_box", "click" }), "CAPABILITY_REQUEST_INVALID")
	expectCode(t, create(func(p *Create) { p.Placement = nil }), "CAPABILITY_REQUEST_INVALID")
	expectCode(t, create(func(p *Create) { p.Kind = "token" }), "CAPABILITY_REQUEST_INVALID")
	expectCode(t, create(func(p *Create) {
		p.ProposedContract = raw(object{"props": object{}, "events": object{"click": object{}}})
	}), "CAPABILITY_CONTRACT_INVALID")
	expectCode(t, create(func(p *Create) {
		p.ProposedContract = raw(object{"props": object{"body": object{"type": "text", "content": true}}})
	}), "CAPABILITY_CONTRACT_INVALID")
	expectCode(t, create(func(p *Create) {
		p.ProposedContract = raw(object{"props": object{}, "slots": object{"actions": object{"max": 99}}})
	}), "CAPABILITY_CONTRACT_INVALID")
	expectCode(t, create(func(p *Create) { p.Key = "Box" }), "CAPABILITY_ALREADY_AVAILABLE")
	expectCode(t, create(func(p *Create) { p.Placement = &Placement{ParentID: "n_none"} }), "CAPABILITY_PLACEMENT_INVALID")
	expectCode(t, create(func(p *Create) { p.Placement = &Placement{ParentID: "n_box", Index: 5} }), "CAPABILITY_PLACEMENT_INVALID")
	expectCode(t, create(func(p *Create) { p.Placement = &Placement{ParentID: "n_box", Slot: "footer"} }), "CAPABILITY_PLACEMENT_INVALID")
	expectCode(t, create(func(p *Create) { p.InitialProps = raw(object{"tone": "accent"}) }), "CAPABILITY_PROPS_INVALID")
	expectCode(t, create(func(p *Create) { p.InitialProps = raw(object{"title": "Sale", "size": 1}) }), "CAPABILITY_PROPS_INVALID")
	expectCode(t, create(func(p *Create) { p.InitialProps = raw(object{"title": "Sale", "tone": "loud"}) }), "CAPABILITY_PROPS_INVALID")
	before := f.seq()
	r := must(f.e, f.a, "create-capability-request", f.componentCreate())
	if r.Request["kind"] != KindComponent || f.seq() != before || obj(r.Request["placement"])["parentId"] != "n_box" {
		t.Fatal("STU-360 a draft keeps the page", r.Request)
	}
	expectCode(t, create(func(*Create) {}), "CAPABILITY_KEY_TAKEN")
	// A placement outside the CODE zone is refused.
	f.e.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"FREE"}' WHERE id=$1`, f.a.ProjectID)
	expectCode(t, create(func(p *Create) { p.Key = "OtherBanner" }), "CAPABILITY_CODE_REQUIRED")
}

func (f *fixture) seq() int32 {
	c, _ := f.e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
	return c.Seq
}

// STU-358/364/370: ordinary edits keep the contract; copies, events,
// extraction and an early marker removal are refused; removal is editing.
func TestSTU364PlaceholderOperations(t *testing.T) {
	f := componentSetup(t)
	r := f.current()
	apply := func(op object) error {
		op["target"] = f.doc
		return f.e.Do(f.a, "apply-operations", object{"changesetId": f.cs, "expectedSeq": f.seq(f.cs), "operations": []any{op}}, nil)
	}
	if e := apply(object{"type": "node.setProps", "payload": object{"nodeId": r.NodeID, "set": object{"tone": "plain"}}}); e != nil {
		t.Fatal("STU-364 props by the contract", e)
	}
	copyNode := object{"id": "n_copy", "type": "PromoBanner", "pending": r.ID.String(), "props": object{"title": "Copy"}}
	expectCode(t, apply(object{"type": "node.insert", "payload": object{"parentId": "n_box", "index": 0, "subtree": copyNode}}), "CAPABILITY_PENDING_INVALID")
	expectCode(t, apply(object{"type": "node.setBehavior", "payload": object{"nodeId": r.NodeID, "event": "click", "handlers": []any{object{"action": "scrollTo", "args": object{"target": object{"lit": "n_box"}}}}}}), "OPERATION_INVALID")
	expectCode(t, apply(object{"type": "node.clearPending", "payload": object{"nodeId": r.NodeID}}), "CAPABILITY_PENDING_INVALID")
	if e := apply(object{"type": "component.extract", "payload": object{"nodeId": "n_box", "name": "Wrapper"}}); e == nil {
		t.Fatal("STU-364 extraction of a placeholder")
	}
	if e := apply(object{"type": "node.remove", "payload": object{"nodeId": r.NodeID}}); e != nil {
		t.Fatal("STU-364 removal is ordinary editing", e)
	}
	if v, e := Get(context.Background(), f.e.Q, f.a, r.ID); e != nil || v["anchorChanged"] != true {
		t.Fatal("STU-273 a removed placeholder is an anchor change", v, e)
	}
}

// STU-363: another owner's Change Set joins with its own instance; detaching
// removes it.
func TestSTU363ComponentInstances(t *testing.T) {
	f := componentSetup(t)
	r := f.current()
	var cs changes.Changeset
	f.e.Must(f.a, "create-changeset", object{"title": "second", "targets": []string{"staging"}}, &cs)
	var made changes.ApplyResult
	f.e.Must(f.a, "apply-operations", object{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{object{"type": "document.create", "payload": object{"kind": "page", "path": "/second", "root": object{"id": "n_box", "type": "Box"}}}}}, &made)
	doc := made.Operations[0].Target
	p := AttachRequest{RequestID: r.ID, ExpectedRequestRevision: f.revision(), ChangesetID: cs.ID, ExpectedSeq: f.seq(cs.ID), DocumentID: doc, ExpectedManifestHash: f.hash, Placement: &Placement{ParentID: "n_box"}, InitialProps: raw(object{"title": "Second"})}
	missing := p
	missing.Placement = nil
	missing.NodeID, missing.Event = "n_box", "click"
	_, e := f.call(f.a, "attach-capability-request", missing, "")
	expectCode(t, e, "CAPABILITY_REQUEST_INVALID")
	bad := p
	bad.InitialProps = raw(object{})
	_, e = f.call(f.a, "attach-capability-request", bad, "")
	expectCode(t, e, "CAPABILITY_PROPS_INVALID")
	view := f.command(f.a, "attach-capability-request", p)
	joined := anchorFor(f.anchors(), cs.ID)
	n := f.node(cs.ID, doc, joined.NodeID)
	if n["pending"] != r.ID.String() || obj(n["props"])["title"] != "Second" || joined.Event != "" || obj(view["anchor"])["state"] != "active" {
		t.Fatal("STU-363 the instance is inserted", n, joined)
	}
	_, e = f.call(f.a, "set-capability-fulfillment-args", AnchorAction{RequestID: r.ID, AnchorID: joined.ID, ExpectedAnchorRevision: joined.Revision, Handlers: raw([]any{object{"action": "PromoBanner"}})}, "")
	expectCode(t, e, "CAPABILITY_STATE_INVALID")
	f.command(f.a, "detach-capability-request", AnchorAction{RequestID: r.ID, AnchorID: joined.ID, ExpectedAnchorRevision: joined.Revision, ExpectedSeq: f.seq(cs.ID)})
	if anchorFor(f.anchors(), cs.ID).State != "detached" || f.node(cs.ID, doc, joined.NodeID) != nil {
		t.Fatal("STU-363 detach removes the instance")
	}
}

// STU-357/358: placeholders of live requests are checked by the proposed
// contract and block publication; unknown or nested ones are invalid.
func TestSTU357_358PlaceholderValidation(t *testing.T) {
	f := componentSetup(t)
	ctx := context.Background()
	r := f.current()
	d, e := changes.GetDocument(ctx, f.e.Q, f.a.ProjectID, f.doc, &f.cs)
	if e != nil {
		t.Fatal(e)
	}
	doc := parse(d.Body)
	nodes := obj(doc["nodes"])
	obj(nodes[r.NodeID])["children"] = []any{"n_nested"}
	nodes["n_nested"] = object{"id": "n_nested", "type": "PromoBanner", "pending": r.ID.String(), "props": object{"title": "x"}}
	nodes["n_ghost"] = object{"id": "n_ghost", "type": "GhostBanner", "pending": uuid.NewString()}
	obj(nodes["n_box"])["children"] = append(arr(obj(nodes["n_box"])["children"]), "n_ghost")
	tx, _ := f.e.Pool.Begin(ctx)
	defer tx.Rollback(ctx)
	c, e := validation.Load(ctx, store.New(tx), f.a.ProjectID, "staging", &f.cs)
	if e != nil {
		t.Fatal(e)
	}
	res, e := c.Validate(ctx, validation.Document{ObjectID: f.doc, Body: raw(doc)})
	if e != nil {
		t.Fatal(e)
	}
	codes := map[string]string{}
	for _, diag := range res.Diagnostics {
		codes[diag.Pointer+" "+string(diag.Code)] = diag.NodeID
	}
	for _, want := range []string{"/nodes/" + r.NodeID + "/pending COMPONENT_PENDING_CAPABILITY", "/nodes/n_nested/pending CAPABILITY_PENDING_INVALID", "/nodes/n_ghost/pending CAPABILITY_PENDING_INVALID", "/nodes/n_ghost/type TYPE_UNKNOWN"} {
		if _, ok := codes[want]; !ok {
			t.Error("missing", want, res.Diagnostics)
		}
	}
	for key := range codes {
		if strings.HasPrefix(key, "/nodes/"+r.NodeID+"/props") {
			t.Error("the placeholder props follow the proposed contract", key)
		}
	}
}

// STU-356/359/365/368: pure rules of component contracts, placements,
// compatibility and the pack anchor.
func TestSTU356_368ComponentRules(t *testing.T) {
	for _, bad := range []object{
		{"props": "x"},
		{"props": object{}, "container": "yes"},
		{"props": object{}, "slots": "x"},
		{"props": object{"Bad": object{"type": "string"}}},
		{"props": object{"title": object{"type": "unknownType"}}},
	} {
		if validComponentContract(bad) == nil {
			t.Error("contract refused", bad)
		}
	}
	good := object{"props": object{"item": object{"type": "reference", "schema": "Product"}}, "slots": object{"actions": object{"max": json.Number("2")}}, "container": true}
	if e := validComponentContract(good); e != nil {
		t.Fatal("reference and slot contract", e)
	}
	app := object{"components": object{"Card": object{"slots": object{"free": object{"max": json.Number("1")}, "typed": object{"allowedTypes": []any{"Button"}}}}}, "primitives": object{"Panel": object{"container": true}}}
	card := object{"type": "Card", "slots": object{"free": []any{"n_a"}}}
	for name, c := range map[string]struct {
		parent object
		p      Placement
		want   bool
	}{
		"full slot":     {card, Placement{ParentID: "n_card", Slot: "free"}, false},
		"typed slot":    {card, Placement{ParentID: "n_card", Slot: "typed"}, false},
		"unknown slot":  {card, Placement{ParentID: "n_card", Slot: "none"}, false},
		"free slot":     {object{"type": "Card"}, Placement{ParentID: "n_card", Slot: "free"}, true},
		"primitive":     {object{"type": "Panel"}, Placement{ParentID: "n_card"}, true},
		"unknown type":  {object{"type": "Ghost"}, Placement{ParentID: "n_card"}, false},
		"composed":      {object{"type": "Composed"}, Placement{ParentID: "n_card"}, false},
		"not container": {object{"type": "Card"}, Placement{ParentID: "n_card"}, false},
	} {
		if acceptsChild(app, c.parent, c.p) != c.want {
			t.Error("placement", name)
		}
	}
	r := store.CapabilityRequest{Kind: KindComponent, Key: "PromoBanner", ProposedContract: raw(object{"props": object{"title": object{"type": "string"}}, "container": true, "slots": object{"actions": object{}}})}
	for name, actual := range map[string]object{
		"missing":       nil,
		"required":      {"container": true, "props": object{"title": object{"type": "string"}, "size": object{"type": "number", "required": true}}, "slots": object{"actions": object{}}},
		"not container": {"props": object{"title": object{"type": "string"}}, "slots": object{"actions": object{}}},
		"no slot":       {"container": true, "props": object{"title": object{"type": "string"}}},
		"typed slot":    {"container": true, "props": object{"title": object{"type": "string"}}, "slots": object{"actions": object{"allowedTypes": []any{"Button"}}}},
	} {
		if componentCompatible(r, actual) {
			t.Error("incompatible", name)
		}
	}
	if !componentCompatible(r, object{"container": true, "props": object{"title": object{"type": "text"}, "size": object{"type": "number", "required": true, "default": json.Number("1")}}, "slots": object{"actions": object{}}}) {
		t.Error("compatible component")
	}
	if !nodeErrors(map[string]map[string][]ir.Diagnostic{"staging": {"doc": {{NodeID: "n_a", Severity: ir.SeverityError}}}}, "n_a") || nodeErrors(nil, "n_a") {
		t.Error("node errors")
	}
	r.Placement = raw(Placement{ParentID: "n_card", Slot: "free", Index: 1})
	r.NodeID = "n_promo"
	if payload := parse(marker(r).Payload); payload["slot"] != "free" {
		t.Error("marker slot", payload)
	}
	if a := anchorContext(r, snapshot{}, nil, nil, false); obj(a["placement"])["slot"] != "free" {
		t.Error("anchor slot", a)
	}
	if substitute(object{"nodes": object{}}, r, nil) {
		t.Error("no placeholder to substitute")
	}
}
