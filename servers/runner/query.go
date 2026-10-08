package runner

import (
	"context"
	"fmt"
	"strings"

	"miren.dev/runtime/api/nodeadmin/nodeadmin_v1alpha"
	"miren.dev/runtime/api/runner/runner_v1alpha"
	"miren.dev/runtime/pkg/rpc"
)

func (s *RegistrationServer) Query(ctx context.Context, req *runner_v1alpha.RunnerRegistrationQuery) error {
	args := req.Args()
	res := req.Results()
	identity := rpc.IdentityFromContext(ctx)
	operator := identity != nil && (identity.Method == rpc.AuthMethodJWT || identity.Method == rpc.AuthMethodAnonymous ||
		identity.Method == rpc.AuthMethodCert && (identity.Subject == "miren-user" || identity.Subject == "miren-server" || identity.Subject == rpc.CoordinatorCertSubject))
	if !operator {
		res.SetError("host queries require an operator identity")
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
	if s.RPC == nil {
		res.SetError("no rpc state to reach the runner with")
		return nil
	}

	node, _, err := s.findNodeByQuery(ctx, target)
	if err != nil {
		res.SetError(err.Error())
		return nil
	}
	if node == nil {
		res.SetError(fmt.Sprintf("runner %q not found", target))
		return nil
	}
	if node.ApiAddress == "" {
		res.SetError(fmt.Sprintf("runner %q has no address to reach it on", target))
		return nil
	}

	cl, err := s.RPC.Connect(node.ApiAddress, rpc.ServiceNodeAdmin)
	if err != nil {
		res.SetError(fmt.Sprintf("connecting to runner %q: %v", target, err))
		return nil
	}
	defer cl.Close()
	result, err := nodeadmin_v1alpha.NewNodeAdminClient(cl).Query(ctx, args.Expression())
	if err != nil {
		res.SetError(fmt.Sprintf("querying runner %q: %v", target, err))
		return nil
	}
	if result.Error() != "" {
		res.SetError(result.Error())
		return nil
	}
	res.SetName(node.Name)
	res.SetData(result.Data())
	return nil
}
