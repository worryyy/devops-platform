package api

import (
	"time"
)

type readinessResponse struct {
	Status string `json:"status"`
}

type healthResponse struct {
	Status string    `json:"status"`
	Time   time.Time `json:"time"`
}
