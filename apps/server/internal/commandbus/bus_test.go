package commandbus_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/store"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

type notePayload struct {
	Text string `json:"text"`
}

type noteResult struct {
	Project string `json:"project"`
	Text    string `json:"text"`
}

type env struct {
	pool  *pgxpool.Pool
	bus   *commandbus.Bus
	actor auth.Actor
	calls *atomic.Int32
}

// setup регистрирует тестовую команду "rename-project", меняющую имя проекта: так видно,
// зафиксирована ли транзакция.
func setup(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	pool := pgtest.NewDB(t)
	q := store.New(pool)
	p, err := q.CreateProject(ctx, store.CreateProjectParams{ID: uuid.New(), Slug: "store", Name: "Store"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := q.CreateActor(ctx, store.CreateActorParams{ID: uuid.New(), Kind: "service", ProjectID: &p.ID, DisplayName: "ci"})
	if err != nil {
		t.Fatal(err)
	}
	calls := &atomic.Int32{}
	bus := commandbus.New(pool)
	commandbus.Register(bus, commandbus.Command[notePayload, noteResult]{
		Name:  "rename-project",
		Right: auth.ProjectAdmin,
		Validate: func(p notePayload) error {
			if p.Text == "" {
				return commandbus.Validation(map[string]string{"text": "обязательно"})
			}
			return nil
		},
		Handle: func(ctx context.Context, tx pgx.Tx, actor auth.Actor, p notePayload) (noteResult, error) {
			calls.Add(1)
			if r := commandbus.ReasonFrom(ctx); r != "" {
				p.Text += " (" + r + ")"
			}
			if _, err := tx.Exec(ctx, "UPDATE projects SET name = $1 WHERE id = $2", p.Text, actor.ProjectID); err != nil {
				return noteResult{}, err
			}
			switch p.Text {
			case "fail":
				return noteResult{}, commandbus.NewError(http.StatusConflict, "CONFLICT_TEST", "конфликт", "тест")
			case "panic":
				panic("handler panic")
			}
			return noteResult{Project: actor.ProjectSlug, Text: p.Text}, nil
		},
	})
	actor := auth.Actor{ID: a.ID, Kind: auth.ActorService, ProjectID: p.ID, ProjectSlug: "store",
		Rights: auth.RightSet{auth.ProjectAdmin: true}}
	return env{pool, bus, actor, calls}
}

func (e env) projectName(t *testing.T) string {
	t.Helper()
	var name string
	if err := e.pool.QueryRow(context.Background(), "SELECT name FROM projects WHERE id = $1", e.actor.ProjectID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	return name
}

func req(key, payload string) commandbus.Request {
	return commandbus.Request{Name: "rename-project", IdempotencyKey: key, Payload: json.RawMessage(payload)}
}

func code(t *testing.T, err error) string {
	t.Helper()
	var cerr *commandbus.Error
	if !errors.As(err, &cerr) {
		t.Fatalf("ожидалась *commandbus.Error, получено %v", err)
	}
	return cerr.Code
}

func TestDispatchCommits(t *testing.T) {
	e := setup(t)
	resp, err := e.bus.Dispatch(context.Background(), e.actor, req("k1", `{"text":"Новое имя"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != http.StatusOK || resp.Replayed || string(resp.Body) != `{"result":{"project":"store","text":"Новое имя"}}` {
		t.Errorf("ответ: %+v %s", resp, resp.Body)
	}
	if e.projectName(t) != "Новое имя" {
		t.Error("изменение не зафиксировано")
	}
}

func TestDispatchRejectsBeforeTransaction(t *testing.T) {
	e := setup(t)
	noRights := e.actor
	noRights.Rights = auth.RightSet{}
	cases := []struct {
		name  string
		actor auth.Actor
		req   commandbus.Request
		code  string
	}{
		{"неизвестная команда", e.actor, commandbus.Request{Name: "nope", IdempotencyKey: "k", Payload: json.RawMessage(`{}`)}, "NOT_FOUND"},
		{"нет ключа идемпотентности", e.actor, req("", `{"text":"x"}`), "IDEMPOTENCY_KEY_REQUIRED"},
		{"слишком длинный ключ", e.actor, req(strings.Repeat("k", 201), `{"text":"x"}`), "IDEMPOTENCY_KEY_REQUIRED"},
		{"неизвестное поле", e.actor, req("k", `{"text":"x","extra":1}`), "PAYLOAD_INVALID"},
		{"не объект", e.actor, req("k", `[1]`), "PAYLOAD_INVALID"},
		{"лишние данные", e.actor, req("k", `{"text":"x"} {}`), "PAYLOAD_INVALID"},
		{"нет права", noRights, req("k", `{"text":"x"}`), "FORBIDDEN"},
		{"не прошла проверка", e.actor, req("k", `{"text":""}`), "VALIDATION_FAILED"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := e.bus.Dispatch(context.Background(), tc.actor, tc.req)
			if got := code(t, err); got != tc.code {
				t.Errorf("код %s, ожидалось %s", got, tc.code)
			}
		})
	}
	if e.calls.Load() != 0 || e.projectName(t) != "Store" {
		t.Error("обработчик не должен вызываться")
	}
}

func TestIdempotencyKeyLengthBoundary(t *testing.T) {
	e := setup(t)
	if _, err := e.bus.Dispatch(context.Background(), e.actor, req(strings.Repeat("k", 200), `{"text":"x"}`)); err != nil {
		t.Errorf("ключ из 200 символов допустим: %v", err)
	}
}

func TestReasonReachesHandler(t *testing.T) {
	e := setup(t)
	r := req("k", `{"text":"x"}`)
	r.Reason = "причина"
	resp, err := e.bus.Dispatch(context.Background(), e.actor, r)
	if err != nil || !strings.Contains(string(resp.Body), "x (причина)") {
		t.Errorf("%v %s", err, resp.Body)
	}
	if commandbus.ReasonFrom(context.Background()) != "" {
		t.Error("без команды причина пуста")
	}
}

func TestForbiddenNamesTheRight(t *testing.T) {
	e := setup(t)
	e.actor.Rights = auth.RightSet{}
	_, err := e.bus.Dispatch(context.Background(), e.actor, req("k", `{"text":"x"}`))
	var cerr *commandbus.Error
	if !errors.As(err, &cerr) || cerr.Status != http.StatusForbidden || cerr.Params["right"] != "project.admin" {
		t.Errorf("ошибка: %+v", cerr)
	}
}

func TestHandlerErrorRollsBack(t *testing.T) {
	e := setup(t)
	_, err := e.bus.Dispatch(context.Background(), e.actor, req("k1", `{"text":"fail"}`))
	if code(t, err) != "CONFLICT_TEST" {
		t.Fatal(err)
	}
	if e.projectName(t) != "Store" {
		t.Error("изменение должно быть отменено")
	}
	// Неуспешный вызов не сохраняет ключ: повтор с тем же ключом исполняется заново.
	if _, err := e.bus.Dispatch(context.Background(), e.actor, req("k1", `{"text":"ok"}`)); err != nil {
		t.Fatalf("повтор после ошибки: %v", err)
	}
}

func TestHandlerPanicRollsBack(t *testing.T) {
	e := setup(t)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("паника должна дойти до вызывающего")
			}
		}()
		_, _ = e.bus.Dispatch(context.Background(), e.actor, req("k1", `{"text":"panic"}`))
	}()
	if e.projectName(t) != "Store" {
		t.Error("изменение должно быть отменено")
	}
}

func TestIdempotentReplay(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	first, err := e.bus.Dispatch(ctx, e.actor, req("k1", `{"text":"A"}`))
	if err != nil {
		t.Fatal(err)
	}
	// Тот же запрос с другими пробелами — повтор, а не новый вызов.
	again, err := e.bus.Dispatch(ctx, e.actor, req("k1", "{ \"text\" : \"A\" }\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Replayed || string(again.Body) != string(first.Body) || again.Status != first.Status || e.calls.Load() != 1 {
		t.Errorf("повтор: %+v, вызовов %d", again, e.calls.Load())
	}

	if _, err := e.bus.Dispatch(ctx, e.actor, req("k1", `{"text":"B"}`)); code(t, err) != "IDEMPOTENCY_KEY_REUSED" {
		t.Error("другой payload с тем же ключом должен отклоняться")
	}
	if e.projectName(t) != "A" {
		t.Error("отклонённый повтор не должен ничего менять")
	}
}

func TestIdempotencyKeyIsPerActor(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	other, err := store.New(e.pool).CreateActor(ctx, store.CreateActorParams{ID: uuid.New(), Kind: "service", ProjectID: &e.actor.ProjectID, DisplayName: "other"})
	if err != nil {
		t.Fatal(err)
	}
	otherActor := e.actor
	otherActor.ID = other.ID
	for _, a := range []auth.Actor{e.actor, otherActor} {
		if resp, err := e.bus.Dispatch(ctx, a, req("same", `{"text":"x"}`)); err != nil || resp.Replayed {
			t.Fatalf("%v %+v", err, resp)
		}
	}
	if e.calls.Load() != 2 {
		t.Errorf("вызовов %d, ожидалось 2", e.calls.Load())
	}
}

func TestExpiredIdempotencyKeyExecutesAgain(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := e.bus.Dispatch(ctx, e.actor, req("k1", `{"text":"A"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, "UPDATE idempotency_keys SET created_at = now() - interval '25 hours'"); err != nil {
		t.Fatal(err)
	}
	resp, err := e.bus.Dispatch(ctx, e.actor, req("k1", `{"text":"B"}`))
	if err != nil || resp.Replayed || e.projectName(t) != "B" {
		t.Errorf("просроченный ключ: %v %+v", err, resp)
	}
}

// Параллельные запросы с одним ключом исполняют команду ровно один раз.
func TestConcurrentSameKeyExecutesOnce(t *testing.T) {
	e := setup(t)
	const n = 8
	var wg sync.WaitGroup
	replayed := atomic.Int32{}
	errs := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := e.bus.Dispatch(context.Background(), e.actor, req("same", `{"text":"once"}`))
			if err != nil {
				errs <- err
				return
			}
			if resp.Replayed {
				replayed.Add(1)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if e.calls.Load() != 1 || replayed.Load() != n-1 {
		t.Errorf("вызовов %d, повторов %d", e.calls.Load(), replayed.Load())
	}
}

func TestAuthorizeOverridesRight(t *testing.T) {
	e := setup(t)
	bus := commandbus.New(e.pool)
	commandbus.Register(bus, commandbus.Command[notePayload, string]{
		Name: "authz",
		Authorize: func(actor auth.Actor, p notePayload) error {
			if p.Text == "allowed" {
				return nil
			}
			return commandbus.Require(actor, auth.SchemaApply)
		},
		Handle: func(context.Context, pgx.Tx, auth.Actor, notePayload) (string, error) { return "ok", nil },
	})
	ctx := context.Background()
	if _, err := bus.Dispatch(ctx, e.actor, commandbus.Request{Name: "authz", IdempotencyKey: "a", Payload: json.RawMessage(`{"text":"allowed"}`)}); err != nil {
		t.Error(err)
	}
	if _, err := bus.Dispatch(ctx, e.actor, commandbus.Request{Name: "authz", IdempotencyKey: "b", Payload: json.RawMessage(`{"text":"other"}`)}); code(t, err) != "FORBIDDEN" {
		t.Error("ожидался FORBIDDEN")
	}
}

func TestRegisterPanicsOnMisuse(t *testing.T) {
	bus := commandbus.New(nil)
	ok := commandbus.Command[notePayload, string]{
		Name: "x", Right: auth.ContentRead,
		Handle: func(context.Context, pgx.Tx, auth.Actor, notePayload) (string, error) { return "", nil },
	}
	commandbus.Register(bus, ok)
	for name, c := range map[string]commandbus.Command[notePayload, string]{
		"повтор имени": ok,
		"нет Handle":   {Name: "y", Right: auth.ContentRead},
		"нет прав":     {Name: "z", Handle: ok.Handle},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("ожидалась паника")
				}
			}()
			commandbus.Register(bus, c)
		})
	}
	if names := bus.Names(); len(names) != 1 || names[0] != "x" {
		t.Errorf("Names = %v", names)
	}
}

func TestErrorHelpers(t *testing.T) {
	err := commandbus.NewError(http.StatusConflict, "X", "t", "d").WithParams(map[string]any{"a": 1})
	if err.Error() != "X: d" || err.Params["a"] != 1 {
		t.Errorf("%v %v", err, err.Params)
	}
	v := commandbus.Validation(map[string]string{"name": "плохо"})
	if v.Status != http.StatusUnprocessableEntity || v.Params["fields"].(map[string]any)["name"] != "плохо" {
		t.Errorf("%+v", v)
	}
}
