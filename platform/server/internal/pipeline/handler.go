package pipeline

import (
	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/responses"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) Handler {
	return Handler{service: service}
}

func (h Handler) List(c *gin.Context) {
	var query stageListQuery
	if !bindQuery(c, &query) {
		return
	}
	items, err := h.service.List(c.Request.Context(), query.filter())
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, items)
}
