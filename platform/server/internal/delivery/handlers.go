package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/worryyy/devops-platform/platform/server/internal/api"
	"github.com/worryyy/devops-platform/platform/server/internal/config"
)

// Deps wires the delivery routes.
type Handlers struct {
	Store   PipelineStoreAPI
	Jenkins JenkinsAPI
	GitHub  GitHubAPI
	Argo    ArgocdReader
	Cfg     config.Config
	// Recorder is the optional ClickHouse events mirror (P4 事件双写):
	// release lifecycle events (kind=release) feed 周报/P5 复盘. nil = off.
	Recorder EventRecorder
}

// EventRecorder mirrors platform events into ClickHouse (satisfied by
// *obs.Client). Consumer-side interface, same shape as alertgw's.
type EventRecorder interface {
	RecordEvent(ctx context.Context, kind, source string, payload []byte) error
}

const releaseEventSource = "platform-server"

// recordRelease is best-effort: PG keeps the authoritative pipeline_runs row.
func (h Handlers) recordRelease(ctx context.Context, payload map[string]any) {
	if h.Recorder == nil {
		return
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	recCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if err := h.Recorder.RecordEvent(recCtx, "release", releaseEventSource, raw); err != nil {
		h.logger().Warn("release event mirror failed", "error", err)
	}
}

// ArgocdReader supplies GitOps sync state for the detail page card.
type ArgocdReader interface {
	Get(ctx context.Context, name string) (ArgoAppStatus, error)
}

// ArgoAppStatus is the transport shape for k8sargo.AppStatus.
type ArgoAppStatus struct {
	Name        string `json:"name"`
	SyncStatus  string `json:"syncStatus"`
	Health      string `json:"health"`
	SyncVersion string `json:"syncVersion"`
}

// JenkinsAPI is the client surface handlers need (interface for tests).
type JenkinsAPI interface {
	TriggerBuild(ctx context.Context, params map[string]string) (queueItem int64, err error)
	BuildStatus(ctx context.Context, buildID int64) (BuildInfo, error)
	ResolveQueueItem(ctx context.Context, queueItem int64) (buildNumber int64, err error)
}

// GitHubAPI mirrors GitHubClient for tests.
type GitHubAPI interface {
	CreateRevertPR(ctx context.Context, commitSHA, branch, title string) (PullRequest, error)
}

func (h Handlers) Register(router *gin.RouterGroup) {
	router.GET("/pipelines", h.list)
	router.POST("/pipelines/trigger", api.RequireRole("admin"), h.trigger)
	router.GET("/pipelines/:id", h.detail)
	router.GET("/pipelines/:id/argocd", h.argocdStatus)
	router.POST("/pipelines/:id/revert", api.RequireRole("admin"), h.revert)
}

// argocdStatus returns the GitOps Application sync/health card; the
// Application name equals `ecampus-<service>` by catalog convention.
func (h Handlers) argocdStatus(c *gin.Context) {
	if h.Argo == nil {
		api.RespFail(c, api.ErrorErr(http.StatusServiceUnavailable, "argocd reader not configured"))
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		api.RespFail(c, api.ErrorErr(http.StatusBadRequest, "invalid id"))
		return
	}
	run, err := h.Store.Get(c.Request.Context(), id)
	if err != nil {
		api.RespFail(c, mapDeliveryError(err))
		return
	}
	status, err := h.Argo.Get(c.Request.Context(), "ecampus-"+run.Service)
	if err != nil {
		api.RespFail(c, api.ErrorErr(http.StatusBadGateway, "read argocd application: "+err.Error()))
		return
	}
	api.RespData(c, status)
}

func (h Handlers) list(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	runs, err := h.Store.List(c.Request.Context(), ListFilter{
		Service: c.Query("service"),
		Status:  c.Query("status"),
		Limit:   limit,
	})
	if err != nil {
		api.RespFail(c, err)
		return
	}
	api.RespData(c, runs)
}

type triggerRequest struct {
	Services    []string `json:"services" binding:"required,min=1,max=13"`
	BeforeSha   string   `json:"beforeSha"`
	AfterSha    string   `json:"afterSha"`
	TargetEnv   string   `json:"targetEnv"`
	SkipRelease bool     `json:"skipRelease"`
}

func (h Handlers) trigger(c *gin.Context) {
	if !h.Cfg.DeliveryReady() {
		api.RespFail(c, api.ErrorErr(http.StatusServiceUnavailable, "delivery integration not configured"))
		return
	}
	var req triggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespFail(c, api.ErrorErr(http.StatusBadRequest, "services (1-13) required"))
		return
	}
	claims := api.MustClaims(c)

	var created []PipelineRun
	for _, service := range req.Services {
		run := PipelineRun{
			Service:     service,
			Status:      "queued",
			TriggeredBy: claims.Username,
		}
		if err := h.Store.Create(c.Request.Context(), &run); err != nil {
			api.RespFail(c, err)
			return
		}
		h.recordRelease(c.Request.Context(), map[string]any{
			"run_id": run.ID, "service": service, "status": "queued",
			"triggered_by": claims.Username, "target_env": req.TargetEnv,
		})
		params := map[string]string{
			"SERVICE_REPO": "https://github.com/worryyy/app-test.git",
			"TARGET_ENV":   req.TargetEnv,
			"BEFORE_SHA":   req.BeforeSha,
			"AFTER_SHA":    req.AfterSha,
		}
		if req.SkipRelease {
			params["SKIP_RELEASE"] = "true"
		}
		queueItem, err := h.Jenkins.TriggerBuild(c.Request.Context(), params)
		if err != nil {
			run.Status = "failed"
			run.Stages = JSONStages{Stages: []Stage{{Name: "trigger", Status: "failed", At: time.Now().Format(time.RFC3339)}}}
			_ = h.Store.MarkTriggerFailed(c.Request.Context(), run.ID, err.Error())
			created = append(created, run)
			continue
		}
		// jenkins_build first holds the queue item id; the resolver
		// overwrites it with the real build number once the agent launches
		// (webhook + backfill lookups key on the build number).
		_ = h.Store.UpdateBuild(c.Request.Context(), run.ID, queueItem)
		run.JenkinsBuild = queueItem
		h.resolveQueuedBuild(run.ID, queueItem)
		created = append(created, run)
	}
	api.RespData(c, created)
}

// resolveQueuedBuild waits for the queued build to start and records its
// build number. Bounded to 10 minutes; a cancelled item resolves to a
// trigger-failed row.
func (h Handlers) resolveQueuedBuild(runID, queueItem int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		build, err := h.Jenkins.ResolveQueueItem(ctx, queueItem)
		if err != nil {
			h.logger().Warn("resolve queue item failed", "run", runID, "queue", queueItem, "error", err)
			return
		}
		if err := h.Store.UpdateBuild(ctx, runID, build); err != nil {
			h.logger().Warn("update build number failed", "run", runID, "build", build, "error", err)
		}
	}()
}

func (h Handlers) logger() *slog.Logger { return slog.Default() }

func (h Handlers) detail(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		api.RespFail(c, api.ErrorErr(http.StatusBadRequest, "invalid id"))
		return
	}
	run, err := h.Store.Get(c.Request.Context(), id)
	if err != nil {
		api.RespFail(c, mapDeliveryError(err))
		return
	}
	// Backfill refresh: if webhook delivery went quiet, ask Jenkins directly.
	if run.JenkinsBuild != 0 && (run.Status == "queued" || run.Status == "running") && h.Cfg.DeliveryReady() {
		if info, err := h.Jenkins.BuildStatus(c.Request.Context(), run.JenkinsBuild); err == nil {
			mapped := mapBuildStatus(info)
			if mapped != "" && mapped != run.Status {
				_ = h.Store.SetStatus(c.Request.Context(), run.ID, mapped)
				run.Status = mapped
			}
		}
	}
	api.RespData(c, run)
}

func (h Handlers) revert(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		api.RespFail(c, api.ErrorErr(http.StatusBadRequest, "invalid id"))
		return
	}
	run, err := h.Store.Get(c.Request.Context(), id)
	if err != nil {
		api.RespFail(c, mapDeliveryError(err))
		return
	}
	if run.GitRevision == "" {
		api.RespFail(c, api.ErrorErr(http.StatusConflict, "run has no git revision to revert"))
		return
	}
	if h.Cfg.GitHubToken == "" {
		api.RespFail(c, api.ErrorErr(http.StatusServiceUnavailable, "github integration not configured"))
		return
	}
	branch := fmt.Sprintf("revert/%s/%d", run.Service, run.JenkinsBuild)
	title := fmt.Sprintf("Revert %s release (build %d)", run.Service, run.JenkinsBuild)
	pr, err := h.GitHub.CreateRevertPR(c.Request.Context(), run.GitRevision, branch, title)
	if err != nil {
		api.RespFail(c, api.ErrorErr(http.StatusBadGateway, "create revert PR failed: "+err.Error()))
		return
	}
	if err := h.Store.SetRevertPR(c.Request.Context(), run.ID, pr.URL); err != nil {
		api.RespFail(c, err)
		return
	}
	api.RespData(c, gin.H{"prUrl": pr.URL, "number": pr.Number})
}

type webhookRequest struct {
	Build       int64   `json:"build" binding:"required"`
	Service     string  `json:"service"`
	Status      string  `json:"status"`
	Stages      []Stage `json:"stages" binding:"required,min=1"`
	GitRevision string  `json:"gitRevision"`
	Digest      string  `json:"digest"`
}

func (h Handlers) Webhook(c *gin.Context) {
	if h.Cfg.WebhookSecret != "" {
		if c.GetHeader("X-Platform-Webhook") != h.Cfg.WebhookSecret {
			api.RespFail(c, api.ErrorErr(http.StatusUnauthorized, "bad webhook secret"))
			return
		}
	}
	var req webhookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		api.RespFail(c, api.ErrorErr(http.StatusBadRequest, "build and stages required"))
		return
	}
	if err := h.Store.ApplyWebhook(c.Request.Context(), req.Build, req.Service, req.Stages, req.Status, req.GitRevision, req.Digest); err != nil {
		api.RespFail(c, mapDeliveryError(err))
		return
	}
	h.recordRelease(c.Request.Context(), map[string]any{
		"build": req.Build, "service": req.Service, "status": req.Status,
		"gitRevision": req.GitRevision, "digest": req.Digest,
	})
	api.RespData(c, gin.H{"ok": true})
}

func mapDeliveryError(err error) error {
	if errors.Is(err, ErrRunNotFound) {
		return api.ErrorErr(http.StatusNotFound, "pipeline run not found")
	}
	return err
}

func mapBuildStatus(info BuildInfo) string {
	switch {
	case info.Building:
		return "running"
	case info.Result == "SUCCESS":
		return "success"
	case info.Result == "FAILURE" || info.Result == "ABORTED":
		return "failed"
	}
	return ""
}
