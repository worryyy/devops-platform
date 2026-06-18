package pipeline

type Status string

const (
	StatusPending  Status = "pending"
	StatusRunning  Status = "running"
	StatusSuccess  Status = "success"
	StatusFailed   Status = "failed"
	StatusSkipped  Status = "skipped"
	StatusCanceled Status = "canceled"
	StatusTimeout  Status = "timeout"
)

type OwnerType string

const (
	OwnerBuild  OwnerType = "build"
	OwnerDeploy OwnerType = "deploy"
)
