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

## Deploy Your First App

Miren includes smart defaults for common languages and frameworks. Just run:

```bash
cd your-project
miren deploy
```

That's it! Miren will:
- Detect your language and framework
- Build a container image
- Deploy your app
- Show you the URL to access it

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

## Working with Routes

View HTTP routes for your applications:

```bash
miren route
```

## Language Support

Miren has built-in support for common languages:

- **Python**: Detects `requirements.txt`, `Pipfile`, `pyproject.toml`
- **JavaScript/Node**: Detects `package.json`
- **Go**: Detects `go.mod`
- **Ruby**: Detects `Gemfile`
- **And more...**

Don't see your language? You can always provide a `Dockerfile`.

## Connecting to miren.cloud (Optional)

Miren works standalone, but connecting to miren.cloud gives you:
- Team management and access control
- Automatic data backup and sync
- Multi-environment workflows

To connect:

```bash
# Login to miren.cloud
miren login

# Register your cluster
miren register -n my-cluster
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
