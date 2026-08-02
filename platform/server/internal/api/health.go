package api

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func RegisterHealthRoutes(router *gin.Engine) {
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, healthResponse{Status: "ok", Time: time.Now().UTC()})
	})
	router.GET("/readyz", func(c *gin.Context) {
		c.JSON(http.StatusOK, readinessResponse{Status: "ready"})
	})
}
