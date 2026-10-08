package runner

import (
	"context"
	"encoding/json"
	"time"

	"miren.dev/runtime/api/nodeadmin/nodeadmin_v1alpha"
)

func (s *nodeAdminServer) Query(ctx context.Context, req *nodeadmin_v1alpha.NodeAdminQuery) error {
	res := req.Results()
	if err := requireCoordinator(ctx); err != nil {
		s.log.Warn("rejected a host query", "error", err)
		res.SetError(err.Error())
		return nil
	}

	if !s.queryMu.TryLock() {
		res.SetError("runner already has a query in progress")
		return nil
	}
	defer s.queryMu.Unlock()

	request, err := s.queryEngine.ParseMonitorQuery(req.Args().Expression())
	if err != nil {
		res.SetError(err.Error())
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	snapshot, err := s.queryEngine.Query(ctx, request)
	if err != nil {
		res.SetError(err.Error())
		return nil
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		res.SetError(err.Error())
		return nil
	}
	res.SetData(data)
	return nil
}
