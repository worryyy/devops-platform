package alertgw

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/notify"
)

// DedupWindow is how long one alertname+service pair stays quiet after a
// Feishu notification (the first no-AI noise gate; P5 replaces the policy).
const DedupWindow = 5 * time.Minute

// CardSender is the notification surface; *notify.FeishuClient satisfies it.
type CardSender interface {
	SendCard(ctx context.Context, card notify.Card) error
	Enabled() bool
}

// Gateway ingests Alertmanager payloads: upsert every alert, then decide per
// firing alert whether a card goes out.
type Gateway struct {
	store     AlertStoreAPI
	sender    CardSender
	recorder  EventRecorder // optional CH events mirror (P4); nil = off
	logger    *slog.Logger
	publicURL string
	now       func() time.Time

	mu       sync.Mutex
	lastSent map[string]time.Time
	deduped  map[string]int
}

func NewGateway(store AlertStoreAPI, sender CardSender, logger *slog.Logger, publicURL string) *Gateway {
	return &Gateway{
		store:     store,
		sender:    sender,
		logger:    logger,
		publicURL: publicURL,
		now:       time.Now,
		lastSent:  map[string]time.Time{},
		deduped:   map[string]int{},
	}
}

// IngestReport summarizes one webhook delivery for tests and debugging.
type IngestReport struct {
	Stored     int `json:"stored"`
	Notified   int `json:"notified"`
	Deduped    int `json:"deduped"`
	Suppressed int `json:"suppressed"` // deploy_context: stored, never notified
}

// Ingest processes one Alertmanager v2 payload. Storage errors abort the
// whole batch (AM will retry); notification failures only log — a Feishu
// outage must not loop alerts back into Alertmanager.
func (g *Gateway) Ingest(ctx context.Context, payload Payload) IngestReport {
	var report IngestReport
	now := g.now()
	for _, pa := range payload.Alerts {
		alert := pa.ToAlert(now)
		if alert.Fingerprint == "" {
			continue
		}
		if err := g.store.Upsert(ctx, alert); err != nil {
			g.logger.Error("alertgw upsert failed", "fingerprint", alert.Fingerprint, "error", err)
			continue
		}
		report.Stored++
		g.recordEvent(ctx, alert)

		if alert.Status != "firing" {
			continue
		}
		switch {
		case alert.SignalType == "deploy_context":
			// Context signals only drive inhibition upstream; keep them
			// queryable but silent (blueprint §7.2).
			report.Suppressed++
		case isHighRisk(alert):
			g.notify(ctx, alert, now)
			report.Notified++
		default:
			key := dedupKey(alert)
			if g.withinWindow(key, now) {
				report.Deduped++
				g.bumpDedup(ctx, alert)
				continue
			}
			g.notify(ctx, alert, now)
			report.Notified++
		}
	}
	return report
}

// isHighRisk whitelists alerts that bypass every later gate (dedup, and in
// P5 the AI triage): sustained crash loops, not-ready pods, disk pressure
// and any user_impact signal.
func isHighRisk(alert Alert) bool {
	if alert.SignalType == "user_impact" {
		return true
	}
	for _, fragment := range []string{"CrashLoop", "NotReady", "Disk", "Filesystem", "PVCUsage"} {
		if strings.Contains(alert.Alertname, fragment) {
			return true
		}
	}
	return alert.Severity == "critical"
}

func dedupKey(alert Alert) string {
	return alert.Alertname + "|" + alert.Service
}

func (g *Gateway) withinWindow(key string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	last, ok := g.lastSent[key]
	return ok && now.Sub(last) < DedupWindow
}

func (g *Gateway) markSent(key string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.lastSent[key] = now
}

// bumpDedup records the suppressed repeat count on the stored row so the
// UI can show how noisy an alert was.
func (g *Gateway) bumpDedup(ctx context.Context, alert Alert) {
	g.mu.Lock()
	key := dedupKey(alert)
	g.deduped[key]++
	count := g.deduped[key]
	g.mu.Unlock()
	if err := g.store.BumpDedup(ctx, alert.Fingerprint, count); err != nil {
		g.logger.Warn("alertgw bump dedup failed", "fingerprint", alert.Fingerprint, "error", err)
	}
}

func (g *Gateway) notify(ctx context.Context, alert Alert, now time.Time) {
	g.markSent(dedupKey(alert), now)
	g.mu.Lock()
	delete(g.deduped, dedupKey(alert))
	g.mu.Unlock()

	if g.sender == nil || !g.sender.Enabled() {
		g.logger.Info("alertgw notify skipped: feishu not configured", "alertname", alert.Alertname, "service", alert.Service)
		return
	}
	card := notify.Card{
		Title:    alert.Alertname,
		Severity: alert.Severity,
		Service:  alert.Service,
		StartsAt: formatTime(alert.StartsAt),
		Summary:  alert.Summary(),
		DeepLink: g.publicURL + "/alerts",
		Status:   alert.Status,
	}
	if err := g.sender.SendCard(ctx, card); err != nil {
		g.logger.Error("alertgw feishu send failed", "alertname", alert.Alertname, "service", alert.Service, "error", err)
	}
}

func formatTime(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}
