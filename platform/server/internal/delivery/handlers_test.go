package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/worryyy/devops-platform/platform/server/internal/api"
	"github.com/worryyy/devops-platform/platform/server/internal/auth"
	"github.com/worryyy/devops-platform/platform/server/internal/config"
)

type fakeJenkins struct {
	params     map[string]string
	buildID    int64
	triggerErr error
}

func (f *fakeJenkins) TriggerBuild(_ context.Context, params map[string]string) (int64, error) {
	if f.triggerErr != nil {
		return 0, f.triggerErr
	}
	f.params = params
	return f.buildID, nil
}

func (f *fakeJenkins) BuildStatus(_ context.Context, buildID int64) (BuildInfo, error) {
	return BuildInfo{Number: buildID, Result: "SUCCESS"}, nil
}

func (f *fakeJenkins) ResolveQueueItem(_ context.Context, queueItem int64) (int64, error) {
	return queueItem, nil
}

type fakeGitHub struct{ pr PullRequest }

func (f *fakeGitHub) CreateRevertPR(_ context.Context, _, _, _ string) (PullRequest, error) {
	return f.pr, nil
}

func newTestRouter(t *testing.T, cfg config.Config, store PipelineStoreAPI, jenkins JenkinsAPI, github GitHubAPI) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	h := Handlers{Store: store, Jenkins: jenkins, GitHub: github, Cfg: cfg}
	return api.NewAPIRouter(api.Deps{
		Logger:    discardLogger(),
		JWTSecret: testSecret,
		Auth: api.AuthHandlers{
			Users:  stubAuthenticator{},
			Secret: testSecret,
			Now:    time.Now,
		},
		Delivery: h,
	})
}

const testSecret = "delivery-test-secret"

type stubAuthenticator struct{}

func (stubAuthenticator) Authenticate(_ context.Context, username, password string) (auth.User, error) {
	if password != "right" {
		return auth.User{}, auth.ErrInvalidCredentials
	}
	if username == "admin" {
		return auth.User{Username: "admin", Role: "admin"}, nil
	}
	return auth.User{Username: username, Role: "viewer"}, nil
}

func loginAs(t *testing.T, router *gin.Engine, role string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login",
		bytes.NewReader(jsonBody(t, map[string]string{"username": role, "password": "right"})))
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct{ Token string } `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return resp.Data.Token
}

func jsonBody(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return raw
}

func doReq(t *testing.T, router *gin.Engine, method, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestWebhookRequiresSharedSecret(t *testing.T) {
	store := &memStore{runs: map[int64]*PipelineRun{1: {ID: 1, Service: "theme", JenkinsBuild: 9}}}
	cfg := config.Config{WebhookSecret: "s3cret"}
	router := newTestRouter(t, cfg, store, nil, nil)

	body := jsonBody(t, map[string]any{
		"build": 9, "service": "theme", "status": "success",
		"stages": []map[string]string{{"name": "build", "status": "success"}},
	})
	if rec := doReq(t, router, http.MethodPost, "/api/webhooks/jenkins", "", body); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no secret header: status = %d, want 401", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/jenkins", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Platform-Webhook", "s3cret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("with secret: status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestWebhookMergesStagesByService(t *testing.T) {
	store := &memStore{runs: map[int64]*PipelineRun{
		1: {ID: 1, Service: "theme", JenkinsBuild: 9, Status: "running"},
		2: {ID: 2, Service: "user", JenkinsBuild: 9, Status: "running"},
	}}
	router := newTestRouter(t, config.Config{}, store, nil, nil)

	post := func(v any) *httptest.ResponseRecorder {
		return doReq(t, router, http.MethodPost, "/api/webhooks/jenkins", "", jsonBody(t, v))
	}
	if rec := post(map[string]any{
		"build": 9, "service": "theme", "status": "success",
		"stages": []map[string]any{{"name": "build", "status": "success", "durationMs": 10}},
	}); rec.Code != http.StatusOK {
		t.Fatalf("theme webhook: %d %s", rec.Code, rec.Body.String())
	}
	if rec := post(map[string]any{
		"build": 9, "service": "user", "status": "failed",
		"stages": []map[string]any{{"name": "deploy", "status": "failed"}},
	}); rec.Code != http.StatusOK {
		t.Fatalf("user webhook: %d %s", rec.Code, rec.Body.String())
	}

	theme := store.runs[1]
	user := store.runs[2]
	if theme.Status != "success" || len(theme.Stages.Stages) != 1 || theme.Stages.Stages[0].Name != "build" {
		t.Fatalf("theme run = %+v", theme)
	}
	if theme.FinishedAt == nil {
		t.Fatal("finished_at must be set on success")
	}
	if user.Status != "failed" || user.Stages.Stages[0].Name != "deploy" {
		t.Fatalf("user run = %+v", user)
	}
}

func TestTriggerPermissionAndJenkinsCall(t *testing.T) {
	store := &memStore{runs: map[int64]*PipelineRun{}}
	jenkins := &fakeJenkins{buildID: 77}
	cfg := config.Config{JenkinsUser: "admin", JenkinsToken: "t"}
	router := newTestRouter(t, cfg, store, jenkins, nil)

	admin := loginAs(t, router, "admin")
	viewer := loginAs(t, router, "viewer")
	body := jsonBody(t, map[string]any{"services": []string{"theme"}, "skipRelease": true})

	if rec := doReq(t, router, http.MethodPost, "/api/pipelines/trigger", viewer, body); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer trigger: %d, want 403", rec.Code)
	}
	if rec := doReq(t, router, http.MethodPost, "/api/pipelines/trigger", admin, body); rec.Code != http.StatusOK {
		t.Fatalf("admin trigger: %d %s", rec.Code, rec.Body.String())
	}
	if jenkins.params["SKIP_RELEASE"] != "true" {
		t.Fatalf("params = %v", jenkins.params)
	}
	if len(store.runs) != 1 || store.runs[1].JenkinsBuild != 77 || store.runs[1].TriggeredBy != "admin" {
		t.Fatalf("store runs = %+v", store.runs)
	}
}

func TestTriggerFailsCleanlyWhenJenkinsDown(t *testing.T) {
	store := &memStore{runs: map[int64]*PipelineRun{}}
	jenkins := &fakeJenkins{triggerErr: context.DeadlineExceeded}
	cfg := config.Config{JenkinsUser: "admin", JenkinsToken: "t"}
	router := newTestRouter(t, cfg, store, jenkins, nil)

	admin := loginAs(t, router, "admin")
	rec := doReq(t, router, http.MethodPost, "/api/pipelines/trigger", admin,
		jsonBody(t, map[string]any{"services": []string{"theme"}}))
	if rec.Code != http.StatusOK {
		t.Fatalf("trigger should still 200 with failed row: %d %s", rec.Code, rec.Body.String())
	}
	if store.runs[1].Status != "failed" {
		t.Fatalf("run status = %s, want failed", store.runs[1].Status)
	}
}

func TestRevertRequiresAdminAndRevision(t *testing.T) {
	store := &memStore{runs: map[int64]*PipelineRun{
		1: {ID: 1, Service: "theme", JenkinsBuild: 5, GitRevision: "abc123", Status: "failed"},
	}}
	router := newTestRouter(t, config.Config{GitHubToken: "gh"}, store, nil, &fakeGitHub{pr: PullRequest{URL: "https://github.com/worryyy/app-test/pull/9", Number: 9}})

	admin := loginAs(t, router, "admin")
	viewer := loginAs(t, router, "viewer")
	if rec := doReq(t, router, http.MethodPost, "/api/pipelines/1/revert", viewer, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer revert: %d, want 403", rec.Code)
	}
	if rec := doReq(t, router, http.MethodPost, "/api/pipelines/1/revert", admin, nil); rec.Code != http.StatusOK {
		t.Fatalf("admin revert: %d %s", rec.Code, rec.Body.String())
	}
	if store.runs[1].RevertPRURL == "" {
		t.Fatal("revert PR url must be stored")
	}

	store.runs[1].GitRevision = ""
	if rec := doReq(t, router, http.MethodPost, "/api/pipelines/1/revert", admin, nil); rec.Code != http.StatusConflict {
		t.Fatalf("revert without revision: %d, want 409", rec.Code)
	}
}
