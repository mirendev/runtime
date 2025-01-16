package server

import (
	"context"

	"miren.dev/runtime/build"
	"miren.dev/runtime/pkg/rpc"
)

type Server struct {
	Port  int `asm:"server_port"`
	Build *build.RPCBuilder
}

func (s *Server) Run(ctx context.Context) error {
	ss, err := rpc.NewState(ctx, rpc.WithSkipVerify)
	if err != nil {
		return err
	}

	serv := ss.Server()

	serv.ExposeValue("build", build.AdaptBuilder(s.Build))

	<-ctx.Done()

	return ctx.Err()
}
