package capabilities

import (
	"context"
	"testing"

	"github.com/google/uuid"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// promoContract — the proposed PromoBanner (STU-356).
func promoContract() object {
	return object{"props": object{"title": object{"type": "string", "required": true}, "tone": object{"type": "enum", "values": []any{"plain", "accent"}}}, "container": true}
}

// promoPage adds a page with a Box root to the fixture Change Set.
func (f *fixture) promoPage(path string) uuid.UUID {
	f.e.T.Helper()
	var made changes.ApplyResult
	c, _ := f.e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
	f.e.Must(f.a, "apply-operations", object{"changesetId": f.cs, "expectedSeq": c.Seq, "operations": []any{object{"type": "document.create", "payload": object{"kind": "page", "path": path, "root": object{"id": "n_box", "type": "Box"}}}}}, &made)
	return made.Operations[0].Target
}

func (f *fixture) componentCreate() Create {
	c, _ := f.e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
	return Create{Draft: Draft{Title: "Promo banner", Description: "A promotional banner with a call to action", ProposedContract: raw(promoContract()), Acceptance: []string{"Shows the title on every width"}}, Environment: "staging", ExpectedManifestHash: f.hash, ChangesetID: f.cs, ExpectedSeq: c.Seq, DocumentID: f.doc, Key: "PromoBanner", Kind: KindComponent, Placement: &Placement{ParentID: "n_box"}, InitialProps: raw(object{"title": "Sale", "tone": "accent"})}
}

// createComponent drafts and submits a PromoBanner request on a new page.
func (f *fixture) createComponent() {
	f.e.T.Helper()
	f.doc = f.promoPage("/promo")
	r := must(f.e, f.a, "create-capability-request", f.componentCreate())
	f.request, _ = f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: uuid.MustParse(r.Request["id"].(string))})
	c, _ := f.e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
	must(f.e, f.a, "submit-capability-request", Action{ID: f.request.ID, ExpectedRevision: 1, ExpectedSeq: c.Seq, ExpectedManifestHash: f.hash})
}

func componentSetup(t *testing.T) *fulfillmentFixture {
	return fulfillmentSetupWith(githubSetupWith(t, (*fixture).createComponent))
}

// deployComponent registers a staging manifest built from commit with the component.
func (f *fulfillmentFixture) deployComponent(commit string, component object) {
	f.e.T.Helper()
	index := object{"repo": object{"root": ".", "commit": commit}, "elements": object{}, "conventions": object{"registry": "src/registry.ts", "testCommand": "pnpm test", "manifestCommand": "pnpm cms build"}}
	indexHash, _ := manifest.Hash(index)
	m := parse([]byte(cmstest.DefaultManifest))
	m["codeIndex"] = object{"uploaded": false, "hash": indexHash}
	if component != nil {
		m["components"] = object{"PromoBanner": component}
	}
	hash, _ := manifest.Hash(m)
	var upload manifestregistry.UploadedIndex
	f.e.Must(f.e.Admin, "upload-code-index", object{"manifestHash": hash, "indexHash": indexHash, "index": index}, &upload)
	id := upload.UploadID.String()
	f.e.Must(f.e.Admin, "register-manifest", manifestregistry.Payload{Manifest: raw(m), Environment: "staging", CodeIndexUploadID: &id}, nil)
}

func (f *fulfillmentFixture) node(cs, doc uuid.UUID, id string) object {
	f.e.T.Helper()
	d, e := changes.GetDocument(context.Background(), f.e.Q, f.a.ProjectID, doc, &cs)
	if e != nil {
		f.e.T.Fatal(e)
	}
	return obj(obj(parse(d.Body)["nodes"])[id])
}

func realPromo() object {
	return object{"origin": "native", "container": true, "props": object{"title": object{"type": "string", "required": true}, "tone": object{"type": "enum", "values": []any{"plain", "accent"}}}}
}

// STU-355…360, STU-366…370: a component request inserts a placeholder that
// blocks publication; an activated compatible component from the merged PR
// fulfils the request and cms-system clears the marker.
func TestSTU355_370ComponentRequestLifecycle(t *testing.T) {
	f := componentSetup(t)
	ctx := context.Background()
	r := f.current()
	if r.Kind != KindComponent || r.Status != "submitted" || r.Event != "" {
		t.Fatal("STU-355 a submitted component request", r.Kind, r.Status)
	}
	n := f.node(f.cs, f.doc, r.NodeID)
	if n["type"] != "PromoBanner" || n["pending"] != r.ID.String() || obj(n["props"])["title"] != "Sale" {
		t.Fatal("STU-360 submit inserts the placeholder with its initial props", n)
	}
	if a := f.anchors(); len(a) != 1 || a[0].NodeID != r.NodeID || a[0].Event != "" {
		t.Fatal("STU-362 the anchor is the node", a)
	}
	workflow.Register(f.e.Bus)
	var out object
	f.e.Must(f.a, "submit-changeset", object{"changesetId": f.cs, "expectedSeq": f.seq(f.cs)}, &out)
	if cs, _ := f.e.Q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs}); cs.State != "failed" {
		t.Fatal("STU-358 a placeholder fails the checks", cs.State)
	}
	var blocked int
	f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM checks WHERE changeset_id=$1 AND details::text LIKE '%COMPONENT_PENDING_CAPABILITY%'`, f.cs).Scan(&blocked)
	if blocked == 0 {
		t.Fatal("STU-358 COMPONENT_PENDING_CAPABILITY")
	}
	f.e.Must(f.a, "reopen-changeset", object{"changesetId": f.cs, "expectedSeq": f.seq(f.cs)}, nil)
	// An incompatible component waits; a compatible one from the merged PR fulfils.
	f.merged()
	f.deployComponent(mergeSHA, object{"props": object{"title": object{"type": "number", "required": true}}, "container": true})
	f.run()
	if got := f.current(); got.Status == "fulfilled" || got.FulfillmentReason != "contract_incompatible" {
		t.Fatal("STU-368 incompatible component", got.FulfillmentReason)
	}
	f.deployComponent(laterSHA, realPromo())
	f.svc.Comparer = comparerFunc(func(context.Context, gh.Repository, string, string) (gh.Comparison, error) {
		return gh.Comparison{Status: "ahead", MergeBase: mergeSHA, Base: mergeSHA, Head: laterSHA, AheadBy: 1}, nil
	})
	f.run()
	if got := f.current(); got.Status != "fulfilled" {
		t.Fatal("STU-369 automatic fulfilment", got.FulfillmentReason)
	}
	if a := anchorFor(f.anchors(), f.cs); a.State != "replaced" {
		t.Fatal("STU-370 replaced", a.State)
	}
	n = f.node(f.cs, f.doc, r.NodeID)
	if n["pending"] != nil || n["type"] != "PromoBanner" || obj(n["props"])["title"] != "Sale" {
		t.Fatal("STU-370 the marker is cleared and props stay", n)
	}
	// Undo restores the marker; the anchor reads as reverted; retry clears it again.
	f.e.Must(f.a, "undo", object{"changesetId": f.cs, "expectedSeq": f.seq(f.cs)}, nil)
	if f.node(f.cs, f.doc, r.NodeID)["pending"] != r.ID.String() {
		t.Fatal("STU-346 undo restores the placeholder")
	}
	view, e := FulfillmentGet(ctx, f.e.Q, f.a, r.ID)
	if e != nil || obj(arr(view["anchors"])[0])["state"] != "reverted" {
		t.Fatal("STU-346 reverted", view, e)
	}
	a := anchorFor(f.anchors(), f.cs)
	f.command(f.a, "retry-capability-fulfillment", AnchorAction{RequestID: r.ID, AnchorID: a.ID, ExpectedAnchorRevision: a.Revision})
	f.run()
	if f.node(f.cs, f.doc, r.NodeID)["pending"] != nil {
		t.Fatal("STU-346 retry clears the marker again")
	}
}
