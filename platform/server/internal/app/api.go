package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/api"
	"github.com/worryyy/devops-platform/platform/server/internal/config"
)

func RunAPI(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	router := api.NewBaseRouter()
	api.RegisterHealthRoutes(router)

	server := &http.Server{Addr: cfg.HTTPAddr, Handler: router}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown api server: %w", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}
