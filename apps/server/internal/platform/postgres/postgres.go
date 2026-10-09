// Package postgres — подключение к PostgreSQL, миграции и транзакции.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/qahnaarln/project-17/apps/server/db"
)

// Connect открывает пул соединений и проверяет доступность БД.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	return pool, nil
}

// Migrate применяет миграции (команда goose: "up", "down", "status", "version") и пишет
// результат в log.
func Migrate(ctx context.Context, pool *pgxpool.Pool, command string, log *slog.Logger) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer sqlDB.Close()

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrationsFS())
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	switch command {
	case "up":
		results, err := provider.Up(ctx)
		for _, r := range results {
			log.Info("migration applied", "version", r.Source.Version, "path", r.Source.Path, "duration", r.Duration)
		}
		return err
	case "down":
		r, err := provider.Down(ctx)
		if r != nil {
			log.Info("migration rolled back", "version", r.Source.Version, "path", r.Source.Path)
		}
		return err
	case "status":
		statuses, err := provider.Status(ctx)
		for _, s := range statuses {
			log.Info("migration", "version", s.Source.Version, "path", s.Source.Path, "state", s.State)
		}
		return err
	case "version":
		v, err := provider.GetDBVersion(ctx)
		if err == nil {
			log.Info("database version", "version", v)
		}
		return err
	}
	return fmt.Errorf("migrate: неизвестная команда %q (up, down, status, version)", command)
}

func migrationsFS() fs.FS {
	sub, err := fs.Sub(db.Migrations, "migrations")
	if err != nil {
		panic(err) // каталог встроен при сборке
	}
	return sub
}

// InTx выполняет fn в транзакции: фиксирует при успехе, откатывает при ошибке или панике.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsUniqueViolation сообщает, нарушено ли ограничение уникальности (SQLSTATE 23505).
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
