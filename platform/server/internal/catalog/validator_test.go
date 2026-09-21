package catalog

import (
	"path/filepath"
	"testing"
)

func loadTestCatalog(t *testing.T) Catalog {
	t.Helper()
	catalog, err := Load(filepath.Clean("../../configs/service-catalog.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return catalog
}

func TestValidateAcceptsCatalog(t *testing.T) {
	if err := Validate(loadTestCatalog(t)); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsMissingSLI(t *testing.T) {
	catalog := loadTestCatalog(t)

	broken := catalog
	broken.Services = append([]Service{}, catalog.Services...)
	topic := broken.Services[len(broken.Services)-1]
	topic.SLI.MaxP95Seconds = 0
	broken.Services[len(broken.Services)-1] = topic
	if err := Validate(broken); err == nil {
		t.Fatal("expected service without a p95 budget to be rejected")
	}

	broken = catalog
	broken.Services = append([]Service{}, catalog.Services...)
	broken.Services[0].SLI.RequestRouteRegex = "("
	if err := Validate(broken); err == nil {
		t.Fatal("expected invalid route regex to be rejected")
	}
}

func TestValidateRejectsMissingWorkload(t *testing.T) {
	catalog := loadTestCatalog(t)

	broken := catalog
	broken.Services = append([]Service{}, catalog.Services...)
	service := broken.Services[0]
	service.Environments = append([]Environment{}, service.Environments...)
	service.Environments[0].Kubernetes.Workload = ""
	broken.Services[0] = service
	if err := Validate(broken); err == nil {
		t.Fatal("expected empty kubernetes.workload to be rejected")
	}
}

func TestValidateRejectsDuplicateServices(t *testing.T) {
	catalog := loadTestCatalog(t)
	broken := catalog
	broken.Services = append([]Service{}, catalog.Services...)
	broken.Services = append(broken.Services, broken.Services[0])
	if err := Validate(broken); err == nil {
		t.Fatal("expected duplicate service to be rejected")
	}
}
