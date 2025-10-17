package commands

import (
	"context"
	"fmt"
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
			existing.RegisteredAt = time.Now()

			if err := registration.SaveRegistration(opts.OutputDir, existing); err != nil {
				return fmt.Errorf("failed to save registration: %w", err)
			}

			ctx.Completed("Registration successful!")
			ctx.Info("Cluster ID: %s", status.ClusterID)
			ctx.Info("Organization ID: %s", status.OrganizationID)
			ctx.Info("Service Account ID: %s", status.ServiceAccountID)
			ctx.Info("Configuration saved to: %s", opts.OutputDir)

			return nil
		} else if existing.Status == "approved" {
			return fmt.Errorf("cluster already registered as %s (ID: %s)", existing.ClusterName, existing.ClusterID)
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
		PrivateKey:       privateKey,
		CloudURL:         opts.CloudURL,
		RegisteredAt:     time.Now(),
		Tags:             opts.Tags,
		Status:           "approved",
	}

	if err := registration.SaveRegistration(opts.OutputDir, stored); err != nil {
		return fmt.Errorf("failed to save registration: %w", err)
	}

	ctx.Completed("Registration successful!")
	ctx.Info("Cluster ID: %s", status.ClusterID)
	ctx.Info("Organization ID: %s", status.OrganizationID)
	ctx.Info("Service Account ID: %s", status.ServiceAccountID)
	ctx.Info("Configuration saved to: %s", opts.OutputDir)

	return nil
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
		ctx.Printf("Run 'miren register' to register this cluster with miren.cloud\n")
		return nil
	}

	ctx.Printf("Cluster Registration Status:\n")
	ctx.Printf("  Status: %s\n", reg.Status)
	ctx.Printf("  Cluster Name: %s\n", reg.ClusterName)

	if reg.Status == "pending" {
		ctx.Printf("  Registration ID: %s\n", reg.RegistrationID)
		ctx.Printf("  Expires At: %s\n", reg.ExpiresAt.Format(time.RFC3339))
		if time.Now().After(reg.ExpiresAt) {
			ctx.Printf("\n⚠️  This registration has expired. Run 'miren register' to start a new registration.\n")
		} else {
			ctx.Printf("\n✓ Registration is pending approval. Run 'miren register' to continue polling.\n")
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
