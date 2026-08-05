package commands

import (
	"fmt"
	"sort"
	"strings"

	"miren.dev/runtime/api/app/app_v1alpha"
	"miren.dev/runtime/pkg/workloadroles"
)

func AppSetWorkloadRole(ctx *Context, opts struct {
	AppCentric
	Args []string `rest:"true"`
}) error {
	if len(opts.Args) != 1 {
		return fmt.Errorf("usage: app set-workload-role -a <app> <role>\nknown roles: %s", knownRoleList())
	}
	role := opts.Args[0]

	if _, ok := workloadroles.Lookup(role); !ok {
		return fmt.Errorf("unknown role %q\nknown roles: %s", role, knownRoleList())
	}

	crudcl, err := ctx.RPCClient("dev.miren.runtime/app")
	if err != nil {
		return err
	}

	crud := app_v1alpha.NewCrudClient(crudcl)
	if _, err := crud.SetWorkloadRole(ctx, opts.App, role); err != nil {
		return err
	}

	ctx.Printf("Set workload role of app %s to %s\n", opts.App, role)
	if !workloadroles.IsAppScoped(role) {
		ctx.Printf("Note: %s is a cluster-scoped role — its tokens are not confined to this app.\n", role)
	}
	return nil
}

func knownRoleList() string {
	names := make([]string, 0, len(workloadroles.Roles))
	for name := range workloadroles.Roles {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
