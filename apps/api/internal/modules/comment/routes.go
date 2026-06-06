package comment

import "github.com/gin-gonic/gin"

func RegisterRoutes(group *gin.RouterGroup, handler *Handler) {
	group.POST("/create", handler.Create)
	group.GET("/list", handler.List)
	group.POST("/delete", handler.Delete)
}
