package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUnknownAPIRouteIsNotRegisteredByBaseRouter(t *testing.T) {
	router := newTestRouter()

	recorder := perform(router, http.MethodGet, "/api/unknown", nil)

	assertHTTPStatus(t, recorder, http.StatusNotFound)
}

func TestRolloutProberRoutesAreRemoved(t *testing.T) {
	router := newTestRouter()

	assertHTTPStatus(t, perform(router, http.MethodPost, "/api/rollout-prober/run", []byte(`{}`)), http.StatusNotFound)
	assertHTTPStatus(t, perform(router, http.MethodGet, "/metrics", nil), http.StatusNotFound)
}

func TestHealthAndReadyzKeepOriginalShape(t *testing.T) {
	router := newTestRouter()

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

func newTestRouter() http.Handler {
	return NewRouter()
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

func assertHTTPStatus(t *testing.T, recorder *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if recorder.Code != expected {
		t.Fatalf("status = %d, want %d, body = %s", recorder.Code, expected, recorder.Body.String())
	}
}
