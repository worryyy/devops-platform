package store

import (
	"context"
	"fmt"

	"github.com/worryyy/devops-platform/platform/server/internal/pipeline"
)

func (s *Store) CreateStage(ctx context.Context, input pipeline.CreateStageInput) error {
	record, err := newPipelineStageRecord(input)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return fmt.Errorf("create pipeline stage: %w", err)
	}
	return nil
}

func (s *Store) ListStages(ctx context.Context, filter pipeline.ListFilter) ([]pipeline.StageRecord, error) {
	query := s.db.WithContext(ctx).Model(&pipelineStageRecord{})
	if filter.BuildID != "" {
		query = query.Where("build_id = ?", filter.BuildID)
	}
	if filter.DeployID != "" {
		query = query.Where("deploy_id = ?", filter.DeployID)
	}
	var records []pipelineStageRecord
	if err := query.Order("stage_order asc, created_at asc").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list pipeline stages: %w", err)
	}
	items := make([]pipeline.StageRecord, 0, len(records))
	for _, record := range records {
		item, err := record.toPipelineStage()
		if err != nil {
			return nil, fmt.Errorf("decode pipeline stage: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}
