package catalog

import (
	"fmt"
	"strings"
)

type DeliveryCatalog struct {
	Services []DeliveryService `json:"services"`
}

// DeliveryService is the per-service slice of the catalog consumed by the
// Jenkins pipeline (delivery-catalog.json). Every workload is a plain
// Deployment; the release identity (digest, deploy_id) travels with the
// values files, not the workload kind.
type DeliveryService struct {
	Service           string  `json:"service"`
	Image             string  `json:"image"`
	ChartPath         string  `json:"chart_path"`
	ValuesFile        string  `json:"values_file"`
	Environment       string  `json:"environment"`
	Namespace         string  `json:"namespace"`
	Application       string  `json:"application"`
	ArgoNamespace     string  `json:"argocd_namespace"`
	Workload          string  `json:"workload"`
	StableService     string  `json:"stable_service"`
	Container         string  `json:"container"`
	HealthPath        string  `json:"health_path"`
	WorkloadKind      string  `json:"workload_kind"`
	ResourceName      string  `json:"resource_name"`
	WaitTimeout       string  `json:"wait_timeout"`
	RequestRouteRegex string  `json:"request_route_regex"`
	OperationRegex    string  `json:"operation_route_regex,omitempty"`
	MaxP95Seconds     float64 `json:"max_p95_seconds"`
}

const defaultWaitTimeout = "15m"

func Export(catalog Catalog, names []string, environmentName string) (DeliveryCatalog, error) {
	if environmentName == "" {
		return DeliveryCatalog{}, fmt.Errorf("environment is required")
	}
	seen := make(map[string]struct{}, len(names))
	result := DeliveryCatalog{Services: make([]DeliveryService, 0, len(names))}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return DeliveryCatalog{}, fmt.Errorf("service name is empty")
		}
		if _, exists := seen[name]; exists {
			return DeliveryCatalog{}, fmt.Errorf("duplicate requested service %q", name)
		}
		seen[name] = struct{}{}
		service, ok := catalog.ServiceByName(name)
		if !ok {
			return DeliveryCatalog{}, fmt.Errorf("service %q is missing from the delivery catalog", name)
		}
		environment, ok := service.EnvironmentByName(environmentName)
		if !ok {
			return DeliveryCatalog{}, fmt.Errorf("service %q has no %q environment", name, environmentName)
		}
		result.Services = append(result.Services, DeliveryService{
			Service: name, Image: environment.Image.Repository,
			ChartPath: environment.Git.ChartPath, ValuesFile: environment.Git.ValuesFile,
			Environment: environment.Name, Namespace: environment.Kubernetes.Namespace,
			Application: environment.ArgoCD.Application, ArgoNamespace: environment.ArgoCD.Namespace,
			Workload:      environment.Kubernetes.Workload,
			StableService: environment.Kubernetes.Service,
			Container:     environment.Kubernetes.Container, HealthPath: environment.Health.HealthPath,
			WorkloadKind: "Deployment", ResourceName: environment.Kubernetes.Workload,
			WaitTimeout:       defaultWaitTimeout,
			RequestRouteRegex: service.SLI.RequestRouteRegex, OperationRegex: service.SLI.OperationRouteRegex,
			MaxP95Seconds: service.SLI.MaxP95Seconds,
		})
	}
	return result, nil
}
