package registry

import "context"

type ImageRef struct {
	Repository string
	Tag        string
	Digest     string
}

type Client interface {
	ResolveDigest(ctx context.Context, repository, tag string) (ImageRef, error)
	Exists(ctx context.Context, repository, digestOrTag string) (bool, error)
}
