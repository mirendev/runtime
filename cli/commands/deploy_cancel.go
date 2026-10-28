package commands

import (
	"errors"
	"fmt"

	"miren.dev/runtime/api/deployment/deployment_v1alpha"
)

func DeployCancel(ctx *Context, opts struct {
	ConfigCentric
	Deployment   string `position:"0" usage:"ID of the deployment to cancel"`
	DeploymentID string `short:"d" long:"deployment" description:"Deprecated: pass the deployment ID as an argument"`
}) error {
	if opts.Deployment != "" {
		if opts.DeploymentID != "" && opts.DeploymentID != opts.Deployment {
			return fmt.Errorf("deployment argument and --deployment disagree")
		}
		opts.DeploymentID = opts.Deployment
	} else if opts.DeploymentID != "" {
		fmt.Fprintln(ctx.Stderr, "Warning: --deployment is deprecated; use 'miren deployment cancel ID' instead.")
	}
	if opts.DeploymentID == "" {
		return fmt.Errorf("missing deployment argument: use 'miren deployment cancel ID'")
	}

	client, err := ctx.RPCClient("dev.miren.runtime/deployment")
	if err != nil {
		return err
	}
	defer client.Close()

	depClient := deployment_v1alpha.NewDeploymentClient(client)

	result, err := depClient.CancelDeployment(ctx, opts.DeploymentID, "")
	if err != nil {
		return err
	}

	if result.Error() != "" {
		return errors.New(result.Error())
	}

	ctx.Printf("Cancelled deployment %s\n", opts.DeploymentID)
	return nil
}
