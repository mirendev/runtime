// Package serverinfo serves the ServerInfo RPC from a serverinfo.Source.
package serverinfo

import (
	"context"

	"miren.dev/runtime/api/server/server_v1alpha"
	"miren.dev/runtime/pkg/rpc/standard"
	"miren.dev/runtime/pkg/serverinfo"
)

// Server answers ServerInfo calls. Version is public so an unauthenticated
// upgrade process can check whether a restart took.
type Server struct {
	source *serverinfo.Source
}

var _ server_v1alpha.ServerInfo = (*Server)(nil)

func NewServer(source *serverinfo.Source) *Server {
	return &Server{source: source}
}

func (s *Server) Version(_ context.Context, state *server_v1alpha.ServerInfoVersion) error {
	info := s.source.Info()
	results := state.Results()
	results.SetVersion(info.Version)
	results.SetCommit(info.Commit)
	if !info.BuildDate.IsZero() {
		results.SetBuildDate(standard.ToTimestamp(info.BuildDate))
	}
	results.SetRuntimeInstanceId(info.InstanceID)
	results.SetStartedAt(standard.ToTimestamp(info.StartedAt))
	results.SetReady(info.Ready)
	results.SetInstallKind(string(info.InstallKind))
	return nil
}
