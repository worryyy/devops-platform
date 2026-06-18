package api

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func RegisterHealthRoutes(router *gin.Engine, store interface {
	Ping(ctx context.Context) error
}) {
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, healthResponse{Status: "ok", Time: time.Now().UTC()})
	})
	router.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if store != nil {
			if err := store.Ping(ctx); err != nil {
				c.JSON(http.StatusServiceUnavailable, readinessResponse{Status: "not_ready"})
				return
			}
		}
		c.JSON(http.StatusOK, readinessResponse{Status: "ready"})
	})
}
