package dashboard

import (
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/build"
	"github.com/worryyy/devops-platform/platform/server/internal/deploy"
)

type SummaryFilter struct {
	Range       string
	ServiceName string
	Environment string
	Status      string
}

type Summary struct {
	Range                    string          `json:"range"`
	TodayBuilds              int64           `json:"today_builds"`
	TodayDeploys             int64           `json:"today_deploys"`
	BuildSuccessRate         float64         `json:"build_success_rate"`
	DeploySuccessRate        float64         `json:"deploy_success_rate"`
	AverageBuildSeconds      float64         `json:"average_build_seconds"`
	AverageDeploySeconds     float64         `json:"average_deploy_seconds"`
	RunningBuilds            int64           `json:"running_builds"`
	RunningDeploys           int64           `json:"running_deploys"`
	RecentFailedBuilds       int64           `json:"recent_failed_builds"`
	RecentFailedDeploys      int64           `json:"recent_failed_deploys"`
	BuildTrend               []TrendPoint    `json:"build_trend"`
	DeployTrend              []TrendPoint    `json:"deploy_trend"`
	BuildStatusDistribution  []StatusCount   `json:"build_status_distribution"`
	DeployStatusDistribution []StatusCount   `json:"deploy_status_distribution"`
	TopFailedServices        []ServiceCount  `json:"top_failed_services"`
	RecentBuilds             []build.Record  `json:"recent_builds"`
	RecentDeploys            []deploy.Record `json:"recent_deploys"`
	ActiveLocks              []deploy.Lock   `json:"active_locks"`
	GeneratedAt              time.Time       `json:"generated_at"`
}

type TrendPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type StatusCount struct {
	Status string `json:"status"`
	Count  int64  `json:"count"`
}

type ServiceCount struct {
	ServiceName string `json:"service_name"`
	Count       int64  `json:"count"`
}
