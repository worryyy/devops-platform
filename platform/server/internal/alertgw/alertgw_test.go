package alertgw

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/worryyy/devops-platform/platform/server/internal/api"
	"github.com/worryyy/devops-platform/platform/server/internal/auth"
	"github.com/worryyy/devops-platform/platform/server/internal/notify"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// memStore is the hermetic AlertStoreAPI for handler/gateway tests.
type memStore struct {
	mu     sync.Mutex
	nextID int64
	byFP   map[string]*Alert
	byID   map[int64]*Alert
}

func newMemStore() *memStore {
	return &memStore{nextID: 0, byFP: map[string]*Alert{}, byID: map[int64]*Alert{}}
}

func (m *memStore) Upsert(_ context.Context, alert Alert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.byFP[alert.Fingerprint]; ok {
		alert.ID = existing.ID
		*existing = alert
		return nil
	}
	m.nextID++
	alert.ID = m.nextID
	copied := alert
	m.byFP[alert.Fingerprint] = &copied
	m.byID[alert.ID] = &copied
	return nil
}

func (m *memStore) List(_ context.Context, filter AlertFilter) ([]Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Alert{}
	for _, a := range m.byID {
		if filter.Status != "" && a.Status != filter.Status {
			continue
		}
		if filter.Service != "" && a.Service != filter.Service {
			continue
		}
		if filter.Severity != "" && a.Severity != filter.Severity {
			continue
		}
		if filter.SignalType != "" && a.SignalType != filter.SignalType {
			continue
		}
		out = append(out, *a)
	}
	return out, nil
}

func (m *memStore) Get(_ context.Context, id int64) (Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.byID[id]; ok {
		return *a, nil
	}
	return Alert{}, ErrAlertNotFound
}

func (m *memStore) Ack(_ context.Context, id int64, by string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byID[id]
	if !ok {
		return ErrAlertNotFound
	}
	if a.Labels.Set == nil {
		a.Labels.Set = map[string]string{}
	}
	a.Labels.Set["acked_by"] = by
	a.Labels.Set["acked_at"] = at.UTC().Format(time.RFC3339)
	return nil
}

func (m *memStore) BumpDedup(_ context.Context, fingerprint string, count int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.byFP[fingerprint]
	if !ok {
		return ErrAlertNotFound
	}
	if a.Labels.Set == nil {
		a.Labels.Set = map[string]string{}
	}
	a.Labels.Set["dedup_count"] = strconv.Itoa(count)
	return nil
}

func (m *memStore) CountSince(_ context.Context, alertname, service string, since time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var count int64
	for _, a := range m.byID {
		if a.ReceivedAt.Before(since) {
			continue
		}
		if alertname != "" && a.Alertname != alertname {
			continue
		}
		if service != "" && a.Service != service {
			continue
		}
		count++
	}
	return count, nil
}

// cardSink records every card the gateway tried to send.
type cardSink struct {
	mu    sync.Mutex
	cards []notify.Card
	fail  bool
}

func (s *cardSink) SendCard(_ context.Context, card notify.Card) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cards = append(s.cards, card)
	if s.fail {
		return io.ErrClosedPipe
	}
	return nil
}

func (s *cardSink) Enabled() bool { return true }

func (s *cardSink) snapshot() []notify.Card {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]notify.Card(nil), s.cards...)
}

// fakeClock keeps dedup windows deterministic.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func mustFixture(t *testing.T) Payload {
	t.Helper()
	raw, err := os.ReadFile("../../../../k3s/ci/fixtures/alertmanager/alert-v2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	payload, err := DecodePayload(raw)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	return payload
}

func newTestGateway(store AlertStoreAPI, sink CardSender, clock *fakeClock) *Gateway {
	g := NewGateway(store, sink, discardLogger(), "http://platform.test")
	g.now = clock.Now
	return g
}

func TestGatewayHighRiskBypassesDedup(t *testing.T) {
	store := newMemStore()
	sink := &cardSink{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)}
	g := newTestGateway(store, sink, clock)
	payload := mustFixture(t)

	// user_impact + critical → whitelist: every repeat sends immediately.
	for i := 0; i < 3; i++ {
		report := g.Ingest(context.Background(), payload)
		if report.Notified != 1 || report.Stored != 1 {
			t.Fatalf("round %d: %+v", i, report)
		}
		clock.Advance(30 * time.Second)
	}
	if got := len(sink.snapshot()); got != 3 {
		t.Fatalf("whitelisted alerts must bypass dedup, got %d cards", got)
	}
}

func TestGatewayDedupWindowSuppressesRepeats(t *testing.T) {
	store := newMemStore()
	sink := &cardSink{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)}
	g := newTestGateway(store, sink, clock)

	// A plain warning alert (not whitelisted): LokiPVCUsageHigh.
	payload := Payload{
		Version: "4", Status: "firing",
		Alerts: []PayloadAlert{{
			Status: "firing",
			Labels: map[string]string{
				"alertname": "SomeServiceWarning", "service": "chat",
				"severity": "warning", "signal_type": "infra",
			},
			Annotations: map[string]string{"summary": "noisy"},
			StartsAt:    clock.Now(),
			Fingerprint: "aaa111",
		}},
	}
	if report := g.Ingest(context.Background(), payload); report.Notified != 1 || report.Deduped != 0 {
		t.Fatalf("first fire should notify: %+v", report)
	}
	clock.Advance(time.Minute)
	if report := g.Ingest(context.Background(), payload); report.Deduped != 1 || report.Notified != 0 {
		t.Fatalf("repeat inside 5m window must be deduped: %+v", report)
	}
	clock.Advance(DedupWindow) // past the window
	if report := g.Ingest(context.Background(), payload); report.Notified != 1 || report.Deduped != 0 {
		t.Fatalf("repeat after the window must notify again: %+v", report)
	}
	if got := len(sink.snapshot()); got != 2 {
		t.Fatalf("expected 2 cards total, got %d", got)
	}
	// Dedup counter lands on the stored labels.
	alerts, _ := store.List(context.Background(), AlertFilter{})
	if alerts[0].Labels.Set["dedup_count"] != "1" {
		t.Fatalf("dedup_count label missing: %v", alerts[0].Labels.Set)
	}
}

func TestGatewayDeployContextStoredNotNotified(t *testing.T) {
	store := newMemStore()
	sink := &cardSink{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)}
	g := newTestGateway(store, sink, clock)

	payload := Payload{
		Version: "4", Status: "firing",
		Alerts: []PayloadAlert{{
			Status: "firing",
			Labels: map[string]string{
				"alertname": "ReleaseDeployNoiseWindow", "service": "theme",
				"severity": "info", "signal_type": "deploy_context",
			},
			StartsAt:   clock.Now(),
			Fingerprint: "ctx001",
		}},
	}
	report := g.Ingest(context.Background(), payload)
	if report.Stored != 1 || report.Suppressed != 1 || report.Notified != 0 {
		t.Fatalf("deploy_context must be stored-only: %+v", report)
	}
	if got := len(sink.snapshot()); got != 0 {
		t.Fatalf("deploy_context must not notify, got %d cards", got)
	}
}

func TestGatewayResolvedFlipsStatus(t *testing.T) {
	store := newMemStore()
	sink := &cardSink{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)}
	g := newTestGateway(store, sink, clock)

	firing := mustFixture(t)
	g.Ingest(context.Background(), firing)

	resolved := firing
	resolved.Status = "resolved"
	resolved.Alerts[0].Status = "resolved"
	end := clock.Now().Add(10 * time.Minute)
	resolved.Alerts[0].EndsAt = end

	report := g.Ingest(context.Background(), resolved)
	if report.Stored != 1 || report.Notified != 0 {
		t.Fatalf("resolved pass: %+v", report)
	}
	alerts, _ := store.List(context.Background(), AlertFilter{Status: "resolved"})
	if len(alerts) != 1 {
		t.Fatalf("row must flip to resolved, got %d", len(alerts))
	}
	if alerts[0].EndsAt == nil || !alerts[0].EndsAt.Equal(end) {
		t.Fatalf("ends_at must persist, got %v", alerts[0].EndsAt)
	}
}

func TestGatewayFeishuFailureDoesNotBlockStorage(t *testing.T) {
	store := newMemStore()
	sink := &cardSink{fail: true}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)}
	g := newTestGateway(store, sink, clock)

	report := g.Ingest(context.Background(), mustFixture(t))
	if report.Stored != 1 {
		t.Fatalf("alert must persist despite feishu failure: %+v", report)
	}
}

// webhookRouter assembles the public + authed routes the way app.go does.
func webhookRouter(t *testing.T, store AlertStoreAPI, sink CardSender, clock *fakeClock, secret string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	gateway := newTestGateway(store, sink, clock)
	handlers := Handlers{Gateway: gateway, Store: store, Now: clock.Now}
	router := api.NewBaseRouter()
	apiGroup := router.Group("/api")
	apiGroup.POST("/webhooks/alertmanager", handlers.Webhook)
	authed := apiGroup.Group("")
	if secret != "" {
		authed.Use(func(c *gin.Context) {
			if c.GetHeader("Authorization") != "Bearer "+secret {
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
			claims := auth.Claims{Username: "tester", Role: "viewer"}
			c.Set("auth_claims", claims)
		})
	} else {
		authed.Use(func(c *gin.Context) {
			c.Set("auth_claims", auth.Claims{Username: "tester", Role: "viewer"})
		})
	}
	handlers.Register(authed)
	return router
}

func TestWebhookHandlerStoresAndLists(t *testing.T) {
	raw, err := os.ReadFile("../../../../k3s/ci/fixtures/alertmanager/alert-v2.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	store := newMemStore()
	sink := &cardSink{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)}
	router := webhookRouter(t, store, sink, clock, "")

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/alertmanager", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook status %d: %s", w.Code, w.Body.String())
	}
	var envelope struct {
		Code int          `json:"code"`
		Data IngestReport `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	if envelope.Data.Stored != 1 || envelope.Data.Notified != 1 {
		t.Fatalf("ingest report: %+v", envelope.Data)
	}

	// Listing reflects the stored alert.
	listReq := httptest.NewRequest(http.MethodGet, "/api/alerts?status=firing&service=theme&severity=critical", nil)
	listW := httptest.NewRecorder()
	router.ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("list status %d", listW.Code)
	}
	var listEnvelope struct {
		Data []Alert `json:"data"`
	}
	if err := json.Unmarshal(listW.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listEnvelope.Data) != 1 || listEnvelope.Data[0].Alertname != "ServiceHighErrorRate" {
		t.Fatalf("list contents unexpected: %+v", listEnvelope.Data)
	}
}

func TestWebhookHandlerRejectsGarbage(t *testing.T) {
	store := newMemStore()
	sink := &cardSink{}
	clock := &fakeClock{now: time.Now()}
	router := webhookRouter(t, store, sink, clock, "")

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/alertmanager", bytes.NewReader([]byte(`{"alerts":[]}`)))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("empty payload must 400, got %d", w.Code)
	}
}

func TestAckWritesUsernameIntoLabels(t *testing.T) {
	store := newMemStore()
	sink := &cardSink{}
	clock := &fakeClock{now: time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)}
	router := webhookRouter(t, store, sink, clock, "tok")

	g := newTestGateway(store, sink, clock)
	g.Ingest(context.Background(), mustFixture(t))

	req := httptest.NewRequest(http.MethodPost, "/api/alerts/1/ack", nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ack status %d: %s", w.Code, w.Body.String())
	}
	alert, err := store.Get(context.Background(), 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if alert.Labels.Set["acked_by"] != "tester" {
		t.Fatalf("acked_by missing: %v", alert.Labels.Set)
	}
}

func TestIsHighRiskWhitelist(t *testing.T) {
	cases := []struct {
		alert Alert
		want  bool
	}{
		{Alert{Alertname: "ReleasePodCrashLooping", Severity: "warning"}, true},
		{Alert{Alertname: "ReleasePodNotReady", Severity: "warning"}, true},
		{Alert{Alertname: "NodeDiskPressure", Severity: "warning"}, true},
		{Alert{Alertname: "LokiPVCUsageHigh", Severity: "warning"}, true},
		{Alert{Alertname: "ServiceHighErrorRate", Severity: "critical"}, true},
		{Alert{Alertname: "SomeNoise", Severity: "warning"}, false},
		{Alert{Alertname: "SomeNoise", Severity: "info", SignalType: "deploy_noise"}, false},
	}
	for _, tc := range cases {
		if got := isHighRisk(tc.alert); got != tc.want {
			t.Fatalf("isHighRisk(%+v) = %v, want %v", tc.alert, got, tc.want)
		}
	}
}
