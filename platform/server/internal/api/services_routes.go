package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
)

type ServicesHandlers struct {
	Catalog ServiceStore
}

type updateServiceRequest struct {
	DisplayName *string              `json:"displayName"`
	Owner       *string              `json:"owner"`
	SLI         *catalog.SLIPolicy   `json:"sli"`
}

func (h ServicesHandlers) Register(router *gin.RouterGroup) {
	router.GET("/services", h.list)
	router.GET("/services/:name", h.get)
	router.PUT("/services/:name", RequireRole("admin"), h.update)
}

func (h ServicesHandlers) list(c *gin.Context) {
	services, err := h.Catalog.List(c.Request.Context(), c.Query("query"))
	if err != nil {
		respFail(c, err)
		return
	}
	respData(c, services)
}

func (h ServicesHandlers) get(c *gin.Context) {
	service, err := h.Catalog.Get(c.Request.Context(), c.Param("name"))
	if err != nil {
		respFail(c, mapCatalogError(err))
		return
	}
	respData(c, service)
}

func (h ServicesHandlers) update(c *gin.Context) {
	var req updateServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respFail(c, ErrorErr(http.StatusBadRequest, "invalid request body"))
		return
	}
	service, err := h.Catalog.Update(c.Request.Context(), c.Param("name"), req.DisplayName, req.Owner, req.SLI)
	if err != nil {
		respFail(c, mapCatalogError(err))
		return
	}
	respData(c, service)
}

func mapCatalogError(err error) error {
	if errors.Is(err, catalog.ErrNotFound) {
		return ErrorErr(http.StatusNotFound, "service not found")
	}
	return err
}

type CatalogHandlers struct {
	Catalog CatalogImporter
}

type importRequest struct {
	Path string `json:"path" binding:"required"`
}

func (h CatalogHandlers) Register(router *gin.RouterGroup) {
	router.POST("/catalog/import", RequireRole("admin"), h.importYAML)
}

func (h CatalogHandlers) importYAML(c *gin.Context) {
	var req importRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respFail(c, ErrorErr(http.StatusBadRequest, "path is required"))
		return
	}
	count, err := h.Catalog.ImportFromYAML(c.Request.Context(), req.Path)
	if err != nil {
		respFail(c, err)
		return
	}
	respData(c, gin.H{"imported": count})
}
