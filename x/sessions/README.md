# Session control-plane SDK

Import `miren.dev/runtime/x/sessions` to manage an app's Sessions through the
coordinator REST API. This package uses only the Go standard library and has
the same experimental, pin-your-version contract as the rest of `x/`.

This is a **backend client**, not a browser or sandbox-local metadata API.
Use [`x/workload`](../workload) inside the worker to receive assignments,
report activity, and handle drain/cleanup notifications. Keep cluster
credentials out of frontend code.

## Inside a Miren workload

```go
client, err := sessions.NewClient(sessions.ConfigFromEnv("support-agent"))
if err != nil {
    return err
}
idleSeconds := int64(30)
s, err := client.Create(ctx, sessions.CreateOptions{
    Name:                  "conversation-42",
    Service:               "agent",
    Group:                 "support",
    MaxSessionsPerSandbox:  4,
    IdleTimeoutSeconds:    &idleSeconds,
})
if err != nil {
    return err
}
_, err = client.Suspend(ctx, s.ID)
return err
```

`ConfigFromEnv(app)` uses `MIREN_API_ADDRESS`, `MIREN_CA_CERT_PATH`, and
`MIREN_IDENTITY_TOKEN_PATH`. TLS verifies the coordinator's certificate, and
the mounted token is re-read for every request so rotation is automatic.
Redirects are never followed, including with a custom HTTP client.

The client does not grant permissions: the workload identity must be authorized
for the requested app and operation. Session mutations currently require
`app-deployer`; an ordinary read-only workload identity is insufficient. After
changing the workload role, existing sandboxes may need to be replaced to get
an updated mounted token. The client uses the mounted token rather than the
on-demand token endpoint, whose current tokens do not preserve that role.

## API and semantics

- `Create(ctx, options)` uses the deployed app's service definition. Do not
  supply a separate image, command, or environment.
- `Get(ctx, nameOrID)` inspects lifecycle and assignment state.
- `List(ctx)` returns all Sessions for the configured app, without pagination.
- `Resume(ctx, nameOrID)` and `Suspend(ctx, nameOrID)` set the desired state.
- `SetDesiredState(ctx, nameOrID, sessions.Running | sessions.Suspended)` is
  the equivalent general operation.
- `Delete(ctx, nameOrID)` removes the Session; workload cleanup may continue.

Methods accept a short name or the full `session/app/name` ID returned by
Create/List. Full IDs for other apps are rejected. One client is scoped to one
app; coordinator authorization remains the enforcement boundary.

Create defaults are a generated name, service `web`, capacity one, and a
five-minute idle timeout. A nil `IdleTimeoutSeconds` uses the default; a pointer
to zero disables idle parking. Capacity zero means omitted/use the default.
The optional group is opaque and scoped by app and service.

Successful creation or a desired-state update does **not** mean a sandbox is
ready, suspended, or cleaned up. Inspect `Phase` with Get if readiness matters.
Methods do not retry or wait for transitions. Context cancellation is preserved.

`errors.Is(err, sessions.ErrNotFound)` and `errors.Is(err, sessions.ErrConflict)`
support lookup/create workflows. Use `errors.As` with `*sessions.HTTPError` for
other statuses and the coordinator's error code/category/message. Error's
ordinary string omits the server message because it may contain sensitive data.

Persist pending work before waking its Session, and retry wakeups from that
durable intent. Mailbox processing, deduplication, ownership, and recovery policy
remain application responsibilities; a Session is not a durable message queue.

## Outside Miren

Pass `Config{URL: "https://cluster.example", App: "support-agent", Token: token}`
with an API bearer token authorized for that app. `TokenPath` can instead read
a rotating token file. Set exactly one of these fields. The URL is the
coordinator's HTTPS origin, not an `/api/v1` URL. System certificate roots are
used by default; `CACertPath` adds a private cluster CA. A custom `HTTPClient`
owns its transport and timeout configuration, but still cannot follow redirects.
