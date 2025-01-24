package image

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"

	"github.com/containerd/errdefs"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/namespaces"
	"miren.dev/runtime/app"
)

type ImageInUser interface {
	ImageInUse(ctx context.Context, image string) (bool, error)
}

type ImagePruner struct {
	Log *slog.Logger
	App *app.AppAccess
	CC  *containerd.Client

	Namespace      string `asm:"namespace"`
	RollbackWindow int    `asm:"rollback_window,optional"`

	ImageInUser ImageInUser

	mu sync.Mutex
}

func (i *ImagePruner) VersionsToPrune(ctx context.Context, ac *app.AppConfig) ([]*app.AppVersion, error) {
	vers, err := i.App.ListVersions(ctx, ac)
	if err != nil {
		return nil, err
	}

	window := i.RollbackWindow + 1

	i.Log.Debug("considering app images for pruning", "app", ac.Name, "versions", len(vers), "window", window)

	if len(vers) <= window {
		return nil, nil
	}

	var oerr error

	vers = vers[window:]

	vers = slices.DeleteFunc(vers, func(v *app.AppVersion) bool {
		used, err := i.ImageInUser.ImageInUse(ctx, v.ImageName())
		if err != nil {
			oerr = err
			i.Log.Error("error checking if image is in use", "error", err)
			return false
		}

		return used
	})

	return vers, oerr
}

func (i *ImagePruner) PruneApp(ctx context.Context, name string) error {
	ac, err := i.App.LoadApp(ctx, name)
	if err != nil {
		return err
	}

	return i.Prune(ctx, ac)
}

func (i *ImagePruner) Prune(ctx context.Context, ac *app.AppConfig) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	ctx = namespaces.WithNamespace(ctx, i.Namespace)

	vers, err := i.VersionsToPrune(ctx, ac)
	if err != nil {
		return err
	}

	i.Log.Debug("pruning app images", "app", ac.Name, "versions", len(vers))

	if len(vers) == 0 {
		return nil
	}

	for _, v := range vers {
		err := i.App.DeleteVersion(ctx, v)
		if err != nil {
			return err
		}

		err = i.CC.ImageService().Delete(ctx, v.ImageName())
		if err != nil {
			if !errors.Is(err, errdefs.ErrNotFound) {
				return err
			}
		}

		i.Log.Info("pruned version", "app", ac.Name, "version", v.Version)
	}

	return err
}
