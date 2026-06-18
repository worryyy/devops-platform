package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/platformerr"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/responses"
	"github.com/worryyy/devops-platform/platform/server/internal/queue"
	"github.com/worryyy/devops-platform/platform/server/internal/release"
)

type testStore struct {
	acquireErr error
	releases   map[string]release.Release
	events     map[string][]release.Event
}

func (s *testStore) CreateRelease(ctx context.Context, input release.CreateReleaseInput) error {
	if s.releases == nil {
		s.releases = map[string]release.Release{}
	}
	s.releases[input.ID] = release.Release{
		ID:          input.ID,
		ServiceName: input.ServiceName,
		Environment: input.Environment,
		Branch:      input.Branch,
		Status:      release.StatusRequested,
		Operator:    input.Operator,
	}
	return nil
}

func (s *testStore) GetRelease(ctx context.Context, id string) (release.Release, error) {
	if item, ok := s.releases[id]; ok {
		return item, nil
	}
	return release.Release{}, platformerr.ErrNotFound
}

func (s *testStore) ListReleasesByService(ctx context.Context, serviceName string, limit int) ([]release.Release, error) {
	var releases []release.Release
	for _, item := range s.releases {
		if item.ServiceName == serviceName {
			releases = append(releases, item)
		}
	}
	return releases, nil
}

func (s *testStore) UpdateReleaseStatus(ctx context.Context, id string, status release.Status, errorMessage *string) error {
	item, ok := s.releases[id]
	if !ok {
		return platformerr.ErrNotFound
	}
	item.Status = status
	s.releases[id] = item
	return nil
}

func (s *testStore) UpdateReleaseStatusIfCurrent(ctx context.Context, id string, from release.Status, to release.Status) (bool, error) {
	item, ok := s.releases[id]
	if !ok {
		return false, platformerr.ErrNotFound
	}
	if item.Status != from {
		return false, nil
	}
	item.Status = to
	s.releases[id] = item
	return true, nil
}

func (s *testStore) SetJenkinsBuild(ctx context.Context, id, jobName string, buildNumber int) error {
	return nil
}

func (s *testStore) SetGitOpsRevision(ctx context.Context, id, commitSHA string) error { return nil }

func (s *testStore) SetImage(ctx context.Context, id, repo, tag, digest string) error { return nil }

func (s *testStore) AddEvent(ctx context.Context, releaseID string, status release.Status, message string, detail map[string]interface{}) error {
	if s.events == nil {
		s.events = map[string][]release.Event{}
	}
	s.events[releaseID] = append(s.events[releaseID], release.Event{
		ReleaseID: releaseID,
		Status:    status,
		Message:   message,
		Detail:    detail,
	})
	return nil
}

func (s *testStore) ListEvents(ctx context.Context, releaseID string) ([]release.Event, error) {
	return s.events[releaseID], nil
}

func (s *testStore) AcquireReleaseLock(ctx context.Context, serviceName, environment, releaseID string, ttl time.Duration) error {
	return s.acquireErr
}

func (s *testStore) ExtendReleaseLock(ctx context.Context, releaseID string, ttl time.Duration) error {
	return nil
}

func (s *testStore) ReleaseReleaseLock(ctx context.Context, serviceName, environment, releaseID string) error {
	return nil
}

type testPublisher struct {
	err error
}

func (p testPublisher) PublishReleaseRequested(ctx context.Context, message queue.ReleaseMessage) error {
	return p.err
}

type testReadiness struct {
	err error
}

func (r testReadiness) Ping(ctx context.Context) error {
	return r.err
}

func TestServicesRouteIsOwnedByCatalogModule(t *testing.T) {
	router := newTestRouter(t, &testStore{}, testPublisher{}, nil)

	recorder := perform(router, http.MethodGet, "/api/services", nil)

	assertHTTPStatus(t, recorder, http.StatusNotFound)
}

func TestGetServiceRouteIsOwnedByCatalogModule(t *testing.T) {
	router := newTestRouter(t, &testStore{}, testPublisher{}, nil)

	recorder := perform(router, http.MethodGet, "/api/services/missing", nil)

	assertHTTPStatus(t, recorder, http.StatusNotFound)
}

func TestCreateReleaseReturnsAcceptedUnifiedResponse(t *testing.T) {
	router := newTestRouter(t, &testStore{}, testPublisher{}, nil)
	body := []byte(`{"service":"forum-api","environment":"dev","branch":"main","operator":"worryyy"}`)

	recorder := perform(router, http.MethodPost, "/api/releases", body)

	assertHTTPStatus(t, recorder, http.StatusAccepted)
	response := decodeAPIResponse(t, recorder)
	if response.Code != http.StatusAccepted {
		t.Fatalf("Code = %d, want %d", response.Code, http.StatusAccepted)
	}
	data := responseData(t, response)
	if data["release_id"] == "" {
		t.Fatalf("release_id is empty")
	}
	if data["status"] != string(release.StatusQueued) {
		t.Fatalf("status = %#v, want %q", data["status"], release.StatusQueued)
	}
}

func TestCreateReleaseInvalidJSONReturnsParamError(t *testing.T) {
	router := newTestRouter(t, &testStore{}, testPublisher{}, nil)

	recorder := perform(router, http.MethodPost, "/api/releases", []byte(`{"service":`))

	assertHTTPStatus(t, recorder, http.StatusBadRequest)
	response := decodeAPIResponse(t, recorder)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("Code = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if response.Message != "invalid request body" {
		t.Fatalf("Message = %q", response.Message)
	}
}

func TestCreateReleaseConflictReturns409(t *testing.T) {
	router := newTestRouter(t, &testStore{acquireErr: platformerr.ErrReleaseLockHeld}, testPublisher{}, nil)
	body := []byte(`{"service":"forum-api","environment":"dev","branch":"main","operator":"worryyy"}`)

	recorder := perform(router, http.MethodPost, "/api/releases", body)

	assertHTTPStatus(t, recorder, http.StatusConflict)
	response := decodeAPIResponse(t, recorder)
	if response.Code != http.StatusConflict {
		t.Fatalf("Code = %d, want %d", response.Code, http.StatusConflict)
	}
	if response.Message != "release already running for service and environment" {
		t.Fatalf("Message = %q", response.Message)
	}
}

func TestReleaseEventsEmptyListUsesArray(t *testing.T) {
	store := &testStore{releases: map[string]release.Release{
		"rel-1": {
			ID:          "rel-1",
			ServiceName: "forum-api",
			Environment: "dev",
			Branch:      "main",
			Status:      release.StatusQueued,
			Operator:    "worryyy",
		},
	}}
	router := newTestRouter(t, store, testPublisher{}, nil)

	recorder := perform(router, http.MethodGet, "/api/releases/rel-1/events", nil)

	assertHTTPStatus(t, recorder, http.StatusOK)
	response := decodeAPIResponse(t, recorder)
	data, ok := response.Data.([]interface{})
	if !ok {
		t.Fatalf("Data = %#v, want array", response.Data)
	}
	if len(data) != 0 {
		t.Fatalf("Data length = %d, want 0", len(data))
	}
}

func TestHealthAndReadyzKeepOriginalShape(t *testing.T) {
	router := newTestRouter(t, &testStore{}, testPublisher{}, nil)

	health := perform(router, http.MethodGet, "/healthz", nil)
	ready := perform(router, http.MethodGet, "/readyz", nil)

	assertHTTPStatus(t, health, http.StatusOK)
	assertHTTPStatus(t, ready, http.StatusOK)
	var healthBody map[string]interface{}
	if err := json.Unmarshal(health.Body.Bytes(), &healthBody); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if _, ok := healthBody["code"]; ok {
		t.Fatalf("healthz should not use unified response: %s", health.Body.String())
	}
	var readyBody map[string]interface{}
	if err := json.Unmarshal(ready.Body.Bytes(), &readyBody); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if _, ok := readyBody["code"]; ok {
		t.Fatalf("readyz should not use unified response: %s", ready.Body.String())
	}
}

func TestReadyzFailureKeepsOriginalShape(t *testing.T) {
	router := newTestRouter(t, &testStore{}, testPublisher{}, errors.New("database down"))

	recorder := perform(router, http.MethodGet, "/readyz", nil)

	assertHTTPStatus(t, recorder, http.StatusServiceUnavailable)
	var body map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode readyz: %v", err)
	}
	if body["status"] != "not_ready" {
		t.Fatalf("status = %#v", body["status"])
	}
	if _, ok := body["code"]; ok {
		t.Fatalf("readyz should not use unified response: %s", recorder.Body.String())
	}
}

func newTestRouter(t *testing.T, store *testStore, publisher testPublisher, readinessErr error) http.Handler {
	t.Helper()
	service := release.NewService(testCatalog(), store, publisher, time.Hour)
	return NewRouter(RouterDependencies{Releases: service, Store: testReadiness{err: readinessErr}})
}

func testCatalog() catalog.Catalog {
	return catalog.Catalog{
		Version: "v1",
		Services: []catalog.Service{
			{
				Name:        "forum-api",
				DisplayName: "Forum API",
				Owner:       "worryyy",
				Environments: []catalog.Environment{
					{
						Name:      "dev",
						Namespace: "app",
						BranchPolicy: catalog.BranchPolicy{
							DefaultBranch:   "main",
							AllowedBranches: []string{"main", "develop", "feature/*"},
						},
						Git: catalog.GitConfig{
							Repo:       "https://github.com/worryyy/forum-app.git",
							ChartPath:  "k3s/charts/forum-api",
							ValuesFile: "k3s/helm-values/workloads/forum-api-business.yaml",
						},
						Image: catalog.ImageConfig{
							Repository: "repo",
							TagPolicy:  "git-sha",
						},
						Jenkins: catalog.JenkinsConfig{
							Mode:    "legacy",
							JobName: "forum-api-pipeline",
						},
						ArgoCD: catalog.ArgoCDConfig{
							Application: "forum-api",
							Namespace:   "argocd",
						},
						Kubernetes: catalog.KubernetesConfig{
							Namespace:  "app",
							Deployment: "forum-api",
							Service:    "forum-api",
							Container:  "forum-api",
						},
						Health: catalog.HealthConfig{
							HealthPath: "/healthz",
							ReadyPath:  "/readyz",
						},
					},
				},
			},
		},
	}
}

func perform(handler http.Handler, method, path string, body []byte) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

func assertHTTPStatus(t *testing.T, recorder *httptest.ResponseRecorder, status int) {
	t.Helper()
	if recorder.Code != status {
		t.Fatalf("HTTP status = %d, want %d; body = %s", recorder.Code, status, recorder.Body.String())
	}
}

func decodeAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) responses.Response {
	t.Helper()
	var response responses.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v; body = %s", err, recorder.Body.String())
	}
	return response
}

func responseData(t *testing.T, response responses.Response) map[string]interface{} {
	t.Helper()
	data, ok := response.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("Data = %#v, want object", response.Data)
	}
	return data
}
