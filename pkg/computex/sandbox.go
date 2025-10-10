package computex

import (
	"context"
	"fmt"
	"io"

	"miren.dev/runtime/api/compute/compute_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/netutil"
	"miren.dev/runtime/pkg/rpc/stream"
)

func WaitForSandbox(ctx context.Context, id string, eac *entityserver_v1alpha.EntityAccessClient) (string, *entity.Entity, error) {
	var (
		runningSB compute_v1alpha.Sandbox
		sbEnt     *entity.Entity
	)

	eac.WatchEntity(ctx, string(id), stream.Callback(func(op *entityserver_v1alpha.EntityOp) error {
		var sb compute_v1alpha.Sandbox

		if op.HasEntity() {
			en := op.Entity().Entity()
			sb.Decode(en)

			if sb.Status == compute_v1alpha.RUNNING {
				runningSB = sb
				sbEnt = en
				// TODO figure out a better way to signal that we're done with the watch.
				return io.EOF
			}
		}

		return nil
	}))

	if runningSB.Status != compute_v1alpha.RUNNING {
		return "", nil, fmt.Errorf("sandbox %s not running: %s", id, runningSB.Status)
	}

	// Parse the address to extract just the IP from potential CIDR notation
	addr, err := netutil.ParseNetworkAddress(runningSB.Network[0].Address)
	if err != nil {
		return "", nil, err
	}
	return addr, sbEnt, nil
}
