package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/worryyy/devops-platform/platform/server/internal/auth"
	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
)

const testSecret = "test-secret-0123456789"

// fakeAuthenticator fakes *auth.UserStore for handler tests.
type fakeAuthenticator struct {
	users map[string]auth.User
}

func (f fakeAuthenticator) Authenticate(_ context.Context, username, password string) (auth.User, error) {
	user, ok := f.users[username]
	if !ok || password != "right-password" {
		return auth.User{}, auth.ErrInvalidCredentials
	}
	return user, nil
}

// fakeServiceStore fakes catalog.Store.
type fakeServiceStore struct {
	services []catalog.Service
}

func (f *fakeServiceStore) List(_ context.Context, _ string) ([]catalog.ServiceSummary, error) {
	summaries := make([]catalog.ServiceSummary, 0, len(f.services))
	for _, s := range f.services {
		summaries = append(summaries, catalog.ServiceSummary{
			Name: s.Name, DisplayName: s.DisplayName, Owner: s.Owner,
			Kind: s.Kind, EnvironmentCount: len(s.Environments),
		})
	}
	return summaries, nil
}

func (f *fakeServiceStore) Get(_ context.Context, name string) (catalog.Service, error) {
	for _, s := range f.services {
		if s.Name == name {
			return s, nil
		}
	}
	return catalog.Service{}, catalog.ErrNotFound
}

func (f *fakeServiceStore) Update(_ context.Context, name string, displayName, _ *string, _ *catalog.SLIPolicy) (catalog.Service, error) {
	for i, s := range f.services {
		if s.Name == name {
			if displayName != nil {
				f.services[i].DisplayName = *displayName
			}
			return f.services[i], nil
		}
	}
	return catalog.Service{}, catalog.ErrNotFound
}

// fakeImporter fakes catalog.Store's ImportFromYAML.
type fakeImporter struct{ count int }

func (f *fakeImporter) ImportFromYAML(_ context.Context, _ string) (int, error) {
	f.count++
	return 13, nil
}

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestAPIRouter(role string) (*gin.Engine, *fakeServiceStore) {
	gin.SetMode(gin.TestMode)
	store := &fakeServiceStore{services: []catalog.Service{
		{Name: "theme", DisplayName: "Theme", Owner: "team-a", Kind: "service",
			Environments: []catalog.Environment{{Name: "dev"}, {Name: "staging"}}},
	}}
	router := NewAPIRouter(Deps{
		Logger:    testLogger(),
		JWTSecret: testSecret,
		Auth: AuthHandlers{
			Users: fakeAuthenticator{users: map[string]auth.User{
				"admin":  {Username: "admin", Role: "admin"},
				"viewer": {Username: "viewer", Role: "viewer"},
			}},
			Secret: testSecret,
			Now:    func() time.Time { return time.Now() },
		},
		Services: ServicesHandlers{Catalog: store},
		Catalog:  CatalogHandlers{Catalog: &fakeImporter{}},
	})
	return router, store
}

func doWithRole(t *testing.T, router http.Handler, role, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if role != "" {
		token, err := auth.SignToken(testSecret, "tester", role, time.Now())
		if err != nil {
			t.Fatalf("sign token: %v", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) Response {
	t.Helper()
	var resp Response
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, rec.Body.String())
	}
	return resp
}

func TestLoginIssuesTokenAndMeReturnsIdentity(t *testing.T) {
	router, _ := newTestAPIRouter("")

	rec := doWithRole(t, router, "", http.MethodPost, "/api/auth/login",
		map[string]string{"username": "admin", "password": "right-password"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	envelope := decodeEnvelope(t, rec)
	payload, _ := json.Marshal(envelope.Data)
	var login loginResponse
	if err := json.Unmarshal(payload, &login); err != nil {
		t.Fatalf("decode login data: %v", err)
	}
	if login.Token == "" || login.User.Role != "admin" {
		t.Fatalf("unexpected login payload: %+v", login)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+login.Token)
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status = %d", rec.Code)
	}
}

func TestLoginRejectsWrongPassword(t *testing.T) {
	router, _ := newTestAPIRouter("")
	rec := doWithRole(t, router, "", http.MethodPost, "/api/auth/login",
		map[string]string{"username": "admin", "password": "wrong"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAPIRequiresBearerToken(t *testing.T) {
	router, _ := newTestAPIRouter("")
	rec := doWithRole(t, router, "", http.MethodGet, "/api/services", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.Header.Set("Authorization", "Bearer not-a-jwt")
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("garbage token status = %d, want 401", rec2.Code)
	}
}

func TestServicesListAndDetail(t *testing.T) {
	router, _ := newTestAPIRouter("viewer")

	rec := doWithRole(t, router, "viewer", http.MethodGet, "/api/services", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d body=%s", rec.Code, rec.Body.String())
	}
	envelope := decodeEnvelope(t, rec)
	var list []catalog.ServiceSummary
	raw, _ := json.Marshal(envelope.Data)
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].EnvironmentCount != 2 {
		t.Fatalf("unexpected list: %+v", list)
	}

	rec = doWithRole(t, router, "viewer", http.MethodGet, "/api/services/theme", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("detail status = %d", rec.Code)
	}

	rec = doWithRole(t, router, "viewer", http.MethodGet, "/api/services/missing", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want 404", rec.Code)
	}
}

func TestUpdateServiceRequiresAdmin(t *testing.T) {
	router, _ := newTestAPIRouter("viewer")

	rec := doWithRole(t, router, "viewer", http.MethodPut, "/api/services/theme",
		map[string]string{"displayName": "Renamed"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer put status = %d, want 403", rec.Code)
	}

	rec = doWithRole(t, router, "admin", http.MethodPut, "/api/services/theme",
		map[string]string{"displayName": "Renamed"})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin put status = %d body=%s", rec.Code, rec.Body.String())
	}
	var svc catalog.Service
	envelope := decodeEnvelope(t, rec)
	raw, _ := json.Marshal(envelope.Data)
	if err := json.Unmarshal(raw, &svc); err != nil {
		t.Fatalf("decode updated service: %v", err)
	}
	if svc.DisplayName != "Renamed" {
		t.Fatalf("displayName = %q, want Renamed", svc.DisplayName)
	}
}

func TestCatalogImportRequiresAdmin(t *testing.T) {
	router, _ := newTestAPIRouter("viewer")

	rec := doWithRole(t, router, "viewer", http.MethodPost, "/api/catalog/import",
		map[string]string{"path": "configs/service-catalog.yaml"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("viewer import status = %d, want 403", rec.Code)
	}

	rec = doWithRole(t, router, "admin", http.MethodPost, "/api/catalog/import",
		map[string]string{"path": "configs/service-catalog.yaml"})
	if rec.Code != http.StatusOK {
		t.Fatalf("admin import status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestExpiredTokenIsRejected(t *testing.T) {
	router, _ := newTestAPIRouter("")
	token, err := auth.SignToken(testSecret, "tester", "viewer", time.Now().Add(-13*time.Hour))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired token status = %d, want 401", rec.Code)
	}
}

func TestJWTRoundTrip(t *testing.T) {
	token, err := auth.SignToken(testSecret, "alice", "admin", time.Now())
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	claims, err := auth.ParseToken(testSecret, token)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Username != "alice" || claims.Role != "admin" {
		t.Fatalf("claims = %+v", claims)
	}
	if _, err := auth.ParseToken("other-secret", token); err == nil {
		t.Fatal("wrong secret must fail")
	}
	if _, err := auth.ParseToken(testSecret, "garbage"); err == nil {
		t.Fatal("garbage must fail")
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	hash, err := auth.HashPassword("s3cret-password")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if !auth.CheckPassword(hash, "s3cret-password") {
		t.Fatal("correct password must verify")
	}
	if auth.CheckPassword(hash, "wrong") {
		t.Fatal("wrong password must not verify")
	}
}
