package changes_test

import (
	"context"
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"testing"
)

func TestDataOperationHistoryRebaseSTU220229234(t *testing.T) {
	e := cntSetup(t)
	var manifest map[string]any
	_ = json.Unmarshal([]byte(cmstest.DefaultManifest), &manifest)
	manifest["dataSources"] = map[string]any{"commerce.products.list": map[string]any{"params": map[string]any{"collection": map[string]any{"type": "string", "required": true}}, "result": map[string]any{"type": "list", "of": map[string]any{"type": "string"}}}}
	raw, _ := json.Marshal(manifest)
	e.ActivateManifest("staging", raw)
	e.ActivateManifest("production", raw)
	cs := cntCS(e)
	made, err := cntOp(e, cs, "document.create", nil, map[string]any{"kind": "page", "path": "/data-sources", "root": map[string]any{"id": "n_root", "type": "Stack"}})
	if err != nil {
		t.Fatal(err)
	}
	page := made.Operations[0].Target
	use := map[string]any{"key": "featured", "use": map[string]any{"source": "commerce.products.list", "params": map[string]any{"collection": map[string]any{"lit": "summer"}}}}
	if _, err = cntOp(e, cs, "document.setDataSource", &page, use); err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil)
	d, err := changes.GetDocument(context.Background(), e.Q, e.Admin.ProjectID, page, &cs)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(d.Body, &body)
	if body["dataSources"] != nil {
		t.Fatal("undo retained declaration")
	}
	e.Must(e.Admin, "redo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil)
	if err := e.Do(e.Human("reader", auth.DesignRead), "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs), "operations": []any{map[string]any{"type": "document.setDataSource", "target": page, "payload": use}}}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	cntPublish(e, cs)
	left, right := cntCS(e), cntCS(e)
	changed := map[string]any{"key": "featured", "use": map[string]any{"source": "commerce.products.list", "params": map[string]any{"collection": map[string]any{"lit": "winter"}}}}
	if _, err := cntOp(e, left, "document.setDataSource", &page, changed); err != nil {
		t.Fatal(err)
	}
	if _, err := cntOp(e, right, "node.rename", &page, map[string]any{"nodeId": "n_root", "name": "upstream"}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, right)
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": left, "expectedSeq": cntSeq(e, left)}, nil)
	if _, err := cntOp(e, right, "document.setDataSource", &page, use); err == nil {
		t.Fatal("closed CS accepted")
	}
	right = cntCS(e)
	if _, err := cntOp(e, right, "document.setDataSource", &page, map[string]any{"key": "featured", "use": nil}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, right)
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": left, "expectedSeq": cntSeq(e, left)}, nil)
	current, err := e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: e.Admin.ProjectID, ID: left})
	if err != nil || !current.HasConflicts {
		t.Fatal("declaration changed without conflict", err)
	}
}
