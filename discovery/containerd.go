package discovery

import (
	"context"
	"errors"
	"log/slog"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
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

	ep, err := c.FindInContainerd(ctx, app)
	if err == nil {
		ch <- BackgroundLookup{Endpoint: ep}
		return
	}

	ch <- BackgroundLookup{Endpoint: ep}
}

func (c *Containerd) FindInContainerd(ctx context.Context, app string) (Endpoint, error) {
	ctx = namespaces.WithNamespace(ctx, c.Namespace)

	containers, err := c.Client.Containers(ctx)
	if err != nil {
		return nil, err
	}

	for _, container := range containers {
		labels, err := container.Labels(ctx)
		if err == nil {
			if labels["app"] == app {
				if host, ok := labels["http_host"]; ok {
					var ep Endpoint

					if dir, ok := labels["static_dir"]; ok {
						c.Log.Info("using local container endpoint for static_dir", "id", container.ID())
						ep = &LocalContainerEndpoint{
							Log: c.Log,
							HTTP: HTTPEndpoint{
								Host: "http://" + host,
							},
							Client:    c.Client,
							Namespace: c.Namespace,
							Dir:       dir,
							Id:        container.ID(),
						}
					} else {
						ep = &HTTPEndpoint{
							Host: "http://" + host,
						}
					}

					return ep, nil
				}
			}
		}
	}

	return nil, ErrNotFound
}
