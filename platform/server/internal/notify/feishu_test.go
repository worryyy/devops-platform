package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// capturedBot records what arrived at the fake Feishu endpoint.
type capturedBot struct {
	mu       sync.Mutex
	payloads []map[string]any
	paths    []string
	auths    []string
	statuses map[int]int
}

func (b *capturedBot) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		payload := map[string]any{}
		_ = json.Unmarshal(raw, &payload)
		b.mu.Lock()
		attempt := len(b.payloads)
		b.payloads = append(b.payloads, payload)
		b.paths = append(b.paths, r.URL.Path)
		b.auths = append(b.auths, r.Header.Get("Authorization"))
		b.mu.Unlock()
		status := http.StatusOK
		if s, ok := b.statuses[attempt]; ok {
			status = s
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		switch {
		case strings.HasSuffix(r.URL.Path, "/tenant_access_token/internal"):
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"t-xyz","expire":7200}`))
		case strings.HasSuffix(r.URL.Path, "/im/v1/chats"):
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":{"items":[{"chat_id":"oc-123","name":"alerts"}]}}`))
		case status != http.StatusOK:
			_, _ = w.Write([]byte(`{"code":99991400,"msg":"bad request"}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"msg":"success"}`))
		}
	}
}

func (b *capturedBot) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.payloads)
}

func (b *capturedBot) last() map[string]any {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.payloads[len(b.payloads)-1]
}

func (b *capturedBot) lastPath() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.paths[len(b.paths)-1]
}

func (b *capturedBot) lastAuth() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.auths[len(b.auths)-1]
}

func sampleCard() Card {
	return Card{
		Title:    "ServiceHighErrorRate",
		Severity: "critical",
		Service:  "theme",
		StartsAt: "2026-09-21 17:00:00",
		Summary:  "Error ratio is above 5 percent",
		DeepLink: "http://platform.test/alerts",
		Status:   "firing",
	}
}

func TestSignMatchesFeishuAlgorithmShape(t *testing.T) {
	got := Sign("mysecret", 1789986000)
	if len(got) != 44 {
		t.Fatalf("sign length %d, want 44", len(got))
	}
	if got != Sign("mysecret", 1789986000) {
		t.Fatal("sign must be deterministic")
	}
	if Sign("other", 1789986000) == got {
		t.Fatal("sign must depend on the secret")
	}
	if Sign("mysecret", 1789986001) == got {
		t.Fatal("sign must depend on the timestamp")
	}
}

func TestAppModeFetchesTokenAndSendsCard(t *testing.T) {
	bot := &capturedBot{}
	server := httptest.NewServer(bot.handler())
	defer server.Close()

	client := NewFeishuClient(FeishuOptions{
		AppID: "cli_test", AppSecret: "sec", HTTP: server.Client(),
	})
	// point at the fake open platform
	client.setBaseURL(server.URL)

	if !client.Enabled() {
		t.Fatal("app mode must be enabled")
	}
	if err := client.SendCard(context.Background(), sampleCard()); err != nil {
		t.Fatalf("send: %v", err)
	}

	// token call
	if bot.paths[0] != "/open-apis/auth/v3/tenant_access_token/internal" {
		t.Fatalf("first call %q", bot.paths[0])
	}
	tokenBody := bot.payloads[0]
	if tokenBody["app_id"] != "cli_test" || tokenBody["app_secret"] != "sec" {
		t.Fatalf("token body: %v", tokenBody)
	}
	// chat discovery then message send
	if bot.lastPath() != "/open-apis/im/v1/messages" {
		t.Fatalf("last call %q", bot.lastPath())
	}
	if bot.lastAuth() != "Bearer t-xyz" {
		t.Fatalf("message auth: %q", bot.lastAuth())
	}
	message := bot.last()
	if message["msg_type"] != "interactive" {
		t.Fatalf("msg_type: %v", message["msg_type"])
	}
	if message["receive_id"] != "oc-123" {
		t.Fatalf("receive_id (auto-discovered chat): %v", message["receive_id"])
	}
	content, _ := message["content"].(string)
	for _, want := range []string{"theme", "critical", "查看告警", "http://platform.test/alerts"} {
		if !strings.Contains(content, want) {
			t.Fatalf("card content missing %q: %s", want, content)
		}
	}
}

func TestAppModeExplicitChatSkipsDiscovery(t *testing.T) {
	bot := &capturedBot{}
	server := httptest.NewServer(bot.handler())
	defer server.Close()

	client := NewFeishuClient(FeishuOptions{
		AppID: "cli_test", AppSecret: "sec", ChatID: "oc-explicit",
		HTTP: server.Client(),
	})
	client.setBaseURL(server.URL)

	if err := client.SendCard(context.Background(), sampleCard()); err != nil {
		t.Fatalf("send: %v", err)
	}
	if bot.count() != 2 { // token + message, no chat listing
		t.Fatalf("explicit chat must skip discovery, got %d calls", bot.count())
	}
	if bot.last()["receive_id"] != "oc-explicit" {
		t.Fatalf("receive_id: %v", bot.last()["receive_id"])
	}
}

func TestWebhookModeSignsAndSends(t *testing.T) {
	bot := &capturedBot{}
	server := httptest.NewServer(bot.handler())
	defer server.Close()

	client := NewFeishuClient(FeishuOptions{
		WebhookURL:    server.URL + "/open-apis/bot/v2/hook/x",
		WebhookSecret: "signsecret",
		HTTP:          server.Client(),
	})
	client.now = func() time.Time { return time.Unix(1789986000, 0) }

	if err := client.SendCard(context.Background(), sampleCard()); err != nil {
		t.Fatalf("send: %v", err)
	}
	payload := bot.last()
	if payload["msg_type"] != "interactive" {
		t.Fatalf("msg_type: %v", payload["msg_type"])
	}
	if payload["sign"] != Sign("signsecret", 1789986000) {
		t.Fatalf("sign mismatch: %v", payload["sign"])
	}
	cardJSON, _ := json.Marshal(payload["card"])
	var parsed struct {
		Header struct {
			Template string `json:"template"`
		} `json:"header"`
	}
	if err := json.Unmarshal(cardJSON, &parsed); err != nil {
		t.Fatalf("card shape: %v", err)
	}
	if parsed.Header.Template != "red" {
		t.Fatalf("critical header must be red, got %q", parsed.Header.Template)
	}
	full := string(cardJSON)
	for _, want := range []string{"theme", "Error ratio is above 5 percent", "查看告警"} {
		if !strings.Contains(full, want) {
			t.Fatalf("card missing %q", want)
		}
	}
}

func TestSendCardRetriesOn429(t *testing.T) {
	bot := &capturedBot{statuses: map[int]int{0: http.StatusTooManyRequests}}
	server := httptest.NewServer(bot.handler())
	defer server.Close()

	client := NewFeishuClient(FeishuOptions{
		WebhookURL: server.URL + "/hook",
		HTTP:       server.Client(),
	})
	if err := client.SendCard(context.Background(), Card{Title: "A", Severity: "warning"}); err != nil {
		t.Fatalf("send after retry: %v", err)
	}
	if bot.count() != 2 {
		t.Fatalf("expected 1 retry, got %d requests", bot.count())
	}
}

func TestSendCardFailsFastOn4xx(t *testing.T) {
	bot := &capturedBot{statuses: map[int]int{0: http.StatusBadRequest}}
	server := httptest.NewServer(bot.handler())
	defer server.Close()

	client := NewFeishuClient(FeishuOptions{
		WebhookURL: server.URL + "/hook",
		HTTP:       server.Client(),
	})
	if err := client.SendCard(context.Background(), Card{Title: "A"}); err == nil {
		t.Fatal("4xx must fail")
	}
	if bot.count() != 1 {
		t.Fatalf("4xx must not retry, got %d requests", bot.count())
	}
}

func TestNotEnabledErrors(t *testing.T) {
	client := NewFeishuClient(FeishuOptions{})
	if client.Enabled() {
		t.Fatal("empty config must not be enabled")
	}
	if err := client.SendCard(context.Background(), Card{Title: "A"}); err == nil {
		t.Fatal("expected error")
	}
}
