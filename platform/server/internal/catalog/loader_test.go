package catalog

import (
	"path/filepath"
	"testing"
)

func TestLoadServiceCatalog(t *testing.T) {
	catalog, err := Load(filepath.Clean("../../configs/service-catalog.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if catalog.Version != "v1" {
		t.Fatalf("Version = %q, want v1", catalog.Version)
	}
	if len(catalog.Services) != 13 {
		t.Fatalf("Services = %d, want 13", len(catalog.Services))
	}
	expected := []string{"academic", "agentchat", "chat", "comment", "file", "marketplace", "moderation", "notification", "reservation", "school", "theme", "topic", "user"}
	for index, service := range catalog.Services {
		if service.Name != expected[index] {
			t.Fatalf("Services[%d] = %q, want %q", index, service.Name, expected[index])
		}
	}
	service, ok := catalog.ServiceByName("topic")
	if !ok {
		t.Fatalf("ServiceByName(topic) = false")
	}
	environment, ok := service.EnvironmentByName("dev")
	if !ok {
		t.Fatalf("EnvironmentByName(dev) = false")
	}
	if !BranchAllowed(environment.BranchPolicy, "main") {
		t.Fatalf("BranchAllowed(main) = false")
	}
	if service.Kind != "go-service" {
		t.Fatalf("Kind = %q, want go-service", service.Kind)
	}
	if environment.Image.Repository != "crpi-gfwwpdquc14b7w22-vpc.cn-shanghai.personal.cr.aliyuncs.com/pulseops/ecampus-topic" {
		t.Fatalf("Image.Repository = %q", environment.Image.Repository)
	}
}

func TestBranchAllowed(t *testing.T) {
	policy := BranchPolicy{
		AllowedBranches: []string{"main", "develop", "feature/*"},
	}
	cases := map[string]bool{
		"main":            true,
		"develop":         true,
		"feature/x":       true,
		"feature/foo/bar": true,
		"bugfix/x":        false,
		"feat":            false,
	}
	for branch, want := range cases {
		if got := BranchAllowed(policy, branch); got != want {
			t.Fatalf("BranchAllowed(%q) = %v, want %v", branch, got, want)
		}
	}
}
