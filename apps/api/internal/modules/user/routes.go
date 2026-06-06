package user

import "github.com/gin-gonic/gin"

func RegisterRoutes(group *gin.RouterGroup, handler *Handler) {
	group.POST("/register", handler.Register)
	group.GET("/detail", handler.Detail)
	group.GET("/topic/list", handler.TopicList)
	group.GET("/comment/list", handler.CommentList)
}
