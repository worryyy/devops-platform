package deploy

type Status string

const (
	StatusPending             Status = "pending"
	StatusRunning             Status = "running"
	StatusWaitingConfirmation Status = "waiting_confirmation"
	StatusSuccess             Status = "success"
	StatusFailed              Status = "failed"
	StatusCanceled            Status = "canceled"
	StatusTimeout             Status = "timeout"
)

type DeployType string

const (
	TypeNormal      DeployType = "normal"
	TypeRedeploy    DeployType = "redeploy"
	TypeCloneDeploy DeployType = "clone_deploy"
	TypeRollback    DeployType = "rollback"
)

type DeliveryMode string

const (
	DeliveryArgoCD     DeliveryMode = "argocd"
	DeliveryDirectHelm DeliveryMode = "direct_helm"
)
