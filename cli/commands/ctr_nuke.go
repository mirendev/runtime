package commands

import (
	"errors"
	"fmt"

	containerd "github.com/containerd/containerd/v2/client"
	"miren.dev/runtime/pkg/containerdx"
	"miren.dev/runtime/pkg/testutils"
)

func CtrNuke(c *Context, opts struct {
	Namespace  string `short:"n" long:"namespace" description:"namespace to nuke"`
	Containers bool   `short:"c" long:"containers" description:"nuke containers only"`
}) error {
	if opts.Namespace == "" {
		return errors.New("namespace is required")
	}

	cl, err := containerd.New(containerdx.DefaultSocketPath)
	if err != nil {
		return err
	}

	if opts.Containers {
		fmt.Printf("Nuking containers in namespace %s\n", opts.Namespace)

		return testutils.ClearContainers(cl, opts.Namespace)
	}

	fmt.Printf("Nuking namespace %s\n", opts.Namespace)

	testutils.NukeNamespace(cl, opts.Namespace)

	return nil
}
