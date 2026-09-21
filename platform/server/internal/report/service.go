package report

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"github.com/worryyy/devops-platform/platform/server/internal/config"
)

// Service owns weekly report execution: the cron schedule, report_runs
// bookkeeping and the runner Job it spawns.
type Service struct {
	store    ReportStoreAPI
	launcher JobLauncher
	cfg      config.Config
	logger   *slog.Logger
	now      func() time.Time
	loc      *time.Location
	cron     *cron.Cron
}

func NewService(store ReportStoreAPI, launcher JobLauncher, cfg config.Config, logger *slog.Logger) *Service {
	loc := time.Local
	if tz, err := time.LoadLocation(cfg.ReportTimezone); err == nil {
		loc = tz
	} else {
		logger.Warn("report timezone invalid, falling back to local", "timezone", cfg.ReportTimezone, "error", err)
	}
	return &Service{
		store:    store,
		launcher: launcher,
		cfg:      cfg,
		logger:   logger,
		now:      time.Now,
		loc:      loc,
	}
}

// Start begins the cron schedule (Monday 09:00 by default) in the
// configured timezone.
func (s *Service) Start() error {
	if s.cfg.ReportSchedule == "" {
		return nil
	}
	c := cron.New(cron.WithLocation(s.loc))
	if _, err := c.AddFunc(s.cfg.ReportSchedule, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if _, err := s.RunWeekly(ctx, s.now().In(s.loc)); err != nil {
			s.logger.Error("scheduled weekly report failed", "error", err)
		}
	}); err != nil {
		return fmt.Errorf("parse report schedule %q: %w", s.cfg.ReportSchedule, err)
	}
	c.Start()
	s.cron = c
	s.logger.Info("weekly report cron started", "schedule", s.cfg.ReportSchedule, "timezone", s.loc.String())
	return nil
}

// Stop halts the cron scheduler.
func (s *Service) Stop() {
	if s.cron != nil {
		s.cron.Stop()
	}
}

// WeeklyWindow returns the previous ISO week (Monday 00:00 to next Monday
// 00:00) relative to `at`, plus its period label (2026-W38).
func WeeklyWindow(at time.Time, loc *time.Location) (period string, start, end time.Time) {
	local := at.In(loc)
	weekday := int(local.Weekday()) // Sunday=0
	if weekday == 0 {
		weekday = 7
	}
	thisMonday := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc).
		AddDate(0, 0, -(weekday - 1))
	start = thisMonday.AddDate(0, 0, -7)
	end = thisMonday
	isoYear, isoWeek := start.ISOWeek()
	period = fmt.Sprintf("%d-W%02d", isoYear, isoWeek)
	return period, start, end
}

// RunWeekly records a report_run and launches the runner Job for the week
// before `at`.
func (s *Service) RunWeekly(ctx context.Context, at time.Time) (*ReportRun, error) {
	period, start, end := WeeklyWindow(at, s.loc)
	return s.launchRun(ctx, "weekly", period, start, end)
}

// RunForPeriod re-runs an explicit ISO period label (manual trigger).
func (s *Service) RunForPeriod(ctx context.Context, period string) (*ReportRun, error) {
	var year, week int
	if _, err := fmt.Sscanf(period, "%d-W%d", &year, &week); err != nil {
		return nil, fmt.Errorf("period must look like 2026-W38")
	}
	// January 4 is always in ISO week 1.
	jan4 := time.Date(year, 1, 4, 0, 0, 0, 0, s.loc)
	_, w1 := jan4.ISOWeek()
	start := jan4.AddDate(0, 0, (week-w1)*7)
	for {
		_, w := start.ISOWeek()
		if w == week {
			break
		}
		start = start.AddDate(0, 0, 1)
	}
	weekday := int(start.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, s.loc).AddDate(0, 0, -(weekday - 1))
	end := start.AddDate(0, 0, 7)
	return s.launchRun(ctx, "weekly", period, start, end)
}

func (s *Service) launchRun(ctx context.Context, kind, period string, start, end time.Time) (*ReportRun, error) {
	run := &ReportRun{Kind: kind, Period: period, Status: "running"}
	if err := s.store.Create(ctx, run); err != nil {
		return nil, fmt.Errorf("record report run: %w", err)
	}
	env := map[string]string{
		"WEEK_START":              start.Format(time.RFC3339),
		"WEEK_END":                end.Format(time.RFC3339),
		"PROMETHEUS_URL":          s.cfg.PrometheusURL,
		"PLATFORM_API_URL":        s.cfg.PlatformInternalURL(),
		"PLATFORM_WEBHOOK_SECRET": s.cfg.WebhookSecret,
		"FEISHU_APP_ID":           s.cfg.FeishuAppID,
		"FEISHU_APP_SECRET":       s.cfg.FeishuAppSecret,
		"FEISHU_CHAT_ID":          s.cfg.FeishuChatID,
		"FEISHU_WEBHOOK_URL":      s.cfg.FeishuWebhookURL,
		"FEISHU_WEBHOOK_SECRET":   s.cfg.FeishuWebhookSecret,
		"PLATFORM_PUBLIC_URL":     s.cfg.PlatformPublicURL,
		"MINIO_ENDPOINT":          s.cfg.MinioEndpoint,
		"MINIO_ACCESS_KEY":        s.cfg.MinioAccessKey,
		"MINIO_SECRET_KEY":        s.cfg.MinioSecretKey,
		"MINIO_BUCKET":            s.cfg.ReportBucket,
		"MINIO_SECURE":            "false",
	}
	spec := JobSpec{
		RunID:     run.ID,
		Kind:      kind,
		Period:    period,
		Start:     start,
		End:       end,
		Image:     s.cfg.ReportRunnerImage,
		Env:       env,
		Namespace: "platform",
	}
	jobName, err := s.launcher.Launch(ctx, spec)
	if err != nil {
		_ = s.store.SetResult(ctx, run.ID, "failed", "", "launch job: "+err.Error())
		run.Status = "failed"
		run.Error = "launch job: " + err.Error()
		return run, nil
	}
	s.logger.Info("report runner launched", "period", period, "run_id", run.ID, "job", jobName)
	return run, nil
}
