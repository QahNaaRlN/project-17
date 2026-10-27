// Command loadseed заполняет пустую базу CMS (после `cms migrate up`) данными профиля
// нагрузки MVP и пишет секреты для сценариев k6 (docs/load-tests.md, STU-542).
//
//	CMS_DATABASE_URL=postgres://… go run ./cmd/loadseed -out ../../perf/results/seed.private.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/qahnaarln/project-17/apps/server/internal/loadseed"
	"github.com/qahnaarln/project-17/apps/server/internal/platform/postgres"
)

func main() {
	cfg := loadseed.Profile()
	out := flag.String("out", "seed.private.json", "файл с ключами доставки и токенами")
	flag.IntVar(&cfg.Projects, "projects", cfg.Projects, "проектов")
	flag.IntVar(&cfg.Entities, "entities", cfg.Entities, "сущностей на проект")
	flag.IntVar(&cfg.Documents, "documents", cfg.Documents, "страниц на проект")
	flag.IntVar(&cfg.Editors, "editors", cfg.Editors, "пользователей Studio в первом проекте")
	flag.Uint64Var(&cfg.Seed, "seed", cfg.Seed, "зерно генератора")
	flag.Parse()
	if err := run(cfg, *out); err != nil {
		fmt.Fprintln(os.Stderr, "loadseed:", err)
		os.Exit(1)
	}
}

func run(cfg loadseed.Config, out string) error {
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, os.Getenv("CMS_DATABASE_URL"))
	if err != nil {
		return err
	}
	defer pool.Close()
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	started := time.Now()
	res, err := loadseed.Run(ctx, pool, cfg, func(msg string, args ...any) { log.Info(msg, args...) })
	if err != nil {
		return err
	}
	log.Info("seed complete", "projects", len(res.Projects), "duration", time.Since(started).Round(time.Second))
	raw, _ := json.MarshalIndent(res, "", "  ")
	return os.WriteFile(out, raw, 0o600)
}
