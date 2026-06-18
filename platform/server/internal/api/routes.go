package api

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/release"
)

type RouterDependencies struct {
	Releases *release.Service
	Store    interface {
		Ping(ctx context.Context) error
	}
}

func NewBaseRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	return router
}

func NewRouter(deps RouterDependencies) *gin.Engine {
	router := NewBaseRouter()
	RegisterHealthRoutes(router, deps.Store)

	api := router.Group("/api")
	RegisterLegacyReleaseRoutes(api, deps.Releases)

	return router
}
