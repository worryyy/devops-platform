package catalog

import (
	"path/filepath"
	"testing"
)

func TestExportDeliveryServices(t *testing.T) {
	data, err := Load(filepath.Clean("../../configs/service-catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Export(data, []string{"topic", "chat"}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Services) != 2 || result.Services[0].Image != "crpi-gfwwpdquc14b7w22.cn-shanghai.personal.cr.aliyuncs.com/pulseops/ecampus-topic" {
		t.Fatalf("unexpected export: %#v", result)
	}
	if result.Services[0].Workload != "ecampus-topic" || result.Services[0].HealthPath != "/health" {
		t.Fatalf("incomplete workload metadata: %#v", result.Services[0])
	}
	for _, service := range result.Services {
		if service.WorkloadKind != "Deployment" || service.ResourceName != service.Workload {
			t.Fatalf("every service must export as a Deployment workload: %#v", service)
		}
		if service.WaitTimeout != "15m" {
			t.Fatalf("unexpected wait timeout %q", service.WaitTimeout)
		}
		if service.MaxP95Seconds <= 0 {
			t.Fatalf("service %q must carry its SLI latency budget", service.Service)
		}
	}
	if result.Services[0].MaxP95Seconds != 0.5 {
		t.Fatalf("topic must keep its SLI p95 budget, got %v", result.Services[0].MaxP95Seconds)
	}
}

func TestExportRejectsMissingAndDuplicateServices(t *testing.T) {
	data, err := Load(filepath.Clean("../../configs/service-catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Export(data, []string{"topic", "topic"}, "dev"); err == nil {
		t.Fatal("expected duplicate service error")
	}
	if _, err := Export(data, []string{"crm"}, "dev"); err == nil {
		t.Fatal("expected missing service error")
	}
	if _, err := Export(data, []string{"topic"}, "prod"); err == nil {
		t.Fatal("expected missing environment error")
	}
}
