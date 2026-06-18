package deploy

import (
	"context"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
	"github.com/worryyy/devops-platform/platform/server/internal/pipeline"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/idgen"
)

type Store interface {
	CreateDeploy(ctx context.Context, input CreateRecordInput) error
	GetDeploy(ctx context.Context, id string) (Record, error)
	ListDeploys(ctx context.Context, filter ListFilter) ([]Record, error)
	UpdateDeploy(ctx context.Context, id string, input UpdateRecordInput) error
	ListActiveLocks(ctx context.Context, serviceName, environment string) ([]Lock, error)
}

type CatalogReader interface {
	GetService(name string) (catalog.Service, error)
}

type StageRecorder interface {
	Create(ctx context.Context, input pipeline.CreateStageInput) (pipeline.StageRecord, error)
}

type Service struct {
	store    Store
	catalog  CatalogReader
	pipeline StageRecorder
}

func NewService(store Store, catalog CatalogReader, pipeline StageRecorder) *Service {
	return &Service{store: store, catalog: catalog, pipeline: pipeline}
}

func (s *Service) DryRun(ctx context.Context, input DryRunInput) (Record, error) {
	if err := validateDryRun(input); err != nil {
		return Record{}, err
	}
	service, environment, err := s.resolveEnvironment(input.ServiceName, input.Environment)
	if err != nil {
		return Record{}, err
	}
	deployID, err := idgen.NewUUID()
	if err != nil {
		return Record{}, bizerr.InternalWrap("generate deploy id failed", err)
	}
	if input.Namespace == "" {
		input.Namespace = environment.Kubernetes.Namespace
	}
	values := valuesSnapshot(input)
	params := map[string]interface{}{
		"service_name": input.ServiceName,
		"environment":  input.Environment,
		"namespace":    input.Namespace,
		"image": map[string]interface{}{
			"repo":   input.ImageRepo,
			"tag":    input.ImageTag,
			"digest": input.ImageDigest,
		},
	}
	for key, value := range input.Params {
		params[key] = value
	}
	now := time.Now().UTC()
	if err := s.store.CreateDeploy(ctx, CreateRecordInput{
		DeployID:             deployID,
		ServiceName:          service.Name,
		Environment:          environment.Name,
		Namespace:            input.Namespace,
		Status:               StatusWaitingConfirmation,
		DeployType:           TypeNormal,
		DeliveryMode:         DeliveryArgoCD,
		Deployer:             input.Deployer,
		BuildID:              input.BuildID,
		ImageRepo:            input.ImageRepo,
		ImageTag:             input.ImageTag,
		ImageDigest:          input.ImageDigest,
		CommitSHA:            input.CommitSHA,
		ArgoCDApplication:    environment.ArgoCD.Application,
		DeployParamsSnapshot: params,
		ValuesYAMLSnapshot:   values,
		StartedAt:            &now,
	}); err != nil {
		return Record{}, bizerr.InternalWrap("create deploy record failed", err)
	}
	detail := dryRunDetail(input, values)
	for _, stage := range dryRunStages(deployID, detail) {
		if _, err := s.pipeline.Create(ctx, stage); err != nil {
			return Record{}, err
		}
	}
	return s.Get(ctx, deployID)
}

func (s *Service) Confirm(ctx context.Context, input ConfirmInput) (Record, error) {
	if input.DryRunDeployID == "" {
		return Record{}, bizerr.Param("dry_run_deploy_id is required")
	}
	if input.ConfirmedBy == "" {
		return Record{}, bizerr.Param("confirmed_by is required")
	}
	item, err := s.Get(ctx, input.DryRunDeployID)
	if err != nil {
		return Record{}, err
	}
	if item.Status != StatusWaitingConfirmation {
		return Record{}, bizerr.Biz("deploy is not waiting for confirmation")
	}
	now := time.Now().UTC()
	if err := s.store.UpdateDeploy(ctx, input.DryRunDeployID, UpdateRecordInput{
		Status:      StatusRunning,
		ConfirmedBy: input.ConfirmedBy,
		ConfirmedAt: &now,
	}); err != nil {
		return Record{}, bizerr.InternalWrap("confirm deploy failed", err)
	}
	for _, stage := range pipeline.DefaultDeployStages(false) {
		if stage.StageOrder <= 60 {
			continue
		}
		stage.DeployID = input.DryRunDeployID
		if stage.Stage == pipeline.DeployStageCommitGitOps {
			stage.Status = pipeline.StatusRunning
			stage.Message = "deploy confirmed; waiting for GitOps execution"
		}
		if _, err := s.pipeline.Create(ctx, stage); err != nil {
			return Record{}, err
		}
	}
	return s.Get(ctx, input.DryRunDeployID)
}

func (s *Service) Redeploy(ctx context.Context, id, deployer string) (Record, error) {
	source, err := s.Get(ctx, id)
	if err != nil {
		return Record{}, err
	}
	return s.copyDeploy(ctx, source, TypeRedeploy, deployer, source.DeployID, "")
}

func (s *Service) Clone(ctx context.Context, id, deployer string) (Record, error) {
	source, err := s.Get(ctx, id)
	if err != nil {
		return Record{}, err
	}
	return s.copyDeploy(ctx, source, TypeCloneDeploy, deployer, "", "")
}

func (s *Service) Rollback(ctx context.Context, id, deployer string) (Record, error) {
	source, err := s.Get(ctx, id)
	if err != nil {
		return Record{}, err
	}
	target := ""
	if source.RollbackToDeployID != nil {
		target = *source.RollbackToDeployID
	}
	return s.copyDeploy(ctx, source, TypeRollback, deployer, id, target)
}

func (s *Service) Get(ctx context.Context, id string) (Record, error) {
	item, err := s.store.GetDeploy(ctx, id)
	if err != nil {
		return Record{}, bizerr.NotFoundWrap("deploy not found", err)
	}
	item.DeployParamsSnapshot = pipeline.SanitizeDetail(item.DeployParamsSnapshot)
	if item.ValuesYAMLSnapshot != nil {
		value := sanitizeText(*item.ValuesYAMLSnapshot)
		item.ValuesYAMLSnapshot = &value
	}
	return item, nil
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]Record, error) {
	items, err := s.store.ListDeploys(ctx, normalizeFilter(filter))
	if err != nil {
		return nil, bizerr.InternalWrap("list deploys failed", err)
	}
	if items == nil {
		items = []Record{}
	}
	for i := range items {
		items[i].DeployParamsSnapshot = pipeline.SanitizeDetail(items[i].DeployParamsSnapshot)
	}
	return items, nil
}

func (s *Service) Locks(ctx context.Context, serviceName, environment string) ([]Lock, error) {
	items, err := s.store.ListActiveLocks(ctx, serviceName, environment)
	if err != nil {
		return nil, bizerr.InternalWrap("list locks failed", err)
	}
	if items == nil {
		items = []Lock{}
	}
	return items, nil
}

func (s *Service) resolveEnvironment(serviceName, environmentName string) (catalog.Service, catalog.Environment, error) {
	service, err := s.catalog.GetService(serviceName)
	if err != nil {
		return catalog.Service{}, catalog.Environment{}, err
	}
	environment, ok := service.EnvironmentByName(environmentName)
	if !ok {
		return catalog.Service{}, catalog.Environment{}, bizerr.NotFound("environment not found")
	}
	return service, environment, nil
}

func (s *Service) copyDeploy(ctx context.Context, source Record, deployType DeployType, deployer, rollbackFrom, rollbackTo string) (Record, error) {
	if deployer == "" {
		deployer = source.Deployer
	}
	id, err := idgen.NewUUID()
	if err != nil {
		return Record{}, bizerr.InternalWrap("generate deploy id failed", err)
	}
	now := time.Now().UTC()
	buildID := valueOf(source.BuildID)
	commit := valueOf(source.CommitSHA)
	current := valueOf(source.CurrentImage)
	argocdApp := valueOf(source.ArgoCDApplication)
	if err := s.store.CreateDeploy(ctx, CreateRecordInput{
		DeployID:             id,
		ServiceName:          source.ServiceName,
		Environment:          source.Environment,
		Namespace:            source.Namespace,
		Status:               StatusWaitingConfirmation,
		DeployType:           deployType,
		DeliveryMode:         source.DeliveryMode,
		Deployer:             deployer,
		BuildID:              buildID,
		ImageRepo:            source.ImageRepo,
		ImageTag:             source.ImageTag,
		ImageDigest:          source.ImageDigest,
		CommitSHA:            commit,
		CurrentImage:         current,
		ArgoCDApplication:    argocdApp,
		DeployParamsSnapshot: source.DeployParamsSnapshot,
		ValuesYAMLSnapshot:   valueOf(source.ValuesYAMLSnapshot),
		RollbackFromDeployID: rollbackFrom,
		RollbackToDeployID:   rollbackTo,
		StartedAt:            &now,
	}); err != nil {
		return Record{}, bizerr.InternalWrap("create deploy record failed", err)
	}
	for _, stage := range pipeline.DefaultDeployStages(true) {
		stage.DeployID = id
		if stage.Stage == pipeline.DeployStageWaitingConfirm {
			stage.Status = pipeline.StatusRunning
			stage.Message = "waiting for deploy confirmation"
		}
		if _, err := s.pipeline.Create(ctx, stage); err != nil {
			return Record{}, err
		}
	}
	return s.Get(ctx, id)
}

func normalizeFilter(filter ListFilter) ListFilter {
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return filter
}

func valueOf(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
