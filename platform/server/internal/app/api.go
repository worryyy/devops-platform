package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/api"
	"github.com/worryyy/devops-platform/platform/server/internal/auth"
	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
	"github.com/worryyy/devops-platform/platform/server/internal/config"
	"github.com/worryyy/devops-platform/platform/server/internal/db"
)

func RunAPI(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	gdb, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}

	router := api.NewAPIRouter(api.Deps{
		Logger:    logger,
		JWTSecret: cfg.JWTSecret,
		Auth: api.AuthHandlers{
			Users:  auth.NewUserStore(gdb),
			Secret: cfg.JWTSecret,
			Now:    time.Now,
		},
		Services: api.ServicesHandlers{Catalog: catalog.NewStore(gdb)},
		Catalog:  api.CatalogHandlers{Catalog: catalog.NewStore(gdb)},
	})

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
