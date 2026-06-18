package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/worryyy/devops-platform/platform/server/internal/build"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/platformerr"
	"gorm.io/gorm"
)

func (s *Store) CreateBuild(ctx context.Context, input build.CreateRecordInput) error {
	record, err := newBuildRecord(input)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return fmt.Errorf("create build record: %w", err)
	}
	return nil
}

func (s *Store) GetBuild(ctx context.Context, id string) (build.Record, error) {
	var record buildRecord
	err := s.db.WithContext(ctx).Where("build_id = ?", id).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return build.Record{}, platformerr.ErrNotFound
		}
		return build.Record{}, fmt.Errorf("get build record: %w", err)
	}
	item, err := record.toBuild()
	if err != nil {
		return build.Record{}, fmt.Errorf("decode build record: %w", err)
	}
	return item, nil
}

func (s *Store) ListBuilds(ctx context.Context, filter build.ListFilter) ([]build.Record, error) {
	query := s.db.WithContext(ctx).Model(&buildRecord{})
	if filter.ServiceName != "" {
		query = query.Where("service_name = ?", filter.ServiceName)
	}
	if filter.Branch != "" {
		query = query.Where("branch = ?", filter.Branch)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", string(filter.Status))
	}
	if filter.Builder != "" {
		query = query.Where("builder = ?", filter.Builder)
	}
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	var records []buildRecord
	err := query.Order("created_at desc").Limit(filter.Limit).Offset(filter.Offset).Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("list build records: %w", err)
	}
	items := make([]build.Record, 0, len(records))
	for _, record := range records {
		item, err := record.toBuild()
		if err != nil {
			return nil, fmt.Errorf("decode build record: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) ListRecentSuccessfulImages(ctx context.Context, serviceName string, limit int) ([]build.RecentImage, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	var records []buildRecord
	err := s.db.WithContext(ctx).
		Where("service_name = ? and status = ? and image_digest is not null", serviceName, string(build.StatusSuccess)).
		Order("finished_at desc nulls last, created_at desc").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("list recent successful images: %w", err)
	}
	items := make([]build.RecentImage, 0, len(records))
	for _, record := range records {
		items = append(items, build.RecentImage{
			BuildID:     record.BuildID,
			ServiceName: record.ServiceName,
			Branch:      record.Branch,
			CommitSHA:   record.CommitSHA,
			ImageRepo:   stringValue(record.ImageRepo),
			ImageTag:    stringValue(record.ImageTag),
			ImageDigest: stringValue(record.ImageDigest),
			Builder:     record.Builder,
			FinishedAt:  record.FinishedAt,
		})
	}
	return items, nil
}
