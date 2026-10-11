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
	for _, args := range [][]string{{"serve"}, {"migrate", "up"}, {"bootstrap", "-slug", "s1", "-name", "n"}} {
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

	for _, args := range [][]string{{"bootstrap"}, {"bootstrap", "-slug", "s1"}, {"bootstrap", "-bogus"}} {
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
	if _, err := run(t, env, "bootstrap", "-slug", "store", "-name", "x"); err == nil {
		t.Error("повторный bootstrap должен падать")
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
