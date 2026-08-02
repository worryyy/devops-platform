package catalog

import (
	"testing"
)

func TestResolveReleaseBaseProfiles(t *testing.T) {
	catalog := loadTestCatalog(t)

	topic, _ := catalog.ServiceByName("topic")
	theme, _ := catalog.ServiceByName("theme")
	marketplace, _ := catalog.ServiceByName("marketplace")

	critical, err := ResolveRelease(catalog, topic, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if critical.Profile != "critical-canary" || critical.Definition.WaitTimeout != "75m" {
		t.Fatalf("topic should keep critical-canary: %#v", critical.Profile)
	}
	if critical.ManualPromotion {
		t.Fatalf("topic should have no manual promotion: %#v", critical)
	}

	rolling, err := ResolveRelease(catalog, theme, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if rolling.Profile != "fast-rolling" {
		t.Fatalf("theme should keep fast-rolling: %#v", rolling.Profile)
	}

	bluegreen, err := ResolveRelease(catalog, marketplace, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if bluegreen.Profile != "controlled-bluegreen" || !bluegreen.ManualPromotion {
		t.Fatalf("marketplace should stay bluegreen with manual promotion: %#v", bluegreen)
	}
}

func TestResolveReleaseIsStaticBaseline(t *testing.T) {
	catalog := loadTestCatalog(t)

	// No changed-file input exists anymore: every release uses the catalog
	// baseline regardless of what code paths were touched.
	for _, name := range []string{"topic", "theme", "file", "user"} {
		service, ok := catalog.ServiceByName(name)
		if !ok {
			t.Fatalf("missing service %q", name)
		}
		resolved, err := ResolveRelease(catalog, service, "dev")
		if err != nil {
			t.Fatal(err)
		}
		if resolved.Profile != service.RolloutProfile {
			t.Fatalf("service %q resolved to %q, want static baseline %q", name, resolved.Profile, service.RolloutProfile)
		}
	}
}

func TestResolveReleaseEnvironmentOverrides(t *testing.T) {
	catalog := loadTestCatalog(t)

	topic, _ := catalog.ServiceByName("topic")
	resolved, err := ResolveRelease(catalog, topic, "dev")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Definition.Analysis.MinSamples != 50 {
		t.Fatalf("dev must override critical-canary minSamples to 50, got %d", resolved.Definition.Analysis.MinSamples)
	}
	if resolved.Definition.Analysis.MaxErrorRate != 0.01 {
		t.Fatalf("dev override must not touch other thresholds, got %v", resolved.Definition.Analysis.MaxErrorRate)
	}

	prod, err := ResolveRelease(catalog, topic, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if prod.Definition.Analysis.MinSamples != 3000 {
		t.Fatalf("prod must keep catalog minSamples 3000, got %d", prod.Definition.Analysis.MinSamples)
	}
}

func TestExportFastRollingIsDeployment(t *testing.T) {
	catalog := loadTestCatalog(t)

	result, err := Export(catalog, []string{"theme"}, "dev")
	if err != nil {
		t.Fatal(err)
	}
	theme := result.Services[0]
	if theme.WorkloadKind != "Deployment" || theme.EffectiveProfile != "fast-rolling" {
		t.Fatalf("theme must export as a Deployment: %#v", theme)
	}
	if theme.Analysis.Enabled {
		t.Fatal("fast-rolling must not export analysis")
	}
	if theme.Rollout != theme.ResourceName {
		t.Fatalf("rollout must equal resource name: %q vs %q", theme.Rollout, theme.ResourceName)
	}
}
