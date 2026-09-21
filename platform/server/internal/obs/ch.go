// Package obs is the ClickHouse side of the P4 data pipeline: log search /
// pattern aggregation / minute histogram for the /logs page, plus the
// platform.events sink shared by alertgw (alerts), delivery (releases) and,
// later, inspections (P6) and AI verdicts (P5).
package obs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// Options mirrors the CLICKHOUSE_* env surface (see internal/config).
type Options struct {
	Addr     string // host:port of the native protocol (…:9000)
	Database string
	Username string
	Password string
	Logger   *slog.Logger
}

// Client wraps one native-protocol connection. All queries are read-only
// SELECTs plus the small events INSERT; safe for the API server's lifetime.
type Client struct {
	conn   driver.Conn
	logger *slog.Logger
}

// Dial opens the connection and pings it; boot only calls this when
// CLICKHOUSE_ADDR is set, so a failure surfaces immediately rather than as
// per-request 503s later.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	if opts.Addr == "" {
		return nil, fmt.Errorf("obs: empty clickhouse addr")
	}
	if opts.Database == "" {
		opts.Database = "platform"
	}
	if opts.Username == "" {
		opts.Username = "default"
	}
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{opts.Addr},
		Auth: clickhouse.Auth{
			Database: opts.Database,
			Username: opts.Username,
			Password: opts.Password,
		},
		// native 9000 明文协议：不设 TLS（设了 clickhouse-go 就按 TLS 握手）
		Settings: clickhouse.Settings{
			// 日志页查询面向交互：硬超时，防拖垮小规格 CH
			"max_execution_time": 30,
		},
		DialTimeout: 5 * time.Second,
		Compression: &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
	})
	if err != nil {
		return nil, fmt.Errorf("obs: open clickhouse: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := conn.Ping(pingCtx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("obs: ping clickhouse: %w", err)
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Client{conn: conn, logger: opts.Logger}, nil
}

func (c *Client) Close() error { return c.conn.Close() }

// Querier is the read surface (mock-friendly for the api layer and tests).
type Querier interface {
	SearchLogs(ctx context.Context, f LogsFilter) ([]LogRow, error)
	Patterns(ctx context.Context, f LogsFilter) ([]PatternRow, error)
	Histogram(ctx context.Context, f LogsFilter) ([]HistogramPoint, error)
}

// EventRecorder is the write surface used by alertgw/delivery for the CH
// events double-write (kind: alert|release|inspection|ai_verdict|report).
type EventRecorder interface {
	RecordEvent(ctx context.Context, kind, source string, payload []byte) error
}

var (
	_ Querier       = (*Client)(nil)
	_ EventRecorder = (*Client)(nil)
)
