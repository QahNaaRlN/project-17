// Package redistest — Redis для интеграционных тестов кэша Delivery и лимитов
// (docs/delivery-cache-limits.md, STU-521).
//
// Сервер берётся из CMS_TEST_REDIS_URL; если переменная не задана, поднимается контейнер
// redis:7 через testcontainers (нужен Docker) — один на пакет. Каждый тест очищает базу.
package redistest

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/qahnaarln/project-17/apps/server/internal/redisstore"
)

var (
	mu        sync.Mutex
	serverURL string
)

// New возвращает клиент к пустой базе Redis.
func New(t testing.TB) *redisstore.Client {
	t.Helper()
	c, err := redisstore.Open(url(t), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.R.FlushDB(context.Background()).Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// Down — клиент к адресу, где Redis нет: каждое обращение завершается ошибкой.
func Down(t testing.TB, log *slog.Logger) *redisstore.Client {
	t.Helper()
	c := redisstore.New(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", MaxRetries: -1}), log)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func url(t testing.TB) string {
	t.Helper()
	mu.Lock()
	defer mu.Unlock()
	if serverURL != "" {
		return serverURL
	}
	if serverURL = os.Getenv("CMS_TEST_REDIS_URL"); serverURL != "" {
		return serverURL
	}
	ctx := context.Background()
	container, err := tcredis.Run(ctx, "mirror.gcr.io/library/redis:7-alpine")
	if err != nil {
		t.Fatalf("redistest: запуск Redis в контейнере (или задайте CMS_TEST_REDIS_URL): %v", err)
	}
	raw, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Docker Desktop forwards ports on IPv4; localhost may resolve to ::1 first.
	serverURL = strings.Replace(raw, "localhost", "127.0.0.1", 1)
	return serverURL
}
