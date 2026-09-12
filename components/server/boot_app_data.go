//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/pkg/boot"
)

type appDataBoot struct {
	component *boot.Component
}

func newAppDataBoot(foundation boot.Output[foundationBootOutput]) *appDataBoot {
	b := &appDataBoot{}
	b.component = boot.Run1("app-version-migration", foundation, b.start)
	return b
}

func (b *appDataBoot) start(ctx context.Context, foundation foundationBootOutput) error {
	return foundation.foundation.PrepareAppData(ctx)
}
