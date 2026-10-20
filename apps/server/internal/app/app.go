// Package app — подкоманды бинарника cms (12-security-ops.md §1.2): serve, migrate, bootstrap, version.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/qahnaarln/project-17/apps/server/internal/assets"
	"github.com/riverqueue/river"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/qahnaarln/project-17/apps/server/internal/agents"
	"github.com/qahnaarln/project-17/apps/server/internal/capabilities"
	"github.com/qahnaarln/project-17/apps/server/internal/changes"
	"github.com/qahnaarln/project-17/apps/server/internal/commandbus"
	"github.com/qahnaarln/project-17/apps/server/internal/config"
	"github.com/qahnaarln/project-17/apps/server/internal/delivery"
	"github.com/qahnaarln/project-17/apps/server/internal/httpapi"
	"github.com/qahnaarln/project-17/apps/server/internal/identity"
	"github.com/qahnaarln/project-17/apps/server/internal/imagedelivery"
	"github.com/qahnaarln/project-17/apps/server/internal/jobs"
	"github.com/qahnaarln/project-17/apps/server/internal/manifestregistry"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
	"github.com/qahnaarln/project-17/apps/server/internal/projects"
	"github.com/qahnaarln/project-17/apps/server/internal/publishing"
	"github.com/qahnaarln/project-17/apps/server/internal/realtime"
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

	assetService := &assets.Service{Pool: pool}
	if cfg.S3Endpoint != "" {
		assetService.Storage, err = assets.NewS3(cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cfg.S3Bucket, cfg.S3Secure)
		if err != nil {
			return err
		}
	}
	var purger jobs.Purger = jobs.LogPurger{Log: log}
	var images *imagedelivery.Service
	if cfg.Images.PublicURL != "" {
		images, err = imagedelivery.New(cfg.Images)
		if err != nil {
			return err
		}
		images.Pool = pool
	}
	if cfg.CDNPurgeURL != "" {
		purger = jobs.HTTPPurger{URL: cfg.CDNPurgeURL, Token: cfg.CDNPurgeToken}
	}
	bus := commandbus.New(pool)
	capabilityDelivery := &capabilities.DeliveryService{Pool: pool, Catalog: cfg.CapabilityWebhooks, Log: log}
	if err := capabilityDelivery.ValidateCatalog(ctx); err != nil {
		return err
	}
	capabilityDelivery.Register(bus)
	capabilityGithub := &capabilities.GithubService{Pool: pool, Catalog: cfg.CapabilityGithub}
	if err := capabilityGithub.ValidateCatalog(ctx); err != nil {
		return err
	}
	capabilityGithub.Register(bus)
	githubRecovery := &capabilities.GithubRecoveryWorker{Service: capabilityGithub}
	deliveryRecovery := &capabilities.DeliveryRecoveryWorker{Service: capabilityDelivery}
	capabilityFulfillment := &capabilities.FulfillmentService{Pool: pool, Github: capabilityGithub}
	agentService := &agents.Service{Pool: pool, Provider: agents.OpenRouter{Key: cfg.OpenRouterKey}, DefaultModel: cfg.OpenRouterModel}
	agentService.Register(bus)
	queue, err := jobs.NewProcessor(pool, purger, log, func(w *river.Workers) error { return river.AddWorkerSafely(w, &assets.Worker{Service: assetService}) }, func(w *river.Workers) error { return river.AddWorkerSafely(w, &agents.Worker{Service: agentService}) }, func(w *river.Workers) error {
		return river.AddWorkerSafely(w, &capabilities.DeliveryWorker{Service: capabilityDelivery})
	}, func(w *river.Workers) error { return river.AddWorkerSafely(w, deliveryRecovery) }, func(w *river.Workers) error {
		return river.AddWorkerSafely(w, &capabilities.GithubWorker{Service: capabilityGithub})
	}, func(w *river.Workers) error { return river.AddWorkerSafely(w, githubRecovery) }, func(w *river.Workers) error {
		return river.AddWorkerSafely(w, &capabilities.PreviewExpiryWorker{Pool: pool})
	}, func(w *river.Workers) error {
		return river.AddWorkerSafely(w, &capabilities.FulfillmentWorker{Service: capabilityFulfillment})
	})
	if err != nil {
		return err
	}
	githubRecovery.Queue = queue
	queue.PeriodicJobs().Add(river.NewPeriodicJob(river.PeriodicInterval(30*time.Second), func() (river.JobArgs, *river.InsertOpts) { return capabilities.GithubRecoveryArgs{}, nil }, &river.PeriodicJobOpts{ID: "capability-github-recovery", RunOnStart: true}))
	deliveryRecovery.Queue = queue
	capabilityFulfillment.Queue = queue
	queue.PeriodicJobs().Add(river.NewPeriodicJob(river.PeriodicInterval(10*time.Second), func() (river.JobArgs, *river.InsertOpts) { return capabilities.FulfillmentArgs{}, nil }, &river.PeriodicJobOpts{ID: "capability-fulfillment", RunOnStart: true}))
	queue.PeriodicJobs().Add(river.NewPeriodicJob(river.PeriodicInterval(time.Hour), func() (river.JobArgs, *river.InsertOpts) { return capabilities.PreviewExpiryArgs{}, nil }, &river.PeriodicJobOpts{ID: "capability-preview-expiry", RunOnStart: true}))
	queue.PeriodicJobs().Add(river.NewPeriodicJob(river.PeriodicInterval(30*time.Second), func() (river.JobArgs, *river.InsertOpts) { return capabilities.DeliveryRecoveryArgs{}, nil }, &river.PeriodicJobOpts{ID: "capability-delivery-recovery", RunOnStart: true}))
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := queue.Stop(stopCtx); err != nil {
			log.Warn("job queue stop", "error", err)
		}
	}()

	realtime.Register(bus)
	bus.WithContext(jobs.WithClient(queue))
	projects.RegisterWith(bus, projects.Preview{Source: capabilityGithub.PreviewSource, Origins: cfg.PreviewOrigins})
	identity.Register(bus)
	changes.Register(bus)
	workflow.Register(bus)
	publishing.Register(bus)
	delivery.Register(bus)
	manifestregistry.RegisterWith(bus, cfg.PreviewOrigins)
	capabilities.Register(bus)
	assetService.Register(bus)
	if err := queue.Start(ctx); err != nil {
		return err
	}
	recoveryCtx, recoveryCancel := context.WithCancel(ctx)
	defer recoveryCancel()
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			if err := agentService.Recover(recoveryCtx); err != nil && recoveryCtx.Err() == nil {
				log.Warn("agent recovery failed")
			}
			select {
			case <-recoveryCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()

	identities, err := identity.New(ctx, pool, cfg.Identity)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           httpapi.NewRouter(httpapi.Deps{Pool: pool, Bus: bus, Log: log, Version: Version, Assets: assetService, Images: images, Identity: identities, Agents: agentService, CapabilityGithub: capabilityGithub, CapabilityDelivery: capabilityDelivery}),
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
