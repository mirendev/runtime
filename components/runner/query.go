package runner

import (
	"context"
	"encoding/json"
	"runtime"
	"time"

	"miren.dev/runtime/api/nodeadmin/nodeadmin_v1alpha"
	"miren.dev/runtime/internal/runnerquery"
	query "miren.dev/runtime/pkg/portalquery"
)

func (s *nodeAdminServer) Query(ctx context.Context, req *nodeadmin_v1alpha.NodeAdminQuery) error {
	res := req.Results()
	if err := requireCoordinator(ctx); err != nil {
		s.log.Warn("rejected a host query", "error", err)
		res.SetError(err.Error())
		return nil
	}
	res.SetEngineRevision(query.Revision)

	s.queryMu.Lock()
	if s.activeQueries >= 10 {
		s.queryMu.Unlock()
		res.SetError("runner already has 10 queries in progress")
		return nil
	}
	s.activeQueries++
	s.queryMu.Unlock()
	defer func() {
		s.queryMu.Lock()
		s.activeQueries--
		s.queryMu.Unlock()
	}()

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

func (s *nodeAdminServer) QueryInfo(ctx context.Context, req *nodeadmin_v1alpha.NodeAdminQueryInfo) error {
	res := req.Results()
	if err := requireCoordinator(ctx); err != nil {
		res.SetError(err.Error())
		return nil
	}
	res.SetEngineRevision(query.Revision)
	reference, err := runnerquery.Reference()
	if err != nil {
		res.SetError(err.Error())
		return nil
	}
	res.SetReference(reference)
	return nil
}

func (s *nodeAdminServer) ValidateQuery(ctx context.Context, req *nodeadmin_v1alpha.NodeAdminValidateQuery) error {
	res := req.Results()
	if err := requireCoordinator(ctx); err != nil {
		res.SetError(err.Error())
		return nil
	}
	res.SetEngineRevision(query.Revision)
	request, err := s.queryEngine.ParseMonitorQuery(req.Args().Expression())
	if err == nil {
		err = s.queryEngine.Validate(request)
	}
	if err == nil {
		_, err = query.ResolveSyscallNames(request, runtime.GOARCH)
	}
	res.SetValid(err == nil)
	if err != nil {
		res.SetError(err.Error())
	}
	return nil
}
