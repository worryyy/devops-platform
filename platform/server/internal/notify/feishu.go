// Package notify sends platform notifications. P3 ships the Feishu sender
// in two modes: a Feishu app (app_id/app_secret + im/v1 messages, preferred)
// or a group-bot webhook with signature; the P5 AI gateway grows on the
// same Card shape.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Card is the notification payload every channel maps onto. Alert fields
// come from the alert gateway; Status lets a card mark itself resolved.
type Card struct {
	Title    string // card title, e.g. the alertname
	Severity string // critical | warning | info
	Service  string
	StartsAt string // preformatted timestamp
	Summary  string // one-liner (annotation summary/description)
	DeepLink string // absolute URL back into the platform
	Status   string // firing | resolved
}

// FeishuClient sends cards via app credentials or a bot webhook, whichever
// is configured (app mode wins when both are present).
type FeishuClient struct {
	appID     string
	appSecret string
	chatID    string
	webhook   string
	webSign   string
	http      *http.Client
	now       func() time.Time
	baseURL   string

	mu            sync.Mutex
	token         string
	tokenExpireAt time.Time
}

// FeishuOptions assembles a client; empty app id falls back to webhook mode.
type FeishuOptions struct {
	AppID     string
	AppSecret string
	// ChatID targets one group chat; empty auto-discovers when the app is
	// in exactly one chat.
	ChatID        string
	WebhookURL    string
	WebhookSecret string
	HTTP          *http.Client
}

const feishuBase = "https://open.feishu.cn"

func NewFeishuClient(opts FeishuOptions) *FeishuClient {
	hc := opts.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}
	return &FeishuClient{
		appID:     opts.AppID,
		appSecret: opts.AppSecret,
		chatID:    opts.ChatID,
		webhook:   opts.WebhookURL,
		webSign:   opts.WebhookSecret,
		http:      hc,
		now:       time.Now,
		baseURL:   feishuBase,
	}
}

// setBaseURL overrides the open-platform base for tests.
func (f *FeishuClient) setBaseURL(base string) { f.baseURL = base }

// Enabled reports whether any send mode is configured.
func (f *FeishuClient) Enabled() bool {
	return f.appID != "" && f.appSecret != "" || f.webhook != ""
}

func (f *FeishuClient) appMode() bool { return f.appID != "" && f.appSecret != "" }

// SendCard delivers one card. Rate limits (429) and server errors back off
// and retry up to three attempts; 4xx answers fail fast.
func (f *FeishuClient) SendCard(ctx context.Context, card Card) error {
	if !f.Enabled() {
		return fmt.Errorf("feishu is not configured")
	}
	if f.appMode() {
		return f.withRetry(ctx, func() error { return f.sendViaApp(ctx, card) })
	}
	return f.withRetry(ctx, func() error { return f.sendViaWebhook(ctx, card) })
}

func (f *FeishuClient) withRetry(ctx context.Context, send func() error) error {
	backoff := time.Second
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
			backoff *= 3
		}
		err := send()
		if err == nil {
			return nil
		}
		if isNoRetry(err) {
			return err
		}
		lastErr = err
	}
	return fmt.Errorf("feishu retries exhausted: %w", lastErr)
}

// errNoRetry marks permanent failures (bad request / wrong credentials).
type errNoRetry struct{ err error }

func (e *errNoRetry) Error() string { return e.err.Error() }
func (e *errNoRetry) Unwrap() error { return e.err }

func isNoRetry(err error) bool {
	_, ok := err.(*errNoRetry)
	return ok
}

// postJSON is the shared HTTP helper returning status + decoded envelope.
func (f *FeishuClient) postJSON(ctx context.Context, url string, body any, headers map[string]string) (int, []byte, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := f.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))
	return resp.StatusCode, payload, nil
}

type feishuEnvelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func checkEnvelope(status int, payload []byte) error {
	if status == http.StatusTooManyRequests || status >= 500 {
		return fmt.Errorf("feishu http %d: %s", status, payload)
	}
	if status != http.StatusOK {
		return &errNoRetry{fmt.Errorf("feishu http %d: %s", status, payload)}
	}
	var envelope feishuEnvelope
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return &errNoRetry{fmt.Errorf("decode feishu reply: %w", err)}
	}
	// 99991400/230002 style errors are credential/argument problems.
	if envelope.Code != 0 {
		return &errNoRetry{fmt.Errorf("feishu code %d: %s", envelope.Code, envelope.Msg)}
	}
	return nil
}

// ------------------------------------------------------------- app mode

type tokenReply struct {
	feishuEnvelope
	TenantAccessToken string `json:"tenant_access_token"`
	Expire            int    `json:"expire"` // seconds
}

func (f *FeishuClient) tenantToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	if f.token != "" && f.now().Before(f.tokenExpireAt) {
		token := f.token
		f.mu.Unlock()
		return token, nil
	}
	f.mu.Unlock()

	status, payload, err := f.postJSON(ctx, f.baseURL+"/open-apis/auth/v3/tenant_access_token/internal",
		map[string]string{"app_id": f.appID, "app_secret": f.appSecret}, nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", &errNoRetry{fmt.Errorf("token http %d: %s", status, payload)}
	}
	var reply tokenReply
	if err := json.Unmarshal(payload, &reply); err != nil {
		return "", &errNoRetry{fmt.Errorf("decode token reply: %w", err)}
	}
	if reply.Code != 0 || reply.TenantAccessToken == "" {
		return "", &errNoRetry{fmt.Errorf("token code %d: %s", reply.Code, reply.Msg)}
	}
	f.mu.Lock()
	f.token = reply.TenantAccessToken
	// refresh a minute before the announced expiry
	f.tokenExpireAt = f.now().Add(time.Duration(reply.Expire)*time.Second - time.Minute)
	f.mu.Unlock()
	return reply.TenantAccessToken, nil
}

type chatListReply struct {
	feishuEnvelope
	Data struct {
		Items []struct {
			ChatID string `json:"chat_id"`
			Name   string `json:"name"`
		} `json:"items"`
	} `json:"data"`
}

func (f *FeishuClient) resolveChatID(ctx context.Context, token string) (string, error) {
	if f.chatID != "" {
		return f.chatID, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		f.baseURL+"/open-apis/im/v1/chats?page_size=20", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := f.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))
	if resp.StatusCode != http.StatusOK {
		return "", &errNoRetry{fmt.Errorf("chat list http %d: %s", resp.StatusCode, payload)}
	}
	var reply chatListReply
	if err := json.Unmarshal(payload, &reply); err != nil {
		return "", &errNoRetry{err}
	}
	if len(reply.Data.Items) == 1 {
		return reply.Data.Items[0].ChatID, nil
	}
	if len(reply.Data.Items) == 0 {
		return "", &errNoRetry{fmt.Errorf("feishu app is in no group chat; add it to the alert group")}
	}
	return "", &errNoRetry{fmt.Errorf("feishu app is in %d chats; set FEISHU_CHAT_ID", len(reply.Data.Items))}
}

func (f *FeishuClient) sendViaApp(ctx context.Context, card Card) error {
	token, err := f.tenantToken(ctx)
	if err != nil {
		return err
	}
	chatID, err := f.resolveChatID(ctx, token)
	if err != nil {
		return err
	}
	content, err := json.Marshal(buildInteractive(card))
	if err != nil {
		return &errNoRetry{err}
	}
	body := map[string]any{
		"receive_id": chatID,
		"msg_type":   "interactive",
		"content":    string(content),
	}
	status, payload, err := f.postJSON(ctx,
		f.baseURL+"/open-apis/im/v1/messages?receive_id_type=chat_id",
		body, map[string]string{"Authorization": "Bearer " + token})
	if err != nil {
		return err
	}
	return checkEnvelope(status, payload)
}

// ---------------------------------------------------------- webhook mode

func (f *FeishuClient) sendViaWebhook(ctx context.Context, card Card) error {
	payload := map[string]any{
		"msg_type": "interactive",
		"card":     buildInteractive(card),
	}
	if f.webSign != "" {
		ts := f.now().Unix()
		payload["timestamp"] = strconv.FormatInt(ts, 10)
		payload["sign"] = Sign(f.webSign, ts)
	}
	status, payloadRaw, err := f.postJSON(ctx, f.webhook, payload, nil)
	if err != nil {
		return err
	}
	return checkEnvelope(status, payloadRaw)
}

// Sign implements the Feishu bot signature: base64 of HMAC-SHA256 with the
// "timestamp\nsecret" string as key over an empty message.
func Sign(secret string, timestamp int64) string {
	stringToSign := fmt.Sprintf("%d\n%s", timestamp, secret)
	mac := hmac.New(sha256.New, []byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

var severityTemplate = map[string]string{
	"critical": "red",
	"warning":  "orange",
	"info":     "blue",
}

func headerTemplate(severity string) string {
	if t, ok := severityTemplate[severity]; ok {
		return t
	}
	return "blue"
}

func buildInteractive(card Card) map[string]any {
	statusMark := "🔴"
	if card.Status == "resolved" {
		statusMark = "✅"
	}
	title := fmt.Sprintf("%s %s", statusMark, card.Title)
	fields := []any{
		shortField("服务", orDash(card.Service)),
		shortField("级别", orDash(card.Severity)),
		shortField("开始时间", orDash(card.StartsAt)),
		shortField("状态", orDash(card.Status)),
	}
	elements := []any{
		map[string]any{"tag": "div", "fields": fields},
		map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "lark_md",
				"content": fmt.Sprintf("**摘要**\n%s", orDash(card.Summary)),
			},
		},
		map[string]any{"tag": "hr"},
	}
	if card.DeepLink != "" {
		elements = append(elements, map[string]any{
			"tag": "action",
			"actions": []any{map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "查看告警"},
				"type": "primary",
				"url":  card.DeepLink,
			}},
		})
	}
	return map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": headerTemplate(card.Severity),
			"title":    map[string]any{"tag": "plain_text", "content": title},
		},
		"elements": elements,
	}
}

func shortField(name, value string) map[string]any {
	return map[string]any{
		"is_short": true,
		"text": map[string]any{
			"tag":     "lark_md",
			"content": fmt.Sprintf("**%s**\n%s", name, value),
		},
	}
}

func orDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "-"
	}
	return v
}
