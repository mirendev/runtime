package disk

import (
	"context"
	"log/slog"
	"time"

	"miren.dev/runtime/lsvd"
	"miren.dev/runtime/pkg/rpc"
)

//go:generate go run ../pkg/rpc/cmd/rpcgen -pkg disk -input rpc.yml -output rpc.gen.go

type ManagementServer struct {
	cancel func()
}

var _ DiskManagement = &ManagementServer{}

func (s *ManagementServer) Status(ctx context.Context, req *DiskManagementStatus) error {

	ms := lsvd.GetMetrics()

	var ds DiskStatus
	ds.SetBlocksRead(ms.BlocksRead)
	ds.SetBlocksWritten(ms.BlocksWritten)
	ds.SetIops(ms.IOPS)
	ds.SetSegmentsWritten(ms.SegmentsWritten)

	req.Results().SetStatus(&ds)

	return nil
}

func (s *ManagementServer) Unmount(ctx context.Context, req *DiskManagementUnmount) error {
	// Give the server a chance to respond before unmounting, since that exits the runner.
	go func() {
		time.Sleep(100 * time.Millisecond)
		s.cancel()
	}()
	return nil
}

func (r *Runner) Serve(ctx context.Context, cancel func(), log *slog.Logger, bindAddr string) error {
	srv := &ManagementServer{
		cancel: cancel,
	}

	ss, err := rpc.NewState(ctx,
		rpc.WithLogger(log),
		rpc.WithBindAddr(bindAddr),
	)
	if err != nil {
		return err
	}

	serv := ss.Server()

	serv.ExposeValue("disk", AdaptDiskManagement(srv))

	log.Info("disk management server started", "addr", bindAddr)

	<-ctx.Done()

	return nil
}
