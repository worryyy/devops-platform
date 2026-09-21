package catalog

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

func Validate(catalog Catalog) error {
	if catalog.Version != "v1" {
		return fmt.Errorf("unsupported catalog version %q", catalog.Version)
	}
	if len(catalog.Services) == 0 {
		return errors.New("catalog must contain at least one service")
	}
	seenServices := map[string]struct{}{}
	for _, service := range catalog.Services {
		if service.Name == "" {
			return errors.New("service name is required")
		}
		if _, exists := seenServices[service.Name]; exists {
			return fmt.Errorf("duplicate service %q", service.Name)
		}
		seenServices[service.Name] = struct{}{}
		if service.SLI.RequestRouteRegex == "" {
			return fmt.Errorf("service %q requires sli.requestRouteRegex", service.Name)
		}
		if _, err := regexp.Compile(service.SLI.RequestRouteRegex); err != nil {
			return fmt.Errorf("service %q has invalid request route regex: %w", service.Name, err)
		}
		if service.SLI.OperationRouteRegex != "" {
			if _, err := regexp.Compile(service.SLI.OperationRouteRegex); err != nil {
				return fmt.Errorf("service %q has invalid operation route regex: %w", service.Name, err)
			}
		}
		if service.SLI.MaxP95Seconds <= 0 {
			return fmt.Errorf("service %q requires a positive sli.maxP95Seconds", service.Name)
		}
		if len(service.Environments) == 0 {
			return fmt.Errorf("service %q must contain at least one environment", service.Name)
		}
		seenEnvironments := map[string]struct{}{}
		for _, environment := range service.Environments {
			if err := validateEnvironment(service.Name, environment); err != nil {
				return err
			}
			if _, exists := seenEnvironments[environment.Name]; exists {
				return fmt.Errorf("service %q has duplicate environment %q", service.Name, environment.Name)
			}
			seenEnvironments[environment.Name] = struct{}{}
		}
	}
	return nil
}

func validateEnvironment(serviceName string, environment Environment) error {
	prefix := fmt.Sprintf("service %q environment %q", serviceName, environment.Name)
	if environment.Name == "" {
		return fmt.Errorf("service %q environment name is required", serviceName)
	}
	required := map[string]string{
		"namespace":                  environment.Namespace,
		"branchPolicy.defaultBranch": environment.BranchPolicy.DefaultBranch,
		"git.repo":                   environment.Git.Repo,
		"git.chartPath":              environment.Git.ChartPath,
		"git.valuesFile":             environment.Git.ValuesFile,
		"image.repository":           environment.Image.Repository,
		"image.tagPolicy":            environment.Image.TagPolicy,
		"jenkins.mode":               environment.Jenkins.Mode,
		"jenkins.jobName":            environment.Jenkins.JobName,
		"argocd.application":         environment.ArgoCD.Application,
		"argocd.namespace":           environment.ArgoCD.Namespace,
		"kubernetes.namespace":       environment.Kubernetes.Namespace,
		"kubernetes.workload":        environment.Kubernetes.Workload,
		"kubernetes.service":         environment.Kubernetes.Service,
		"kubernetes.container":       environment.Kubernetes.Container,
		"health.healthPath":          environment.Health.HealthPath,
		"health.readyPath":           environment.Health.ReadyPath,
	}
	for field, value := range required {
		if value == "" {
			return fmt.Errorf("%s missing %s", prefix, field)
		}
	}
	if len(environment.BranchPolicy.AllowedBranches) == 0 {
		return fmt.Errorf("%s must allow at least one branch", prefix)
	}
	if !BranchAllowed(environment.BranchPolicy, environment.BranchPolicy.DefaultBranch) {
		return fmt.Errorf("%s default branch %q is not allowed", prefix, environment.BranchPolicy.DefaultBranch)
	}
	if environment.Namespace != environment.Kubernetes.Namespace {
		return fmt.Errorf("%s namespace does not match kubernetes.namespace", prefix)
	}
	if environment.Image.TagPolicy != "git-sha" || !environment.Image.RequireDigest {
		return fmt.Errorf("%s image must use git-sha tags and require a digest", prefix)
	}
	if !strings.HasPrefix(environment.Health.HealthPath, "/") || !strings.HasPrefix(environment.Health.ReadyPath, "/") {
		return fmt.Errorf("%s health paths must start with /", prefix)
	}
	return nil
}

func BranchAllowed(policy BranchPolicy, branch string) bool {
	for _, allowed := range policy.AllowedBranches {
		if allowed == branch {
			return true
		}
		if strings.HasSuffix(allowed, "/*") {
			prefix := strings.TrimSuffix(allowed, "*")
			if strings.HasPrefix(branch, prefix) && len(branch) > len(prefix) {
				return true
			}
		}
	}
	return false
}
