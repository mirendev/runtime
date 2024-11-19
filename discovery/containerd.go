package discovery

import (
	"context"
	"errors"
	"log/slog"

	containerd "github.com/containerd/containerd/v2/client"
)

type Containerd struct {
	Log       *slog.Logger
	Namespace string `asm:"namespace"`
	Client    *containerd.Client
}

func (c *Containerd) Lookup(ctx context.Context, app string) (Endpoint, chan BackgroundLookup, error) {
	ch := make(chan BackgroundLookup, 1)

	go c.lookupBG(ctx, app, ch)

	return nil, ch, nil
}

var ErrNotFound = errors.New("no endpoints found")

func (c *Containerd) lookupBG(ctx context.Context, app string, ch chan BackgroundLookup) {
	defer close(ch)

	containers, err := c.Client.Containers(ctx)
	if err != nil {
		ch <- BackgroundLookup{Error: err}
		return
	}

	for _, c := range containers {
		labels, err := c.Labels(ctx)
		if err == nil {
			if labels["app"] == app {
				if host, ok := labels["http_host"]; ok {
					ep := &HTTPEndpoint{
						Host: "http://" + host,
					}
					ch <- BackgroundLookup{Endpoint: ep}
					return
				}
			}
		}
	}

	ch <- BackgroundLookup{Error: ErrNotFound}
}
