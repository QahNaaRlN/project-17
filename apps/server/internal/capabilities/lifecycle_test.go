package capabilities

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

func (f *fixture) submit() store.CapabilityRequest {
	f.e.T.Helper()
	r := f.create()
	must(f.e, f.a, "submit-capability-request", Action{ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash})
	r.Revision = 2
	r.Status = "submitted"
	return r
}
func (f *fixture) engineering(r store.CapabilityRequest) store.CapabilityRequest {
	f.e.T.Helper()
	for _, status := range []string{"accepted", "in_progress", "pr_open"} {
		view, err := Get(context.Background(), f.e.Q, f.engineer, r.ID)
		if err != nil || view["status"] != r.Status || view["seq"] != nil {
			f.e.T.Fatal("engineering projection", view, err)
		}
		p := Update{ID: r.ID, ExpectedRevision: r.Revision, Transition: status}
		if status == "pr_open" {
			p.PRURL = "https://example.test/repo/pull/1"
		}
		must(f.e, f.engineer, "update-capability-request", p)
		r.Revision++
		r.Status = status
	}
	return r
}
func (f *fixture) activate(args object) string {
	m := parse([]byte(cmstest.DefaultManifest))
	m["actions"] = object{"store.favorite": object{"args": args}}
	f.e.ActivateManifest("staging", raw(m))
	h, _ := manifest.Hash(m)
	return h
}
func expectCode(t *testing.T, err error, want string) {
	t.Helper()
	if cmstest.Code(err) != want {
		t.Fatalf("want %s, got %v", want, err)
	}
}

func TestExplicitResolveCompatibilityAndUndo(t *testing.T) {
	f := setup(t)
	r := f.submit()
	ctx := context.Background()
	request := Action{ID: r.ID, ExpectedRevision: r.Revision, ExpectedSeq: 2, ExpectedManifestHash: f.hash, Handlers: raw([]any{object{"action": r.Key, "args": object{"value": object{"lit": "chosen"}}}})}
	_, err := call(f.e, f.a, "resolve-capability-request", request, "")
	expectCode(t, err, "CAPABILITY_STATE_INVALID")
	r = f.engineering(r)
	request.ExpectedRevision = r.Revision
	_, err = call(f.e, f.a, "resolve-capability-request", request, "")
	expectCode(t, err, "CAPABILITY_NOT_AVAILABLE")
	hash := f.activate(object{"value": object{"type": "number", "required": true}})
	request.ExpectedManifestHash = hash
	_, err = call(f.e, f.a, "resolve-capability-request", request, "")
	expectCode(t, err, "CAPABILITY_CONTRACT_MISMATCH")
	hash = f.activate(object{"value": object{"type": "string", "required": true}, "extra": object{"type": "boolean", "required": true}})
	request.ExpectedManifestHash = hash
	_, err = call(f.e, f.a, "resolve-capability-request", request, "")
	expectCode(t, err, "CAPABILITY_CONTRACT_MISMATCH")
	hash = f.activate(object{"value": object{"type": "string", "required": true}, "extra": object{"type": "boolean"}})
	request.ExpectedManifestHash = hash
	wrong := request
	wrong.Handlers = raw([]any{object{"action": "other.action"}})
	_, err = call(f.e, f.a, "resolve-capability-request", wrong, "")
	expectCode(t, err, "CAPABILITY_HANDLERS_INVALID")
	wrong.Handlers = raw([]any{object{"action": r.Key, "args": object{"value": object{"lit": 42}}}})
	_, err = call(f.e, f.a, "resolve-capability-request", wrong, "")
	expectCode(t, err, "CAPABILITY_HANDLERS_INVALID")
	wrong = request
	wrong.ExpectedSeq = 1
	_, err = call(f.e, f.a, "resolve-capability-request", wrong, "")
	expectCode(t, err, "CAPABILITY_CONTEXT_STALE")
	wrong = request
	wrong.ExpectedRevision = 1
	_, err = call(f.e, f.a, "resolve-capability-request", wrong, "")
	expectCode(t, err, "CAPABILITY_REVISION_CONFLICT")
	view, err := Get(ctx, f.e.Q, f.a, r.ID)
	if err != nil || view["stale"] != true {
		t.Fatal(view, err)
	}
	before, _ := Download(ctx, f.e.Q, f.engineer, r.ID, 2)
	result, err := call(f.e, f.a, "resolve-capability-request", request, "resolve-exact")
	if err != nil {
		t.Fatal(err)
	}
	if result.Request["status"] != "fulfilled" || result.Change.Changeset.Seq != 3 {
		t.Fatal(result)
	}
	_, err = call(f.e, f.a, "resolve-capability-request", request, "resolve-exact")
	if err != nil {
		t.Fatal(err)
	}
	after, err := Download(ctx, f.e.Q, f.engineer, r.ID, 2)
	if err != nil || before.Hash != after.Hash || string(before.Body) != string(after.Body) {
		t.Fatal("pack mutated", err)
	}
	f.e.Must(f.a, "undo", object{"changesetId": f.cs, "expectedSeq": 3}, nil)
	view, err = Get(ctx, f.e.Q, f.a, r.ID)
	if err != nil || view["status"] != "fulfilled" || view["anchorChanged"] != false {
		t.Fatal(view, err)
	}
	_, err = call(f.e, f.a, "refresh-capability-context", Action{ID: r.ID, ExpectedRevision: 6, ExpectedSeq: 4, ExpectedManifestHash: hash}, "")
	expectCode(t, err, "CAPABILITY_STATE_INVALID")
	_, err = call(f.e, f.engineer, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 6, Transition: "accepted"}, "")
	expectCode(t, err, "CAPABILITY_STATE_INVALID")
}

// STU-268: an exact pending call is eligible in either valid IR representation.
func TestResolveSinglePendingHandler(t *testing.T) {
	f := setup(t)
	r := f.engineering(f.submit())
	f.hash = f.activate(object{"value": object{"type": "string", "required": true}})
	f.e.Must(f.a, "apply-operations", object{"changesetId": f.cs, "expectedSeq": 2, "operations": []any{object{"type": "node.setBehavior", "target": f.doc, "payload": object{"nodeId": r.NodeID, "event": r.Event, "handlers": object{"action": "pending:" + r.ID.String()}}}}}, nil)
	view, err := Get(context.Background(), f.e.Q, f.a, r.ID)
	if err != nil || view["anchorChanged"] != false {
		t.Fatal("equivalent single pending considered changed", view, err)
	}
	result := must(f.e, f.a, "resolve-capability-request", Action{ID: r.ID, ExpectedRevision: r.Revision, ExpectedSeq: 3, ExpectedManifestHash: f.hash, Handlers: raw([]any{object{"action": r.Key, "args": object{"value": object{"lit": "chosen"}}}})})
	if result.Request["status"] != "fulfilled" || result.Change.Changeset.Seq != 4 {
		t.Fatal(result)
	}
}

func TestSubmitRollbackAndNoIndex(t *testing.T) {
	for _, table := range []string{"capability_context_packs", "capability_request_audit", "operations"} {
		t.Run(table, func(t *testing.T) {
			f := setup(t)
			r := f.create()
			f.e.Break(table, "INSERT")
			_, err := call(f.e, f.a, "submit-capability-request", Action{ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash}, "")
			if err == nil {
				t.Fatal("failed write committed")
			}
			saved, err := f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: r.ID})
			if err != nil || saved.Status != "draft" || saved.Revision != 1 {
				t.Fatal(saved, err)
			}
			cs, _ := f.e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
			if cs.Seq != 1 {
				t.Fatal("partial IR committed")
			}
		})
	}
	t.Run("missing index", func(t *testing.T) {
		f := setup(t)
		r := f.create()
		f.e.ActivateManifest("staging", []byte(cmstest.DefaultManifest))
		f.hash, _ = manifest.Hash(parse([]byte(cmstest.DefaultManifest)))
		_, err := Preview(context.Background(), f.e.Q, f.a, r.ID)
		expectCode(t, err, "CAPABILITY_INDEX_UNAVAILABLE")
		_, err = call(f.e, f.a, "submit-capability-request", Action{ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash}, "")
		expectCode(t, err, "CAPABILITY_INDEX_UNAVAILABLE")
	})
}

func TestVisibilityPaginationRevisionsAndRefresh(t *testing.T) {
	f := setup(t)
	r := f.create()
	ctx := context.Background()
	outsider := f.e.Human("outsider", auth.DesignRead, auth.CapabilityRequest)
	_, err := Get(ctx, f.e.Q, outsider, r.ID)
	expectCode(t, err, "NOT_FOUND")
	_, err = call(f.e, outsider, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 99, Transition: "cancelled"}, "")
	expectCode(t, err, "NOT_FOUND")
	_, err = List(ctx, f.e.Q, f.engineer, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := List(ctx, f.e.Q, f.engineer, Filter{})
	if len(arr(list["items"])) != 0 {
		t.Fatal("draft leaked")
	}
	for _, filter := range []Filter{{Limit: 101}, {Kind: "component"}, {Status: "unknown"}, {Cursor: "not-cursor"}} {
		if _, err := List(ctx, f.e.Q, f.a, filter); err == nil {
			t.Fatal(filter)
		}
	}
	d := Draft{Title: "Updated", Description: "Updated task", ProposedContract: r.ProposedContract, Acceptance: []string{"Updated criterion"}}
	must(f.e, f.a, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 1, Draft: &d})
	_, err = call(f.e, f.a, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 1, Draft: &d}, "")
	expectCode(t, err, "CAPABILITY_REVISION_CONFLICT")
	must(f.e, f.a, "submit-capability-request", Action{ID: r.ID, ExpectedRevision: 2, ExpectedSeq: 1, ExpectedManifestHash: f.hash})
	_, err = call(f.e, f.a, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 3, Draft: &d}, "")
	expectCode(t, err, "CAPABILITY_STATE_INVALID")
	preview, err := Preview(ctx, f.e.Q, f.a, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	refreshed := must(f.e, f.a, "refresh-capability-context", Action{ID: r.ID, ExpectedRevision: 3, ExpectedSeq: 2, ExpectedManifestHash: f.hash})
	if refreshed.Pack.Hash != preview.Hash {
		t.Fatal("refresh not reproducible")
	}
	_, err = Preview(ctx, f.e.Q, f.engineer, r.ID)
	expectCode(t, err, "NOT_FOUND")
	_, err = Download(ctx, f.e.Q, f.a, r.ID, 1)
	expectCode(t, err, "NOT_FOUND")
	for _, status := range []string{"pr_open", "fulfilled", "rejected"} {
		_, err = call(f.e, f.engineer, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 4, Transition: status}, "")
		expectCode(t, err, "CAPABILITY_STATE_INVALID")
	}
	must(f.e, f.engineer, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 4, Transition: "rejected", Reason: "Contract is out of scope"})
	_, err = call(f.e, f.a, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 5, Transition: "cancelled"}, "")
	expectCode(t, err, "CAPABILITY_STATE_INVALID")
	// Terminal keys may be requested again; stable cursor pages contain no duplicates.
	f.create()
	page, err := List(ctx, f.e.Q, f.a, Filter{Limit: 1})
	if err != nil || page["nextCursor"] == nil {
		t.Fatal(page, err)
	}
	last, err := List(ctx, f.e.Q, f.a, Filter{Limit: 1, Cursor: page["nextCursor"].(string)})
	if err != nil || len(arr(last["items"])) != 1 {
		t.Fatal(last, err)
	}
	_, err = List(ctx, f.e.Q, f.a, Filter{Limit: 1, Status: "submitted", Cursor: page["nextCursor"].(string)})
	expectCode(t, err, "CAPABILITY_CURSOR_INVALID")
	_, err = Get(ctx, f.e.Q, f.a, uuid.New())
	expectCode(t, err, "NOT_FOUND")
	noRights := f.a
	noRights.Rights = auth.RightSet{}
	_, err = Download(ctx, f.e.Q, noRights, r.ID, 3)
	expectCode(t, err, "NOT_FOUND")
	other := f.engineer
	other.ProjectID = uuid.New()
	_, err = Get(ctx, f.e.Q, other, r.ID)
	expectCode(t, err, "NOT_FOUND")
}

func TestDraftContractsAndSafeProjection(t *testing.T) {
	base := Draft{Title: "Action", Description: "A task", Acceptance: []string{"works"}, ProposedContract: raw(object{"args": object{}})}
	for _, field := range []object{{"type": "string", "required": true, "minLength": 1, "maxLength": 20}, {"type": "text"}, {"type": "number", "min": 0, "max": 10}, {"type": "boolean"}, {"type": "enum", "values": []string{"one", "two"}}, {"type": "reference", "schema": "Article"}, {"type": "link"}} {
		d := base
		d.ProposedContract = raw(object{"args": object{"value": field}})
		if err := validDraft(d); err != nil {
			t.Fatal(field, err)
		}
	}
	for _, field := range []object{{"type": "object", "fields": object{}}, {"type": "list", "of": object{"type": "string"}}, {"type": "string", "pattern": "secret"}, {"type": "number", "min": 2, "max": 1}, {"type": "enum", "values": []string{}}, {"type": "reference"}, {"type": "string", "required": "yes"}} {
		d := base
		d.ProposedContract = raw(object{"args": object{"value": field}})
		if validDraft(d) == nil {
			t.Fatal("invalid field accepted", field)
		}
	}
	for _, mutate := range []func(*Draft){func(d *Draft) { d.Title = "" }, func(d *Draft) { d.Description = strings.Repeat("a", 4001) }, func(d *Draft) { d.Acceptance = nil }, func(d *Draft) { d.Acceptance = []string{strings.Repeat("a", 501)} }, func(d *Draft) { d.ProposedContract = raw(object{"args": object{"Wrong": object{"type": "string"}}}) }, func(d *Draft) { d.ProposedContract = []byte(`null`) }} {
		d := base
		mutate(&d)
		if validDraft(d) == nil {
			t.Fatal(d)
		}
	}
	for _, url := range []string{"http://example.test/pr/1", "https://secret:password@example.test/pr/1", "javascript:alert(1)", "https://example.test/\n"} {
		if validURL(url) {
			t.Fatal(url)
		}
	}
	projection := typeProjection(object{"sourceRef": object{"file": "private"}, "props": object{"default": object{"type": "string", "default": "private"}}, "provides": object{"context": object{"type": "object", "fields": object{"x": object{"type": "string", "example": "private"}}}}}, 0)
	if strings.Contains(string(raw(projection)), "private") || obj(projection["props"])["default"] == nil {
		t.Fatal(projection)
	}
}
