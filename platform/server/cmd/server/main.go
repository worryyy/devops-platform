package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/worryyy/devops-platform/platform/server/internal/app"
	"github.com/worryyy/devops-platform/platform/server/internal/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if len(os.Args) < 2 {
		usageAndExit()
	}

	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "api":
		if err := app.RunAPI(ctx, cfg, logger); err != nil {
			logger.Error("api exited", "error", err)
			os.Exit(1)
		}
	case "worker":
		if err := app.RunWorker(ctx, cfg, logger); err != nil {
			logger.Error("worker exited", "error", err)
			os.Exit(1)
		}
	default:
		usageAndExit()
	}
}

func usageAndExit() {
	fmt.Fprintln(os.Stderr, "usage: platform-server api|worker")
	os.Exit(2)
}
