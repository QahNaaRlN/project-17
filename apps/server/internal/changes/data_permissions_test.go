package changes_test

import (
	"encoding/json"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
	"strings"
	"testing"
)

func TestDataSourceAuthorReadRevokedSTU234(t *testing.T) {
	e := cntSetup(t)
	var manifest, schemas map[string]any
	_ = json.Unmarshal([]byte(cmstest.DefaultManifest), &manifest)
	_ = json.Unmarshal([]byte("{"+cntSchemas+"}"), &schemas)
	manifest["schemas"] = schemas["schemas"]
	manifest["dataSources"] = map[string]any{"test.items": map[string]any{"params": map[string]any{}, "result": map[string]any{"type": "string"}}}
	raw, _ := json.Marshal(manifest)
	e.ActivateManifest("staging", raw)
	entity := e.ContentFixture("entity", "Article", cntArticle("source"), "staging")
	initial := cntCS(e)
	made, err := cntOp(e, initial, "document.create", nil, map[string]any{"kind": "page", "content": map[string]any{"schema": "Article", "resolve": map[string]any{"by": "entity", "entityId": entity}}, "root": map[string]any{"id": "n_root", "type": "Stack"}})
	if err != nil {
		t.Fatal(err)
	}
	page := made.Operations[0].Target
	cntPublish(e, initial)
	author := e.Human("source-editor", auth.DesignRead, auth.DesignCompose, auth.ContentRead, auth.ContentPublish)
	cs := e.Edit(author, page, "source draft")
	e.Must(author, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs), "operations": []any{map[string]any{"type": "document.setDataSource", "target": page, "payload": map[string]any{"key": "featured", "use": map[string]any{"source": "test.items"}}}}}, nil)
	e.Exec(`UPDATE roles SET capabilities=array_remove(capabilities,'content.read') WHERE name='role-source-editor'`)
	var review workflow.Review
	e.Must(author, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs), "targets": []string{"staging"}}, &review)
	for _, check := range review.Checks {
		if check.Stage == "policy" && check.Status == "failed" && strings.Contains(string(check.Details), "document.setDataSource") && strings.Contains(string(check.Details), "content.read") {
			return
		}
	}
	t.Fatal("revoked source read permission was not rechecked", review)
}
