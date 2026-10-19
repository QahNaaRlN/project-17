package changes_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
)

var extractList = map[string]any{"type": "list", "of": map[string]any{"type": "object", "fields": map[string]any{"id": map[string]any{"type": "string"}, "title": map[string]any{"type": "text"}}}}

func extractSetup(t *testing.T) *cmstest.Env {
	e := cntSetup(t)
	var m map[string]any
	_ = json.Unmarshal([]byte(strings.TrimSuffix(cmstest.DefaultManifest, "}")+","+cntSchemas+"}"), &m)
	m["dataSources"] = map[string]any{"store.products": map[string]any{"result": extractList}}
	m["components"] = map[string]any{"Panel": map[string]any{"container": true, "props": map[string]any{}}}
	raw, _ := json.Marshal(m)
	e.ActivateManifest("staging", raw)
	e.ActivateManifest("production", raw)
	return e
}

// extractPage создаёт страницу с блоком шага 2 и локальными текстами.
func extractPage(e *cmstest.Env, cs uuid.UUID, path string) uuid.UUID {
	made, err := cntOp(e, cs, "document.create", nil, map[string]any{"kind": "page", "path": path, "root": map[string]any{
		"id": "n_root", "type": "Box", "children": []any{map[string]any{
			"id": "n_grid", "type": "Grid", "children": []any{map[string]any{
				"id": "n_stack", "type": "Stack", "children": []any{
					map[string]any{"id": "n_h1", "type": "Heading", "bindings": map[string]any{"text": "$local.t_title"}},
					map[string]any{"id": "n_txt", "type": "Text", "bindings": map[string]any{"text": "$local.t_desc"}},
					map[string]any{"id": "n_btn", "type": "Button", "bindings": map[string]any{"label": "$local.t_cta"}},
				}}}}}}})
	if err != nil {
		e.T.Fatal(err)
	}
	page := made.Operations[0].Target
	for key, text := range map[string]string{"t_title": "Новинки", "t_desc": "Описание", "t_cta": "Купить"} {
		if _, err := cntOp(e, cs, "localContent.set", &page, map[string]any{"key": key, "type": "text", "locale": "ru", "value": text}); err != nil {
			e.T.Fatal(err)
		}
	}
	return page
}

func extractPayload(name string) map[string]any {
	return map[string]any{
		"nodeId": "n_grid", "name": name,
		"inputs": map[string]any{
			"title":       map[string]any{"from": []any{map[string]any{"nodeId": "n_h1", "prop": "text"}}, "required": true},
			"description": map[string]any{"from": []any{map[string]any{"nodeId": "n_txt", "prop": "text"}}},
			"products":    map[string]any{"def": extractList},
		},
		"slots": map[string]any{"footer": map[string]any{"nodeIds": []any{"n_btn"}, "allowedTypes": []any{"Button", "Link"}, "max": 2}},
	}
}

func extractApply(e *cmstest.Env, actor auth.Actor, cs, page uuid.UUID, payload any) (changes.ApplyResult, error) {
	var r changes.ApplyResult
	err := e.Do(actor, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs), "group": true,
		"operations": []any{map[string]any{"type": "component.extract", "target": page, "payload": payload}}}, &r)
	return r, err
}

func extractBody(e *cmstest.Env, id uuid.UUID, cs *uuid.UUID) (map[string]any, error) {
	d, err := changes.GetDocument(context.Background(), e.Q, e.Admin.ProjectID, id, cs)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	err = json.Unmarshal(d.Body, &body)
	return body, err
}

func TestComponentExtractCommandSTU402407408(t *testing.T) {
	e := extractSetup(t)
	cs := cntCS(e)
	page := extractPage(e, cs, "/showcase")
	before, _ := extractBody(e, page, &cs)

	// STU-407: без component.write и без design.compose запись запрещена.
	designer := e.Human("compose-only", auth.DesignRead, auth.DesignCompose)
	if _, err := extractApply(e, designer, cs, page, extractPayload("ProductShowcase")); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatalf("component.write: %v", err)
	}
	// Сервер назначает ID: канонический payload от клиента и внутренний inverse отклоняются.
	forged := extractPayload("ProductShowcase")
	forged["componentId"] = uuid.NewString()
	if _, err := extractApply(e, e.Admin, cs, page, forged); opCode(err) != "PAYLOAD_INVALID" {
		t.Fatalf("canonical: %v", err)
	}
	if err := e.Do(e.Admin, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs), "operations": []any{map[string]any{"type": "component.revertExtract", "target": page, "payload": map[string]any{}}}}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Fatalf("revert: %v", err)
	}

	r, err := extractApply(e, e.Admin, cs, page, extractPayload("ProductShowcase"))
	if err != nil {
		t.Fatal(err)
	}
	after := r.Operations[0].After.(map[string]any)
	component := uuid.MustParse(after["componentId"].(string))
	instance := after["instanceNodeId"].(string)
	comp, err := extractBody(e, component, &cs)
	if err != nil {
		t.Fatal(err)
	}
	if comp["kind"] != "component" || comp["meta"].(map[string]any)["title"] != "ProductShowcase" || len(comp["inputs"].(map[string]any)) != 3 {
		t.Fatalf("component: %v", comp)
	}
	body, _ := extractBody(e, page, &cs)
	node := body["nodes"].(map[string]any)[instance].(map[string]any)
	if node["type"] != "Composed" || node["ref"].(map[string]any)["version"] != "live" || len(node["slots"].(map[string]any)["footer"].([]any)) != 1 {
		t.Fatalf("instance: %v", node)
	}
	var obj store.Object
	if obj, err = e.Q.GetObject(context.Background(), store.GetObjectParams{ID: component, ProjectID: e.Admin.ProjectID}); err != nil || *obj.DocKind != "component" {
		t.Fatal(err)
	}

	// STU-408: undo возвращает страницу байт-в-байт и исключает компонент; redo — тот же ID.
	e.Must(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil)
	restored, _ := extractBody(e, page, &cs)
	if mustJSON(t, restored) != mustJSON(t, before) {
		t.Fatalf("undo:\n%s\n%s", mustJSON(t, before), mustJSON(t, restored))
	}
	if _, err := extractBody(e, component, &cs); err == nil {
		t.Fatal("component stays in CS after undo")
	}
	e.Must(e.Admin, "redo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil)
	if again, err := extractBody(e, component, &cs); err != nil || mustJSON(t, again) != mustJSON(t, comp) {
		t.Fatalf("redo: %v", err)
	}
	// После правки компонента undo сначала отменяет её, затем извлечение.
	if _, err := cntOp(e, cs, "node.rename", &component, map[string]any{"nodeId": "n_stack", "name": "Колонка"}); err != nil {
		t.Fatal(err)
	}
	e.Must(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil)
	if err := e.Do(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil); err != nil {
		t.Fatalf("%v %s", err, mustJSON(t, err.(*commandbus.Error).Params))
	}
	if restored, _ := extractBody(e, page, &cs); mustJSON(t, restored) != mustJSON(t, before) {
		t.Fatal("second undo")
	}
	e.Must(e.Admin, "redo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil)

	// Q-6: имя уникально в проекте без учёта регистра.
	other := extractPage(e, cs, "/other")
	if _, err := extractApply(e, e.Admin, cs, other, extractPayload("productshowcase")); opCode(err) != "COMPONENT_NAME_TAKEN" {
		t.Fatalf("name: %v", err)
	}

	// STU-411: публикация атомарно выпускает страницу и компонент.
	cntPublish(e, cs)
	if head, err := extractBody(e, component, nil); err != nil || head["kind"] != "component" {
		t.Fatalf("component head: %v", err)
	}
}

func TestComponentExtractStrictSTU406(t *testing.T) {
	e := extractSetup(t)
	cs := cntCS(e)
	made, err := cntOp(e, cs, "document.create", nil, map[string]any{"kind": "page", "path": "/strict", "root": map[string]any{
		"id": "n_root", "type": "Box", "children": []any{map[string]any{
			"id": "n_zone", "type": "Panel", "zone": map[string]any{"id": "checkout", "mode": "STRICT"},
			"children": []any{map[string]any{"id": "n_inner", "type": "Panel"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	page := made.Operations[0].Target
	// Q-5: новый компонент не сертифицирован — в STRICT-месте извлечение отклоняется.
	_, err = extractApply(e, e.Admin, cs, page, map[string]any{"nodeId": "n_inner", "name": "Inner"})
	if err == nil || !strings.Contains(mustJSON(t, err.(*commandbus.Error).Params), "POLICY_COMPONENT_MODE_REQUIRED") {
		t.Fatalf("strict: %v %v", err, err.(*commandbus.Error).Params)
	}
	// Защищённое поддерево (зона внутри) не извлекается.
	if _, err = extractApply(e, e.Admin, cs, page, map[string]any{"nodeId": "n_zone", "name": "Zone"}); opCode(err) != "COMPONENT_EXTRACT_PROTECTED" {
		t.Fatalf("zone: %v", err)
	}
}

func TestComponentExtractRebaseSTU409(t *testing.T) {
	e := extractSetup(t)
	cs := cntCS(e)
	page := extractPage(e, cs, "/rebase")
	cntPublish(e, cs)

	// Совпадающее поддерево: извлечение переигрывается на новой базе.
	left := cntCS(e)
	r, err := extractApply(e, e.Admin, left, page, extractPayload("Rebased"))
	if err != nil {
		t.Fatal(err)
	}
	component := uuid.MustParse(r.Operations[0].After.(map[string]any)["componentId"].(string))
	instance := r.Operations[0].After.(map[string]any)["instanceNodeId"].(string)
	if _, err := cntOp(e, left, "node.rename", &page, map[string]any{"nodeId": instance, "name": "Витрина"}); err != nil {
		t.Fatal(err)
	}
	if _, err := cntOp(e, left, "node.rename", &component, map[string]any{"nodeId": "n_stack", "name": "Колонка"}); err != nil {
		t.Fatal(err)
	}
	right := cntCS(e)
	if _, err := cntOp(e, right, "node.rename", &page, map[string]any{"nodeId": "n_root", "name": "upstream"}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, right)
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": left, "expectedSeq": cntSeq(e, left)}, nil)
	if c, _ := e.Q.GetChangeset(context.Background(), store.GetChangesetParams{ProjectID: e.Admin.ProjectID, ID: left}); c.HasConflicts {
		t.Fatal("unchanged subtree must replay")
	}

	// Изменённое поддерево: конфликт только с theirs и каскадное исключение.
	right = cntCS(e)
	if _, err := cntOp(e, right, "node.rename", &page, map[string]any{"nodeId": "n_h1", "name": "changed"}); err != nil {
		t.Fatal(err)
	}
	cntPublish(e, right)
	var res changes.RebaseResult
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": left, "expectedSeq": cntSeq(e, left)}, &res)
	var extractOp uuid.UUID
	for _, c := range res.Conflicts {
		if c.Type == "component.extract" {
			if c.Code != "COMPONENT_EXTRACT_BASE_CHANGED" {
				t.Fatalf("code: %s", c.Code)
			}
			extractOp = c.OperationID
		}
	}
	if extractOp == uuid.Nil {
		t.Fatalf("no extract conflict: %+v", res.Conflicts)
	}
	// mine не принимается: конфликт остаётся.
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": left, "expectedSeq": cntSeq(e, left), "resolutions": []any{map[string]any{"operationId": extractOp, "choice": "mine"}}}, &res)
	if len(res.Conflicts) == 0 {
		t.Fatal("mine accepted")
	}
	e.Must(e.Admin, "rebase-changeset", map[string]any{"changesetId": left, "expectedSeq": cntSeq(e, left), "resolutions": []any{map[string]any{"operationId": extractOp, "choice": "theirs"}}}, &res)
	if len(res.Conflicts) != 0 {
		t.Fatalf("conflicts after theirs: %+v", res.Conflicts)
	}
	found := false
	for _, id := range res.Removed {
		found = found || id == component
	}
	if !found {
		t.Fatalf("component not removed: %+v", res)
	}
	if _, err := extractBody(e, component, &left); err == nil {
		t.Fatal("component remains in CS")
	}
	if body, _ := extractBody(e, page, &left); body["nodes"].(map[string]any)["n_grid"] == nil {
		t.Fatal("page must keep the subtree")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestComponentExtractRejectionsSTU407410(t *testing.T) {
	e := extractSetup(t)
	cs := cntCS(e)
	page := extractPage(e, cs, "/rejections")
	// component.write без design.compose недостаточно.
	writer := e.Human("component-only", auth.DesignRead, auth.ComponentWrite)
	var own changes.Changeset
	e.Must(writer, "create-changeset", map[string]any{"title": "own"}, &own)
	if _, err := extractApply(e, writer, own.ID, page, extractPayload("A")); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatalf("design.compose: %v", err)
	}
	bad := extractPayload("A")
	bad["unknown"] = true
	if _, err := extractApply(e, e.Admin, cs, page, bad); opCode(err) != "PAYLOAD_INVALID" {
		t.Fatalf("strict payload: %v", err)
	}
	missing := extractPayload("A")
	missing["nodeId"] = "n_missing"
	if _, err := extractApply(e, e.Admin, cs, page, missing); opCode(err) != "NODE_NOT_FOUND" {
		t.Fatalf("node: %v", err)
	}
	// План: чужой черновик доступен для чтения с причиной, неизвестный документ — 404.
	other := e.Human("other-designer", auth.DesignRead, auth.DesignCompose, auth.ComponentWrite)
	plan, err := changes.ExtractPlan(context.Background(), e.Q, other, page, cs, "n_grid", "staging")
	if err != nil || len(plan.Blockers) != 1 || plan.Blockers[0].Code != "CHANGESET_READ_ONLY" {
		t.Fatalf("other owner: %v %+v", err, plan.Blockers)
	}
	if _, err := changes.ExtractPlan(context.Background(), e.Q, e.Admin, uuid.New(), cs, "n_grid", ""); cmstest.Code(err) != "NOT_FOUND" {
		t.Fatalf("document: %v", err)
	}
	if _, err := changes.ExtractPlan(context.Background(), e.Q, e.Human("nobody"), page, cs, "n_grid", ""); cmstest.Code(err) != "FORBIDDEN" {
		t.Fatalf("design.read: %v", err)
	}
}

func TestComponentExtractLockedZoneSTU406(t *testing.T) {
	e := extractSetup(t)
	cs := cntCS(e)
	made, err := cntOp(e, cs, "document.create", nil, map[string]any{"kind": "page", "path": "/locked", "root": map[string]any{
		"id": "n_root", "type": "Box", "zone": map[string]any{"id": "header", "mode": "SYSTEM", "locked": true}, "children": []any{map[string]any{
			"id": "n_block", "type": "Stack", "children": []any{map[string]any{"id": "n_text", "type": "Text"}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	page := made.Operations[0].Target
	cntPublish(e, cs)
	designer := e.Human("designer-locked", auth.DesignRead, auth.DesignCompose, auth.ComponentWrite)
	var own changes.Changeset
	e.Must(designer, "create-changeset", map[string]any{"title": "locked"}, &own)
	if _, err := extractApply(e, designer, own.ID, page, map[string]any{"nodeId": "n_block", "name": "Locked"}); opCode(err) != "POLICY_ZONES_MANAGE_REQUIRED" {
		t.Fatalf("locked: %v", err)
	}
}

func TestComponentExtractContentInputSTU407(t *testing.T) {
	e := extractSetup(t)
	cs := cntCS(e)
	made, err := cntOp(e, cs, "document.create", nil, map[string]any{"kind": "page", "path": "/content/:slug",
		"content": map[string]any{"schema": "Article", "resolve": map[string]any{"by": "routeParam", "field": "slug", "param": "slug"}}, "root": map[string]any{
		"id": "n_root", "type": "Box", "children": []any{map[string]any{
			"id": "n_wrap", "type": "Stack", "children": []any{map[string]any{
				"id": "n_block", "type": "Stack", "children": []any{
					map[string]any{"id": "n_head", "type": "Heading", "bindings": map[string]any{"text": "$content.title"}},
					map[string]any{"id": "n_panel", "type": "Panel"}}}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	page := made.Operations[0].Target
	before, _ := extractBody(e, page, &cs)
	payload := map[string]any{"nodeId": "n_block", "name": "Content", "inputs": map[string]any{"title": map[string]any{"from": []any{map[string]any{"nodeId": "n_head", "prop": "text"}}}}}
	if _, err := extractApply(e, e.Human("no-content", auth.DesignRead, auth.DesignCompose, auth.ComponentWrite), cs, page, payload); err == nil {
		t.Fatal("content.read required")
	}
	r, err := extractApply(e, e.Admin, cs, page, payload)
	if err != nil {
		t.Fatal(err)
	}
	instance := r.Operations[0].After.(map[string]any)["instanceNodeId"].(string)
	// Тип входа вложенного Composed берётся из компонента этого CS.
	plan, err := changes.ExtractPlan(context.Background(), e.Q, e.Admin, page, cs, "n_wrap", "staging")
	if err != nil || len(plan.Blockers) != 0 {
		t.Fatalf("plan: %v %+v", err, plan.Blockers)
	}
	found := false
	for _, c := range plan.Candidates {
		found = found || c.NodeID == instance && c.Prop == "title" && c.External
	}
	if !found {
		t.Fatalf("nested instance input: %+v", plan.Candidates)
	}
	if err := e.Do(e.Admin, "undo", map[string]any{"changesetId": cs, "expectedSeq": cntSeq(e, cs)}, nil); err != nil {
		t.Fatalf("%v %s", err, mustJSON(t, err.(*commandbus.Error).Params))
	}
	if restored, _ := extractBody(e, page, &cs); mustJSON(t, restored) != mustJSON(t, before) {
		t.Fatal("undo with content input")
	}
}
