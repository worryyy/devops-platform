package catalog

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
)

// JSONB maps a Go value to a PostgreSQL jsonb column via GORM's
// Valuer/Scanner hooks: writes marshal the typed value, reads unmarshal
// into it.
type JSONB[T any] struct {
	Data T
}

func (j JSONB[T]) Value() (driver.Value, error) {
	if any(j.Data) == nil {
		return nil, nil
	}
	raw, err := json.Marshal(j.Data)
	if err != nil {
		return nil, fmt.Errorf("marshal jsonb: %w", err)
	}
	return string(raw), nil
}

func (j *JSONB[T]) Scan(value any) error {
	if value == nil {
		return nil
	}
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case string:
		raw = []byte(v)
	default:
		return errors.New("jsonb: unsupported scan source")
	}
	if err := json.Unmarshal(raw, &j.Data); err != nil {
		return fmt.Errorf("unmarshal jsonb: %w", err)
	}
	return nil
}
