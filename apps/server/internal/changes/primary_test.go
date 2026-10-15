package changes_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"testing"
)

func TestPrimaryCommandSTU204205208209214217(t *testing.T) {
	e := cntSetup(t)
	cs := cntCS(e)
	source := cntCreate(e, cs, "Article", cntArticle("first"))
	page, err := cntOp(e, cs, "document.create", nil, map[string]any{"kind": "page", "path": "/primary", "root": map[string]any{"id": "n_root", "type": "Heading", "bindings": map[string]any{"text": "$content.title"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := page.Operations[0].Target
	declaration := map[string]any{"schema": "Article", "resolve": map[string]any{"by": "entity", "entityId": source}}
	if _, err := cntOp(e, cs, "document.setContent", &id, map[string]any{"content": declaration}); err != nil {
		t.Fatal(err)
	}
	seq := cntSeq(e, cs)
	if err := changes.RequirePrimaryRead(context.Background(), e.Q, e.Human("design-viewer", auth.DesignRead), cs); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal("primary comparison lost read gate", err)
	}
	if err := changes.RequirePrimaryRead(context.Background(), e.Q, e.Human("content-viewer", auth.ContentRead), cs); err != nil {
		t.Fatal(err)
	}
	if _, err := cntOp(e, cs, "document.setContent", &id, map[string]any{"content": nil}); err == nil {
		t.Fatal("removed source with dependent binding")
	}
	if cntSeq(e, cs) != seq {
		t.Fatal("partial command committed")
	}
	missing := e.Human("compose-only", auth.DesignCompose)
	if err := e.Do(missing, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": seq, "operations": []any{map[string]any{"type": "document.setContent", "target": id, "payload": map[string]any{"content": nil}}}}, nil); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	foreignCS := cntCS(e)
	foreign := cntCreate(e, foreignCS, "Article", cntArticle("foreign"))
	invalid := map[string]any{"schema": "Article", "resolve": map[string]any{"by": "entity", "entityId": foreign}}
	if _, err := cntOp(e, cs, "document.setContent", &id, map[string]any{"content": invalid}); err == nil {
		t.Fatal("foreign draft visible")
	}
	group := []any{map[string]any{"type": "node.setBinding", "target": id, "payload": map[string]any{"nodeId": "n_root", "prop": "text", "binding": nil}}, map[string]any{"type": "document.setContent", "target": id, "payload": map[string]any{"content": nil}}}
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": seq, "group": true, "operations": group}, nil)
	e.Must(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": seq + 2}, nil)
	doc, err := changes.GetDocument(context.Background(), e.Q, e.Admin.ProjectID, id, &cs)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(doc.Body, &body)
	if body["content"] == nil {
		t.Fatal("undo lost source")
	}
	e.Must(e.Admin, "redo", map[string]any{"changesetId": cs, "expectedSeq": seq + 4}, nil)
	doc, err = changes.GetDocument(context.Background(), e.Q, e.Admin.ProjectID, id, &cs)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(doc.Body, &body)
	// JSON decoding into a reused map retains absent keys, so inspect a fresh map.
	body = map[string]any{}
	_ = json.Unmarshal(doc.Body, &body)
	if _, ok := body["content"]; ok {
		t.Fatal("redo wrote null instead of absence")
	}
}

func TestPrimaryRebaseSTU204217(t *testing.T) {
	e := cntSetup(t)
	a := e.ContentFixture("entity", "Article", cntArticle("source-a"), "staging")
	b := e.ContentFixture("entity", "Article", cntArticle("source-b"), "staging")
	c := e.ContentFixture("entity", "Article", cntArticle("source-c"), "staging")
	declare := func(id uuid.UUID) map[string]any {
		return map[string]any{"content": map[string]any{"schema": "Article", "resolve": map[string]any{"by": "entity", "entityId": id}}}
	}
	initial := cntCS(e)
	made, err := cntOp(e, initial, "document.create", nil, map[string]any{"kind": "page", "path": "/rebase-primary", "root": map[string]any{"id": "n_root", "type": "Heading", "bindings": map[string]any{"text": "$content.title"}}})
	if err != nil {
		t.Fatal(err)
	}
	page := made.Operations[0].Target
	if _, err := cntOp(e, initial, "document.setContent", &page, declare(a)); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, initial)
	left, right := cntCS(e), cntCS(e)
	if _, err := cntOp(e, left, "document.setContent", &page, declare(b)); err != nil {
		t.Fatal(err)
	}
	if _, err := cntOp(e, right, "node.rename", &page, map[string]any{"nodeId": "n_root", "name": "upstream"}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, right)
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": left, "expectedSeq": cntSeq(e, left)}, nil)
	d, err := changes.GetDocument(context.Background(), e.Q, e.Admin.ProjectID, page, &left)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(d.Body, &body)
	if body["content"].(map[string]any)["resolve"].(map[string]any)["entityId"] != b.String() || body["nodes"].(map[string]any)["n_root"].(map[string]any)["name"] != "upstream" {
		t.Fatal(body)
	}
	right = cntCS(e)
	if _, err := cntOp(e, right, "document.setContent", &page, declare(c)); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, right)
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": left, "expectedSeq": cntSeq(e, left)}, nil)
	cs, err := e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: e.Admin.ProjectID, ID: left})
	if err != nil || !cs.HasConflicts {
		t.Fatal("source changed without explicit resolution", cs, err)
	}
}

func TestPrimaryBindingRebaseSTU209217(t *testing.T) {
	e := cntSetup(t)
	article := e.ContentFixture("entity", "Article", cntArticle("rebase-binding"), "staging")
	author := e.ContentFixture("entity", "Author", map[string]any{"name": "Author"}, "staging")
	initial := cntCS(e)
	made, err := cntOp(e, initial, "document.create", nil, map[string]any{"kind": "page", "path": "/binding-rebase", "root": map[string]any{"id": "n_root", "type": "Heading"}})
	if err != nil {
		t.Fatal(err)
	}
	page := made.Operations[0].Target
	declare := func(cs uuid.UUID, schema string, source uuid.UUID) {
		t.Helper()
		if _, err := cntOp(e, cs, "document.setContent", &page, map[string]any{"content": map[string]any{"schema": schema, "resolve": map[string]any{"by": "entity", "entityId": source}}}); err != nil {
			t.Fatal(err)
		}
	}
	declare(initial, "Article", article)
	cntPublish(e, initial)
	left, right := cntCS(e), cntCS(e)
	e.Must(e.Admin, "apply-operations", map[string]any{"changesetId": left, "expectedSeq": 0, "group": true, "operations": []any{
		map[string]any{"type": "node.setBinding", "target": page, "payload": map[string]any{"nodeId": "n_root", "prop": "text", "binding": "$content.title"}},
	}}, nil)
	declare(right, "Author", author)
	cntPublish(e, right)
	seq := cntSeq(e, left)
	if err := e.Do(e.Admin, "rebase-changeset", map[string]any{"changesetId": left, "expectedSeq": seq}, nil); err == nil {
		t.Fatal("rebase silently retained a binding incompatible with the upstream source")
	}
	doc, err := changes.GetDocument(context.Background(), e.Q, e.Admin.ProjectID, page, &left)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	_ = json.Unmarshal(doc.Body, &body)
	if body["content"].(map[string]any)["schema"] != "Article" || cntSeq(e, left) != seq {
		t.Fatal("failed rebase partially committed", body)
	}
}

func TestPrimaryDeliverySTU210211215(t *testing.T) {
	e := cntSetup(t)
	cs := cntCS(e)
	source := cntCreate(e, cs, "Article", cntArticle("source"))
	result, err := cntOp(e, cs, "document.create", nil, map[string]any{"kind": "page", "path": "/primary", "root": map[string]any{"id": "n_root", "type": "Heading", "bindings": map[string]any{"text": "$content.title"}}})
	if err != nil {
		t.Fatal(err)
	}
	id := result.Operations[0].Target
	if _, err := cntOp(e, cs, "document.setContent", &id, map[string]any{"content": map[string]any{"schema": "Article", "resolve": map[string]any{"by": "entity", "entityId": source}}}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, cs)
	env, err := e.Q.GetEnvironmentByName(context.Background(), store.GetEnvironmentByNameParams{ProjectID: e.Admin.ProjectID, Name: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	access := delivery.Access{ProjectID: e.Admin.ProjectID, EnvironmentID: env.ID, Environment: "staging"}
	before, err := delivery.GetPage(context.Background(), e.Q, access, "/primary", "ru")
	if err != nil {
		t.Fatal(err)
	}
	content := before.Data["content"].(map[string]any)
	if len(content) != 1 || content["title"] != "Статья" {
		t.Fatal(before.Data)
	}
	en, err := delivery.GetPage(context.Background(), e.Q, access, "/primary", "en")
	if err != nil || en.Data["content"].(map[string]any)["title"] != "Article" {
		t.Fatal(en.Data, err)
	}
	if _, err := delivery.GetPage(context.Background(), e.Q, access, "/primary", "fr"); err == nil {
		t.Fatal("unknown locale accepted")
	}
	edit := cntCS(e)
	if _, err := cntOp(e, edit, "entity.setFields", &source, map[string]any{"set": map[string]any{"title.ru": "Новый заголовок"}}); err != nil {
		t.Fatal(err)
	}
	draft := access
	draft.Draft = true
	draft.ChangesetID = &edit
	preview, err := delivery.GetPage(context.Background(), e.Q, draft, "/primary", "ru")
	if err != nil || preview.Data["content"].(map[string]any)["title"] != "Новый заголовок" {
		t.Fatal(preview, err)
	}
	still, err := delivery.GetPage(context.Background(), e.Q, access, "/primary", "ru")
	if err != nil || still.Data["content"].(map[string]any)["title"] != "Статья" {
		t.Fatal("draft leaked", still, err)
	}
	cntPublish(e, edit)
	after, err := delivery.GetPage(context.Background(), e.Q, access, "/primary", "ru")
	if err != nil || after.Data["content"].(map[string]any)["title"] != "Новый заголовок" || after.Page.VersionID != before.Page.VersionID || after.ContentContext.Versions[source.String()] == before.ContentContext.Versions[source.String()] {
		t.Fatal(after, err)
	}
	// Versions remain in the service field, never inside the selected entity data.
	if after.ContentContext.Versions[source.String()] == uuid.Nil {
		t.Fatal("missing dependency version")
	}
}
