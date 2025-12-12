package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
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
		authUser       string
		authOrg        string
		appCount       int
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
				server.ok = true
				server.message = "connected"

				// Get user info
				if cluster.Identity == "" {
					authentication.message = "(no identity)"
				} else {
					identity, err := cfg.GetIdentity(cluster.Identity)
					if err == nil && identity != nil && identity.Type == "keypair" {
						privateKeyPEM, err := cfg.GetPrivateKeyPEM(identity)
						if err == nil {
							keyPair, err := cloudauth.LoadKeyPairFromPEM(privateKeyPEM)
							if err == nil {
								authServer := identity.Issuer
								if authServer == "" {
									authServer = cluster.Hostname
								}
								if !strings.HasPrefix(authServer, "http://") && !strings.HasPrefix(authServer, "https://") {
									if strings.Contains(authServer, "localhost") || strings.Contains(authServer, "127.0.0.1") {
										authServer = "http://" + authServer
									} else {
										authServer = "https://" + authServer
									}
								}
								token, err := clientconfig.AuthenticateWithKey(ctx, authServer, keyPair)
								if err == nil {
									claims, _ := auth.ParseUnverifiedClaims(token)
									if claims != nil {
										authentication.ok = true
										authentication.message = claims.Subject
										authUser = claims.Subject
										authOrg = claims.OrganizationID
									}
								}
							}
						}
					}
				}

				// Count apps
				eac := entityserver_v1alpha.NewEntityAccessClient(client)
				kindRes, err := eac.LookupKind(ctx, "app")
				if err == nil {
					res, err := eac.List(ctx, kindRes.Attr())
					if err == nil {
						appCount = len(res.Values())
					}
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
			}
		}
	} else {
		server.message = "(skipped)"
		authentication.message = "(skipped)"
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
		ctx.Printf("%s %d deployed\n", infoLabel.Render("Apps:"), appCount)
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
