package build

import (
	"context"
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/catalog"
	"github.com/worryyy/devops-platform/platform/server/internal/pipeline"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"
	"github.com/worryyy/devops-platform/platform/server/internal/pkg/idgen"
)

type Store interface {
	CreateBuild(ctx context.Context, input CreateRecordInput) error
	GetBuild(ctx context.Context, id string) (Record, error)
	ListBuilds(ctx context.Context, filter ListFilter) ([]Record, error)
	ListRecentSuccessfulImages(ctx context.Context, serviceName string, limit int) ([]RecentImage, error)
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

func (s *Service) Create(ctx context.Context, input CreateInput) (Record, error) {
	if err := validateCreate(input); err != nil {
		return Record{}, err
	}
	service, err := s.catalog.GetService(input.ServiceName)
	if err != nil {
		return Record{}, err
	}
	environment := defaultEnvironment(service)
	if environment.Name != "" && !catalog.BranchAllowed(environment.BranchPolicy, input.Branch) {
		return Record{}, bizerr.Param("branch not allowed")
	}
	buildID, err := idgen.NewUUID()
	if err != nil {
		return Record{}, bizerr.InternalWrap("generate build id failed", err)
	}
	now := time.Now().UTC()
	trigger := input.TriggerType
	if trigger == "" {
		trigger = TriggerManual
	}
	params := map[string]interface{}{
		"service_name": service.Name,
		"branch":       input.Branch,
		"commit_sha":   input.CommitSHA,
		"trigger_type": string(trigger),
	}
	for key, value := range input.Params {
		params[key] = value
	}
	recordInput := CreateRecordInput{
		BuildID:        buildID,
		ServiceName:    service.Name,
		RepoURL:        environment.Git.Repo,
		Branch:         input.Branch,
		CommitSHA:      input.CommitSHA,
		Status:         StatusPending,
		TriggerType:    trigger,
		Builder:        input.Builder,
		JenkinsJob:     environment.Jenkins.JobName,
		SourceBuildID:  input.SourceBuildID,
		ImageRepo:      environment.Image.Repository,
		ParamsSnapshot: params,
		StartedAt:      &now,
	}
	if err := s.store.CreateBuild(ctx, recordInput); err != nil {
		return Record{}, bizerr.InternalWrap("create build record failed", err)
	}
	for _, stage := range pipeline.DefaultBuildStages() {
		stage.BuildID = buildID
		if _, err := s.pipeline.Create(ctx, stage); err != nil {
			return Record{}, err
		}
	}
	return s.Get(ctx, buildID)
}

func (s *Service) Rebuild(ctx context.Context, id, builder string) (Record, error) {
	source, err := s.Get(ctx, id)
	if err != nil {
		return Record{}, err
	}
	if builder == "" {
		builder = source.Builder
	}
	return s.Create(ctx, CreateInput{
		ServiceName:   source.ServiceName,
		Branch:        source.Branch,
		CommitSHA:     source.CommitSHA,
		TriggerType:   TriggerRebuild,
		Builder:       builder,
		SourceBuildID: source.BuildID,
		Params: map[string]interface{}{
			"rebuild_from": source.BuildID,
		},
	})
}

func (s *Service) Get(ctx context.Context, id string) (Record, error) {
	item, err := s.store.GetBuild(ctx, id)
	if err != nil {
		return Record{}, bizerr.NotFoundWrap("build not found", err)
	}
	item.ParamsSnapshot = pipeline.SanitizeDetail(item.ParamsSnapshot)
	return item, nil
}

func (s *Service) List(ctx context.Context, filter ListFilter) ([]Record, error) {
	items, err := s.store.ListBuilds(ctx, normalizeFilter(filter))
	if err != nil {
		return nil, bizerr.InternalWrap("list builds failed", err)
	}
	if items == nil {
		items = []Record{}
	}
	for i := range items {
		items[i].ParamsSnapshot = pipeline.SanitizeDetail(items[i].ParamsSnapshot)
	}
	return items, nil
}

func (s *Service) RecentSuccessfulImages(ctx context.Context, serviceName string, limit int) ([]RecentImage, error) {
	if _, err := s.catalog.GetService(serviceName); err != nil {
		return nil, err
	}
	items, err := s.store.ListRecentSuccessfulImages(ctx, serviceName, limit)
	if err != nil {
		return nil, bizerr.InternalWrap("list recent successful images failed", err)
	}
	if items == nil {
		items = []RecentImage{}
	}
	return items, nil
}

func defaultEnvironment(service catalog.Service) catalog.Environment {
	if len(service.Environments) == 0 {
		return catalog.Environment{}
	}
	return service.Environments[0]
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
