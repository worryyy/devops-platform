package app

import (
	"context"
	"log/slog"

	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
	"github.com/worryyy/devops-platform/platform/server/internal/config"
	"github.com/worryyy/devops-platform/platform/server/internal/integrations/argocd"
	"github.com/worryyy/devops-platform/platform/server/internal/integrations/jenkins"
	k8sclient "github.com/worryyy/devops-platform/platform/server/internal/integrations/kubernetes"
	"github.com/worryyy/devops-platform/platform/server/internal/queue"
	"github.com/worryyy/devops-platform/platform/server/internal/release"
	"github.com/worryyy/devops-platform/platform/server/internal/store"
)

func RunWorker(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
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

	jenkinsClient := jenkins.NewClient(cfg.JenkinsBaseURL, cfg.JenkinsUsername, cfg.JenkinsToken, nil)
	argoReader, err := argocd.NewReader(cfg.Kubeconfig)
	if err != nil {
		return err
	}
	kubeClient, err := k8sclient.NewClient(cfg.Kubeconfig)
	if err != nil {
		return err
	}

	worker := release.NewWorker(release.WorkerConfig{
		Catalog:             catalogData,
		Store:               db,
		Jenkins:             jenkinsClient,
		ArgoCD:              argoReader,
		Kubernetes:          kubeClient,
		Logger:              logger,
		JenkinsPollInterval: cfg.JenkinsPollInterval,
		JenkinsTimeout:      cfg.JenkinsTimeout,
		ArgoPollInterval:    cfg.ArgoPollInterval,
		ArgoTimeout:         cfg.ArgoTimeout,
		RolloutPollInterval: cfg.RolloutPollInterval,
		RolloutTimeout:      cfg.RolloutTimeout,
		LockTTL:             cfg.ReleaseLockTTL,
	})

	logger.Info("worker listening", "queue", cfg.RabbitMQQueue)
	return mq.Consume(ctx, worker.HandleReleaseMessage)
}
