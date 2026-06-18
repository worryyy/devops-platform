package helm

import "context"

type RenderInput struct {
	ReleaseName string
	ChartPath   string
	ValuesYAML  []byte
	Namespace   string
}

type RenderResult struct {
	Success           bool
	Warnings          []string
	RenderedResources []string
	Output            string
}

type Client interface {
	Lint(ctx context.Context, input RenderInput) error
	Template(ctx context.Context, input RenderInput) (RenderResult, error)
	DryRun(ctx context.Context, input RenderInput) (RenderResult, error)
}
