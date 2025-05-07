//go:build linux
// +build linux

package commands

import "miren.dev/runtime/components/coordinate"

func CoordinatorRun(ctx *Context, opts struct {
	Address       string   `short:"a" long:"address" description:"Address to listen on" default:":8443"`
	EtcdEndpoints []string `short:"e" long:"etcd" description:"Etcd endpoints" default:"http://etcd:2379"`
	EtcdPrefix    string   `short:"p" long:"etcd-prefix" description:"Etcd prefix" default:"/miren"`
}) error {
	co := coordinate.NewCoordinator(ctx.Log, coordinate.CoordinatorConfig{
		Address:       opts.Address,
		EtcdEndpoints: opts.EtcdEndpoints,
		Prefix:        opts.EtcdPrefix,
	})

	return co.Start(ctx)
}
