# Workload SDK

Import `miren.dev/runtime/x/workload` from the lightweight `miren.dev/runtime/x`
module. It uses only the Go standard library. The API has the same experimental,
pin-your-version contract as the rest of `x/`.

## Hosting Session loops

Create one `Host` per sandbox. Dedicated and shared hosts use the same code:

```go
host, err := workload.NewHost(workload.ConfigFromEnv())
if err != nil {
    return err
}
return host.Run(ctx, func(loopCtx context.Context, s workload.Session) (workload.StopFunc, error) {
    // Application-owned: construct an agent, queue consumer, or other loop.
    agent, err := startAgent(loopCtx, s.ID, s.Spec)
    if err != nil {
        return nil, err
    }
    return func(cleanup context.Context, reason workload.StopReason) error {
        if err := agent.Close(cleanup); err != nil {
            return err
        }
        if reason == workload.StopDeleted {
            return deleteWorkspace(cleanup, s.ID)
        }
        return nil // Preserve the workspace for another assignment.
    }, nil
})
```

`startAgent` and `deleteWorkspace` above represent your application code. The
callback must return promptly after starting its loop. Its context lives for the assignment and is
canceled when that Session is removed or the host shuts down. The cleanup
function must honor its context, tolerate retries, and return only after resources
used by in-flight work are quiesced; durable resources may be retained. Cleanup
attempts have a 30-second timeout. Failure keeps deletion or detachment
unacknowledged and admission closed for that Session; cleanup is retried.

`Session` contains the ID, app, app version, service, optional group, and resolved
service spec. `Spec` is `json.RawMessage` so the SDK does not depend on generated
runtime types. Decode the fields your application needs. It may contain secrets;
do not log the full Session or its spec. Details are supplied when the loop starts;
this is not an application-state persistence layer.

## Accepting work

Before taking a job from an external queue or accepting a task:

```go
release, err := host.Begin(ctx, sessionID)
if err != nil {
    // Do not accept/claim the job. ErrDraining means shutdown is pending;
    // ErrUnavailable means this Session is not currently accepting work.
    return err
}
defer release()
return processJob(ctx)
```

For asynchronous work, pass `release` with the task and call it when processing
finishes or the task is discarded. Count queued work, not just running work.
Do not tie asynchronous processing to an HTTP request's context; use the
assignment context supplied to your start callback.

`Begin` reports active synchronously before granting admission. If that response
advertises shutdown, it refuses the task. Reports are serialized so an earlier
idle heartbeat cannot arrive after a newer admission report. Release is
idempotent. Each Session reports active while it has outstanding work; aggregate
sandbox activity remains active while any Session has outstanding work.

The host renews activity every ten seconds and reports promptly on release. Failed
admission reports refuse work rather than assuming the runtime received them.
Transport errors, HTTP 429, and server errors are retried by the background loops;
permanent HTTP errors and start failures are returned from `Run` after cleanup.

## Shutdown and deletion

`host.Draining()` closes when shutdown is advertised, and `host.ShutdownAt()`
returns its deadline. Admission never reopens after a notice. Use the signal to
stop producers and let accepted work finish before the deadline. The runtime
still owns termination; the SDK does not extend the deadline or persist work.

Sessions created through the API or CLI default to parking after five minutes of
continuously reported idle (`miren session create --idle-timeout 5m`). Set
`--idle-timeout 0` to disable automatic parking. The controller retains the
Session identity and app configuration but releases its sandbox capacity after
cleanup. Resume with `miren session resume SESSION_ID`, or the coordinator's
Session resume API. Work arrival does not implicitly resume a parked Session:
your scheduler must resume it before offering work.

On removal, the host cancels the assignment context and calls its cleanup
function with a `StopReason`:

| Reason | Meaning |
| --- | --- |
| `StopDetached` | The Session is parked or reassigned. Preserve its durable resources. |
| `StopDeleted` | An explicit Session deletion notice was received. Final resource cleanup may run. |
| `StopRemoved` | The assignment disappeared without a deletion/detachment notice. Do not infer deletion. |
| `StopShutdown` | The host is exiting, including cancellation or a fatal error. Preserve durable resources. |

A failed cleanup retains its known reason on retries, including during host
shutdown. A later explicit deletion can supersede a pending detachment/removal.
The reason describes lifecycle intent, not proof that remote commands stopped;
your callback must quiesce or fence them before releasing ownership.

The SDK acknowledges shared Session deletion or detachment only after
successful cleanup, retrying without restarting neighbouring agents. Detachment
acknowledgments include the notice timestamp, so a late retry cannot release a
new assignment of the same Session. On process
shutdown, cancel `Run` and wait for it to return before exiting. All assignment
contexts are canceled before shutdown cleanup starts.

Stop reasons apply to assignments this host still tracks. Deleting a previously
parked Session does not recreate an agent just to invoke its cleanup callback.
An application retaining external workspaces still needs durable ownership and
deletion reconciliation for parked Sessions, host crashes, and missed notices.
The SDK supplies lifecycle intent; it does not implement provider retention or
garbage collection.

The lower-level `Client` exposes `Sessions`, `AcknowledgeDeletion`,
`AcknowledgeDetachment`, `ReportSessionActivity`, and `ReportActivity` if you need
to implement a different lifecycle. It never follows
redirects, and HTTP errors omit response bodies to avoid accidental secret logs.
