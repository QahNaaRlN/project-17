// Command cms — сервер CMS: serve, migrate, bootstrap, version.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/qahnaarln/project-17/apps/server/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := app.Run(ctx, os.Args[1:], os.Getenv, os.Stdout, nil); err != nil {
		if !errors.Is(err, app.ErrUsage) {
			fmt.Fprintln(os.Stderr, "cms:", err)
		}
		os.Exit(1)
	}
}
