package alertgw

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/worryyy/devops-platform/platform/server/internal/api"
)

// Handlers wires the alert gateway routes: the public Alertmanager webhook
// plus the authed listing/ack endpoints.
type Handlers struct {
	Gateway *Gateway
	Store   AlertStoreAPI
	Now     func() time.Time
}

// Register mounts the authed routes; the webhook stays public and is
// registered by the api package.
func (h Handlers) Register(router *gin.RouterGroup) {
	router.GET("/alerts", h.list)
	router.POST("/alerts/:id/ack", h.ack)
}

// Webhook handles POST /api/webhooks/alertmanager (Alertmanager v2 payload).
func (h Handlers) Webhook(c *gin.Context) {
	var payload Payload
	if err := c.ShouldBindJSON(&payload); err != nil {
		api.RespFail(c, api.ErrorErr(400, "invalid alertmanager payload: "+err.Error()))
		return
	}
	if payload.Version != "4" && len(payload.Alerts) == 0 {
		api.RespFail(c, api.ErrorErr(400, "payload carries no alerts"))
		return
	}
	report := h.Gateway.Ingest(c.Request.Context(), payload)
	api.RespData(c, report)
}

func (h Handlers) list(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	alerts, err := h.Store.List(c.Request.Context(), AlertFilter{
		Status:     c.Query("status"),
		Service:    c.Query("service"),
		Severity:   c.Query("severity"),
		SignalType: c.Query("signalType"),
		Limit:      limit,
	})
	if err != nil {
		api.RespFail(c, err)
		return
	}
	api.RespData(c, alerts)
}

// ack writes the acting username into labels.acked_by.
func (h Handlers) ack(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		api.RespFail(c, api.ErrorErr(400, "invalid id"))
		return
	}
	claims := api.MustClaims(c)
	now := h.Now()
	if err := h.Store.Ack(c.Request.Context(), id, claims.Username, now); err != nil {
		api.RespFail(c, mapAlertError(err))
		return
	}
	api.RespData(c, gin.H{"ok": true, "ackedBy": claims.Username})
}

func mapAlertError(err error) error {
	if err == ErrAlertNotFound {
		return api.ErrorErr(404, "alert not found")
	}
	return err
}
