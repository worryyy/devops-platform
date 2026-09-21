package catalog

type Catalog struct {
	Version  string    `json:"version" yaml:"version"`
	Services []Service `json:"services" yaml:"services"`
}

type Service struct {
	Name         string        `json:"name" yaml:"name"`
	DisplayName  string        `json:"displayName" yaml:"displayName"`
	Owner        string        `json:"owner" yaml:"owner"`
	Kind         string        `json:"kind,omitempty" yaml:"kind,omitempty"`
	SLI          SLIPolicy     `json:"sli" yaml:"sli"`
	Environments []Environment `json:"environments" yaml:"environments"`
}

// SLIPolicy is the service-level delivery contract: which routes count as the
// service's traffic and what post-release latency is acceptable.
type SLIPolicy struct {
	RequestRouteRegex   string  `json:"requestRouteRegex" yaml:"requestRouteRegex"`
	OperationRouteRegex string  `json:"operationRouteRegex,omitempty" yaml:"operationRouteRegex,omitempty"`
	MaxP95Seconds       float64 `json:"maxP95Seconds" yaml:"maxP95Seconds"`
}

type Environment struct {
	Name         string           `json:"name" yaml:"name"`
	Namespace    string           `json:"namespace" yaml:"namespace"`
	BranchPolicy BranchPolicy     `json:"branchPolicy" yaml:"branchPolicy"`
	Git          GitConfig        `json:"git" yaml:"git"`
	Image        ImageConfig      `json:"image" yaml:"image"`
	Jenkins      JenkinsConfig    `json:"jenkins" yaml:"jenkins"`
	ArgoCD       ArgoCDConfig     `json:"argocd" yaml:"argocd"`
	Kubernetes   KubernetesConfig `json:"kubernetes" yaml:"kubernetes"`
	Health       HealthConfig     `json:"health" yaml:"health"`
}

type BranchPolicy struct {
	DefaultBranch   string   `json:"defaultBranch" yaml:"defaultBranch"`
	AllowedBranches []string `json:"allowedBranches" yaml:"allowedBranches"`
}

type GitConfig struct {
	Repo       string `json:"repo" yaml:"repo"`
	ChartPath  string `json:"chartPath" yaml:"chartPath"`
	ValuesFile string `json:"valuesFile" yaml:"valuesFile"`
}

type ImageConfig struct {
	Repository    string `json:"repository" yaml:"repository"`
	TagPolicy     string `json:"tagPolicy" yaml:"tagPolicy"`
	RequireDigest bool   `json:"requireDigest" yaml:"requireDigest"`
}

type JenkinsConfig struct {
	Mode    string `json:"mode" yaml:"mode"`
	JobName string `json:"jobName" yaml:"jobName"`
}

type ArgoCDConfig struct {
	Application string `json:"application" yaml:"application"`
	Namespace   string `json:"namespace" yaml:"namespace"`
}

type KubernetesConfig struct {
	Namespace string `json:"namespace" yaml:"namespace"`
	Workload  string `json:"workload" yaml:"workload"`
	Service   string `json:"service" yaml:"service"`
	Container string `json:"container" yaml:"container"`
}

type HealthConfig struct {
	HealthPath string `json:"healthPath" yaml:"healthPath"`
	ReadyPath  string `json:"readyPath" yaml:"readyPath"`
}

func (c Catalog) ServiceByName(name string) (Service, bool) {
	for _, service := range c.Services {
		if service.Name == name {
			return service, true
		}
	}
	return Service{}, false
}

func (s Service) EnvironmentByName(name string) (Environment, bool) {
	for _, environment := range s.Environments {
		if environment.Name == name {
			return environment, true
		}
	}
	return Environment{}, false
}
