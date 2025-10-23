package core_v1alpha

import (
	"fmt"

	entity "miren.dev/runtime/pkg/entity"
)

func MD(ea entity.AttrGetter) Metadata {
	var md Metadata
	md.Decode(ea)
	return md
}

// GetServiceConcurrency returns the concurrency configuration for a named service.
// Returns an error if the service is not found - all service configs should be
// hydrated with defaults during app version creation.
func GetServiceConcurrency(ver *AppVersion, serviceName string) (*ServiceConcurrency, error) {
	for _, svc := range ver.Config.Services {
		if svc.Name == serviceName {
			return &svc.ServiceConcurrency, nil
		}
	}
	return nil, fmt.Errorf("service %q not found in version config (services should be hydrated with defaults)", serviceName)
}
