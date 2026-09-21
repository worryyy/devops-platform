package report

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

// ReportRun tracks one weekly report execution (cron or manual).
type ReportRun struct {
	ID         int64     `gorm:"primaryKey" json:"id"`
	Kind       string    `gorm:"size:16" json:"kind"` // weekly
	Period     string    `gorm:"size:16;index" json:"period"`
	ObjectPath string    `json:"objectPath"` // MinIO key, e.g. weekly/2026-W38/report.html
	Status     string    `gorm:"default:running" json:"status"`
	Error      string    `json:"error"`
	CreatedAt  time.Time `json:"createdAt"`
}

func (ReportRun) TableName() string { return "report_runs" }

// Store persists report_runs rows.
type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// ReportStoreAPI is the surface handlers use; *Store satisfies it and tests
// substitute a hermetic implementation.
type ReportStoreAPI interface {
	Create(ctx context.Context, run *ReportRun) error
	SetResult(ctx context.Context, id int64, status, objectPath, errMsg string) error
	Get(ctx context.Context, id int64) (ReportRun, error)
	List(ctx context.Context, limit int) ([]ReportRun, error)
}

var ErrRunNotFound = errors.New("report run not found")

func (s *Store) Create(ctx context.Context, run *ReportRun) error {
	return s.db.WithContext(ctx).Create(run).Error
}

func (s *Store) SetResult(ctx context.Context, id int64, status, objectPath, errMsg string) error {
	result := s.db.WithContext(ctx).Model(&ReportRun{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "object_path": objectPath, "error": errMsg})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrRunNotFound
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id int64) (ReportRun, error) {
	var run ReportRun
	if err := s.db.WithContext(ctx).First(&run, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ReportRun{}, ErrRunNotFound
		}
		return ReportRun{}, err
	}
	return run, nil
}

func (s *Store) List(ctx context.Context, limit int) ([]ReportRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var runs []ReportRun
	if err := s.db.WithContext(ctx).Order("created_at desc").Limit(limit).Find(&runs).Error; err != nil {
		return nil, err
	}
	return runs, nil
}
