package catalog

import (
	"encoding/json"
	"path/filepath"
	"strings"
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
	if len(result.Services) != 2 || result.Services[0].Image != "crpi-gfwwpdquc14b7w22-vpc.cn-shanghai.personal.cr.aliyuncs.com/pulseops/ecampus-topic" {
		t.Fatalf("unexpected export: %#v", result)
	}
	if result.Services[0].Rollout != "ecampus-topic" || result.Services[0].HealthPath != "/health" {
		t.Fatalf("incomplete rollout metadata: %#v", result.Services[0])
	}
	if result.Services[0].EffectiveProfile != "critical-canary" {
		t.Fatalf("topic must keep its static baseline critical-canary, got %q", result.Services[0].EffectiveProfile)
	}
	if result.Services[1].EffectiveProfile != "controlled-bluegreen" {
		t.Fatalf("chat must keep its static baseline controlled-bluegreen, got %q", result.Services[1].EffectiveProfile)
	}
}

func TestExportAnalysisContractUsesSnakeCase(t *testing.T) {
	data, err := Load(filepath.Clean("../../configs/service-catalog.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := Export(data, []string{"topic"}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(encoded)
	for _, key := range []string{
		`"min_samples"`,
		`"stable_min_samples"`,
		`"max_error_rate"`,
		`"max_error_rate_increase"`,
		`"max_p95_ratio"`,
		`"max_p95_seconds"`,
		`"min_operation_success_rate"`,
		`"inconclusive_limit"`,
		`"failure_limit"`,
		`"consecutive_success_limit"`,
		`"consecutive_error_limit"`,
		`"inconclusive_timeout"`,
	} {
		if !strings.Contains(raw, key) {
			t.Fatalf("delivery JSON must contain %s: %s", key, raw)
		}
	}
	for _, forbidden := range []string{
		`"minSamples"`,
		`"maxErrorRate"`,
		`"maxP95Ratio"`,
		`"matched_risk_rules"`,
		`"required_checks"`,
		`"riskRules"`,
	} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("delivery JSON must not contain %s: %s", forbidden, raw)
		}
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
}
