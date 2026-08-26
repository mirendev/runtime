# Managed metrics smoke app

This app exposes a Prometheus-compatible endpoint from two fixed replicas. Its
metric deliberately tries to spoof Miren's reserved labels so a receiver can
confirm that the runtime replaces them.

Deploy the happy path from the repository root:

```bash
miren deploy --dir testdata/managed-metrics-app --force
```

For a local end-to-end test, run the inspection receiver inside the development
environment. In another terminal, restart the development server with a stable
cluster label and point it at the receiver:

```bash
./hack/dev-exec go run ./hack/cmd/remote-write-dump
```

```bash
./hack/dev-exec --root env \
  DEV_SERVER_FLAGS="--config-cluster-name managed-metrics-dev \
  --metrics-remote-write-url http://127.0.0.1:19090/api/v1/write \
  --metrics-remote-write-audience managed-metrics-smoke" \
  hack/dev-server restart
```

The receiver decodes Prometheus Remote Write requests and checks the expected
telemetry-writer JWT claims. It does not verify the token signature; the runtime
integration test covers that boundary. Inspect received samples or switch the
receiver into a failure mode with:

```bash
curl 'http://127.0.0.1:19090/api/v1/samples?metric=managed_metrics_smoke_value'
curl -X POST 'http://127.0.0.1:19090/control?mode=unauthorized&clear=true'
curl -X POST 'http://127.0.0.1:19090/control?mode=unavailable&clear=true'
curl -X POST 'http://127.0.0.1:19090/control?mode=accept'
```

Set `SMOKE_METRICS_MODE` during a later deploy to exercise scrape failures:

```bash
miren deploy --dir testdata/managed-metrics-app --env SMOKE_METRICS_MODE=error --force
miren deploy --dir testdata/managed-metrics-app --env SMOKE_METRICS_MODE=malformed --force
miren deploy --dir testdata/managed-metrics-app --env SMOKE_METRICS_MODE=oversized --force
```

The modes return HTTP 503, invalid exposition, and a response larger than the
managed scraper's 2 MiB limit, respectively.

Scrape and delivery failures appear in the scraper's system logs:

```bash
miren logs system vmagent --last 15m
```
