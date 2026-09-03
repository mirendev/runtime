//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/api/admin/admin_v1alpha"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/servers/admin"
)

type adminBoot struct {
	component *boot.Component
}

func newAdminBoot(foundation boot.Output[foundationBootOutput], entityAccess boot.Output[entityAccessBootOutput], ingress boot.Output[ingressBootOutput], observability boot.Output[observabilityBootOutput]) *adminBoot {
	b := &adminBoot{}
	b.component = boot.Run4("admin-api", foundation, entityAccess, ingress, observability, b.start)
	return b
}

func (b *adminBoot) start(_ context.Context, foundation foundationBootOutput, entityAccess entityAccessBootOutput, ingress ingressBootOutput, observability observabilityBootOutput) error {
	server := admin.NewServer(observability.log, entityAccess.client, ingress.server, observability.logWriter)
	foundation.foundation.Server().ExposeValue("dev.miren.runtime/admin", admin_v1alpha.AdaptAdmin(server))
	return nil
}
