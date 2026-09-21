package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ServiceRow is the services-table mapping of the YAML Service. SLI and
// environments stay JSONB exactly as the YAML declares them; P7 normalizes
// environments if ever needed.
type ServiceRow struct {
	ID           int64     `gorm:"primaryKey" json:"id"`
	Name         string    `gorm:"uniqueIndex;size:64" json:"name"`
	DisplayName  string    `json:"display_name"`
	Owner        string    `json:"owner"`
	Kind         string    `json:"kind"`
	SLI          JSONB[SLIPolicy]
	Environments JSONB[[]Environment]
	Source       string    `gorm:"default:yaml" json:"source"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (ServiceRow) TableName() string { return "services" }

// API is the shape returned by /api/services — environments metadata only in
// list view, full payload in detail view.
type ServiceSummary struct {
	Name             string    `json:"name"`
	DisplayName      string    `json:"displayName"`
	Owner            string    `json:"owner"`
	Kind             string    `json:"kind"`
	EnvironmentCount int       `json:"environmentCount"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context, query string) ([]ServiceSummary, error) {
	db := s.db.WithContext(ctx).Model(&ServiceRow{})
	if query != "" {
		like := "%" + query + "%"
		db = db.Where("name ILIKE ? OR display_name ILIKE ? OR owner ILIKE ?", like, like, like)
	}
	var rows []ServiceRow
	if err := db.Order("name").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	summaries := make([]ServiceSummary, 0, len(rows))
	for _, row := range rows {
		summaries = append(summaries, ServiceSummary{
			Name:             row.Name,
			DisplayName:      row.DisplayName,
			Owner:            row.Owner,
			Kind:             row.Kind,
			EnvironmentCount: len(row.Environments.Data),
			UpdatedAt:        row.UpdatedAt,
		})
	}
	return summaries, nil
}

var ErrNotFound = errors.New("service not found")

func (s *Store) Get(ctx context.Context, name string) (Service, error) {
	var row ServiceRow
	err := s.db.WithContext(ctx).Where("name = ?", name).First(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Service{}, ErrNotFound
		}
		return Service{}, fmt.Errorf("get service: %w", err)
	}
	return row.toService(), nil
}

// Update rewrites mutable metadata fields (display name, owner, SLI) for a
// service previously imported from YAML; name/environments stay managed by
// the import path.
func (s *Store) Update(ctx context.Context, name string, displayName, owner *string, sli *SLIPolicy) (Service, error) {
	updates := map[string]any{}
	if displayName != nil {
		updates["display_name"] = *displayName
	}
	if owner != nil {
		updates["owner"] = *owner
	}
	if sli != nil {
		raw, err := json.Marshal(*sli)
		if err != nil {
			return Service{}, fmt.Errorf("marshal sli: %w", err)
		}
		updates["sli"] = raw
	}
	if len(updates) == 0 {
		return s.Get(ctx, name)
	}
	updates["updated_at"] = time.Now()

	result := s.db.WithContext(ctx).Model(&ServiceRow{}).Where("name = ?", name).Updates(updates)
	if result.Error != nil {
		return Service{}, fmt.Errorf("update service: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return Service{}, ErrNotFound
	}
	return s.Get(ctx, name)
}

// ImportFromYAML upserts every service in the catalog file: yaml-sourced rows
// are replaced wholesale, DB-edited metadata (source=db edits) is preserved
// for fields the import does not own. Returns number of applied services.
func (s *Store) ImportFromYAML(ctx context.Context, path string) (int, error) {
	cat, err := Load(path)
	if err != nil {
		return 0, err
	}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, svc := range cat.Services {
			row := ServiceRow{
				Name:         svc.Name,
				DisplayName:  svc.DisplayName,
				Owner:        svc.Owner,
				Kind:         svc.Kind,
				SLI:          JSONB[SLIPolicy]{Data: svc.SLI},
				Environments: JSONB[[]Environment]{Data: svc.Environments},
				Source:       "yaml",
			}
			if err := tx.Clauses(clause.OnConflict{
				Columns: []clause.Column{{Name: "name"}},
				DoUpdates: clause.AssignmentColumns([]string{
					"display_name", "owner", "kind", "sli", "environments", "source", "updated_at",
				}),
			}).Create(&row).Error; err != nil {
				return fmt.Errorf("upsert service %s: %w", svc.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return len(cat.Services), nil
}

func (r ServiceRow) toService() Service {
	return Service{
		Name:         r.Name,
		DisplayName:  r.DisplayName,
		Owner:        r.Owner,
		Kind:         r.Kind,
		SLI:          r.SLI.Data,
		Environments: r.Environments.Data,
	}
}
