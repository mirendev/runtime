//go:build linux

package server

import (
	"context"

	"miren.dev/runtime/components/coordinate"
	"miren.dev/runtime/pkg/boot"
)

type resourceUsageBootOutput struct {
	resourceUsage *coordinate.ResourceUsage
}

type resourceUsageBoot struct {
	component *boot.Component
	value     *coordinate.ResourceUsage
	output    boot.Output[resourceUsageBootOutput]
}

func newResourceUsageBoot(foundation boot.Output[foundationBootOutput]) *resourceUsageBoot {
	b := &resourceUsageBoot{}
	b.component, b.output = boot.Provide1("resource-usage", foundation, b.start)
	return b
}

func (b *resourceUsageBoot) start(ctx context.Context, foundation foundationBootOutput) (resourceUsageBootOutput, error) {
	b.value = coordinate.NewResourceUsage(foundation.foundation)
	if err := b.value.Start(ctx); err != nil {
		return resourceUsageBootOutput{}, err
	}
	return resourceUsageBootOutput{resourceUsage: b.value}, nil
}
