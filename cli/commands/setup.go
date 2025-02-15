package commands

import (
	"bytes"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	docker "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"miren.dev/runtime/clientconfig"
)

func Setup(ctx *Context, opts struct {
	Fresh bool `long:"fresh" description:"Force a fresh setup"`
}) error {
	var (
		cc  *clientconfig.Config
		err error
	)

	if opts.Fresh {
		ctx.Printf("Forcing fresh setup\n")
		cc = &clientconfig.Config{}
	} else {
		cc, err = clientconfig.LoadConfig()
		if err != nil {
			ctx.Printf("Unable to load client config, creating one\n")
			cc = &clientconfig.Config{}
		}
	}

	if len(cc.Clusters) == 0 {
		cc.Clusters = make(map[string]*clientconfig.ClusterConfig)

		ctx.Printf("No clusters found, attempting to find a local one\n")
		cli, err := docker.NewClientWithOpts(docker.FromEnv)
		if err != nil {
			ctx.Printf("Unable to create docker client: %v\n", err)
			return fmt.Errorf("unable to find local cluster")
		}

		containers, err := cli.ContainerList(ctx, container.ListOptions{
			Filters: filters.NewArgs(filters.Arg("label", "computer.runtime.cluster")),
		})

		if err != nil {
			ctx.Printf("Unable to list containers: %v\n", err)
			return fmt.Errorf("unable to find local cluster")
		}

		if len(containers) == 0 {
			return fmt.Errorf("unable to find local cluster")
		}

		ctx.Printf("Found local cluster\n")

		resp, err := cli.ContainerExecCreate(ctx, containers[0].ID, container.ExecOptions{
			AttachStdout: true,
			Cmd:          []string{"cat", "/run/runtime/clientconfig.yaml"},
		})

		if err != nil {
			ctx.Printf("Unable to create exec: %v\n", err)
			return fmt.Errorf("unable to find local cluster")
		}

		hr, err := cli.ContainerExecAttach(ctx, resp.ID, container.ExecAttachOptions{})
		if err != nil {
			ctx.Printf("Unable to attach to exec: %v\n", err)
			return fmt.Errorf("unable to find local cluster")
		}

		defer hr.Close()

		var stdout bytes.Buffer

		_, err = stdcopy.StdCopy(&stdout, &stdout, hr.Reader)
		if err != nil {
			ctx.Printf("Unable to read exec output: %v\n", err)
			return fmt.Errorf("unable to find local cluster")
		}

		lc, err := clientconfig.DecodeConfig(stdout.Bytes())
		if err != nil {
			ctx.Printf("Unable to decode client config: %v\n", err)
			return fmt.Errorf("unable to find local cluster")
		}

		name := containers[0].Labels["computer.runtime.cluster"]

		cc.Clusters[name] = lc.Clusters["local"]

		if cc.ActiveCluster == "" {
			cc.ActiveCluster = name
		}

		ctx.Printf("Added local cluster %s\n", name)
	}

	ctx.Printf("Active cluster: %s\n", cc.ActiveCluster)

	return cc.SaveToHome()
}
