package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// JenkinsClient is a thin HTTP client over the Jenkins remote API using an
// API token (no CSRF crumb needed for token-authenticated POSTs).
type JenkinsClient struct {
	BaseURL string
	User    string
	Token   string
	Job     string
	HTTP    *http.Client
	// PollInterval spaces queue-item polls (tests shrink it).
	PollInterval time.Duration
}

func NewJenkinsClient(baseURL, user, token, job string) *JenkinsClient {
	return &JenkinsClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		User:    user,
		Token:   token,
		Job:     job,
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		PollInterval: 5 * time.Second,
	}
}

// TriggerBuild enqueues a parameterized build and returns the queued item id
// plus the queue URL Jenkins answers with.
func (c *JenkinsClient) TriggerBuild(ctx context.Context, params map[string]string) (queueItem int64, err error) {
	form := url.Values{}
	for k, v := range params {
		form.Set(k, v)
	}
	endpoint := fmt.Sprintf("%s/job/%s/buildWithParameters", c.BaseURL, c.Job)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, fmt.Errorf("build trigger request: %w", err)
	}
	req.SetBasicAuth(c.User, c.Token)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("trigger build: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if resp.StatusCode != http.StatusCreated {
		return 0, fmt.Errorf("trigger build: status %d: %s", resp.StatusCode, string(body))
	}
	// Location: .../queue/item/123/
	location := resp.Header.Get("Location")
	return queueItemFromURL(location), nil
}

func queueItemFromURL(location string) int64 {
	parts := strings.Split(strings.TrimRight(location, "/"), "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "item" && i+1 < len(parts) {
			id, err := strconv.ParseInt(parts[i+1], 10, 64)
			if err == nil {
				return id
			}
		}
	}
	return 0
}

// queueItemState is the subset of the queue API entry we need.
type queueItemState struct {
	Cancelled  bool `json:"cancelled"`
	Executable struct {
		Number int64 `json:"number"`
	} `json:"executable"`
}

// ResolveQueueItem polls the queue entry until the queued build starts (or
// the context deadline passes) and returns its build number. The platform
// stores the queue id first and overwrites it with the build number as soon
// as the Jenkins agent launches — webhook and backfill lookups key on the
// build number.
func (c *JenkinsClient) ResolveQueueItem(ctx context.Context, queueItem int64) (int64, error) {
	interval := c.PollInterval
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	endpoint := fmt.Sprintf("%s/queue/item/%d/api/json", c.BaseURL, queueItem)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return 0, fmt.Errorf("queue item request: %w", err)
		}
		req.SetBasicAuth(c.User, c.Token)
		resp, err := c.HTTP.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<10))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var state queueItemState
				if err := json.Unmarshal(body, &state); err == nil {
					if state.Cancelled {
						return 0, fmt.Errorf("queue item %d cancelled", queueItem)
					}
					if state.Executable.Number != 0 {
						return state.Executable.Number, nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
		}
	}
}

// BuildInfo is the subset of the Jenkins build API the platform shows.
type BuildInfo struct {
	Number    int64  `json:"number"`
	Result    string `json:"result"` // success | failure | aborted | "" while building
	Building  bool   `json:"building"`
	Timestamp int64  `json:"timestamp"`
	Duration  int64  `json:"duration"`
	URL       string `json:"url"`
}

// BuildStatus fetches one build's state.
func (c *JenkinsClient) BuildStatus(ctx context.Context, buildID int64) (BuildInfo, error) {
	endpoint := fmt.Sprintf("%s/job/%s/%d/api/json", c.BaseURL, c.Job, buildID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return BuildInfo{}, fmt.Errorf("build status request: %w", err)
	}
	req.SetBasicAuth(c.User, c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return BuildInfo{}, fmt.Errorf("build status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return BuildInfo{}, fmt.Errorf("build %d not found", buildID)
	}
	if resp.StatusCode != http.StatusOK {
		return BuildInfo{}, fmt.Errorf("build status: status %d", resp.StatusCode)
	}
	var info BuildInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return BuildInfo{}, fmt.Errorf("decode build status: %w", err)
	}
	return info, nil
}

// QueueItemInfo is the queue API's assignment view.
type QueueItemInfo struct {
	Cancelled  bool `json:"cancelled"`
	Executable *struct {
		Number int64 `json:"number"`
	} `json:"executable"`
}

// ResolveBuild polls a queue item until Jenkins assigns the build number
// (bounded; returns 0 when it times out or the item is cancelled).
func (c *JenkinsClient) ResolveBuild(ctx context.Context, queueItem int64, attempts int, delay time.Duration) (int64, error) {
	endpoint := fmt.Sprintf("%s/queue/item/%d/api/json", c.BaseURL, queueItem)
	for i := 0; i < attempts; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return 0, err
		}
		req.SetBasicAuth(c.User, c.Token)
		resp, err := c.HTTP.Do(req)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var info QueueItemInfo
				if json.Unmarshal(body, &info) == nil {
					if info.Cancelled {
						return 0, fmt.Errorf("queue item %d cancelled", queueItem)
					}
					if info.Executable != nil {
						return info.Executable.Number, nil
					}
				}
			}
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(delay):
		}
	}
	return 0, fmt.Errorf("queue item %d not assigned after %d attempts", queueItem, attempts)
}
