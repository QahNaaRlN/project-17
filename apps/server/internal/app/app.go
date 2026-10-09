// Package app — подкоманды бинарника cms (12-security-ops.md §1.2): serve, migrate, bootstrap, version.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/config"
	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/workflow"
)

// Version подставляется при сборке: -ldflags "-X .../internal/app.Version=…".
var Version = "dev"

const usage = `Использование: cms <команда> [флаги]

Команды:
  serve                       HTTP-сервер (Command API, Query API, health)
  migrate up|down|status|version
                              миграции схемы БД
  bootstrap -slug S -name N   создать проект, окружения, роль admin и сервисный токен
  version                     версия сборки

Конфигурация — переменные окружения CMS_* (см. apps/server/README.md).
`

// ErrUsage — неверные аргументы командной строки.
var ErrUsage = errors.New("usage")

// Run исполняет подкоманду. ready вызывается, когда serve начал принимать соединения
// (используется тестами; может быть nil).
func Run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer, ready func(addr string)) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return ErrUsage
	}
	if args[0] == "version" {
		fmt.Fprintln(stdout, Version)
		return nil
	}

	cfg, err := config.FromEnv(getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))

	switch args[0] {
	case "serve":
		return serve(ctx, cfg, log, ready)
	case "migrate":
		if len(args) != 2 {
			fmt.Fprint(stdout, usage)
			return ErrUsage
		}
		pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		return postgres.Migrate(ctx, pool, args[1], log)
	case "bootstrap":
		return bootstrap(ctx, cfg, args[1:], stdout)
	}
	fmt.Fprint(stdout, usage)
	return ErrUsage
}

func serve(ctx context.Context, cfg config.Config, log *slog.Logger, ready func(string)) error {
	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	bus := commandbus.New(pool)
	projects.Register(bus)
	changes.Register(bus)
	workflow.Register(bus)
	publishing.Register(bus)

	srv := &http.Server{
		Handler:           httpapi.NewRouter(httpapi.Deps{Pool: pool, Bus: bus, Log: log, Version: Version}),
		ReadHeaderTimeout: 10 * time.Second,
	}
	ln, err := net.Listen("tcp", cfg.HTTPAddr)
	if err != nil {
		return err
	}
	log.Info("http server started", "addr", ln.Addr().String(), "version", Version, "commands", bus.Names())
	if ready != nil {
		ready(ln.Addr().String())
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func bootstrap(ctx context.Context, cfg config.Config, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(stdout)
	slug := fs.String("slug", "", "идентификатор проекта (латиница, цифры, '-')")
	name := fs.String("name", "", "название проекта")
	ttl := fs.Duration("token-ttl", 90*24*time.Hour, "срок действия сервисного токена")
	if err := fs.Parse(args); err != nil {
		return ErrUsage
	}
	if *slug == "" || *name == "" {
		fs.Usage()
		return ErrUsage
	}
	pool, err := postgres.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	res, err := projects.Bootstrap(ctx, pool, *slug, *name, *ttl)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "Проект %s создан (id %s).\nСервисный токен (показывается один раз, действует до %s):\n%s\n",
		*slug, res.ProjectID, res.ExpiresAt.Format(time.RFC3339), res.Token)
	return nil
}
