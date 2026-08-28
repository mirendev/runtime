package commands

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"miren.dev/runtime/pkg/registration"
)

// RegisterOptions contains options for cluster registration
type RegisterOptions struct {
	ClusterName string            `short:"n" long:"name" description:"Cluster name" required:"true"`
	CloudURL    string            `short:"u" long:"url" description:"Cloud URL" default:"https://miren.cloud"`
	Tags        map[string]string `short:"t" long:"tag" description:"Tags for the cluster (key:value)"`
	OutputDir   string            `short:"o" long:"output" description:"Output directory for registration" default:"/var/lib/miren/server"`

	// EnrollToken, when set, registers unattended: no browser approval, no
	// polling. It is minted by an org admin in miren.cloud and normally arrives
	// via the cloud-init bootstrap payload rather than being typed by hand.
	EnrollToken string `long:"enroll-token" description:"Unattended enroll token from miren.cloud"`
}

// RegisterStandalone is the CLI entrypoint for `miren server register`. It runs
// Register and then bounces the local miren.service if one is active, so the
// user doesn't have to restart manually. The install paths call Register
// directly because they own the service lifecycle themselves.
func RegisterStandalone(ctx *Context, opts RegisterOptions) error {
	if err := Register(ctx, opts); err != nil {
		return err
	}
	restartMirenServiceIfActive(ctx)
	return nil
}

// Register handles cluster registration with miren.cloud
func Register(ctx *Context, opts RegisterOptions) error {
	clean := map[string]string{}

	// Validate tags
	for key, value := range opts.Tags {
		if key == "" {
			return fmt.Errorf("invalid tag: key cannot be empty")
		}
		if strings.Contains(key, "=") {
			return fmt.Errorf("invalid tag key '%s': cannot contain '='", key)
		}
		if value == "" {
			return fmt.Errorf("invalid tag '%s': value cannot be empty", key)
		}

		clean[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}

	opts.Tags = clean

	// An enroll token takes the unattended path: cloud creates the cluster in
	// the initiate response, so there is no browser approval and nothing to
	// poll. It is a fully separate flow from the interactive one below.
	if opts.EnrollToken != "" {
		return registerWithEnrollToken(ctx, opts)
	}

	// Check if already registered
	existing, err := registration.LoadRegistration(opts.OutputDir)
	if err != nil {
		return fmt.Errorf("failed to check existing registration: %w", err)
	}
	if existing != nil {
		// Check if it's a pending registration that hasn't expired, but make sure we've still got
		// at least a minute left.
		if existing.Status == "pending" && existing.ExpiresAt.After(time.Now().Add(-5*time.Minute)) {
			ctx.Warn("Found pending registration for cluster '%s' (ID: %s)", existing.ClusterName, existing.RegistrationID)
			ctx.Info("Expires at: %s", existing.ExpiresAt.Format(time.RFC3339))
			ctx.Info("Resuming registration process...")

			// Create client and poll
			config := registration.Config{
				ClusterName: existing.ClusterName,
				Tags:        existing.Tags,
			}
			client := registration.NewClient(existing.CloudURL, config)

			// Poll for approval
			ctx.Info("Waiting for approval")
			pollCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()

			status, err := client.PollForApproval(pollCtx, existing.PollURL, 5*time.Second, func() {
				fmt.Print(".")
			})
			if err != nil {
				ctx.Warn(" Failed!")
				return fmt.Errorf("registration failed: %w", err)
			}
			ctx.Info(" Approved!")

			// Update and save the approved registration
			existing.Status = "approved"
			existing.ClusterID = status.ClusterID
			existing.OrganizationID = status.OrganizationID
			existing.ServiceAccountID = status.ServiceAccountID
			existing.DNSHostname = status.DNSHostname
			existing.IdentityIssuerURL = status.IdentityIssuerURL
			existing.IdentityAnchor = anchorForRegistration(status.IdentityIssuerURL)
			existing.RegisteredAt = time.Now()

			if err := registration.SaveRegistration(opts.OutputDir, existing); err != nil {
				return fmt.Errorf("failed to save registration: %w", err)
			}

			ctx.Completed("Registration successful!")
			ctx.Info("Cluster ID: %s", status.ClusterID)
			ctx.Info("Organization ID: %s", status.OrganizationID)
			ctx.Info("Service Account ID: %s", status.ServiceAccountID)
			if status.DNSHostname != "" {
				ctx.Info("DNS Hostname: %s", status.DNSHostname)
			}
			reportIdentityAnchor(ctx, status.IdentityIssuerURL)
			ctx.Info("Configuration saved to: %s", opts.OutputDir)

			return nil
		} else if existing.Status == "approved" {
			return fmt.Errorf("cluster already registered as %s (ID: %s); run 'miren server unregister' to detach it first",
				existing.ClusterName, existing.ClusterID)
		}
		// If pending but expired, we'll start fresh
	}

	ctx.Info("Registering cluster '%s' with %s...", opts.ClusterName, opts.CloudURL)

	// Generate key pair for service account
	privateKey, publicKey, err := registration.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("failed to generate key pair: %w", err)
	}

	// Save the private key immediately to verify write access before making the request
	// This also ensures we don't lose the key if something goes wrong
	initial := &registration.StoredRegistration{
		ClusterName: opts.ClusterName,
		PrivateKey:  privateKey,
		CloudURL:    opts.CloudURL,
		Tags:        opts.Tags,
		Status:      "initializing",
	}

	if err := registration.SaveRegistration(opts.OutputDir, initial); err != nil {
		return fmt.Errorf("cannot save registration to %s: %w", opts.OutputDir, err)
	}

	// Create registration config
	config := registration.Config{
		ClusterName: opts.ClusterName,
		Tags:        opts.Tags,
		PublicKey:   publicKey,
	}

	// Create registration client
	client := registration.NewClient(opts.CloudURL, config)

	// Start registration
	bgCtx := context.Background()
	result, err := client.StartRegistration(bgCtx)
	if err != nil {
		return fmt.Errorf("failed to start registration: %w", err)
	}

	ctx.Completed("Registration initiated!")
	ctx.Info("Please approve the registration at: %s", result.AuthURL)
	ctx.Info("Registration ID: %s", result.RegistrationID)
	ctx.Info("Expires at: %s", result.ExpiresAt.Format(time.RFC3339))

	// Update with pending registration details
	pending := &registration.StoredRegistration{
		ClusterName:    opts.ClusterName,
		PrivateKey:     privateKey,
		CloudURL:       opts.CloudURL,
		Tags:           opts.Tags,
		Status:         "pending",
		RegistrationID: result.RegistrationID,
		PollURL:        result.PollURL,
		ExpiresAt:      result.ExpiresAt,
	}

	if err := registration.SaveRegistration(opts.OutputDir, pending); err != nil {
		return fmt.Errorf("failed to save pending registration: %w", err)
	}

	// Poll for approval with timeout
	ctx.Info("Waiting for approval")
	pollCtx, cancel := context.WithTimeout(bgCtx, 30*time.Minute)
	defer cancel()

	status, err := client.PollForApproval(pollCtx, result.PollURL, 5*time.Second, func() {
		fmt.Print(".")
	})
	if err != nil {
		ctx.Warn(" Failed!")
		return fmt.Errorf("registration failed: %w", err)
	}
	ctx.Info("Approved!")

	// Update registration data with approved status
	stored := &registration.StoredRegistration{
		ClusterID:        status.ClusterID,
		ClusterName:      opts.ClusterName,
		OrganizationID:   status.OrganizationID,
		ServiceAccountID: status.ServiceAccountID,
		DNSHostname:      status.DNSHostname,
		// Recorded even when the cluster is not anchored at cloud, so
		// switching later with --identity-anchor=cloud needs no re-register.
		IdentityIssuerURL: status.IdentityIssuerURL,
		IdentityAnchor:    anchorForRegistration(status.IdentityIssuerURL),
		PrivateKey:        privateKey,
		CloudURL:          opts.CloudURL,
		RegisteredAt:      time.Now(),
		Tags:              opts.Tags,
		Status:            "approved",
	}

	if err := registration.SaveRegistration(opts.OutputDir, stored); err != nil {
		return fmt.Errorf("failed to save registration: %w", err)
	}

	ctx.Completed("Registration successful!")
	ctx.Info("Cluster ID: %s", status.ClusterID)
	ctx.Info("Organization ID: %s", status.OrganizationID)
	ctx.Info("Service Account ID: %s", status.ServiceAccountID)
	if status.DNSHostname != "" {
		ctx.Info("DNS Hostname: %s", status.DNSHostname)
	}
	reportIdentityAnchor(ctx, status.IdentityIssuerURL)
	ctx.Info("Configuration saved to: %s", opts.OutputDir)

	return nil
}

// registerWithEnrollToken handles the unattended registration path. A valid
// enroll token means an org admin already approved this cluster when they
// minted the token, so cloud creates the cluster in the initiate response and
// there is nothing to approve in a browser or poll for.
//
// A token that cloud rejects is terminal here: this never falls back to the
// interactive flow. A machine booted from a cloud-init payload has no human at
// a browser, so falling back would strand it waiting for an approval nobody
// knows to give.
func registerWithEnrollToken(ctx *Context, opts RegisterOptions) error {
	existing, err := registration.LoadRegistration(opts.OutputDir)
	if err != nil {
		return fmt.Errorf("failed to check existing registration: %w", err)
	}
	if existing != nil && existing.Status == "approved" {
		return fmt.Errorf("cluster already registered as %s (ID: %s); run 'miren server unregister' to detach it first",
			existing.ClusterName, existing.ClusterID)
	}

	// Reuse a keypair left behind by an interrupted attempt. Presenting the same
	// public key is what lets cloud replay the original registration instead of
	// refusing a token it already spent, so a retry after a lost response lands
	// on the same cluster rather than an error.
	var privateKey, publicKey string
	if existing != nil && existing.PrivateKey != "" {
		privateKey = existing.PrivateKey
		publicKey, err = registration.PublicKeyFromPrivateKeyPEM(privateKey)
		if err != nil {
			return fmt.Errorf("failed to derive public key from saved private key: %w", err)
		}
		ctx.Info("Reusing the keypair from a previous enrollment attempt")
	} else {
		privateKey, publicKey, err = registration.GenerateKeyPair()
		if err != nil {
			return fmt.Errorf("failed to generate key pair: %w", err)
		}
	}

	// Save the key before the request, so a crash mid-flight still leaves us
	// able to retry with the same key.
	initial := &registration.StoredRegistration{
		ClusterName: opts.ClusterName,
		PrivateKey:  privateKey,
		CloudURL:    opts.CloudURL,
		Tags:        opts.Tags,
		Status:      "initializing",
	}
	if err := registration.SaveRegistration(opts.OutputDir, initial); err != nil {
		return fmt.Errorf("cannot save registration to %s: %w", opts.OutputDir, err)
	}

	ctx.Info("Registering cluster '%s' with %s using an enroll token...", opts.ClusterName, opts.CloudURL)

	config := registration.Config{
		ClusterName: opts.ClusterName,
		Tags:        opts.Tags,
		PublicKey:   publicKey,
		EnrollToken: opts.EnrollToken,
	}
	client := registration.NewClient(opts.CloudURL, config)

	result, err := client.StartRegistration(context.Background())
	if err != nil {
		return fmt.Errorf("enrollment failed: %w", err)
	}

	if result.Status != registration.StatusRegistered {
		// Cloud answered with the interactive shape despite the token, which
		// means the token was ignored — an older server, or enroll tokens
		// disabled. Treat it as terminal for the reason above.
		return fmt.Errorf("cloud did not honor the enroll token (status %q); unattended enrollment is not available at %s",
			result.Status, opts.CloudURL)
	}

	// The response carries the authoritative tag set: grant defaults an admin
	// stamped on the token, merged over what this node asserted. Prefer it over
	// the node's own tags when present.
	tags := opts.Tags
	if len(result.Tags) > 0 {
		tags = result.Tags
	}

	stored := &registration.StoredRegistration{
		ClusterID:         result.ClusterID,
		ClusterName:       opts.ClusterName,
		OrganizationID:    result.OrganizationID,
		ServiceAccountID:  result.ServiceAccountID,
		DNSHostname:       result.DNSHostname,
		IdentityIssuerURL: result.IdentityIssuerURL,
		IdentityAnchor:    anchorForRegistration(result.IdentityIssuerURL),
		PrivateKey:        privateKey,
		CloudURL:          opts.CloudURL,
		RegisteredAt:      time.Now(),
		Tags:              tags,
		Status:            "approved",
	}
	if err := registration.SaveRegistration(opts.OutputDir, stored); err != nil {
		return fmt.Errorf("failed to save registration: %w", err)
	}

	ctx.Completed("Registration successful!")
	ctx.Info("Cluster ID: %s", result.ClusterID)
	ctx.Info("Organization ID: %s", result.OrganizationID)
	ctx.Info("Service Account ID: %s", result.ServiceAccountID)
	if result.DNSHostname != "" {
		ctx.Info("DNS Hostname: %s", result.DNSHostname)
	}
	reportIdentityAnchor(ctx, result.IdentityIssuerURL)
	ctx.Info("Configuration saved to: %s", opts.OutputDir)

	return nil
}

// restartMirenServiceIfActive bounces the systemd miren.service so a newly
// saved registration takes effect, but only when systemd is managing a
// currently-running server. Other deployment styles (docker, manual, dev) are
// left alone — install paths run their own lifecycle steps afterward, and
// nothing the user needs to act on remains.
func restartMirenServiceIfActive(ctx *Context) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return
	}
	if err := exec.Command("systemctl", "is-active", "--quiet", "miren.service").Run(); err != nil {
		return
	}
	if err := exec.Command("systemctl", "restart", "miren.service").Run(); err != nil {
		ctx.Warn("Failed to restart miren.service: %v. Restart manually to apply the new registration.", err)
		return
	}
	ctx.Info("Restarted miren.service to apply the new registration.")
}

// RegisterStatus displays the current registration status
func RegisterStatus(ctx *Context, opts struct {
	Dir string `short:"d" long:"dir" description:"Registration directory" default:"/var/lib/miren/server"`
}) error {

	reg, err := registration.LoadRegistration(opts.Dir)
	if err != nil {
		return fmt.Errorf("failed to load registration: %w", err)
	}

	if reg == nil {
		fmt.Println("No cluster registration found")
		ctx.Printf("Run 'miren server register' to register this cluster with miren.cloud\n")
		return nil
	}

	ctx.Printf("Cluster Registration Status:\n")
	ctx.Printf("  Status: %s\n", reg.Status)
	ctx.Printf("  Cluster Name: %s\n", reg.ClusterName)

	if reg.Status == "pending" {
		ctx.Printf("  Registration ID: %s\n", reg.RegistrationID)
		ctx.Printf("  Expires At: %s\n", reg.ExpiresAt.Format(time.RFC3339))
		if time.Now().After(reg.ExpiresAt) {
			ctx.Printf("\n⚠️  This registration has expired. Run 'miren server register' to start a new registration.\n")
		} else {
			ctx.Printf("\n✓ Registration is pending approval. Run 'miren server register' to continue polling.\n")
		}
	} else {
		ctx.Printf("  Cluster ID: %s\n", reg.ClusterID)
		ctx.Printf("  Organization ID: %s\n", reg.OrganizationID)
		ctx.Printf("  Service Account ID: %s\n", reg.ServiceAccountID)
		ctx.Printf("  Cloud URL: %s\n", reg.CloudURL)
		ctx.Printf("  Registered At: %s\n", reg.RegisteredAt.Format(time.RFC3339))
	}

	if len(reg.Tags) > 0 {
		ctx.Printf("  Tags:\n")
		for k, v := range reg.Tags {
			ctx.Printf("    %s: %s\n", k, v)
		}
	}

	return nil
}

// anchorForRegistration picks the workload identity anchor a newly registered
// cluster starts on.
//
// New clusters anchor at cloud: it is the only option that works for a cluster
// that isn't reachable from the internet, and it keeps federation up while the
// cluster is down. Nothing is pinned to the old anchor yet, so a fresh
// registration is the one moment the choice is free — which is exactly why it
// is made here and recorded, rather than defaulted in server config where it
// would also catch clusters that registered long ago.
//
// Falls back to the cluster anchor when cloud offers none, so a self-hosted
// miren.cloud without discovery configured still registers cleanly.
func anchorForRegistration(identityIssuerURL string) string {
	if identityIssuerURL == "" {
		return registration.AnchorCluster
	}
	return registration.AnchorCloud
}

func reportIdentityAnchor(ctx *Context, identityIssuerURL string) {
	if identityIssuerURL == "" {
		ctx.Info("Workload Identity: anchored at this cluster (miren.cloud is not serving discovery)")
		return
	}
	ctx.Info("Workload Identity Issuer: %s", identityIssuerURL)
	ctx.Info("  Signing keys stay on this cluster; miren.cloud serves discovery for them.")
	ctx.Info("  Switch with: miren server identity-anchor cluster")
}
