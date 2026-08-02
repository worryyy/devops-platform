package api

import "github.com/gin-gonic/gin"

func NewBaseRouter() *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	return router
}

func NewRouter() *gin.Engine {
	router := NewBaseRouter()
	RegisterHealthRoutes(router)
	return router
}
