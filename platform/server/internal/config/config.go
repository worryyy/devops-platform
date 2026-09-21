package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	HTTPAddr string

	DatabaseURL string
	JWTSecret   string
	LogLevel    slog.Level

	// Optional for now; consumers land in later phases (Redis session/cache
	// P1 late, MinIO artifacts P3+).
	RedisAddr      string
	MinioEndpoint  string
	MinioAccessKey string
	MinioSecretKey string

	// Delivery integration (P2). All optional at boot: without them the
	// pipeline endpoints fail per-request with a clear 503 instead of
	// taking the whole API down.
	JenkinsURL    string
	JenkinsUser   string
	JenkinsToken  string
	GitHubToken   string
	GitOpsOwner   string
	GitOpsRepo    string
	GiteaURL      string
	WebhookSecret string

	// Observability & alerting (P3). Feishu absent → alerts land in the
	// table but nothing is pushed (dev-friendly degradation). App mode
	// (app id/secret) wins over the bot-webhook mode when both are set.
	FeishuAppID        string
	FeishuAppSecret    string
	FeishuChatID       string
	FeishuWebhookURL    string
	FeishuWebhookSecret string
	PlatformPublicURL   string

	// Grafana embed (P3): /grafana reverse proxy target + credentials.
	GrafanaURL      string
	GrafanaToken    string
	GrafanaUser     string
	GrafanaPassword string

	// Weekly report (P3).
	PrometheusURL     string
	ReportSchedule    string
	ReportTimezone    string
	ReportRunnerImage string
	ReportBucket      string

	// Data pipeline (P4): ClickHouse for /logs search + events mirror.
	// Empty addr disables both per-request (503 on /api/logs, PG-only alerts).
	ClickhouseAddr     string
	ClickhouseDatabase string
	ClickhouseUser     string
	ClickhousePassword string
}

// Load reads the environment and fails fast when a critical key is missing,
// reporting every missing key at once instead of the first one.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:      getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		JWTSecret:     os.Getenv("JWT_SECRET"),
		RedisAddr:     os.Getenv("REDIS_ADDR"),
		MinioEndpoint: os.Getenv("MINIO_ENDPOINT"),

		MinioAccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		MinioSecretKey: os.Getenv("MINIO_SECRET_KEY"),

		JenkinsURL:    getEnv("JENKINS_URL", "http://jenkins.delivery.svc.cluster.local:8080"),
		JenkinsUser:   os.Getenv("JENKINS_USER"),
		JenkinsToken:  os.Getenv("JENKINS_TOKEN"),
		GitHubToken:   os.Getenv("GITHUB_TOKEN"),
		GitOpsOwner:   getEnv("GITOPS_OWNER", "worryyy"),
		GitOpsRepo:    getEnv("GITOPS_REPO", "app-test"),
		GiteaURL:      getEnv("GITEA_URL", "http://gitea-http.delivery.svc.cluster.local:3000"),
		WebhookSecret: os.Getenv("PLATFORM_WEBHOOK_SECRET"),

		FeishuAppID:         os.Getenv("FEISHU_APP_ID"),
		FeishuAppSecret:     os.Getenv("FEISHU_APP_SECRET"),
		FeishuChatID:        os.Getenv("FEISHU_CHAT_ID"),
		FeishuWebhookURL:    os.Getenv("FEISHU_WEBHOOK_URL"),
		FeishuWebhookSecret: os.Getenv("FEISHU_WEBHOOK_SECRET"),
		PlatformPublicURL:   getEnv("PLATFORM_PUBLIC_URL", "http://platform.100.115.204.94.nip.io"),

		GrafanaURL:      getEnv("GRAFANA_URL", "http://grafana.platform.svc"),
		GrafanaToken:    os.Getenv("GRAFANA_TOKEN"),
		GrafanaUser:     os.Getenv("GRAFANA_USER"),
		GrafanaPassword: os.Getenv("GRAFANA_PASSWORD"),

		PrometheusURL:     getEnv("PROMETHEUS_URL", "http://prometheus-server.monitoring.svc"),
		ReportSchedule:    getEnv("REPORT_SCHEDULE", "0 9 * * 1"),
		ReportTimezone:    getEnv("REPORT_TIMEZONE", "Asia/Shanghai"),
		ReportRunnerImage: getEnv("REPORT_RUNNER_IMAGE", "crpi-gfwwpdquc14b7w22.cn-shanghai.personal.cr.aliyuncs.com/pulseops/report-runner:dev"),
		ReportBucket:      getEnv("REPORTS_BUCKET", "reports"),

		ClickhouseAddr:     getEnv("CLICKHOUSE_ADDR", "platform-clickhouse.platform.svc.cluster.local:9000"),
		ClickhouseDatabase: getEnv("CLICKHOUSE_DB", "platform"),
		ClickhouseUser:     getEnv("CLICKHOUSE_USER", "platform"),
		ClickhousePassword: os.Getenv("CLICKHOUSE_PASSWORD"),
	}

	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if cfg.JWTSecret == "" {
		missing = append(missing, "JWT_SECRET")
	}
	if len(missing) > 0 {
		return cfg, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}

	cfg.LogLevel = parseLevel(os.Getenv("LOG_LEVEL"))
	return cfg, nil
}

// DeliveryReady reports whether the Jenkins/GitHub integration is configured.
func (c Config) DeliveryReady() bool {
	return c.JenkinsUser != "" && c.JenkinsToken != ""
}

// PlatformInternalURL is the in-cluster address the report runner uses to
// call back for stats and completion.
func (c Config) PlatformInternalURL() string {
	return "http://platform-server-api.platform.svc/api"
}

func parseLevel(value string) slog.Level {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
