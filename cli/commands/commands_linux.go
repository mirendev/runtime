package commands

import (
	"miren.dev/mflags"
)

func addCommands(d *mflags.Dispatcher) {
	// Server command is now defined in commands.go (renamed from dev)

	// Cloud registration commands
	d.Dispatch("server register", Infer("server register", "Register this cluster with miren.cloud", RegisterStandalone,
		WithExample(mflags.Example{
			Name: "Register with cloud",
			Body: "miren server register --name my-cluster",
		}),
		WithExample(mflags.Example{
			Name: "Register with a specific cloud URL",
			Body: "miren server register --name my-cluster --url https://cloud.example.com",
		}),
	))

	d.Dispatch("server register status", Infer("server register status", "Show cluster registration status", RegisterStatus,
		WithExample(mflags.Example{
			Name: "Check registration status",
			Body: "miren server register status",
		}),
	))

	d.Dispatch("server unregister", Infer("server unregister", "Detach this cluster from miren.cloud", Unregister,
		WithExample(mflags.Example{
			Name: "Unregister from cloud",
			Body: "miren server unregister",
		}),
		WithExample(mflags.Example{
			Name: "Clear local registration when the cloud entry is already gone",
			Body: "miren server unregister --local-only",
		}),
	))

	d.Dispatch("server identity-anchor", Infer("server identity-anchor", "Move where this cluster's workload identity is anchored", IdentityAnchor,
		WithExample(mflags.Example{
			Name: "Let miren.cloud serve discovery for this cluster",
			Body: "miren server identity-anchor cloud",
		}),
		WithExample(mflags.Example{
			Name: "Serve discovery from the cluster itself",
			Body: "miren server identity-anchor cluster",
		}),
	))

	// Server management commands
	d.Dispatch("server install", Infer("server install", "Install systemd service for miren server", ServerInstall,
		WithExample(mflags.Example{
			Name: "Install with cloud registration",
			Body: "miren server install",
		}),
		WithExample(mflags.Example{
			Name: "Install without cloud (local only)",
			Body: "miren server install --without-cloud",
		}),
		WithExample(mflags.Example{
			Name: "Install with an unattended enroll token",
			Body: `miren server install --enroll-token "$(cat /etc/miren/enroll-token)"`,
		}),
	))

	d.Dispatch("server uninstall", Infer("server uninstall", "Remove systemd service for miren server", ServerUninstall,
		WithExample(mflags.Example{
			Name: "Uninstall the server",
			Body: "miren server uninstall",
		}),
		WithExample(mflags.Example{
			Name: "Uninstall and remove all data",
			Body: "miren server uninstall --remove-data",
		}),
	))

	d.Dispatch("server status", Infer("server status", "Show miren service status", ServerStatus,
		WithExample(mflags.Example{
			Name: "Show server status",
			Body: "miren server status",
		}),
		WithExample(mflags.Example{
			Name: "Follow server logs",
			Body: "miren server status --follow",
		}),
	))
}

// setupServerComponents is deprecated and will be removed.
// All server components are now initialized explicitly via ServerState.
