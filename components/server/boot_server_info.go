//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/boot"
	"miren.dev/runtime/pkg/serverinfo"
	serverinfosrv "miren.dev/runtime/servers/serverinfo"
)

const ServerInfoService = "dev.miren.runtime/server-info"

type serverInfoBoot struct {
	component *boot.Component
	source    *serverinfo.Source
}

// Exposed as soon as the RPC server is up so callers can watch the process
// become ready.
func newServerInfoBoot(source *serverinfo.Source, foundation boot.Output[foundationBootOutput]) *serverInfoBoot {
	b := &serverInfoBoot{source: source}
	b.component = boot.Run1("server-info", foundation, b.start)
	return b
}

func (b *serverInfoBoot) start(_ context.Context, foundation foundationBootOutput) error {
	foundation.foundation.Server().ExposeValue(ServerInfoService, server_v1alpha.AdaptServerInfo(serverinfosrv.NewServer(b.source)))
	return nil
}
