package pipeline

import (
	"context"

	"github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/idgen"
)

type Store interface {
	CreateStage(ctx context.Context, input CreateStageInput) error
	ListStages(ctx context.Context, filter ListFilter) ([]StageRecord, error)
}

type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

func (s *Service) Create(ctx context.Context, input CreateStageInput) (StageRecord, error) {
	if input.BuildID == "" && input.DeployID == "" {
		return StageRecord{}, bizerr.Param("build_id or deploy_id is required")
	}
	if input.BuildID != "" && input.DeployID != "" {
		return StageRecord{}, bizerr.Param("build_id and deploy_id are mutually exclusive")
	}
	if input.Stage == "" {
		return StageRecord{}, bizerr.Param("stage is required")
	}
	if input.StageOrder == 0 {
		return StageRecord{}, bizerr.Param("stage_order is required")
	}
	if input.Status == "" {
		input.Status = StatusPending
	}
	if input.ID == "" {
		id, err := idgen.NewUUID()
		if err != nil {
			return StageRecord{}, bizerr.InternalWrap("generate stage id failed", err)
		}
		input.ID = id
	}
	input.Detail = SanitizeDetail(input.Detail)
	if err := s.store.CreateStage(ctx, input); err != nil {
		return StageRecord{}, bizerr.InternalWrap("create pipeline stage failed", err)
	}
	items, err := s.List(ctx, ListFilter{BuildID: input.BuildID, DeployID: input.DeployID})
	if err != nil {
		return StageRecord{}, err
	}
	for _, item := range items {
		if item.ID == input.ID {
			return item, nil
		}
	}
	return StageRecord{ID: input.ID}, nil
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]StageRecord, error) {
	if filter.BuildID == "" && filter.DeployID == "" {
		return nil, bizerr.Param("build_id or deploy_id is required")
	}
	if filter.BuildID != "" && filter.DeployID != "" {
		return nil, bizerr.Param("build_id and deploy_id are mutually exclusive")
	}
	items, err := s.store.ListStages(ctx, filter)
	if err != nil {
		return nil, bizerr.InternalWrap("list pipeline stages failed", err)
	}
	if items == nil {
		items = []StageRecord{}
	}
	for i := range items {
		items[i].Detail = SanitizeDetail(items[i].Detail)
	}
	return items, nil
}
