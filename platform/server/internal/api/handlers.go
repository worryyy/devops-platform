package api

import (
	"context"

	"github.com/worryyy/devops-platform/platform/server/internal/auth"
	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
)

// Authenticator decouples auth routes from the GORM-backed UserStore so
// handler tests run without a database. *auth.UserStore satisfies it.
type Authenticator interface {
	Authenticate(ctx context.Context, username, password string) (auth.User, error)
}

// ServiceStore decouples services/catalog routes from *catalog.Store.
type ServiceStore interface {
	List(ctx context.Context, query string) ([]catalog.ServiceSummary, error)
	Get(ctx context.Context, name string) (catalog.Service, error)
	Update(ctx context.Context, name string, displayName, owner *string, sli *catalog.SLIPolicy) (catalog.Service, error)
}

// CatalogImporter decouples the import route from *catalog.Store.
type CatalogImporter interface {
	ImportFromYAML(ctx context.Context, path string) (int, error)
}
