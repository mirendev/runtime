package mysql

import (
	"context"
	"database/sql/driver"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-sql-driver/mysql"
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
	Endpoint     string
	Database     string
	User         string
	Password     string
	RootPassword string
}

func (a *Addon) Provision(ctx context.Context, name string, plan addons.Plan) (*addons.InstanceConfig, error) {
	img, err := a.Images.PullImage(ctx, "docker.io/library/mysql:9")
	if err != nil {
		return nil, err
	}

	a.Log.Debug("pulled image", "image", img.Name())

	id := addons.InstanceId(idgen.Gen("mysql"))

	pass := idgen.Gen("p")

	ec, err := network.AllocateOnBridge(a.Bridge, a.Subnet)
	if err != nil {
		return nil, err
	}

	rootPass := idgen.Gen("p")

	cfg := &run.ContainerConfig{
		Image:     "docker.io/library/mysql:9",
		Endpoint:  ec,
		LogEntity: string(id),
		Env: map[string]string{
			"MYSQL_USER":          name,
			"MYSQL_PASSWORD":      pass,
			"MYSQL_DATABASE":      name,
			"MYSQL_ROOT_PASSWORD": rootPass,
		},
		Ports: []run.PortConfig{
			{
				Port: 3306,
				Name: "mysql",
				Type: "mysql",
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

	url := fmt.Sprintf(
		"mysql://%s:%s@%s:3306/%s", name, pass, ec.Addresses[0].Addr().String(), name)

	res := &addons.InstanceConfig{
		Id:        id,
		Container: cid,

		Env: map[string]string{
			"DATABASE_URL": url,
		},
	}

	res.SetConfig(&instanceConfig{
		Endpoint:     ec.Addresses[0].Addr().String(),
		User:         name,
		Password:     pass,
		Database:     name,
		RootPassword: rootPass,
	})

	for {
		status, err := a.HealthCheck(ctx, res)
		if err != nil {
			a.Log.Error("error checking health", "error", err)
		} else if status != addons.StatusRunning {
			a.Log.Debug("mysql not ready", "status", status)
		} else {
			break
		}

		a.Log.Debug("waiting to connect to mysql", "error", err)
		time.Sleep(500 * time.Millisecond)
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

	mcfg := &mysql.Config{
		User:   ic.User,
		Passwd: ic.Password,
		Net:    "tcp",
		Addr:   ic.Endpoint + ":3306",
		DBName: ic.Database,
	}

	conn, err := mysql.NewConnector(mcfg)
	if err != nil {
		return "", err
	}

	c, err := conn.Connect(ctx)
	if err != nil {
		return "", err
	}

	if p, ok := c.(driver.Pinger); ok {
		err = p.Ping(ctx)
		if err != nil {
			return "", err
		}
	}

	return addons.StatusRunning, nil
}
