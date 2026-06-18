package store

import (
	"context"
	"fmt"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/build"
	"github.com/worryyy/devops-platform/platform/server/internal/dashboard"
	"github.com/worryyy/devops-platform/platform/server/internal/deploy"
	"gorm.io/gorm"
)

func (s *Store) DashboardSummary(ctx context.Context, filter dashboard.SummaryFilter) (dashboard.Summary, error) {
	from := dashboardStart(filter.Range)
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var summary dashboard.Summary

	_ = s.db.WithContext(ctx).Model(&buildRecord{}).Where("created_at >= ?", today).Count(&summary.TodayBuilds).Error
	_ = s.db.WithContext(ctx).Model(&deployRecord{}).Where("created_at >= ?", today).Count(&summary.TodayDeploys).Error
	_ = s.db.WithContext(ctx).Model(&buildRecord{}).Where("status = ?", string(build.StatusRunning)).Count(&summary.RunningBuilds).Error
	_ = s.db.WithContext(ctx).Model(&deployRecord{}).Where("status = ?", string(deploy.StatusRunning)).Count(&summary.RunningDeploys).Error
	_ = s.buildDashboardQuery(ctx, from, filter).Where("status = ?", string(build.StatusFailed)).Count(&summary.RecentFailedBuilds).Error
	_ = s.deployDashboardQuery(ctx, from, filter).Where("status = ?", string(deploy.StatusFailed)).Count(&summary.RecentFailedDeploys).Error

	var totalBuilds int64
	var successBuilds int64
	_ = s.buildDashboardQuery(ctx, from, filter).Count(&totalBuilds).Error
	_ = s.buildDashboardQuery(ctx, from, filter).Where("status = ?", string(build.StatusSuccess)).Count(&successBuilds).Error
	summary.BuildSuccessRate = ratio(successBuilds, totalBuilds)
	summary.AverageBuildSeconds = s.averageDuration(ctx, "build_records", from, filter)

	var totalDeploys int64
	var successDeploys int64
	_ = s.deployDashboardQuery(ctx, from, filter).Count(&totalDeploys).Error
	_ = s.deployDashboardQuery(ctx, from, filter).Where("status = ?", string(deploy.StatusSuccess)).Count(&successDeploys).Error
	summary.DeploySuccessRate = ratio(successDeploys, totalDeploys)
	summary.AverageDeploySeconds = s.averageDuration(ctx, "deploy_records", from, filter)

	summary.BuildStatusDistribution = s.statusCounts(ctx, "build_records", from, filter)
	summary.DeployStatusDistribution = s.statusCounts(ctx, "deploy_records", from, filter)
	summary.TopFailedServices = s.topFailedServices(ctx, from)
	summary.BuildTrend = s.trend(ctx, "build_records", from, filter)
	summary.DeployTrend = s.trend(ctx, "deploy_records", from, filter)
	summary.RecentBuilds, _ = s.ListBuilds(ctx, build.ListFilter{Limit: 10})
	summary.RecentDeploys, _ = s.ListDeploys(ctx, deploy.ListFilter{Limit: 10})
	summary.ActiveLocks, _ = s.ListActiveLocks(ctx, filter.ServiceName, filter.Environment)
	return summary, nil
}

func dashboardStart(value string) time.Time {
	now := time.Now().UTC()
	switch value {
	case "7d":
		return now.AddDate(0, 0, -7)
	case "30d":
		return now.AddDate(0, 0, -30)
	default:
		return now.Truncate(24 * time.Hour)
	}
}

func ratio(part, total int64) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func (s *Store) buildDashboardQuery(ctx context.Context, from time.Time, filter dashboard.SummaryFilter) *gorm.DB {
	query := s.db.WithContext(ctx).Model(&buildRecord{}).Where("created_at >= ?", from)
	if filter.ServiceName != "" {
		query = query.Where("service_name = ?", filter.ServiceName)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	return query
}

func (s *Store) deployDashboardQuery(ctx context.Context, from time.Time, filter dashboard.SummaryFilter) *gorm.DB {
	query := s.db.WithContext(ctx).Model(&deployRecord{}).Where("created_at >= ?", from)
	if filter.ServiceName != "" {
		query = query.Where("service_name = ?", filter.ServiceName)
	}
	if filter.Environment != "" {
		query = query.Where("environment = ?", filter.Environment)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	return query
}

func (s *Store) statusCounts(ctx context.Context, table string, from time.Time, filter dashboard.SummaryFilter) []dashboard.StatusCount {
	var items []dashboard.StatusCount
	query := fmt.Sprintf("select status, count(*) as count from %s where created_at >= ?", table)
	args := []interface{}{from}
	if filter.ServiceName != "" {
		query += " and service_name = ?"
		args = append(args, filter.ServiceName)
	}
	if table == "deploy_records" && filter.Environment != "" {
		query += " and environment = ?"
		args = append(args, filter.Environment)
	}
	query += " group by status order by count desc"
	_ = s.db.WithContext(ctx).Raw(query, args...).Scan(&items).Error
	if items == nil {
		return []dashboard.StatusCount{}
	}
	return items
}

func (s *Store) topFailedServices(ctx context.Context, from time.Time) []dashboard.ServiceCount {
	var items []dashboard.ServiceCount
	_ = s.db.WithContext(ctx).Raw(`
		select service_name, count(*) as count
		from (
			select service_name from build_records where created_at >= ? and status = 'failed'
			union all
			select service_name from deploy_records where created_at >= ? and status = 'failed'
		) failures
		group by service_name
		order by count desc
		limit 5
	`, from, from).Scan(&items).Error
	if items == nil {
		return []dashboard.ServiceCount{}
	}
	return items
}

func (s *Store) trend(ctx context.Context, table string, from time.Time, filter dashboard.SummaryFilter) []dashboard.TrendPoint {
	var items []dashboard.TrendPoint
	query := fmt.Sprintf("select to_char(date_trunc('day', created_at), 'YYYY-MM-DD') as date, count(*) as count from %s where created_at >= ?", table)
	args := []interface{}{from}
	if filter.ServiceName != "" {
		query += " and service_name = ?"
		args = append(args, filter.ServiceName)
	}
	if table == "deploy_records" && filter.Environment != "" {
		query += " and environment = ?"
		args = append(args, filter.Environment)
	}
	query += " group by date order by date asc"
	_ = s.db.WithContext(ctx).Raw(query, args...).Scan(&items).Error
	if items == nil {
		return []dashboard.TrendPoint{}
	}
	return items
}

func (s *Store) averageDuration(ctx context.Context, table string, from time.Time, filter dashboard.SummaryFilter) float64 {
	var value float64
	query := fmt.Sprintf("select coalesce(avg(extract(epoch from (finished_at - started_at))), 0) from %s where created_at >= ? and started_at is not null and finished_at is not null", table)
	args := []interface{}{from}
	if filter.ServiceName != "" {
		query += " and service_name = ?"
		args = append(args, filter.ServiceName)
	}
	if table == "deploy_records" && filter.Environment != "" {
		query += " and environment = ?"
		args = append(args, filter.Environment)
	}
	if filter.Status != "" {
		query += " and status = ?"
		args = append(args, filter.Status)
	}
	_ = s.db.WithContext(ctx).Raw(query, args...).Scan(&value).Error
	return value
}
