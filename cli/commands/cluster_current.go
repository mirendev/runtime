package commands

import "fmt"

func ClusterCurrent(ctx *Context, opts struct {
	ConfigCentric
}) error {
	cc, name, err := opts.LoadCluster()
	if err != nil {
		return fmt.Errorf("no cluster configured: %w", err)
	}

	if cc == nil || name == "" {
		ctx.Printf("No cluster configured\n")
		return nil
	}

	ctx.Printf("%s\n", name)
	return nil
}
