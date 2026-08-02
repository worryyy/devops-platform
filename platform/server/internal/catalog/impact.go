package catalog

import (
	"fmt"
)

type EffectiveRelease struct {
	Profile         string
	Definition      ReleaseProfile
	ManualPromotion bool
}

// ResolveRelease returns the service's static release baseline. Strategy is
// fixed per service in the catalog; only per-environment analysis thresholds
// and the service/profile manual-promotion flag are applied.
func ResolveRelease(catalog Catalog, service Service, environment string) (EffectiveRelease, error) {
	profileName := service.RolloutProfile
	manualPromotion := false
	if service.ManualPromotion != nil {
		manualPromotion = *service.ManualPromotion
	}

	definition, ok := catalog.ReleaseProfiles[profileName]
	if !ok {
		return EffectiveRelease{}, fmt.Errorf("release profile %q does not exist", profileName)
	}
	if override, ok := catalog.EnvironmentOverrides[environment].Profiles[profileName]; ok {
		if override.Analysis.MinSamples != nil {
			definition.Analysis.MinSamples = *override.Analysis.MinSamples
		}
		if override.Analysis.StableMinSamples != nil {
			definition.Analysis.StableMinSamples = *override.Analysis.StableMinSamples
		}
	}
	manualPromotion = manualPromotion || definition.ManualPromotion
	return EffectiveRelease{
		Profile: profileName, Definition: definition, ManualPromotion: manualPromotion,
	}, nil
}
