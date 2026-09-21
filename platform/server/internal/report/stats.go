package report

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// WindowStats is the aggregate the runner embeds in report.json: release
// counts, pipeline outcome and the alert Top-N for the week.
type WindowStats struct {
	Window struct {
		Start string `json:"start"`
		End   string `json:"end"`
	} `json:"window"`
	Releases struct {
		Total     int            `json:"total"`
		Stable    int            `json:"stable"`
		Failed    int            `json:"failed"`
		Ongoing   int            `json:"ongoing"`
		ByService map[string]int `json:"byService"`
	} `json:"releases"`
	Pipelines struct {
		Total   int `json:"total"`
		Success int `json:"success"`
		Failed  int `json:"failed"`
		Running int `json:"running"`
		Queued  int `json:"queued"`
	} `json:"pipelines"`
	Alerts struct {
		Total int        `json:"total"`
		Top   []AlertTop `json:"top"`
	} `json:"alerts"`
}

// AlertTop is one entry of the noisiest-alerts ranking.
type AlertTop struct {
	Alertname string `json:"alertname"`
	Service   string `json:"service"`
	Count     int64  `json:"count"`
}

type releaseRow struct {
	Service string
	Status  string
	Count   int64
}

type pipelineRow struct {
	Status string
	Count  int64
}

// CollectStats aggregates the platform tables for [start, end).
func CollectStats(ctx context.Context, db *gorm.DB, start, end time.Time) (WindowStats, error) {
	stats := WindowStats{}
	stats.Window.Start = start.Format(time.RFC3339)
	stats.Window.End = end.Format(time.RFC3339)

	var releases []releaseRow
	if err := db.WithContext(ctx).
		Raw(`select service, release_status as status, count(*) as count
		     from service_releases
		     where coalesce(released_at, created_at) >= ? and coalesce(released_at, created_at) < ?
		     group by 1, 2`, start, end).
		Scan(&releases).Error; err != nil {
		return stats, err
	}
	if stats.Releases.ByService == nil {
		stats.Releases.ByService = map[string]int{}
	}
	for _, row := range releases {
		stats.Releases.Total += int(row.Count)
		stats.Releases.ByService[row.Service] += int(row.Count)
		switch row.Status {
		case "stable":
			stats.Releases.Stable += int(row.Count)
		case "failed":
			stats.Releases.Failed += int(row.Count)
		default:
			stats.Releases.Ongoing += int(row.Count)
		}
	}

	var pipelines []pipelineRow
	if err := db.WithContext(ctx).
		Raw(`select status, count(*) as count
		     from pipeline_runs
		     where created_at >= ? and created_at < ?
		     group by 1`, start, end).
		Scan(&pipelines).Error; err != nil {
		return stats, err
	}
	for _, row := range pipelines {
		stats.Pipelines.Total += int(row.Count)
		switch row.Status {
		case "success":
			stats.Pipelines.Success += int(row.Count)
		case "failed":
			stats.Pipelines.Failed += int(row.Count)
		case "running":
			stats.Pipelines.Running += int(row.Count)
		default:
			stats.Pipelines.Queued += int(row.Count)
		}
	}

	if err := db.WithContext(ctx).
		Raw(`select coalesce(alertname,'') as alertname, coalesce(service,'') as service, count(*) as count
		     from alerts
		     where received_at >= ? and received_at < ?
		     group by 1, 2
		     order by count desc
		     limit 10`, start, end).
		Scan(&stats.Alerts.Top).Error; err != nil {
		return stats, err
	}
	var alertTotal int64
	if err := db.WithContext(ctx).
		Raw(`select count(*) from alerts where received_at >= ? and received_at < ?`, start, end).
		Scan(&alertTotal).Error; err != nil {
		return stats, err
	}
	stats.Alerts.Total = int(alertTotal)
	return stats, nil
}
