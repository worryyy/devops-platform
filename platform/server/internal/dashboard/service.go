package dashboard

import (
	"context"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/build"
	"github.com/worryyy/devops-platform/platform/server/internal/deploy"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"
)

type Store interface {
	DashboardSummary(ctx context.Context, filter SummaryFilter) (Summary, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Summary(ctx context.Context, filter SummaryFilter) (Summary, error) {
	if filter.Range == "" {
		filter.Range = "today"
	}
	summary, err := s.store.DashboardSummary(ctx, filter)
	if err != nil {
		return Summary{}, bizerr.InternalWrap("get dashboard summary failed", err)
	}
	if summary.RecentBuilds == nil {
		summary.RecentBuilds = []build.Record{}
	}
	if summary.RecentDeploys == nil {
		summary.RecentDeploys = []deploy.Record{}
	}
	if summary.ActiveLocks == nil {
		summary.ActiveLocks = []deploy.Lock{}
	}
	summary.Range = filter.Range
	summary.GeneratedAt = time.Now().UTC()
	return summary, nil
}
