package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/worryyy/devops-platform/platform/server/internal/app"
	"github.com/worryyy/devops-platform/platform/server/internal/auth"
	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
	"github.com/worryyy/devops-platform/platform/server/internal/config"
	"github.com/worryyy/devops-platform/platform/server/internal/migrate"
	"github.com/worryyy/devops-platform/platform/server/internal/releasestore"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) < 2 {
		usageAndExit()
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "api":
		cfg, err := config.Load()
		if err != nil {
			logger.Error("config invalid", "error", err)
			os.Exit(1)
		}
		logger.Info("starting", "level", cfg.LogLevel.String())
		if err := app.RunAPI(ctx, cfg, logger); err != nil {
			logger.Error("api exited", "error", err)
			os.Exit(1)
		}
	case "migrate":
		cfg, err := config.Load()
		if err != nil {
			logger.Error("config invalid", "error", err)
			os.Exit(1)
		}
		if err := migrate.Up(cfg.DatabaseURL); err != nil {
			logger.Error("migrate failed", "error", err)
			os.Exit(1)
		}
		logger.Info("migrations applied")
	case "user-create":
		os.Exit(auth.RunUserCreate(os.Args[2:], os.Stdout, os.Stderr))
	case "catalog":
		os.Exit(catalog.RunCLI(os.Args[2:], os.Stdout, os.Stderr))
	case "release-record":
		os.Exit(releasestore.RunCLI(os.Args[2:], os.Stdout, os.Stderr))
	default:
		usageAndExit()
	}
}

func usageAndExit() {
	fmt.Fprintln(os.Stderr, "usage: platform-server api|migrate|user-create|catalog|release-record")
	os.Exit(2)
}
