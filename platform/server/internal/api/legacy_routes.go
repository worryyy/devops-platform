package api

import (
	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/release"
)

func RegisterLegacyReleaseRoutes(api *gin.RouterGroup, releases *release.Service) {
	handler := ReleaseHandler{releases: releases}
	api.POST("/releases", handler.Create)
	api.GET("/releases/:id", handler.Get)
	api.GET("/releases/:id/events", handler.Events)
	api.GET("/releases/by-service/:serviceName", handler.ListByService)
}
