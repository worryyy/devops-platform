package router

import (
	"campus-forum/internal/modules/comment"
	"campus-forum/internal/modules/health"
	"campus-forum/internal/modules/session"
	"campus-forum/internal/modules/topic"
	"campus-forum/internal/modules/user"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"gorm.io/gorm"
)

type Dependencies struct {
	UserService    *user.Service
	SessionService *session.Service
	TopicService   *topic.Service
	CommentService *comment.Service
	MySQL           *gorm.DB
	Mongo           *mongo.Database
}

func New(deps Dependencies) *gin.Engine {
	engine := gin.Default()
	RegisterRoutes(engine, deps)
	return engine
}

func RegisterRoutes(engine *gin.Engine, deps Dependencies) {
	user.RegisterRoutes(
		engine.Group("/user"),
		user.NewHandler(deps.UserService, deps.TopicService, deps.CommentService),
	)
	session.RegisterRoutes(
		engine.Group("/session"),
		session.NewHandler(deps.SessionService),
	)
	topic.RegisterRoutes(
		engine.Group("/topic"),
		topic.NewHandler(deps.TopicService),
	)
	comment.RegisterRoutes(
		engine.Group("/comment"),
		comment.NewHandler(deps.CommentService),
	)
	health.RegisterRoutes(
		engine,
		health.NewHandler(deps.MySQL, deps.Mongo),
	)
}
