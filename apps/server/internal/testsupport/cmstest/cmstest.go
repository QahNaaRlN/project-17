// Package cmstest — общий стенд для интеграционных тестов модулей CMS: проект, акторы,
// шина команд со всеми модулями и вызов команд.
package cmstest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/composition/manifest"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

// ContentFixture represents already imported, committed content; production mutations use Command Bus.
func (e *Env) ContentFixture(kind, schema string, body any, environments ...string) uuid.UUID {
	e.T.Helper()
	id, version := uuid.New(), uuid.New()
	var name *string
	var schemaVersion *int32
	if kind == "entity" {
		name = &schema
		n := int32(1)
		schemaVersion = &n
	}
	raw, err := json.Marshal(body)
	if err != nil {
		e.T.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	e.Exec(`INSERT INTO objects(id,project_id,kind,schema_name) VALUES($1,$2,$3,$4)`, id, e.Admin.ProjectID, kind, name)
	e.Exec(`INSERT INTO object_versions(id,project_id,object_id,number,state,schema_version,body,body_hash,created_by) VALUES($1,$2,$3,1,'committed',$4,$5,$6,$7)`, version, e.Admin.ProjectID, id, schemaVersion, raw, hash[:], e.Admin.ID)
	e.Exec(`UPDATE objects SET head_version_id=$1 WHERE id=$2`, version, id)
	for _, env := range environments {
		pub := uuid.New()
		e.Exec(`INSERT INTO publications(id,project_id,environment_id,kind,actor_id) SELECT $1,$2,id,'publish',$3 FROM environments WHERE project_id=$2 AND name=$4`, pub, e.Admin.ProjectID, e.Admin.ID, env)
		e.Exec(`INSERT INTO published_pointers(environment_id,object_id,version_id,publication_id) SELECT id,$1,$2,$3 FROM environments WHERE project_id=$4 AND name=$5`, id, version, pub, e.Admin.ProjectID, env)
	}
	return id
}

// Env — стенд одного теста.
type Env struct {
	T     *testing.T
	Pool  *pgxpool.Pool
	Q     *store.Queries
	Bus   *commandbus.Bus
	Admin auth.Actor // сервисный администратор из bootstrap
}

const DefaultManifest = `{"manifestVersion":"1.0","app":{"id":"test","version":"1.0.0","framework":"react"},"irVersions":["1.0"],"breakpoints":{},"tokens":{}}`

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
	queue, err := jobs.NewInserter(pool)
	if err != nil {
		t.Fatal(err)
	}
	bus.WithContext(jobs.WithClient(queue))
	projects.Register(bus)
	changes.Register(bus)
	for _, r := range register {
		r(bus)
	}
	e := &Env{T: t, Pool: pool, Q: store.New(pool), Bus: bus, Admin: admin}
	// Command scenarios start with a ready contract. Bootstrap itself still leaves NULL.
	for _, name := range []string{"staging", "production"} {
		e.ActivateManifest(name, []byte(DefaultManifest))
	}
	return e
}

// ActivateManifest prepares immutable manifest storage without a registration command.
func (e *Env) ActivateManifest(environment string, raw []byte) uuid.UUID {
	e.T.Helper()
	ctx := context.Background()
	app, err := manifest.ParseJSON(raw)
	if err != nil {
		e.T.Fatal(err)
	}
	if r := manifest.Validate(app); !r.Valid {
		e.T.Fatalf("invalid test manifest: %+v", r)
	}
	hash, err := manifest.Hash(app)
	if err != nil {
		e.T.Fatal(err)
	}
	if err := e.Q.InsertManifest(ctx, store.InsertManifestParams{ID: uuid.New(), ProjectID: e.Admin.ProjectID, Hash: hash, AppVersion: "1.0.0", Body: raw, RegisteredBy: e.Admin.ID}); err != nil {
		e.T.Fatal(err)
	}
	m, err := e.Q.GetManifestByHash(ctx, store.GetManifestByHashParams{ProjectID: e.Admin.ProjectID, Hash: hash})
	if err != nil {
		e.T.Fatal(err)
	}
	e.Exec("UPDATE environments SET active_manifest_id=$1 WHERE project_id=$2 AND name=$3", m.ID, e.Admin.ProjectID, environment)
	return m.ID
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
