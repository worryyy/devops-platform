package obs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// P4-1.5: 查询构造器注入用例——任何用户输入要么被 Validate 拒绝，要么只
// 以绑定参数进 SQL，绝不拼接。

func TestValidateRejectsInjection(t *testing.T) {
	bad := []string{
		`'; DROP TABLE logs;--`,
		`user" OR 1=1--`,
		`a b`,
		`ECAMPUS-USER`,   // 大写不允许（服务名小写约定）
		`../etc/passwd`,
		"-leading-dash",
	}
	for _, svc := range bad {
		if _, err := (LogsFilter{Service: svc}).Validate(); err == nil {
			t.Errorf("service %q should be rejected", svc)
		}
	}
	badTrace := []string{`abc' OR '1'='1`, "zz\x00", strings.Repeat("a", 65)}
	for _, tr := range badTrace {
		if _, err := (LogsFilter{TraceID: tr}).Validate(); err == nil {
			t.Errorf("trace_id %q should be rejected", tr)
		}
	}
	if _, err := (LogsFilter{Level: "SILLY'; --"}).Validate(); err == nil {
		t.Error("level injection should be rejected")
	}
	if _, err := (LogsFilter{Query: strings.Repeat("x", 257)}).Validate(); err == nil {
		t.Error("oversized q should be rejected")
	}
}

func TestValidateNormalizes(t *testing.T) {
	f, err := LogsFilter{Level: "WARNING", Limit: 99999}.Validate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Level != "WARN" {
		t.Errorf("WARNING not normalized, got %q", f.Level)
	}
	if f.Limit != maxLimit {
		t.Errorf("limit not capped, got %d", f.Limit)
	}
	f, err = LogsFilter{}.Validate()
	if err != nil || f.Limit != defaultLimit {
		t.Errorf("default limit wrong: %d err=%v", f.Limit, err)
	}
	end := time.Now()
	start := end.Add(-time.Hour)
	if _, err := (LogsFilter{Start: end, End: start}).Validate(); err == nil {
		t.Error("end<start should be rejected")
	}
}

func TestBuildWhereNeverConcatenatesValues(t *testing.T) {
	f, err := LogsFilter{
		Service: "ecampus-user",
		Level:   "ERROR",
		Query:   `x' OR '1'='1`,
		TraceID: "0af7651916cd43dd8448eb211c80319c",
	}.Validate()
	if err != nil {
		t.Fatalf("valid filter rejected: %v", err)
	}
	where, params := buildLogsWhere(f)

	// 恶意值绝不能出现在 SQL 文本里——只能出现在绑定参数里
	if strings.Contains(where, "OR") || strings.Contains(where, "'") {
		t.Fatalf("value leaked into SQL: %s", where)
	}
	for _, frag := range []string{
		"service = {service: String}",
		"level = {level: String}",
		"positionCaseInsensitive(msg, {q: String}) > 0",
		"trace_id = {trace_id: String}",
	} {
		if !strings.Contains(where, frag) {
			t.Errorf("where missing fragment %q: %s", frag, where)
		}
	}
	if params["q"] != `x' OR '1'='1` || params["service"] != "ecampus-user" || params["level"] != "ERROR" {
		t.Errorf("bound params wrong: %+v", params)
	}
}

func TestHandlersDegradeWithoutCH(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := Handlers{Querier: nil}
	group := router.Group("/api", func(c *gin.Context) { c.Next() })
	h.Register(group)

	for _, path := range []string{"/api/logs", "/api/logs/patterns", "/api/logs/histogram"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: want 503 without clickhouse, got %d", path, rec.Code)
		}
	}
}

func TestHandlersPassFilterToQuerier(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var gotFilter LogsFilter
	fake := &fakeQuerier{
		search: func(ctx context.Context, f LogsFilter) ([]LogRow, error) {
			gotFilter = f
			return []LogRow{{Service: "ecampus-user", Level: "ERROR", Msg: "boom"}}, nil
		},
	}
	router := gin.New()
	h := Handlers{Querier: fake}
	h.Register(router.Group("/api"))

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/api/logs?service=ecampus-user&level=ERROR&trace_id=0af7651916cd43dd8448eb211c80319c&limit=50", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if gotFilter.Service != "ecampus-user" || gotFilter.Level != "ERROR" || gotFilter.Limit != 50 {
		t.Errorf("filter not passed through: %+v", gotFilter)
	}
	// 注入值在 handler 层就被 400 挡下，不触达 CH
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/logs?service=a';DROP", nil))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("injected service should 400, got %d", rec.Code)
	}
}

type fakeQuerier struct {
	search func(ctx context.Context, f LogsFilter) ([]LogRow, error)
}

func (f *fakeQuerier) SearchLogs(ctx context.Context, fl LogsFilter) ([]LogRow, error) {
	if f.search != nil {
		return f.search(ctx, fl)
	}
	return nil, nil
}
func (f *fakeQuerier) Patterns(ctx context.Context, fl LogsFilter) ([]PatternRow, error) { return nil, nil }
func (f *fakeQuerier) Histogram(ctx context.Context, fl LogsFilter) ([]HistogramPoint, error) {
	return nil, nil
}
