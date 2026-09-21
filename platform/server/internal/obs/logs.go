package obs

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// LogsFilter is the /logs query surface. Validate() is the injection guard:
// every field that reaches SQL as a literal is checked against a whitelist or
// a strict charset; values used in LIKE/等值 comparisons are bound as
// parameters (`@name`), never string-concatenated.
type LogsFilter struct {
	Service string
	Level   string
	Query   string // msg 关键字（子串匹配）
	TraceID string
	Start   time.Time
	End     time.Time
	Limit   int
}

var (
	serviceRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,62}$`)
	traceRe   = regexp.MustCompile(`^[0-9a-fA-F-]{1,64}$`)
	levels    = map[string]bool{"DEBUG": true, "INFO": true, "WARN": true, "WARNING": true, "ERROR": true, "FATAL": true}
)

const (
	defaultLimit = 200
	maxLimit     = 500
	maxQueryLen  = 256
)

// Validate normalizes and rejects bad input; the returned filter is safe to
// feed buildLogsWhere.
func (f LogsFilter) Validate() (LogsFilter, error) {
	if f.Service != "" && !serviceRe.MatchString(f.Service) {
		return f, fmt.Errorf("invalid service %q", f.Service)
	}
	if f.Level != "" {
		if f.Level == "WARNING" {
			f.Level = "WARN"
		}
		if !levels[f.Level] {
			return f, fmt.Errorf("invalid level %q", f.Level)
		}
	}
	if len(f.Query) > maxQueryLen {
		return f, fmt.Errorf("query too long (max %d)", maxQueryLen)
	}
	if f.TraceID != "" && !traceRe.MatchString(f.TraceID) {
		return f, fmt.Errorf("invalid trace_id %q", f.TraceID)
	}
	if !f.Start.IsZero() && !f.End.IsZero() && f.End.Before(f.Start) {
		return f, fmt.Errorf("end before start")
	}
	if f.Limit <= 0 {
		f.Limit = defaultLimit
	}
	if f.Limit > maxLimit {
		f.Limit = maxLimit
	}
	return f, nil
}

// buildLogsWhere emits the shared WHERE clause with clickhouse-go v2 native
// parameters (`{name: Type}` placeholders + clickhouse.Parameters map — the
// `@name` form is the v1/go-sql-driver dialect and errors out here). Only
// whitelisted fragments are concatenated; every value is server-side bound.
func buildLogsWhere(f LogsFilter) (string, clickhouse.Parameters) {
	where := "1=1"
	params := clickhouse.Parameters{}
	if f.Service != "" {
		where += " AND service = {service: String}"
		params["service"] = f.Service
	}
	if f.Level != "" {
		where += " AND level = {level: String}"
		params["level"] = f.Level
	}
	if f.Query != "" {
		// 子串匹配（大小写不敏感），值经参数绑定，% 不转义放大不了语义
		where += " AND positionCaseInsensitive(msg, {q: String}) > 0"
		params["q"] = f.Query
	}
	if f.TraceID != "" {
		where += " AND trace_id = {trace_id: String}"
		params["trace_id"] = f.TraceID
	}
	if !f.Start.IsZero() {
		where += " AND ts >= {start: DateTime64(3)}"
		params["start"] = epochMillis(f.Start)
	}
	if !f.End.IsZero() {
		where += " AND ts < {end: DateTime64(3)}"
		params["end"] = epochMillis(f.End)
	}
	return where, params
}

// epochMillis renders t as "sec.milli" — server-side DateTime64 param parsing
// reads it as a unix epoch, immune to session-timezone drift.
func epochMillis(t time.Time) string {
	return fmt.Sprintf("%d.%03d", t.Unix(), int64(t.Nanosecond())/1e6)
}

// LogRow is one raw log line for the result table.
type LogRow struct {
	Service string    `json:"service"`
	Ts      time.Time `json:"ts"`
	Level   string    `json:"level"`
	Route   string    `json:"route"`
	TraceID string    `json:"trace_id"`
	Pod     string    `json:"pod"`
	Msg     string    `json:"msg"`
}

const logColumns = "service, ts, level, route, trace_id, pod, msg"

// SearchLogs returns newest-first raw logs.
func (c *Client) SearchLogs(ctx context.Context, f LogsFilter) ([]LogRow, error) {
	f, err := f.Validate()
	if err != nil {
		return nil, err
	}
	where, args := buildLogsWhere(f)
	sql := fmt.Sprintf(
		"SELECT %s FROM logs WHERE %s ORDER BY ts DESC, service LIMIT %d",
		logColumns, where, f.Limit)
	rows, err := c.conn.Query(clickhouse.Context(ctx, clickhouse.WithParameters(args)), sql)
	if err != nil {
		return nil, fmt.Errorf("obs: search logs: %w", err)
	}
	defer rows.Close()
	out := make([]LogRow, 0, f.Limit)
	for rows.Next() {
		var r LogRow
		if err := rows.Scan(&r.Service, &r.Ts, &r.Level, &r.Route, &r.TraceID, &r.Pod, &r.Msg); err != nil {
			return nil, fmt.Errorf("obs: scan log row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PatternRow is one aggregated error/log template (fingerprint via
// replaceRegexpAll of numbers/uuids/quoted strings, first 120 chars).
type PatternRow struct {
	Pattern   string    `json:"pattern"`
	Count     uint64    `json:"count"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Pods      []string  `json:"pods"`
	Sample    string    `json:"sample"`
}

// patternExpr normalizes a msg into its template group-by key.
const patternExpr = `substring(replaceRegexpAll(replaceRegexpAll(replaceRegexpAll(msg, '"[^"]*"', '<s>'), '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}', '<u>'), '[0-9]+', '<n>'), 1, 120)`

// Patterns aggregates the filtered window into at most 10 templates.
func (c *Client) Patterns(ctx context.Context, f LogsFilter) ([]PatternRow, error) {
	f, err := f.Validate()
	if err != nil {
		return nil, err
	}
	where, args := buildLogsWhere(f)
	sql := fmt.Sprintf(`
SELECT
    pattern,
    count()          AS cnt,
    min(ts)          AS first_seen,
    max(ts)          AS last_seen,
    arraySlice(groupUniqArray(pod), 1, 5) AS pods,
    any(msg)         AS sample
FROM (
    SELECT pod, ts, msg, %s AS pattern
    FROM logs
    WHERE %s
)
GROUP BY pattern
ORDER BY cnt DESC
LIMIT 10`, patternExpr, where)
	rows, err := c.conn.Query(clickhouse.Context(ctx, clickhouse.WithParameters(args)), sql)
	if err != nil {
		return nil, fmt.Errorf("obs: patterns: %w", err)
	}
	defer rows.Close()
	var out []PatternRow
	for rows.Next() {
		var r PatternRow
		if err := rows.Scan(&r.Pattern, &r.Count, &r.FirstSeen, &r.LastSeen, &r.Pods, &r.Sample); err != nil {
			return nil, fmt.Errorf("obs: scan pattern row: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// HistogramPoint is one minute bucket of the time histogram.
type HistogramPoint struct {
	Minute time.Time `json:"minute"`
	Count  uint64    `json:"count"`
}

// Histogram groups the filtered window per minute for the frontend chart.
func (c *Client) Histogram(ctx context.Context, f LogsFilter) ([]HistogramPoint, error) {
	f, err := f.Validate()
	if err != nil {
		return nil, err
	}
	where, args := buildLogsWhere(f)
	sql := fmt.Sprintf(
		"SELECT toStartOfMinute(ts) AS minute, count() AS cnt FROM logs WHERE %s GROUP BY minute ORDER BY minute", where)
	rows, err := c.conn.Query(clickhouse.Context(ctx, clickhouse.WithParameters(args)), sql)
	if err != nil {
		return nil, fmt.Errorf("obs: histogram: %w", err)
	}
	defer rows.Close()
	var out []HistogramPoint
	for rows.Next() {
		var p HistogramPoint
		if err := rows.Scan(&p.Minute, &p.Count); err != nil {
			return nil, fmt.Errorf("obs: scan histogram point: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
