---
title: Build a durable agent with Pi
description: Add an internal Pi Durable service to your Miren app for many independent conversations, backed by the SQLite addon.
keywords: [pi, pi-durable, agent, sqlite, durable, recovery, node, example]
---

import CliCommand from '@site/src/components/CliCommand';

# Build a durable agent with Pi

[Pi Durable](https://github.com/earendil-works/pi/tree/v1.3.0/packages/durable)
is a TypeScript/JavaScript agent harness that persists conversations, model turns,
and tool work. Run it as an internal `agent` service in your Miren app. Your
application backend calls its HTTP APIs through same-app DNS; browsers continue
to talk only to your application's public `web` service.

One agent process owns one SQLite database containing many conversations, each
with its own transcript and pending work. You do not need a new app or database
for each conversation. Miren's SQLite addon provides persistent storage, and Pi
resumes unfinished work when the agent process is replaced.

```text
Browser → web service → http://agent.app.miren:8080 → SQLite
          public        internal agent service      agent only
```

The agent has an `uppercase` tool. It starts with Pi's deterministic faux model,
so you can try it without an API key, then switches to Anthropic. It deliberately
does not give the model a shell, filesystem access, or deployment credentials.

The runnable agent service is in
[`testdata/pi-durable`](https://github.com/mirendev/runtime/tree/main/testdata/pi-durable).
It contains the agent, not a public web application. The integration below adds
that service to your existing app and shows how your backend calls it.

:::warning[Pin the experimental API]
This recipe targets `@earendil-works/pi-durable`, `pi-ai`, and `chord` version
`1.3.0`. Pi Durable's API is experimental. Use the included lockfile and review
upstream release notes before upgrading; examples from upstream `main` may use
different APIs.
:::

## Prerequisites

This guide assumes you have a working Miren cluster, an authenticated `miren`
CLI targeting it, and an existing app with a backend service such as `web`.
Commands use your default cluster; add `-C <name>` to target another configured
cluster. The agent needs Node 24; the backend examples also use Node's built-in
`fetch`. An Anthropic API key is only needed for the real-model step.

Get the example source:

```bash
git clone https://github.com/mirendev/runtime.git
```

Copy `server.mjs` from `runtime/testdata/pi-durable` into your app and add the
three pinned dependencies from its `package.json` to your app's package manifest
and lockfile. Ensure your app's image contains the agent file and dependencies
and runs Node 24. The example's Dockerfile and lockfile are a reference if your
app already has its own build. Do not replace your existing web entrypoint.

## 1. Write the agent

The example contains these files:

| File | Responsibility |
| --- | --- |
| `server.mjs` | Models, tool, persistent harness, authenticated HTTP API, shutdown |
| `package.json` and `package-lock.json` | Pinned Pi dependencies |
| `Dockerfile` | Node 24 image with built-in SQLite |
| `.miren/app.toml` | Internal `agent` service and its `miren-sqlite` addon |

### Register a tool

Tools have a JSON schema, a description the model sees, and an implementation.
Install them in an extension so the same tool is available when work is recovered:

```javascript
const registry = createRegistry();
registry.install(defineExtension({
  name: "text-tools",
  tools: [defineTool({
    name: "uppercase",
    description: "Convert text to uppercase when requested.",
    parameters: Type.Object({ text: Type.String() }),
    replay: "safe",
    async execute({ text }, api, callContext) {
      await api.details({ phase: "waiting", taskId: api.taskId }, callContext);
      await delay(toolDelay, undefined, { signal: callContext.abortSignal });
      return { output: [{ type: "text", text: text.toUpperCase() }] };
    },
  })],
}));
```

The example includes an artificial delay and persisted task details so you can
observe work in progress. Respecting the cancellation signal lets graceful
shutdown interrupt the delay.

:::warning[Replay-safe does not mean exactly once]
`replay: "safe"` permits an interrupted tool to run again from its beginning.
Uppercasing is pure, so repeating it is harmless. For a payment, email, or other
external side effect, use a stable idempotency key such as `api.taskId` with a
downstream system that enforces it. A separate "already done" flag does not
close the crash window between the side effect and storing the flag. Do not
mark a non-idempotent tool safe.
:::

### Open persistent storage and resume work

```javascript
const harness = await Harness.open(
  await openNodeSqliteStorage(sqlitePath),
  { models, registry },
  context,
);
harness.resume();
```

`context` is `BACKGROUND_CONTEXT` from `@earendil-works/chord/context`.
`sqlitePath` is the required `process.env.SQLITE_PATH`, injected by Miren's
SQLite addon as `/data/data.db`. Open that exact file, not another database in
the same directory: it is the database Miren continuously backs up.
`harness.resume()` starts pending work without waiting for another HTTP request.
Reinstall your extensions on every startup.

### Create one conversation per chat

`POST /conversations` creates an independent conversation and returns its numeric
ID. The agent creates it with the model and instructions in the same durable
commit:

```javascript
const conversation = await harness.createConversation({
  ownership: { kind: "ownerless" },
  agent: {
    model,
    instructions: "You are a text assistant. " +
      "Use uppercase for uppercase requests; return its result.",
  },
}, context);
return send(res, 201, { id: conversation.id });
```

`ownerless` means an independent top-level conversation, not a child of a tool
task. It is not an authorization policy.

Save the returned ID with the chat in your application's own data store. Later
requests resolve the existing conversation instead of creating another:

```javascript
const conversation = await harness.conversation(
  conversationId, context,
);
```

No in-memory map of chat handles is needed: the same ID works after a restart.
Do not use `harness.root()` for all requests, which would put every chat into
one shared conversation. Conversation creation always creates a new chat;
persist its returned ID rather than blindly retrying creation after an ambiguous
network failure. Make your backend's chat-to-conversation mapping and its
creation/reconciliation policy durable too.

Model settings are persisted per conversation. Changing the deployment's provider
or `MODEL_ID` affects newly created conversations, not existing ones. To change
an existing conversation deliberately, call `conversation.configure(...)` from
your trusted backend once its current work is finished. Keep the providers and
credentials needed by existing conversations registered. The example always
registers faux and also registers Anthropic when its API key is present.

### Admit work before returning HTTP success

```javascript
const submission = await conversation.submit({
  type: "input",
  requestId: input.requestId,
  content: input.content,
  whenBusy: "followUp",
}, context);
return send(res, 202, { id: submission.id, conversationId: conversation.id });
```

The caller chooses a stable `requestId` for each logical request. Deduplication
is scoped to the conversation: retrying the same body and ID in the same chat
reacquires its original submission, while another chat can use that ID
independently. Do not reuse an ID for a new request in the same chat.
`whenBusy: "followUp"` queues new requests while that conversation is busy;
different conversations can run model/tool work concurrently in the same process.

The API returns after durable admission, not after inference. Poll the submission
for its status and committed answer rather than keeping a request open during
a long turn. The status endpoint checks that the submission belongs to the
conversation in the URL, returning `404` for another chat's submission.

### Close without canceling the conversation

On `SIGTERM`, the server stops admitting requests and awaits
`harness.close(context)`. Closing interrupts execution but leaves recoverable
work in storage. Do not call the conversation's abort operation as a shutdown
substitute: abort intentionally cancels work.

The full server validates request bodies, limits them to 16 KiB, requires a
bearer token on every API endpoint except `/health`, and binds to `0.0.0.0:8080`.

## 2. Add the internal service to your app

Merge the following into your app's `.miren/app.toml`, keeping your existing
`[build]`, `[services.web]`, routes, and other configuration. This assumes the
shared app image contains `server.mjs` at its working directory:

```toml
[services.agent]
command = "node server.mjs"

[services.agent.concurrency]
mode = "fixed"
num_instances = 1
shutdown_timeout = "30s"

[addons.miren-sqlite]
variant = "standard"
services = ["agent"]

[[services.web.env]]
key = "AGENT_URL"
value = "http://agent.app.miren:8080"

[[env]]
key = "AGENT_TOKEN"
required = true
sensitive = true

[[services.agent.env]]
key = "AGENT_PROVIDER"
value = "faux"

[[services.agent.env]]
key = "TOOL_DELAY_MS"
value = "1000"
```

Use your actual backend service name instead of `web` if it differs. The shared
`AGENT_TOKEN` is available to both services; never include it in browser bundles.
If your app already uses the SQLite addon, review its service attachments before
changing them: this agent must have its own database, not open your web service's
database concurrently.

The agent needs no `port`, `port_type`, public hostname, or route. It listens on
8080 by default; the backend reaches that listener directly through
[`agent.app.miren`](../traffic-routing.md#service-to-service-communication).
This DNS name works only between services in the same app, not from your laptop
or browser. Keep your existing public route pointing at `web`; do not create a
route to `agent`.

The [SQLite addon](../addons.md#sqlite-is-different) attaches its own storage;
do not add a `services.agent.disks` entry. It sets `SQLITE_PATH=/data/data.db`
and `DATABASE_URL=file:/data/data.db`. Pi's Node adapter takes a filesystem path,
so the agent uses `SQLITE_PATH`.

:::warning[Addon variables are app-wide]
`services = ["agent"]` scopes the disk mount, not the injected environment
variables. Your web service also sees the SQLite variables but has no SQLite
disk attached. It must keep using its own database configuration, not the
addon's `DATABASE_URL`. If it already relies on `DATABASE_URL`, explicitly
configure that variable for `web` and verify its value before deploying.
:::

Why these settings matter:

- Only `agent` opens the database. Your web service uses the HTTP API, not the
  SQLite file, and can scale independently.
- Fixed concurrency keeps exactly one harness process owning the database and
  keeps background work running without HTTP traffic. This is not a
  one-conversation limit.
- The SQLite addon's storage makes Miren drain the old agent instance before
  starting its replacement, at the cost of a short availability gap. Your
  backend should handle connection failures during replacement.
- The addon persists the database and WAL/SHM files across replacement and
  continuously backs up the database to the coordinator.
- The 30-second Miren shutdown budget exceeds the agent's 20-second deadline.

:::warning[SQLite backup is asynchronous and still early]
The addon replicates writes in the background; abrupt server loss can lose the
latest writes. Per-app backup authorization is not yet implemented, and a runner
that starts while the coordinator is unreachable leaves backups off until that
runner restarts. Use it for data you can recover. See the
[SQLite addon limits](../addons.md#sqlite-is-different) before relying on it.
:::

Pi's SQLite adapter uses WAL and `synchronous=NORMAL`. Process-crash recovery is
not a guarantee that the newest commits survive host power loss. Continuous
backup also does not replace testing your restore procedure.

## 3. Deploy the app

From your existing app's directory, generate a development token in an ignored
directory. Add `.secrets/` to both `.gitignore` and `.dockerignore` first:

```bash
mkdir -p .secrets
(umask 077; openssl rand -hex 32 > .secrets/agent-token)
```

Deploy the whole app, including the new internal service:

<CliCommand context="client">
```bash
miren deploy --sensitive AGENT_TOKEN=@.secrets/agent-token
miren addon list
miren app status
```
</CliCommand>

The agent service starts alongside your existing backend. No additional public
route is needed. Keep the token stable across deploys unless rotating it
deliberately; never put it in source or the image.

## 4. Call the agent from your backend

Use a server-side client like this in `web`. It reads the internal URL and shared
credential from the environment, never from browser input:

```javascript
async function agentRequest(path, body) {
  const response = await fetch(
    new URL(path, process.env.AGENT_URL), {
    method: body === undefined ? "GET" : "POST",
    headers: {
      authorization: `Bearer ${process.env.AGENT_TOKEN}`,
      "content-type": "application/json",
    },
    ...(body === undefined ? {} : { body: JSON.stringify(body) }),
    signal: AbortSignal.timeout(10000),
  });
  if (!response.ok) {
    throw new Error(`Agent request failed: ${response.status}`);
  }
  return response.json();
}
```

When your application creates a chat, create its conversation and persist the
returned ID alongside the chat's owner:

```javascript
const { id: conversationId } = await agentRequest(
  "/conversations", {},
);
// Persist conversationId with your chat and its authenticated owner.
```

On a message request, authenticate the user and load a chat they own from your
application's data store. Use that stored conversation ID, not an unchecked ID
supplied by the browser:

```javascript
const submission = await agentRequest(
  `/conversations/${conversationId}/submissions`,
  { requestId: messageId, content: messageText },
);
// Persist submission.id with the message; return your app's response to the user.
```

Here `messageId` is the stable ID for this logical message. If the HTTP response
is lost during an agent restart, retry with the same conversation ID, request ID,
and content. Do not mint a new request ID for a network retry. Conversation
creation is different: it is not idempotent, so reconcile ambiguous creation
rather than blindly issuing another create.

For a subsequent status request, check the user's ownership again, then fetch
the saved submission:

```javascript
const result = await agentRequest(
  `/conversations/${conversationId}/submissions/${submission.id}`,
);
if (result.submission.status === "done") {
  const text = result.answer.model
    .flatMap((message) => message.content)
    .filter((block) => block.type === "text")
    .map((block) => block.text)
    .join("");
  // Return text through your application's authenticated response.
}
```

Schedule another poll while work is pending; do not spin in a tight loop. Faux
mode returns `Result: <UPPERCASED INPUT>` after the tool completes. Two chats
use different conversation IDs even if they happen to use the same message ID.
To reopen a chat after a deploy, use its stored conversation ID to fetch
`/conversations/<chat>/view`.

The internal API surface is:

| Method and path | Response |
| --- | --- |
| `GET /health` | Readiness response without a bearer token |
| `POST /conversations` | `201` with a new conversation ID |
| `POST /conversations/<chat>/submissions` | `202` with submission ID and conversation ID |
| `GET /conversations/<chat>/submissions/<id>` | Status and committed answer; `404` if missing or in another chat |
| `GET /conversations/<chat>/view` | That chat's transcript and documents, including live tool details |

Missing/wrong bearer credentials return `401`. Invalid JSON or missing input
fields return `400`; oversized bodies return `413`.

:::danger[Internal access is not user authorization]
The agent trusts the backend with all conversations. Authenticate users and
check chat ownership in your backend before every submit, status, or transcript
request. Do not expose a generic public proxy to the agent, hand its token to
browsers, or let callers choose arbitrary conversation IDs. Separate transcripts
do not create a tenant security boundary. Add rate limits and spending controls
before accepting untrusted traffic.
:::

## 5. Connect a real model

Read your Anthropic API key into a restricted, ignored file:

```bash
read -rsp 'Anthropic API key: ' KEY; echo
(umask 077; printf '%s' "$KEY" > .secrets/anthropic-key)
unset KEY
```

Change the agent's provider in `.miren/app.toml`:

```toml
[[services.agent.env]]
key = "AGENT_PROVIDER"
value = "anthropic"

[[services.agent.env]]
key = "ANTHROPIC_API_KEY"
required = true
sensitive = true
```

Replace the existing `AGENT_PROVIDER` entry rather than appending another one.
Supply the key at runtime, never during the image build:

<CliCommand context="client">
```bash
miren deploy --sensitive ANTHROPIC_API_KEY=@.secrets/anthropic-key
```
</CliCommand>

The server registers `anthropicProvider()` and uses
`{ provider: "anthropic", modelId: "claude-haiku-4-5" }` for new conversations.
Set `MODEL_ID` in the agent's service environment if your account uses another
model supported by Pi 1.3.0. Create a new chat through your backend to pick up
the model; existing faux chats retain their persisted settings.

Ask the new chat to use the uppercase tool. Its `/view` includes tool results
and `docs["pi.usage"]` token/cost totals. The final prose is model-dependent;
the faux provider is deterministic and is not an LLM.

## Next steps

Replace the text tool with a narrowly scoped tool for your application's task.
Keep its replay policy honest and handle external side effects idempotently.

Keep one harness process per database, even as the number of conversations grows.
Independent chats share process resources and model-provider quotas: bound
admission, queue depth, and active work in your backend, and monitor memory,
latency, and usage. Scale your web service independently, but do not add agent
replicas against the same SQLite database. If you outgrow one agent process,
partition conversations across separately owned databases and persist the routing
in your backend. Each additional agent service must have its own storage;
cross-app internal DNS is not supported. See upstream's
[Pi Durable documentation](https://github.com/earendil-works/pi/blob/v1.3.0/packages/durable/README.md)
for extensions and durable documents.

Remove your local secret files when they are no longer needed.
