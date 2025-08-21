package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"miren.dev/runtime/pkg/cloudauth"
	"miren.dev/runtime/pkg/rbac"
	"miren.dev/runtime/pkg/registration"
)

// DebugRBAC fetches and displays the RBAC rules from miren.cloud
func DebugRBAC(ctx *Context, opts struct {
	OutputDir string `short:"d" long:"dir" description:"Registration directory" default:"/var/lib/miren/server"`
	Raw       bool   `short:"r" long:"raw" description:"Show raw JSON response"`
}) error {
	// Load registration to get auth credentials
	reg, err := registration.LoadRegistration(opts.OutputDir)
	if err != nil {
		return fmt.Errorf("failed to load registration: %w", err)
	}
	if reg == nil {
		return fmt.Errorf("no cluster registration found - run 'miren register' first")
	}
	if reg.Status != "approved" {
		return fmt.Errorf("registration is not approved (status: %s)", reg.Status)
	}

	// Use CloudURL from registration
	cloudURL := reg.CloudURL
	if cloudURL == "" {
		cloudURL = cloudauth.DefaultCloudURL
	}

	ctx.Info("Fetching RBAC rules from %s...", cloudURL)

	// Load the private key
	keyPath := filepath.Join(opts.OutputDir, "service-account.key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to load service account key: %w", err)
	}

	keyPair, err := cloudauth.LoadKeyPairFromPEM(string(keyData))
	if err != nil {
		return fmt.Errorf("failed to parse service account key: %w", err)
	}

	// Create auth client
	authClient, err := cloudauth.NewAuthClient(cloudURL, keyPair)
	if err != nil {
		return fmt.Errorf("failed to create auth client: %w", err)
	}

	// Create PolicyFetcher
	fetcher := cloudauth.NewPolicyFetcher(cloudURL, authClient,
		cloudauth.WithLogger(ctx.Log))

	// Fetch the policy immediately
	if err := fetcher.Fetch(ctx); err != nil {
		return fmt.Errorf("failed to fetch policy: %w", err)
	}

	// Get the policy
	policy := fetcher.GetPolicy()
	if policy == nil {
		return fmt.Errorf("failed to fetch policy - no policy returned")
	}

	// If raw output requested, marshal and print the policy
	if opts.Raw {
		prettyJSON, err := json.MarshalIndent(policy, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to format JSON: %w", err)
		}

		fmt.Println(string(prettyJSON))
		return nil
	}

	// Display the parsed policy
	ctx.Completed("Successfully fetched RBAC policy")
	ctx.Printf("\nRBAC Policy:\n")
	ctx.Printf("  Rules: %d\n", len(policy.Rules))

	if len(policy.Rules) > 0 {
		ctx.Printf("\nRules:\n")
		for i, rule := range policy.Rules {
			ctx.Printf("\n  Rule %d:\n", i+1)
			ctx.Printf("    ID: %s\n", rule.ID)
			ctx.Printf("    Name: %s\n", rule.Name)
			if rule.Description != "" {
				ctx.Printf("    Description: %s\n", rule.Description)
			}

			// Display tag selector
			if len(rule.TagSelector.Expressions) > 0 {
				ctx.Printf("    Tag Expressions:\n")
				for _, expr := range rule.TagSelector.Expressions {
					if expr.Operator == "exists" || expr.Operator == "not_exists" {
						ctx.Printf("      - %s %s\n", expr.Tag, expr.Operator)
					} else {
						ctx.Printf("      - %s %s %v\n", expr.Tag, expr.Operator, expr.Value)
					}
				}
			} else {
				ctx.Printf("    Tag Selector: (matches all)\n")
			}

			// Display groups
			if len(rule.Groups) > 0 {
				ctx.Printf("    Groups:\n")
				for _, group := range rule.Groups {
					ctx.Printf("      - %s\n", group)
				}
			}

			// Display permissions
			if len(rule.Permissions) > 0 {
				ctx.Printf("    Permissions:\n")
				for _, perm := range rule.Permissions {
					ctx.Printf("      - %s: %v\n", perm.Resource, perm.Actions)
				}
			}

			ctx.Printf("    Created: %s\n", rule.CreatedAt)
			if rule.UpdatedAt != "" && rule.UpdatedAt != rule.CreatedAt {
				ctx.Printf("    Updated: %s\n", rule.UpdatedAt)
			}
		}
	}

	return nil
}

// DebugRBACTest tests RBAC evaluation locally with fetched rules
func DebugRBACTest(ctx *Context, opts struct {
	OutputDir string            `short:"d" long:"dir" description:"Registration directory" default:"/var/lib/miren/server"`
	Groups    []string          `short:"g" long:"group" description:"Groups to test with"`
	Tags      map[string]string `short:"t" long:"tag" description:"Tags to test with (key:value)"`
	Resource  string            `short:"r" long:"resource" description:"Resource to test" required:"true"`
	Action    string            `short:"a" long:"action" description:"Action to test" required:"true"`
}) error {
	// Load registration to get auth credentials
	reg, err := registration.LoadRegistration(opts.OutputDir)
	if err != nil {
		return fmt.Errorf("failed to load registration: %w", err)
	}
	if reg == nil {
		return fmt.Errorf("no cluster registration found - run 'miren register' first")
	}
	if reg.Status != "approved" {
		return fmt.Errorf("registration is not approved (status: %s)", reg.Status)
	}

	// Use CloudURL from registration
	cloudURL := reg.CloudURL
	if cloudURL == "" {
		cloudURL = cloudauth.DefaultCloudURL
	}

	ctx.Info("Fetching RBAC rules from %s...", cloudURL)

	// Load the private key
	keyPath := filepath.Join(opts.OutputDir, "service-account.key")
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("failed to load service account key: %w", err)
	}

	keyPair, err := cloudauth.LoadKeyPairFromPEM(string(keyData))
	if err != nil {
		return fmt.Errorf("failed to parse service account key: %w", err)
	}

	// Create auth client
	authClient, err := cloudauth.NewAuthClient(cloudURL, keyPair)
	if err != nil {
		return fmt.Errorf("failed to create auth client: %w", err)
	}

	// Create PolicyFetcher
	fetcher := cloudauth.NewPolicyFetcher(cloudURL, authClient,
		cloudauth.WithLogger(ctx.Log))

	// Fetch the policy immediately
	bgCtx := context.Background()
	if err := fetcher.Fetch(bgCtx); err != nil {
		return fmt.Errorf("failed to fetch policy: %w", err)
	}

	// Get the policy
	policy := fetcher.GetPolicy()
	if policy == nil {
		return fmt.Errorf("failed to fetch policy - no policy returned")
	}

	ctx.Completed("Successfully fetched RBAC policy with %d rules", len(policy.Rules))

	// The PolicyFetcher itself implements PolicyProvider, so we can use it directly
	// as the provider for the evaluator

	// Create evaluator with a basic logger
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	evaluator := rbac.NewEvaluator(ctx, fetcher, logger)

	// Clean up tags
	cleanTags := make(map[string]interface{})
	for k, v := range opts.Tags {
		cleanTags[k] = v
	}

	ctx.Printf("\nTesting RBAC evaluation:\n")
	ctx.Printf("  Groups: %v\n", opts.Groups)
	ctx.Printf("  Tags: %v\n", cleanTags)
	ctx.Printf("  Resource: %s\n", opts.Resource)
	ctx.Printf("  Action: %s\n", opts.Action)

	// Create request
	request := &rbac.Request{
		Subject:  "debug-user",
		Groups:   opts.Groups,
		Resource: opts.Resource,
		Action:   opts.Action,
		Tags:     cleanTags,
	}

	// Create an explainer to track the evaluation
	explainer := &debugExplainer{ctx: ctx}

	decision := evaluator.Evaluate(request, rbac.WithExplainer(explainer))
	allowed := decision == rbac.DecisionAllow

	ctx.Printf("\n")
	if allowed {
		ctx.Completed("Action is ALLOWED")
	} else {
		ctx.Warn("Action is DENIED")
	}

	return nil
}

// debugExplainer implements rbac.Explainer for debugging
type debugExplainer struct {
	ctx *Context
}

func (d *debugExplainer) RuleConsidered(rule *rbac.Rule) {
	d.ctx.Printf("\nConsidering rule '%s' (ID: %s)\n", rule.Name, rule.ID)
	if len(rule.TagSelector.Expressions) > 0 {
		d.ctx.Printf("  Tag expressions:\n")
		for _, expr := range rule.TagSelector.Expressions {
			if expr.Operator == "exists" || expr.Operator == "not_exists" {
				d.ctx.Printf("    - %s %s\n", expr.Tag, expr.Operator)
			} else {
				d.ctx.Printf("    - %s %s %v\n", expr.Tag, expr.Operator, expr.Value)
			}
		}
	} else {
		d.ctx.Printf("  Tag expressions: (matches all)\n")
	}
	d.ctx.Printf("  Groups: %v\n", rule.Groups)
}

func (d *debugExplainer) RuleSkipped(rule *rbac.Rule, reason string) {
	d.ctx.Printf("  ✗ Skipped: %s\n", reason)
}

func (d *debugExplainer) RuleMatched(rule *rbac.Rule, resource string, action string) {
	d.ctx.Printf("  ✓ MATCHED! Grants '%s' on '%s'\n", action, resource)
	d.ctx.Printf("  Permissions granted by this rule:\n")
	for _, perm := range rule.Permissions {
		d.ctx.Printf("    - Resource: %s, Actions: %v\n", perm.Resource, perm.Actions)
	}
}

func (d *debugExplainer) NoRulesMatched() {
	d.ctx.Printf("\n✗ No rules matched the request\n")
}
