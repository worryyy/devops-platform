package alertgw

import (
	"context"
	"encoding/json"
	"time"
)

// EventRecorder is the ClickHouse mirror surface (P4 事件双写): alerts stay
// PG-backed (source of truth) and are mirrored into platform.events for the
// weekly report and P5 复盘. Consumer-side interface, satisfied by
// *obs.Client.
type EventRecorder interface {
	RecordEvent(ctx context.Context, kind, source string, payload []byte) error
}

const alertEventSource = "alertmanager"

// SetEventRecorder arms the optional CH mirror; nil (default) keeps the P3
// behavior exactly.
func (g *Gateway) SetEventRecorder(r EventRecorder) { g.recorder = r }

// recordEvent is best-effort: a CH outage must never affect alert ingestion
// or notification (fail-open in the data plane too).
func (g *Gateway) recordEvent(ctx context.Context, alert Alert) {
	if g.recorder == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"fingerprint": alert.Fingerprint,
		"alertname":   alert.Alertname,
		"service":     alert.Service,
		"severity":    alert.Severity,
		"signal_type": alert.SignalType,
		"status":      alert.Status,
		"starts_at":   alert.StartsAt,
		"ends_at":     alert.EndsAt,
	})
	if err != nil {
		g.logger.Warn("alertgw event marshal failed", "fingerprint", alert.Fingerprint, "error", err)
		return
	}
	recCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := g.recorder.RecordEvent(recCtx, "alert", alertEventSource, payload); err != nil {
		g.logger.Warn("alertgw event mirror failed", "fingerprint", alert.Fingerprint, "error", err)
	}
}
