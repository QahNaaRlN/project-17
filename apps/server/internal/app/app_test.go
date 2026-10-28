package app_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qahnaarln/project-17/apps/server/internal/app"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

func getenv(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func run(t *testing.T, env map[string]string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := app.Run(context.Background(), args, getenv(env), &out, nil)
	return out.String(), err
}

func TestUsageAndVersion(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"migrate"}} {
		out, err := run(t, map[string]string{"CMS_DATABASE_URL": "postgres://x"}, args...)
		if !errors.Is(err, app.ErrUsage) || !strings.Contains(out, "Использование: cms") {
			t.Errorf("%v: %v %q", args, err, out)
		}
	}
	out, err := run(t, nil, "version")
	if err != nil || strings.TrimSpace(out) != app.Version {
		t.Errorf("version: %v %q", err, out)
	}
}

func TestConfigError(t *testing.T) {
	if _, err := run(t, nil, "serve"); err == nil || !strings.Contains(err.Error(), "CMS_DATABASE_URL") {
		t.Errorf("ожидалась ошибка конфигурации: %v", err)
	}
}

func TestDatabaseUnavailable(t *testing.T) {
	env := map[string]string{"CMS_DATABASE_URL": "postgres://nobody@127.0.0.1:1/x?connect_timeout=1"}
	for _, args := range [][]string{{"serve"}, {"migrate", "up"}, {"bootstrap", "-slug", "s1", "-name", "n"}, {"check-runner-token", "-slug", "s1"}, {"anonymize-user", "-issuer", "i", "-subject", "s"}} {
		if _, err := run(t, env, args...); err == nil {
			t.Errorf("%v: ожидалась ошибка подключения", args)
		}
	}
}

func TestMigrateAndBootstrap(t *testing.T) {
	env := map[string]string{"CMS_DATABASE_URL": pgtest.URL(t)}
	if out, err := run(t, env, "migrate", "up"); err != nil || !strings.Contains(out, "migration applied") {
		t.Fatalf("migrate up: %v\n%s", err, out)
	}

	for _, args := range [][]string{{"bootstrap"}, {"bootstrap", "-slug", "s1"}, {"bootstrap", "-bogus"}, {"bootstrap", "-slug", "s1", "-name", "n", "-max-mode", "ANY"}} {
		if _, err := run(t, env, args...); !errors.Is(err, app.ErrUsage) {
			t.Errorf("%v: %v", args, err)
		}
	}

	out, err := run(t, env, "bootstrap", "-slug", "store", "-name", "Магазин", "-token-ttl", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^cms_svc_[A-Za-z0-9_-]{43}$`).MatchString(out) {
		t.Errorf("в выводе нет токена:\n%s", out)
	}
	// The §2 scenario stand allows CODE zones through project configuration (STU-453).
	if _, err := run(t, env, "bootstrap", "-slug", "code", "-name", "Code", "-max-mode", "CODE"); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), env["CMS_DATABASE_URL"])
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var maxMode string
	if err := pool.QueryRow(context.Background(), `SELECT settings->>'maxMode' FROM projects WHERE slug='code'`).Scan(&maxMode); err != nil || maxMode != "CODE" {
		t.Fatal(maxMode, err)
	}
	if _, err := run(t, env, "bootstrap", "-slug", "store", "-name", "x"); err == nil {
		t.Error("повторный bootstrap должен падать")
	}
	// STU-488: the runner token is printed alone; the cms-checks actor is reused.
	for _, args := range [][]string{{"check-runner-token"}, {"check-runner-token", "-bogus"}} {
		if _, err := run(t, env, args...); !errors.Is(err, app.ErrUsage) {
			t.Errorf("%v: %v", args, err)
		}
	}
	if _, err := run(t, env, "check-runner-token", "-slug", "missing"); err == nil {
		t.Error("токен исполнителя для неизвестного проекта")
	}
	for i := 0; i < 2; i++ {
		out, err := run(t, env, "check-runner-token", "-slug", "store", "-token-ttl", "1h")
		if err != nil || !regexp.MustCompile(`^cms_svc_[A-Za-z0-9_-]{43}
$`).MatchString(out) {
			t.Fatalf("check-runner-token: %v %q", err, out)
		}
	}
	// SEC-008: the operator command validates its flags and reports an unknown person.
	for _, args := range [][]string{{"anonymize-user"}, {"anonymize-user", "-issuer", "i"}, {"anonymize-user", "-bogus"}} {
		if _, err := run(t, env, args...); !errors.Is(err, app.ErrUsage) {
			t.Errorf("%v: %v", args, err)
		}
	}
	if _, err := run(t, env, "anonymize-user", "-issuer", "https://idp.example", "-subject", "nobody"); err == nil {
		t.Error("обезличивание неизвестного пользователя")
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO actors (id, kind, display_name, oidc_issuer, oidc_subject) VALUES (gen_random_uuid(), 'human', 'Ann', 'https://idp.example', 'ann')`); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"обезличен; сессии", "уже обезличен"} {
		if out, err := run(t, env, "anonymize-user", "-issuer", "https://idp.example", "-subject", "ann"); err != nil || !strings.Contains(out, want) {
			t.Fatalf("anonymize-user: %v %q", err, out)
		}
	}
	var runners int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM actors WHERE display_name='cms-checks'`).Scan(&runners); err != nil || runners != 1 {
		t.Fatal(runners, err)
	}

	// Срок действия токена по умолчанию — 90 дней.
	out, err = run(t, env, "bootstrap", "-slug", "store2", "-name", "Второй")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`действует до (\S+)\)`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("нет срока действия:\n%s", out)
	}
	until, err := time.Parse(time.RFC3339, m[1])
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Until(until); d < 89*24*time.Hour || d > 91*24*time.Hour {
		t.Errorf("срок действия по умолчанию %v, ожидалось 90 дней", d)
	}
}

func TestServeLifecycle(t *testing.T) {
	url := pgtest.URL(t)
	if _, err := run(t, map[string]string{"CMS_DATABASE_URL": url}, "migrate", "up"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	addrc := make(chan string, 1)
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() {
		done <- app.Run(ctx, []string{"serve"},
			getenv(map[string]string{"CMS_DATABASE_URL": url, "CMS_HTTP_ADDR": "127.0.0.1:0", "CMS_SHUTDOWN_TIMEOUT": "5s",
				"CMS_S3_ENDPOINT": "localhost:9000", "CMS_S3_ACCESS_KEY": "test-access", "CMS_S3_SECRET_KEY": "test-secret", "CMS_S3_BUCKET": "assets", "CMS_S3_SECURE": "false",
				"CMS_ASSET_PUBLIC_URL": "http://localhost:8080", "CMS_ASSET_SIGNING_KEY": strings.Repeat("11", 32), "CMS_IMGPROXY_URL": "http://localhost:8081", "CMS_IMGPROXY_KEY": strings.Repeat("22", 32), "CMS_IMGPROXY_SALT": strings.Repeat("33", 16)}),
			&out, func(addr string) { addrc <- addr })
	}()
	var addr string
	select {
	case addr = <-addrc:
	case err := <-done:
		t.Fatalf("serve завершился: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("serve не запустился")
	}
	resp, err := http.Get("http://" + addr + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "ready") {
		t.Errorf("readyz: %d %s", resp.StatusCode, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("остановка: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("serve не остановился")
	}
	if !strings.Contains(out.String(), "create-environment") || !strings.Contains(out.String(), "shutting down") {
		t.Errorf("журнал serve:\n%s", out.String())
	}
}

func TestServeListenError(t *testing.T) {
	url := pgtest.URL(t)
	if _, err := run(t, map[string]string{"CMS_DATABASE_URL": url, "CMS_HTTP_ADDR": "256.0.0.1:99999"}, "serve"); err == nil {
		t.Error("ожидалась ошибка прослушивания")
	}
}

// STU-590: a broken telemetry configuration stops serve before it opens the database.
func TestServeTelemetryError(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "broken")
	if _, err := run(t, map[string]string{"CMS_DATABASE_URL": "postgres://127.0.0.1:1/x"}, "serve"); err == nil {
		t.Error("ожидалась ошибка настройки телеметрии")
	}
}
