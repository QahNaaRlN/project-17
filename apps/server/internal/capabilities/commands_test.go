package capabilities

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }
func call(e *cmstest.Env, a auth.Actor, name string, p any, key string) (Result, error) {
	e.T.Helper()
	if key == "" {
		key = uuid.NewString()
	}
	resp, err := e.Bus.Dispatch(context.Background(), a, commandbus.Request{Name: name, IdempotencyKey: key, Reason: "explicit engineering workflow", Payload: raw(p)})
	var r struct{ Result Result }
	if err == nil {
		err = json.Unmarshal(resp.Body, &r)
	}
	return r.Result, err
}
func must(e *cmstest.Env, a auth.Actor, name string, p any) Result {
	e.T.Helper()
	r, err := call(e, a, name, p, "")
	if err != nil {
		e.T.Fatalf("%s: %v", name, err)
	}
	return r
}

type fixture struct {
	e           *cmstest.Env
	a, engineer auth.Actor
	cs, doc     uuid.UUID
	hash        string
	request     store.CapabilityRequest
}

func setup(t *testing.T) fixture {
	e := cmstest.New(t, Register, manifestregistry.Register)
	a := e.Human("designer", auth.DesignRead, auth.DesignCompose, auth.DesignZonesManage, auth.BehaviorUse, auth.CapabilityRequest)
	engineer := e.Human("engineer", auth.CodePropose)
	e.Exec(`UPDATE projects SET settings=settings || '{"defaultMode":"CODE","maxMode":"CODE"}' WHERE id=$1`, a.ProjectID)
	var cs changes.Changeset
	e.Must(a, "create-changeset", object{"title": "capability page", "targets": []string{"staging"}}, &cs)
	var made changes.ApplyResult
	e.Must(a, "apply-operations", object{"changesetId": cs.ID, "expectedSeq": 0, "operations": []any{object{"type": "document.create", "payload": object{"kind": "page", "path": "/favorite", "root": object{"id": "n_root", "type": "Button", "props": object{"label": "private-secret"}}}}}}, &made)
	doc := made.Operations[0].Target
	index := object{"repo": object{"root": ".", "commit": strings.Repeat("a", 40)}, "elements": object{}, "conventions": object{"registry": "src/registry.ts", "testCommand": "pnpm test", "manifestCommand": "pnpm cms build"}}
	indexHash, _ := manifest.Hash(index)
	m := parse([]byte(cmstest.DefaultManifest))
	m["codeIndex"] = object{"uploaded": false, "hash": indexHash}
	hash, _ := manifest.Hash(m)
	var upload manifestregistry.UploadedIndex
	e.Must(e.Admin, "upload-code-index", object{"manifestHash": hash, "indexHash": indexHash, "index": index}, &upload)
	id := upload.UploadID.String()
	e.Must(e.Admin, "register-manifest", manifestregistry.Payload{Manifest: raw(m), Environment: "staging", CodeIndexUploadID: &id}, nil)
	return fixture{e: e, a: a, engineer: engineer, cs: cs.ID, doc: doc, hash: hash}
}
func (f *fixture) create() store.CapabilityRequest {
	p := Create{Draft: Draft{Title: "Favorite", Description: "Save a favorite", ProposedContract: raw(object{"args": object{"value": object{"type": "string", "required": true}}}), Acceptance: []string{"An explicit click saves the choice"}}, Environment: "staging", ExpectedManifestHash: f.hash, ChangesetID: f.cs, ExpectedSeq: 1, DocumentID: f.doc, NodeID: "n_root", Event: "click", Key: "store.favorite"}
	cs, err := f.e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: f.a.ProjectID, ID: f.cs})
	if err != nil {
		f.e.T.Fatal(err)
	}
	p.ExpectedSeq = cs.Seq
	r := must(f.e, f.a, "create-capability-request", p)
	id := uuid.MustParse(r.Request["id"].(string))
	saved, err := f.e.Q.GetCapabilityRequest(context.Background(), store.GetCapabilityRequestParams{ProjectID: f.a.ProjectID, ID: id})
	if err != nil {
		f.e.T.Fatal(err)
	}
	f.request = saved
	return saved
}
func TestSubmitPackAtomicityAndHistory(t *testing.T) {
	f := setup(t)
	r := f.create()
	ctx := context.Background()
	if _, err := Get(ctx, f.e.Q, f.engineer, r.ID); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatal("draft exposed", err)
	}
	preview, err := Preview(ctx, f.e.Q, f.a, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	input := Action{ID: r.ID, ExpectedRevision: 1, ExpectedSeq: 1, ExpectedManifestHash: f.hash}
	result, err := call(f.e, f.a, "submit-capability-request", input, "submit-exact")
	if err != nil {
		t.Fatal(err)
	}
	if result.Change.Changeset.Seq != 2 || result.Pack.Hash != preview.Hash || len(result.Pack.Body) > 0 {
		t.Fatal("preview differs or command leaked pack", result)
	}
	if _, err = call(f.e, f.a, "submit-capability-request", input, "submit-exact"); err != nil {
		t.Fatal("exact retry", err)
	}
	view, err := Get(ctx, f.e.Q, f.a, r.ID)
	if err != nil || view["stale"] != false || view["anchorChanged"] != false {
		t.Fatal(view, err)
	}
	pack, err := Download(ctx, f.e.Q, f.engineer, r.ID, 2)
	if err != nil || pack.Hash != preview.Hash {
		t.Fatal(err, pack.Hash)
	}
	if strings.Contains(string(pack.Body), "private-secret") || strings.Contains(string(pack.Body), "localContent") || strings.Contains(string(pack.Body), "bindings") {
		t.Fatal("private data exported")
	}
	f.e.Must(f.a, "undo", object{"changesetId": f.cs, "expectedSeq": 2}, nil)
	view, err = Get(ctx, f.e.Q, f.a, r.ID)
	if err != nil || view["status"] != "submitted" || view["anchorChanged"] != true {
		t.Fatal(view, err)
	}
	must(f.e, f.a, "update-capability-request", Update{ID: r.ID, ExpectedRevision: 2, Transition: "cancelled"})
	f.e.Must(f.a, "redo", object{"changesetId": f.cs, "expectedSeq": 3}, nil)
	// A cancelled placeholder may only return through the protected history path.
	f.e.Must(f.a, "apply-operations", object{"changesetId": f.cs, "expectedSeq": 4, "operations": []any{object{"type": "node.setBehavior", "target": f.doc, "payload": object{"nodeId": "n_root", "event": "click", "handlers": nil}}}}, nil)
	err = f.e.Do(f.a, "apply-operations", object{"changesetId": f.cs, "expectedSeq": 5, "operations": []any{object{"type": "node.setBehavior", "target": f.doc, "payload": object{"nodeId": "n_root", "event": "click", "handlers": pending(r)}}}}, nil)
	if cmstest.Code(err) != "CAPABILITY_PENDING_INVALID" {
		t.Fatal(err)
	}
}
