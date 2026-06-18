package dashboard

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

func (h Handler) Summary(c *gin.Context) {
	var query summaryQuery
	if !bindQuery(c, &query) {
		return
	}
	summary, err := h.service.Summary(c.Request.Context(), query.filter())
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, summary)
}
