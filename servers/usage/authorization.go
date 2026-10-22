package usage

import (
	"context"
	"fmt"

	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/pkg/entity"
	"miren.dev/runtime/pkg/rpc"
)

type appOwner struct {
	name  string
	owner string
}

func (s *Server) loadAppOwners(ctx context.Context) (map[string]*appOwner, error) {
	res, err := s.EC.List(ctx, entity.Ref(entity.EntityKind, core_v1alpha.KindApp))
	if err != nil {
		return nil, fmt.Errorf("listing apps: %w", err)
	}
	owners := map[string]*appOwner{}
	for res.Next() {
		var app core_v1alpha.App
		if err := res.Read(&app); err != nil {
			return nil, err
		}
		var md core_v1alpha.Metadata
		md.Decode(res.Entity())
		if md.Name == "" {
			continue
		}
		owner := &appOwner{name: md.Name, owner: app.OwnedBy}
		// Names and entity IDs occupy disjoint namespaces (IDs contain '/').
		owners[app.ID.String()] = owner
		owners[md.Name] = owner
	}
	return owners, nil
}

func (d *directory) appByName(name string) *appOwner {
	return d.apps[name]
}

func authorizeOwner(ctx context.Context, owner *appOwner, action string, filter bool) (bool, error) {
	request := rpc.ResourceRequest{Resource: "cluster", Action: "admin"}
	if owner != nil {
		if !rpc.AllowApp(ctx, owner.name) {
			if filter {
				return false, nil
			}
			return false, rpc.AppAccessError(ctx, owner.name)
		}
		request = rpc.ResourceRequest{Resource: "app", Action: action, Instance: owner.name, Owner: owner.owner}
	} else if rpc.BoundApp(ctx) != "" {
		if filter {
			return false, nil
		}
		return false, rpc.ErrUnauthorized
	}
	if filter {
		return rpc.FilterResource(ctx, request)
	}
	err := rpc.AuthorizeResource(ctx, request)
	return err == nil, err
}

func preflightList(ctx context.Context) error {
	_, err := rpc.FilterResource(ctx, rpc.ResourceRequest{Resource: "app", Action: "list"})
	return err
}
