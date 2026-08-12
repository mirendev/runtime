package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"miren.dev/mflags"
	"miren.dev/runtime/pkg/registration"
	"miren.dev/runtime/pkg/serverconfig"
)

// IdentityAnchorOptions contains options for moving a cluster's workload
// identity anchor.
type IdentityAnchorOptions struct {
	Anchor     string `position:"0" usage:"Where to anchor workload identity: cluster or cloud"`
	DataPath   string `short:"d" long:"data-path" description:"Server data path" default:"/var/lib/miren"`
	ConfigFile string `long:"config" description:"Path to the server configuration file (defaults to the usual discovery under --data-path)"`
}

// IdentityAnchor moves where this cluster's workload identity is anchored.
//
// The signing key does not move: it is generated here and stays here under
// either anchor. What changes is the iss claim in every token minted from now
// on, and who serves the discovery document verifiers fetch to check them.
//
// Because iss changes, this is a cutover for anything that federates against
// this cluster — an AWS IAM OIDC provider, a GCP workload identity pool — and
// each of those has to be repointed at the new issuer. Inside the cluster the
// move is seamless: the outgoing anchor is recorded so it keeps verifying until
// the tokens carrying it have expired.
func IdentityAnchor(ctx *Context, opts IdentityAnchorOptions) error {
	target := opts.Anchor
	if target != registration.AnchorCluster && target != registration.AnchorCloud {
		return fmt.Errorf("anchor must be %q or %q, got %q",
			registration.AnchorCluster, registration.AnchorCloud, target)
	}

	registrationDir := filepath.Join(opts.DataPath, "server")

	reg, err := registration.LoadRegistration(registrationDir)
	if err != nil {
		return fmt.Errorf("failed to load registration: %w", err)
	}
	if reg == nil || reg.Status != "approved" {
		return fmt.Errorf("this cluster is not registered with miren.cloud, so it has no cloud anchor to use; "+
			"run 'miren server register' first (looked in %s)", registrationDir)
	}

	current := reg.IdentityAnchor
	if current == "" {
		current = registration.AnchorCluster
	}
	if current == target {
		ctx.Info("Workload identity is already anchored at %s. Nothing to do.", target)
		return nil
	}

	if target == registration.AnchorCloud && reg.IdentityIssuerURL == "" {
		return fmt.Errorf("miren.cloud has not assigned this cluster an identity issuer, so there is nothing to anchor to; " +
			"this usually means cloud is running without IDENTITY_ISSUER_BASE_URL set")
	}

	// The server lets an explicit setting outrank the registration, so writing
	// the registration alone can be a no-op that still reports success. Resolve
	// the same configuration the server will and refuse rather than promise a
	// change that will not happen.
	if err := checkAnchorOverride(ctx, opts, target); err != nil {
		return err
	}

	reg.IdentityAnchor = target
	if err := registration.SaveRegistration(registrationDir, reg); err != nil {
		return fmt.Errorf("failed to save registration: %w", err)
	}

	ctx.Completed("Workload identity will be anchored at %s after the restart.", target)
	if target == registration.AnchorCloud {
		ctx.Info("  Issuer: %s", reg.IdentityIssuerURL)
		ctx.Info("  Signing keys stay on this cluster; miren.cloud serves discovery for them.")
	} else {
		ctx.Info("  This cluster will serve its own discovery again.")
	}
	ctx.Info("")

	// The server records the anchor it was minting under, so it recognizes the
	// move on the next boot and keeps accepting the outgoing issuer for as long
	// as tokens carrying it can still be valid. Nothing to record from here.
	ctx.Warn("Tokens minted after the restart carry the new issuer.")
	ctx.Info("  - Repoint any external trust configuration (an AWS IAM OIDC provider, a GCP")
	ctx.Info("    workload identity pool) at the new issuer, or it will reject this cluster's")
	ctx.Info("    tokens. The old issuer keeps verifying in-cluster until its tokens expire.")
	ctx.Info("  - Mounted token files are rewritten on restart. An app that read its token")
	ctx.Info("    once and cached it in memory needs a restart to pick up the new issuer.")

	restartMirenServiceIfActive(ctx)

	return nil
}

// checkAnchorOverride refuses the move when server configuration pins the
// anchor to something other than the target.
//
// Startup precedence is: explicit setting, then the registration, then the
// cluster anchor. Recording the registration under an explicit setting that
// disagrees changes nothing at boot, so the operator has to clear the setting
// for the move to mean anything.
//
// A config problem must not block the move on its own: the anchor lives in the
// registration and the server will resolve it again at startup either way, so
// an unreadable config is reported and stepped over rather than treated as
// fatal.
func checkAnchorOverride(ctx *Context, opts IdentityAnchorOptions, target string) error {
	// Mirrors what serverconfig.Load does for a nil flag set: wiring the struct
	// through mflags is what applies the MIREN_* environment variables. Load
	// only does that when it builds the flags itself, and we need our own so
	// config discovery honors --data-path.
	flags := serverconfig.NewCLIFlags()
	fs := mflags.NewFlagSet("serverconfig")
	if err := fs.FromStruct(flags); err != nil {
		ctx.Warn("Could not read server configuration to check for an anchor override: %v", err)
		return nil
	}
	flags.ServerConfigDataPath = &opts.DataPath

	cfg, err := serverconfig.Load(opts.ConfigFile, flags, ctx.Log)
	if err != nil {
		ctx.Warn("Could not read server configuration to check for an anchor override: %v", err)
		return nil
	}

	configured := cfg.WorkloadIdentity.GetAnchor()
	if configured == "" || configured == target {
		return nil
	}

	source := "your server configuration file"
	if os.Getenv("MIREN_WORKLOAD_IDENTITY_ANCHOR") != "" {
		source = "MIREN_WORKLOAD_IDENTITY_ANCHOR"
	}

	return fmt.Errorf(
		"%s pins the workload identity anchor to %q, which outranks the registration at startup; "+
			"clear it (or set it to %q) and run this again, otherwise the restart would leave the anchor unchanged",
		source, configured, target)
}
