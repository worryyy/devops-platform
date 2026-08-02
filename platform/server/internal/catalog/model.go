package catalog

type Catalog struct {
	Version              string                         `json:"version" yaml:"version"`
	ReleaseProfiles      map[string]ReleaseProfile      `json:"releaseProfiles" yaml:"releaseProfiles"`
	EnvironmentOverrides map[string]EnvironmentOverride `json:"environmentOverrides,omitempty" yaml:"environmentOverrides,omitempty"`
	Services             []Service                      `json:"services" yaml:"services"`
}

type Service struct {
	Name            string         `json:"name" yaml:"name"`
	DisplayName     string         `json:"displayName" yaml:"displayName"`
	Owner           string         `json:"owner" yaml:"owner"`
	Kind            string         `json:"kind,omitempty" yaml:"kind,omitempty"`
	RolloutProfile  string         `json:"rolloutProfile" yaml:"rolloutProfile"`
	SLI             SLIPolicy      `json:"sli" yaml:"sli"`
	PreviewProbes   []PreviewProbe `json:"previewProbes,omitempty" yaml:"previewProbes,omitempty"`
	ManualPromotion *bool          `json:"manualPromotion,omitempty" yaml:"manualPromotion,omitempty"`
	Environments    []Environment  `json:"environments" yaml:"environments"`
}

type ReleaseProfile struct {
	Strategy            string         `json:"strategy" yaml:"strategy"`
	WaitTimeout         string         `json:"waitTimeout" yaml:"waitTimeout"`
	ManualPromotion     bool           `json:"manualPromotion" yaml:"manualPromotion"`
	PreviewReplicaCount int            `json:"previewReplicaCount,omitempty" yaml:"previewReplicaCount,omitempty"`
	PromotionTimeout    string         `json:"promotionTimeout,omitempty" yaml:"promotionTimeout,omitempty"`
	ScaleDownDelaySeconds int          `json:"scaleDownDelaySeconds,omitempty" yaml:"scaleDownDelaySeconds,omitempty"`
	Analysis            AnalysisPolicy `json:"analysis" yaml:"analysis"`
}

// AnalysisPolicy JSON tags are snake_case because the delivery-catalog.json
// contract is consumed by the Jenkins pipeline; YAML tags stay camelCase to
// match the service catalog input format.
type AnalysisPolicy struct {
	Enabled                 bool    `json:"enabled" yaml:"enabled"`
	Interval                string  `json:"interval,omitempty" yaml:"interval,omitempty"`
	Count                   int     `json:"count,omitempty" yaml:"count,omitempty"`
	ConsecutiveSuccessLimit int     `json:"consecutive_success_limit,omitempty" yaml:"consecutiveSuccessLimit,omitempty"`
	FailureLimit            int     `json:"failure_limit,omitempty" yaml:"failureLimit,omitempty"`
	InconclusiveLimit       int     `json:"inconclusive_limit,omitempty" yaml:"inconclusiveLimit,omitempty"`
	ConsecutiveErrorLimit   int     `json:"consecutive_error_limit,omitempty" yaml:"consecutiveErrorLimit,omitempty"`
	MinSamples              int     `json:"min_samples,omitempty" yaml:"minSamples,omitempty"`
	StableMinSamples        int     `json:"stable_min_samples,omitempty" yaml:"stableMinSamples,omitempty"`
	MaxErrorRate            float64 `json:"max_error_rate,omitempty" yaml:"maxErrorRate,omitempty"`
	MaxErrorRateIncrease    float64 `json:"max_error_rate_increase,omitempty" yaml:"maxErrorRateIncrease,omitempty"`
	MaxP95Seconds           float64 `json:"max_p95_seconds,omitempty" yaml:"maxP95Seconds,omitempty"`
	MaxP95Ratio             float64 `json:"max_p95_ratio,omitempty" yaml:"maxP95Ratio,omitempty"`
	MinOperationSuccessRate float64 `json:"min_operation_success_rate,omitempty" yaml:"minOperationSuccessRate,omitempty"`
	InconclusiveTimeout     string  `json:"inconclusive_timeout,omitempty" yaml:"inconclusiveTimeout,omitempty"`
}

type EnvironmentOverride struct {
	Profiles map[string]ProfileOverride `json:"profiles" yaml:"profiles"`
}

type ProfileOverride struct {
	Analysis AnalysisOverride `json:"analysis" yaml:"analysis"`
}

type AnalysisOverride struct {
	MinSamples       *int `json:"minSamples,omitempty" yaml:"minSamples,omitempty"`
	StableMinSamples *int `json:"stableMinSamples,omitempty" yaml:"stableMinSamples,omitempty"`
}

type SLIPolicy struct {
	RequestRouteRegex   string  `json:"requestRouteRegex" yaml:"requestRouteRegex"`
	OperationRouteRegex string  `json:"operationRouteRegex,omitempty" yaml:"operationRouteRegex,omitempty"`
	MaxP95Seconds       float64 `json:"maxP95Seconds" yaml:"maxP95Seconds"`
}

type PreviewProbe struct {
	Name         string `json:"name" yaml:"name"`
	Path         string `json:"path" yaml:"path"`
	Method       string `json:"method" yaml:"method"`
	RequiresAuth bool   `json:"requiresAuth,omitempty" yaml:"requiresAuth,omitempty"`
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
	Rollout   string `json:"rollout,omitempty" yaml:"rollout,omitempty"`
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
