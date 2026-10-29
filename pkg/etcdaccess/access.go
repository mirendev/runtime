// Package etcdaccess restricts runner transport identities to networking state.
package etcdaccess

import (
	"context"
	"errors"
	"path"

	"go.etcd.io/etcd/api/v3/v3rpc/rpctypes"
	clientv3 "go.etcd.io/etcd/client/v3"
)

const Coordinator = "etcd-client-coordinator"
const runnerRole = "miren-runner-network"

// Ensure enables certificate-CN authentication before credential-bearing state
// is imported. It is restartable across every provisioning step.
func Ensure(ctx context.Context, client *clientv3.Client, prefix string) error {
	for _, user := range []string{"root", Coordinator} {
		if err := addUser(ctx, client, user); err != nil {
			return err
		}
		if _, err := client.UserGrantRole(ctx, user, "root"); err != nil {
			return err
		}
	}
	if _, err := client.RoleAdd(ctx, runnerRole); err != nil && !errors.Is(err, rpctypes.ErrRoleAlreadyExist) {
		return err
	}
	// Flannel builds its registry keys with path.Join, including for the raw
	// prefix persisted by older runners. Grant the actual etcd key range.
	key := path.Join(prefix, "sub", "flannel") + "/"
	if _, err := client.RoleGrantPermission(ctx, runnerRole, key, clientv3.GetPrefixRangeEnd(key), clientv3.PermissionType(clientv3.PermReadWrite)); err != nil {
		return err
	}
	_, err := client.AuthEnable(ctx)
	return err
}

func Runner(ctx context.Context, client *clientv3.Client, name string) error {
	if err := addUser(ctx, client, name); err != nil {
		return err
	}
	_, err := client.UserGrantRole(ctx, name, runnerRole)
	return err
}

func addUser(ctx context.Context, client *clientv3.Client, name string) error {
	_, err := client.UserAddWithOptions(ctx, name, "", &clientv3.UserAddOptions{NoPassword: true})
	if errors.Is(err, rpctypes.ErrUserAlreadyExist) {
		return nil
	}
	return err
}
