package server

import (
	"context"

	"miren.dev/runtime/api"
	"miren.dev/runtime/pkg/rpc"
)

var _ api.UserQuery = &Server{}

func (s *Server) WhoAmI(ctx context.Context, req *api.UserQueryWhoAmI) error {
	var ui api.UserInfo

	ci := rpc.ConnectionInfo(ctx)
	if ci != nil {
		ui.SetSubject(ci.PeerSubject)
	}

	req.Results().SetInfo(&ui)

	return nil
}
