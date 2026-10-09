package workflow_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/cmstest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// rebaseEnv — стенд с публикацией и политикой без согласований.
func rebaseEnv(t *testing.T) *cmstest.Env {
	e := cmstest.New(t, workflow.Register, publishing.Register)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	return e
}

// ship подаёт и публикует Change Set в staging.
func ship(e *cmstest.Env, cs uuid.UUID, seq int) publishing.Publication {
	e.T.Helper()
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": seq}, nil)
	var p publishing.Publication
	e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, &p)
	return p
}

// published создаёт и публикует страницу; у корня n_root — дети n_aaa и n_bbb.
func published(e *cmstest.Env) (uuid.UUID, publishing.Publication) {
	e.T.Helper()
	cs, doc := e.Draft(e.Admin, "v1")
	if err := e.Apply(e.Admin, cs, 1, doc,
		cmstest.Op("node.insert", map[string]any{"parentId": "n_root", "subtree": map[string]any{"id": "n_aaa", "type": "Box"}}),
		cmstest.Op("node.insert", map[string]any{"parentId": "n_root", "subtree": map[string]any{"id": "n_bbb", "type": "Box"}}),
	); err != nil {
		e.T.Fatal(err)
	}
	return doc, ship(e, cs, 3)
}

// change создаёт Change Set с операциями над doc; возвращает его ID.
func change(e *cmstest.Env, doc uuid.UUID, operations ...map[string]any) uuid.UUID {
	e.T.Helper()
	var c changes.Changeset
	e.Must(e.Admin, "create-changeset", map[string]any{"title": "change"}, &c)
	if err := e.Apply(e.Admin, c.ID, 0, doc, operations...); err != nil {
		e.T.Fatal(err)
	}
	return c.ID
}

func rename(name string) map[string]any {
	return cmstest.Op("node.rename", map[string]any{"nodeId": "n_root", "name": name})
}

func rebase(e *cmstest.Env, cs uuid.UUID, seq int, resolutions ...workflow.Resolution) (workflow.RebaseOutcome, error) {
	var out workflow.RebaseOutcome
	err := e.Do(e.Admin, "rebase-changeset", map[string]any{"changesetId": cs, "expectedSeq": seq, "resolutions": resolutions}, &out)
	return out, err
}

type body struct {
	Nodes map[string]struct {
		Name     string
		Children []string
		Props    map[string]any
	}
}

func working(e *cmstest.Env, doc, cs uuid.UUID) body {
	e.T.Helper()
	d, err := changes.GetDocument(context.Background(), e.Q, e.Admin.ProjectID, doc, &cs)
	if err != nil {
		e.T.Fatal(err)
	}
	var b body
	if err := json.Unmarshal(d.Body, &b); err != nil {
		e.T.Fatal(err)
	}
	return b
}

func operations(e *cmstest.Env, cs uuid.UUID) []changes.Operation {
	e.T.Helper()
	list, err := changes.ListOperations(context.Background(), e.Q, e.Admin.ProjectID, cs, 0)
	if err != nil {
		e.T.Fatal(err)
	}
	return list
}

func TestRebaseWithoutConflicts(t *testing.T) {
	e := rebaseEnv(t)
	doc, _ := published(e)
	a := change(e, doc, rename("a"))
	b := change(e, doc,
		cmstest.Op("node.setProps", map[string]any{"nodeId": "n_aaa", "set": map[string]any{"as": "section"}}),
		// Вставка с генерируемыми ID и операция над вставленным узлом.
		cmstest.Op("node.insert", map[string]any{"parentId": "n_root", "index": 0, "subtree": map[string]any{"type": "Box", "children": []any{map[string]any{"type": "Box"}}}}),
	)
	before := working(e, doc, b)
	ship(e, a, 1)

	if err := e.Do(e.Admin, "submit-changeset", map[string]any{"changesetId": b, "expectedSeq": 2}, nil); cmstest.Code(err) != "REBASE_REQUIRED" {
		t.Fatalf("до rebase: %v", err)
	}
	out, err := rebase(e, b, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Rebased) != 1 || out.Rebased[0] != doc || len(out.Conflicts) != 0 || out.Changeset.HasConflicts || out.Checks != nil {
		t.Errorf("rebase: %+v", out)
	}
	after := working(e, doc, b)
	if after.Nodes["n_root"].Name != "a" || after.Nodes["n_aaa"].Props["as"] != "section" {
		t.Errorf("рабочая версия: %+v", after)
	}
	// Вставленные узлы сохранили ID: операция в журнале хранит поддерево с назначенными ID.
	if len(after.Nodes) != len(before.Nodes) || after.Nodes["n_root"].Children[0] != before.Nodes["n_root"].Children[0] {
		t.Errorf("ID вставленных узлов: было %+v, стало %+v", before.Nodes, after.Nodes)
	}
	ship(e, b, 2)
	head, _ := changes.GetDocument(context.Background(), e.Q, e.Admin.ProjectID, doc, nil)
	var h body
	_ = json.Unmarshal(head.Body, &h)
	if h.Nodes["n_root"].Name != "a" || h.Nodes["n_aaa"].Props["as"] != "section" || len(h.Nodes) != 5 {
		t.Errorf("head: %+v", h)
	}

	// Повторный rebase ничего не делает.
	c := change(e, doc, rename("c"))
	if out, err := rebase(e, c, 1); err != nil || len(out.Rebased)+len(out.Removed)+len(out.Conflicts) != 0 {
		t.Errorf("нечего переносить: %+v %v", out, err)
	}
}

func TestRebaseConflictMine(t *testing.T) {
	e := rebaseEnv(t)
	doc, _ := published(e)
	a, b := change(e, doc, rename("a")), change(e, doc, rename("b"))
	ship(e, a, 1)

	out, err := rebase(e, b, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Conflicts) != 1 || out.Conflicts[0].Code != "BEFORE_MISMATCH" || !out.Changeset.HasConflicts || len(out.Rebased) != 0 {
		t.Fatalf("конфликт: %+v", out)
	}
	c := out.Conflicts[0]
	if c.Target != doc || c.Seq != 1 || c.Type != "node.rename" {
		t.Errorf("описание конфликта: %+v", c)
	}
	if ops := operations(e, b); ops[0].Status != "conflict" {
		t.Errorf("статус операции: %+v", ops[0])
	}
	if working(e, doc, b).Nodes["n_root"].Name != "b" {
		t.Error("при конфликте рабочая версия не меняется")
	}
	if err := e.Do(e.Admin, "submit-changeset", map[string]any{"changesetId": b, "expectedSeq": 1}, nil); cmstest.Code(err) != "CHANGESET_HAS_CONFLICTS" {
		t.Errorf("подача с конфликтом: %v", err)
	}

	out, err = rebase(e, b, 1, workflow.Resolution{OperationID: c.OperationID, Choice: "mine"})
	if err != nil || len(out.Conflicts) != 0 || out.Changeset.HasConflicts || len(out.Rebased) != 1 {
		t.Fatalf("mine: %+v %v", out, err)
	}
	if working(e, doc, b).Nodes["n_root"].Name != "b" {
		t.Error("mine оставляет значение автора")
	}
	op := operations(e, b)[0]
	var opBefore map[string]any
	_ = json.Unmarshal(op.Before, &opBefore)
	if op.Status != "applied" || opBefore["name"] != "a" {
		t.Errorf("операция переиграна с новым before: %s %s", op.Status, op.Before)
	}
	ship(e, b, 1)
}

func TestRebaseConflictTheirsRemovesObject(t *testing.T) {
	e := rebaseEnv(t)
	doc, _ := published(e)
	a := change(e, doc, rename("a"))
	b := change(e, doc, rename("b"))
	// Отмена исключённой операции исключается вместе с ней.
	e.Must(e.Admin, "undo", map[string]any{"changesetId": b, "expectedSeq": 1}, nil)
	ship(e, a, 1)

	out, _ := rebase(e, b, 2)
	if len(out.Conflicts) != 2 {
		t.Fatalf("и операция, и её отмена конфликтуют: %+v", out.Conflicts)
	}
	out, err := rebase(e, b, 2, workflow.Resolution{OperationID: out.Conflicts[0].OperationID, Choice: "theirs"})
	if err != nil || len(out.Removed) != 1 || out.Removed[0] != doc || len(out.Rebased) != 0 || len(out.Conflicts) != 0 {
		t.Fatalf("theirs: %+v %v", out, err)
	}
	for _, op := range operations(e, b) {
		if op.Status != "dropped" {
			t.Errorf("операция %d: %s", op.Seq, op.Status)
		}
	}
	detail, _ := changes.GetChangeset(context.Background(), e.Q, e.Admin.ProjectID, b)
	if len(detail.Objects) != 0 {
		t.Errorf("объект вышел из Change Set: %+v", detail.Objects)
	}
	if err := e.Do(e.Admin, "submit-changeset", map[string]any{"changesetId": b, "expectedSeq": 2}, nil); cmstest.Code(err) != "CHANGESET_EMPTY" {
		t.Errorf("все операции исключены — подавать нечего: %v", err)
	}
}

func TestRebaseEmptyAndStructuralOperations(t *testing.T) {
	e := rebaseEnv(t)
	doc, _ := published(e)
	a := change(e, doc, rename("same"), cmstest.Op("node.remove", map[string]any{"nodeId": "n_bbb"}),
		cmstest.Op("node.remove", map[string]any{"nodeId": "n_aaa"}))
	b := change(e, doc,
		rename("same"), // на новой базе уже то же значение — операция пуста
		cmstest.Op("node.remove", map[string]any{"nodeId": "n_bbb"}),                                                                         // узел уже удалён
		cmstest.Op("node.insert", map[string]any{"parentId": "n_root", "index": 1, "subtree": map[string]any{"id": "n_ccc", "type": "Box"}}), // индекс за концом списка
	)
	ship(e, a, 3)
	out, err := rebase(e, b, 3)
	if err != nil || len(out.Conflicts) != 0 || len(out.Rebased) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	w := working(e, doc, b)
	if got := w.Nodes["n_root"].Children; len(got) != 1 || got[0] != "n_ccc" {
		t.Errorf("дети: %v", got)
	}
	statuses := []string{}
	for _, op := range operations(e, b) {
		statuses = append(statuses, op.Status)
	}
	if statuses[0] != "applied" || statuses[1] != "dropped" || statuses[2] != "applied" {
		t.Errorf("статусы: %v", statuses)
	}
}

func TestRebaseInapplicableOperation(t *testing.T) {
	e := rebaseEnv(t)
	doc, _ := published(e)
	a := change(e, doc, rename("a"), cmstest.Op("node.remove", map[string]any{"nodeId": "n_aaa"}))
	b := change(e, doc, rename("b"), cmstest.Op("node.setProps", map[string]any{"nodeId": "n_aaa", "set": map[string]any{"as": "section"}}))
	ship(e, a, 2)
	out, _ := rebase(e, b, 2)
	if len(out.Conflicts) != 2 || out.Conflicts[1].Code != "OPERATION_INAPPLICABLE" {
		t.Fatalf("%+v", out)
	}
	renameID, id := out.Conflicts[0].OperationID, out.Conflicts[1].OperationID
	// Конфликт переименования разрешён; «оставить моё» для неприменимой операции
	// невозможно — её конфликт остаётся, а переименование возвращается в applied.
	out, _ = rebase(e, b, 2, workflow.Resolution{OperationID: renameID, Choice: "mine"}, workflow.Resolution{OperationID: id, Choice: "mine"})
	if len(out.Conflicts) != 1 || out.Conflicts[0].OperationID != id {
		t.Errorf("mine: %+v", out)
	}
	if st := operations(e, b); st[0].Status != "applied" || st[1].Status != "conflict" {
		t.Errorf("статусы: %s %s", st[0].Status, st[1].Status)
	}
	out, _ = rebase(e, b, 2, workflow.Resolution{OperationID: renameID, Choice: "mine"}, workflow.Resolution{OperationID: id, Choice: "theirs"})
	if len(out.Rebased) != 1 || out.Changeset.HasConflicts || working(e, doc, b).Nodes["n_root"].Name != "b" {
		t.Errorf("theirs: %+v", out)
	}
}

func TestRebaseAfterFirstPublicationRolledBack(t *testing.T) {
	e := rebaseEnv(t)
	doc, p1 := published(e)
	b := change(e, doc, rename("b"))
	e.Must(e.Admin, "rollback", map[string]any{"publicationId": p1.ID}, nil)
	out, _ := rebase(e, b, 1)
	if len(out.Conflicts) != 1 || out.Conflicts[0].Code != "OBJECT_GONE" {
		t.Fatalf("%+v", out)
	}
	out, err := rebase(e, b, 1, workflow.Resolution{OperationID: out.Conflicts[0].OperationID, Choice: "theirs"})
	if err != nil || len(out.Removed) != 1 || len(out.Conflicts) != 0 {
		t.Errorf("%+v %v", out, err)
	}
}

func TestRebaseKeepsApprovals(t *testing.T) {
	e := rebaseEnv(t)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 1, "high": 1}, nil)
	reviewer := e.Human("reviewer", auth.ContentPublish)
	doc, _ := func() (uuid.UUID, any) {
		cs, doc := e.Draft(e.Admin, "v1")
		e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)
		e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil)
		e.Must(e.Admin, "publish", map[string]any{"changesetId": cs, "environment": "staging"}, nil)
		return doc, nil
	}()
	approve := func(cs uuid.UUID) {
		e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)
		e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": cs}, nil)
	}
	a := change(e, doc, rename("a"))
	b := change(e, doc, cmstest.Op("node.setProps", map[string]any{"nodeId": "n_root", "set": map[string]any{"as": "main"}}))
	c := change(e, doc, rename("c"))
	approve(a)
	approve(b)
	approve(c)
	e.Must(e.Admin, "publish", map[string]any{"changesetId": a, "environment": "staging"}, nil)

	// CHG-040: без конфликтов согласование сохраняется, проверки повторены.
	out, err := rebase(e, b, 1)
	if err != nil || out.Changeset.State != "approved" || len(out.Checks) != 2 {
		t.Fatalf("без конфликтов: %+v %v", out, err)
	}
	detail, _ := workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, b)
	if detail.Approvals != 1 || len(detail.Checks) != 2 {
		t.Errorf("согласования после rebase: %+v", detail)
	}
	e.Must(e.Admin, "publish", map[string]any{"changesetId": b, "environment": "staging"}, nil)

	// CHG-041: конфликт сбрасывает согласования и возвращает в open.
	out, _ = rebase(e, c, 1)
	if out.Changeset.State != "open" || !out.Changeset.HasConflicts {
		t.Errorf("с конфликтом: %+v", out)
	}
	detail, _ = workflow.GetReview(context.Background(), e.Q, e.Admin.ProjectID, c)
	if detail.Approvals != 0 {
		t.Errorf("согласования сброшены: %+v", detail)
	}
}

func TestRebaseRecomputesState(t *testing.T) {
	e := rebaseEnv(t)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 1, "high": 1}, nil)
	reviewer := e.Human("reviewer", auth.ContentPublish)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 0, "high": 0}, nil)
	doc, _ := published(e)
	e.Must(e.Admin, "set-approval-policy", map[string]int{"low": 0, "medium": 1, "high": 1}, nil)
	a := change(e, doc, rename("a"))
	waiting := change(e, doc, cmstest.Op("node.setProps", map[string]any{"nodeId": "n_root", "set": map[string]any{"as": "main"}}))
	asked := change(e, doc, cmstest.Op("node.setProps", map[string]any{"nodeId": "n_aaa", "set": map[string]any{"as": "aside"}}))
	broken := change(e, doc, cmstest.Op("node.setProps", map[string]any{"nodeId": "n_bbb", "set": map[string]any{"as": "nav"}}))
	for _, cs := range []uuid.UUID{a, waiting, asked, broken} {
		e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": cs, "expectedSeq": 1}, nil)
	}
	e.Must(reviewer, "approve-changeset", map[string]any{"changesetId": a}, nil)
	e.Must(reviewer, "request-changes", map[string]any{"changesetId": asked, "comment": "нет"}, nil)
	e.Must(e.Admin, "publish", map[string]any{"changesetId": a, "environment": "staging"}, nil)

	if out, _ := rebase(e, waiting, 1); out.Changeset.State != "in_review" {
		t.Errorf("ожидает согласования: %+v", out.Changeset)
	}
	if out, _ := rebase(e, asked, 1); out.Changeset.State != "changes_requested" {
		t.Errorf("запрошены изменения: %+v", out.Changeset)
	}
	// Автор потерял право — проверка policy после rebase не проходит.
	e.Exec("UPDATE roles SET capabilities = ARRAY['content.publish'] WHERE name = 'admin'")
	if out, err := rebase(e, broken, 1); err != nil || out.Changeset.State != "failed" {
		t.Errorf("проверки не пройдены: %+v %v", out.Changeset, err)
	}
}

func TestRebaseErrors(t *testing.T) {
	e := rebaseEnv(t)
	doc, _ := published(e)
	b := change(e, doc, rename("b"))
	other := e.Human("other", auth.ContentWrite)
	cases := []struct {
		name    string
		actor   auth.Actor
		payload map[string]any
		code    string
	}{
		{"чужой", other, map[string]any{"changesetId": b, "expectedSeq": 1}, "CHANGESET_NOT_OWNER"},
		{"seq", e.Admin, map[string]any{"changesetId": b, "expectedSeq": 0}, "CHANGESET_SEQ_CONFLICT"},
		{"нет", e.Admin, map[string]any{"changesetId": uuid.New(), "expectedSeq": 0}, "NOT_FOUND"},
		{"решение", e.Admin, map[string]any{"changesetId": b, "expectedSeq": 1, "resolutions": []any{map[string]any{"operationId": uuid.New(), "choice": "both"}}}, "VALIDATION_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := e.Do(tc.actor, "rebase-changeset", tc.payload, nil); cmstest.Code(err) != tc.code {
				t.Errorf("%v", err)
			}
		})
	}
	many := make([]any, workflow.MaxResolutions+1)
	for i := range many {
		many[i] = map[string]any{"operationId": uuid.New(), "choice": "mine"}
	}
	if err := e.Do(e.Admin, "rebase-changeset", map[string]any{"changesetId": b, "expectedSeq": 1, "resolutions": many}, nil); cmstest.Code(err) != "VALIDATION_FAILED" {
		t.Errorf("много решений: %v", err)
	}
	exact := many[:workflow.MaxResolutions]
	if err := e.Do(e.Admin, "rebase-changeset", map[string]any{"changesetId": b, "expectedSeq": 1, "resolutions": exact}, nil); err != nil {
		t.Errorf("ровно %d решений: %v", workflow.MaxResolutions, err)
	}
	// Поданный Change Set без изменившихся объектов rebase не трогает.
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": b, "expectedSeq": 1}, nil)
	if out, err := rebase(e, b, 1); err != nil || out.Checks != nil || out.Changeset.State != "approved" {
		t.Errorf("нечего переносить: %+v %v", out, err)
	}
	e.Must(e.Admin, "publish", map[string]any{"changesetId": b, "environment": "staging"}, nil)
	if _, err := rebase(e, b, 1); cmstest.Code(err) != "CHANGESET_STATE_INVALID" {
		t.Errorf("слитый Change Set: %v", err)
	}
}

func TestRebaseDroppingEverythingReopens(t *testing.T) {
	e := rebaseEnv(t)
	doc, _ := published(e)
	remove := cmstest.Op("node.remove", map[string]any{"nodeId": "n_bbb"})
	a := change(e, doc, remove)
	b := change(e, doc, cmstest.Op("node.remove", map[string]any{"nodeId": "n_bbb"}))
	e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": b, "expectedSeq": 1}, nil)
	ship(e, a, 1)
	// Узел уже удалён — операция пуста и исключается; поданный Change Set опустел.
	out, err := rebase(e, b, 1)
	if err != nil || len(out.Removed) != 1 || out.Changeset.State != "open" || out.Checks != nil {
		t.Fatalf("%+v %v", out, err)
	}
	if err := e.Do(e.Admin, "submit-changeset", map[string]any{"changesetId": b, "expectedSeq": 1}, nil); cmstest.Code(err) != "CHANGESET_EMPTY" {
		t.Errorf("%v", err)
	}
}

// Сбои БД на каждом шаге rebase прерывают команду, не оставляя частичных изменений.
func TestRebaseDatabaseFaults(t *testing.T) {
	type scenario func(e *cmstest.Env) (cs uuid.UUID, seq int)
	clean := func(submitted bool) scenario {
		return func(e *cmstest.Env) (uuid.UUID, int) {
			doc, _ := published(e)
			a := change(e, doc, rename("a"))
			b := change(e, doc, cmstest.Op("node.setProps", map[string]any{"nodeId": "n_aaa", "set": map[string]any{"as": "main"}}))
			if submitted {
				e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": b, "expectedSeq": 1}, nil)
			}
			ship(e, a, 1)
			return b, 1
		}
	}
	conflicting := func(submitted bool) scenario {
		return func(e *cmstest.Env) (uuid.UUID, int) {
			doc, _ := published(e)
			a, b := change(e, doc, rename("a")), change(e, doc, rename("b"))
			if submitted {
				e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": b, "expectedSeq": 1}, nil)
			}
			ship(e, a, 1)
			return b, 1
		}
	}
	dropping := func(e *cmstest.Env) (uuid.UUID, int) {
		doc, _ := published(e)
		a := change(e, doc, cmstest.Op("node.remove", map[string]any{"nodeId": "n_bbb"}))
		b := change(e, doc, cmstest.Op("node.remove", map[string]any{"nodeId": "n_bbb"}))
		e.Must(e.Admin, "submit-changeset", map[string]any{"changesetId": b, "expectedSeq": 1}, nil)
		ship(e, a, 1)
		return b, 1
	}
	contentHashChange := `CREATE FUNCTION cms_test_fail() RETURNS trigger LANGUAGE plpgsql AS
		$$BEGIN RAISE EXCEPTION 'сбой, внесённый тестом'; END$$;
		CREATE TRIGGER cms_test_fail BEFORE UPDATE ON changesets FOR EACH ROW
		WHEN (NEW.content_hash IS DISTINCT FROM OLD.content_hash) EXECUTE FUNCTION cms_test_fail()`
	for name, tc := range map[string]struct {
		setup scenario
		brk   func(e *cmstest.Env)
	}{
		"mismatches":       {clean(false), func(e *cmstest.Env) { e.Break("changeset_objects", "") }},
		"working":          {clean(false), func(e *cmstest.Env) { e.Break("object_versions", "") }},
		"operations":       {clean(false), func(e *cmstest.Env) { e.Break("operations", "") }},
		"replay":           {clean(false), func(e *cmstest.Env) { e.Break("operations", "UPDATE") }},
		"working update":   {clean(false), func(e *cmstest.Env) { e.Break("object_versions", "UPDATE") }},
		"base":             {clean(false), func(e *cmstest.Env) { e.Break("changeset_objects", "UPDATE") }},
		"flag":             {clean(false), func(e *cmstest.Env) { e.Break("changesets", "UPDATE") }},
		"conflict status":  {conflicting(false), func(e *cmstest.Env) { e.Break("operations", "UPDATE") }},
		"conflict reopen":  {conflicting(true), func(e *cmstest.Env) { e.Break("approvals", "UPDATE") }},
		"remove object":    {dropping, func(e *cmstest.Env) { e.Break("changeset_objects", "DELETE") }},
		"remove version":   {dropping, func(e *cmstest.Env) { e.Break("object_versions", "DELETE") }},
		"emptied reopen":   {dropping, func(e *cmstest.Env) { e.Break("approvals", "UPDATE") }},
		"recheck":          {clean(true), func(e *cmstest.Env) { e.Break("checks", "INSERT") }},
		"rebind approvals": {clean(true), func(e *cmstest.Env) { e.Break("approvals", "UPDATE") }},
		"count approvals": {clean(true), func(e *cmstest.Env) {
			e.Exec("DROP INDEX approvals_by_cs; ALTER TABLE approvals RENAME COLUMN decision TO broken")
		}},
		"resubmit": {clean(true), func(e *cmstest.Env) { e.Exec(contentHashChange) }},
	} {
		t.Run(name, func(t *testing.T) {
			e := rebaseEnv(t)
			cs, seq := tc.setup(e)
			tc.brk(e)
			_, err := rebase(e, cs, seq)
			cmstest.ExpectDBError(t, err)
		})
	}
}
