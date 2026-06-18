package deploy

import "github.com/worryyy/devops-platform/platform/server/internal/pkg/bizerr"

func validateDryRun(input DryRunInput) error {
	if input.ServiceName == "" {
		return bizerr.Param("service_name is required")
	}
	if input.Environment == "" {
		return bizerr.Param("environment is required")
	}
	if input.ImageRepo == "" {
		return bizerr.Param("image_repo is required")
	}
	if input.ImageTag == "" {
		return bizerr.Param("image_tag is required")
	}
	if input.ImageDigest == "" {
		return bizerr.Param("image_digest is required")
	}
	if input.Deployer == "" {
		return bizerr.Param("deployer is required")
	}
	return nil
}
