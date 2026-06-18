package catalog

import "github.com/gin-gonic/gin"

type Module struct {
	handler Handler
}

func NewModule(service *ServiceLayer) *Module {
	return &Module{handler: NewHandler(service)}
}

func (m *Module) RegisterRoutes(api *gin.RouterGroup) {
	api.GET("/services", m.handler.List)
	api.GET("/services/:serviceName", m.handler.Get)
	api.GET("/services/:serviceName/catalog-validation", m.handler.Validate)
}
