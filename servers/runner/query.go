package runner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"miren.dev/runtime/api/nodeadmin/nodeadmin_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

func (s *RegistrationServer) Query(ctx context.Context, req *runner_v1alpha.RunnerRegistrationQuery) error {
	args := req.Args()
	res := req.Results()
	if err := requireQueryOperator(ctx); err != nil {
		res.SetError(err.Error())
		return nil
	}
	target := strings.TrimSpace(args.Runner())
	if target == "" {
		res.SetError("runner name or ID is required")
		return nil
	}
	if strings.TrimSpace(args.Expression()) == "" {
		res.SetError("query expression is required")
		return nil
	}
	name, cl, err := s.connectQueryRunner(ctx, target)
	if err != nil {
		res.SetError(err.Error())
		return nil
	}
	defer cl.Close()
	result, err := nodeadmin_v1alpha.NewNodeAdminClient(cl).Query(ctx, args.Expression())
	if err != nil {
		res.SetError(fmt.Sprintf("querying runner %q: %v", target, err))
		return nil
	}
	if result.HasEngineRevision() {
		res.SetEngineRevision(result.EngineRevision())
	}
	if result.Error() != "" {
		res.SetError(result.Error())
		return nil
	}
	res.SetName(name)
	res.SetData(result.Data())
	return nil
}

func (s *RegistrationServer) QueryInfo(ctx context.Context, req *runner_v1alpha.RunnerRegistrationQueryInfo) error {
	res := req.Results()
	if err := requireQueryOperator(ctx); err != nil {
		res.SetError(err.Error())
		return nil
	}
	name, cl, err := s.connectQueryRunner(ctx, req.Args().Runner())
	if err != nil {
		res.SetError(err.Error())
		return nil
	}
	defer cl.Close()
	result, err := nodeadmin_v1alpha.NewNodeAdminClient(cl).QueryInfo(ctx)
	if err != nil {
		res.SetError(fmt.Sprintf("getting query syntax from runner %q: %v", name, err))
		return nil
	}
	res.SetName(name)
	res.SetEngineRevision(result.EngineRevision())
	if result.Error() != "" {
		res.SetError(result.Error())
		return nil
	}
	res.SetReference(result.Reference())
	return nil
}

func (s *RegistrationServer) ValidateQuery(ctx context.Context, req *runner_v1alpha.RunnerRegistrationValidateQuery) error {
	res := req.Results()
	if err := requireQueryOperator(ctx); err != nil {
		res.SetError(err.Error())
		return nil
	}
	name, cl, err := s.connectQueryRunner(ctx, req.Args().Runner())
	if err != nil {
		res.SetError(err.Error())
		return nil
	}
	defer cl.Close()
	result, err := nodeadmin_v1alpha.NewNodeAdminClient(cl).ValidateQuery(ctx, req.Args().Expression())
	if err != nil {
		res.SetError(fmt.Sprintf("validating query on runner %q: %v", name, err))
		return nil
	}
	res.SetName(name)
	res.SetEngineRevision(result.EngineRevision())
	res.SetValid(result.Valid())
	if result.Error() != "" {
		res.SetError(result.Error())
	}
	return nil
}

func requireQueryOperator(ctx context.Context) error {
	identity := rpc.IdentityFromContext(ctx)
	operator := identity != nil && (identity.Method == rpc.AuthMethodJWT || identity.Method == rpc.AuthMethodAnonymous ||
		identity.Method == rpc.AuthMethodCert && (identity.Subject == "miren-user" || identity.Subject == "miren-server" || identity.Subject == rpc.CoordinatorCertSubject))
	if !operator {
		return errors.New("host queries require an operator identity")
	}
	return nil
}

func (s *RegistrationServer) connectQueryRunner(ctx context.Context, target string) (string, *rpc.NetworkClient, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return "", nil, errors.New("runner name or ID is required")
	}
	if s.RPC == nil {
		return "", nil, errors.New("no rpc state to reach the runner with")
	}
	node, _, err := s.findNodeByQuery(ctx, target)
	if err != nil {
		return "", nil, err
	}
	if node == nil {
		return "", nil, fmt.Errorf("runner %q not found", target)
	}
	if node.ApiAddress == "" {
		return "", nil, fmt.Errorf("runner %q has no address to reach it on", target)
	}
	cl, err := s.RPC.Connect(node.ApiAddress, rpc.ServiceNodeAdmin)
	if err != nil {
		return "", nil, fmt.Errorf("connecting to runner %q: %v", target, err)
	}
	return node.Name, cl, nil
}
