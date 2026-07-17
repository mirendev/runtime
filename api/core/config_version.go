package compute

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
)

// ConfigSpecFromConfig converts the existing inline Config into a
// ConfigSpec suitable for a ConfigVersion entity. This is used during
// dual-write to create ConfigVersion entities alongside inline Config.
func ConfigSpecFromConfig(cfg *core_v1alpha.Config) core_v1alpha.ConfigSpec {
	spec := core_v1alpha.ConfigSpec{
		Entrypoint:     cfg.Entrypoint,
		StartDirectory: cfg.StartDirectory,
	}

	// Build a map of service -> command for merging into services
	cmdMap := make(map[string]string, len(cfg.Commands))
	for _, cmd := range cfg.Commands {
		cmdMap[cmd.Service] = cmd.Command
	}

	// Convert variables
	for _, v := range cfg.Variable {
		spec.Variables = append(spec.Variables, core_v1alpha.ConfigSpecVariables(v))
	}

	// Convert services, merging in commands
	for _, svc := range cfg.Services {
		s := core_v1alpha.ConfigSpecServices{
			Name:     svc.Name,
			Command:  cmdMap[svc.Name],
			Port:     svc.Port,
			PortName: svc.PortName,
			PortType: svc.PortType,
			Image:    svc.Image,
			Concurrency: core_v1alpha.ConfigSpecServicesConcurrency{
				Mode:                svc.ServiceConcurrency.Mode,
				NumInstances:        svc.ServiceConcurrency.NumInstances,
				RequestsPerInstance: svc.ServiceConcurrency.RequestsPerInstance,
				ScaleDownDelay:      svc.ServiceConcurrency.ScaleDownDelay,
				ShutdownTimeout:     svc.ServiceConcurrency.ShutdownTimeout,
			},
		}

		// Convert ports array
		for _, p := range svc.Ports {
			sp := core_v1alpha.ConfigSpecServicesPorts{
				Port:     p.Port,
				Name:     p.Name,
				Type:     p.Type,
				NodePort: p.NodePort,
			}
			switch p.Protocol {
			case core_v1alpha.TCP:
				sp.Protocol = core_v1alpha.ConfigSpecServicesPortsTCP
			case core_v1alpha.UDP:
				sp.Protocol = core_v1alpha.ConfigSpecServicesPortsUDP
			}
			s.Ports = append(s.Ports, sp)
		}

		// Convert service-level env vars
		for _, e := range svc.Env {
			s.Env = append(s.Env, core_v1alpha.ConfigSpecServicesEnv(e))
		}

		// Convert disks (field-by-field since Provider enum types differ)
		for _, d := range svc.Disks {
			provider := core_v1alpha.ConfigSpecServicesDisksMIREN
			switch d.Provider {
			case core_v1alpha.LOCAL:
				provider = core_v1alpha.ConfigSpecServicesDisksLOCAL
			case core_v1alpha.MIREN:
				provider = core_v1alpha.ConfigSpecServicesDisksMIREN
			}
			s.Disks = append(s.Disks, core_v1alpha.ConfigSpecServicesDisks{
				Name:         d.Name,
				Provider:     provider,
				MountPath:    d.MountPath,
				ReadOnly:     d.ReadOnly,
				SizeGb:       d.SizeGb,
				Filesystem:   d.Filesystem,
				LeaseTimeout: d.LeaseTimeout,
				Owner:        d.Owner,
			})
		}

		spec.Services = append(spec.Services, s)
	}

	return spec
}

// ResolveConfig loads the configuration for an AppVersion. If the version has
// a ConfigVersion, it loads the ConfigVersion entity and returns its spec directly.
// Otherwise, it falls back to the inline Config field and converts it.
func ResolveConfig(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient, ver *core_v1alpha.AppVersion) (*core_v1alpha.ConfigSpec, error) {
	if ver.ConfigVersion != "" {
		ret, err := eac.Get(ctx, ver.ConfigVersion.String())
		if err != nil {
			return nil, fmt.Errorf("failed to get config version %s: %w", ver.ConfigVersion, err)
		}

		var cv core_v1alpha.ConfigVersion
		cv.Decode(ret.Entity().Entity())

		return &cv.Spec, nil
	}

	// Fallback to inline config, converting to ConfigSpec
	spec := ConfigSpecFromConfig(&ver.Config)
	return &spec, nil
}

// GetServiceConcurrency returns the concurrency configuration for a
// named service from a ConfigSpec.
func GetServiceConcurrency(spec *core_v1alpha.ConfigSpec, serviceName string) (core_v1alpha.ConfigSpecServicesConcurrency, error) {
	for _, svc := range spec.Services {
		if svc.Name == serviceName {
			return svc.Concurrency, nil
		}
	}
	return core_v1alpha.ConfigSpecServicesConcurrency{}, fmt.Errorf("service %q not found in config (services should be hydrated with defaults)", serviceName)
}
