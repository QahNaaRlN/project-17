package capabilities

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ir"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/ops"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/validation"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

// STU-376: contracts of both kinds.
func TestSTU376DataTokenContracts(t *testing.T) {
	for name, c := range map[string]object{
		"paginated not bool": {"result": object{"type": "string"}, "paginated": "yes"},
		"extra field":        {"result": object{"type": "string"}, "cache": object{}},
		"bad result":         {"result": object{"type": "unknown"}},
		"params not object":  {"result": object{"type": "string"}, "params": "x"},
	} {
		if validDataSourceContract(c) == nil {
			t.Error("data source contract", name)
		}
	}
	ref := object{"params": object{"item": object{"type": "reference", "schema": "Product"}}, "result": object{"type": "list", "of": object{"type": "reference", "schema": "Product"}}, "paginated": true}
	if e := validDataSourceContract(ref); e != nil {
		t.Fatal("references and pagination", e)
	}
	for name, c := range map[string]object{
		"missing value": {"category": "spacing"},
		"typography":    {"category": "typography", "value": "x"},
		"extra":         {"category": "spacing", "value": "8px", "x": 1},
	} {
		if validTokenContract(c) == nil {
			t.Error("token contract", name)
		}
	}
	if validTokenContract(object{"category": "colors", "value": "#ffffff"}) != nil {
		t.Error("colour token")
	}
}

// STU-378/383/384/390: design paths, values, leaves and the token operations.
func TestSTU378_390DesignPaths(t *testing.T) {
	plain := object{"design": object{"padding": "md"}}
	responsive := object{"design": object{"padding": object{"base": "md", "lg": "xl"}}}
	empty := object{}
	for _, c := range []struct {
		node object
		p    Placement
		path string
		ok   bool
	}{
		{plain, Placement{Property: "padding"}, "padding", true},
		{plain, Placement{Property: "padding", Breakpoint: "md"}, "padding/md", true},
		{empty, Placement{Property: "padding", Breakpoint: "md"}, "padding/md", false},
		{responsive, Placement{Property: "padding"}, "padding/base", true},
		{responsive, Placement{Property: "padding", Breakpoint: "lg"}, "padding/lg", true},
	} {
		if path, ok := designPath(c.node, c.p); path != c.path || ok != c.ok {
			t.Error("design path", c.p, path, ok)
		}
	}
	if _, v := designValue(plain, "padding/md", "x"); obj(v)["base"] != "md" || obj(v)["md"] != "x" {
		t.Error("a plain value becomes the base", v)
	}
	if designLeaf(responsive, "padding/lg") != "xl" || designLeaf(plain, "padding") != "md" {
		t.Error("design leaves")
	}
	id := uuid.New()
	doc := object{"nodes": object{"n_box": object{"id": "n_box", "type": "Box", "design": object{"padding": object{"base": "md", "lg": "pending:" + id.String()}}}}}
	r := store.CapabilityRequest{ID: id, Kind: KindToken, Key: "spacing.huge", NodeID: "n_box", Event: "padding/lg"}
	if !dataTokenPending(r, doc) {
		t.Fatal("the placeholder is found")
	}
	clear := parse(dataTokenClear(r, doc).Payload)
	if obj(obj(clear["set"])["padding"])["lg"] != "huge" || obj(obj(clear["set"])["padding"])["base"] != "md" {
		t.Error("the token name replaces the leaf", clear)
	}
	detach := parse(dataTokenDetach(r, doc).Payload)
	if p := obj(obj(detach["set"])["padding"]); p["lg"] != nil || p["base"] != "md" {
		t.Error("detach drops the breakpoint only", detach)
	}
	r.Event = "padding"
	if detach := parse(dataTokenDetach(r, doc).Payload); arr(detach["unset"])[0] != "padding" {
		t.Error("detach of a base value unsets the property", detach)
	}
	if got := sortedLeaves(object{"padding": object{"base": "md", "lg": "pending:x"}, "gap": "sm", "states": object{"hover": object{"padding": "lg"}}, "shadow": object{"raw": "1px"}}); len(got) != 4 {
		t.Error("leaves", got)
	}
}

// STU-381/386: initial params and the data paths of the pack.
func TestSTU381_386DataSourceParams(t *testing.T) {
	r := store.CapabilityRequest{Kind: KindDataSource, ProposedContract: raw(recommendationsContract())}
	for name, params := range map[string]object{
		"missing required": {},
		"unknown":          {"limit": object{"lit": 1}, "x": object{"lit": 1}},
		"wrong literal":    {"limit": object{"lit": "x"}},
		"two keys":         {"limit": object{"lit": 1, "x": 1}},
		"plain string":     {"limit": "4"},
	} {
		if validInitialParams(r, params, nil) {
			t.Error("params refused", name)
		}
	}
	if !validInitialParams(r, object{"limit": "$content.count"}, nil) || !validInitial(store.CapabilityRequest{Kind: KindToken}, nil) {
		t.Error("expressions and tokens")
	}
	doc := object{"nodes": object{"n": object{"bindings": object{"items": "$data.rec", "title": "$data.rec.0.title", "other": "$data.records"}, "list": []any{"$data.rec.1"}}}}
	if got := dataPaths(doc, "rec"); len(got) != 3 {
		t.Error("data paths", got)
	}
}

// STU-379/390: errors at a placeholder's place after replacement.
func TestSTU379PlaceErrors(t *testing.T) {
	warn := func(d ir.Diagnostic) map[string]map[string][]ir.Diagnostic {
		return map[string]map[string][]ir.Diagnostic{"staging": {"doc": {d}}}
	}
	source := store.CapabilityRequest{Kind: KindDataSource}
	token := store.CapabilityRequest{Kind: KindToken}
	component := store.CapabilityRequest{Kind: KindComponent}
	if !placeErrors(source, "rec", "", warn(ir.Diagnostic{Pointer: "/dataSources/rec/params/limit", Severity: ir.SeverityError})) {
		t.Error("source error")
	}
	if placeErrors(source, "rec", "", warn(ir.Diagnostic{Pointer: "/dataSources/rec", Severity: ir.SeverityWarning})) {
		t.Error("warnings do not refuse")
	}
	if !placeErrors(token, "n_box", "padding/lg", warn(ir.Diagnostic{NodeID: "n_box", Pointer: "/nodes/n_box/design/padding/lg", Severity: ir.SeverityError})) {
		t.Error("token error")
	}
	if placeErrors(token, "n_box", "padding", warn(ir.Diagnostic{NodeID: "n_box", Pointer: "/nodes/n_box/design/gap", Severity: ir.SeverityError})) {
		t.Error("another property")
	}
	if !placeErrors(component, "n_box", "", warn(ir.Diagnostic{NodeID: "n_box", Severity: ir.SeverityError})) {
		t.Error("component error")
	}
	if anchorKind(KindAction) != "event" || anchorKind(KindToken) != "design" {
		t.Error("anchor kinds")
	}
	if !validPlacementFor(KindToken, &Placement{NodeID: "n_box", Property: "padding", Breakpoint: "md"}) || validPlacementFor(KindToken, &Placement{NodeID: "n_box", Property: "padding", Index: 1}) || validPlacementFor("other", &Placement{}) || validPlacementFor(KindDataSource, nil) {
		t.Error("placements")
	}
}

// STU-387: the proposed value of a token placeholder in a draft preview.
func TestSTU387ProposedTokens(t *testing.T) {
	f := tokenSetup(t)
	ctx := context.Background()
	r := f.current()
	doc := object{"nodes": object{"n_box": object{"design": object{"padding": object{"base": "md", "lg": "pending:" + r.ID.String()}, "gap": "pending:" + uuid.NewString(), "margin": "pending:not-a-uuid", "states": object{"hover": object{"padding": "pending:" + r.ID.String()}}}}}}
	changed, e := proposedTokens(ctx, f.e.Q, f.a.ProjectID, doc)
	design := obj(obj(obj(doc["nodes"])["n_box"])["design"])
	if e != nil || !changed || obj(obj(design["padding"])["lg"])["raw"] != "48px" || obj(design["padding"])["base"] != "md" {
		t.Fatal("STU-387 the breakpoint value is proposed", design, e)
	}
	if _, raw := design["gap"].(string); !raw {
		t.Error("an unknown request stays")
	}
	f.e.Break("capability_requests", "")
	if _, e := proposedTokens(ctx, f.e.Q, f.a.ProjectID, object{"nodes": object{"n": object{"design": object{"padding": "pending:" + r.ID.String()}}}}); e == nil {
		t.Error("storage failure")
	}
	if _, e := ops.Apply(object{}, ops.Op{Type: "x"}, nil); e == nil {
		t.Error("sanity")
	}
}

// STU-377…379: validation of data source and token placeholders.
func TestSTU377_379PlaceholderValidation(t *testing.T) {
	f := tokenSetup(t)
	ctx := context.Background()
	r := f.current()
	doc := f.body()
	nodes := obj(doc["nodes"])
	obj(nodes["n_box"])["design"] = object{"padding": "pending:" + r.ID.String(), "background": "pending:" + r.ID.String(), "gap": "pending:" + uuid.NewString()}
	doc["dataSources"] = object{"ghost": object{"source": "store.ghost", "pending": uuid.NewString()}}
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
	got := map[string]bool{}
	for _, d := range res.Diagnostics {
		got[d.Pointer+" "+string(d.Code)] = true
	}
	for _, want := range []string{
		"/nodes/n_box/design/padding TOKEN_PENDING_CAPABILITY",
		"/nodes/n_box/design/background CAPABILITY_PENDING_INVALID",
		"/nodes/n_box/design/gap CAPABILITY_PENDING_INVALID",
		"/dataSources/ghost/pending CAPABILITY_PENDING_INVALID",
	} {
		if !got[want] {
			t.Error("missing", want, res.Diagnostics)
		}
	}
	if got["/nodes/n_box/design/padding DESIGN_VALUE_INVALID"] {
		t.Error("a live token placeholder is a valid design value")
	}
}

// STU-388: preview/<ref> runs the real source in that preview only.
func TestSTU388DataSourcePreview(t *testing.T) {
	f := previewSetupWith(t, githubSetupWith(t, func(f *fixture) { f.submitted(f.dataSourceCreate) }))
	ctx := context.Background()
	f.createEnv()
	f.register(f.buildWith(shaA, func(m object) {
		m["dataSources"] = object{"store.recommendations": object{"params": object{"limit": object{"type": "number", "required": true}}, "result": object{"type": "list", "of": object{"type": "object", "fields": object{"title": object{"type": "string"}}}}}}
	}))
	if f.state() != PreviewReady {
		t.Fatal("STU-388 ready", f.state())
	}
	use := func(doc object) object { return obj(obj(doc["dataSources"])["recommended"]) }
	if u := use(f.project(f.environment().ID)); u["pending"] != nil || u["source"] != "store.recommendations" {
		t.Fatal("STU-388 the real source in the preview", u)
	}
	if use(parse(f.body()))["pending"] == nil {
		t.Fatal("the stored draft keeps the placeholder")
	}
	cs, e := ChangesetPreview(ctx, f.e.Q, f.a, f.cs, f.env)
	if e != nil || obj(arr(cs["substitutions"])[0])["substituted"] != true {
		t.Fatal("Change Set preview", cs, e)
	}
}

// STU-384/390: a token joins from another node; manual resolve of both kinds.
func TestSTU384_390TokenJoinAndManualResolve(t *testing.T) {
	f := tokenSetup(t)
	r := f.current()
	f.e.Must(f.a, "apply-operations", object{"changesetId": f.cs, "expectedSeq": f.seq(f.cs), "operations": []any{object{"type": "node.insert", "target": f.doc, "payload": object{"parentId": "n_box", "index": 0, "subtree": object{"id": "n_inner", "type": "Stack"}}}}}, nil)
	p := AttachRequest{RequestID: r.ID, ExpectedRequestRevision: f.revision(), ChangesetID: f.cs, ExpectedSeq: f.seq(f.cs), DocumentID: f.doc, ExpectedManifestHash: f.hash, Placement: &Placement{NodeID: "n_inner", Property: "gap"}}
	f.command(f.a, "attach-capability-request", p)
	var joined store.CapabilityRequestAnchor
	for _, a := range f.anchors() {
		if a.NodeID == "n_inner" {
			joined = a
		}
	}
	if joined.Event != "gap" || obj(obj(obj(f.body()["nodes"])["n_inner"])["design"])["gap"] != "pending:"+r.ID.String() {
		t.Fatal("STU-384 the joined breakpoint", joined, f.body())
	}
	r = f.engineering(f.current())
	m := parse([]byte(cmstest.DefaultManifest))
	m["tokens"] = object{"spacing": object{"huge": "48px"}}
	f.e.ActivateManifest("staging", raw(m))
	hash, _ := manifest.Hash(m)
	if _, e := f.call(f.a, "resolve-capability-request", Action{ID: r.ID, ExpectedRevision: r.Revision, ExpectedSeq: f.seq(f.cs), ExpectedManifestHash: hash}, ""); e != nil {
		t.Fatal("STU-390 manual resolve of a token", e)
	}
	if obj(obj(obj(f.body()["nodes"])["n_box"])["design"])["padding"] != "huge" || anchorFor(f.anchors(), f.cs).State == "" {
		t.Fatal("the original placeholder is replaced", f.body())
	}
	d := dataSourceSetup(t)
	q := d.engineering(d.current())
	dm := parse([]byte(cmstest.DefaultManifest))
	dm["dataSources"] = object{"store.recommendations": object{"params": object{"limit": object{"type": "number", "required": true}}, "result": object{"type": "list", "of": object{"type": "object", "fields": object{"title": object{"type": "string"}}}}}}
	d.e.ActivateManifest("staging", raw(dm))
	dh, _ := manifest.Hash(dm)
	if _, e := d.call(d.a, "resolve-capability-request", Action{ID: q.ID, ExpectedRevision: q.Revision, ExpectedSeq: d.seq(d.cs), ExpectedManifestHash: dh}, ""); e != nil {
		t.Fatal("STU-390 manual resolve of a data source", e)
	}
	if obj(obj(d.body()["dataSources"])["recommended"])["pending"] != nil {
		t.Fatal("the source marker is dropped")
	}
}
