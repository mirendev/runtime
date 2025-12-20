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
- **Memory**: 4GB minimum, 8GB recommended
- **Storage**: 50GB minimum, 100GB recommended

[Why these requirements?](#a-note-on-system-requirements)

### Verify Installation

Check that Miren is installed correctly:

```bash
miren version
```

### Start the Miren Server

(Skip this step if you are using our [demo cluster](#using-our-demo-cluster))

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

### Using Our Demo Cluster

Ask for access to our demo cluster in #miren-club on [Discord](https://miren.dev/discord).

```bash
# Once you have access, connect to the demo cluster
$ miren login
$ miren cluster add

Select a cluster to bind:
   NAME                ORGANIZATION   ADDRESS
▸  miren-demo          mirendev       1.2.3.4:8443 (+7)
```

## Deploy Your First App

Miren automatically detects and builds your app. Supported languages include:
- **Python**: Detects `requirements.txt`, `Pipfile`, `pyproject.toml`, `uv.lock`
- **JavaScript/Node**: Detects `package.json` (npm, yarn)
- **Bun**: Detects `bun.lockb`
- **Go**: Detects `go.mod`
- **Ruby**: Detects `Gemfile`
- **Rust**: Detects `Cargo.toml`

Don't see your language? You can always provide a `Dockerfile`.

:::tip Preview before deploying
Use `miren deploy --analyze` to see what Miren detects without actually building or deploying:

```bash
miren deploy --analyze
```

This shows the detected stack, services, environment variables, and how each service will be started.
:::

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

### "How do I configure multiple instances of my application?"

By default, you don't!

A core philosophy of Miren is that guessing replica/instance/copy counts is the wrong way to manage
application scaling by default. For that reason, from day 1, Miren has built around autoscaling application
instances. It does this using the same technique that Google Cloud Run uses, namely that as
the amount of traffic to the application increases, additional instances are automatically launched.

And as the traffic reduces, the instance counts are automatically reduced.

If an application isn't stateless and needs to only run a certain number of copies, the application
can be switched to fixed mode where you set the number of instances. This is commonly used for services
that your application also needs, like databases, background workers, etc.

See [Application Scaling](/scaling) for full documentation on configuring scaling behavior.

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
miren app delete myapp
```

## Environment Variables

Manage environment variables for your application:

```bash
# View environment variables
miren env list

# Set an environment variable (then redeploy)
miren env set --env KEY=value
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
- View the [FAQ](https://miren.dev/developer-preview#faq-1)
- See [Support](/support) for bug reports, feature requests, and community help

## A Note on System Requirements

Miren runs several components—containerd, etcd, buildkit, metrics, and logging—that together use around 600MB of memory at idle. During builds, memory usage spikes as buildkit compiles your application. A single Rails app with Postgres can push total usage past 1.3GB during deployment, which is why we recommend 4GB minimum.

For storage, container images and build caches add up quickly. Base images for languages like Ruby or Python are 50-80MB compressed but expand on disk, and BuildKit caches intermediate build layers aggressively—keeping up to 10GB by default. A single Rails deployment can use 15-20GB between base images, build cache, and the container registry. With multiple apps and their version history, usage grows from there. Starting with 50GB gives you comfortable headroom; if you're tight on space, you'll see "no space left on device" errors during builds.

We're still learning about system requirements as more users deploy Miren in different contexts. If you have an interesting deployment scenario or resource constraints you'd like to discuss, come chat with us on [Discord](https://miren.dev/discord)!

## Next Steps

- [Application Scaling](/scaling) - Configure how your app scales
- [CLI Reference](/cli-reference) - Learn about all available commands
- [App Commands](/cli/app) - Detailed application command reference
- [Disks](/disks) - Persistent storage for your applications
