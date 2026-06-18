package pipeline

const (
	BuildStageCheckout        = "Checkout"
	BuildStageTest            = "Test"
	BuildStageBuildArtifact   = "BuildArtifact"
	BuildStageBuildImage      = "BuildImage"
	BuildStagePushTCR         = "PushTCR"
	BuildStageResolveDigest   = "ResolveDigest"
	BuildStageSaveBuildRecord = "SaveBuildRecord"

	DeployStagePrecheck          = "Precheck"
	DeployStageRenderValues      = "RenderValues"
	DeployStageHelmLint          = "HelmLint"
	DeployStageHelmTemplate      = "HelmTemplate"
	DeployStageDryRun            = "DryRun"
	DeployStageWaitingConfirm    = "WaitingConfirmation"
	DeployStageCommitGitOps      = "CommitGitOpsChange"
	DeployStageArgoCDSync        = "ArgoCDSync"
	DeployStageRolloutProgress   = "RolloutProgress"
	DeployStageReadinessCheck    = "ReadinessCheck"
	DeployStageSmokeTest         = "SmokeTest"
	DeployStagePrometheusAnalyze = "PrometheusAnalysis"
	DeployStageComplete          = "Complete"
)

func DefaultBuildStages() []CreateStageInput {
	return []CreateStageInput{
		{Stage: BuildStageCheckout, StageOrder: 10, Status: StatusPending},
		{Stage: BuildStageTest, StageOrder: 20, Status: StatusPending},
		{Stage: BuildStageBuildArtifact, StageOrder: 30, Status: StatusPending},
		{Stage: BuildStageBuildImage, StageOrder: 40, Status: StatusPending},
		{Stage: BuildStagePushTCR, StageOrder: 50, Status: StatusPending},
		{Stage: BuildStageResolveDigest, StageOrder: 60, Status: StatusPending},
		{Stage: BuildStageSaveBuildRecord, StageOrder: 70, Status: StatusPending},
	}
}

func DefaultDeployStages(includeDryRun bool) []CreateStageInput {
	stages := []CreateStageInput{
		{Stage: DeployStagePrecheck, StageOrder: 10, Status: StatusPending},
		{Stage: DeployStageRenderValues, StageOrder: 20, Status: StatusPending},
		{Stage: DeployStageHelmLint, StageOrder: 30, Status: StatusPending},
		{Stage: DeployStageHelmTemplate, StageOrder: 40, Status: StatusPending},
	}
	if includeDryRun {
		stages = append(stages, CreateStageInput{Stage: DeployStageDryRun, StageOrder: 50, Status: StatusPending})
	}
	stages = append(stages,
		CreateStageInput{Stage: DeployStageWaitingConfirm, StageOrder: 60, Status: StatusPending},
		CreateStageInput{Stage: DeployStageCommitGitOps, StageOrder: 70, Status: StatusPending},
		CreateStageInput{Stage: DeployStageArgoCDSync, StageOrder: 80, Status: StatusPending},
		CreateStageInput{Stage: DeployStageRolloutProgress, StageOrder: 90, Status: StatusPending},
		CreateStageInput{Stage: DeployStageReadinessCheck, StageOrder: 100, Status: StatusPending},
		CreateStageInput{Stage: DeployStageSmokeTest, StageOrder: 110, Status: StatusPending},
		CreateStageInput{Stage: DeployStagePrometheusAnalyze, StageOrder: 120, Status: StatusSkipped, Message: "Argo Rollouts and Prometheus are not configured"},
		CreateStageInput{Stage: DeployStageComplete, StageOrder: 130, Status: StatusPending},
	)
	return stages
}
