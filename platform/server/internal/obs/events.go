package obs

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// Valid event kinds (platform.events.kind LowCardinality column): a typo here
// would silently fork the cardinality, so validate before insert.
var eventKinds = map[string]bool{
	"alert": true, "release": true, "inspection": true, "ai_verdict": true, "report": true,
}

// RecordEvent is the CH double-write sink. Payload is a caller-owned JSON
// document (schema per kind, see monitoring/pipeline/clickhouse/README.md).
// Failures are the caller's business: alertgw/delivery log-and-continue —
// the PG write is the source of truth, CH only feeds analytics/周报.
func (c *Client) RecordEvent(ctx context.Context, kind, source string, payload []byte) error {
	if !eventKinds[kind] {
		return fmt.Errorf("obs: unknown event kind %q", kind)
	}
	if len(payload) > 1<<20 { // 1MiB cap: events are metadata, not blobs
		return fmt.Errorf("obs: event payload too large (%d bytes)", len(payload))
	}
	batch, err := c.conn.PrepareBatch(ctx, "INSERT INTO events")
	if err != nil {
		return fmt.Errorf("obs: prepare event insert: %w", err)
	}
	if err := batch.AppendStruct(&eventRow{
		Kind:    kind,
		Source:  source,
		Payload: string(payload),
		TS:      time.Now().UTC(),
	}); err != nil {
		return fmt.Errorf("obs: append event: %w", err)
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("obs: send event: %w", err)
	}
	c.logger.Debug("obs event recorded", "kind", kind, "source", source, "bytes", len(payload))
	return nil
}

type eventRow struct {
	Kind    string    `ch:"kind"`
	Source  string    `ch:"source"`
	Payload string    `ch:"payload"`
	TS      time.Time `ch:"ts"`
}

// ListEvents reads events back (weekly report + P5 复盘页数据源).
func (c *Client) ListEvents(ctx context.Context, kind string, start, end time.Time, limit int) ([]EventRow, error) {
	if kind != "" && !eventKinds[kind] {
		return nil, fmt.Errorf("obs: unknown event kind %q", kind)
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	where, params := "1=1", clickhouse.Parameters{}
	if kind != "" {
		where += " AND kind = {kind: String}"
		params["kind"] = kind
	}
	if !start.IsZero() {
		where += " AND ts >= {start: DateTime64(3)}"
		params["start"] = epochMillis(start)
	}
	if !end.IsZero() {
		where += " AND ts < {end: DateTime64(3)}"
		params["end"] = epochMillis(end)
	}
	sql := fmt.Sprintf(
		"SELECT kind, source, payload, ts FROM events WHERE %s ORDER BY ts DESC LIMIT %d", where, limit)
	rows, err := c.conn.Query(clickhouse.Context(ctx, clickhouse.WithParameters(params)), sql)
	if err != nil {
		return nil, fmt.Errorf("obs: list events: %w", err)
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		var r EventRow
		if err := rows.Scan(&r.Kind, &r.Source, &r.Payload, &r.Ts); err != nil {
			return nil, fmt.Errorf("obs: scan event row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EventRow is one row out of platform.events.
type EventRow struct {
	Kind    string    `json:"kind"`
	Source  string    `json:"source"`
	Payload string    `json:"payload"` // JSON 文本
	Ts      time.Time `json:"ts"`
}
