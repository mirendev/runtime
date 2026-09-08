//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
)

type secretStoreBootOutput struct {
	secretStore *coordinate.SecretStore
}

type secretStoreBoot struct {
	component *boot.Component
	value     *coordinate.SecretStore
	output    boot.Output[secretStoreBootOutput]
}

func newSecretStoreBoot(foundation boot.Output[foundationBootOutput]) *secretStoreBoot {
	b := &secretStoreBoot{}
	b.component, b.output = boot.Provide1(
		"secret-store", foundation, b.start,
		boot.WithStop(b.stop, componentStopTimeout),
	)
	return b
}

func (b *secretStoreBoot) start(ctx context.Context, foundation foundationBootOutput) (secretStoreBootOutput, error) {
	b.value = coordinate.NewSecretStore(foundation.foundation)
	if err := b.value.Start(ctx); err != nil {
		return secretStoreBootOutput{}, err
	}
	return secretStoreBootOutput{secretStore: b.value}, nil
}

func (b *secretStoreBoot) stop(context.Context) error {
	if b.value != nil {
		b.value.Stop()
	}
	return nil
}
