package ondemand

import (
	"context"
	"log/slog"

	"miren.dev/runtime/app"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/health"
	"miren.dev/runtime/network"
	"miren.dev/runtime/pkg/netdb"
	"miren.dev/runtime/run"
)

type LaunchContainer struct {
	Log       *slog.Logger
	AppAccess *app.AppAccess
	CR        *run.ContainerRunner
	CD        *discovery.Containerd
	Subnet    *netdb.Subnet
	Health    *health.ContainerMonitor
}

func (l *LaunchContainer) Lookup(ctx context.Context, app string) (discovery.Endpoint, chan discovery.BackgroundLookup, error) {
	ac, err := l.AppAccess.LoadApp(ctx, app)
	if err != nil {
		return nil, nil, err
	}

	mrv, err := l.AppAccess.MostRecentVersion(ctx, ac)
	if err != nil {
		return nil, nil, err
	}

	ch := make(chan discovery.BackgroundLookup, 1)

	l.Log.Info("launching container", "app", ac.Name, "version", mrv.Version)

	go func() {
		ep, err := l.launch(ctx, ac, mrv)
		if err != nil {
			l.Log.Error("failed to launch container", "app", ac.Name, "version", mrv.Version, "error", err)
		} else {
			l.Log.Info("launched container", "app", ac.Name, "version", mrv.Version, "endpoint", ep)
		}

		ch <- discovery.BackgroundLookup{
			Endpoint: ep,
			Error:    err,
		}
	}()

	return nil, ch, nil
}

func (l *LaunchContainer) launch(
	ctx context.Context,
	ac *app.AppConfig,
	mrv *app.AppVersion,
) (discovery.Endpoint, error) {
	ec, err := network.AllocateOnBridge("mtest", l.Subnet)
	if err != nil {
		return nil, err
	}

	config := &run.ContainerConfig{
		App:      ac.Name,
		Image:    mrv.ImageName(),
		Endpoint: ec,
	}

	_, err = l.CR.RunContainer(ctx, config)
	if err != nil {
		return nil, err
	}

	err = l.Health.WaitForReady(ctx, config.Id)
	if err != nil {
		return nil, err
	}

	return l.CD.FindInContainerd(ctx, ac.Name)
}

var _ discovery.Lookup = &LaunchContainer{}
