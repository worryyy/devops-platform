package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/alertgw"
	"github.com/worryyy/devops-platform/platform/server/internal/api"
	"github.com/worryyy/devops-platform/platform/server/internal/auth"
	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
	"github.com/worryyy/devops-platform/platform/server/internal/config"
	"github.com/worryyy/devops-platform/platform/server/internal/db"
	"github.com/worryyy/devops-platform/platform/server/internal/delivery"
	"github.com/worryyy/devops-platform/platform/server/internal/grafanaproxy"
	"github.com/worryyy/devops-platform/platform/server/internal/k8sargo"
	"github.com/worryyy/devops-platform/platform/server/internal/notify"
	"github.com/worryyy/devops-platform/platform/server/internal/obs"
	"github.com/worryyy/devops-platform/platform/server/internal/report"
)

func RunAPI(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	gdb, err := db.Open(ctx, cfg)
	if err != nil {
		return err
	}

	// Argo CD reader is optional: outside the cluster (local dev/tests) the
	// detail page degrades to "not configured" instead of failing boot.
	var argoReader delivery.ArgocdReader
	if client, err := k8sargo.NewInCluster(); err == nil {
		argoReader = adapter{client}
	} else {
		logger.Warn("argocd reader disabled", "error", err)
	}

	// Report runner Jobs need the in-cluster client too; without it the
	// cron/manual trigger records a failed run instead of crashing boot.
	var reportLauncher report.JobLauncher
	if launcher, err := report.NewK8sJobLauncher(); err == nil {
		reportLauncher = launcher
	} else {
		logger.Warn("report job launcher disabled", "error", err)
	}
	var reportObjects report.ObjectStore
	if cfg.MinioEndpoint != "" {
		if store, err := report.NewMinioStore(cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey, false); err == nil {
			reportObjects = store
		} else {
			logger.Warn("report object store disabled", "error", err)
		}
	}

	deliveryStore := delivery.NewStore(gdb)
	alertStore := alertgw.NewStore(gdb)
	feishu := notify.NewFeishuClient(notify.FeishuOptions{
		AppID:         cfg.FeishuAppID,
		AppSecret:     cfg.FeishuAppSecret,
		ChatID:        cfg.FeishuChatID,
		WebhookURL:    cfg.FeishuWebhookURL,
		WebhookSecret: cfg.FeishuWebhookSecret,
	})
	alertGateway := alertgw.NewGateway(alertStore, feishu, logger, cfg.PlatformPublicURL)

	// P4: ClickHouse is an optional collaborator — without it the platform
	// keeps running (PG-only alerts, /logs returns 503 per request).
	var obsQuerier obs.Querier
	var chRecorder obs.EventRecorder
	if cfg.ClickhouseAddr != "" {
		chClient, err := obs.Dial(ctx, obs.Options{
			Addr:     cfg.ClickhouseAddr,
			Database: cfg.ClickhouseDatabase,
			Username: cfg.ClickhouseUser,
			Password: cfg.ClickhousePassword,
			Logger:   logger,
		})
		if err != nil {
			logger.Warn("clickhouse disabled", "addr", cfg.ClickhouseAddr, "error", err)
		} else {
			defer chClient.Close()
			obsQuerier = chClient
			chRecorder = chClient
			// 事件双写（P4-1.2）：告警与发布生命周期镜像进 CH events
			alertGateway.SetEventRecorder(chClient)
			logger.Info("clickhouse events mirror armed")
		}
	}

	reportService := report.NewService(report.NewStore(gdb), reportLauncher, cfg, logger)
	if err := reportService.Start(); err != nil {
		logger.Warn("report cron disabled", "error", err)
	}
	defer reportService.Stop()

	// The Grafana proxy only mounts when a target and credentials exist;
	// a missing embed must not take the platform down.
	routerDeps := api.Deps{
		Logger:    logger,
		JWTSecret: cfg.JWTSecret,
		Auth: api.AuthHandlers{
			Users:  auth.NewUserStore(gdb),
			Secret: cfg.JWTSecret,
			Now:    time.Now,
		},
		Services: api.ServicesHandlers{Catalog: catalog.NewStore(gdb)},
		Catalog:  api.CatalogHandlers{Catalog: catalog.NewStore(gdb)},
		Delivery: delivery.Handlers{
			Store:   deliveryStore,
			Jenkins: delivery.NewJenkinsClient(cfg.JenkinsURL, cfg.JenkinsUser, cfg.JenkinsToken, "ecampus-pipeline"),
			GitHub:  delivery.NewGitHubClient(cfg.GitHubToken, cfg.GitOpsOwner, cfg.GitOpsRepo),
			Argo:    argoReader,
			Cfg:     cfg,
			Recorder: chRecorder,
		},
		Alertgw: alertgw.Handlers{Gateway: alertGateway, Store: alertStore, Now: time.Now},
		Reports: report.Handlers{
			Service: reportService,
			Store:   report.NewStore(gdb),
			Objects: reportObjects,
			DB:      gdb,
			Cfg:     cfg,
			Now:     time.Now,
		},
	}
	if obsQuerier != nil {
		routerDeps.Obs = obs.Handlers{Querier: obsQuerier, Logger: logger}
	}
	if cfg.GrafanaURL != "" && (cfg.GrafanaToken != "" || cfg.GrafanaUser != "") {
		handler, err := grafanaproxy.Handler(grafanaproxy.Options{
			Target:   cfg.GrafanaURL,
			Token:    cfg.GrafanaToken,
			User:     cfg.GrafanaUser,
			Password: cfg.GrafanaPassword,
		})
		if err != nil {
			logger.Warn("grafana proxy disabled", "error", err)
		} else {
			routerDeps.GrafanaProxy = handler
		}
	} else {
		logger.Warn("grafana proxy disabled: no url or credentials")
	}

	server := &http.Server{Addr: cfg.HTTPAddr, Handler: api.NewAPIRouter(routerDeps)}

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

// adapter bridges k8sargo.Client to delivery.ArgocdReader without leaking
// client-go types into the delivery package.
type adapter struct{ c *k8sargo.Client }

func (a adapter) Get(ctx context.Context, name string) (delivery.ArgoAppStatus, error) {
	st, err := a.c.Get(ctx, name)
	if err != nil {
		return delivery.ArgoAppStatus{}, err
	}
	return delivery.ArgoAppStatus{
		Name:        st.Name,
		SyncStatus:  st.SyncStatus,
		Health:      st.Health,
		SyncVersion: st.SyncVersion,
	}, nil
}
