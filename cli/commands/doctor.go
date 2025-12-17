package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"miren.dev/runtime/api/core/core_v1alpha"
	"miren.dev/runtime/api/entityserver/entityserver_v1alpha"
	"miren.dev/runtime/api/ingress"
	"miren.dev/runtime/clientconfig"
	"miren.dev/runtime/pkg/auth"
	"miren.dev/runtime/pkg/cloudauth"
)

var (
	infoGreen = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	infoRed   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	infoGray  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	infoLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("12"))
	infoBold  = lipgloss.NewStyle().Bold(true)
)

type cloudUserInfo struct {
	User struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
}

func fetchCloudUserInfo(ctx context.Context, cloudURL, token string) (*cloudUserInfo, error) {
	meURL, err := url.JoinPath(cloudURL, "/api/v1/me")
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", meURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var info cloudUserInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

func normalizeAuthServerURL(authServer string) string {
	if !strings.HasPrefix(authServer, "http://") && !strings.HasPrefix(authServer, "https://") {
		if strings.Contains(authServer, "localhost") || strings.Contains(authServer, "127.0.0.1") {
			return "http://" + authServer
		}
		return "https://" + authServer
	}
	return authServer
}

type authResult struct {
	Method       string
	IdentityName string
	Claims       *auth.ExtendedClaims
	UserInfo     *cloudUserInfo
}

// tryAuthenticate attempts to authenticate with the cluster using the configured identity.
// It returns auth details without printing anything - callers handle display.
func tryAuthenticate(ctx *Context, cfg *clientconfig.Config, cluster *clientconfig.ClusterConfig) authResult {
	result := authResult{Method: "none"}

	if cluster.Identity == "" {
		return result
	}

	identity, err := cfg.GetIdentity(cluster.Identity)
	if err != nil || identity == nil {
		return result
	}

	result.IdentityName = cluster.Identity

	switch identity.Type {
	case "keypair":
		privateKeyPEM, err := cfg.GetPrivateKeyPEM(identity)
		if err != nil {
			return result
		}

		keyPair, err := cloudauth.LoadKeyPairFromPEM(privateKeyPEM)
		if err != nil {
			return result
		}

		authServer := identity.Issuer
		if authServer == "" {
			authServer = cluster.Hostname
		}
		authServer = normalizeAuthServerURL(authServer)

		token, err := clientconfig.AuthenticateWithKey(ctx, authServer, keyPair)
		if err != nil {
			return result
		}

		result.Claims, _ = auth.ParseUnverifiedClaims(token)
		result.Method = "keypair"

		// Fetch user info from cloud
		result.UserInfo, _ = fetchCloudUserInfo(ctx, authServer, token)

	case "certificate":
		result.Method = "certificate"
	}

	return result
}

// Doctor shows a quick overview of the miren environment
func Doctor(ctx *Context, opts struct {
	ConfigCentric
}) error {
	type infoSection struct {
		ok      bool
		message string
	}

	var (
		configuration  infoSection
		server         infoSection
		authentication infoSection
		apps           infoSection
		authUser       string
		authOrg        string
		routeCount     int
	)

	// Load configuration
	cfg, err := opts.LoadConfig()
	if err != nil && !errors.Is(err, clientconfig.ErrNoConfig) {
		return err
	}

	// Configuration info
	if cfg == nil || errors.Is(err, clientconfig.ErrNoConfig) {
		configuration.ok = false
		configuration.message = "no clusters configured"
	} else {
		clusterCount := 0
		cfg.IterateClusters(func(_ string, _ *clientconfig.ClusterConfig) error {
			clusterCount++
			return nil
		})

		if clusterCount == 0 {
			configuration.ok = false
			configuration.message = "no clusters configured"
		} else {
			configuration.ok = true
			configuration.message = fmt.Sprintf("%s (%d clusters)", cfg.ActiveCluster(), clusterCount)
		}
	}

	// Server and auth info (only if configured)
	if configuration.ok && cfg != nil {
		clusterName := cfg.ActiveCluster()
		cluster, err := cfg.GetCluster(clusterName)
		if err == nil && cluster != nil {
			// Try to connect
			client, err := ctx.RPCClient("entities")
			if err == nil {
				defer client.Close()
				server.ok = true
				server.message = "connected"

				// Get user info
				if cluster.Identity == "" {
					authentication.message = "(no identity)"
				} else {
					authRes := tryAuthenticate(ctx, cfg, cluster)
					if authRes.Claims != nil {
						authentication.ok = true
						authentication.message = authRes.Claims.Subject
						authUser = authRes.Claims.Subject
						authOrg = authRes.Claims.OrganizationID
					}
				}

				// Count apps and check health
				eac := entityserver_v1alpha.NewEntityAccessClient(client)
				appCount, unhealthyCount := countAppsHealth(ctx, eac)
				if appCount == 0 {
					apps.message = "none"
				} else if unhealthyCount > 0 {
					apps.ok = false
					apps.message = fmt.Sprintf("%d/%d unhealthy", unhealthyCount, appCount)
				} else {
					apps.ok = true
					apps.message = fmt.Sprintf("%d deployed", appCount)
				}

				// Count routes
				ic := ingress.NewClient(ctx.Log, client)
				routes, err := ic.List(ctx)
				if err == nil {
					routeCount = len(routes)
				}
			} else {
				server.ok = false
				server.message = "not connected"
				authentication.message = "(skipped)"
				apps.message = "(skipped)"
			}
		}
	} else {
		server.message = "(skipped)"
		authentication.message = "(skipped)"
		apps.message = "(skipped)"
	}

	// Text output
	ctx.Printf("%s\n", infoBold.Render("Miren Doctor"))
	ctx.Printf("%s\n", infoGray.Render("============"))

	// Configuration
	printInfoLine(ctx, "Configuration", configuration.ok, configuration.message, false)

	// Server
	skipped := server.message == "(skipped)"
	printInfoLine(ctx, "Server", server.ok, server.message, skipped)

	// Authentication
	skipped = authentication.message == "(skipped)" || authentication.message == "(no identity)"
	printInfoLine(ctx, "Authentication", authentication.ok, authentication.message, skipped)

	// Apps
	skipped = apps.message == "(skipped)"
	appsNeutral := apps.message == "none"
	if appsNeutral {
		skipped = true
	}
	printInfoLine(ctx, "Apps", apps.ok, apps.message, skipped)

	// User info and counts
	if configuration.ok && server.ok {
		ctx.Printf("\n")
		if authUser != "" {
			userLine := fmt.Sprintf("%s %s", infoLabel.Render("User:"), authUser)
			if authOrg != "" {
				userLine += infoGray.Render(fmt.Sprintf(" (org: %s)", authOrg))
			}
			ctx.Printf("%s\n", userLine)
		}
		ctx.Printf("%s %d configured\n", infoLabel.Render("Routes:"), routeCount)
	}

	// Help text
	ctx.Printf("\n")
	if !configuration.ok {
		ctx.Printf("Get started:\n")
		ctx.Printf("  %s        %s\n", infoBold.Render("miren login"), infoGray.Render("# Authenticate with miren.cloud"))
		ctx.Printf("  %s  %s\n", infoBold.Render("miren cluster add"), infoGray.Render("# Add a cluster manually"))
	} else {
		ctx.Printf("%s\n", infoGray.Render("Use 'miren doctor <topic>' for details: config, server, auth, apps"))
	}

	return nil
}

func printInfoLine(ctx *Context, label string, ok bool, message string, skipped bool) {
	var indicator string
	if ok {
		indicator = infoGreen.Render("[✓]")
	} else if skipped {
		indicator = infoGray.Render("[-]")
		message = infoGray.Render(message)
	} else {
		indicator = infoRed.Render("[✗]")
	}

	// Pad label to 14 chars for alignment
	paddedLabel := fmt.Sprintf("%-14s", label)
	ctx.Printf("  %s %s %s\n", indicator, paddedLabel, message)
}

// countAppsHealth returns total app count and unhealthy app count.
// An app is considered unhealthy if its deployment status is "failed".
func countAppsHealth(ctx context.Context, eac *entityserver_v1alpha.EntityAccessClient) (total int, unhealthy int) {
	// Get apps
	kindRes, err := eac.LookupKind(ctx, "app")
	if err != nil {
		return 0, 0
	}
	appsRes, err := eac.List(ctx, kindRes.Attr())
	if err != nil {
		return 0, 0
	}

	total = len(appsRes.Values())
	if total == 0 {
		return 0, 0
	}

	// Build app name set
	appNames := make(map[string]bool)
	for _, e := range appsRes.Values() {
		var md core_v1alpha.Metadata
		md.Decode(e.Entity())
		appNames[md.Name] = true
	}

	// Get deployments to check for failed status
	deploymentKindRes, err := eac.LookupKind(ctx, "deployment")
	if err != nil {
		return total, 0
	}
	deploymentsRes, err := eac.List(ctx, deploymentKindRes.Attr())
	if err != nil {
		return total, 0
	}

	// Build map of most recent deployment per app
	type deploymentInfo struct {
		status      string
		completedAt string
	}
	deploymentMap := make(map[string]deploymentInfo)
	for _, e := range deploymentsRes.Values() {
		var d core_v1alpha.Deployment
		d.Decode(e.Entity())

		existing, ok := deploymentMap[d.AppName]
		if !ok || d.CompletedAt > existing.completedAt {
			deploymentMap[d.AppName] = deploymentInfo{
				status:      d.Status,
				completedAt: d.CompletedAt,
			}
		}
	}

	// Count unhealthy apps (only failed deployments count as unhealthy)
	for appName := range appNames {
		if dep, ok := deploymentMap[appName]; ok {
			if dep.status == "failed" {
				unhealthy++
			}
		}
	}

	return total, unhealthy
}
