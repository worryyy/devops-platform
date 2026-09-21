package grafanaproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
)

// capturedRequest remembers the last backend request.
type capturedRequest struct {
	mu  sync.Mutex
	req *http.Request
}

func (c *capturedRequest) store(r *http.Request) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.req = r
}

func (c *capturedRequest) get() *http.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.req
}

// serve mounts the proxy in front of a recording backend and returns a live
// URL. A real server is required: ReverseProxy's streaming path needs
// CloseNotifier support that httptest.ResponseRecorder lacks.
func serve(t *testing.T, opts Options) (*httptest.Server, *capturedRequest) {
	t.Helper()
	captured := &capturedRequest{}
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.store(r)
		_, _ = w.Write([]byte("grafana says hi"))
	}))
	t.Cleanup(backend.Close)

	gin.SetMode(gin.TestMode)
	opts.Target = backend.URL
	handler, err := Handler(opts)
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	router := gin.New()
	router.Any("/grafana/*path", handler)
	front := httptest.NewServer(router)
	t.Cleanup(front.Close)
	return front, captured
}

func TestProxyKeepsPrefixAndInjectsBearer(t *testing.T) {
	front, captured := serve(t, Options{Token: "sa-token"})

	resp, err := http.Get(front.URL + "/grafana/d/ecampus-sli/x")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != "grafana says hi" {
		t.Fatalf("status %d body %q", resp.StatusCode, body)
	}
	backend := captured.get()
	// serve_from_sub_path Grafana expects the FULL /grafana-prefixed path.
	if backend.URL.Path != "/grafana/d/ecampus-sli/x" {
		t.Fatalf("path forwarded as %q, want verbatim", backend.URL.Path)
	}
	if backend.Header.Get("Authorization") != "Bearer sa-token" {
		t.Fatalf("authorization %q", backend.Header.Get("Authorization"))
	}
}

func TestProxyBasicAuthFallback(t *testing.T) {
	front, captured := serve(t, Options{User: "admin", Password: "s3cret"})

	resp, err := http.Get(front.URL + "/grafana/api/health")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	user, pass, _ := captured.get().BasicAuth()
	if user != "admin" || pass != "s3cret" {
		t.Fatalf("basic auth %q/%q", user, pass)
	}
}

func TestProxyRedirectsBarePrefix(t *testing.T) {
	front, _ := serve(t, Options{Token: "t"})

	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(front.URL + "/grafana")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMovedPermanently || resp.Header.Get("Location") != "/grafana/" {
		t.Fatalf("bare prefix redirect: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
}
