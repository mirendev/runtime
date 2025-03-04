package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/pkg/errors"
	"miren.dev/runtime/addons"
	"miren.dev/runtime/disk"
	"miren.dev/runtime/health"
	"miren.dev/runtime/image"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/idgen"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/pkg/units"
	"miren.dev/runtime/run"
)

type Addon struct {
	Log    *slog.Logger
	CR     *run.ContainerRunner
	Subnet *netdb.Subnet
	Bridge string `asm:"bridge-iface"`
	Health *health.ContainerMonitor
	Images *image.ImageImporter

	Disks *disk.Manager

	Tempdir string `asm:"tempdir"`

	localDisk bool
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
	Database string
	User     string
	Password string
}

func (a *Addon) Provision(ctx context.Context, name string, plan addons.Plan) (*addons.InstanceConfig, error) {
	id := addons.InstanceId(idgen.GenNS("postgres"))

	a.Log.Info("provisioning postgres disk", "id", id)

	var path string

	if a.localDisk {
		path = filepath.Join(a.Tempdir, string(id))
		err := os.MkdirAll(path, 0755)
		if err != nil {
			return nil, err
		}
	} else {
		// Provision a disk!
		dpath, err := a.Disks.CreateDisk(ctx, disk.CreateDiskParams{
			Name:     string(id),
			Capacity: units.GigaBytes(100),
		})

		if err != nil {
			return nil, errors.Wrap(err, "error creating disk")
		}

		path = dpath
	}

	img, err := a.Images.PullImage(ctx, "docker.io/library/postgres:17")
	if err != nil {
		return nil, err
	}

	a.Log.Debug("pulled image", "image", img.Name())

	pass := idgen.Gen("p")

	ec, err := network.AllocateOnBridge(a.Bridge, a.Subnet)
	if err != nil {
		return nil, err
	}

	cfg := &run.ContainerConfig{
		Image:     "docker.io/library/postgres:17",
		Endpoint:  ec,
		LogEntity: string(id),
		Env: map[string]string{
			"POSTGRES_USER":     name,
			"POSTGRES_PASSWORD": pass,
			"POSTGRES_DB":       name,
			"PGDATA":            "/var/lib/postgresql/data/pgdata",
		},
		Ports: []run.PortConfig{
			{
				Port: 5432,
				Name: "postgres",
				Type: "postgres",
			},
		},
		Mounts: []run.MountConfig{
			{
				Source: path,
				Target: "/var/lib/postgresql/data",
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
		"postgres://%s:%s@%s:5432/%s?sslmode=disable", name, pass, ec.Addresses[0].Addr().String(), name)

	res := &addons.InstanceConfig{
		Id:        id,
		Container: cid,

		Env: map[string]string{
			"DATABASE_URL": url,
		},
	}

	a.Log.Info("provisioned postgres", "id", id, "container", cid, "url", url)

	res.SetConfig(&instanceConfig{
		Endpoint: ec.Addresses[0].Addr().String(),
		User:     name,
		Password: pass,
		Database: name,
	})

	for {
		status, err := a.HealthCheck(ctx, res)
		if err != nil {
			a.Log.Error("error checking health", "error", err)
		} else if status != addons.StatusRunning {
			a.Log.Debug("postgres not ready", "status", status)
		} else {
			break
		}

		a.Log.Debug("waiting to connect to postgres", "error", err)
		time.Sleep(500 * time.Millisecond)
	}

	return res, nil
}

func (a *Addon) HealthCheck(ctx context.Context, cfg *addons.InstanceConfig) (addons.Status, error) {
	var ic instanceConfig

	err := cfg.Map(&ic)
	if err != nil {
		return "", err
	}

	conn, err := pgconn.Connect(ctx, cfg.Env["DATABASE_URL"])
	if err != nil {
		return "", err
	}

	defer conn.Close(ctx)

	err = conn.Ping(ctx)
	if err != nil {
		return "", err
	}

	return "running", nil
}

func (a *Addon) Deprovision(ctx context.Context, cfg *addons.InstanceConfig) error {
	return a.CR.StopContainer(ctx, cfg.Container)
}

type PostgresPortChecker struct{}

func (p *PostgresPortChecker) CheckPort(ctx context.Context, log *slog.Logger, addr string, port int) error {
	return nil
}
