package httpapi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qahnaarln/project-17/apps/server/internal/auth"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

type env struct {
	srv   *httptest.Server
	pool  *pgxpool.Pool
	token string
	logs  *bytes.Buffer
}

type panicPayload struct{}

func setup(t *testing.T) env {
	t.Helper()
	pool := pgtest.NewDB(t)
	res, err := projects.Bootstrap(context.Background(), pool, "store", "Магазин", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	bus := commandbus.New(pool)
	projects.Register(bus)
	changes.Register(bus)
	workflow.Register(bus)
	publishing.Register(bus)
	delivery.Register(bus)
	commandbus.Register(bus, commandbus.Command[panicPayload, string]{
		Name: "test-panic", Right: auth.ContentRead,
		Handle: func(context.Context, pgx.Tx, auth.Actor, panicPayload) (string, error) { panic("boom") },
	})
	logs := &bytes.Buffer{}
	srv := httptest.NewServer(httpapi.NewRouter(httpapi.Deps{
		Pool: pool, Bus: bus, Log: slog.New(slog.NewJSONHandler(logs, nil)), Version: "test",
	}))
	t.Cleanup(srv.Close)
	return env{srv, pool, res.Token, logs}
}

func (e env) do(t *testing.T, method, path, body string, headers map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("не JSON: %s", raw)
		}
	}
	return resp, out
}

func (e env) authed(extra map[string]string) map[string]string {
	h := map[string]string{"Authorization": "Bearer " + e.token, "X-CMS-Project": "store", "Content-Type": "application/json"}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func expectProblem(t *testing.T, resp *http.Response, body map[string]any, status int, code string) {
	t.Helper()
	if resp.StatusCode != status || body["code"] != code || resp.Header.Get("Content-Type") != "application/problem+json" {
		t.Fatalf("ожидалось %d %s, получено %d %v (%s)", status, code, resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
	if body["requestId"] == "" || body["instance"] == "" || !strings.HasPrefix(body["type"].(string), "https://cms.dev/errors/") {
		t.Errorf("поля problem: %v", body)
	}
}

func TestHealth(t *testing.T) {
	e := setup(t)
	resp, body := e.do(t, "GET", "/healthz", "", nil)
	if resp.StatusCode != 200 || body["version"] != "test" {
		t.Errorf("healthz: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(t, "GET", "/readyz", "", nil)
	if resp.StatusCode != 200 || body["status"] != "ready" {
		t.Errorf("readyz: %d %v", resp.StatusCode, body)
	}
	e.pool.Close()
	resp, body = e.do(t, "GET", "/readyz", "", nil)
	if resp.StatusCode != http.StatusServiceUnavailable || body["database"] != "down" {
		t.Errorf("readyz без БД: %d %v", resp.StatusCode, body)
	}
}

func TestCommandAndQuery(t *testing.T) {
	e := setup(t)
	payload := `{"payload":{"name":"qa","kind":"standard"},"reason":"тест"}`
	resp, body := e.do(t, "POST", "/api/v1/commands/create-environment", payload, e.authed(map[string]string{"Idempotency-Key": "k1"}))
	if resp.StatusCode != 200 || body["result"].(map[string]any)["name"] != "qa" || resp.Header.Get("Idempotent-Replayed") != "" {
		t.Fatalf("команда: %d %v", resp.StatusCode, body)
	}
	resp, _ = e.do(t, "POST", "/api/v1/commands/create-environment", payload, e.authed(map[string]string{"Idempotency-Key": "k1"}))
	if resp.Header.Get("Idempotent-Replayed") != "true" {
		t.Error("повтор должен помечаться заголовком Idempotent-Replayed")
	}
	resp, body = e.do(t, "GET", "/api/v1/environments", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 3 {
		t.Errorf("окружения: %d %v", resp.StatusCode, body)
	}
}

func TestCommandErrors(t *testing.T) {
	e := setup(t)
	key := map[string]string{"Idempotency-Key": "k"}
	cases := []struct {
		name, path, body string
		headers          map[string]string
		status           int
		code             string
	}{
		{"нет токена", "/api/v1/environments", "", map[string]string{"X-CMS-Project": "store"}, 401, "UNAUTHENTICATED"},
		{"чужой проект", "/api/v1/environments", "", map[string]string{"Authorization": "Bearer " + e.token, "X-CMS-Project": "other"}, 403, "PROJECT_MISMATCH"},
		{"не JSON", "/api/v1/commands/create-environment", "nope", e.authed(key), 400, "PAYLOAD_INVALID"},
		{"нет payload", "/api/v1/commands/create-environment", `{"reason":"x"}`, e.authed(key), 400, "PAYLOAD_INVALID"},
		{"неизвестная команда", "/api/v1/commands/nope", `{"payload":{}}`, e.authed(key), 404, "NOT_FOUND"},
		{"нет ключа", "/api/v1/commands/create-environment", `{"payload":{"name":"qa","kind":"standard"}}`, e.authed(nil), 400, "IDEMPOTENCY_KEY_REQUIRED"},
		{"проверка полей", "/api/v1/commands/create-environment", `{"payload":{"name":"","kind":"x"}}`, e.authed(key), 422, "VALIDATION_FAILED"},
		{"слишком большое тело", "/api/v1/commands/create-environment", `{"payload":"` + strings.Repeat("x", httpapi.MaxCommandBody) + `"}`, e.authed(key), 413, "LIMIT_EXCEEDED"},
		{"нет маршрута", "/nope", "", nil, 404, "NOT_FOUND"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method := "POST"
			if tc.body == "" {
				method = "GET"
			}
			resp, body := e.do(t, method, tc.path, tc.body, tc.headers)
			expectProblem(t, resp, body, tc.status, tc.code)
			if tc.status == 401 && resp.Header.Get("WWW-Authenticate") == "" {
				t.Error("нет WWW-Authenticate")
			}
		})
	}
	resp, body := e.do(t, "DELETE", "/healthz", "", nil)
	expectProblem(t, resp, body, 405, "METHOD_NOT_ALLOWED")

	// Конверт без payload отклоняется HTTP-слоем с подсказкой о формате, а не шиной команд.
	resp, body = e.do(t, "POST", "/api/v1/commands/create-environment", `{"reason":"x"}`, e.authed(key))
	expectProblem(t, resp, body, 400, "PAYLOAD_INVALID")
	if !strings.Contains(body["detail"].(string), `{"payload": {...}`) {
		t.Errorf("подсказка о формате конверта: %v", body["detail"])
	}
}

func TestInternalErrorsAreHidden(t *testing.T) {
	e := setup(t)
	resp, body := e.do(t, "POST", "/api/v1/commands/test-panic", `{"payload":{}}`, e.authed(map[string]string{"Idempotency-Key": "p"}))
	expectProblem(t, resp, body, 500, "INTERNAL")
	if strings.Contains(body["detail"].(string), "boom") {
		t.Error("подробности паники не должны уходить клиенту")
	}
	if !strings.Contains(e.logs.String(), "boom") {
		t.Error("паника должна попасть в журнал")
	}

	// Ошибка БД при запросе — тоже 500 без подробностей.
	e.pool.Close()
	resp, body = e.do(t, "GET", "/api/v1/environments", "", e.authed(nil))
	expectProblem(t, resp, body, 500, "INTERNAL")
}

func TestRequestsAreLogged(t *testing.T) {
	e := setup(t)
	e.do(t, "GET", "/healthz", "", nil)
	if !strings.Contains(e.logs.String(), `"path":"/healthz"`) || !strings.Contains(e.logs.String(), `"status":200`) {
		t.Errorf("журнал: %s", e.logs.String())
	}
}

func (e env) command(t *testing.T, name, payload string) map[string]any {
	t.Helper()
	resp, body := e.do(t, "POST", "/api/v1/commands/"+name, `{"payload":`+payload+`,"reason":"http"}`,
		e.authed(map[string]string{"Idempotency-Key": fmt.Sprintf("%s-%x", name, sha256.Sum256([]byte(payload)))}))
	if resp.StatusCode != 200 {
		t.Fatalf("%s: %d %v", name, resp.StatusCode, body)
	}
	return body["result"].(map[string]any)
}

func TestChangesetAndDocumentRoutes(t *testing.T) {
	e := setup(t)
	cs := e.command(t, "create-changeset", `{"title":"HTTP"}`)["id"].(string)
	res := e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","root":{"id":"n_root","type":"Box"}}}]}`)
	doc := res["operations"].([]any)[0].(map[string]any)["target"].(string)

	resp, body := e.do(t, "GET", "/api/v1/changesets?state=open", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 1 {
		t.Errorf("список: %d %v", resp.StatusCode, body)
	}
	resp, body = e.do(t, "GET", "/api/v1/changesets", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 1 {
		t.Errorf("список без фильтра: %v", body)
	}
	resp, body = e.do(t, "GET", "/api/v1/changesets/"+cs, "", e.authed(nil))
	if resp.StatusCode != 200 || body["seq"].(float64) != 1 || len(body["objects"].([]any)) != 1 {
		t.Errorf("Change Set: %v", body)
	}
	resp, body = e.do(t, "GET", "/api/v1/changesets/"+cs+"/operations?afterSeq=0", "", e.authed(nil))
	items := body["items"].([]any)
	if resp.StatusCode != 200 || len(items) != 1 || items[0].(map[string]any)["reason"] != "http" {
		t.Errorf("операции: %v", body)
	}
	resp, body = e.do(t, "GET", "/api/v1/documents/"+doc+"?changesetId="+cs, "", e.authed(nil))
	if resp.StatusCode != 200 || body["state"] != "working" || body["body"].(map[string]any)["root"] != "n_root" {
		t.Errorf("документ: %v", body)
	}
	resp, body = e.do(t, "GET", "/api/v1/documents/"+doc, "", e.authed(nil))
	expectProblem(t, resp, body, 404, "NOT_FOUND")
}

func TestRouteParamErrors(t *testing.T) {
	e := setup(t)
	for _, path := range []string{
		"/api/v1/changesets/not-a-uuid",
		"/api/v1/changesets/not-a-uuid/operations",
		"/api/v1/changesets/0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11/operations?afterSeq=-1",
		"/api/v1/changesets/0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11/operations?afterSeq=x",
		"/api/v1/documents/not-a-uuid",
		"/api/v1/documents/0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11?changesetId=x",
		"/api/v1/changesets/not-a-uuid/review",
		"/api/v1/publications/not-a-uuid",
	} {
		resp, body := e.do(t, "GET", path, "", e.authed(nil))
		expectProblem(t, resp, body, 400, "PARAM_INVALID")
	}
	resp, body := e.do(t, "GET", "/api/v1/changesets/0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11", "", e.authed(nil))
	expectProblem(t, resp, body, 404, "NOT_FOUND")
}

func TestDocumentsRequireDesignRead(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	if _, err := e.pool.Exec(ctx, "UPDATE roles SET capabilities = ARRAY['content.read']"); err != nil {
		t.Fatal(err)
	}
	resp, body := e.do(t, "GET", "/api/v1/documents/0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11", "", e.authed(nil))
	expectProblem(t, resp, body, 403, "FORBIDDEN")
}

func TestReviewAndPublicationRoutes(t *testing.T) {
	e := setup(t)
	e.command(t, "set-approval-policy", `{"low":0,"medium":0,"high":0}`)
	cs := e.command(t, "create-changeset", `{"title":"HTTP"}`)["id"].(string)
	res := e.command(t, "apply-operations", `{"changesetId":"`+cs+`","expectedSeq":0,"operations":[{"type":"document.create","payload":{"kind":"page","root":{"id":"n_root","type":"Box"}}}]}`)
	doc := res["operations"].([]any)[0].(map[string]any)["target"].(string)
	e.command(t, "submit-changeset", `{"changesetId":"`+cs+`","expectedSeq":1}`)

	resp, body := e.do(t, "GET", "/api/v1/changesets/"+cs+"/review", "", e.authed(nil))
	if resp.StatusCode != 200 || body["changeset"].(map[string]any)["state"] != "approved" || len(body["checks"].([]any)) != 2 {
		t.Errorf("review: %d %v", resp.StatusCode, body)
	}
	pub := e.command(t, "publish", `{"changesetId":"`+cs+`","environment":"staging"}`)["id"].(string)

	resp, body = e.do(t, "GET", "/api/v1/publications?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 1 {
		t.Errorf("публикации staging: %v", body)
	}
	resp, body = e.do(t, "GET", "/api/v1/publications?environment=production", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 0 {
		t.Errorf("публикации production: %v", body)
	}
	resp, body = e.do(t, "GET", "/api/v1/publications", "", e.authed(nil))
	if resp.StatusCode != 200 || len(body["items"].([]any)) != 1 {
		t.Errorf("все публикации: %v", body)
	}
	resp, body = e.do(t, "GET", "/api/v1/publications/"+pub, "", e.authed(nil))
	if resp.StatusCode != 200 || body["reason"] != "http" || len(body["items"].([]any)) != 1 {
		t.Errorf("публикация: %v", body)
	}
	resp, body = e.do(t, "GET", "/api/v1/documents/"+doc+"?environment=staging", "", e.authed(nil))
	if resp.StatusCode != 200 || body["state"] != "committed" {
		t.Errorf("опубликованный документ: %v", body)
	}
	resp, body = e.do(t, "GET", "/api/v1/documents/"+doc+"?environment=production", "", e.authed(nil))
	expectProblem(t, resp, body, 404, "NOT_FOUND")
	resp, body = e.do(t, "GET", "/api/v1/publications/0192f1c4-7a1e-7c2b-9d10-3b5f2a9e4c11", "", e.authed(nil))
	expectProblem(t, resp, body, 404, "NOT_FOUND")
}
