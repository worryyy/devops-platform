package deploy

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
	var query deployListQuery
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
	var uri deployURI
	if !bindURI(c, &uri) {
		return
	}
	item, err := h.service.Get(c.Request.Context(), uri.DeployID)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, item)
}

func (h Handler) DryRun(c *gin.Context) {
	var input DryRunInput
	if !bindJSON(c, &input) {
		return
	}
	item, err := h.service.DryRun(c.Request.Context(), input)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	c.Status(http.StatusAccepted)
	responses.Success.RespData(c, item)
}

func (h Handler) Confirm(c *gin.Context) {
	var input ConfirmInput
	if !bindJSON(c, &input) {
		return
	}
	item, err := h.service.Confirm(c.Request.Context(), input)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	c.Status(http.StatusAccepted)
	responses.Success.RespData(c, item)
}

func (h Handler) Redeploy(c *gin.Context) {
	uri, actor, ok := bindDeployAction(c)
	if !ok {
		return
	}
	item, err := h.service.Redeploy(c.Request.Context(), uri.DeployID, actor)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	c.Status(http.StatusAccepted)
	responses.Success.RespData(c, item)
}

func (h Handler) Clone(c *gin.Context) {
	uri, actor, ok := bindDeployAction(c)
	if !ok {
		return
	}
	item, err := h.service.Clone(c.Request.Context(), uri.DeployID, actor)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	c.Status(http.StatusAccepted)
	responses.Success.RespData(c, item)
}

func (h Handler) Rollback(c *gin.Context) {
	uri, actor, ok := bindDeployAction(c)
	if !ok {
		return
	}
	item, err := h.service.Rollback(c.Request.Context(), uri.DeployID, actor)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	c.Status(http.StatusAccepted)
	responses.Success.RespData(c, item)
}

func (h Handler) Locks(c *gin.Context) {
	var query locksQuery
	if !bindQuery(c, &query) {
		return
	}
	items, err := h.service.Locks(c.Request.Context(), query.ServiceName, query.Environment)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, items)
}

func bindDeployAction(c *gin.Context) (deployURI, string, bool) {
	var uri deployURI
	if !bindURI(c, &uri) {
		return deployURI{}, "", false
	}
	var input actorRequest
	bindOptionalJSON(c, &input)
	return uri, input.actor(), true
}
