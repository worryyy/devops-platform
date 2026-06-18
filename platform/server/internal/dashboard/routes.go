package dashboard

import "github.com/gin-gonic/gin"

type Module struct {
	handler Handler
}

func NewModule(service *Service) *Module {
	return &Module{handler: NewHandler(service)}
}

func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	api.GET("/dashboard/summary", m.handler.Summary)
}
