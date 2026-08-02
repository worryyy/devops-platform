package catalog

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var supportedProfiles = map[string]string{
	"critical-canary":      "canary",
	"standard-canary":      "canary",
	"controlled-bluegreen": "bluegreen",
	"fast-rolling":         "rolling",
}

func Validate(catalog Catalog) error {
	if catalog.Version != "v1" {
		return fmt.Errorf("unsupported catalog version %q", catalog.Version)
	}
	if len(catalog.Services) == 0 {
		return errors.New("catalog must contain at least one service")
	}
	if err := validateProfiles(catalog); err != nil {
		return err
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
		profile, ok := catalog.ReleaseProfiles[service.RolloutProfile]
		if !ok {
			return fmt.Errorf("service %q references unknown rollout profile %q", service.Name, service.RolloutProfile)
		}
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
		if profile.Strategy == "bluegreen" && len(service.PreviewProbes) == 0 {
			return fmt.Errorf("service %q requires at least one preview probe", service.Name)
		}
		for _, probe := range service.PreviewProbes {
			if probe.Name == "" || probe.Method != "GET" || !strings.HasPrefix(probe.Path, "/") {
				return fmt.Errorf("service %q preview probes must be named read-only GET paths", service.Name)
			}
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

func validateProfiles(catalog Catalog) error {
	if len(catalog.ReleaseProfiles) != len(supportedProfiles) {
		return fmt.Errorf("catalog must define exactly %d release profiles", len(supportedProfiles))
	}
	for name, strategy := range supportedProfiles {
		profile, ok := catalog.ReleaseProfiles[name]
		if !ok {
			return fmt.Errorf("catalog is missing release profile %q", name)
		}
		if profile.Strategy != strategy {
			return fmt.Errorf("release profile %q must use strategy %q", name, strategy)
		}
		if _, err := time.ParseDuration(profile.WaitTimeout); err != nil {
			return fmt.Errorf("release profile %q has invalid waitTimeout: %w", name, err)
		}
		if strategy == "rolling" {
			if profile.Analysis.Enabled {
				return fmt.Errorf("release profile %q cannot enable analysis", name)
			}
			continue
		}
		if !profile.Analysis.Enabled {
			return fmt.Errorf("release profile %q must enable analysis", name)
		}
		if _, err := time.ParseDuration(profile.Analysis.Interval); err != nil {
			return fmt.Errorf("release profile %q has invalid analysis interval: %w", name, err)
		}
		if profile.Analysis.Count <= 0 || profile.Analysis.FailureLimit <= 0 || profile.Analysis.ConsecutiveSuccessLimit <= 0 {
			return fmt.Errorf("release profile %q has invalid analysis limits", name)
		}
		if profile.Analysis.ConsecutiveErrorLimit <= 0 ||
			profile.Analysis.MinSamples <= 0 ||
			profile.Analysis.MaxErrorRate <= 0 ||
			profile.Analysis.MaxP95Ratio < 1 {
			return fmt.Errorf("release profile %q has invalid SLI thresholds", name)
		}
		if strategy == "bluegreen" {
			if !profile.ManualPromotion || profile.PreviewReplicaCount <= 0 {
				return fmt.Errorf("release profile %q requires manual promotion and preview replicas", name)
			}
			if _, err := time.ParseDuration(profile.PromotionTimeout); err != nil {
				return fmt.Errorf("release profile %q has invalid promotionTimeout: %w", name, err)
			}
		}
	}
	for environment, override := range catalog.EnvironmentOverrides {
		if environment == "" {
			return errors.New("environment override name is required")
		}
		for profileName, profileOverride := range override.Profiles {
			if _, ok := supportedProfiles[profileName]; !ok {
				return fmt.Errorf("environment %q overrides unknown profile %q", environment, profileName)
			}
			if profileOverride.Analysis.MinSamples != nil && *profileOverride.Analysis.MinSamples <= 0 {
				return fmt.Errorf("environment %q profile %q has invalid minSamples", environment, profileName)
			}
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
		"kubernetes.rollout":         environment.Kubernetes.Rollout,
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
