package server

import (
	"context"

	"miren.dev/runtime/pkg/rpc"
)

var _ UserQuery = &Server{}

func (s *Server) WhoAmI(ctx context.Context, req *UserQueryWhoAmI) error {
	var ui UserInfo

	ci := rpc.ConnectionInfo(ctx)
	if ci != nil {
		ui.SetSubject(ci.PeerSubject)
	}

	req.Results().SetInfo(&ui)

	return nil
}
