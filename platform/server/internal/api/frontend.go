package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func RegisterFrontend(router *gin.Engine, embedded embed.FS, root string) {
	sub, err := fs.Sub(embedded, root)
	if err != nil {
		registerFallbackFrontend(router)
		return
	}
	index, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		registerFallbackFrontend(router)
		return
	}
	fileServer := http.FileServer(http.FS(sub))
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/dashboard")
	})
	router.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.JSON(http.StatusNotFound, gin.H{"code": http.StatusNotFound, "message": "not found", "data": nil})
			return
		}
		if strings.Contains(c.Request.URL.Path, ".") {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
}

func registerFallbackFrontend(router *gin.Engine) {
	router.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/dashboard")
	})
	router.GET("/dashboard", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(`<!doctype html><html><head><meta charset="utf-8"><title>CI/CD Dashboard</title></head><body><main><h1>CI/CD Dashboard</h1><p>Frontend assets are not built yet.</p></main></body></html>`))
	})
}
