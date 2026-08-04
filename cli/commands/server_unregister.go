package commands

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/ui"
)

// UnregisterOptions contains options for detaching a cluster from miren.cloud
type UnregisterOptions struct {
	Force     bool   `short:"f" long:"force" description:"Skip the confirmation prompt"`
	LocalOnly bool   `long:"local-only" description:"Clear local registration without telling miren.cloud"`
	OutputDir string `short:"o" long:"output" description:"Directory holding the registration" default:"/var/lib/miren/server"`
}

// Unregister is the inverse of `miren server register`: it detaches the cluster
// from the miren.cloud organization it registered with and clears the local
// registration state, so the cluster can be retired or re-registered elsewhere.
//
// The cloud half runs first. The service account key is the only credential
// that can authorize the cloud-side removal, and it lives in the files the
// local half deletes — clearing those first would strand an unreachable
// cluster row in the organization with no way to finish from this machine.
func Unregister(ctx *Context, opts UnregisterOptions) error {
	reg, err := registration.LoadRegistration(opts.OutputDir)
	if err != nil {
		return fmt.Errorf("failed to load registration: %w", err)
	}
	if reg == nil {
		ctx.Info("No cluster registration found in %s; nothing to unregister.", opts.OutputDir)
		return nil
	}

	if !opts.Force {
		confirmed, err := confirmUnregister(ctx, reg, opts.LocalOnly)
		if err != nil {
			return err
		}
		if !confirmed {
			ctx.Info("Unregister cancelled")
			return nil
		}
	}

	if opts.LocalOnly {
		ctx.Warn("Skipping miren.cloud; the cluster entry in the organization is left in place.")
	} else if err := unregisterFromCloud(ctx, reg); err != nil {
		return err
	}

	if err := registration.ClearRegistration(opts.OutputDir); err != nil {
		if !opts.LocalOnly {
			// The cloud side is already done at this point, so the credentials
			// left on disk are dead. Name the files rather than leaving the
			// operator to guess which ones are safe to remove.
			ctx.Warn("The cluster was removed from miren.cloud, but its local registration could not be cleared.")
			ctx.Warn("Remove these by hand, then restart the server: %s",
				strings.Join(registrationFilePaths(opts.OutputDir), ", "))
		}
		return err
	}

	ctx.Completed("Cluster unregistered.")
	restartMirenServiceIfActive(ctx)

	return nil
}

// unregisterFromCloud removes the cluster from its organization, authenticating
// as the cluster's own service account.
func unregisterFromCloud(ctx *Context, reg *registration.StoredRegistration) error {
	if reg.Status != "approved" {
		// A pending registration was never approved, so no cluster exists in
		// the organization to remove. Clearing the local files is the whole job.
		ctx.Info("Registration was never approved; clearing local state only.")
		return nil
	}

	keyPair, err := cloudauth.LoadKeyPairFromPEM(reg.PrivateKey)
	if err != nil {
		return fmt.Errorf("failed to load service account key: %w", err)
	}

	client, err := cloudauth.NewAuthClient(reg.CloudURL, keyPair)
	if err != nil {
		return fmt.Errorf("failed to create cloud client: %w", err)
	}

	ctx.Info("Removing cluster '%s' from %s...", reg.ClusterName, reg.CloudURL)

	if err := client.UnregisterSelf(ctx); err != nil {
		if errors.Is(err, cloudauth.ErrClusterNotRegistered) {
			ctx.Info("Cluster was already removed from miren.cloud.")
			return nil
		}
		return fmt.Errorf("failed to unregister from %s: %w (use --local-only to clear local state anyway)",
			reg.CloudURL, err)
	}

	ctx.Completed("Removed from miren.cloud.")

	return nil
}

// confirmUnregister spells out what detaching costs before asking. Workloads
// keep running throughout — what goes away is everything the cluster gets from
// being part of an organization.
func confirmUnregister(ctx *Context, reg *registration.StoredRegistration, localOnly bool) (bool, error) {
	ctx.Printf("Unregistering cluster '%s' from %s\n", reg.ClusterName, reg.CloudURL)
	ctx.Printf("\n")
	ctx.Printf("Apps and sandboxes keep running, but the cluster loses:\n")
	if reg.DNSHostname != "" {
		ctx.Printf("  - its cloud DNS records, including %s\n", reg.DNSHostname)
	} else {
		ctx.Printf("  - its cloud DNS records\n")
	}
	ctx.Printf("  - Miren Anywhere routing, so traffic must reach the cluster directly\n")
	ctx.Printf("  - cloud RBAC rules, leaving certificate auth as the only way in\n")
	ctx.Printf("\n")
	ctx.Printf("If the miren service is running under systemd it restarts at the end,\n")
	ctx.Printf("so the server stops using the registration.\n")
	if localOnly {
		ctx.Printf("\n")
		ctx.Printf("--local-only: miren.cloud is not contacted, so the cluster entry\n")
		ctx.Printf("stays in the organization and has to be removed there.\n")
	}
	ctx.Printf("\n")

	confirmed, err := ui.Confirm(
		ui.WithMessage(fmt.Sprintf("Unregister '%s'?", reg.ClusterName)),
		ui.WithDefault(false),
	)
	if err != nil {
		return false, fmt.Errorf("confirmation failed: %w", err)
	}

	return confirmed, nil
}

func registrationFilePaths(dir string) []string {
	paths := make([]string, 0, len(registration.RegistrationFiles))
	for _, name := range registration.RegistrationFiles {
		paths = append(paths, filepath.Join(dir, name))
	}
	return paths
}
