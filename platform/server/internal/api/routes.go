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
	Logger       *slog.Logger
	JWTSecret    string
	Auth         AuthHandlers
	Services     ServicesHandlers
	Catalog      CatalogHandlers
}

// NewAPIRouter builds the full server: probes plus the authenticated /api
// group. /api/auth/login is the only public /api route.
func NewAPIRouter(deps Deps) *gin.Engine {
	router := NewBaseRouter()
	router.Use(RequestLog(deps.Logger))
	RegisterHealthRoutes(router)

	apiGroup := router.Group("/api")
	apiGroup.POST("/auth/login", deps.Auth.login)

	authed := apiGroup.Group("")
	authed.Use(JWTAuth(deps.JWTSecret))
	authed.GET("/auth/me", deps.Auth.me)
	deps.Services.Register(authed)
	deps.Catalog.Register(authed)

	return router
}
