package core_v1alpha

import (
	"context"
	"fmt"
	"log/slog"

	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/appconfig"
	"miren.dev/runtime/pkg/entity"
)

// MigrateAppVersionConcurrency backfills missing service_concurrency config
// for all app versions using the same defaults applied at build time.
func MigrateAppVersionConcurrency(ctx context.Context, log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) error {
	log.Info("migrating app versions to include service concurrency defaults")

	resp, err := eac.List(ctx, entity.Ref(entity.EntityKind, KindAppVersion))
	if err != nil {
		return fmt.Errorf("failed to list app versions for migration: %w", err)
	}

	migratedCount := 0
	for _, e := range resp.Values() {
		var ver AppVersion
		ver.Decode(e.Entity())

		// Collect service names that need migration
		var servicesToMigrate []string
		for i := range ver.Config.Services {
			svc := &ver.Config.Services[i]
			if isServiceConcurrencyEmpty(&svc.ServiceConcurrency) {
				servicesToMigrate = append(servicesToMigrate, svc.Name)
			}
		}

		if len(servicesToMigrate) == 0 {
			continue
		}

		log.Info("migrating app version to add service concurrency defaults",
			"version", ver.ID,
			"services_to_migrate", servicesToMigrate)

		// Get defaults from appconfig (same as build time)
		ac := appconfig.GetDefaultsForServices(servicesToMigrate)

		// Apply defaults to services
		for i := range ver.Config.Services {
			svc := &ver.Config.Services[i]
			if !isServiceConcurrencyEmpty(&svc.ServiceConcurrency) {
				continue
			}

			// Map from appconfig to entity schema (same as build.go:423-432)
			if serviceConfig, ok := ac.Services[svc.Name]; ok && serviceConfig.Concurrency != nil {
				svc.ServiceConcurrency = ServiceConcurrency{
					Mode:                serviceConfig.Concurrency.Mode,
					NumInstances:        int64(serviceConfig.Concurrency.NumInstances),
					RequestsPerInstance: int64(serviceConfig.Concurrency.RequestsPerInstance),
					ScaleDownDelay:      serviceConfig.Concurrency.ScaleDownDelay,
				}
			}
		}

		// Update the entity
		var rpcE entityserver_v1alpha.Entity
		rpcE.SetId(ver.ID.String())
		rpcE.SetAttrs(entity.New(ver.Encode).Attrs())

		if _, err := eac.Put(ctx, &rpcE); err != nil {
			log.Error("failed to migrate app version",
				"version", ver.ID,
				"error", err)
			continue
		}

		migratedCount++
	}

	if migratedCount > 0 {
		log.Info("completed app version migration",
			"migrated", migratedCount)
	}

	return nil
}

// isServiceConcurrencyEmpty checks if concurrency config is missing/empty
func isServiceConcurrencyEmpty(sc *ServiceConcurrency) bool {
	return sc.Mode == "" && sc.RequestsPerInstance == 0 &&
		sc.NumInstances == 0 && sc.ScaleDownDelay == ""
}
