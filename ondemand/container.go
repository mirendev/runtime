package ondemand

import (
	"context"
	"log/slog"
	"net/netip"

	"miren.dev/runtime/app"
	"miren.dev/runtime/discovery"
	"miren.dev/runtime/run"
)

type LaunchContainer struct {
	Log       *slog.Logger
	AppAccess *app.AppAccess
	CR        *run.ContainerRunner
	CD        *discovery.Containerd
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
	// TODO implement a network address pool to allocate IPs from
	sa, err := netip.ParsePrefix("172.16.8.1/24")
	if err != nil {
		return nil, err
	}

	ca, err := netip.ParsePrefix("172.16.8.2/24")
	if err != nil {
		return nil, err
	}

	config := &run.ContainerConfig{
		App:   ac.Name,
		Image: mrv.ImageName(),
		IPs:   []netip.Prefix{ca},
		Subnet: &run.Subnet{
			Id:     "sub",
			IP:     []netip.Prefix{sa},
			OSName: "mtest",
		},
	}

	_, err = l.CR.RunContainer(ctx, config)
	if err != nil {
		return nil, err
	}

	// TODO wait for the container to be ready

	return l.CD.FindInContainerd(ctx, ac.Name)
}

var _ discovery.Lookup = &LaunchContainer{}
