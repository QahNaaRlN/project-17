package capabilities

import (
	"context"
	"testing"

	"github.com/google/uuid"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

func recommendationsContract() object {
	return object{"params": object{"limit": object{"type": "number", "required": true}}, "result": object{"type": "list", "of": object{"type": "object", "fields": object{"title": object{"type": "string"}}}}}
}

func (f *fixture) dataSourceCreate() Create {
	return Create{Draft: Draft{Title: "Recommendations", Description: "Products recommended for the page", ProposedContract: raw(recommendationsContract()), Acceptance: []string{"Returns up to limit items"}}, Environment: "staging", ExpectedManifestHash: f.hash, ChangesetID: f.cs, ExpectedSeq: f.seq(), DocumentID: f.doc, Key: "store.recommendations", Kind: KindDataSource, Placement: &Placement{DataSourceKey: "recommended"}, InitialProps: raw(object{"limit": object{"lit": 4}})}
}

func (f *fixture) tokenCreate() Create {
	return Create{Draft: Draft{Title: "Huge spacing", Description: "A larger spacing step for hero blocks", ProposedContract: raw(object{"category": "spacing", "value": "48px"}), Acceptance: []string{"48px between hero blocks"}}, Environment: "staging", ExpectedManifestHash: f.hash, ChangesetID: f.cs, ExpectedSeq: f.seq(), DocumentID: f.doc, Key: "spacing.huge", Kind: KindToken, Placement: &Placement{NodeID: "n_box", Property: "padding"}}
}

// submitted drafts and submits a request on a new page with a Box root.
func (f *fixture) submitted(create func() Create) {
	f.e.T.Helper()
	f.doc = f.promoPage("/data")
	r := must(f.e, f.a, "create-capability-request", create())
	f.request, _ = f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: uuid.MustParse(r.Request["id"].(string))})
	must(f.e, f.a, "submit-capability-request", Action{ID: f.request.ID, ExpectedRevision: 1, ExpectedSeq: f.seq(), ExpectedManifestHash: f.hash})
}

func dataSourceSetup(t *testing.T) *fulfillmentFixture {
	return fulfillmentSetupWith(githubSetupWith(t, func(f *fixture) { f.submitted(f.dataSourceCreate) }))
}

func tokenSetup(t *testing.T) *fulfillmentFixture {
	return fulfillmentSetupWith(githubSetupWith(t, func(f *fixture) { f.submitted(f.tokenCreate) }))
}

// deployWith registers a staging manifest built from commit, changed by change.
func (f *fulfillmentFixture) deployWith(commit string, change func(object)) {
	f.e.T.Helper()
	index := object{"repo": object{"root": ".", "commit": commit}, "elements": object{}, "conventions": object{"registry": "src/registry.ts", "testCommand": "pnpm test", "manifestCommand": "pnpm cms build"}}
	indexHash, _ := manifest.Hash(index)
	m := parse([]byte(cmstest.DefaultManifest))
	m["codeIndex"] = object{"uploaded": false, "hash": indexHash}
	change(m)
	hash, _ := manifest.Hash(m)
	var upload manifestregistry.UploadedIndex
	f.e.Must(f.e.Admin, "upload-code-index", object{"manifestHash": hash, "indexHash": indexHash, "index": index}, &upload)
	id := upload.UploadID.String()
	f.e.Must(f.e.Admin, "register-manifest", manifestregistry.Payload{Manifest: raw(m), Environment: "staging", CodeIndexUploadID: &id}, nil)
}

func (f *fulfillmentFixture) body() object {
	f.e.T.Helper()
	rows, _ := f.e.Q.ChangesetWorkingVersions(context.Background(), f.cs)
	for _, v := range rows {
		if v.ObjectID == f.doc {
			return parse(v.Body)
		}
	}
	f.e.T.Fatal("working version missing")
	return nil
}

func (f *fulfillmentFixture) checksFail(code string) {
	f.e.T.Helper()
	ctx := context.Background()
	workflow.Register(f.e.Bus)
	f.e.Must(f.a, "submit-changeset", object{"changesetId": f.cs, "expectedSeq": f.seq(f.cs)}, nil)
	var n int
	f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM checks WHERE changeset_id=$1 AND details::text LIKE '%'||$2||'%'`, f.cs, code).Scan(&n)
	if n == 0 {
		f.e.T.Fatal("checks report", code)
	}
	f.e.Must(f.a, "reopen-changeset", object{"changesetId": f.cs, "expectedSeq": f.seq(f.cs)}, nil)
}

// STU-375…390: a data source placeholder blocks publication; the activated
// compatible source of the merged PR fulfils the request and cms-system
// drops the marker; undo and retry restore and replace it.
func TestSTU375_390DataSourceLifecycle(t *testing.T) {
	f := dataSourceSetup(t)
	ctx := context.Background()
	r := f.current()
	use := obj(obj(f.body()["dataSources"])["recommended"])
	if r.Kind != KindDataSource || use["pending"] != r.ID.String() || use["source"] != "store.recommendations" || obj(obj(use["params"])["limit"])["lit"] == nil {
		t.Fatal("STU-381 submit connects the placeholder", use)
	}
	if a := f.anchors(); len(a) != 1 || a[0].NodeID != "recommended" || a[0].AnchorKind != "source" {
		t.Fatal("STU-383 the anchor is the page key", a)
	}
	f.checksFail("DATA_SOURCE_PENDING_CAPABILITY")
	f.merged()
	f.deployWith(mergeSHA, func(m object) {
		m["dataSources"] = object{"store.recommendations": object{"params": object{"limit": object{"type": "number", "required": true}}, "result": object{"type": "object", "fields": object{}}}}
	})
	f.run()
	if got := f.current(); got.Status == "fulfilled" || got.FulfillmentReason != "contract_incompatible" {
		t.Fatal("STU-389 an incompatible result waits", got.FulfillmentReason)
	}
	f.deployWith(laterSHA, func(m object) {
		m["dataSources"] = object{"store.recommendations": object{"params": object{"limit": object{"type": "number", "required": true}, "locale": object{"type": "string"}}, "result": object{"type": "list", "of": object{"type": "object", "fields": object{"title": object{"type": "string"}, "price": object{"type": "number"}}}}}}
	})
	f.svc.Comparer = comparerFunc(func(context.Context, gh.Repository, string, string) (gh.Comparison, error) {
		return gh.Comparison{Status: "ahead", MergeBase: mergeSHA, Base: mergeSHA, Head: laterSHA, AheadBy: 1}, nil
	})
	f.run()
	if got := f.current(); got.Status != "fulfilled" || anchorFor(f.anchors(), f.cs).State != "replaced" {
		t.Fatal("STU-390 fulfilled and replaced", got.FulfillmentReason, f.anchors())
	}
	use = obj(obj(f.body()["dataSources"])["recommended"])
	if use["pending"] != nil || use["source"] != "store.recommendations" {
		t.Fatal("STU-390 the marker is dropped", use)
	}
	f.e.Must(f.a, "undo", object{"changesetId": f.cs, "expectedSeq": f.seq(f.cs)}, nil)
	if v, e := FulfillmentGet(ctx, f.e.Q, f.a, r.ID); e != nil || obj(arr(v["anchors"])[0])["state"] != "reverted" {
		t.Fatal("STU-346 reverted", v, e)
	}
	if v, e := Get(ctx, f.e.Q, f.a, r.ID); e != nil || v["fulfillmentReverted"] != true {
		t.Fatal("reverted request card", v, e)
	}
	a := anchorFor(f.anchors(), f.cs)
	f.command(f.a, "retry-capability-fulfillment", AnchorAction{RequestID: r.ID, AnchorID: a.ID, ExpectedAnchorRevision: a.Revision})
	f.run()
	if obj(obj(f.body()["dataSources"])["recommended"])["pending"] != nil {
		t.Fatal("STU-346 retry replaces again")
	}
}

// STU-378/387/390: a token placeholder shows its proposed value in a draft
// preview, blocks publication and becomes the token name when fulfilled.
func TestSTU378_390TokenLifecycle(t *testing.T) {
	f := tokenSetup(t)
	ctx := context.Background()
	r := f.current()
	marker := "pending:" + r.ID.String()
	if padding := obj(obj(obj(f.body()["nodes"])["n_box"])["design"])["padding"]; padding != marker || r.Event != "padding" {
		t.Fatal("STU-381 submit writes the token placeholder", padding, r.Event)
	}
	staging, _ := f.e.Q.GetEnvironmentByName(ctx, store.GetEnvironmentByNameParams{ProjectID: f.a.ProjectID, Name: "staging"})
	cs := f.cs
	body := raw(f.body())
	projected, e := ProjectPreview(ctx, f.e.Q, f.a.ProjectID, staging.ID, &cs, f.doc, body)
	if e != nil || obj(obj(obj(obj(parse(projected)["nodes"])["n_box"])["design"])["padding"])["raw"] != "48px" {
		t.Fatal("STU-387 the draft preview renders the proposed value", string(projected), e)
	}
	if published, _ := ProjectPreview(ctx, f.e.Q, f.a.ProjectID, staging.ID, nil, f.doc, body); string(published) != string(body) {
		t.Fatal("STU-387 published reads keep the stored value")
	}
	f.checksFail("TOKEN_PENDING_CAPABILITY")
	f.merged()
	f.deployWith(mergeSHA, func(m object) { m["tokens"] = object{"spacing": object{"huge": "56px"}} })
	f.run()
	if got := f.current(); got.Status != "fulfilled" {
		t.Fatal("STU-389 the token fulfils whatever its value", got.FulfillmentReason)
	}
	if padding := obj(obj(obj(f.body()["nodes"])["n_box"])["design"])["padding"]; padding != "huge" {
		t.Fatal("STU-390 the token name replaces the placeholder", padding)
	}
	var basis object
	view, _ := FulfillmentGet(ctx, f.e.Q, f.a, r.ID)
	basis = obj(view["basis"])
	if basis == nil {
		t.Fatal("basis recorded", view)
	}
}
