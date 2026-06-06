package health

import (
	"context"
	"net/http"

	"campus-forum/internal/database"
	"campus-forum/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
	"gorm.io/gorm"
)

type Handler struct {
	mysql *gorm.DB
	mongo *mongo.Database
}

func NewHandler(mysql *gorm.DB, mongo *mongo.Database) *Handler {
	return &Handler{
		mysql: mysql,
		mongo: mongo,
	}
}

func (h *Handler) Healthz(c *gin.Context) {
	response.Success(c, gin.H{"status": "ok"})
}

func (h *Handler) Readyz(c *gin.Context) {
	ctx := context.Background()
	data := gin.H{
		"mysql": "ok",
		"mongo": "ok",
	}

	ready := true
	if h.mysql == nil {
		ready = false
		data["mysql"] = "not configured"
	} else if err := database.PingMySQL(ctx, h.mysql); err != nil {
		ready = false
		data["mysql"] = err.Error()
	}

	if h.mongo == nil {
		ready = false
		data["mongo"] = "not configured"
	} else if err := database.PingMongo(ctx, h.mongo); err != nil {
		ready = false
		data["mongo"] = err.Error()
	}

	if ready {
		response.Success(c, data)
		return
	}

	c.JSON(http.StatusServiceUnavailable, response.Body{
		Code: http.StatusServiceUnavailable,
		Msg:  "service unavailable",
		Data: data,
	})
}
