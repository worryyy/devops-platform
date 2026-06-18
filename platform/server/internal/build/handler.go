package build

import (
	"net/http"

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
	var query buildListQuery
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

func (h Handler) Get(c *gin.Context) {
	var uri buildURI
	if !bindURI(c, &uri) {
		return
	}
	item, err := h.service.Get(c.Request.Context(), uri.BuildID)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, item)
}

func (h Handler) Create(c *gin.Context) {
	var input CreateInput
	if !bindJSON(c, &input) {
		return
	}
	item, err := h.service.Create(c.Request.Context(), input)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	c.Status(http.StatusAccepted)
	responses.Success.RespData(c, item)
}

func (h Handler) Rebuild(c *gin.Context) {
	var uri buildURI
	if !bindURI(c, &uri) {
		return
	}
	var input rebuildRequest
	bindOptionalJSON(c, &input)
	item, err := h.service.Rebuild(c.Request.Context(), uri.BuildID, input.Builder)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	c.Status(http.StatusAccepted)
	responses.Success.RespData(c, item)
}

func (h Handler) RecentImages(c *gin.Context) {
	var uri recentImagesURI
	if !bindURI(c, &uri) {
		return
	}
	var query recentImagesQuery
	if !bindQuery(c, &query) {
		return
	}
	items, err := h.service.RecentSuccessfulImages(c.Request.Context(), uri.ServiceName, query.requestLimit())
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, items)
}
