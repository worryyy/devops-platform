package catalog

import (
	"fmt"
	"strings"
)

type DeliveryCatalog struct {
	Services []DeliveryService `json:"services"`
}

type DeliveryService struct {
	Service               string         `json:"service"`
	Image                 string         `json:"image"`
	ChartPath             string         `json:"chart_path"`
	ValuesFile            string         `json:"values_file"`
	Environment           string         `json:"environment"`
	Namespace             string         `json:"namespace"`
	Application           string         `json:"application"`
	ArgoNamespace         string         `json:"argocd_namespace"`
	Rollout               string         `json:"rollout"`
	StableService         string         `json:"stable_service"`
	CandidateService      string         `json:"candidate_service"`
	Container             string         `json:"container"`
	HealthPath            string         `json:"health_path"`
	WorkloadKind          string         `json:"workload_kind"`
	ResourceName          string         `json:"resource_name"`
	EffectiveProfile      string         `json:"effective_profile"`
	Analysis              AnalysisPolicy `json:"analysis"`
	WaitTimeout           string         `json:"wait_timeout"`
	ManualPromotion       bool           `json:"manual_promotion"`
	PreviewReplicas       int            `json:"preview_replica_count,omitempty"`
	PromotionTimeout      string         `json:"promotion_timeout,omitempty"`
	ScaleDownDelaySeconds int            `json:"scale_down_delay_seconds,omitempty"`
	RequestRouteRegex     string         `json:"request_route_regex"`
	OperationRegex        string         `json:"operation_route_regex,omitempty"`
	PreviewProbes         []PreviewProbe `json:"preview_probes,omitempty"`
}

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
		effective, err := ResolveRelease(catalog, service, environmentName)
		if err != nil {
			return DeliveryCatalog{}, fmt.Errorf("service %q: %w", name, err)
		}
		analysis := effective.Definition.Analysis
		analysis.MaxP95Seconds = service.SLI.MaxP95Seconds
		workloadKind := "Rollout"
		if effective.Definition.Strategy == "rolling" {
			workloadKind = "Deployment"
		}
		result.Services = append(result.Services, DeliveryService{
			Service: name, Image: environment.Image.Repository,
			ChartPath: environment.Git.ChartPath, ValuesFile: environment.Git.ValuesFile,
			Environment: environment.Name, Namespace: environment.Kubernetes.Namespace,
			Application: environment.ArgoCD.Application, ArgoNamespace: environment.ArgoCD.Namespace,
			Rollout: environment.Kubernetes.Rollout, StableService: environment.Kubernetes.Service,
			CandidateService: environment.Kubernetes.Service + "-candidate",
			Container:        environment.Kubernetes.Container, HealthPath: environment.Health.HealthPath,
			WorkloadKind: workloadKind, ResourceName: environment.Kubernetes.Rollout,
			EffectiveProfile: effective.Profile, Analysis: analysis,
			WaitTimeout: effective.Definition.WaitTimeout, ManualPromotion: effective.ManualPromotion,
			PreviewReplicas:  effective.Definition.PreviewReplicaCount,
			PromotionTimeout: effective.Definition.PromotionTimeout, ScaleDownDelaySeconds: effective.Definition.ScaleDownDelaySeconds,
			RequestRouteRegex: service.SLI.RequestRouteRegex, OperationRegex: service.SLI.OperationRouteRegex,
			PreviewProbes: service.PreviewProbes,
		})
	}
	return result, nil
}
