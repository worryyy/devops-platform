package delivery

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func bytesReader(body []byte) *bytes.Reader { return bytes.NewReader(body) }

// memStore is the hermetic PipelineStoreAPI implementation for handler tests.
type memStore struct {
	runs map[int64]*PipelineRun
}

func (m *memStore) List(_ context.Context, _ ListFilter) ([]PipelineRun, error) {
	out := []PipelineRun{}
	for _, r := range m.runs {
		out = append(out, *r)
	}
	return out, nil
}

func (m *memStore) Get(_ context.Context, id int64) (PipelineRun, error) {
	if r, ok := m.runs[id]; ok {
		return *r, nil
	}
	return PipelineRun{}, ErrRunNotFound
}

func (m *memStore) Create(_ context.Context, run *PipelineRun) error {
	run.ID = int64(len(m.runs) + 1)
	run.CreatedAt = time.Now()
	copied := *run
	m.runs[run.ID] = &copied
	return nil
}

func (m *memStore) UpdateBuild(_ context.Context, id, buildID int64) error {
	m.runs[id].JenkinsBuild = buildID
	return nil
}

func (m *memStore) ApplyWebhook(_ context.Context, buildID int64, service string, stages []Stage, runStatus, gitRevision, digest string) error {
	for _, r := range m.runs {
		if r.JenkinsBuild == buildID && (service == "" || r.Service == service) {
			r.Stages.Stages = MergeStages(r.Stages.Stages, stages)
			if runStatus != "" {
				r.Status = runStatus
				switch runStatus {
				case "running":
					now := time.Now()
					r.StartedAt = &now
				case "success", "failed":
					now := time.Now()
					r.FinishedAt = &now
				}
			}
			if gitRevision != "" {
				r.GitRevision = gitRevision
			}
			if digest != "" {
				r.ImageDigest = digest
			}
			return nil
		}
	}
	return ErrRunNotFound
}

func (m *memStore) SetRevertPR(_ context.Context, id int64, url string) error {
	m.runs[id].RevertPRURL = url
	return nil
}

func (m *memStore) SetStatus(_ context.Context, id int64, status string) error {
	m.runs[id].Status = status
	return nil
}

func (m *memStore) MarkTriggerFailed(_ context.Context, id int64, reason string) error {
	m.runs[id].Status = "failed"
	m.runs[id].ConfigRevision = reason
	return nil
}

var _ = testing.Short
