package api

import (
	"time"

	"github.com/worryyy/devops-platform/platform/server/internal/release"
)

type createReleaseRequest struct {
	Service     string `json:"service" binding:"required"`
	Environment string `json:"environment" binding:"required"`
	Branch      string `json:"branch" binding:"required"`
	Operator    string `json:"operator" binding:"required"`
}

type createReleaseResponse struct {
	ReleaseID string         `json:"release_id"`
	Status    release.Status `json:"status"`
}

type readinessResponse struct {
	Status string `json:"status"`
}

type healthResponse struct {
	Status string    `json:"status"`
	Time   time.Time `json:"time"`
}
