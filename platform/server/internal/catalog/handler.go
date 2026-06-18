package catalog

import (
	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/responses"
)

type Handler struct {
	service *ServiceLayer
}

func NewHandler(service *ServiceLayer) Handler {
	return Handler{service: service}
}

func (h Handler) List(c *gin.Context) {
	responses.Success.RespData(c, gin.H{
		"version":  h.service.Catalog().Version,
		"services": h.service.ListServices(),
	})
}

func (h Handler) Get(c *gin.Context) {
	var uri serviceURI
	if !bindURI(c, &uri) {
		return
	}
	item, err := h.service.GetService(uri.ServiceName)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, item)
}

func (h Handler) Validate(c *gin.Context) {
	var uri serviceURI
	if !bindURI(c, &uri) {
		return
	}
	responses.Success.RespData(c, h.service.ValidateService(uri.ServiceName))
}
