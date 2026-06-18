package catalog

import "github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"

type ValidationResult struct {
	Valid  bool   `json:"valid"`
	Error  string `json:"error,omitempty"`
	Source string `json:"source,omitempty"`
}

type ServiceLayer struct {
	catalog Catalog
	source  string
}

func NewServiceLayer(catalog Catalog, source string) *ServiceLayer {
	return &ServiceLayer{catalog: catalog, source: source}
}

func (s *ServiceLayer) Catalog() Catalog {
	return SanitizeCatalog(s.catalog)
}

func (s *ServiceLayer) ListServices() []Service {
	return SanitizeCatalog(s.catalog).Services
}

func (s *ServiceLayer) GetService(name string) (Service, error) {
	item, ok := s.catalog.ServiceByName(name)
	if !ok {
		return Service{}, bizerr.NotFound("service not found")
	}
	return SanitizeCatalog(Catalog{Version: s.catalog.Version, Services: []Service{item}}).Services[0], nil
}

func (s *ServiceLayer) ValidateService(name string) ValidationResult {
	item, ok := s.catalog.ServiceByName(name)
	if !ok {
		return ValidationResult{Valid: false, Error: "service not found", Source: s.source}
	}
	err := Validate(Catalog{Version: s.catalog.Version, Services: []Service{item}})
	if err != nil {
		return ValidationResult{Valid: false, Error: err.Error(), Source: s.source}
	}
	return ValidationResult{Valid: true, Source: s.source}
}
