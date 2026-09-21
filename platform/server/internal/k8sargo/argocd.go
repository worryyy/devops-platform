package k8sargo

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var applicationGVR = schema.GroupVersionResource{
	Group:    "argoproj.io",
	Version:  "v1alpha1",
	Resource: "applications",
}

// AppStatus is the Argo CD Application view shown on the pipeline detail page.
type AppStatus struct {
	Name        string `json:"name"`
	SyncStatus  string `json:"syncStatus"`
	Health      string `json:"health"`
	SyncVersion string `json:"syncVersion"`
}

// Client reads Argo CD Application objects via the in-cluster config.
type Client struct{ iface dynamic.Interface }

func NewInCluster() (*Client, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	iface, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("dynamic client: %w", err)
	}
	return &Client{iface: iface}, nil
}

// Get reads one Application's sync/health status from the argocd namespace.
func (c *Client) Get(ctx context.Context, name string) (AppStatus, error) {
	obj, err := c.iface.Resource(applicationGVR).Namespace("argocd").
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return AppStatus{}, fmt.Errorf("get application %s: %w", name, err)
	}
	status, ok := obj.Object["status"].(map[string]any)
	if !ok {
		return AppStatus{Name: name}, nil
	}
	out := AppStatus{Name: name}
	if sync, ok := status["sync"].(map[string]any); ok {
		out.SyncStatus, _ = sync["status"].(string)
		out.SyncVersion, _ = sync["revision"].(string)
	}
	if health, ok := status["health"].(map[string]any); ok {
		out.Health, _ = health["status"].(string)
	}
	return out, nil
}

// StatusReader is the subset the API handlers depend on (mockable in tests).
type StatusReader interface {
	Get(ctx context.Context, name string) (AppStatus, error)
}
