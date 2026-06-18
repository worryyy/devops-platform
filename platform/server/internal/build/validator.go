package build

import "github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"

func validateCreate(input CreateInput) error {
	if input.ServiceName == "" {
		return bizerr.Param("service_name is required")
	}
	if input.Branch == "" {
		return bizerr.Param("branch is required")
	}
	if input.CommitSHA == "" {
		return bizerr.Param("commit_sha is required")
	}
	if input.Builder == "" {
		return bizerr.Param("builder is required")
	}
	if input.TriggerType == "" {
		input.TriggerType = TriggerManual
	}
	return nil
}
