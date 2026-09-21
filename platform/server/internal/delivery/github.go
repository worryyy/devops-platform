package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GitHubClient covers the two calls the delivery module needs: create a
// revert PR, and (fallback) push a revert branch via the git-free contents API
// is intentionally NOT implemented — see plan §4 risk table.
type GitHubClient struct {
	Token string
	Owner string
	Repo  string
	HTTP  *http.Client
}

func NewGitHubClient(token, owner, repo string) *GitHubClient {
	return &GitHubClient{
		Token: token,
		Owner: owner,
		Repo:  repo,
		HTTP:  &http.Client{Timeout: 30 * time.Second},
	}
}

type PullRequest struct {
	URL    string `json:"html_url"`
	Number int64  `json:"number"`
	State  string `json:"state"`
}

// CreateRevertPR asks GitHub to revert one commit and open a PR for it.
// Endpoint: POST /repos/{owner}/{repo}/reverts.
func (c *GitHubClient) CreateRevertPR(ctx context.Context, commitSHA, branch, title string) (PullRequest, error) {
	body := map[string]any{
		"commit_sha": commitSHA,
		"branch":     branch,
		"title":      title,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return PullRequest{}, err
	}
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s/reverts", c.Owner, c.Repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return PullRequest{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return PullRequest{}, fmt.Errorf("github revert: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode == http.StatusConflict {
		return PullRequest{}, fmt.Errorf("revert conflict: nothing to revert or branch exists (%s)", string(respBody))
	}
	if resp.StatusCode != http.StatusCreated {
		return PullRequest{}, fmt.Errorf("github revert: status %d: %s", resp.StatusCode, string(respBody))
	}
	var pr PullRequest
	if err := json.Unmarshal(respBody, &pr); err != nil {
		return PullRequest{}, fmt.Errorf("decode revert PR: %w", err)
	}
	return pr, nil
}
