package deploy

import (
	"fmt"

	"github.com/worryyy/devops-platform/platform/server/internal/pipeline"
)

func dryRunDetail(input DryRunInput, values string) map[string]interface{} {
	return map[string]interface{}{
		"success": true,
		"command": fmt.Sprintf("helm template %s <chart> -f values.yaml", input.ServiceName),
		"values_summary": map[string]interface{}{
			"service":      input.ServiceName,
			"environment":  input.Environment,
			"namespace":    input.Namespace,
			"image_repo":   input.ImageRepo,
			"image_tag":    input.ImageTag,
			"image_digest": input.ImageDigest,
		},
		"rendered_resources": []interface{}{
			fmt.Sprintf("Deployment/%s", input.ServiceName),
			fmt.Sprintf("Service/%s", input.ServiceName),
		},
		"diff": map[string]interface{}{
			"target_image": map[string]interface{}{
				"repo":   input.ImageRepo,
				"tag":    input.ImageTag,
				"digest": input.ImageDigest,
			},
		},
		"values_yaml": values,
	}
}

func valuesSnapshot(input DryRunInput) string {
	return fmt.Sprintf("image:\n  repository: %s\n  tag: %s\n  digest: %s\n", input.ImageRepo, input.ImageTag, input.ImageDigest)
}

func dryRunStages(deployID string, detail map[string]interface{}) []pipeline.CreateStageInput {
	allStages := pipeline.DefaultDeployStages(true)
	stages := make([]pipeline.CreateStageInput, 0, 6)
	for _, stage := range allStages {
		if stage.StageOrder > 60 {
			continue
		}
		stages = append(stages, stage)
	}
	for i := range stages {
		stages[i].DeployID = deployID
		switch stages[i].Stage {
		case pipeline.DeployStagePrecheck, pipeline.DeployStageRenderValues, pipeline.DeployStageHelmLint, pipeline.DeployStageHelmTemplate:
			stages[i].Status = pipeline.StatusSuccess
		case pipeline.DeployStageDryRun:
			stages[i].Status = pipeline.StatusSuccess
			stages[i].Message = "dry-run completed"
			stages[i].Detail = detail
		case pipeline.DeployStageWaitingConfirm:
			stages[i].Status = pipeline.StatusRunning
			stages[i].Message = "waiting for deploy confirmation"
		}
	}
	return stages
}
