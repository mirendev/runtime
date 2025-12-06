---
sidebar_position: 5
---

# Application Scaling

Miren automatically scales your application instances based on traffic. This page explains how scaling works and how to configure it for your needs.

## How Autoscaling Works

By default, Miren uses **autoscaling** for web services. As traffic to your application increases, Miren automatically launches additional instances to handle the load. When traffic decreases, instances are scaled back down.

This approach is similar to Google Cloud Run's scaling model: instead of guessing how many replicas you need, Miren observes actual demand and adjusts automatically.

### Why Autoscaling by Default?

Guessing instance counts is error-prone:
- Too few instances and your app can't handle traffic spikes
- Too many instances waste resources when traffic is low
- Manual scaling requires constant monitoring and adjustment

With autoscaling, Miren handles this automatically so you can focus on your application.

### Scale to Zero

Miren can scale your application all the way down to zero instances when there's no traffic. This is particularly valuable for self-hosted deployments where resource efficiency matters:

- **Better utilization**: Run dozens of apps on a single server—only active apps consume resources
- **Lower costs**: Development, staging, and low-traffic production apps don't waste memory or CPU
- **No idle tax**: Internal tools, webhooks, and scheduled tasks don't need dedicated instances waiting around

When a request arrives for a scaled-to-zero app, Miren quickly spins up an instance to handle it. The first request may have slightly higher latency, but subsequent requests are served immediately.

## Scaling Modes

Miren supports two scaling modes:

| Mode | Description | Use Case |
|------|-------------|----------|
| `auto` | Scales instances based on traffic | Stateless web services, APIs |
| `fixed` | Runs a set number of instances | Databases, workers, stateful services |

## Default Behavior

When you deploy without explicit configuration:

- **`web` service**: Auto mode with 10 concurrent requests per instance, 15-minute scale-down delay
- **All other services**: Fixed mode with 1 instance

## Configuring Scaling

Configure scaling in your `.miren/app.toml` file under `[services.<name>.concurrency]`.

### Auto Mode (Default for Web)

Auto mode scales instances based on concurrent requests:

```toml
[services.web.concurrency]
mode = "auto"
requests_per_instance = 10
scale_down_delay = "15m"
```

#### Auto Mode Options

| Option | Description | Default |
|--------|-------------|---------|
| `mode` | Must be `"auto"` | `"auto"` for web |
| `requests_per_instance` | Target concurrent requests per instance | 10 |
| `scale_down_delay` | How long to wait before scaling down idle instances | 15m |

#### How Auto Mode Calculates Instances

Miren targets `requests_per_instance` **concurrent** requests per instance, the number of in-flight requests being handled simultaneously, not requests over a period of time. For example, with `requests_per_instance = 10`:

- 5 concurrent requests → 1 instance
- 15 concurrent requests → 2 instances
- 100 concurrent requests → 10 instances

This means a single instance handling fast requests (e.g., 10ms each) can serve thousands of requests per second while staying under the concurrency limit.

The `scale_down_delay` prevents thrashing when traffic fluctuates. An instance won't be terminated until it has been idle for this duration.

### Fixed Mode

Fixed mode runs a specific number of instances regardless of traffic:

```toml
[services.worker.concurrency]
mode = "fixed"
num_instances = 3
```

#### Fixed Mode Options

| Option | Description | Default |
|--------|-------------|---------|
| `mode` | Must be `"fixed"` | `"fixed"` for non-web |
| `num_instances` | Exact number of instances to run | 1 |

## Examples

### High-Traffic API

For an API that handles many concurrent requests:

```toml
[services.web.concurrency]
mode = "auto"
requests_per_instance = 50
scale_down_delay = "5m"
```

This configuration:
- Allows 50 concurrent requests per instance (higher density)
- Scales down after 5 minutes of reduced traffic (faster scale-down)

### Background Worker

For a background job processor:

```toml
[services.worker.concurrency]
mode = "fixed"
num_instances = 2
```

This runs exactly 2 worker instances at all times.

### Database Service

For a database that should always be running:

```toml
[services.db]
image = "postgres:16"

[services.db.concurrency]
mode = "fixed"
num_instances = 1
```

### Complete Multi-Service App

```toml
name = "myapp"

# Web service: autoscales based on traffic
[services.web.concurrency]
mode = "auto"
requests_per_instance = 20
scale_down_delay = "10m"

# Worker service: fixed at 3 instances
[services.worker.concurrency]
mode = "fixed"
num_instances = 3

# Database: single instance with persistent storage
[services.db]
image = "postgres:16"

[services.db.concurrency]
mode = "fixed"
num_instances = 1

[[services.db.disks]]
name = "postgres-data"
mount_path = "/var/lib/postgresql/data"
size_gb = 20
```

## Scaling and Disks

Services with persistent disks must use fixed mode with exactly 1 instance:

```toml
[services.db.concurrency]
mode = "fixed"
num_instances = 1

[[services.db.disks]]
name = "db-data"
mount_path = "/data"
size_gb = 10
```

This restriction exists because disks use exclusive leasing where only one instance can mount a disk at a time.

## Tuning Tips

### Choosing `requests_per_instance`

- **Lower values** (5-10): More responsive scaling, higher resource usage
- **Higher values** (50-100): More efficient resource usage, may have slower response to traffic spikes

Start with the default (10) and adjust based on your application's characteristics.

### Choosing `scale_down_delay`

- **Shorter delays** (2m-5m): Faster resource reclamation, but may cause more scaling churn
- **Longer delays** (15m-30m): More stable instance counts, but holds resources longer

Consider your traffic patterns:
- Bursty traffic: Use longer delays to avoid constant scaling
- Predictable traffic: Shorter delays work well

## Monitoring Scaling

View your current instance counts:

```bash
# See app status including instance counts
miren app

# Watch instance counts in real-time
miren app --watch

# List all running sandboxes
miren sandbox list
```

## Next Steps

- [Getting Started](/getting-started) - Deploy your first app
- [CLI Reference](/cli-reference) - All available commands
