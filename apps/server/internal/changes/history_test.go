package changes_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"pgregory.net/rapid"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
)

func (e *env) group(cs uuid.UUID, seq int, ops ...map[string]any) changes.ApplyResult {
	e.t.Helper()
	var result changes.ApplyResult
	raw := e.must(e.dispatch(e.actor, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": seq, "group": true, "operations": ops}, "Studio action"))
	if err := json.Unmarshal(raw, &result); err != nil {
		e.t.Fatal(err)
	}
	return result
}
func (e *env) historyCommand(cs uuid.UUID, seq int, action string) changes.ApplyResult {
	e.t.Helper()
	var result changes.ApplyResult
	raw := e.must(e.dispatch(e.actor, action, map[string]any{"changesetId": cs, "expectedSeq": seq}, "Studio history"))
	if err := json.Unmarshal(raw, &result); err != nil {
		e.t.Fatal(err)
	}
	return result
}
func TestStudioHistoryGroupedText(t *testing.T) {
	// STU-051/054: one undo restores the entire text gesture, all locales, and its binding.
	e := setup(t)
	cs := e.createCS("grouped text")
	created, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Heading","bindings":{"text":"$local.t_shared"}}`))
	// Local content is part of the document, not the nested root; initialise through public operations.
	doc := created.Operations[0].Target
	e.must(e.dispatch(e.actor, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "operations": []any{nodeOp(doc, "localContent.set", `{"key":"t_shared","type":"text","value":{"ru":"До","en":"Original"}}`)}}, "initial"))
	before := e.body(doc, cs.ID)
	edited := e.group(cs.ID, 2, nodeOp(doc, "localContent.set", `{"key":"t_private","type":"text","value":{"ru":"После","en":"Original"}}`), nodeOp(doc, "node.setBinding", `{"nodeId":"n_root","prop":"text","binding":"$local.t_private"}`))
	after := e.body(doc, cs.ID)
	if before == after {
		t.Fatal("no edit")
	}
	undone := e.historyCommand(cs.ID, int(edited.Changeset.Seq), "undo")
	if len(undone.Operations) != 2 || e.body(doc, cs.ID) != before {
		t.Fatal("text/binding undo was partial")
	}
	redone := e.historyCommand(cs.ID, int(undone.Changeset.Seq), "redo")
	if len(redone.Operations) != 2 || e.body(doc, cs.ID) != after {
		t.Fatal("redo did not restore exact document")
	}
	rows, err := changes.ListOperations(context.Background(), e.q, e.actor.ProjectID, cs.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if rows[2].BatchID == nil || *rows[2].BatchID != *rows[3].BatchID || rows[4].UndoOf == nil || *rows[4].UndoOf != rows[3].ID || rows[6].RedoOf == nil || *rows[6].RedoOf != rows[5].ID {
		t.Fatal("immutable history links", rows)
	}
	h, err := changes.GetHistory(context.Background(), e.q, e.actor, cs.ID, math.MaxInt32)
	if err != nil || len(h.Items) != 5 || h.Items[0].Kind != "redo" || len(h.Items[0].Operations) != 2 || !h.CanUndo || h.CanRedo {
		t.Fatalf("history: %+v %v", h, err)
	}
}
func TestStudioHistoryStackAndNewEdit(t *testing.T) {
	// STU-051/054: several undo/redo actions form a stack; a normal edit discards redo.
	e := setup(t)
	cs := e.createCS("stack")
	created, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := created.Operations[0].Target
	seq := 1
	for _, name := range []string{"A", "B", "C"} {
		r := e.group(cs.ID, seq, nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"`+name+`"}`))
		seq = int(r.Changeset.Seq)
	}
	for _, action := range []string{"undo", "undo", "redo", "redo", "undo", "redo"} {
		seq = int(e.historyCommand(cs.ID, seq, action).Changeset.Seq)
	}
	if got := e.body(doc, cs.ID); !containsName(got, "C") {
		t.Fatal(got)
	}
	seq = int(e.historyCommand(cs.ID, seq, "undo").Changeset.Seq)
	seq = int(e.group(cs.ID, seq, nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"New"}`)).Changeset.Seq)
	if _, err := e.dispatch(e.actor, "redo", map[string]any{"changesetId": cs.ID, "expectedSeq": seq}, ""); code(err) != "NOTHING_TO_REDO" {
		t.Fatal(err)
	}
}
func containsName(body, name string) bool {
	var v struct {
		Nodes map[string]struct{ Name string }
	}
	_ = json.Unmarshal([]byte(body), &v)
	return v.Nodes["n_root"].Name == name
}
func TestStudioHistoryWholeGroupUnsupportedAndRights(t *testing.T) {
	// STU-054: no partial rollback when one operation lacks inverse or current permission.
	e := setup(t)
	cs := e.createCS("mixed")
	r := e.group(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`), createDoc(`{"id":"n_root","type":"Box"}`))
	before := e.body(r.Operations[0].Target, cs.ID)
	if _, err := e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 2}, ""); code(err) != "UNDO_NOT_SUPPORTED" {
		t.Fatal(err)
	}
	if e.body(r.Operations[0].Target, cs.ID) != before {
		t.Fatal("partial undo")
	}
	doc := r.Operations[0].Target
	r = e.group(cs.ID, 2, nodeOp(doc, "localContent.set", `{"key":"t_text","type":"text","locale":"ru","value":"Text"}`), nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"Changed"}`))
	before = e.body(doc, cs.ID)
	writer := e.actor
	writer.Rights = auth.RightSet{auth.DesignCompose: true, auth.DesignRead: true}
	if _, err := e.dispatch(writer, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": r.Changeset.Seq}, ""); code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	if e.body(doc, cs.ID) != before {
		t.Fatal("permission failure partially changed group")
	}
	h, err := changes.GetHistory(context.Background(), e.q, writer, cs.ID, math.MaxInt32)
	if err != nil || h.CanUndo || *h.UndoReason != "FORBIDDEN" {
		t.Fatalf("%+v %v", h, err)
	}
	other := e.actor
	other.ID = uuid.New()
	if _, err := e.dispatch(other, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": r.Changeset.Seq}, ""); code(err) != "CHANGESET_NOT_OWNER" {
		t.Fatal(err)
	}
	if _, err := e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 0}, ""); code(err) != "CHANGESET_SEQ_CONFLICT" {
		t.Fatal(err)
	}
}
func TestStudioHistoryLegacyRedoAndBarrier(t *testing.T) {
	// STU-054: old ungrouped operations remain individually undoable; rebase clears redo.
	e := setup(t)
	cs := e.createCS("legacy")
	r, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := r.Operations[0].Target
	e.must(e.dispatch(e.actor, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "operations": []any{nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"A"}`), nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"B"}`)}}, "legacy"))
	u := e.historyCommand(cs.ID, 3, "undo")
	if len(u.Operations) != 1 || !containsName(e.body(doc, cs.ID), "A") {
		t.Fatal(u)
	}
	r = e.historyCommand(cs.ID, 4, "redo")
	if len(r.Operations) != 1 || !containsName(e.body(doc, cs.ID), "B") {
		t.Fatal(r)
	}
	e.historyCommand(cs.ID, 5, "undo")
	raw, err := e.q.GetChangeset(context.Background(), store.GetChangesetParams{ID: cs.ID, ProjectID: e.actor.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changes.Rebase(context.Background(), e.q, raw, nil, e.actor); err != nil {
		t.Fatal(err)
	}
	if _, err := e.dispatch(e.actor, "redo", map[string]any{"changesetId": cs.ID, "expectedSeq": 6}, ""); code(err) != "NOTHING_TO_REDO" {
		t.Fatal(err)
	}
}
func TestStudioHistoryPaginationAndReadonly(t *testing.T) {
	// STU-050/054: cursor pages contain complete groups and never expose raw payload/value.
	e := setup(t)
	cs := e.createCS("pages")
	r, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := r.Operations[0].Target
	seq := 1
	for i := 0; i < 23; i++ {
		seq = int(e.group(cs.ID, seq, nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"one"}`), nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"two"}`)).Changeset.Seq)
	}
	h, err := changes.GetHistory(context.Background(), e.q, e.actor, cs.ID, math.MaxInt32)
	if err != nil || len(h.Items) != 20 || h.NextBeforeSeq == nil {
		t.Fatalf("%+v %v", h, err)
	}
	older, err := changes.GetHistory(context.Background(), e.q, e.actor, cs.ID, *h.NextBeforeSeq)
	if err != nil || len(older.Items) != 4 || older.NextBeforeSeq != nil {
		t.Fatalf("%+v %v", older, err)
	}
	for _, item := range h.Items {
		if len(item.Operations) != 2 || item.LastSeq-item.FirstSeq != 1 {
			t.Fatal(item)
		}
	}
	if err := e.q.SetChangesetState(context.Background(), store.SetChangesetStateParams{ID: cs.ID, State: "in_review"}); err != nil {
		t.Fatal(err)
	}
	h, err = changes.GetHistory(context.Background(), e.q, e.actor, cs.ID, math.MaxInt32)
	if err != nil || h.CanUndo || h.CanRedo || *h.UndoReason != "CHANGESET_STATE_INVALID" {
		t.Fatal(h, err)
	}
	for _, action := range []string{"undo", "redo"} {
		if _, err := e.dispatch(e.actor, action, map[string]any{"changesetId": cs.ID, "expectedSeq": seq}, ""); code(err) != "CHANGESET_STATE_INVALID" {
			t.Fatal(err)
		}
	}
}
func TestStudioHistoryRoundTripProperty(t *testing.T) {
	// STU-051: grouped rename + local text is an exact undo/redo round trip for arbitrary text.
	e := setup(t)
	cs := e.createCS("property")
	r, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := r.Operations[0].Target
	seq := 1
	rapid.Check(t, func(rt *rapid.T) {
		// PostgreSQL jsonb cannot represent U+0000; exercise arbitrary representable text.
		name := rapid.StringMatching(`[^\x00]{0,80}`).Draw(rt, "text")
		raw, _ := json.Marshal(map[string]any{"key": "t_test", "type": "text", "locale": "en", "value": name})
		before := e.body(doc, cs.ID)
		r := e.group(cs.ID, seq, nodeOp(doc, "localContent.set", string(raw)), nodeOp(doc, "node.setBinding", `{"nodeId":"n_root","prop":"text","binding":"$local.t_test"}`))
		seq = int(r.Changeset.Seq)
		after := e.body(doc, cs.ID)
		seq = int(e.historyCommand(cs.ID, seq, "undo").Changeset.Seq)
		if e.body(doc, cs.ID) != before {
			rt.Fatal("undo differs")
		}
		seq = int(e.historyCommand(cs.ID, seq, "redo").Changeset.Seq)
		if e.body(doc, cs.ID) != after {
			rt.Fatal("redo differs")
		}
	})
}

func TestStudioHistoryConflictsAndAccess(t *testing.T) {
	e := setup(t)
	cs := e.createCS("access")
	ctx := context.Background()
	h, err := changes.GetHistory(ctx, e.q, e.actor, cs.ID, math.MaxInt32)
	if err != nil || len(h.Items) != 0 || *h.UndoReason != "NOTHING_TO_UNDO" {
		t.Fatal(h, err)
	}
	denied := e.actor
	denied.Rights = auth.RightSet{}
	if _, err := changes.GetHistory(ctx, e.q, denied, cs.ID, math.MaxInt32); code(err) != "FORBIDDEN" {
		t.Fatal(err)
	}
	if _, err := changes.GetHistory(ctx, e.q, e.actor, uuid.New(), math.MaxInt32); code(err) != "NOT_FOUND" {
		t.Fatal(err)
	}
	other := e.actor
	other.ID = uuid.New()
	h, err = changes.GetHistory(ctx, e.q, other, cs.ID, math.MaxInt32)
	if err != nil || *h.UndoReason != "CHANGESET_NOT_OWNER" {
		t.Fatal(h, err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE changesets SET has_conflicts=true WHERE id=$1`, cs.ID); err != nil {
		t.Fatal(err)
	}
	h, err = changes.GetHistory(ctx, e.q, e.actor, cs.ID, math.MaxInt32)
	if err != nil || h.CanUndo || *h.UndoReason != "CHANGESET_HAS_CONFLICTS" {
		t.Fatal(h, err)
	}
	for _, action := range []string{"undo", "redo"} {
		if _, err := e.dispatch(e.actor, action, map[string]any{"changesetId": cs.ID, "expectedSeq": 0}, ""); code(err) != "CHANGESET_HAS_CONFLICTS" {
			t.Fatal(err)
		}
	}
	e.pool.Close()
	if _, err := changes.GetHistory(ctx, e.q, e.actor, cs.ID, math.MaxInt32); err == nil {
		t.Fatal("closed storage reported as empty history")
	}
}
func TestStudioHistoryMultipleObjects(t *testing.T) {
	// STU-051: a gesture spanning two documents is atomically undone/redone as one action.
	e := setup(t)
	cs := e.createCS("two pages")
	r, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`), createDoc(`{"id":"n_root","type":"Box"}`))
	a, b := r.Operations[0].Target, r.Operations[1].Target
	beforeA, beforeB := e.body(a, cs.ID), e.body(b, cs.ID)
	e.group(cs.ID, 2, nodeOp(a, "node.rename", `{"nodeId":"n_root","name":"A"}`), nodeOp(b, "node.rename", `{"nodeId":"n_root","name":"B"}`))
	afterA, afterB := e.body(a, cs.ID), e.body(b, cs.ID)
	e.historyCommand(cs.ID, 4, "undo")
	if e.body(a, cs.ID) != beforeA || e.body(b, cs.ID) != beforeB {
		t.Fatal("partial multi-page undo")
	}
	e.historyCommand(cs.ID, 6, "redo")
	if e.body(a, cs.ID) != afterA || e.body(b, cs.ID) != afterB {
		t.Fatal("partial multi-page redo")
	}
}
func TestStudioHistoryRebaseDropsRedoDependency(t *testing.T) {
	// STU-054: dropping an edit on a new base drops its undo and redo, preserving history rows.
	e := setup(t)
	ctx := context.Background()
	doc := e.commitHead(`{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box"}}}`)
	cs := e.createCS("rebase history")
	edit := e.group(cs.ID, 0, nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"Mine"}`))
	e.historyCommand(cs.ID, 1, "undo")
	e.historyCommand(cs.ID, 2, "redo")
	head, err := e.q.GetHeadDocument(ctx, store.GetHeadDocumentParams{ID: doc, ProjectID: e.actor.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	version := uuid.New()
	body := `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box","name":"NewHead"}}}`
	if _, err := e.pool.Exec(ctx, `INSERT INTO object_versions(id,project_id,object_id,number,state,ir_version,body,body_hash,created_by) VALUES($1,$2,$3,2,'committed','1.0',$4,decode(repeat('00',32),'hex'),$5)`, version, e.actor.ProjectID, doc, body, e.actor.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE objects SET head_version_id=$1 WHERE id=$2`, version, doc); err != nil {
		t.Fatal(err)
	}
	if head.VersionID == version {
		t.Fatal("fixture did not advance head")
	}
	raw, err := e.q.GetChangeset(ctx, store.GetChangesetParams{ID: cs.ID, ProjectID: e.actor.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	result, err := changes.Rebase(ctx, e.q, raw, map[uuid.UUID]string{edit.Operations[0].ID: changes.ResolutionTheirs}, e.actor)
	if err != nil || len(result.Conflicts) != 0 || len(result.Removed) != 1 {
		t.Fatal(result, err)
	}
	rows, err := changes.ListOperations(ctx, e.q, e.actor.ProjectID, cs.ID, 0)
	if err != nil || len(rows) != 3 {
		t.Fatal(rows, err)
	}
	for _, row := range rows {
		if row.Status != "dropped" {
			t.Fatal(row)
		}
	}
	if !containsName(e.body(doc, cs.ID), "NewHead") {
		t.Fatal("redo replayed dropped edit")
	}
}
func TestStudioHistoryAtomicFailure(t *testing.T) {
	// STU-054: failing the second inverse rolls back the first inverse and its history record.
	e := setup(t)
	cs := e.createCS("atomic")
	r, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := r.Operations[0].Target
	e.group(cs.ID, 1, nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"A"}`), nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"B"}`))
	before := e.body(doc, cs.ID)
	if _, err := e.pool.Exec(context.Background(), `UPDATE operations SET inverse='{"type":"node.rename","payload":{"nodeId":"n_missing","name":"old"}}' WHERE changeset_id=$1 AND seq=2`, cs.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 3}, ""); code(err) != "OPERATION_INVALID" {
		t.Fatal(err)
	}
	rows, err := changes.ListOperations(context.Background(), e.q, e.actor.ProjectID, cs.ID, 0)
	if err != nil || len(rows) != 3 || e.body(doc, cs.ID) != before {
		t.Fatal("partial undo", rows, err)
	}
}
func TestStudioHistoryCorruptInverseAndStorage(t *testing.T) {
	e := setup(t)
	cs := e.createCS("inverse")
	r, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := r.Operations[0].Target
	e.group(cs.ID, 1, nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"A"}`))
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `UPDATE operations SET inverse='"bad"' WHERE changeset_id=$1 AND seq=2`, cs.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 2}, ""); err == nil {
		t.Fatal("bad inverse accepted")
	}
	if _, err := changes.GetHistory(ctx, e.q, e.actor, cs.ID, math.MaxInt32); err == nil {
		t.Fatal("invalid inverse represented as available")
	}
	if _, err := e.pool.Exec(ctx, `UPDATE operations SET inverse='{"type":"node.rename","payload":{"nodeId":"n_root","name":null}}' WHERE changeset_id=$1 AND seq=2`, cs.ID); err != nil {
		t.Fatal(err)
	}
	e.historyCommand(cs.ID, 2, "undo")
	if _, err := e.pool.Exec(ctx, `UPDATE operations SET inverse='"bad"' WHERE changeset_id=$1 AND undo_of IS NOT NULL`, cs.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := changes.GetHistory(ctx, e.q, e.actor, cs.ID, math.MaxInt32); err == nil {
		t.Fatal("invalid redo inverse represented as available")
	}
	if _, err := e.pool.Exec(ctx, `ALTER TABLE operations RENAME TO operations_broken`); err != nil {
		t.Fatal(err)
	}
	if _, err := changes.GetHistory(ctx, e.q, e.actor, cs.ID, math.MaxInt32); err == nil {
		t.Fatal("history storage failed silently")
	}
	if _, err := e.dispatch(e.actor, "redo", map[string]any{"changesetId": cs.ID, "expectedSeq": 3}, ""); err == nil {
		t.Fatal("history storage failure ignored")
	}
	if _, err := e.pool.Exec(ctx, `ALTER TABLE environments RENAME TO environments_broken`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 3}, ""); err == nil {
		t.Fatal("environment locking failure ignored")
	}
}
func TestStudioHistoryRecordFailureAndLinks(t *testing.T) {
	e := setup(t)
	cs := e.createCS("storage")
	r, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := r.Operations[0].Target
	e.group(cs.ID, 1, nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"A"}`))
	before := e.body(doc, cs.ID)
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, `CREATE FUNCTION history_test_fail() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'history insertion failed'; END$$; CREATE TRIGGER history_test_fail BEFORE INSERT ON operations FOR EACH STATEMENT EXECUTE FUNCTION history_test_fail()`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 2}, ""); err == nil || !strings.Contains(err.Error(), "history insertion failed") {
		t.Fatal(err)
	}
	if e.body(doc, cs.ID) != before {
		t.Fatal("failed history insertion changed document")
	}
	// PostgreSQL enforces the mutually exclusive history links at persistence time.
	if _, err := e.pool.Exec(ctx, `UPDATE operations SET undo_of=id,redo_of=id WHERE changeset_id=$1 AND seq=2`, cs.ID); err == nil {
		t.Fatal("ambiguous undo/redo links accepted")
	}
}

// Prepare the migration under test rather than assuming it is the latest one.
func (e *env) historyMigrationBase(ctx context.Context, log *slog.Logger) {
	e.t.Helper()
	for {
		var version int
		if err := e.pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil {
			e.t.Fatal(err)
		}
		if version == 15 {
			return
		}
		if version < 15 {
			e.t.Fatal("history migration is not applied", version)
		}
		if err := postgres.Migrate(ctx, e.pool, "down", log); err != nil {
			e.t.Fatal(err)
		}
	}
}
func TestStudioHistoryMigrationGuard(t *testing.T) {
	// STU-054: populated history cannot be downgraded into a format losing action/link semantics.
	e := setup(t)
	cs := e.createCS("migration")
	e.group(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.historyMigrationBase(ctx, log)
	if err := postgres.Migrate(ctx, e.pool, "down", log); err == nil || !strings.Contains(err.Error(), "operation history prevents downgrade") {
		t.Fatal(err)
	}
	var version int
	if err := e.pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_db_version WHERE is_applied`).Scan(&version); err != nil || version != 15 {
		t.Fatal(version, err)
	}
	// The current query code targets the current schema. First verify the old
	// migration's guard, then upgrade before exercising the current read API.
	if err := postgres.Migrate(ctx, e.pool, "up", log); err != nil {
		t.Fatal(err)
	}
	rows, err := changes.ListOperations(ctx, e.q, e.actor.ProjectID, cs.ID, 0)
	if err != nil || len(rows) != 1 || rows[0].BatchID == nil {
		t.Fatal(rows, err)
	}
}
func TestStudioHistoryConcurrentUndo(t *testing.T) {
	// STU-052/054: two commands with the same sequence cancel a gesture exactly once.
	e := setup(t)
	cs := e.createCS("concurrent")
	r, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := r.Operations[0].Target
	e.group(cs.ID, 1, nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"A"}`), nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"B"}`))
	var wait sync.WaitGroup
	codes := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 3}, "")
			if err == nil {
				codes <- ""
			} else {
				codes <- code(err)
			}
		}()
	}
	wait.Wait()
	close(codes)
	success, conflicts := 0, 0
	for c := range codes {
		if c == "" {
			success++
		} else if c == "CHANGESET_SEQ_CONFLICT" {
			conflicts++
		} else {
			t.Fatal(c)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	rows, err := changes.ListOperations(context.Background(), e.q, e.actor.ProjectID, cs.ID, 0)
	if err != nil || len(rows) != 5 {
		t.Fatal(rows, err)
	}
}
func TestStudioHistoryLegacyBarrierMigrationGuard(t *testing.T) {
	// STU-054: preserving redo barriers matters even when legacy actions have no batch/redo links.
	e := setup(t)
	cs := e.createCS("legacy migration")
	r, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := r.Operations[0].Target
	e.must(e.dispatch(e.actor, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1, "operations": []any{nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"A"}`)}}, "legacy"))
	e.historyCommand(cs.ID, 2, "undo")
	ctx := context.Background()
	raw, err := e.q.GetChangeset(ctx, store.GetChangesetParams{ID: cs.ID, ProjectID: e.actor.ProjectID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changes.Rebase(ctx, e.q, raw, nil, e.actor); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	e.historyMigrationBase(ctx, log)
	if err := postgres.Migrate(ctx, e.pool, "down", log); err == nil || !strings.Contains(err.Error(), "operation history prevents downgrade") {
		t.Fatal(err)
	}
	if err := postgres.Migrate(ctx, e.pool, "up", log); err != nil {
		t.Fatal(err)
	}
	h, err := changes.GetHistory(ctx, e.q, e.actor, cs.ID, math.MaxInt32)
	if err != nil || h.CanRedo {
		t.Fatal(h, err)
	}
}
