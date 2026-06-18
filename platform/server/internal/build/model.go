package build

import "time"

type Record struct {
	BuildID            string                 `json:"build_id"`
	ServiceName        string                 `json:"service_name"`
	RepoURL            string                 `json:"repo_url"`
	Branch             string                 `json:"branch"`
	CommitSHA          string                 `json:"commit_sha"`
	Status             Status                 `json:"status"`
	TriggerType        TriggerType            `json:"trigger_type"`
	Builder            string                 `json:"builder"`
	JenkinsJob         *string                `json:"jenkins_job,omitempty"`
	JenkinsBuildNumber *int                   `json:"jenkins_build_number,omitempty"`
	JenkinsBuildURL    *string                `json:"jenkins_build_url,omitempty"`
	SourceBuildID      *string                `json:"source_build_id,omitempty"`
	ImageRepo          *string                `json:"image_repo,omitempty"`
	ImageTag           *string                `json:"image_tag,omitempty"`
	ImageDigest        *string                `json:"image_digest,omitempty"`
	FailedStage        *string                `json:"failed_stage,omitempty"`
	ErrorMessage       *string                `json:"error_message,omitempty"`
	ParamsSnapshot     map[string]interface{} `json:"params_snapshot,omitempty"`
	StartedAt          *time.Time             `json:"started_at,omitempty"`
	FinishedAt         *time.Time             `json:"finished_at,omitempty"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

type CreateInput struct {
	ServiceName   string                 `json:"service_name"`
	Branch        string                 `json:"branch"`
	CommitSHA     string                 `json:"commit_sha"`
	TriggerType   TriggerType            `json:"trigger_type"`
	Builder       string                 `json:"builder"`
	SourceBuildID string                 `json:"source_build_id"`
	Params        map[string]interface{} `json:"params"`
}

type CreateRecordInput struct {
	BuildID        string
	ServiceName    string
	RepoURL        string
	Branch         string
	CommitSHA      string
	Status         Status
	TriggerType    TriggerType
	Builder        string
	JenkinsJob     string
	SourceBuildID  string
	ImageRepo      string
	ParamsSnapshot map[string]interface{}
	StartedAt      *time.Time
}

type ListFilter struct {
	ServiceName string
	Branch      string
	Status      Status
	Builder     string
	Limit       int
	Offset      int
}

type RecentImage struct {
	BuildID     string     `json:"build_id"`
	ServiceName string     `json:"service_name"`
	Branch      string     `json:"branch"`
	CommitSHA   string     `json:"commit_sha"`
	ImageRepo   string     `json:"image_repo"`
	ImageTag    string     `json:"image_tag"`
	ImageDigest string     `json:"image_digest"`
	Builder     string     `json:"builder"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}
