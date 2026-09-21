package delivery

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Stage is one pipeline stage entry pushed by the Jenkinsfile Notify hook.
type Stage struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // success | failed | skipped | running
	DurationMs int64  `json:"durationMs"`
	At         string `json:"at,omitempty"`
}

type PipelineRun struct {
	ID             int64      `gorm:"primaryKey" json:"id"`
	Service        string     `gorm:"index:idx_pipeline_service;size:64" json:"service"`
	JenkinsBuild   int64      `json:"jenkinsBuild"`
	Status         string     `gorm:"default:queued" json:"status"`
	Stages         JSONStages `json:"stages"`
	TriggeredBy    string     `gorm:"size:64" json:"triggeredBy"`
	GitRevision    string     `json:"gitRevision"`
	ConfigRevision string     `json:"configRevision"`
	ImageDigest    string     `json:"imageDigest"`
	RevertPRURL    string     `json:"revertPrUrl"`
	StartedAt      *time.Time `json:"startedAt"`
	FinishedAt     *time.Time `json:"finishedAt"`
	CreatedAt      time.Time  `json:"createdAt"`
}

func (PipelineRun) TableName() string { return "pipeline_runs" }

// JSONStages keeps the stage timeline as JSONB with idempotent merge-by-name.
type JSONStages struct{ Stages []Stage }

// Value implements driver.Valuer.
func (s JSONStages) Value() (driver.Value, error) {
	if s.Stages == nil {
		return "[]", nil
	}
	raw, err := json.Marshal(s.Stages)
	if err != nil {
		return nil, err
	}
	return string(raw), nil
}

// Scan implements sql.Scanner.
func (s *JSONStages) Scan(value any) error {
	if value == nil {
		s.Stages = nil
		return nil
	}
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return errors.New("jsonstages: unsupported scan source")
	}
	return json.Unmarshal(raw, &s.Stages)
}

// MergeStages overlays incoming stages onto existing ones by name (last
// write wins per stage, order preserved from first appearance).
func MergeStages(existing, incoming []Stage) []Stage {
	index := map[string]int{}
	for i, st := range existing {
		index[st.Name] = i
	}
	merged := append([]Stage(nil), existing...)
	for _, st := range incoming {
		if idx, ok := index[st.Name]; ok {
			merged[idx] = st
		} else {
			index[st.Name] = len(merged)
			merged = append(merged, st)
		}
	}
	return merged
}

type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// PipelineStoreAPI is the store surface handlers use; *Store satisfies it
// and tests provide a hermetic implementation.
type PipelineStoreAPI interface {
	List(ctx context.Context, filter ListFilter) ([]PipelineRun, error)
	Get(ctx context.Context, id int64) (PipelineRun, error)
	Create(ctx context.Context, run *PipelineRun) error
	UpdateBuild(ctx context.Context, id, buildID int64) error
	ApplyWebhook(ctx context.Context, buildID int64, service string, stages []Stage, runStatus, gitRevision, digest string) error
	SetRevertPR(ctx context.Context, id int64, url string) error
	SetStatus(ctx context.Context, id int64, status string) error
	MarkTriggerFailed(ctx context.Context, id int64, reason string) error
}

var ErrRunNotFound = errors.New("pipeline run not found")

type ListFilter struct {
	Service string
	Status  string
	Limit   int
}

func (s *Store) List(ctx context.Context, filter ListFilter) ([]PipelineRun, error) {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	db := s.db.WithContext(ctx).Model(&PipelineRun{})
	if filter.Service != "" {
		db = db.Where("service = ?", filter.Service)
	}
	if filter.Status != "" {
		db = db.Where("status = ?", filter.Status)
	}
	var runs []PipelineRun
	if err := db.Order("created_at desc").Limit(filter.Limit).Find(&runs).Error; err != nil {
		return nil, fmt.Errorf("list pipeline runs: %w", err)
	}
	return runs, nil
}

func (s *Store) Get(ctx context.Context, id int64) (PipelineRun, error) {
	var run PipelineRun
	err := s.db.WithContext(ctx).First(&run, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return PipelineRun{}, ErrRunNotFound
		}
		return PipelineRun{}, fmt.Errorf("get pipeline run: %w", err)
	}
	return run, nil
}

func (s *Store) Create(ctx context.Context, run *PipelineRun) error {
	return s.db.WithContext(ctx).Create(run).Error
}

func (s *Store) UpdateBuild(ctx context.Context, id, buildID int64) error {
	return s.db.WithContext(ctx).Model(&PipelineRun{}).Where("id = ?", id).
		Update("jenkins_build", buildID).Error
}

// ApplyStage merges one webhook stage event into a run, mapping the Jenkins
// build lifecycle to run status transitions.
func (s *Store) ApplyStage(ctx context.Context, buildID int64, stage Stage, runStatus string) error {
	return s.ApplyWebhook(ctx, buildID, "", []Stage{stage}, runStatus, "", "")
}

// ApplyWebhook merges a batch of stage events (idempotent by name), applies
// the run-level status transition, and records digest/revisions when present.
// A Jenkins build batches several services; service disambiguates the run.
func (s *Store) ApplyWebhook(ctx context.Context, buildID int64, service string, stages []Stage, runStatus, gitRevision, digest string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Where("jenkins_build = ?", buildID)
		if service != "" {
			query = query.Where("service = ?", service)
		}
		var run PipelineRun
		err := query.First(&run).Error
		if errors.Is(err, gorm.ErrRecordNotFound) && service != "" {
			// Self-healing claim: queue-item→build-number resolution can miss
			// (queue items are GC'd fast); the job is serial per service, so
			// the newest still-active run for this service is this build.
			err = tx.Where("service = ? AND status IN ('queued','running')", service).
				Order("created_at desc").First(&run).Error
			if err == nil {
				if saveErr := tx.Model(&PipelineRun{}).Where("id = ?", run.ID).
					Update("jenkins_build", buildID).Error; saveErr != nil {
					return saveErr
				}
				run.JenkinsBuild = buildID
			}
		}
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRunNotFound
			}
			return err
		}
		merged := MergeStages(run.Stages.Stages, stages)
		updates := map[string]any{
			"stages": JSONStages{Stages: merged},
		}
		if runStatus != "" && runStatus != run.Status {
			updates["status"] = runStatus
			switch runStatus {
			case "running":
				now := time.Now()
				updates["started_at"] = &now
			case "success", "failed":
				now := time.Now()
				updates["finished_at"] = &now
			}
		}
		if gitRevision != "" {
			updates["git_revision"] = gitRevision
		}
		if digest != "" {
			updates["image_digest"] = digest
		}
		return tx.Model(&PipelineRun{}).Where("id = ?", run.ID).Updates(updates).Error
	})
}

// SetStatus transitions run status (used by the Jenkins backfill refresh).
func (s *Store) SetStatus(ctx context.Context, id int64, status string) error {
	updates := map[string]any{"status": status}
	switch status {
	case "running":
		now := time.Now()
		updates["started_at"] = &now
	case "success", "failed":
		now := time.Now()
		updates["finished_at"] = &now
	}
	return s.db.WithContext(ctx).Model(&PipelineRun{}).Where("id = ?", id).Updates(updates).Error
}

// MarkTriggerFailed records a failed Jenkins enqueue (network/auth).
func (s *Store) MarkTriggerFailed(ctx context.Context, id int64, reason string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&PipelineRun{}).Where("id = ?", id).Updates(map[string]any{
		"status":          "failed",
		"finished_at":     &now,
		"config_revision": reason,
	}).Error
}

// AttachReleaseByBuild stores digest/revision reported by the pipeline.
func (s *Store) AttachReleaseByBuild(ctx context.Context, buildID int64, gitRevision, digest string) error {
	updates := map[string]any{}
	if gitRevision != "" {
		updates["git_revision"] = gitRevision
	}
	if digest != "" {
		updates["image_digest"] = digest
	}
	if len(updates) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&PipelineRun{}).Where("jenkins_build = ?", buildID).Updates(updates).Error
}

// AttachRelease stores the delivery outcome (digest/revisions) reported by
// the pipeline.
func (s *Store) AttachRelease(ctx context.Context, id int64, gitRevision, configRevision, digest string) error {
	return s.db.WithContext(ctx).Model(&PipelineRun{}).Where("id = ?", id).Updates(map[string]any{
		"git_revision":    gitRevision,
		"config_revision": configRevision,
		"image_digest":    digest,
	}).Error
}

func (s *Store) SetRevertPR(ctx context.Context, id int64, url string) error {
	res := s.db.WithContext(ctx).Model(&PipelineRun{}).Where("id = ?", id).
		Update("revert_pr_url", url)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrRunNotFound
	}
	return nil
}

// UpsertStagesByBuild is used by tests and backfills.
func (s *Store) UpsertStagesByBuild(ctx context.Context, run *PipelineRun) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"status", "stages", "finished_at"}),
	}).Create(run).Error
}
