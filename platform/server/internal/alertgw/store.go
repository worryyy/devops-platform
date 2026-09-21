package alertgw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Store persists alerts in PostgreSQL.
type Store struct{ db *gorm.DB }

func NewStore(db *gorm.DB) *Store { return &Store{db: db} }

// AlertStoreAPI is the store surface the gateway needs; *Store satisfies it
// and tests provide a hermetic implementation.
type AlertStoreAPI interface {
	Upsert(ctx context.Context, alert Alert) error
	List(ctx context.Context, filter AlertFilter) ([]Alert, error)
	Get(ctx context.Context, id int64) (Alert, error)
	Ack(ctx context.Context, id int64, by string, at time.Time) error
	BumpDedup(ctx context.Context, fingerprint string, count int) error
	CountSince(ctx context.Context, alertname, service string, since time.Time) (int64, error)
}

var ErrAlertNotFound = errors.New("alert not found")

// AlertFilter narrows the /api/alerts listing.
type AlertFilter struct {
	Status     string
	Service    string
	Severity   string
	SignalType string
	Limit      int
}

// Upsert merges one Alertmanager fingerprint: a firing notification refreshes
// labels/annotations and reopens a resolved row; resolved stamps ends_at.
func (s *Store) Upsert(ctx context.Context, alert Alert) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "fingerprint"}},
		DoUpdates: clause.Assignments(map[string]any{
			"alertname":   alert.Alertname,
			"service":     alert.Service,
			"severity":    alert.Severity,
			"signal_type": alert.SignalType,
			"labels":      alert.Labels,
			"annotations": alert.Annotations,
			"status":      alert.Status,
			"starts_at":   alert.StartsAt,
			"ends_at":     alert.EndsAt,
			"received_at": alert.ReceivedAt,
		}),
	}).Create(&alert).Error
}

func (s *Store) List(ctx context.Context, filter AlertFilter) ([]Alert, error) {
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 100
	}
	query := s.db.WithContext(ctx).Order("received_at desc").Limit(filter.Limit)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if filter.Service != "" {
		query = query.Where("service = ?", filter.Service)
	}
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	if filter.SignalType != "" {
		query = query.Where("signal_type = ?", filter.SignalType)
	}
	var out []Alert
	if err := query.Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Alert, error) {
	var alert Alert
	if err := s.db.WithContext(ctx).First(&alert, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Alert{}, ErrAlertNotFound
		}
		return Alert{}, err
	}
	return alert, nil
}

// Ack writes acked_by/acked_at into the labels JSONB.
func (s *Store) Ack(ctx context.Context, id int64, by string, at time.Time) error {
	alert, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if alert.Labels.Set == nil {
		alert.Labels.Set = map[string]string{}
	}
	alert.Labels.Set["acked_by"] = by
	alert.Labels.Set["acked_at"] = at.UTC().Format(time.RFC3339)
	return s.db.WithContext(ctx).Model(&Alert{}).Where("id = ?", id).
		Update("labels", alert.Labels).Error
}

// BumpDedup records how many repeats the gateway suppressed for a
// fingerprint inside the current dedup window.
func (s *Store) BumpDedup(ctx context.Context, fingerprint string, count int) error {
	alert, err := s.GetByFingerprint(ctx, fingerprint)
	if err != nil {
		return err
	}
	if alert.Labels.Set == nil {
		alert.Labels.Set = map[string]string{}
	}
	alert.Labels.Set["dedup_count"] = fmt.Sprintf("%d", count)
	return s.db.WithContext(ctx).Model(&Alert{}).Where("id = ?", alert.ID).
		Update("labels", alert.Labels).Error
}

// GetByFingerprint loads one alert row by its Alertmanager fingerprint.
func (s *Store) GetByFingerprint(ctx context.Context, fingerprint string) (Alert, error) {
	var alert Alert
	if err := s.db.WithContext(ctx).Where("fingerprint = ?", fingerprint).First(&alert).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Alert{}, ErrAlertNotFound
		}
		return Alert{}, err
	}
	return alert, nil
}

// CountSince supports the weekly report's alert Top-N (and tests).
func (s *Store) CountSince(ctx context.Context, alertname, service string, since time.Time) (int64, error) {
	query := s.db.WithContext(ctx).Model(&Alert{}).Where("received_at >= ?", since)
	if alertname != "" {
		query = query.Where("alertname = ?", alertname)
	}
	if service != "" {
		query = query.Where("service = ?", service)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

// Payload is the Alertmanager v2 webhook body.
type Payload struct {
	Version  string         `json:"version"`
	Status   string         `json:"status"`
	Receiver string         `json:"receiver"`
	Alerts   []PayloadAlert `json:"alerts"`
}

// PayloadAlert is one entry of the v2 payload's alerts array.
type PayloadAlert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

// ToAlert maps a payload entry onto the storage model. Missing
// ends_at (zero time) stays NULL so firing rows never look resolved.
func (a PayloadAlert) ToAlert(receivedAt time.Time) Alert {
	alert := Alert{
		Fingerprint: a.Fingerprint,
		Alertname:   a.Labels["alertname"],
		Service:     a.Labels["service"],
		Severity:    a.Labels["severity"],
		SignalType:  a.Labels["signal_type"],
		Labels:      Labels{Set: a.Labels},
		Annotations: Annotations{Set: a.Annotations},
		Status:      statusOf(a),
		ReceivedAt:  receivedAt,
	}
	if !a.StartsAt.IsZero() {
		t := a.StartsAt
		alert.StartsAt = &t
	}
	if !a.EndsAt.IsZero() {
		t := a.EndsAt
		alert.EndsAt = &t
	}
	return alert
}

func statusOf(a PayloadAlert) string {
	if a.Status == "resolved" {
		return "resolved"
	}
	return "firing"
}

// Summary returns the human one-liner for cards: annotation summary first,
// then description, then the alertname.
func (a Alert) Summary() string {
	if v := strings.TrimSpace(a.Annotations.Set["summary"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.Annotations.Set["description"]); v != "" {
		return v
	}
	return a.Alertname
}

// DecodePayload is a helper for tests and debugging.
func DecodePayload(raw []byte) (Payload, error) {
	var payload Payload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Payload{}, err
	}
	return payload, nil
}
