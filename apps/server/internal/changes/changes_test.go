package changes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

type env struct {
	t     *testing.T
	pool  *pgxpool.Pool
	q     *store.Queries
	bus   *commandbus.Bus
	actor auth.Actor
}

func setup(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.NewDB(t)
	res, err := projects.Bootstrap(ctx, pool, "store", "Магазин", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := auth.Authenticate(ctx, store.New(pool), "Bearer "+res.Token)
	if err != nil {
		t.Fatal(err)
	}
	bus := commandbus.New(pool)
	changes.Register(bus)
	return &env{t, pool, store.New(pool), bus, actor}
}

func (e *env) dispatch(actor auth.Actor, name string, payload any, reason string) (json.RawMessage, error) {
	e.t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		e.t.Fatal(err)
	}
	resp, err := e.bus.Dispatch(context.Background(), actor, commandbus.Request{
		Name: name, IdempotencyKey: uuid.NewString(), Payload: raw, Reason: reason,
	})
	if err != nil {
		return nil, err
	}
	var body struct{ Result json.RawMessage }
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		e.t.Fatal(err)
	}
	return body.Result, nil
}

func (e *env) must(raw json.RawMessage, err error) json.RawMessage {
	e.t.Helper()
	if err != nil {
		e.t.Fatal(err)
	}
	return raw
}

func (e *env) createCS(title string) changes.Changeset {
	var cs changes.Changeset
	if err := json.Unmarshal(e.must(e.dispatch(e.actor, "create-changeset", map[string]any{"title": title}, "")), &cs); err != nil {
		e.t.Fatal(err)
	}
	return cs
}

func (e *env) apply(cs uuid.UUID, seq int, ops ...map[string]any) (changes.ApplyResult, error) {
	var res changes.ApplyResult
	raw, err := e.dispatch(e.actor, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": seq, "operations": ops}, "тест")
	if err != nil {
		return res, err
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		e.t.Fatal(err)
	}
	return res, nil
}

func createDoc(root string) map[string]any {
	return map[string]any{"type": "document.create", "payload": json.RawMessage(`{"kind":"page","root":` + root + `}`)}
}

func nodeOp(doc uuid.UUID, typ, payload string) map[string]any {
	return map[string]any{"type": typ, "target": doc, "payload": json.RawMessage(payload)}
}

func code(err error) string {
	var cerr *commandbus.Error
	if errors.As(err, &cerr) {
		return cerr.Code
	}
	return fmt.Sprintf("не commandbus.Error: %v", err)
}

func (e *env) body(doc, cs uuid.UUID) string {
	e.t.Helper()
	d, err := changes.GetDocument(context.Background(), e.q, e.actor.ProjectID, doc, &cs)
	if err != nil {
		e.t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(d.Body, &v); err != nil {
		e.t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func TestCreateDocumentAndApply(t *testing.T) {
	e := setup(t)
	cs := e.createCS("Страница")
	res, err := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Container"}`))
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Operations[0].Target
	if res.Changeset.Seq != 1 || res.Operations[0].Type != changes.DocumentCreate {
		t.Fatalf("результат: %+v", res)
	}
	res, err = e.apply(cs.ID, 1,
		nodeOp(doc, "node.insert", `{"parentId":"n_root","subtree":{"id":"n_head","type":"Heading"}}`),
		nodeOp(doc, "node.setProps", `{"nodeId":"n_head","set":{"level":2}}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changeset.Seq != 3 || len(res.Operations) != 2 || res.Operations[1].Seq != 3 {
		t.Errorf("seq: %+v", res)
	}
	want := `{"irVersion":"1.0","kind":"page","nodes":{"n_head":{"id":"n_head","props":{"level":2},"type":"Heading"},"n_root":{"children":["n_head"],"id":"n_root","type":"Container"}},"root":"n_root"}`
	if got := e.body(doc, cs.ID); got != want {
		t.Errorf("документ:\n%s\n%s", got, want)
	}
	if _, err := changes.GetDocument(context.Background(), e.q, e.actor.ProjectID, doc, nil); code(err) != "NOT_FOUND" {
		t.Errorf("head до слияния не существует: %v", err)
	}
}

func TestClientOpIDIsEchoedAndStored(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	res, err := e.apply(cs.ID, 0, map[string]any{"type": "document.create", "clientOpId": "c-1",
		"payload": json.RawMessage(`{"kind":"page","root":{"id":"n_root","type":"Box"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Operations[0].ClientOpID; got == nil || *got != "c-1" {
		t.Errorf("clientOpId в ответе: %v", got)
	}
	ops, _ := changes.ListOperations(context.Background(), e.q, e.actor.ProjectID, cs.ID, 0)
	if ops[0].ClientOpID == nil || *ops[0].ClientOpID != "c-1" {
		t.Errorf("clientOpId в журнале: %v", ops[0].ClientOpID)
	}
}

// В одном пакете создаются документы и меняется существующий: сохраняются все рабочие версии.
func TestBatchAcrossDocuments(t *testing.T) {
	e := setup(t)
	existing := e.commitHead(`{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box"}}}`)
	cs := e.createCS("x")
	res, err := e.apply(cs.ID, 0,
		createDoc(`{"id":"n_root","type":"Box"}`),
		createDoc(`{"id":"n_root","type":"Stack"}`),
		nodeOp(existing, "node.rename", `{"nodeId":"n_root","name":"Изменён"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.body(existing, cs.ID), "Изменён") {
		t.Error("изменение существующего документа не сохранено")
	}
	for _, op := range res.Operations[:2] {
		if !strings.Contains(e.body(op.Target, cs.ID), `"root":"n_root"`) {
			t.Errorf("созданный документ %s не сохранён", op.Target)
		}
	}
}

func TestOperationsAreLoggedWithReason(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	res, err := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	if err != nil {
		t.Fatal(err)
	}
	doc := res.Operations[0].Target
	if _, err := e.apply(cs.ID, 1, nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"Корень"}`)); err != nil {
		t.Fatal(err)
	}
	ops, err := changes.ListOperations(context.Background(), e.q, e.actor.ProjectID, cs.ID, 0)
	if err != nil || len(ops) != 2 {
		t.Fatalf("%v %v", ops, err)
	}
	op := ops[1]
	if op.Type != "node.rename" || *op.Reason != "тест" || op.ActorID != e.actor.ID || op.Target != doc ||
		compact(op.Before) != `{"name":null}` || compact(op.After) != `{"name":"Корень"}` {
		t.Errorf("операция: %+v before=%s after=%s", op, op.Before, op.After)
	}
	later, _ := changes.ListOperations(context.Background(), e.q, e.actor.ProjectID, cs.ID, 1)
	if len(later) != 1 || later[0].Seq != 2 {
		t.Errorf("afterSeq: %+v", later)
	}
}

func TestBatchIsAtomic(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	res, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := res.Operations[0].Target
	before := e.body(doc, cs.ID)
	_, err := e.apply(cs.ID, 1,
		nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"ok"}`),
		nodeOp(doc, "node.remove", `{"nodeId":"n_root"}`),
	)
	var cerr *commandbus.Error
	if !errors.As(err, &cerr) || cerr.Code != "OPERATION_INVALID" || cerr.Params["operationIndex"] != 1 || cerr.Params["operationCode"] != "ROOT_IMMUTABLE" {
		t.Fatalf("ошибка: %+v", cerr)
	}
	if e.body(doc, cs.ID) != before {
		t.Error("пакет должен откатываться целиком (CHG-011)")
	}
	got, _ := changes.GetChangeset(context.Background(), e.q, e.actor.ProjectID, cs.ID)
	if got.Seq != 1 {
		t.Errorf("seq после отката: %d", got.Seq)
	}
}

func TestOperationInvalidDiagnostics(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	res, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	_, err := e.apply(cs.ID, 1, nodeOp(res.Operations[0].Target, "node.setDesign", `{"nodeId":"n_root","set":{"gap":"1px; x"}}`))
	var cerr *commandbus.Error
	if !errors.As(err, &cerr) || cerr.Status != 422 {
		t.Fatalf("%v", err)
	}
	diags, _ := json.Marshal(cerr.Params["diagnostics"])
	if !strings.Contains(string(diags), `"/nodes/n_root/design/gap"`) {
		t.Errorf("диагностики: %s", diags)
	}
}

func TestApplyErrors(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	res, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := res.Operations[0].Target
	missing := uuid.New()
	many := make([]map[string]any, changes.MaxOperationsPerCommand+1)
	for i := range many {
		many[i] = nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"x"}`)
	}
	cases := []struct {
		name string
		cs   uuid.UUID
		seq  int
		ops  []map[string]any
		code string
	}{
		{"устаревший seq", cs.ID, 0, []map[string]any{nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"x"}`)}, "CHANGESET_SEQ_CONFLICT"},
		{"нет Change Set", uuid.New(), 0, []map[string]any{nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"x"}`)}, "NOT_FOUND"},
		{"нет операций", cs.ID, 1, nil, "VALIDATION_FAILED"},
		{"слишком много операций", cs.ID, 1, many, "VALIDATION_FAILED"},
		{"неизвестный тип", cs.ID, 1, []map[string]any{nodeOp(doc, "node.explode", `{}`)}, "VALIDATION_FAILED"},
		{"нет target", cs.ID, 1, []map[string]any{{"type": "node.rename", "payload": json.RawMessage(`{}`)}}, "VALIDATION_FAILED"},
		{"target у create", cs.ID, 1, []map[string]any{{"type": "document.create", "target": doc, "payload": json.RawMessage(`{}`)}}, "VALIDATION_FAILED"},
		{"нет payload", cs.ID, 1, []map[string]any{{"type": "node.rename", "target": doc}}, "VALIDATION_FAILED"},
		{"нет документа", cs.ID, 1, []map[string]any{nodeOp(missing, "node.rename", `{"nodeId":"n_root","name":"x"}`)}, "NOT_FOUND"},
		{"create: неизвестное поле", cs.ID, 1, []map[string]any{{"type": "document.create", "payload": json.RawMessage(`{"kind":"page","x":1}`)}}, "OPERATION_INVALID"},
		{"create: неизвестный kind", cs.ID, 1, []map[string]any{{"type": "document.create", "payload": json.RawMessage(`{"kind":"form","root":{"type":"Box"}}`)}}, "OPERATION_INVALID"},
		{"create: нет kind", cs.ID, 1, []map[string]any{{"type": "document.create", "payload": json.RawMessage(`{"root":{"type":"Box"}}`)}}, "OPERATION_INVALID"},
		{"create: плохой ID", cs.ID, 1, []map[string]any{createDoc(`{"id":"x","type":"Box"}`)}, "OPERATION_INVALID"},
		{"create: невалидный документ", cs.ID, 1, []map[string]any{createDoc(`{"type":"Text","bindings":{"text":"$bad"}}`)}, "OPERATION_INVALID"},
		{"повтор clientOpId", cs.ID, 1, []map[string]any{
			{"type": "node.rename", "target": doc, "clientOpId": "c1", "payload": json.RawMessage(`{"nodeId":"n_root","name":"a"}`)},
			{"type": "node.rename", "target": doc, "clientOpId": "c1", "payload": json.RawMessage(`{"nodeId":"n_root","name":"b"}`)},
		}, "CLIENT_OP_ID_REUSED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := e.apply(tc.cs, tc.seq, tc.ops...); code(err) != tc.code {
				t.Errorf("код %s, ожидалось %s", code(err), tc.code)
			}
		})
	}
}

func TestDocumentOfOtherOpenChangesetIsInvisible(t *testing.T) {
	e := setup(t)
	a := e.createCS("A")
	res, _ := e.apply(a.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	b := e.createCS("B")
	_, err := e.apply(b.ID, 0, nodeOp(res.Operations[0].Target, "node.rename", `{"nodeId":"n_root","name":"x"}`))
	if code(err) != "NOT_FOUND" {
		t.Errorf("документ существует только в Change Set A: %v", err)
	}
}

func TestOwnershipAndRights(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	res, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := res.Operations[0].Target

	other, err := e.q.CreateActor(context.Background(), store.CreateActorParams{ID: uuid.New(), Kind: "service", ProjectID: &e.actor.ProjectID, DisplayName: "other"})
	if err != nil {
		t.Fatal(err)
	}
	otherActor := e.actor
	otherActor.ID = other.ID
	if _, err := e.dispatch(otherActor, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1,
		"operations": []map[string]any{nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"x"}`)}}, ""); code(err) != "CHANGESET_NOT_OWNER" {
		t.Errorf("чужой Change Set: %v", err)
	}

	designer := e.actor
	designer.Rights = auth.RightSet{auth.DesignCompose: true}
	if _, err := e.dispatch(designer, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1,
		"operations": []map[string]any{nodeOp(doc, "node.setBehavior", `{"nodeId":"n_root","event":"click","handlers":null}`)}}, ""); code(err) != "FORBIDDEN" {
		t.Errorf("node.setBehavior требует behavior.use: %v", err)
	}
	reader := e.actor
	reader.Rights = auth.RightSet{auth.ContentRead: true}
	if _, err := e.dispatch(reader, "create-changeset", map[string]any{"title": "x"}, ""); code(err) != "FORBIDDEN" {
		t.Errorf("создание без права записи: %v", err)
	}
	if _, err := e.dispatch(reader, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 1}, ""); code(err) != "FORBIDDEN" {
		t.Errorf("undo без права записи: %v", err)
	}
	if _, err := e.dispatch(reader, "abandon-changeset", map[string]any{"changesetId": cs.ID}, ""); code(err) != "FORBIDDEN" {
		t.Errorf("abandon без права записи: %v", err)
	}
	if _, err := e.dispatch(otherActor, "abandon-changeset", map[string]any{"changesetId": cs.ID}, ""); code(err) != "CHANGESET_NOT_OWNER" {
		t.Errorf("abandon чужого Change Set: %v", err)
	}
	// Неизвестная операция в начале пакета не отменяет проверку прав остальных.
	if _, err := e.dispatch(designer, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1,
		"operations": []map[string]any{nodeOp(doc, "node.unknown", `{}`), nodeOp(doc, "node.setBehavior", `{"nodeId":"n_root","event":"click","handlers":null}`)}}, ""); code(err) != "FORBIDDEN" {
		t.Errorf("права проверяются для всех операций пакета: %v", err)
	}
	if _, err := e.dispatch(e.actor, "apply-operations", map[string]any{"changesetId": cs.ID, "expectedSeq": 1,
		"operations": []map[string]any{nodeOp(doc, "node.unknown", `{}`)}}, ""); code(err) != "VALIDATION_FAILED" {
		t.Errorf("неизвестный тип при авторизации пропускается, отклоняет Validate: %v", err)
	}
}

func TestMaxOperationsBoundary(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	res, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	batch := make([]map[string]any, changes.MaxOperationsPerCommand)
	for i := range batch {
		batch[i] = nodeOp(res.Operations[0].Target, "node.rename", fmt.Sprintf(`{"nodeId":"n_root","name":"v%d"}`, i))
	}
	if r, err := e.apply(cs.ID, 1, batch...); err != nil || len(r.Operations) != changes.MaxOperationsPerCommand {
		t.Errorf("%d операций допустимо: %v", changes.MaxOperationsPerCommand, err)
	}
}

func TestCreateChangesetValidation(t *testing.T) {
	e := setup(t)
	for _, title := range []string{"", strings.Repeat("я", 201)} {
		if _, err := e.dispatch(e.actor, "create-changeset", map[string]any{"title": title}, ""); code(err) != "VALIDATION_FAILED" {
			t.Errorf("title %d символов: %v", len([]rune(title)), err)
		}
	}
	if _, err := e.dispatch(e.actor, "create-changeset", map[string]any{"title": strings.Repeat("я", 200)}, ""); err != nil {
		t.Errorf("200 символов допустимо: %v", err)
	}
}

func TestUndo(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	res, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := res.Operations[0].Target
	afterCreate := e.body(doc, cs.ID)
	if _, err := e.apply(cs.ID, 1,
		nodeOp(doc, "node.insert", `{"parentId":"n_root","subtree":{"id":"n_aaaa","type":"Text"}}`),
		nodeOp(doc, "node.rename", `{"nodeId":"n_aaaa","name":"A"}`),
	); err != nil {
		t.Fatal(err)
	}
	undo := func(seq int) (changes.ApplyResult, error) {
		var r changes.ApplyResult
		raw, err := e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": seq}, "")
		if err == nil {
			_ = json.Unmarshal(raw, &r)
		}
		return r, err
	}
	r, err := undo(3)
	if err != nil || r.Changeset.Seq != 4 || r.Operations[0].Type != "node.rename" {
		t.Fatalf("первая отмена: %+v %v", r, err)
	}
	if _, err := undo(4); err != nil {
		t.Fatal(err)
	}
	if got := e.body(doc, cs.ID); got != afterCreate {
		t.Errorf("две отмены не вернули документ:\n%s\n%s", got, afterCreate)
	}
	if _, err := undo(5); code(err) != "UNDO_NOT_SUPPORTED" {
		t.Errorf("создание документа не отменяется: %v", err)
	}
	if _, err := undo(1); code(err) != "CHANGESET_SEQ_CONFLICT" {
		t.Errorf("устаревший seq: %v", err)
	}
	ops, _ := changes.ListOperations(context.Background(), e.q, e.actor.ProjectID, cs.ID, 0)
	if ops[3].UndoOf == nil || *ops[3].UndoOf != ops[2].ID || ops[4].UndoOf == nil || *ops[4].UndoOf != ops[1].ID {
		t.Errorf("undoOf: %+v", ops)
	}
}

func TestUndoNothing(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	if _, err := e.dispatch(e.actor, "undo", map[string]any{"changesetId": cs.ID, "expectedSeq": 0}, ""); code(err) != "NOTHING_TO_UNDO" {
		t.Errorf("%v", err)
	}
}

func TestAbandon(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	var got changes.Changeset
	if err := json.Unmarshal(e.must(e.dispatch(e.actor, "abandon-changeset", map[string]any{"changesetId": cs.ID}, "")), &got); err != nil || got.State != "abandoned" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := e.apply(cs.ID, 0, createDoc(`{"type":"Box"}`)); code(err) != "CHANGESET_STATE_INVALID" {
		t.Errorf("операции в закрытом Change Set: %v", err)
	}
	list, err := changes.ListChangesets(context.Background(), e.q, e.actor.ProjectID, ptr("abandoned"))
	if err != nil || len(list) != 1 {
		t.Errorf("список по состоянию: %v %v", list, err)
	}
	all, _ := changes.ListChangesets(context.Background(), e.q, e.actor.ProjectID, nil)
	open, _ := changes.ListChangesets(context.Background(), e.q, e.actor.ProjectID, ptr("open"))
	if len(all) != 1 || len(open) != 0 {
		t.Errorf("все: %d, открытые: %d", len(all), len(open))
	}
}

// Параллельные apply-operations с одним expectedSeq: ровно один успешен.
func TestConcurrentApplySameSeq(t *testing.T) {
	e := setup(t)
	cs := e.createCS("x")
	res, _ := e.apply(cs.ID, 0, createDoc(`{"id":"n_root","type":"Box"}`))
	doc := res.Operations[0].Target
	var wg sync.WaitGroup
	results := make(chan error, 6)
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := e.apply(cs.ID, 1, nodeOp(doc, "node.rename", fmt.Sprintf(`{"nodeId":"n_root","name":"v%d"}`, i)))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			ok++
		case code(err) == "CHANGESET_SEQ_CONFLICT":
			conflicts++
		default:
			t.Error(err)
		}
	}
	if ok != 1 || conflicts != 5 {
		t.Errorf("успешных %d, конфликтов %d", ok, conflicts)
	}
}

func TestQueriesNotFound(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	missing := uuid.New()
	if _, err := changes.GetChangeset(ctx, e.q, e.actor.ProjectID, missing); code(err) != "NOT_FOUND" {
		t.Error(err)
	}
	if _, err := changes.ListOperations(ctx, e.q, e.actor.ProjectID, missing, 0); code(err) != "NOT_FOUND" {
		t.Error(err)
	}
	if _, err := changes.GetDocument(ctx, e.q, e.actor.ProjectID, missing, &missing); code(err) != "NOT_FOUND" {
		t.Error(err)
	}
	cs := e.createCS("x")
	if _, err := changes.GetDocument(ctx, e.q, e.actor.ProjectID, missing, &cs.ID); code(err) != "NOT_FOUND" {
		t.Error(err)
	}
}

func TestQueriesDatabaseErrors(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	cs := e.createCS("x")
	e.pool.Close()
	id := uuid.New()
	for name, call := range map[string]func() error{
		"GetChangeset":   func() error { _, err := changes.GetChangeset(ctx, e.q, e.actor.ProjectID, cs.ID); return err },
		"ListChangesets": func() error { _, err := changes.ListChangesets(ctx, e.q, e.actor.ProjectID, nil); return err },
		"GetDocument":    func() error { _, err := changes.GetDocument(ctx, e.q, e.actor.ProjectID, id, nil); return err },
	} {
		if err := call(); err == nil || code(err) == "NOT_FOUND" {
			t.Errorf("%s: ожидалась ошибка БД, получено %v", name, err)
		}
	}
}

func ptr[T any](v T) *T { return &v }

func compact(raw json.RawMessage) string {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		panic(err)
	}
	return buf.String()
}

// commitHead имитирует слияние: фиксирует версию документа и делает её head.
// Команды слияния появятся вместе с публикацией; до тех пор head создаётся напрямую.
func (e *env) commitHead(body string) uuid.UUID {
	e.t.Helper()
	ctx := context.Background()
	obj, ver := uuid.New(), uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{"INSERT INTO objects (id, project_id, kind, doc_kind) VALUES ($1, $2, 'document', 'page')", []any{obj, e.actor.ProjectID}},
		{`INSERT INTO object_versions (id, project_id, object_id, number, state, ir_version, body, body_hash, created_by, committed_at)
		  VALUES ($1, $2, $3, 1, 'committed', '1.0', $4, '\x00', $5, now())`, []any{ver, e.actor.ProjectID, obj, body, e.actor.ID}},
		{"UPDATE objects SET head_version_id = $1 WHERE id = $2", []any{ver, obj}},
	} {
		if _, err := e.pool.Exec(ctx, q.sql, q.args...); err != nil {
			e.t.Fatal(err)
		}
	}
	return obj
}

func TestChangesOnExistingDocumentDoNotTouchHead(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	head := `{"irVersion":"1.0","kind":"page","root":"n_root","nodes":{"n_root":{"id":"n_root","type":"Box"}}}`
	doc := e.commitHead(head)

	cs := e.createCS("Правка")
	if _, err := e.apply(cs.ID, 0, nodeOp(doc, "node.rename", `{"nodeId":"n_root","name":"Новое"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.apply(cs.ID, 1, nodeOp(doc, "node.setProps", `{"nodeId":"n_root","set":{"as":"section"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := e.body(doc, cs.ID); !strings.Contains(got, `"name":"Новое"`) || !strings.Contains(got, `"as":"section"`) {
		t.Errorf("рабочая версия: %s", got)
	}
	h, err := changes.GetDocument(ctx, e.q, e.actor.ProjectID, doc, nil)
	if err != nil || h.State != "committed" || strings.Contains(string(h.Body), "Новое") {
		t.Errorf("head не должен меняться до слияния (FR-030): %+v %v", h, err)
	}
	detail, _ := changes.GetChangeset(ctx, e.q, e.actor.ProjectID, cs.ID)
	if len(detail.Objects) != 1 || detail.Objects[0].BaseVersionID == nil || *detail.Objects[0].BaseVersionID != h.VersionID {
		t.Errorf("база рабочей версии — head: %+v", detail.Objects)
	}
	// Другой Change Set видит head, а не чужую рабочую версию.
	other := e.createCS("Другой")
	if got, err := changes.GetDocument(ctx, e.q, e.actor.ProjectID, doc, &other.ID); err != nil || strings.Contains(string(got.Body), "Новое") || got.State != "committed" {
		t.Errorf("документ в другом Change Set: %+v %v", got, err)
	}
}
