package catalog

import (
	"path/filepath"
	"testing"
)

func loadTestCatalog(t *testing.T) Catalog {
	t.Helper()
	catalog, err := Load(filepath.Clean("../../configs/service-catalog.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	return catalog
}

func TestProfileThresholdsAreTiered(t *testing.T) {
	catalog := loadTestCatalog(t)

	critical := catalog.ReleaseProfiles["critical-canary"]
	standard := catalog.ReleaseProfiles["standard-canary"]
	bluegreen := catalog.ReleaseProfiles["controlled-bluegreen"]
	rolling := catalog.ReleaseProfiles["fast-rolling"]

	if critical.Analysis.MinSamples <= standard.Analysis.MinSamples {
		t.Fatalf("critical minSamples %d must exceed standard %d", critical.Analysis.MinSamples, standard.Analysis.MinSamples)
	}
	if standard.Analysis.MinSamples < bluegreen.Analysis.MinSamples {
		t.Fatalf("standard minSamples %d must not trail bluegreen %d", standard.Analysis.MinSamples, bluegreen.Analysis.MinSamples)
	}
	if bluegreen.Analysis.MaxErrorRate > critical.Analysis.MaxErrorRate {
		t.Fatalf("bluegreen maxErrorRate %v must not be looser than critical %v", bluegreen.Analysis.MaxErrorRate, critical.Analysis.MaxErrorRate)
	}
	if bluegreen.Analysis.MaxErrorRate >= standard.Analysis.MaxErrorRate {
		t.Fatalf("bluegreen maxErrorRate %v must be stricter than standard %v", bluegreen.Analysis.MaxErrorRate, standard.Analysis.MaxErrorRate)
	}
	if critical.Analysis.MaxErrorRate >= standard.Analysis.MaxErrorRate {
		t.Fatalf("critical maxErrorRate %v must be stricter than standard %v", critical.Analysis.MaxErrorRate, standard.Analysis.MaxErrorRate)
	}
	if critical.Analysis.MaxP95Ratio >= standard.Analysis.MaxP95Ratio {
		t.Fatalf("critical maxP95Ratio %v must be stricter than standard %v", critical.Analysis.MaxP95Ratio, standard.Analysis.MaxP95Ratio)
	}
	if bluegreen.Analysis.InconclusiveLimit != 2 {
		t.Fatalf("bluegreen inconclusiveLimit = %d, want 2 (fail fast)", bluegreen.Analysis.InconclusiveLimit)
	}
	for name, profile := range catalog.ReleaseProfiles {
		if profile.Strategy == "rolling" {
			continue
		}
		if profile.Analysis.ConsecutiveErrorLimit <= 0 {
			t.Fatalf("profile %q must configure consecutiveErrorLimit", name)
		}
	}
	if rolling.Analysis.Enabled || rolling.ManualPromotion {
		t.Fatalf("fast-rolling must disable analysis and manual promotion: %#v", rolling.Analysis)
	}
	if !bluegreen.ManualPromotion || bluegreen.PreviewReplicaCount <= 0 {
		t.Fatalf("controlled-bluegreen must require manual promotion and preview replicas")
	}
}

func TestValidateRejectsBrokenProfiles(t *testing.T) {
	catalog := loadTestCatalog(t)

	broken := catalog
	broken.ReleaseProfiles = map[string]ReleaseProfile{}
	for name, profile := range catalog.ReleaseProfiles {
		broken.ReleaseProfiles[name] = profile
	}
	rolling := broken.ReleaseProfiles["fast-rolling"]
	rolling.Analysis.Enabled = true
	broken.ReleaseProfiles["fast-rolling"] = rolling
	if err := Validate(broken); err == nil {
		t.Fatal("expected rolling profile with analysis to be rejected")
	}

	broken = catalog
	broken.ReleaseProfiles = map[string]ReleaseProfile{}
	for name, profile := range catalog.ReleaseProfiles {
		broken.ReleaseProfiles[name] = profile
	}
	bluegreen := broken.ReleaseProfiles["controlled-bluegreen"]
	bluegreen.ManualPromotion = false
	broken.ReleaseProfiles["controlled-bluegreen"] = bluegreen
	if err := Validate(broken); err == nil {
		t.Fatal("expected bluegreen without manual promotion to be rejected")
	}

	broken = catalog
	broken.ReleaseProfiles = map[string]ReleaseProfile{}
	for name, profile := range catalog.ReleaseProfiles {
		broken.ReleaseProfiles[name] = profile
	}
	delete(broken.ReleaseProfiles, "critical-canary")
	if err := Validate(broken); err == nil {
		t.Fatal("expected missing critical-canary to be rejected")
	}
}
