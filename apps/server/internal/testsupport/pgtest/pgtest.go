// Package pgtest — изолированные базы PostgreSQL для интеграционных тестов (docs/testing.md §3.4).
//
// Сервер берётся из CMS_TEST_DATABASE_URL; если переменная не задана, поднимается контейнер
// postgres:16 через testcontainers (нужен Docker). Миграции применяются один раз к базе-шаблону,
// каждый тест получает копию шаблона (CREATE DATABASE … TEMPLATE) и удаляет её по завершении.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
)

var (
	mu        sync.Mutex
	serverURL string // URL сервера с базой "postgres"
	template  string // имя базы-шаблона
	cleanup   []func()
)

// Main — TestMain для пакетов с интеграционными тестами.
func Main(m *testing.M) {
	code := m.Run()
	mu.Lock()
	for i := len(cleanup) - 1; i >= 0; i-- {
		cleanup[i]()
	}
	mu.Unlock()
	os.Exit(code)
}

// NewDB возвращает пул к новой базе с применёнными миграциями.
func NewDB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()
	base, tpl := setup(t)

	name := "cms_test_" + randomSuffix()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, tpl)); err != nil {
		t.Fatal(err)
	}
	pool, err := postgres.Connect(ctx, WithDatabase(base, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		conn, err := pgx.Connect(context.Background(), base)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close(context.Background())
		if _, err := conn.Exec(context.Background(), fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", name)); err != nil {
			t.Error(err)
		}
	})
	return pool
}

// URL возвращает строку подключения к новой пустой (без миграций) базе — для тестов миграций и CLI.
func URL(t testing.TB) string {
	t.Helper()
	ctx := context.Background()
	base, _ := setup(t)
	name := "cms_test_" + randomSuffix()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn, err := pgx.Connect(context.Background(), base)
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close(context.Background())
		_, _ = conn.Exec(context.Background(), fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", name))
	})
	return WithDatabase(base, name)
}

// WithDatabase заменяет имя базы в URL подключения.
func WithDatabase(rawURL, db string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		panic(err)
	}
	u.Path = "/" + db
	return u.String()
}

func setup(t testing.TB) (base, tpl string) {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if template != "" {
		return serverURL, template
	}
	ctx := context.Background()

	serverURL = os.Getenv("CMS_TEST_DATABASE_URL")
	if serverURL == "" {
		container, err := tcpostgres.Run(ctx, "mirror.gcr.io/library/postgres:16-alpine",
			tcpostgres.WithDatabase("postgres"), tcpostgres.WithUsername("postgres"), tcpostgres.WithPassword("postgres"),
			tcpostgres.BasicWaitStrategies())
		if err != nil {
			t.Fatalf("pgtest: запуск PostgreSQL в контейнере (или задайте CMS_TEST_DATABASE_URL): %v", err)
		}
		cleanup = append(cleanup, func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = container.Terminate(ctx)
		})
		serverURL, err = container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatal(err)
		}
	}

	name := "cms_tpl_" + randomSuffix()
	conn, err := pgx.Connect(ctx, serverURL)
	if err != nil {
		t.Fatalf("pgtest: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	cleanup = append(cleanup, func() {
		conn, err := pgx.Connect(context.Background(), serverURL)
		if err == nil {
			_, _ = conn.Exec(context.Background(), fmt.Sprintf("DROP DATABASE %s WITH (FORCE)", name))
			conn.Close(context.Background())
		}
	})
	pool, err := postgres.Connect(ctx, WithDatabase(serverURL, name))
	if err != nil {
		t.Fatal(err)
	}
	err = postgres.Migrate(ctx, pool, "up", slog.New(slog.NewTextHandler(io.Discard, nil)))
	pool.Close() // у шаблона не должно быть открытых соединений
	if err != nil {
		t.Fatalf("pgtest: миграции: %v", err)
	}
	template = name
	return serverURL, template
}

func randomSuffix() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
