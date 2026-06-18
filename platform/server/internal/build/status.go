package build

type Status string

const (
	StatusPending  Status = "pending"
	StatusRunning  Status = "running"
	StatusSuccess  Status = "success"
	StatusFailed   Status = "failed"
	StatusCanceled Status = "canceled"
	StatusTimeout  Status = "timeout"
)

type TriggerType string

const (
	TriggerManual   TriggerType = "manual"
	TriggerWebhook  TriggerType = "webhook"
	TriggerRebuild  TriggerType = "rebuild"
	TriggerSchedule TriggerType = "scheduled"
)

func IsTerminal(status Status) bool {
	switch status {
	case StatusSuccess, StatusFailed, StatusCanceled, StatusTimeout:
		return true
	default:
		return false
	}
}
