package pipeline

import "github.com/gin-gonic/gin"

type Module struct {
	handler Handler
}

func NewModule(service *Service) *Module {
	return &Module{handler: NewHandler(service)}
}

func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	group := api.Group("/pipeline")
	group.GET("/stages", m.handler.List)
}
