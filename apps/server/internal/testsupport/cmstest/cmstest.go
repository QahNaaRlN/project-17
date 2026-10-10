// Package cmstest — общий стенд для интеграционных тестов модулей CMS: проект, акторы,
// шина команд со всеми модулями и вызов команд.
package cmstest

import (
	"context"
	"encoding/json"
	"errors"
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

// Env — стенд одного теста.
type Env struct {
	T     *testing.T
	Pool  *pgxpool.Pool
	Q     *store.Queries
	Bus   *commandbus.Bus
	Admin auth.Actor // сервисный администратор из bootstrap
}

// New создаёт проект и шину; register регистрирует модули (projects и changes — всегда).
func New(t *testing.T, register ...func(*commandbus.Bus)) *Env {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.NewDB(t)
	res, err := projects.Bootstrap(ctx, pool, "store", "Магазин", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := auth.Authenticate(ctx, store.New(pool), "Bearer "+res.Token)
	if err != nil {
		t.Fatal(err)
	}
	bus := commandbus.New(pool)
	projects.Register(bus)
	changes.Register(bus)
	for _, r := range register {
		r(bus)
	}
	return &Env{T: t, Pool: pool, Q: store.New(pool), Bus: bus, Admin: admin}
}

// Human создаёт человека с ролью, дающей права, и возвращает его как актора.
func (e *Env) Human(name string, rights ...auth.Right) auth.Actor {
	e.T.Helper()
	ctx := context.Background()
	a, err := e.Q.CreateActor(ctx, store.CreateActorParams{ID: uuid.New(), Kind: string(auth.ActorHuman), DisplayName: name})
	if err != nil {
		e.T.Fatal(err)
	}
	caps := make([]string, len(rights))
	set := auth.RightSet{}
	for i, r := range rights {
		caps[i] = string(r)
		set[r] = true
	}
	role, err := e.Q.CreateRole(ctx, store.CreateRoleParams{ID: uuid.New(), ProjectID: e.Admin.ProjectID, Name: "role-" + name, Capabilities: caps})
	if err != nil {
		e.T.Fatal(err)
	}
	if err := e.Q.BindRole(ctx, store.BindRoleParams{ProjectID: e.Admin.ProjectID, ActorID: a.ID, RoleID: role.ID}); err != nil {
		e.T.Fatal(err)
	}
	return auth.Actor{ID: a.ID, Kind: auth.ActorHuman, DisplayName: name, ProjectID: e.Admin.ProjectID, ProjectSlug: e.Admin.ProjectSlug, Rights: set}
}

// Do исполняет команду и разбирает result в out (если out != nil).
func (e *Env) Do(actor auth.Actor, name string, payload any, out any) error {
	e.T.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		e.T.Fatal(err)
	}
	resp, err := e.Bus.Dispatch(context.Background(), actor, commandbus.Request{Name: name, IdempotencyKey: uuid.NewString(), Payload: raw})
	if err != nil {
		return err
	}
	if out != nil {
		var body struct{ Result json.RawMessage }
		if err := json.Unmarshal(resp.Body, &body); err != nil {
			e.T.Fatal(err)
		}
		if err := json.Unmarshal(body.Result, out); err != nil {
			e.T.Fatal(err)
		}
	}
	return nil
}

// Must — Do, падающий при ошибке.
func (e *Env) Must(actor auth.Actor, name string, payload any, out any) {
	e.T.Helper()
	if err := e.Do(actor, name, payload, out); err != nil {
		e.T.Fatalf("%s: %v", name, err)
	}
}

// Code — код ошибки команды ("" — не *commandbus.Error).
func Code(err error) string {
	var cerr *commandbus.Error
	if errors.As(err, &cerr) {
		return cerr.Code
	}
	return ""
}

// Draft создаёт Change Set актора со страницей; возвращает ID Change Set и документа.
func (e *Env) Draft(actor auth.Actor, rootName string) (cs, doc uuid.UUID) {
	e.T.Helper()
	var c changes.Changeset
	e.Must(actor, "create-changeset", map[string]any{"title": "draft " + rootName}, &c)
	var res changes.ApplyResult
	e.Must(actor, "apply-operations", map[string]any{"changesetId": c.ID, "expectedSeq": 0, "operations": []any{
		map[string]any{"type": "document.create", "payload": map[string]any{"kind": "page", "root": map[string]any{"id": "n_root", "type": "Box", "name": rootName}}},
	}}, &res)
	return c.ID, res.Operations[0].Target
}

// Edit создаёт Change Set, переименовывающий корень документа.
func (e *Env) Edit(actor auth.Actor, doc uuid.UUID, name string) uuid.UUID {
	e.T.Helper()
	var c changes.Changeset
	e.Must(actor, "create-changeset", map[string]any{"title": "edit " + name}, &c)
	e.Must(actor, "apply-operations", map[string]any{"changesetId": c.ID, "expectedSeq": 0, "operations": []any{
		map[string]any{"type": "node.rename", "target": doc, "payload": map[string]any{"nodeId": "n_root", "name": name}},
	}}, nil)
	return c.ID
}

// Break ломает доступ к таблице, чтобы проверить обработку ошибок БД:
// op "INSERT"/"UPDATE"/"DELETE" — триггер отклоняет такую запись; "" — таблица переименовывается
// и падают любые запросы к ней.
func (e *Env) Break(table, op string) {
	e.T.Helper()
	sql := "ALTER TABLE " + table + " RENAME TO " + table + "_broken"
	if op != "" {
		sql = `CREATE OR REPLACE FUNCTION cms_test_fail() RETURNS trigger LANGUAGE plpgsql AS
		         $$BEGIN RAISE EXCEPTION 'сбой, внесённый тестом'; END$$;
		       CREATE TRIGGER cms_test_fail BEFORE ` + op + ` ON ` + table + ` FOR EACH STATEMENT EXECUTE FUNCTION cms_test_fail()`
	}
	if _, err := e.Pool.Exec(context.Background(), sql); err != nil {
		e.T.Fatal(err)
	}
}

// Exec выполняет SQL напрямую (подготовка состояния, которого нельзя достичь командами).
func (e *Env) Exec(sql string, args ...any) {
	e.T.Helper()
	if _, err := e.Pool.Exec(context.Background(), sql, args...); err != nil {
		e.T.Fatal(err)
	}
}

// ExpectDBError проверяет, что ошибка есть и это не доменная ошибка команды.
func ExpectDBError(t *testing.T, err error) {
	t.Helper()
	if err == nil || Code(err) != "" {
		t.Errorf("ожидалась ошибка БД, получено %v", err)
	}
}

// Apply применяет операции к документу doc в Change Set cs при ожидаемом seq.
func (e *Env) Apply(actor auth.Actor, cs uuid.UUID, seq int, doc uuid.UUID, operations ...map[string]any) error {
	e.T.Helper()
	list := make([]any, len(operations))
	for i, op := range operations {
		op["target"] = doc
		list[i] = op
	}
	return e.Do(actor, "apply-operations", map[string]any{"changesetId": cs, "expectedSeq": seq, "operations": list}, nil)
}

// Op — операция над документом для Apply.
func Op(opType string, payload map[string]any) map[string]any {
	return map[string]any{"type": opType, "payload": payload}
}
