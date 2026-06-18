package store

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/build"
	"github.com/worryyy/devops-platform/platform/server/internal/deploy"
	"github.com/worryyy/devops-platform/platform/server/internal/pipeline"
	"github.com/worryyy/devops-platform/platform/server/internal/release"
)

type releaseRecord struct {
	ID          string `gorm:"column:id;primaryKey"`
	ServiceName string `gorm:"column:service_name"`
	Environment string `gorm:"column:environment"`
	Branch      string `gorm:"column:branch"`

	Status   string `gorm:"column:status"`
	Operator string `gorm:"column:operator"`

	JenkinsJob         *string `gorm:"column:jenkins_job"`
	JenkinsBuildNumber *int    `gorm:"column:jenkins_build_number"`

	CommitSHA   *string `gorm:"column:commit_sha"`
	ImageRepo   *string `gorm:"column:image_repo"`
	ImageTag    *string `gorm:"column:image_tag"`
	ImageDigest *string `gorm:"column:image_digest"`

	ArgoCDApp  *string `gorm:"column:argocd_app"`
	Namespace  *string `gorm:"column:namespace"`
	Deployment *string `gorm:"column:deployment"`

	ErrorMessage *string `gorm:"column:error_message"`

	StartedAt  *time.Time `gorm:"column:started_at"`
	FinishedAt *time.Time `gorm:"column:finished_at"`

	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (releaseRecord) TableName() string {
	return "releases"
}

func newReleaseRecord(input release.CreateReleaseInput) releaseRecord {
	now := time.Now().UTC()
	return releaseRecord{
		ID:          input.ID,
		ServiceName: input.ServiceName,
		Environment: input.Environment,
		Branch:      input.Branch,
		Status:      string(release.StatusRequested),
		Operator:    input.Operator,
		JenkinsJob:  stringPtrOrNil(input.JenkinsJob),
		ImageRepo:   stringPtrOrNil(input.ImageRepo),
		ArgoCDApp:   stringPtrOrNil(input.ArgoCDApp),
		Namespace:   stringPtrOrNil(input.Namespace),
		Deployment:  stringPtrOrNil(input.Deployment),
		StartedAt:   &now,
	}
}

func (r releaseRecord) toRelease() release.Release {
	return release.Release{
		ID:                 r.ID,
		ServiceName:        r.ServiceName,
		Environment:        r.Environment,
		Branch:             r.Branch,
		Status:             release.Status(r.Status),
		Operator:           r.Operator,
		JenkinsJob:         r.JenkinsJob,
		JenkinsBuildNumber: r.JenkinsBuildNumber,
		CommitSHA:          r.CommitSHA,
		ImageRepo:          r.ImageRepo,
		ImageTag:           r.ImageTag,
		ImageDigest:        r.ImageDigest,
		ArgoCDApp:          r.ArgoCDApp,
		Namespace:          r.Namespace,
		Deployment:         r.Deployment,
		ErrorMessage:       r.ErrorMessage,
		StartedAt:          r.StartedAt,
		FinishedAt:         r.FinishedAt,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}
}

type releaseEventRecord struct {
	ID        int64     `gorm:"column:id;primaryKey"`
	ReleaseID string    `gorm:"column:release_id"`
	Status    string    `gorm:"column:status"`
	Message   string    `gorm:"column:message"`
	Detail    jsonBytes `gorm:"column:detail;type:jsonb"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (releaseEventRecord) TableName() string {
	return "release_events"
}

func (r releaseEventRecord) toEvent() (release.Event, error) {
	event := release.Event{
		ID:        r.ID,
		ReleaseID: r.ReleaseID,
		Status:    release.Status(r.Status),
		Message:   r.Message,
		CreatedAt: r.CreatedAt,
	}
	if len(r.Detail) > 0 {
		if err := json.Unmarshal(r.Detail, &event.Detail); err != nil {
			return release.Event{}, fmt.Errorf("decode release event detail: %w", err)
		}
	}
	return event, nil
}

type releaseLockRecord struct {
	ServiceName string `gorm:"column:service_name;primaryKey"`
	Environment string `gorm:"column:environment;primaryKey"`

	ReleaseID   string    `gorm:"column:release_id"`
	LockedUntil time.Time `gorm:"column:locked_until"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (releaseLockRecord) TableName() string {
	return "release_locks"
}

type jsonBytes []byte

func (j jsonBytes) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	if !json.Valid(j) {
		return nil, fmt.Errorf("invalid json payload")
	}
	return string(j), nil
}

func (j *jsonBytes) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch data := value.(type) {
	case []byte:
		*j = append((*j)[:0], data...)
	case string:
		*j = append((*j)[:0], data...)
	default:
		return fmt.Errorf("unsupported jsonb value type %T", value)
	}
	return nil
}

func stringPtrOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

type buildRecord struct {
	BuildID            string     `gorm:"column:build_id;primaryKey"`
	ServiceName        string     `gorm:"column:service_name"`
	RepoURL            string     `gorm:"column:repo_url"`
	Branch             string     `gorm:"column:branch"`
	CommitSHA          string     `gorm:"column:commit_sha"`
	Status             string     `gorm:"column:status"`
	TriggerType        string     `gorm:"column:trigger_type"`
	Builder            string     `gorm:"column:builder"`
	JenkinsJob         *string    `gorm:"column:jenkins_job"`
	JenkinsBuildNumber *int       `gorm:"column:jenkins_build_number"`
	JenkinsBuildURL    *string    `gorm:"column:jenkins_build_url"`
	SourceBuildID      *string    `gorm:"column:source_build_id"`
	ImageRepo          *string    `gorm:"column:image_repo"`
	ImageTag           *string    `gorm:"column:image_tag"`
	ImageDigest        *string    `gorm:"column:image_digest"`
	FailedStage        *string    `gorm:"column:failed_stage"`
	ErrorMessage       *string    `gorm:"column:error_message"`
	ParamsSnapshot     jsonBytes  `gorm:"column:params_snapshot;type:jsonb"`
	StartedAt          *time.Time `gorm:"column:started_at"`
	FinishedAt         *time.Time `gorm:"column:finished_at"`
	CreatedAt          time.Time  `gorm:"column:created_at"`
	UpdatedAt          time.Time  `gorm:"column:updated_at"`
}

func (buildRecord) TableName() string {
	return "build_records"
}

func newBuildRecord(input build.CreateRecordInput) (buildRecord, error) {
	params, err := marshalJSON(input.ParamsSnapshot)
	if err != nil {
		return buildRecord{}, err
	}
	now := time.Now().UTC()
	return buildRecord{
		BuildID:        input.BuildID,
		ServiceName:    input.ServiceName,
		RepoURL:        input.RepoURL,
		Branch:         input.Branch,
		CommitSHA:      input.CommitSHA,
		Status:         string(input.Status),
		TriggerType:    string(input.TriggerType),
		Builder:        input.Builder,
		JenkinsJob:     stringPtrOrNil(input.JenkinsJob),
		SourceBuildID:  stringPtrOrNil(input.SourceBuildID),
		ImageRepo:      stringPtrOrNil(input.ImageRepo),
		ParamsSnapshot: params,
		StartedAt:      input.StartedAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (r buildRecord) toBuild() (build.Record, error) {
	params, err := unmarshalMap(r.ParamsSnapshot)
	if err != nil {
		return build.Record{}, err
	}
	return build.Record{
		BuildID:            r.BuildID,
		ServiceName:        r.ServiceName,
		RepoURL:            r.RepoURL,
		Branch:             r.Branch,
		CommitSHA:          r.CommitSHA,
		Status:             build.Status(r.Status),
		TriggerType:        build.TriggerType(r.TriggerType),
		Builder:            r.Builder,
		JenkinsJob:         r.JenkinsJob,
		JenkinsBuildNumber: r.JenkinsBuildNumber,
		JenkinsBuildURL:    r.JenkinsBuildURL,
		SourceBuildID:      r.SourceBuildID,
		ImageRepo:          r.ImageRepo,
		ImageTag:           r.ImageTag,
		ImageDigest:        r.ImageDigest,
		FailedStage:        r.FailedStage,
		ErrorMessage:       r.ErrorMessage,
		ParamsSnapshot:     params,
		StartedAt:          r.StartedAt,
		FinishedAt:         r.FinishedAt,
		CreatedAt:          r.CreatedAt,
		UpdatedAt:          r.UpdatedAt,
	}, nil
}

type deployRecord struct {
	DeployID             string     `gorm:"column:deploy_id;primaryKey"`
	ServiceName          string     `gorm:"column:service_name"`
	Environment          string     `gorm:"column:environment"`
	Namespace            string     `gorm:"column:namespace"`
	Status               string     `gorm:"column:status"`
	DeployType           string     `gorm:"column:deploy_type"`
	DeliveryMode         string     `gorm:"column:delivery_mode"`
	Deployer             string     `gorm:"column:deployer"`
	ConfirmedBy          *string    `gorm:"column:confirmed_by"`
	ConfirmedAt          *time.Time `gorm:"column:confirmed_at"`
	BuildID              *string    `gorm:"column:build_id"`
	ImageRepo            string     `gorm:"column:image_repo"`
	ImageTag             string     `gorm:"column:image_tag"`
	ImageDigest          string     `gorm:"column:image_digest"`
	CommitSHA            *string    `gorm:"column:commit_sha"`
	CurrentImage         *string    `gorm:"column:current_image"`
	ArgoCDApplication    *string    `gorm:"column:argocd_application"`
	GitOpsCommit         *string    `gorm:"column:gitops_commit"`
	DeployParamsSnapshot jsonBytes  `gorm:"column:deploy_params_snapshot;type:jsonb"`
	ValuesYAMLSnapshot   *string    `gorm:"column:values_yaml_snapshot"`
	RollbackFromDeployID *string    `gorm:"column:rollback_from_deploy_id"`
	RollbackToDeployID   *string    `gorm:"column:rollback_to_deploy_id"`
	FailedStage          *string    `gorm:"column:failed_stage"`
	ErrorMessage         *string    `gorm:"column:error_message"`
	StartedAt            *time.Time `gorm:"column:started_at"`
	FinishedAt           *time.Time `gorm:"column:finished_at"`
	CreatedAt            time.Time  `gorm:"column:created_at"`
	UpdatedAt            time.Time  `gorm:"column:updated_at"`
}

func (deployRecord) TableName() string {
	return "deploy_records"
}

func newDeployRecord(input deploy.CreateRecordInput) (deployRecord, error) {
	params, err := marshalJSON(input.DeployParamsSnapshot)
	if err != nil {
		return deployRecord{}, err
	}
	now := time.Now().UTC()
	return deployRecord{
		DeployID:             input.DeployID,
		ServiceName:          input.ServiceName,
		Environment:          input.Environment,
		Namespace:            input.Namespace,
		Status:               string(input.Status),
		DeployType:           string(input.DeployType),
		DeliveryMode:         string(input.DeliveryMode),
		Deployer:             input.Deployer,
		ConfirmedBy:          stringPtrOrNil(input.ConfirmedBy),
		ConfirmedAt:          input.ConfirmedAt,
		BuildID:              stringPtrOrNil(input.BuildID),
		ImageRepo:            input.ImageRepo,
		ImageTag:             input.ImageTag,
		ImageDigest:          input.ImageDigest,
		CommitSHA:            stringPtrOrNil(input.CommitSHA),
		CurrentImage:         stringPtrOrNil(input.CurrentImage),
		ArgoCDApplication:    stringPtrOrNil(input.ArgoCDApplication),
		GitOpsCommit:         stringPtrOrNil(input.GitOpsCommit),
		DeployParamsSnapshot: params,
		ValuesYAMLSnapshot:   stringPtrOrNil(input.ValuesYAMLSnapshot),
		RollbackFromDeployID: stringPtrOrNil(input.RollbackFromDeployID),
		RollbackToDeployID:   stringPtrOrNil(input.RollbackToDeployID),
		StartedAt:            input.StartedAt,
		CreatedAt:            now,
		UpdatedAt:            now,
	}, nil
}

func (r deployRecord) toDeploy() (deploy.Record, error) {
	params, err := unmarshalMap(r.DeployParamsSnapshot)
	if err != nil {
		return deploy.Record{}, err
	}
	return deploy.Record{
		DeployID:             r.DeployID,
		ServiceName:          r.ServiceName,
		Environment:          r.Environment,
		Namespace:            r.Namespace,
		Status:               deploy.Status(r.Status),
		DeployType:           deploy.DeployType(r.DeployType),
		DeliveryMode:         deploy.DeliveryMode(r.DeliveryMode),
		Deployer:             r.Deployer,
		ConfirmedBy:          r.ConfirmedBy,
		ConfirmedAt:          r.ConfirmedAt,
		BuildID:              r.BuildID,
		ImageRepo:            r.ImageRepo,
		ImageTag:             r.ImageTag,
		ImageDigest:          r.ImageDigest,
		CommitSHA:            r.CommitSHA,
		CurrentImage:         r.CurrentImage,
		ArgoCDApplication:    r.ArgoCDApplication,
		GitOpsCommit:         r.GitOpsCommit,
		DeployParamsSnapshot: params,
		ValuesYAMLSnapshot:   r.ValuesYAMLSnapshot,
		RollbackFromDeployID: r.RollbackFromDeployID,
		RollbackToDeployID:   r.RollbackToDeployID,
		FailedStage:          r.FailedStage,
		ErrorMessage:         r.ErrorMessage,
		StartedAt:            r.StartedAt,
		FinishedAt:           r.FinishedAt,
		CreatedAt:            r.CreatedAt,
		UpdatedAt:            r.UpdatedAt,
	}, nil
}

type pipelineStageRecord struct {
	ID         string     `gorm:"column:id;primaryKey"`
	BuildID    *string    `gorm:"column:build_id"`
	DeployID   *string    `gorm:"column:deploy_id"`
	Stage      string     `gorm:"column:stage"`
	StageOrder int        `gorm:"column:stage_order"`
	Status     string     `gorm:"column:status"`
	Message    string     `gorm:"column:message"`
	Detail     jsonBytes  `gorm:"column:detail;type:jsonb"`
	StartedAt  *time.Time `gorm:"column:started_at"`
	FinishedAt *time.Time `gorm:"column:finished_at"`
	CreatedAt  time.Time  `gorm:"column:created_at"`
	UpdatedAt  time.Time  `gorm:"column:updated_at"`
}

func (pipelineStageRecord) TableName() string {
	return "pipeline_stage_records"
}

func newPipelineStageRecord(input pipeline.CreateStageInput) (pipelineStageRecord, error) {
	detail, err := marshalJSON(input.Detail)
	if err != nil {
		return pipelineStageRecord{}, err
	}
	now := time.Now().UTC()
	return pipelineStageRecord{
		ID:         input.ID,
		BuildID:    stringPtrOrNil(input.BuildID),
		DeployID:   stringPtrOrNil(input.DeployID),
		Stage:      input.Stage,
		StageOrder: input.StageOrder,
		Status:     string(input.Status),
		Message:    input.Message,
		Detail:     detail,
		StartedAt:  input.StartedAt,
		FinishedAt: input.FinishedAt,
		CreatedAt:  now,
		UpdatedAt:  now,
	}, nil
}

func (r pipelineStageRecord) toPipelineStage() (pipeline.StageRecord, error) {
	detail, err := unmarshalMap(r.Detail)
	if err != nil {
		return pipeline.StageRecord{}, err
	}
	return pipeline.StageRecord{
		ID:         r.ID,
		BuildID:    r.BuildID,
		DeployID:   r.DeployID,
		Stage:      r.Stage,
		StageOrder: r.StageOrder,
		Status:     pipeline.Status(r.Status),
		Message:    r.Message,
		Detail:     detail,
		StartedAt:  r.StartedAt,
		FinishedAt: r.FinishedAt,
		CreatedAt:  r.CreatedAt,
		UpdatedAt:  r.UpdatedAt,
	}, nil
}

func marshalJSON(value map[string]interface{}) (jsonBytes, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal json: %w", err)
	}
	return data, nil
}

func unmarshalMap(data jsonBytes) (map[string]interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	var value map[string]interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("decode json: %w", err)
	}
	return value, nil
}
