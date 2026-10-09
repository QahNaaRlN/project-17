package postgres_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/testsupport/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

func TestConnectFails(t *testing.T) {
	ctx := context.Background()
	if _, err := postgres.Connect(ctx, "::not a url"); err == nil {
		t.Error("ожидалась ошибка разбора URL")
	}
	if _, err := postgres.Connect(ctx, "postgres://nobody@127.0.0.1:1/x?connect_timeout=1"); err == nil {
		t.Error("ожидалась ошибка подключения")
	}
}

func TestMigrateLifecycle(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var out bytes.Buffer
	log := slog.New(slog.NewTextHandler(&out, nil))

	for _, cmd := range []string{"up", "version", "status"} {
		if err := postgres.Migrate(ctx, pool, cmd, log); err != nil {
			t.Fatalf("%s: %v", cmd, err)
		}
	}
	if !strings.Contains(out.String(), "00001_init.sql") {
		t.Errorf("в журнале нет применённой миграции:\n%s", out.String())
	}
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.projects') IS NOT NULL").Scan(&exists); err != nil || !exists {
		t.Fatalf("таблица projects не создана: %v", err)
	}
	if !strings.Contains(out.String(), "database version") {
		t.Errorf("version не записал версию в журнал:\n%s", out.String())
	}
	if err := postgres.Migrate(ctx, pool, "down", log); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "migration rolled back") {
		t.Errorf("down не записал откат в журнал:\n%s", out.String())
	}
	if err := pool.QueryRow(ctx, "SELECT to_regclass('public.projects') IS NOT NULL").Scan(&exists); err != nil || exists {
		t.Errorf("down не удалил таблицы: %v", err)
	}
	if err := postgres.Migrate(ctx, pool, "sideways", log); err == nil || !strings.Contains(err.Error(), "sideways") {
		t.Errorf("неизвестная команда: %v", err)
	}
}

func TestInTx(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.NewDB(t)
	if _, err := pool.Exec(ctx, "CREATE TABLE t (v int UNIQUE)"); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM t").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	insert := func(tx pgx.Tx) error { _, err := tx.Exec(ctx, "INSERT INTO t VALUES (1)"); return err }

	if err := postgres.InTx(ctx, pool, insert); err != nil || count() != 1 {
		t.Fatalf("фиксация: %v", err)
	}

	boom := errors.New("boom")
	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO t VALUES (2)"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) || count() != 1 {
		t.Errorf("откат при ошибке: %v, строк %d", err, count())
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Error("паника должна пробрасываться")
			}
		}()
		_ = postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
			_, _ = tx.Exec(ctx, "INSERT INTO t VALUES (3)")
			panic("x")
		})
	}()
	if count() != 1 {
		t.Error("откат при панике")
	}

	err = postgres.InTx(ctx, pool, insert)
	if !postgres.IsUniqueViolation(err) {
		t.Errorf("ожидалось нарушение уникальности: %v", err)
	}
	if postgres.IsUniqueViolation(boom) {
		t.Error("обычная ошибка не нарушение уникальности")
	}

	pool.Close()
	if err := postgres.InTx(ctx, pool, insert); err == nil {
		t.Error("закрытый пул: ожидалась ошибка Begin")
	}
}
