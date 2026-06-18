package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/deploy"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/platformerr"
	"gorm.io/gorm"
)

func (s *Store) CreateDeploy(ctx context.Context, input deploy.CreateRecordInput) error {
	record, err := newDeployRecord(input)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Create(&record).Error; err != nil {
		return fmt.Errorf("create deploy record: %w", err)
	}
	return nil
}

func (s *Store) GetDeploy(ctx context.Context, id string) (deploy.Record, error) {
	var record deployRecord
	err := s.db.WithContext(ctx).Where("deploy_id = ?", id).First(&record).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return deploy.Record{}, platformerr.ErrNotFound
		}
		return deploy.Record{}, fmt.Errorf("get deploy record: %w", err)
	}
	item, err := record.toDeploy()
	if err != nil {
		return deploy.Record{}, fmt.Errorf("decode deploy record: %w", err)
	}
	return item, nil
}

func (s *Store) ListDeploys(ctx context.Context, filter deploy.ListFilter) ([]deploy.Record, error) {
	query := s.db.WithContext(ctx).Model(&deployRecord{})
	if filter.ServiceName != "" {
		query = query.Where("service_name = ?", filter.ServiceName)
	}
	if filter.Environment != "" {
		query = query.Where("environment = ?", filter.Environment)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", string(filter.Status))
	}
	if filter.Deployer != "" {
		query = query.Where("deployer = ?", filter.Deployer)
	}
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	var records []deployRecord
	err := query.Order("created_at desc").Limit(filter.Limit).Offset(filter.Offset).Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("list deploy records: %w", err)
	}
	items := make([]deploy.Record, 0, len(records))
	for _, record := range records {
		item, err := record.toDeploy()
		if err != nil {
			return nil, fmt.Errorf("decode deploy record: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Store) UpdateDeploy(ctx context.Context, id string, input deploy.UpdateRecordInput) error {
	updates := map[string]interface{}{"updated_at": time.Now().UTC()}
	if input.Status != "" {
		updates["status"] = string(input.Status)
	}
	if input.ConfirmedBy != "" {
		updates["confirmed_by"] = input.ConfirmedBy
	}
	if input.ConfirmedAt != nil {
		updates["confirmed_at"] = input.ConfirmedAt
	}
	if input.FailedStage != "" {
		updates["failed_stage"] = input.FailedStage
	}
	if input.ErrorMessage != "" {
		updates["error_message"] = input.ErrorMessage
	}
	if input.FinishedAt != nil {
		updates["finished_at"] = input.FinishedAt
	}
	result := s.db.WithContext(ctx).Model(&deployRecord{}).Where("deploy_id = ?", id).Updates(updates)
	return requireRowsAffected(result, "deploy", "update deploy record")
}

func (s *Store) ListActiveLocks(ctx context.Context, serviceName, environment string) ([]deploy.Lock, error) {
	query := s.db.WithContext(ctx).
		Model(&releaseLockRecord{}).
		Where("locked_until > ?", time.Now().UTC())
	if serviceName != "" {
		query = query.Where("service_name = ?", serviceName)
	}
	if environment != "" {
		query = query.Where("environment = ?", environment)
	}
	var records []releaseLockRecord
	if err := query.Order("locked_until asc").Find(&records).Error; err != nil {
		return nil, fmt.Errorf("list active locks: %w", err)
	}
	items := make([]deploy.Lock, 0, len(records))
	for _, record := range records {
		items = append(items, deploy.Lock{
			ServiceName: record.ServiceName,
			Environment: record.Environment,
			ReleaseID:   record.ReleaseID,
			LockedUntil: record.LockedUntil,
			CreatedAt:   record.CreatedAt,
			UpdatedAt:   record.UpdatedAt,
		})
	}
	return items, nil
}
