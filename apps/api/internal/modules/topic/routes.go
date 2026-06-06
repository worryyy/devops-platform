package topic

import "github.com/gin-gonic/gin"

func RegisterRoutes(group *gin.RouterGroup, handler *Handler) {
	group.POST("/create", handler.Create)
	group.GET("/list", handler.List)
	group.GET("/detail", handler.Detail)
	group.POST("/delete", handler.Delete)
	group.GET("/search", handler.Search)
}
