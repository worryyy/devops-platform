package obs

import (
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/worryyy/devops-platform/platform/server/internal/api"
)

// Handlers mounts the /logs surface (P4-1.3). Querier nil → 503 per request:
// a missing ClickHouse must degrade the log page, not the whole API.
type Handlers struct {
	Querier Querier
	Logger  *slog.Logger
}

// Register wires the authed routes.
func (h Handlers) Register(router *gin.RouterGroup) {
	router.GET("/logs", h.logs)
	router.GET("/logs/patterns", h.patterns)
	router.GET("/logs/histogram", h.histogram)
}

func (h Handlers) ready() bool { return h.Querier != nil }

func (h Handlers) logs(c *gin.Context) {
	if !h.ready() {
		api.RespFail(c, api.ErrorErr(http.StatusServiceUnavailable, "clickhouse not configured"))
		return
	}
	f, ok := parseFilter(c)
	if !ok {
		return
	}
	rows, err := h.Querier.SearchLogs(c.Request.Context(), f)
	if err != nil {
		h.fail(c, err)
		return
	}
	api.RespData(c, rows)
}

func (h Handlers) patterns(c *gin.Context) {
	if !h.ready() {
		api.RespFail(c, api.ErrorErr(http.StatusServiceUnavailable, "clickhouse not configured"))
		return
	}
	f, ok := parseFilter(c)
	if !ok {
		return
	}
	patterns, err := h.Querier.Patterns(c.Request.Context(), f)
	if err != nil {
		h.fail(c, err)
		return
	}
	api.RespData(c, patterns)
}

func (h Handlers) histogram(c *gin.Context) {
	if !h.ready() {
		api.RespFail(c, api.ErrorErr(http.StatusServiceUnavailable, "clickhouse not configured"))
		return
	}
	f, ok := parseFilter(c)
	if !ok {
		return
	}
	points, err := h.Querier.Histogram(c.Request.Context(), f)
	if err != nil {
		h.fail(c, err)
		return
	}
	api.RespData(c, points)
}

func parseFilter(c *gin.Context) (LogsFilter, bool) {
	// 显式解析：含 `;` 等非法分隔符的 query 会被 net/url 整体拒掉，而
	// gin 的 c.Query 静默返回空值——那会把"注入尝试"放宽成"无条件查询"。
	if _, err := url.ParseQuery(c.Request.URL.RawQuery); err != nil {
		api.RespFail(c, api.ErrorErr(http.StatusBadRequest, "malformed query string"))
		return LogsFilter{}, false
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	f := LogsFilter{
		Service: c.Query("service"),
		Level:   c.Query("level"),
		Query:   c.Query("q"),
		TraceID: c.Query("trace_id"),
		Limit:   limit,
	}
	var err error
	if v := c.Query("start"); v != "" {
		if f.Start, err = time.Parse(time.RFC3339, v); err != nil {
			api.RespFail(c, api.ErrorErr(http.StatusBadRequest, "start must be RFC3339"))
			return f, false
		}
	}
	if v := c.Query("end"); v != "" {
		if f.End, err = time.Parse(time.RFC3339, v); err != nil {
			api.RespFail(c, api.ErrorErr(http.StatusBadRequest, "end must be RFC3339"))
			return f, false
		}
	}
	if _, err := f.Validate(); err != nil {
		api.RespFail(c, api.ErrorErr(http.StatusBadRequest, err.Error()))
		return f, false
	}
	return f, true
}

func (h Handlers) fail(c *gin.Context, err error) {
	if h.Logger != nil {
		h.Logger.Error("obs handler failed", "path", c.FullPath(), "error", err)
	}
	api.RespFail(c, api.ErrorErr(http.StatusBadGateway, "clickhouse query failed"))
}
