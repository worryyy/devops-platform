package health

import "github.com/gin-gonic/gin"

func RegisterRoutes(engine *gin.Engine, handler *Handler) {
	engine.GET("/healthz", handler.Healthz)
	engine.GET("/readyz", handler.Readyz)
}
