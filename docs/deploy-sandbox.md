# Deploying to Sandbox Server

This document outlines the steps to deploy the runtime to the sandbox server.

## Prerequisites

- Access to the sandbox server via gcloud
- Proper permissions for the phinze-sandbox-462120 project
- Built runtime binary

## Deployment Steps

1. **Build the distribution binary**
   ```bash
   make dist
   ```

2. **Copy the binary to the sandbox server**
   ```bash
   gcloud compute scp bin/runtime-dist runtime-sandbox:~/runtime \
     --zone=us-central1-a \
     --tunnel-through-iap \
     --project=phinze-sandbox-462120
   ```

3. **SSH into the sandbox server**
   ```bash
   gcloud compute ssh runtime-sandbox \
     --zone=us-central1-a \
     --tunnel-through-iap \
     --project=phinze-sandbox-462120
   ```

4. **Stop the miren-runtime service**
   ```bash
   sudo systemctl stop miren-runtime
   ```

5. **Copy the binary into place**
   ```bash
   sudo cp ~/runtime /usr/local/bin/runtime
   sudo chmod +x /usr/local/bin/runtime
   ```

6. **Start the service**
   ```bash
   sudo systemctl start miren-runtime
   ```

7. **Verify the service is running**
   ```bash
   sudo systemctl status miren-runtime
   sudo journalctl -u miren-runtime -f
   ```

## Quick Deploy Script

You can create a script to automate these steps:

```bash
#!/bin/bash
set -e

# Build
make dist

# Copy to server
gcloud compute scp bin/runtime-dist runtime-sandbox:~/runtime \
  --zone=us-central1-a \
  --tunnel-through-iap \
  --project=phinze-sandbox-462120

# Deploy on server
gcloud compute ssh runtime-sandbox \
  --zone=us-central1-a \
  --tunnel-through-iap \
  --project=phinze-sandbox-462120 \
  --command="sudo systemctl stop miren-runtime && \
             sudo cp ~/runtime /usr/local/bin/runtime && \
             sudo chmod +x /usr/local/bin/runtime && \
             sudo systemctl start miren-runtime && \
             sudo systemctl status miren-runtime"
```

## Troubleshooting

- Check logs: `sudo journalctl -u miren-runtime -f`
- Check service status: `sudo systemctl status miren-runtime`
- Verify binary permissions: `ls -la /usr/local/bin/runtime`