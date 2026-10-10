package changes_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

const rebaseBase = `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{
  "n_root":{"id":"n_root","type":"Box","name":"v1","children":["n_aaa","n_bbb"]},
  "n_aaa":{"id":"n_aaa","type":"Box"},
  "n_bbb":{"id":"n_bbb","type":"Box"}}}`

// moveHead фиксирует новую версию объекта и делает её head (как публикация другого CS).
func (e *env) moveHead(obj uuid.UUID, body string) {
	e.t.Helper()
	ctx := context.Background()
	ver := uuid.New()
	if _, err := e.pool.Exec(ctx, `INSERT INTO object_versions (id, project_id, object_id, number, state, ir_version, body, body_hash, created_by, committed_at)
	  VALUES ($1, $2, $3, (SELECT max(number) + 1 FROM object_versions WHERE object_id = $3), 'committed', '1.0', $4, '\x00', $5, now())`,
		ver, e.actor.ProjectID, obj, body, e.actor.ID); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, "UPDATE objects SET head_version_id = $1 WHERE id = $2", ver, obj); err != nil {
		e.t.Fatal(err)
	}
}

// edited — rebaseBase с заменой фрагмента.
func edited(old, new string) string { return strings.Replace(rebaseBase, old, new, 1) }

func (e *env) rebase(cs uuid.UUID, resolutions map[uuid.UUID]string) changes.RebaseResult {
	e.t.Helper()
	ctx := context.Background()
	row, err := e.q.GetChangeset(ctx, store.GetChangesetParams{ID: cs, ProjectID: e.actor.ProjectID})
	if err != nil {
		e.t.Fatal(err)
	}
	res, err := changes.Rebase(ctx, e.q, row, resolutions, e.actor)
	if err != nil {
		e.t.Fatal(err)
	}
	return res
}

func (e *env) statuses(cs uuid.UUID) string {
	e.t.Helper()
	list, err := changes.ListOperations(context.Background(), e.q, e.actor.ProjectID, cs, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []string
	for _, op := range list {
		out = append(out, op.Status)
	}
	return strings.Join(out, ",")
}

func (e *env) hasConflicts(cs uuid.UUID) bool {
	d, _ := changes.GetChangeset(context.Background(), e.q, e.actor.ProjectID, cs)
	return d.HasConflicts
}

func rename(doc uuid.UUID, name string) map[string]any {
	return nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"`+name+`"}`)
}

func TestRebaseReplaysOntoNewHead(t *testing.T) {
	e := setup(t)
	doc := e.commitHead(rebaseBase)
	cs := e.createCS("b")
	if _, err := e.apply(cs.ID, 0, nodeOp(doc, "node.setProps", `{"nodeId":"n_aaa","set":{"as":"main"}}`)); err != nil {
		t.Fatal(err)
	}
	if res := e.rebase(cs.ID, nil); len(res.Rebased)+len(res.Removed)+len(res.Conflicts) != 0 {
		t.Errorf("head не двигался: %+v", res)
	}
	e.moveHead(doc, edited(`"name":"v1"`, `"name":"a"`))
	res := e.rebase(cs.ID, nil)
	if len(res.Rebased) != 1 || res.Rebased[0] != doc || len(res.Conflicts) != 0 || len(res.Removed) != 0 {
		t.Fatalf("%+v", res)
	}
	if got := e.body(doc, cs.ID); !strings.Contains(got, `"name":"a"`) || !strings.Contains(got, `"as":"main"`) {
		t.Errorf("рабочая версия: %s", got)
	}
	detail, _ := changes.GetChangeset(context.Background(), e.q, e.actor.ProjectID, cs.ID)
	head, _ := changes.GetDocument(context.Background(), e.q, e.actor.ProjectID, doc, nil)
	if *detail.Objects[0].BaseVersionID != head.VersionID || detail.HasConflicts {
		t.Errorf("база не обновлена: %+v", detail)
	}
	if res := e.rebase(cs.ID, nil); len(res.Rebased) != 0 {
		t.Errorf("повторный rebase: %+v", res)
	}
}

func TestRebaseConflictsAndResolutions(t *testing.T) {
	e := setup(t)
	doc := e.commitHead(rebaseBase)
	cs := e.createCS("b")
	if _, err := e.apply(cs.ID, 0, rename(doc, "b"),
		nodeOp(doc, "node.setProps", `{"nodeId":"n_aaa","set":{"as":"main"}}`),
		nodeOp(doc, "node.setProps", `{"nodeId":"n_bbb","set":{"as":"nav"}}`)); err != nil {
		t.Fatal(err)
	}
	e.moveHead(doc, strings.Replace(edited(`"name":"v1","children":["n_aaa","n_bbb"]`, `"name":"a","children":["n_bbb"]`), `"n_aaa":{"id":"n_aaa","type":"Box"},`, "", 1))

	res := e.rebase(cs.ID, nil)
	if len(res.Conflicts) != 2 || len(res.Rebased) != 0 || !e.hasConflicts(cs.ID) || e.statuses(cs.ID) != "conflict,conflict,applied" {
		t.Fatalf("%+v %s", res, e.statuses(cs.ID))
	}
	mismatch, inapplicable := res.Conflicts[0], res.Conflicts[1]
	if mismatch.Code != "BEFORE_MISMATCH" || mismatch.Target != doc || mismatch.Seq != 1 || mismatch.Type != "node.rename" ||
		mismatch.Expected.(map[string]any)["name"] != "v1" || mismatch.Current.(map[string]any)["name"] != "a" {
		t.Errorf("BEFORE_MISMATCH: %+v", mismatch)
	}
	if inapplicable.Code != "OPERATION_INAPPLICABLE" || inapplicable.Seq != 2 || inapplicable.Message == "" {
		t.Errorf("OPERATION_INAPPLICABLE: %+v", inapplicable)
	}
	if !strings.Contains(e.body(doc, cs.ID), `"name":"b"`) {
		t.Error("при конфликте рабочая версия не меняется")
	}

	// Решено только переименование; неприменимая операция с mine остаётся конфликтом,
	// а переименование возвращается в applied.
	res = e.rebase(cs.ID, map[uuid.UUID]string{mismatch.OperationID: changes.ResolutionMine, inapplicable.OperationID: changes.ResolutionMine})
	if len(res.Conflicts) != 1 || res.Conflicts[0].OperationID != inapplicable.OperationID || e.statuses(cs.ID) != "applied,conflict,applied" {
		t.Fatalf("%+v %s", res, e.statuses(cs.ID))
	}
	res = e.rebase(cs.ID, map[uuid.UUID]string{mismatch.OperationID: changes.ResolutionMine, inapplicable.OperationID: changes.ResolutionTheirs})
	if len(res.Conflicts) != 0 || len(res.Rebased) != 1 || e.hasConflicts(cs.ID) || e.statuses(cs.ID) != "applied,dropped,applied" {
		t.Fatalf("%+v %s", res, e.statuses(cs.ID))
	}
	got := e.body(doc, cs.ID)
	if !strings.Contains(got, `"name":"b"`) || !strings.Contains(got, `"as":"nav"`) || strings.Contains(got, "n_aaa") {
		t.Errorf("рабочая версия: %s", got)
	}
	list, _ := changes.ListOperations(context.Background(), e.q, e.actor.ProjectID, cs.ID, 0)
	var before map[string]any
	_ = json.Unmarshal(list[0].Before, &before)
	if before["name"] != "a" {
		t.Errorf("before пересчитан по новой базе: %s", list[0].Before)
	}
}

func TestRebaseTheirsDropsUndoAndObject(t *testing.T) {
	e := setup(t)
	doc := e.commitHead(rebaseBase)
	cs := e.createCS("b")
	if _, err := e.apply(cs.ID, 0, rename(doc, "b")); err != nil {
		t.Fatal(err)
	}
	e.must(e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 1}, ""))
	e.moveHead(doc, edited(`"name":"v1"`, `"name":"a"`))
	res := e.rebase(cs.ID, nil)
	if len(res.Conflicts) != 2 {
		t.Fatalf("%+v", res)
	}
	res = e.rebase(cs.ID, map[uuid.UUID]string{res.Conflicts[0].OperationID: changes.ResolutionTheirs})
	if len(res.Removed) != 1 || res.Removed[0] != doc || len(res.Rebased) != 0 || e.statuses(cs.ID) != "dropped,dropped" {
		t.Fatalf("%+v %s", res, e.statuses(cs.ID))
	}
	if d, _ := changes.GetChangeset(context.Background(), e.q, e.actor.ProjectID, cs.ID); len(d.Objects) != 0 {
		t.Errorf("объект остался в Change Set: %+v", d.Objects)
	}
}

func TestRebaseEmptyOperations(t *testing.T) {
	e := setup(t)
	doc := e.commitHead(rebaseBase)
	cs := e.createCS("b")
	if _, err := e.apply(cs.ID, 0,
		rename(doc, "same"),
		nodeOp(doc, "node.remove", `{"nodeId":"n_bbb"}`),
		nodeOp(doc, "node.insert", `{"parentId":"n_root","index":1,"subtree":{"id":"n_ccc","type":"Box"}}`),
		nodeOp(doc, "node.move", `{"nodeId":"n_ccc","parentId":"n_aaa"}`),
	); err != nil {
		t.Fatal(err)
	}
	// На новой базе: то же имя, детей нет — вставка по индексу 1 уходит в конец пустого списка.
	e.moveHead(doc, `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{
	  "n_root":{"id":"n_root","type":"Box","name":"same"}}}`)
	res := e.rebase(cs.ID, nil)
	if len(res.Conflicts) != 1 || res.Conflicts[0].Type != "node.move" {
		t.Fatalf("перенос в удалённый узел — конфликт: %+v", res)
	}
	res = e.rebase(cs.ID, map[uuid.UUID]string{res.Conflicts[0].OperationID: changes.ResolutionTheirs})
	if len(res.Conflicts) != 0 || e.statuses(cs.ID) != "applied,dropped,applied,dropped" {
		t.Fatalf("%+v %s", res, e.statuses(cs.ID))
	}
	if got := e.body(doc, cs.ID); !strings.Contains(got, `"children":["n_ccc"]`) || !strings.Contains(got, `"name":"same"`) {
		t.Errorf("индекс за концом списка — вставка в конец: %s", got)
	}
}

func TestRebaseObjectGone(t *testing.T) {
	e := setup(t)
	doc := e.commitHead(rebaseBase)
	cs := e.createCS("b")
	if _, err := e.apply(cs.ID, 0, rename(doc, "b"), nodeOp(doc, "node.setProps", `{"nodeId":"n_aaa","set":{"as":"main"}}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), "UPDATE objects SET head_version_id = NULL WHERE id = $1", doc); err != nil {
		t.Fatal(err)
	}
	res := e.rebase(cs.ID, map[uuid.UUID]string{uuid.New(): changes.ResolutionMine})
	if len(res.Conflicts) != 2 || res.Conflicts[0].Code != "OBJECT_GONE" || res.Conflicts[1].Seq != 2 {
		t.Fatalf("%+v", res)
	}
	first, second := res.Conflicts[0].OperationID, res.Conflicts[1].OperationID
	if res = e.rebase(cs.ID, map[uuid.UUID]string{first: changes.ResolutionTheirs}); len(res.Conflicts) != 1 || res.Conflicts[0].OperationID != second {
		t.Fatalf("решена только первая: %+v", res)
	}
	res = e.rebase(cs.ID, map[uuid.UUID]string{first: changes.ResolutionTheirs, second: changes.ResolutionTheirs})
	if len(res.Removed) != 1 || len(res.Conflicts) != 0 {
		t.Errorf("%+v", res)
	}
}

// Rebase обрабатывает все устаревшие объекты Change Set, а не только первый.
func TestRebaseSeveralObjects(t *testing.T) {
	e := setup(t)
	gone, removed, kept := e.commitHead(rebaseBase), e.commitHead(rebaseBase), e.commitHead(rebaseBase)
	cs := e.createCS("b")
	if _, err := e.apply(cs.ID, 0, rename(gone, "b"), nodeOp(removed, "node.remove", `{"nodeId":"n_bbb"}`), rename(kept, "b")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(context.Background(), "UPDATE objects SET head_version_id = NULL WHERE id = $1", gone); err != nil {
		t.Fatal(err)
	}
	e.moveHead(removed, strings.Replace(edited(`"children":["n_aaa","n_bbb"]`, `"children":["n_aaa"]`), `,
  "n_bbb":{"id":"n_bbb","type":"Box"}`, "", 1))
	e.moveHead(kept, edited(`"name":"v1","children":["n_aaa","n_bbb"]`, `"name":"v1","children":["n_bbb","n_aaa"]`))
	res := e.rebase(cs.ID, nil)
	if len(res.Conflicts) != 1 || res.Conflicts[0].Target != gone {
		t.Fatalf("%+v", res)
	}
	res = e.rebase(cs.ID, map[uuid.UUID]string{res.Conflicts[0].OperationID: changes.ResolutionTheirs})
	if len(res.Removed) != 2 || len(res.Rebased) != 1 || res.Rebased[0] != kept {
		t.Errorf("%+v", res)
	}
}
