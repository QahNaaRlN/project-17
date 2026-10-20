package capabilities

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	gh "github.com/qahnaarln/project-17/apps/server/internal/capabilitygithub"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

var (
	mergeSHA = strings.Repeat("e", 40)
	laterSHA = strings.Repeat("f", 40)
)

type comparerFunc func(context.Context, gh.Repository, string, string) (gh.Comparison, error)

func (f comparerFunc) Compare(c context.Context, r gh.Repository, base, head string) (gh.Comparison, error) {
	return f(c, r, base, head)
}

type fulfillmentFixture struct {
	githubFixture
	svc   *FulfillmentService
	facts gh.Observation
}

// fulfillmentSetup: a submitted request linked to PR #1 whose verified facts
// the test controls, and a fulfillment service without network.
func fulfillmentSetup(t *testing.T) *fulfillmentFixture {
	g := githubSetup(t)
	f := &fulfillmentFixture{githubFixture: g}
	f.facts = observed(gh.Repository{RepositoryID: "42"}, 1)
	f.s.Fetcher = githubFetcher(func(_ context.Context, r gh.Repository, n int64) (gh.Observation, error) { return f.facts, nil })
	f.svc = &FulfillmentService{Pool: g.e.Pool, Github: g.s, Comparer: comparerFunc(func(context.Context, gh.Repository, string, string) (gh.Comparison, error) {
		return gh.Comparison{}, errors.New("no network in tests")
	})}
	g.link()
	g.sync()
	return f
}

// merged makes the next observation report PR #1 merged into mergeSHA.
func (f *fulfillmentFixture) merged() {
	f.e.T.Helper()
	f.facts.State, f.facts.IntegrationType, f.facts.IntegrationSHA = "merged", "merged_commit", mergeSHA
	f.e.Exec(`UPDATE capability_github_links SET refresh_after=now()`)
	l := f.row()
	f.command(f.engineer, "refresh-capability-github-pr", GithubAction{RequestID: f.githubFixture.request.ID, ExpectedRequestRevision: f.revision(), ExpectedConfigRevision: 1, ExpectedLinkRevision: l.Revision})
	f.e.Exec(`UPDATE capability_github_links SET next_at=now()`)
	f.sync()
}

func (f *fulfillmentFixture) revision() int64 {
	r, _ := f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: f.githubFixture.request.ID})
	return r.Revision
}

// deploy registers a staging manifest built from commit with the action.
func (f *fulfillmentFixture) deploy(commit string, args object) {
	f.e.T.Helper()
	index := object{"repo": object{"root": ".", "commit": commit}, "elements": object{}, "conventions": object{"registry": "src/registry.ts", "testCommand": "pnpm test", "manifestCommand": "pnpm cms build"}}
	indexHash, _ := manifest.Hash(index)
	m := parse([]byte(cmstest.DefaultManifest))
	m["codeIndex"] = object{"uploaded": false, "hash": indexHash}
	if args != nil {
		m["actions"] = object{"store.favorite": object{"args": args}}
	}
	hash, _ := manifest.Hash(m)
	var upload manifestregistry.UploadedIndex
	f.e.Must(f.e.Admin, "upload-code-index", object{"manifestHash": hash, "indexHash": indexHash, "index": index}, &upload)
	id := upload.UploadID.String()
	f.e.Must(f.e.Admin, "register-manifest", manifestregistry.Payload{Manifest: raw(m), Environment: "staging", CodeIndexUploadID: &id}, nil)
}

func (f *fulfillmentFixture) run() {
	f.e.T.Helper()
	if e := f.svc.Run(context.Background()); e != nil {
		f.e.T.Fatal(e)
	}
}

func (f *fulfillmentFixture) current() store.CapabilityRequest {
	r, _ := f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: f.githubFixture.request.ID})
	return r
}

func (f *fulfillmentFixture) anchors() []store.CapabilityRequestAnchor {
	rows, _ := f.e.Q.ListCapabilityAnchors(context.Background(), store.ListCapabilityAnchorsParams{ProjectID: f.a.ProjectID, RequestID: f.githubFixture.request.ID})
	return rows
}

// page creates a second open Change Set with its own page for the group.
func (f *fulfillmentFixture) page(title string) (uuid.UUID, uuid.UUID) {
	f.e.T.Helper()
	var cs changes.Changeset
	f.e.Must(f.a, "create-changeset", object{"title": title, "targets": []string{"staging"}}, &cs)
	var made changes.ApplyResult
	f.e.Must(f.a, "apply-operations", object{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{object{"type": "document.create", "payload": object{"kind": "page", "path": "/" + title, "root": object{"id": "n_root", "type": "Button", "props": object{"label": title}}}}}}, &made)
	return cs.ID, made.Operations[0].Target
}

func (f *fulfillmentFixture) attach(cs, doc uuid.UUID) object {
	f.e.T.Helper()
	seq := f.seq(cs)
	active, _ := f.e.Q.GetEnvironmentByName(context.Background(), store.GetEnvironmentByNameParams{ProjectID: f.a.ProjectID, Name: "staging"})
	m, _ := f.e.Q.GetManifestByID(context.Background(), store.GetManifestByIDParams{ProjectID: f.a.ProjectID, ID: *active.ActiveManifestID})
	return f.command(f.a, "attach-capability-request", AttachRequest{RequestID: f.githubFixture.request.ID, ExpectedRequestRevision: f.revision(), ChangesetID: cs, ExpectedSeq: seq, DocumentID: doc, NodeID: "n_root", Event: "click", ExpectedManifestHash: m.Hash})
}

func (f *fulfillmentFixture) seq(cs uuid.UUID) int32 {
	c, _ := f.e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: cs})
	return c.Seq
}

func (f *fulfillmentFixture) clickOf(cs, doc uuid.UUID) object {
	f.e.T.Helper()
	d, e := changes.GetDocument(context.Background(), f.e.Q, f.a.ProjectID, doc, &cs)
	if e != nil {
		f.e.T.Fatal(e)
	}
	return obj(handlerChain(obj(obj(obj(parse(d.Body)["nodes"])["n_root"])["on"])["click"])[0])
}

func anchorFor(rows []store.CapabilityRequestAnchor, cs uuid.UUID) store.CapabilityRequestAnchor {
	for _, a := range rows {
		if a.ChangesetID == cs {
			return a
		}
	}
	return store.CapabilityRequestAnchor{}
}

func TestSTU335_344AutomaticFulfillmentReplacesTheGroup(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	if len(f.anchors()) != 1 || !f.anchors()[0].Original {
		t.Fatal("STU-335 original anchor migrates", f.anchors())
	}
	cs2, doc2 := f.page("second")
	attached := f.attach(cs2, doc2)
	again := f.attach(cs2, doc2)
	if obj(attached["anchor"])["id"] != obj(again["anchor"])["id"] {
		t.Fatal("STU-336 attach is idempotent")
	}
	if pending := f.clickOf(cs2, doc2)["action"]; pending != "pending:"+f.githubFixture.request.ID.String() {
		t.Fatal("STU-336 attach writes the marker", pending)
	}
	original := anchorFor(f.anchors(), f.cs)
	f.command(f.a, "set-capability-fulfillment-args", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: original.ID, ExpectedAnchorRevision: original.Revision, Handlers: raw([]any{object{"action": "store.favorite", "args": object{"value": object{"lit": "chosen"}}}})})

	// An open PR with a green CI and a deployed action does not fulfill.
	f.deploy(mergeSHA, object{"value": object{"type": "string", "required": true}})
	f.run()
	if r := f.current(); r.Status == "fulfilled" || r.FulfillmentReason != "pr_not_merged" {
		t.Fatal("STU-339 open PR never fulfills", r.Status, r.FulfillmentReason)
	}
	f.merged()
	f.run()
	r := f.current()
	if r.Status != "fulfilled" || r.FulfillmentMode == nil || *r.FulfillmentMode != "automatic" || *r.FulfillmentCommit != mergeSHA {
		t.Fatal("STU-341 automatic fulfillment", r.Status, r.FulfillmentReason)
	}
	if call := f.clickOf(f.cs, f.doc); call["action"] != "store.favorite" || obj(obj(call["args"])["value"])["lit"] != "chosen" {
		t.Fatal("STU-342 replacement with confirmed args", call)
	}
	second := anchorFor(f.anchors(), cs2)
	if second.State != "awaiting_args" || f.clickOf(cs2, doc2)["action"] == "store.favorite" {
		t.Fatal("STU-344 required args wait for the owner", second.State)
	}
	f.command(f.a, "set-capability-fulfillment-args", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: second.ID, ExpectedAnchorRevision: second.Revision, Handlers: raw([]any{object{"action": "store.favorite", "args": object{"value": object{"lit": "later"}}}})})
	f.run()
	if anchorFor(f.anchors(), cs2).State != "replaced" || f.clickOf(cs2, doc2)["action"] != "store.favorite" {
		t.Fatal("STU-344 args then replacement")
	}

	// STU-346: undo restores the marker; the anchor shows reverted; retry replaces again.
	f.e.Must(f.a, "undo", object{"changesetId": cs2, "expectedSeq": f.seq(cs2)}, nil)
	view, e := FulfillmentGet(ctx, f.e.Q, f.a, f.githubFixture.request.ID)
	states := map[string]bool{}
	for _, a := range arr(view["anchors"]) {
		states[obj(a)["state"].(string)] = true
	}
	if e != nil || !states["reverted"] || view["anchorCount"] != 2 {
		t.Fatal("STU-346 reverted anchor", view, e)
	}
	second = anchorFor(f.anchors(), cs2)
	f.command(f.a, "retry-capability-fulfillment", AnchorAction{RequestID: f.githubFixture.request.ID, AnchorID: second.ID, ExpectedAnchorRevision: second.Revision})
	f.run()
	if f.clickOf(cs2, doc2)["action"] != "store.favorite" {
		t.Fatal("STU-346 retry replaces again")
	}
	dev, e := FulfillmentGet(ctx, f.e.Q, f.engineer, f.githubFixture.request.ID)
	for _, a := range arr(dev["anchors"]) {
		if obj(a)["nodeId"] != nil {
			t.Fatal("STU-337 engineer sees no IR anchors", a)
		}
	}
	if e != nil || len(arr(dev["anchors"])) != 2 {
		t.Fatal(dev, e)
	}
	// STU-349: the same manifest again and a manifest without the key change nothing.
	f.deploy(mergeSHA, object{"value": object{"type": "string", "required": true}})
	f.deploy(laterSHA, nil)
	f.run()
	if r := f.current(); r.Status != "fulfilled" || f.clickOf(f.cs, f.doc)["action"] != "store.favorite" {
		t.Fatal("STU-349 later registrations do not undo fulfillment")
	}
}

func TestSTU339ProvenanceConditions(t *testing.T) {
	ctx := context.Background()
	t.Run("descendant", func(t *testing.T) {
		f := fulfillmentSetup(t)
		calls := 0
		f.svc.Comparer = comparerFunc(func(_ context.Context, _ gh.Repository, base, head string) (gh.Comparison, error) {
			calls++
			return gh.Comparison{Base: base, Head: head, Status: "ahead", MergeBase: base, AheadBy: 3}, nil
		})
		f.merged()
		f.deploy(laterSHA, object{"value": object{"type": "string"}})
		f.run()
		r := f.current()
		if r.Status != "fulfilled" || calls != 1 || !strings.Contains(string(r.FulfillmentBasis), `"ahead"`) {
			t.Fatal("decision §11.1 descendant via compare", r.Status, r.FulfillmentReason, calls)
		}
		if call := f.clickOf(f.cs, f.doc); call["action"] != "store.favorite" || call["args"] != nil {
			t.Fatal("STU-344 action without required args", call)
		}
	})
	for _, c := range []struct {
		name, want string
		prepare    func(*fulfillmentFixture)
	}{
		{"diverged", "commit_unrelated", func(f *fulfillmentFixture) {
			f.svc.Comparer = comparerFunc(func(_ context.Context, _ gh.Repository, base, head string) (gh.Comparison, error) {
				return gh.Comparison{Base: base, Head: head, Status: "diverged", MergeBase: strings.Repeat("1", 40)}, nil
			})
			f.merged()
			f.deploy(laterSHA, object{"value": object{"type": "string"}})
		}},
		{"comparison unavailable", "comparison_unavailable", func(f *fulfillmentFixture) {
			f.merged()
			f.deploy(laterSHA, object{"value": object{"type": "string"}})
		}},
		{"no index", "no_code_index", func(f *fulfillmentFixture) {
			f.merged()
			m := parse([]byte(cmstest.DefaultManifest))
			m["actions"] = object{"store.favorite": object{"args": object{"value": object{"type": "string"}}}}
			f.e.Must(f.e.Admin, "register-manifest", manifestregistry.Payload{Manifest: raw(m), Environment: "staging"}, nil)
		}},
		{"incompatible", "contract_incompatible", func(f *fulfillmentFixture) {
			f.merged()
			f.deploy(mergeSHA, object{"value": object{"type": "number", "required": true}})
		}},
		{"integration CI", "ci_not_successful", func(f *fulfillmentFixture) {
			f.facts.Results = append(f.facts.Results, gh.Result{Source: "check_run", Name: "Merge", Target: "integration", Outcome: "failure"})
			f.merged()
			f.deploy(mergeSHA, object{"value": object{"type": "string"}})
		}},
		{"stale observation", "observation_stale", func(f *fulfillmentFixture) {
			f.merged()
			f.e.Exec(`UPDATE capability_github_observations SET fresh_until=now()-interval '1 minute'`)
			f.deploy(mergeSHA, object{"value": object{"type": "string"}})
		}},
		{"no link", "no_github_link", func(f *fulfillmentFixture) {
			l := f.row()
			f.command(f.engineer, "unlink-capability-github-pr", GithubAction{RequestID: f.githubFixture.request.ID, ExpectedRequestRevision: f.revision(), ExpectedLinkRevision: l.Revision})
			f.deploy(mergeSHA, object{"value": object{"type": "string"}})
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := fulfillmentSetup(t)
			c.prepare(f)
			f.run()
			if r := f.current(); r.Status == "fulfilled" || r.FulfillmentReason != c.want {
				t.Fatal("STU-339", c.want, r.Status, r.FulfillmentReason)
			}
			if f.clickOf(f.cs, f.doc)["action"] == "store.favorite" {
				t.Fatal("waiting request changed a page")
			}
		})
	}
	t.Run("explicit check and key missing", func(t *testing.T) {
		f := fulfillmentSetup(t)
		f.command(f.engineer, "check-capability-fulfillment", CheckFulfillment{RequestID: f.githubFixture.request.ID})
		f.run()
		if r := f.current(); r.FulfillmentReason != "key_missing" {
			t.Fatal("decision §11.8 explicit check", r.FulfillmentReason)
		}
		if _, e := f.call(f.engineer, "check-capability-fulfillment", CheckFulfillment{RequestID: uuid.New()}, ""); e == nil {
			t.Fatal("unknown request")
		}
		_ = ctx
	})
}

func TestSTU343_347ReviewClosedAndChangedAnchors(t *testing.T) {
	f := fulfillmentSetup(t)
	workflow.Register(f.e.Bus)
	cs2, doc2 := f.page("review")
	f.attach(cs2, doc2)
	cs3, doc3 := f.page("changed")
	f.attach(cs3, doc3)
	// The owner replaces the marker of CS-3 by hand before fulfillment.
	f.e.Must(f.a, "apply-operations", object{"changesetId": cs3, "expectedSeq": f.seq(cs3), "operations": []any{object{"type": "node.setBehavior", "target": doc3, "payload": object{"nodeId": "n_root", "event": "click", "handlers": nil}}}}, nil)
	f.e.Exec(`UPDATE changesets SET state='in_review' WHERE id=$1`, cs2)
	f.merged()
	f.deploy(mergeSHA, object{"value": object{"type": "string"}})
	f.run()
	rows := f.anchors()
	if anchorFor(rows, cs2).State != "changeset_in_review" || anchorFor(rows, cs3).State != "anchor_changed" || anchorFor(rows, f.cs).State != "replaced" {
		t.Fatal("STU-343/347 per Change Set outcome", rows)
	}
	f.e.Exec(`UPDATE changesets SET state='open' WHERE id=$1`, cs2)
	f.run()
	if anchorFor(f.anchors(), cs2).State != "replaced" || f.clickOf(cs2, doc2)["action"] != "store.favorite" {
		t.Fatal("STU-347 reopened Change Set is replaced")
	}
	cs4, doc4 := f.page("closed")
	_ = doc4
	if _, e := f.call(f.a, "attach-capability-request", AttachRequest{RequestID: f.githubFixture.request.ID, ExpectedRequestRevision: f.revision(), ChangesetID: cs4, ExpectedSeq: f.seq(cs4), DocumentID: doc4, NodeID: "n_root", Event: "click", ExpectedManifestHash: "sha256:" + strings.Repeat("0", 64)}, ""); e == nil {
		t.Fatal("STU-336 a fulfilled request accepts no new anchors")
	}
	f.e.Exec(`UPDATE capability_request_anchors SET state='queued' WHERE changeset_id=$1`, cs3)
	f.e.Exec(`UPDATE changesets SET state='merged' WHERE id=$1`, cs3)
	f.run()
	if anchorFor(f.anchors(), cs3).State != "changeset_closed" {
		t.Fatal("STU-347 closed Change Set")
	}
}

func TestSTU345SystemActorIsNarrow(t *testing.T) {
	f := fulfillmentSetup(t)
	ctx := context.Background()
	tx, e := f.e.Pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	q := store.New(tx)
	cs, _ := f.e.Q.GetChangeset(ctx, store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
	system, e := systemActor(ctx, q, f.a.ProjectID, cs.OwnerID)
	if e != nil || !system.System {
		t.Fatal(system, e)
	}
	for _, op := range []changes.OperationInput{
		{Type: "node.setProps", Target: &f.doc, Payload: raw(object{"nodeId": "n_root", "set": object{"label": "x"}})},
		{Type: "node.setBehavior", Target: &f.doc, Payload: raw(object{"nodeId": "n_root", "event": "click", "handlers": []any{object{"action": "store.favorite"}}})},
	} {
		if _, e := changes.ApplyInTx(ctx, tx, system, f.cs, cs.Seq, []changes.OperationInput{op}); e == nil || !strings.Contains(e.Error(), "FORBIDDEN") && !strings.Contains(e.Error(), "SYSTEM") {
			t.Fatal("STU-345 system actor refused", op.Type, e)
		}
	}
	other := system
	owner := uuid.New()
	other.OnBehalfOf = &owner
	if e := changes.RequireOwner(cs, other); e == nil {
		t.Fatal("system acts only for the Change Set owner")
	}
	var token int
	f.e.Pool.QueryRow(ctx, `SELECT count(*) FROM api_tokens WHERE actor_id=$1`, system.ID).Scan(&token)
	if token != 0 {
		t.Fatal("system actor has no tokens")
	}
	_ = auth.ActorService
	_ = time.Now
}
