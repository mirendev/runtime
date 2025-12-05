---
sidebar_position: 2
---

# Getting Started

Get up and running with Miren in minutes.

## Installation

Install Miren with a single command:

```bash
curl -fsSL https://miren.cloud/install | sudo bash
```

This script will:
- Download the latest Miren release
- Install the `miren` CLI binary
- Set up the Miren server
- Configure everything you need to start deploying

### System Requirements

- **Operating System**: Linux (kernel 5.10+)
- **Architecture**: x86_64 or arm64
- **Memory**: 2GB minimum, 4GB recommended
- **Storage**: 10GB minimum free space

### Verify Installation

Check that Miren is installed correctly:

```bash
miren version
```

### Start the Miren Server

Set up and start the Miren server:

```bash
sudo miren server install
```

This will:
- Download required components
- Register your cluster with [miren.cloud](/working-with-miren-cloud) (follow the prompts)
- Install and start the Miren systemd service
- Configure your CLI to use the local cluster

To skip cloud registration and run standalone:

```bash
sudo miren server install --without-cloud
```

## Deploy Your First App

Miren automatically detects and builds your app. Supported languages include:
- **Python**: Detects `requirements.txt`, `Pipfile`, `pyproject.toml`
- **JavaScript/Node**: Detects `package.json`
- **Go**: Detects `go.mod`
- **Ruby**: Detects `Gemfile`

Don't see your language? You can always provide a `Dockerfile`.

Just run:

```bash
cd your-project
miren init
miren deploy
```

That's it! Miren will:
- Detect your language and framework
- Build a container image
- Deploy your app
- Show you the URL to access it

Note: Your first app gets a default route automatically. For subsequent apps, you'll need to configure routes manually. See [Working with Routes](#working-with-routes).

## Check Your Application

### View All Applications

```bash
miren app list
```

### View Application Details

```bash
miren app
```

Or to view a specific app:

```bash
miren app --app myapp
```

### View Deployment History

See past deployments:

```bash
miren app history
```

## View Logs

See what your application is doing:

```bash
miren logs
```

View logs for a specific app:

```bash
miren logs --app myapp
```

Show logs from the last 5 minutes:

```bash
miren logs --last 5m
```

## Managing Applications

### Initialize a New Application

If you want to set up a new project:

```bash
miren init
```

This creates the necessary configuration for your project.

### Delete an Application

Remove an application and all its resources:

```bash
miren app delete --app myapp
```

## Environment Variables

Manage environment variables for your application:

```bash
# View environment variables
miren env

# Set an environment variable (then redeploy)
miren env set KEY=value
```

## Persistent Storage with Disks

Need to store data that survives restarts? Add a disk to your service in `.miren/app.toml`:

```toml
[[services.db.disks]]
name = "myapp-data"
mount_path = "/data"
size_gb = 10
```

**NOTE:** Disks can only be used with fixed instance scheduled services, which the default
web service is not.

Your data is automatically backed up to Miren Cloud. See [Disks](/disks) for full documentation.

## Working with Routes

When you deploy your first application, Miren automatically creates a default route for it.

For additional apps, you'll need to either create a custom route:

```bash
miren route set myapp.example.com myapp
```

Or set the app as the new default:

```bash
miren route set-default myapp
```

View HTTP routes for your applications:

```bash
miren route
```

## Troubleshooting

### Application Won't Start

Check the logs:

```bash
miren logs
```

### Can't Access Application

Verify the app is running:

```bash
miren app status
```

Check the routes:

```bash
miren route
```

### Need Help?

- Check the [CLI Reference](/cli-reference) for command details
- Join our [Discord community](https://discord.gg/miren)
- View the [FAQ](https://miren.dev/developer-preview#faq-1)

## Next Steps

- [CLI Reference](/cli-reference) - Learn about all available commands
- [App Commands](/cli/app) - Detailed application command reference
- [Disks](/disks) - Persistent storage for your applications
