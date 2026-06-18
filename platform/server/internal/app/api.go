package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/api"
	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
	"github.com/worryyy/devops-platform/platform/server/internal/config"
	"github.com/worryyy/devops-platform/platform/server/internal/queue"
	"github.com/worryyy/devops-platform/platform/server/internal/release"
	"github.com/worryyy/devops-platform/platform/server/internal/store"
)

func RunAPI(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	catalogData, err := catalog.Load(cfg.ServiceCatalogPath)
	if err != nil {
		return err
	}

	db, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	mq, err := queue.NewRabbitMQ(queue.Config{
		URL:         cfg.RabbitMQURL,
		Exchange:    cfg.RabbitMQExchange,
		Queue:       cfg.RabbitMQQueue,
		DLQ:         cfg.RabbitMQDLQ,
		RoutingKey:  cfg.RabbitMQRoutingKey,
		ConsumerTag: cfg.RabbitMQConsumerTag,
	}, logger)
	if err != nil {
		return err
	}
	defer mq.Close()

	releaseService := release.NewService(catalogData, db, mq, cfg.ReleaseLockTTL)
	catalogService := catalog.NewServiceLayer(catalogData, cfg.ServiceCatalogPath)
	router := api.NewBaseRouter()
	modules := newAPIModules(moduleDependencies{
		Store:          db,
		CatalogService: catalogService,
		ReleaseService: releaseService,
	})
	modules.Register(router.Group("/api"))
	api.RegisterHealthRoutes(router, db)
	api.RegisterFrontend(router, frontendFS, "frontend/dist")
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
