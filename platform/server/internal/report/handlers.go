package report

import (
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/worryyy/devops-platform/platform/server/internal/api"
	"github.com/worryyy/devops-platform/platform/server/internal/auth"
	"github.com/worryyy/devops-platform/platform/server/internal/config"
)

// Handlers wires the report routes: authed listing/trigger/preview plus the
// public runner completion webhook and the dual-auth stats endpoint.
type Handlers struct {
	Service *Service
	Store   ReportStoreAPI
	Objects ObjectStore
	DB      *gorm.DB
	Cfg     config.Config
	Now     func() time.Time
}

func (h Handlers) Register(router *gin.RouterGroup) {
	router.GET("/reports", h.list)
	router.POST("/reports/trigger", api.RequireRole("admin"), h.trigger)
	router.GET("/reports/:id/preview", h.preview)
	router.GET("/reports/:id/url", h.presign)
}

// RegisterPublic mounts the runner-facing endpoints on the public /api group:
// stats accepts the runner's webhook secret, the completion webhook requires
// it.
func (h Handlers) RegisterPublic(router *gin.RouterGroup) {
	router.GET("/reports/stats", h.stats)
	router.POST("/webhooks/report-runner", h.runnerWebhook)
}

func (h Handlers) list(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	runs, err := h.Store.List(c.Request.Context(), limit)
	if err != nil {
		api.RespFail(c, err)
		return
	}
	api.RespData(c, runs)
}

type triggerRequest struct {
	// Period like 2026-W38; empty runs the previous week.
	Period string `json:"period"`
}

func (h Handlers) trigger(c *gin.Context) {
	var req triggerRequest
	_ = c.ShouldBindJSON(&req)
	var run *ReportRun
	var err error
	if req.Period == "" {
		run, err = h.Service.RunWeekly(c.Request.Context(), h.Now())
	} else {
		run, err = h.Service.RunForPeriod(c.Request.Context(), req.Period)
	}
	if err != nil {
		api.RespFail(c, api.ErrorErr(503, err.Error()))
		return
	}
	api.RespData(c, run)
}

// stats serves the platform aggregates the python runner embeds in the
// report. The runner Job carries the webhook secret; humans carry a JWT.
func (h Handlers) stats(c *gin.Context) {
	if !h.authorizeRunner(c) {
		api.RespFail(c, api.ErrorErr(401, "unauthorized"))
		return
	}
	start, err := time.Parse(time.RFC3339, c.Query("start"))
	if err != nil {
		api.RespFail(c, api.ErrorErr(400, "start (RFC3339) required"))
		return
	}
	end, err := time.Parse(time.RFC3339, c.Query("end"))
	if err != nil {
		api.RespFail(c, api.ErrorErr(400, "end (RFC3339) required"))
		return
	}
	stats, err := CollectStats(c.Request.Context(), h.DB, start, end)
	if err != nil {
		api.RespFail(c, err)
		return
	}
	api.RespData(c, stats)
}

func (h Handlers) authorizeRunner(c *gin.Context) bool {
	if h.Cfg.WebhookSecret != "" && c.GetHeader("X-Platform-Webhook") == h.Cfg.WebhookSecret {
		return true
	}
	header := c.GetHeader("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok {
		return false
	}
	_, err := auth.ParseToken(h.Cfg.JWTSecret, token)
	return err == nil
}

type runnerCallback struct {
	RunID      int64  `json:"runId" binding:"required"`
	Status     string `json:"status" binding:"required"`
	ObjectPath string `json:"objectPath"`
	Error      string `json:"error"`
}

func (h Handlers) runnerWebhook(c *gin.Context) {
	if h.Cfg.WebhookSecret != "" && c.GetHeader("X-Platform-Webhook") != h.Cfg.WebhookSecret {
		api.RespFail(c, api.ErrorErr(401, "bad webhook secret"))
		return
	}
	var req runnerCallback
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespFail(c, api.ErrorErr(400, "runId and status required"))
		return
	}
	if req.Status != "success" && req.Status != "failed" {
		api.RespFail(c, api.ErrorErr(400, "status must be success|failed"))
		return
	}
	if err := h.Store.SetResult(c.Request.Context(), req.RunID, req.Status, req.ObjectPath, req.Error); err != nil {
		api.RespFail(c, mapReportError(err))
		return
	}
	api.RespData(c, gin.H{"ok": true})
}

// preview streams the stored HTML through the API so the iframe never needs
// a cluster-internal MinIO address.
func (h Handlers) preview(c *gin.Context) {
	_, rc, ok := h.openRunObject(c)
	if !ok {
		return
	}
	defer rc.Close()
	c.Header("Content-Type", "text/html; charset=utf-8")
	_, _ = io.Copy(c.Writer, rc)
}

func (h Handlers) presign(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		api.RespFail(c, api.ErrorErr(400, "invalid id"))
		return
	}
	target, err := h.Store.Get(c.Request.Context(), id)
	if err != nil {
		api.RespFail(c, mapReportError(err))
		return
	}
	if h.Objects == nil || target.ObjectPath == "" {
		api.RespFail(c, api.ErrorErr(404, "report object not available"))
		return
	}
	urlStr, err := h.Objects.PresignGET(c.Request.Context(), h.Cfg.ReportBucket, target.ObjectPath, time.Hour)
	if err != nil {
		api.RespFail(c, api.ErrorErr(502, "presign failed: "+err.Error()))
		return
	}
	api.RespData(c, gin.H{"url": urlStr})
}

func (h Handlers) openRunObject(c *gin.Context) (ReportRun, io.ReadCloser, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		api.RespFail(c, api.ErrorErr(400, "invalid id"))
		return ReportRun{}, nil, false
	}
	run, err := h.Store.Get(c.Request.Context(), id)
	if err != nil {
		api.RespFail(c, mapReportError(err))
		return ReportRun{}, nil, false
	}
	if run.ObjectPath == "" {
		api.RespFail(c, api.ErrorErr(404, "report has no stored object"))
		return run, nil, false
	}
	if h.Objects == nil {
		api.RespFail(c, api.ErrorErr(503, "object store not configured"))
		return run, nil, false
	}
	rc, _, err := h.Objects.Open(c.Request.Context(), h.Cfg.ReportBucket, run.ObjectPath)
	if err != nil {
		api.RespFail(c, api.ErrorErr(502, "open object: "+err.Error()))
		return run, nil, false
	}
	return run, rc, true
}

func mapReportError(err error) error {
	if err == ErrRunNotFound {
		return api.ErrorErr(404, "report run not found")
	}
	return err
}
