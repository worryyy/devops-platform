package build

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
	builds := api.Group("/builds")
	builds.GET("", m.handler.List)
	builds.GET("/:buildId", m.handler.Get)
	builds.GET("/:buildId/stages", m.stages)
	builds.POST("", m.handler.Create)
	builds.POST("/:buildId/rebuild", m.handler.Rebuild)

	api.GET("/services/:serviceName/images", m.handler.RecentImages)
}

func (m *Module) stages(c *gin.Context) {
	var uri buildURI
	if !bindURI(c, &uri) {
		return
	}
	items, err := m.pipeline.List(c.Request.Context(), pipeline.ListFilter{BuildID: uri.BuildID})
	if err != nil {
		responses.Fail(c, err)
		return
	}
	responses.Success.RespData(c, items)
}
