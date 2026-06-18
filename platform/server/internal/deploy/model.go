package deploy

import "time"

type Record struct {
	DeployID             string                 `json:"deploy_id"`
	ServiceName          string                 `json:"service_name"`
	Environment          string                 `json:"environment"`
	Namespace            string                 `json:"namespace"`
	Status               Status                 `json:"status"`
	DeployType           DeployType             `json:"deploy_type"`
	DeliveryMode         DeliveryMode           `json:"delivery_mode"`
	Deployer             string                 `json:"deployer"`
	ConfirmedBy          *string                `json:"confirmed_by,omitempty"`
	ConfirmedAt          *time.Time             `json:"confirmed_at,omitempty"`
	BuildID              *string                `json:"build_id,omitempty"`
	ImageRepo            string                 `json:"image_repo"`
	ImageTag             string                 `json:"image_tag"`
	ImageDigest          string                 `json:"image_digest"`
	CommitSHA            *string                `json:"commit_sha,omitempty"`
	CurrentImage         *string                `json:"current_image,omitempty"`
	ArgoCDApplication    *string                `json:"argocd_application,omitempty"`
	GitOpsCommit         *string                `json:"gitops_commit,omitempty"`
	DeployParamsSnapshot map[string]interface{} `json:"deploy_params_snapshot,omitempty"`
	ValuesYAMLSnapshot   *string                `json:"values_yaml_snapshot,omitempty"`
	RollbackFromDeployID *string                `json:"rollback_from_deploy_id,omitempty"`
	RollbackToDeployID   *string                `json:"rollback_to_deploy_id,omitempty"`
	FailedStage          *string                `json:"failed_stage,omitempty"`
	ErrorMessage         *string                `json:"error_message,omitempty"`
	StartedAt            *time.Time             `json:"started_at,omitempty"`
	FinishedAt           *time.Time             `json:"finished_at,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
}

type DryRunInput struct {
	ServiceName string                 `json:"service_name"`
	Environment string                 `json:"environment"`
	Namespace   string                 `json:"namespace"`
	BuildID     string                 `json:"build_id"`
	ImageRepo   string                 `json:"image_repo"`
	ImageTag    string                 `json:"image_tag"`
	ImageDigest string                 `json:"image_digest"`
	CommitSHA   string                 `json:"commit_sha"`
	Deployer    string                 `json:"deployer"`
	Params      map[string]interface{} `json:"params"`
}

type ConfirmInput struct {
	DryRunDeployID string `json:"dry_run_deploy_id"`
	ConfirmedBy    string `json:"confirmed_by"`
}

type CreateRecordInput struct {
	DeployID             string
	ServiceName          string
	Environment          string
	Namespace            string
	Status               Status
	DeployType           DeployType
	DeliveryMode         DeliveryMode
	Deployer             string
	ConfirmedBy          string
	ConfirmedAt          *time.Time
	BuildID              string
	ImageRepo            string
	ImageTag             string
	ImageDigest          string
	CommitSHA            string
	CurrentImage         string
	ArgoCDApplication    string
	GitOpsCommit         string
	DeployParamsSnapshot map[string]interface{}
	ValuesYAMLSnapshot   string
	RollbackFromDeployID string
	RollbackToDeployID   string
	StartedAt            *time.Time
}

type UpdateRecordInput struct {
	Status       Status
	ConfirmedBy  string
	ConfirmedAt  *time.Time
	FailedStage  string
	ErrorMessage string
	FinishedAt   *time.Time
}

type ListFilter struct {
	ServiceName string
	Environment string
	Status      Status
	Deployer    string
	Limit       int
	Offset      int
}

type Lock struct {
	ServiceName string    `json:"service_name"`
	Environment string    `json:"environment"`
	ReleaseID   string    `json:"release_id"`
	LockedUntil time.Time `json:"locked_until"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
