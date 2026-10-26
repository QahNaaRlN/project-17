package projects_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

type env struct {
	pool  *pgxpool.Pool
	bus   *commandbus.Bus
	admin auth.Actor
}

func setup(t *testing.T) env {
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
	return env{pool, bus, admin}
}

func (e env) create(actor auth.Actor, payload string) (projects.Environment, error) {
	resp, err := e.bus.Dispatch(context.Background(), actor, commandbus.Request{
		Name: "create-environment", IdempotencyKey: uuid.NewString(), Payload: json.RawMessage(payload),
	})
	if err != nil {
		return projects.Environment{}, err
	}
	var body struct{ Result projects.Environment }
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		panic(err)
	}
	return body.Result, nil
}

func errCode(err error) string {
	var cerr *commandbus.Error
	if errors.As(err, &cerr) {
		return cerr.Code
	}
	return ""
}

func TestBootstrap(t *testing.T) {
	e := setup(t)
	if len(e.admin.Rights) != len(auth.AllRights)-2 || e.admin.Rights.Has(auth.CapabilityDeliver) || e.admin.Rights.Has(auth.ChecksRun) || e.admin.ProjectSlug != "store" || e.admin.DisplayName != "bootstrap-admin" {
		t.Errorf("администратор: %+v", e.admin)
	}
	envs, err := projects.ListEnvironments(context.Background(), store.New(e.pool), e.admin.ProjectID)
	if err != nil || len(envs) != 2 || envs[0].Name != "production" || envs[1].Name != "staging" {
		t.Errorf("окружения: %+v %v", envs, err)
	}
	if _, err := projects.Bootstrap(context.Background(), e.pool, "store", "Повтор", time.Hour); err == nil || !strings.Contains(err.Error(), "уже существует") {
		t.Errorf("повторный bootstrap: %v", err)
	}
	if _, err := projects.Bootstrap(context.Background(), e.pool, "Bad Slug", "x", time.Hour); err == nil {
		t.Error("недопустимый slug должен отклоняться ограничением БД")
	}
}

func TestCreateEnvironment(t *testing.T) {
	e := setup(t)
	env, err := e.create(e.admin, `{"name":"qa","kind":"standard","appUrl":"https://qa.example.com"}`)
	if err != nil {
		t.Fatal(err)
	}
	if env.Name != "qa" || env.Kind != "standard" || env.AppURL == nil || *env.AppURL != "https://qa.example.com" {
		t.Errorf("окружение: %+v", env)
	}
	if _, err := e.create(e.admin, `{"name":"qa","kind":"standard"}`); errCode(err) != "ENVIRONMENT_EXISTS" {
		t.Errorf("дубликат: %v", err)
	}
}

func TestCreateEnvironmentValidation(t *testing.T) {
	e := setup(t)
	cases := map[string]string{
		"имя с пробелом":    `{"name":"bad name","kind":"standard"}`,
		"имя с заглавной":   `{"name":"QA","kind":"standard"}`,
		"неизвестный вид":   `{"name":"qa","kind":"other"}`,
		"относительный URL": `{"name":"qa","kind":"standard","appUrl":"/app"}`,
		"схема не http":     `{"name":"qa","kind":"standard","appUrl":"ftp://x"}`,
		"некорректный URL":  `{"name":"qa","kind":"standard","appUrl":"http://[::1"}`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := e.create(e.admin, payload); errCode(err) != "VALIDATION_FAILED" {
				t.Errorf("ожидалось VALIDATION_FAILED, получено %v", err)
			}
		})
	}
}

func TestCreateEnvironmentAuthorization(t *testing.T) {
	e := setup(t)
	ci := e.admin
	ci.Rights = auth.RightSet{auth.ManifestRegister: true}
	if _, err := e.create(ci, `{"name":"preview/pr-1","kind":"preview"}`); err != nil {
		t.Errorf("CI создаёт preview-окружение: %v", err)
	}
	if _, err := e.create(ci, `{"name":"qa","kind":"standard"}`); errCode(err) != "FORBIDDEN" {
		t.Errorf("CI не создаёт стандартное окружение: %v", err)
	}
	nobody := e.admin
	nobody.Rights = auth.RightSet{}
	if _, err := e.create(nobody, `{"name":"preview/pr-2","kind":"preview"}`); errCode(err) != "FORBIDDEN" {
		t.Errorf("без прав: %v", err)
	}
}

func TestListEnvironmentsIsolatedByProject(t *testing.T) {
	e := setup(t)
	other, err := projects.Bootstrap(context.Background(), e.pool, "other", "Другой", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.create(e.admin, `{"name":"qa","kind":"standard"}`); err != nil {
		t.Fatal(err)
	}
	envs, err := projects.ListEnvironments(context.Background(), store.New(e.pool), other.ProjectID)
	if err != nil || len(envs) != 2 {
		t.Errorf("окружения другого проекта: %+v %v", envs, err)
	}
}

func TestListEnvironmentsError(t *testing.T) {
	e := setup(t)
	e.pool.Close()
	if _, err := projects.ListEnvironments(context.Background(), store.New(e.pool), e.admin.ProjectID); err == nil {
		t.Error("ожидалась ошибка")
	}
}

// STU-481/488: storage failures of the runner token and the application address.
func TestCheckRunnerTokenAndAppURLFailures(t *testing.T) {
	for _, table := range []string{"roles", "actors", "role_bindings", "api_tokens"} {
		t.Run(table, func(t *testing.T) {
			e := setup(t)
			fail(t, e, table, "INSERT")
			if _, err := projects.CheckRunnerToken(context.Background(), e.pool, "store", time.Hour); err == nil {
				t.Fatal("ожидалась ошибка БД")
			}
		})
	}
	t.Run("lookup", func(t *testing.T) {
		e := setup(t)
		if _, err := e.pool.Exec(context.Background(), "ALTER TABLE role_bindings RENAME TO role_bindings_broken"); err != nil {
			t.Fatal(err)
		}
		if _, err := projects.CheckRunnerToken(context.Background(), e.pool, "store", time.Hour); err == nil {
			t.Fatal("ожидалась ошибка БД")
		}
	})
	t.Run("app url", func(t *testing.T) {
		e := setup(t)
		fail(t, e, "environments", "UPDATE")
		_, err := e.bus.Dispatch(context.Background(), e.admin, commandbus.Request{Name: "set-environment-app-url", IdempotencyKey: "app-url",
			Payload: []byte(`{"environment":"staging","appUrl":"https://shop.example"}`)})
		var ce *commandbus.Error
		if err == nil || errors.As(err, &ce) {
			t.Fatal(err)
		}
	})
}

func fail(t *testing.T, e env, table, op string) {
	t.Helper()
	if _, err := e.pool.Exec(context.Background(), `CREATE OR REPLACE FUNCTION cms_test_fail() RETURNS trigger LANGUAGE plpgsql AS
		$$BEGIN RAISE EXCEPTION 'сбой, внесённый тестом'; END$$;
		CREATE TRIGGER cms_test_fail BEFORE `+op+` ON `+table+` FOR EACH STATEMENT EXECUTE FUNCTION cms_test_fail()`); err != nil {
		t.Fatal(err)
	}
}
