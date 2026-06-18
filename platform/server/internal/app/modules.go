package app

import (
	"github.com/gin-gonic/gin"
	"github.com/worryyy/devops-platform/platform/server/internal/api"
	"github.com/worryyy/devops-platform/platform/server/internal/build"
	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
	"github.com/worryyy/devops-platform/platform/server/internal/dashboard"
	"github.com/worryyy/devops-platform/platform/server/internal/deploy"
	"github.com/worryyy/devops-platform/platform/server/internal/pipeline"
	"github.com/worryyy/devops-platform/platform/server/internal/release"
	"github.com/worryyy/devops-platform/platform/server/internal/store"
)

type APIModules struct {
	Catalog        *catalog.Module
	Build          *build.Module
	Deploy         *deploy.Module
	Pipeline       *pipeline.Module
	Dashboard      *dashboard.Module
	LegacyReleases *release.Service
}

type moduleDependencies struct {
	Store          *store.Store
	CatalogService *catalog.ServiceLayer
	ReleaseService *release.Service
}

func newAPIModules(deps moduleDependencies) APIModules {
	pipelineService := pipeline.NewService(deps.Store)
	buildService := build.NewService(deps.Store, deps.CatalogService, pipelineService)
	deployService := deploy.NewService(deps.Store, deps.CatalogService, pipelineService)
	dashboardService := dashboard.NewService(deps.Store)

	return APIModules{
		Catalog:        catalog.NewModule(deps.CatalogService),
		Build:          build.NewModule(buildService, pipelineService),
		Deploy:         deploy.NewModule(deployService, pipelineService),
		Pipeline:       pipeline.NewModule(pipelineService),
		Dashboard:      dashboard.NewModule(dashboardService),
		LegacyReleases: deps.ReleaseService,
	}
}

func (m APIModules) Register(apiGroup *gin.RouterGroup) {
	m.Catalog.RegisterRoutes(apiGroup)
	m.Build.RegisterRoutes(apiGroup)
	m.Deploy.RegisterRoutes(apiGroup)
	m.Pipeline.RegisterRoutes(apiGroup)
	m.Dashboard.RegisterRoutes(apiGroup)
	if m.LegacyReleases != nil {
		api.RegisterLegacyReleaseRoutes(apiGroup, m.LegacyReleases)
	}
}
