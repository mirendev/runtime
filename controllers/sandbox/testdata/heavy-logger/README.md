# Heavy Logger Test App

A Go HTTP server designed to generate large volumes of logs for testing the log reattachment fix after miren restart. Mimics the cloud server's logging behavior using `slog`.

## Purpose

This app helps test the fix for the restart bug where stdout buffers fill up and block processes when logs aren't properly reattached. It generates configurable amounts of logs per request, making it easy to reproduce the buffer-filling scenario.

## Features

- **Mimics cloud server** - Uses `slog` with JSON output, same logging patterns
- **Logs heavily on each request** - Configurable number of log lines per request
- **Request lifecycle logging** - "Request began" + "Request ended" (like cloud server)
- **Background heartbeat logging** - Keeps stdout active even without requests
- **Request tracking** - Each request gets a unique ID and detailed logging
- **Configurable log volume** - Adjust via environment variables

## Configuration

Environment variables:
- `PORT` - Server port (default: 3000)
- `LINES_PER_REQUEST` - Number of log lines per request (default: 20)
- `LOG_LINE_SIZE` - Approximate size of each log line in bytes (default: 200)

## Usage

### Manual Testing of Log Reattachment

1. **Deploy with miren:**
   ```bash
   cd controllers/sandbox/testdata/heavy-logger
   miren deploy heavy-logger
   ```

   Or for heavy logging mode:
   ```bash
   LINES_PER_REQUEST=100 LOG_LINE_SIZE=500 miren deploy heavy-logger
   ```

2. **Expose via http route:**
   ```bash
   miren route add heavy-logger.test heavy-logger
   ```

3. **Generate traffic to fill stdout buffer:**
   ```bash
   # Make many requests quickly
   for i in {1..100}; do
     curl http://heavy-logger.test/ &
   done
   wait

   # Or use a simple loop
   while true; do curl http://heavy-logger.test/; sleep 0.1; done
   ```

4. **Restart miren and verify app still works:**
   ```bash
   systemctl restart miren
   # Wait a few seconds for recovery
   curl http://heavy-logger.test/  # Should still respond

   # Check that logs were reattached
   miren logs -a heavy-logger | grep "reattached logs"
   ```

### Testing Different Log Volumes

**Light logging (baseline):**
- `LINES_PER_REQUEST=5`, `LOG_LINE_SIZE=100`
- ~500 bytes per request
- Should work even without log reattachment

**Medium logging (typical app):**
- `LINES_PER_REQUEST=20`, `LOG_LINE_SIZE=200`
- ~4KB per request
- Will fill 64KB buffer after ~16 requests

**Heavy logging (reproduces bug):**
- `LINES_PER_REQUEST=100`, `LOG_LINE_SIZE=500`
- ~50KB per request
- Will fill buffer after just 1-2 requests

## What to Look For

### Bug Present (without fix):
1. Deploy heavy-logger with high log volume
2. Make a few requests - app works initially
3. Restart miren
4. Make requests after restart
5. **App wedges** - requests hang, TCP connections in CLOSE_WAIT
6. `ss -s` inside container shows many sockets stuck

### Bug Fixed (with log reattachment):
1. Same steps as above
2. After restart, check logs: `INFO reattached logs to container`
3. **App continues working** - requests succeed
4. Logs continue being collected in ClickHouse

## Response Format

```json
{
  "requestId": 42,
  "message": "Heavy Logger Test App",
  "linesLogged": 50,
  "duration": "23ms",
  "totalRequests": 42,
  "config": {
    "linesPerRequest": 50,
    "logLineSize": 300
  }
}
```

## Logs Generated

Each request generates:
- 1 REQUEST START line
- N lines of heavy log data (configurable)
- 1 REQUEST END line
- 1 STATS line

Plus background heartbeat every 5 seconds.

With default settings (20 lines * 200 bytes = 4KB per request), the 64KB stdout buffer fills after ~16 requests, making it easy to reproduce the blocking issue.
