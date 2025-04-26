package ocireg

import (
	"context"
	"io"
	"log/slog"
	"net/netip"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/entity/types"
	"miren.dev/runtime/pkg/rpc/stream"
)

func SetupReg(ctx context.Context, log *slog.Logger, eac *entityserver_v1alpha.EntityAccessClient) (netip.Addr, error) {
	var sb compute_v1alpha.Sandbox

	reg := compute_v1alpha.Container{
		Name:  "registry",
		Image: "docker.io/library/registry:2",
		Port: []compute_v1alpha.Port{
			{
				Port: 5000,
				Name: "http",
				Type: "http",
			},
		},
	}

	sb.Container = append(sb.Container, reg)

	var rpcE entityserver_v1alpha.Entity
	rpcE.SetAttrs(entity.Attrs(
		(&core_v1alpha.Metadata{
			Name:   "registry",
			Labels: types.LabelSet("visibility", "system"),
		}).Encode,
		entity.Ident, "sandbox/registry",
		sb.Encode,
	))

	pr, err := eac.Put(ctx, &rpcE)
	if err != nil {
		return netip.Addr{}, err
	}

	var (
		runningSB compute_v1alpha.Sandbox
	)

	log.Debug("watching sandbox", "sandbox", pr.Id())

	eac.WatchEntity(ctx, pr.Id(), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		var sb compute_v1alpha.Sandbox

		if op.HasEntity() {
			en := op.Entity().Entity()
			sb.Decode(en)

			if sb.Status == compute_v1alpha.RUNNING {
				runningSB = sb
				// TODO figure out a better way to signal that we're done with the watch.
				return io.EOF
			}
		}

		return nil
	}))

	return netip.ParseAddr(runningSB.Network[0].Address)
}
