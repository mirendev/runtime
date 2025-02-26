package redis

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
	"miren.dev/runtime/addons"
	"miren.dev/runtime/health"
	"miren.dev/runtime/image"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/run"
)

type Addon struct {
	Log    *slog.Logger
	CR     *run.ContainerRunner
	Subnet *netdb.Subnet
	Bridge string `asm:"bridge-iface"`
	Health *health.ContainerMonitor
	Images *image.ImageImporter
}

var _ addons.Addon = &Addon{}

type Plan struct {
	size string
}

var _ addons.Plan = &Plan{}

func (p *Plan) Name() string {
	return p.size
}

func (a *Addon) Plans() []addons.Plan {
	return []addons.Plan{
		&Plan{size: "mini"},
	}
}

func (a *Addon) Default() addons.Plan {
	return &Plan{size: "mini"}
}

type instanceConfig struct {
	Endpoint string
	Password string
}

func (a *Addon) Provision(ctx context.Context, name string, plan addons.Plan) (*addons.InstanceConfig, error) {
	img, err := a.Images.PullImage(ctx, "docker.io/valkey/valkey:8")
	if err != nil {
		return nil, err
	}

	a.Log.Debug("pulled image", "image", img.Name())

	id := addons.InstanceId(idgen.Gen("redis"))

	pass := idgen.Gen("p")

	ec, err := network.AllocateOnBridge(a.Bridge, a.Subnet)
	if err != nil {
		return nil, err
	}

	cfg := &run.ContainerConfig{
		Image:     "docker.io/valkey/valkey:8",
		Endpoint:  ec,
		LogEntity: string(id),
		Command:   "redis-server --requirepass " + pass,
		Ports: []run.PortConfig{
			{
				Port: 6379,
				Name: "redis",
				Type: "redis",
			},
		},
		AlwaysRun: true,
	}

	cid, err := a.CR.RunContainer(ctx, cfg)
	if err != nil {
		return nil, err
	}

	err = a.Health.WaitForReady(ctx, cfg.Id)
	if err != nil {
		a.Log.Error("error waiting for container readiness", "container", cfg.Id, "error", err)
		a.CR.StopContainer(ctx, cfg.Id)
		return nil, err
	}

	url := fmt.Sprintf("redis://:%s@%s:6379", pass, ec.Addresses[0].Addr().String())

	res := &addons.InstanceConfig{
		Id:        id,
		Container: cid,

		Env: map[string]string{
			"REDIS_URL": url,
		},
	}

	res.SetConfig(&instanceConfig{
		Endpoint: ec.Addresses[0].Addr().String(),
		Password: pass,
	})

	for {
		status, err := a.HealthCheck(ctx, res)
		if err != nil {
			a.Log.Error("error checking health", "error", err)
		} else if status != addons.StatusRunning {
			a.Log.Debug("redis not ready", "status", status)
		} else {
			break
		}
	}

	return res, nil
}

func (a *Addon) Deprovision(ctx context.Context, cfg *addons.InstanceConfig) error {
	return a.CR.StopContainer(ctx, cfg.Container)
}

func (a *Addon) HealthCheck(ctx context.Context, cfg *addons.InstanceConfig) (addons.Status, error) {
	var ic instanceConfig

	err := cfg.Map(&ic)
	if err != nil {
		return "", err
	}

	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:6379", ic.Endpoint),
		Password: ic.Password,
	})

	conn := client.Conn()

	defer conn.Close()

	a.Log.Debug("checking redis health")

	err = conn.Hello(ctx, 3, "", ic.Password, "runtime").Err()
	if err != nil {
		a.Log.Error("error checking redis health with HELLO", "error", err)
		return "", err
	}

	err = conn.Auth(ctx, ic.Password).Err()
	if err != nil {
		a.Log.Error("error checking redis health with AUTH", "error", err)
		return "", err
	}

	return addons.StatusRunning, nil
}
