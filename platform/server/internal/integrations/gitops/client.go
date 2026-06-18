package gitops

import "context"

type Change struct {
	Repository string
	Path       string
	Message    string
	Content    []byte
}

type Client interface {
	Commit(ctx context.Context, change Change) (commitSHA string, err error)
}
