package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/responses"
	"github.com/worryyy/devops-platform/platform/server/internal/release"
)

type ReleaseHandler struct {
	releases *release.Service
}

func (h ReleaseHandler) Create(c *gin.Context) {
	var request createReleaseRequest
	if !bindJSON(c, &request) {
		return
	}

	result, err := h.releases.Create(c.Request.Context(), release.CreateCommand{
		Service:     request.Service,
		Environment: request.Environment,
		Branch:      request.Branch,
		Operator:    request.Operator,
	})
	if err != nil {
		responses.Fail(c, err)
		return
	}

	c.Status(http.StatusAccepted)
	responses.Success.RespData(c, createReleaseResponse{ReleaseID: result.ReleaseID, Status: result.Status})
}

func (h ReleaseHandler) Get(c *gin.Context) {
	item, err := h.releases.GetRelease(c.Request.Context(), c.Param("id"))
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, item)
}

func (h ReleaseHandler) Events(c *gin.Context) {
	events, err := h.releases.ListEvents(c.Request.Context(), c.Param("id"))
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, events)
}

func (h ReleaseHandler) ListByService(c *gin.Context) {
	limit := requestLimit(c, 50)
	releases, err := h.releases.ListReleasesByService(c.Request.Context(), c.Param("serviceName"), limit)
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, releases)
}
