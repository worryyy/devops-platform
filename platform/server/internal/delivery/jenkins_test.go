package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTriggerBuildParsesQueueItem(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/ecampus-pipeline/buildWithParameters" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if r.Form.Get("SOURCE_REPO") == "" || r.Form.Get("SKIP_RELEASE") != "true" {
			t.Errorf("params = %v", r.Form)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "admin" || pass != "token" {
			t.Errorf("basic auth = %s:%s", user, pass)
		}
		w.Header().Set("Location", serverURLBase+"/queue/item/42/")
		w.WriteHeader(http.StatusCreated)
	}))
	// capture the base URL before the handler references it
	serverURLBase = server.URL

	client := NewJenkinsClient(server.URL, "admin", "token", "ecampus-pipeline")
	item, err := client.TriggerBuild(context.Background(), map[string]string{
		"SOURCE_REPO":  "https://github.com/worryyy/app-test.git",
		"SKIP_RELEASE": "true",
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if item != 42 {
		t.Fatalf("queue item = %d, want 42", item)
	}
	server.Close()
}

var serverURLBase string

func TestTriggerBuildRejectsNon201(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("forbidden"))
	}))
	defer server.Close()

	client := NewJenkinsClient(server.URL, "admin", "token", "ecampus-pipeline")
	if _, err := client.TriggerBuild(context.Background(), nil); err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestBuildStatusDecodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/ecampus-pipeline/7/api/json" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"number": 7, "result": "SUCCESS", "building": false,
			"timestamp": 1, "duration": 2, "url": "x",
		})
	}))
	defer server.Close()

	client := NewJenkinsClient(server.URL, "admin", "token", "ecampus-pipeline")
	info, err := client.BuildStatus(context.Background(), 7)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if info.Result != "SUCCESS" || info.Building {
		t.Fatalf("info = %+v", info)
	}
}

func TestMergeStagesIdempotent(t *testing.T) {
	existing := []Stage{
		{Name: "build", Status: "running"},
		{Name: "pr", Status: "queued"},
	}
	merged := MergeStages(existing, []Stage{{Name: "build", Status: "success", DurationMs: 100}})
	if len(merged) != 2 {
		t.Fatalf("len = %d, want 2 (same stage overwritten)", len(merged))
	}
	if merged[0].Status != "success" || merged[0].DurationMs != 100 {
		t.Fatalf("merged[0] = %+v", merged[0])
	}
	if merged[1].Name != "pr" {
		t.Fatalf("order not preserved: %+v", merged)
	}

	again := MergeStages(merged, []Stage{{Name: "deploy", Status: "success"}})
	if len(again) != 3 || again[2].Name != "deploy" {
		t.Fatalf("append failed: %+v", again)
	}
}

func TestMapBuildStatus(t *testing.T) {
	cases := []struct {
		info BuildInfo
		want string
	}{
		{BuildInfo{Building: true}, "running"},
		{BuildInfo{Result: "SUCCESS"}, "success"},
		{BuildInfo{Result: "FAILURE"}, "failed"},
		{BuildInfo{Result: "ABORTED"}, "failed"},
		{BuildInfo{}, ""},
	}
	for _, c := range cases {
		if got := mapBuildStatus(c.info); got != c.want {
			t.Errorf("mapBuildStatus(%+v) = %q, want %q", c.info, got, c.want)
		}
	}
}

func TestResolveQueueItemReturnsBuildNumberOnceExecutable(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/queue/item/9/api/json") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"_class":"hudson.model.Queue$LeftItem","cancelled":false,"why":"waiting for agent"}`))
			return
		}
		_, _ = w.Write([]byte(`{"_class":"hudson.model.Queue$LeftItem","cancelled":false,"executable":{"number":42}}`))
	}))
	defer server.Close()

	client := NewJenkinsClient(server.URL, "u", "t", "ecampus-pipeline")
	client.PollInterval = 5 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	build, err := client.ResolveQueueItem(ctx, 9)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if build != 42 {
		t.Fatalf("build = %d, want 42", build)
	}
	if calls != 2 {
		t.Fatalf("expected one poll before executable, got %d calls", calls)
	}
}

func TestResolveQueueItemCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cancelled":true}`))
	}))
	defer server.Close()

	client := NewJenkinsClient(server.URL, "u", "t", "ecampus-pipeline")
	client.PollInterval = time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.ResolveQueueItem(ctx, 3); err == nil {
		t.Fatal("cancelled item must error")
	}
}
