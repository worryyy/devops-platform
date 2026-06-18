package pipeline

import "time"

type StageRecord struct {
	ID         string                 `json:"id"`
	BuildID    *string                `json:"build_id,omitempty"`
	DeployID   *string                `json:"deploy_id,omitempty"`
	Stage      string                 `json:"stage"`
	StageOrder int                    `json:"stage_order"`
	Status     Status                 `json:"status"`
	Message    string                 `json:"message,omitempty"`
	Detail     map[string]interface{} `json:"detail,omitempty"`
	StartedAt  *time.Time             `json:"started_at,omitempty"`
	FinishedAt *time.Time             `json:"finished_at,omitempty"`
	CreatedAt  time.Time              `json:"created_at"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

type CreateStageInput struct {
	ID         string
	BuildID    string
	DeployID   string
	Stage      string
	StageOrder int
	Status     Status
	Message    string
	Detail     map[string]interface{}
	StartedAt  *time.Time
	FinishedAt *time.Time
}

type ListFilter struct {
	BuildID  string
	DeployID string
}
