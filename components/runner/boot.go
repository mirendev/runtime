package runner

import (
	"context"
	"time"

	"miren.dev/runtime/pkg/boot"
)

// CapabilityBoot owns one independently started runner capability.
type CapabilityBoot[T any] struct {
	Component *boot.Component
	Output    boot.Output[T]
}

// NewStorageAgentBoot starts storage reconciliation after storage and the
// sandbox host are both ready.
func NewStorageAgentBoot(storage boot.Output[*NodeStorage], host *boot.Component, stopTimeout time.Duration) *CapabilityBoot[*StorageAgent] {
	b := &CapabilityBoot[*StorageAgent]{}
	var value *StorageAgent
	b.Component, b.Output = boot.Provide1("storage-agent", storage,
		func(ctx context.Context, storage *NodeStorage) (*StorageAgent, error) {
			value = NewStorageAgent(storage)
			if err := value.Start(ctx); err != nil {
				return nil, err
			}
			return value, nil
		},
		boot.DependsOn(host),
		boot.WithStop(func(context.Context) error {
			if value == nil {
				return nil
			}
			return value.Close()
		}, stopTimeout),
	)
	return b
}

// NewSandboxAgentBoot starts sandbox reconciliation after the restored host
// and any composition-specific order-only dependencies are ready.
func NewSandboxAgentBoot(host boot.Output[*SandboxHost], stopTimeout time.Duration, dependencies ...*boot.Component) *CapabilityBoot[*SandboxAgent] {
	b := &CapabilityBoot[*SandboxAgent]{}
	var value *SandboxAgent
	b.Component, b.Output = boot.Provide1("sandbox-agent", host,
		func(ctx context.Context, host *SandboxHost) (*SandboxAgent, error) {
			value = NewSandboxAgent(host)
			if err := value.Start(ctx); err != nil {
				return nil, err
			}
			return value, nil
		},
		boot.DependsOn(dependencies...),
		boot.WithStop(func(context.Context) error {
			if value == nil {
				return nil
			}
			return value.Close()
		}, stopTimeout),
	)
	return b
}

// NewNodePresenceBoot publishes node readiness only after both agents are
// active.
func NewNodePresenceBoot(host boot.Output[*SandboxHost], storageAgent, sandboxAgent *boot.Component, stopTimeout time.Duration) *CapabilityBoot[*NodePresence] {
	b := &CapabilityBoot[*NodePresence]{}
	var value *NodePresence
	b.Component, b.Output = boot.Provide1("node-presence", host,
		func(ctx context.Context, host *SandboxHost) (*NodePresence, error) {
			value = NewNodePresence(host)
			if err := value.Start(ctx); err != nil {
				return nil, err
			}
			return value, nil
		},
		boot.DependsOn(storageAgent, sandboxAgent),
		boot.WithStop(func(context.Context) error {
			if value == nil {
				return nil
			}
			return value.Close()
		}, stopTimeout),
	)
	return b
}
