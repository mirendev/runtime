package addons

import (
	"context"
	"log/slog"

	"github.com/mitchellh/mapstructure"
	"miren.dev/runtime/pkg/asm/autoreg"
)

type Plan interface {
	Name() string
}

type InstanceId string

type InstanceConfig struct {
	Id        InstanceId
	Container string

	Env map[string]string

	Config map[string]any
}

func (c *InstanceConfig) Map(v any) error {
	return mapstructure.Decode(c.Config, v)
}

func (c *InstanceConfig) SetConfig(v any) error {
	return mapstructure.Decode(v, &c.Config)
}

type Status string

const (
	StatusUnknown Status = "unknown"
	StatusReady   Status = "ready"
	StatusRunning Status = "running"
	StatusError   Status = "error"
)

type Addon interface {
	Plans() []Plan

	Default() Plan

	Provision(ctx context.Context, name string, plan Plan) (*InstanceConfig, error)
	HealthCheck(ctx context.Context, cfg *InstanceConfig) (Status, error)
	Deprovision(ctx context.Context, cfg *InstanceConfig) error
}

type Registry struct {
	Log      *slog.Logger
	registry map[string]Addon
}

var _ = autoreg.Register[Registry]()

func (r *Registry) Populated() error {
	r.registry = make(map[string]Addon)
	return nil
}

func (r *Registry) Register(name string, addon Addon) {
	r.registry[name] = addon
}

func (r *Registry) Get(name string) Addon {
	return r.registry[name]
}
