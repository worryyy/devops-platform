package alertgw

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Alert mirrors the alerts table: one row per Alertmanager fingerprint,
// upserted on every firing/resolved notification.
type Alert struct {
	ID          int64  `gorm:"primaryKey" json:"id"`
	Fingerprint string `gorm:"uniqueIndex;size:64" json:"fingerprint"`
	Alertname   string `gorm:"size:128" json:"alertname"`
	Service     string `gorm:"size:64" json:"service"`
	Severity    string `gorm:"size:32" json:"severity"`
	SignalType  string `gorm:"column:signal_type;size:32" json:"signalType"`

	Labels      Labels      `json:"labels"`
	Annotations Annotations `json:"annotations"`

	Status     string     `gorm:"default:firing" json:"status"`
	StartsAt   *time.Time `json:"startsAt"`
	EndsAt     *time.Time `json:"endsAt"`
	ReceivedAt time.Time  `json:"receivedAt"`
}

func (Alert) TableName() string { return "alerts" }

// Labels keeps the full Alertmanager label set as JSONB; ack state
// (acked_by/acked_at) is written back into it too.
type Labels struct {
	Set map[string]string `json:"set"`
}

func (l Labels) Value() (driver.Value, error) { return marshalJSONB(l.Set) }

func (l *Labels) Scan(value any) error {
	if value == nil {
		l.Set = map[string]string{}
		return nil
	}
	return unmarshalJSONB(value, &l.Set)
}

// Annotations mirrors Labels for the annotation set.
type Annotations struct {
	Set map[string]string `json:"set"`
}

func (a Annotations) Value() (driver.Value, error) { return marshalJSONB(a.Set) }

func (a *Annotations) Scan(value any) error {
	if value == nil {
		a.Set = map[string]string{}
		return nil
	}
	return unmarshalJSONB(value, &a.Set)
}

func marshalJSONB(v any) (driver.Value, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonb: %w", err)
	}
	return string(raw), nil
}

func unmarshalJSONB(value any, dst any) error {
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return errors.New("jsonb: unsupported scan source")
	}
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("unmarshal jsonb: %w", err)
	}
	return nil
}
