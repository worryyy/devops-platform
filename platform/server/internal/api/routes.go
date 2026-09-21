package api

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

func NewBaseRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	return router
}

// NewRouter keeps the health-probe-only surface used by probes and tests.
func NewRouter() *gin.Engine {
	router := NewBaseRouter()
	RegisterHealthRoutes(router)
	return router
}

// Deps wires the API server's collaborators.
type Deps struct {
	Logger    *slog.Logger
	JWTSecret string
	Auth      AuthHandlers
	Services  ServicesHandlers
	Catalog   CatalogHandlers
	Delivery  DeliveryHandlers
	Alertgw   AlertgwHandlers
	Reports   ReportHandlers
	// Obs is the /logs search surface (P4). Nil keeps the routes absent
	// (local dev without ClickHouse).
	Obs ObsHandlers
	// GrafanaProxy, when set, serves the /grafana embed (P3). Nil keeps the
	// route absent (local dev without Grafana).
	GrafanaProxy gin.HandlerFunc
}

// DeliveryHandlers is the surface the delivery module registers; the public
// webhook is mounted separately from the authed group.
type DeliveryHandlers interface {
	Register(router *gin.RouterGroup)
	Webhook(c *gin.Context)
}

// AlertgwHandlers is the alert gateway surface (P3): public Alertmanager
// webhook plus authed listing/ack.
type AlertgwHandlers interface {
	Register(router *gin.RouterGroup)
	Webhook(c *gin.Context)
}

// ReportHandlers is the weekly report surface (P3): authed pages plus the
// public runner endpoints (stats, completion webhook).
type ReportHandlers interface {
	Register(router *gin.RouterGroup)
	RegisterPublic(router *gin.RouterGroup)
}

// ObsHandlers is the log search surface (P4): authed /logs routes, 503 per
// request when ClickHouse is absent.
type ObsHandlers interface {
	Register(router *gin.RouterGroup)
}

// NewAPIRouter builds the full server: probes plus the authenticated /api
// group. Public /api routes are the auth login and the platform webhooks.
func NewAPIRouter(deps Deps) *gin.Engine {
	router := NewBaseRouter()
	router.Use(RequestLog(deps.Logger))
	RegisterHealthRoutes(router)

	if deps.GrafanaProxy != nil {
		router.Any("/grafana/*path", deps.GrafanaProxy)
	}

	apiGroup := router.Group("/api")
	apiGroup.POST("/auth/login", deps.Auth.login)
	if deps.Delivery != nil {
		apiGroup.POST("/webhooks/jenkins", deps.Delivery.Webhook)
	}
	if deps.Alertgw != nil {
		apiGroup.POST("/webhooks/alertmanager", deps.Alertgw.Webhook)
	}
	if deps.Reports != nil {
		deps.Reports.RegisterPublic(apiGroup)
	}

	authed := apiGroup.Group("")
	authed.Use(JWTAuth(deps.JWTSecret))
	authed.GET("/auth/me", deps.Auth.me)
	deps.Services.Register(authed)
	deps.Catalog.Register(authed)
	if deps.Delivery != nil {
		deps.Delivery.Register(authed)
	}
	if deps.Alertgw != nil {
		deps.Alertgw.Register(authed)
	}
	if deps.Reports != nil {
		deps.Reports.Register(authed)
	}
	if deps.Obs != nil {
		deps.Obs.Register(authed)
	}

	return router
}
