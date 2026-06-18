package deploy

import (
	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/pipeline"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/responses"
)

type Module struct {
	handler  Handler
	pipeline *pipeline.Service
}

func NewModule(service *Service, pipelineService *pipeline.Service) *Module {
	return &Module{handler: NewHandler(service), pipeline: pipelineService}
}

func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	deploys := api.Group("/deploys")
	deploys.GET("", m.handler.List)
	deploys.GET("/:deployId", m.handler.Get)
	deploys.GET("/:deployId/stages", m.stages)
	deploys.POST("/dry-run", m.handler.DryRun)
	deploys.POST("/confirm", m.handler.Confirm)
	deploys.POST("/:deployId/redeploy", m.handler.Redeploy)
	deploys.POST("/:deployId/clone", m.handler.Clone)
	deploys.POST("/:deployId/rollback", m.handler.Rollback)

	api.GET("/locks", m.handler.Locks)
}

func (m *Module) stages(c *gin.Context) {
	var uri deployURI
	if !bindURI(c, &uri) {
		return
	}
	items, err := m.pipeline.List(c.Request.Context(), pipeline.ListFilter{DeployID: uri.DeployID})
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, items)
}
