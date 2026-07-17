---
title: Changelog
description: All notable changes to Miren Runtime, organized by release version.
keywords: [changelog, releases, updates, version history]
---

# Changelog

All notable changes to Miren Runtime will be documented in this file.

## Unreleased
*main*

**Breaking Changes**
- **Env vars injected into sandboxes moved to a `MIREN_RUNTIME_` prefix** - `MIREN_APP` meant two different things: the app name injected into your running sandbox, and the CLI's target app read from your own environment. Running `miren` inside a sandbox therefore picked up the sandbox's app and silently ignored the `.miren/app.toml` in front of it. The injected variables are now namespaced and no longer collide: `MIREN_APP` → `MIREN_RUNTIME_APP`, `MIREN_VERSION` → `MIREN_RUNTIME_VERSION`, `MIREN_INSTANCE_NUM` → `MIREN_RUNTIME_INSTANCE_NUM`. If your app reads any of the old names, update it before deploying — the old names are gone, not deprecated. `MIREN_APP` as a CLI input is unchanged, so CI pipelines that set it keep working, as do the `MIREN_IDENTITY_*` workload identity variables. See [App Configuration](/app-configuration#variables-miren-injects).
- **LSVD disk volumes are no longer supported** - The legacy LSVD/NBD disk backend has been removed entirely, along with the one-time migration that converted LSVD volumes to the Universal (loop-device) format. That automatic migration shipped in **v0.6.0** and has run on first boot of every release since. If a node still holds un-migrated LSVD volume data when it reaches this release, that data can no longer be converted and the volume will not come back. Before upgrading to this version, upgrade to **at least v0.6.0** and boot it once so every LSVD volume is migrated first.

---

## v0.11.1
*2026-07-08*

**Improvements**
- **Podman support for containerized server installs** - `miren server docker install` is now `miren server container install`, working across both Docker and Podman (the old `server docker *` commands stay as deprecated aliases). Pick a runtime with `--runtime docker|podman` or let it auto-detect. Rootful Podman works end to end; rootless isn't supported for sandboxes, so the installer now stops up front with the fix rather than letting deploys crash-loop later. ([#894](https://github.com/mirendev/runtime/pull/894))
- **CLI colors adapt to your terminal background** - On light-background terminals the output was washed out and hard to read. Miren now detects light vs. dark and picks a readable palette, with `NO_COLOR`/`FORCE_COLOR` and a `MIREN_THEME` override honored. Run `miren debug colors` to see what it detected. ([#893](https://github.com/mirendev/runtime/pull/893))
- **Memory metrics for the control process** - The coordinator now reports its own memory and goroutine counts to the bundled metrics stack (under `entity="miren/control"`), so a growing control-plane heap is finally visible instead of invisible. A localhost pprof endpoint lets you grab a live heap profile during an incident. ([#892](https://github.com/mirendev/runtime/pull/892))
- **Install warnings stop nagging correctly-sized hosts** - A host provisioned at a nominal 8GB/100GB reports slightly less and used to trip the memory and disk warnings anyway. A 10% tolerance fixes it. ([#894](https://github.com/mirendev/runtime/pull/894))

**Bug Fixes**
- **Fixed an authentication bypass in the RPC control plane** ([GHSA-8fh7-7q4q-cq52](https://github.com/mirendev/runtime/security/advisories/GHSA-8fh7-7q4q-cq52)) - The RPC listener on `:8443` asked for a TLS client certificate but never checked it chained to the cluster CA, and a certificate identity gets full privileges. Anyone able to reach the port could present a forged cert and act as a cluster superuser. Certificates are now verified against the CA before they're trusted. Upgrade and restart the server to pick up the fix.

---

## v0.11.0
*2026-07-06*

**Features**
- **Version retention and image blob reclamation** - Every deploy used to mint an AppVersion that lived forever, bloating the entity store and pinning old images on disk. Miren now runs a periodic retention GC that per-app keeps the most recent `retention_count` versions (or anything newer than `retention_period`), always keeps the active version, and skips anything a running or pending sandbox still references. Reclaimed versions release their artifacts so the existing image and blob GC can finally recover the disk. ([#877](https://github.com/mirendev/runtime/pull/877))
- **Absolute time windows for `miren logs`** - `miren logs` could only ever look backward from now via `--last`. All four log subcommands (`app`, `sandbox`, `build`, `system`) now take a `--since`/`--until` pair, each of which accepts an RFC3339 timestamp, a friendly local form like `"2026-06-25 14:00"`, or a duration read as "that long ago." `--since 2h --until 30m` gives you exactly the window from two hours ago to thirty minutes ago. The old `--last` still works as shorthand. ([#868](https://github.com/mirendev/runtime/pull/868))

**Improvements**
- **`miren deploy` waits for healthy and reports the truth** - Deploy used to print "All traffic moved to new version" the instant it flipped the record active, before anything actually served a request, so a crash-looping app and a healthy one looked identical. Deploy now waits for the new version to come up healthy (sandbox RUNNING plus the network health check) before reporting, prints the crash logs when a version never comes up, and exits non-zero so CI catches it. The same wait wires into `rollback` and `env set`/`env delete`. ([#869](https://github.com/mirendev/runtime/pull/869))
- **Miren detects the port your app actually binds** - If an app ignores `$PORT` and hardcodes a different port, you used to get `port 3000 not reachable after 15s` and a killed instance with no hint why. Now when the configured port doesn't appear, Miren looks at what the app is actually listening on inside its sandbox: if it bound a single reachable port, traffic routes there with a note in the logs suggesting you set `$PORT` or `[services.web] port` to match; if it's ambiguous, the failure names exactly what was seen. ([#862](https://github.com/mirendev/runtime/pull/862))
- **Ruby version detection from `.ruby-version` and the Gemfile** - The Ruby buildpack never read any version source, defaulting to a hardcoded value and only honoring `[build] version`. It now reads `.ruby-version` first, falling back to an inline `ruby "x.y"` directive in the Gemfile, matching how the Go stack reads `go.mod`. The file is also staged before `bundle install` so a `ruby file: ".ruby-version"` Gemfile resolves cleanly. ([#858](https://github.com/mirendev/runtime/pull/858))
- **Go buildpack moved to a glibc/bookworm base** - Go was the last stack still building on Alpine/musl, which was where a whole class of prebuilt-binary and cgo compatibility problems lived. The Go buildpack now builds on `golang:<v>-bookworm` (which ships a C toolchain so cgo works) and compiles multi-stage: by default the binary is built `CGO_ENABLED=0` and ships on a tiny `distroless/static` runtime, while cgo apps get an adaptive glibc runtime stage. Full patch versions from version files now resolve directly through the pull-through cache instead of being truncated to major.minor. ([#865](https://github.com/mirendev/runtime/pull/865), [#864](https://github.com/mirendev/runtime/pull/864))
- **`miren server` docker install handles taken ports gracefully** - Installing via Docker assumed it could grab 80, 443, and 8443, and failed with a raw daemon error (after cloud registration) if something already held one. A pre-flight check now verifies each port is free and tailors its guidance to which one conflicted: a taken HTTP port can move with `--http-port`, while a taken 443 (common with `tailscale serve`, nginx, Caddy) gets pointed at `--ingress-mode behind-proxy-http` so Miren serves plain HTTP behind the existing TLS terminator. ([#867](https://github.com/mirendev/runtime/pull/867))
- **Quieter server journal** - In steady state, most of the server's journal was reconcile loops heartbeating on every tick even when nothing had changed, which made it harder to spot the lines that actually mattered. The highest-volume offenders (the sandboxpool counts line, the deployment launcher's per-pass narration, the sandbox controller's per-reconcile lines) are now gated on real work instead of firing every pass. ([#878](https://github.com/mirendev/runtime/pull/878))

**Bug Fixes**
- **Fixed a coordinator that could hang for hours on startup** - After a restart, the coordinator could park in `starting entity migration` and never bind its ports, leaving apps unreachable even though their containers kept running fine. The startup maintenance phase reads the entire entity keyspace in one unbounded request, and on an etcd that has built up a lot of MVCC history (compaction falling behind, say), that read can stall indefinitely. The maintenance phase is now bounded by a 2-minute timeout, so it fails fast and retries on the next boot instead of hanging forever, and all five full-keyspace startup scans now read in bounded pages. ([#871](https://github.com/mirendev/runtime/pull/871), [#872](https://github.com/mirendev/runtime/pull/872), [#873](https://github.com/mirendev/runtime/pull/873))
- **Fixed two sandboxes getting handed the same bridge IP** - Two live sandboxes could end up sharing one IP, causing ARP conflicts and dropped packets that surfaced to your app as flapping connections and intermittent "empty reply (no responder)" errors. A container-watchdog bug was releasing a still-running sandbox's IP lease while its container kept using the address, and later that "free" IP got handed to a second sandbox. The watchdog now releases an IP only after the container is confirmed removed, and re-fetches the sandbox directly by ID (a linearizable lookup, not a laggy index read) before treating it as orphaned. ([#856](https://github.com/mirendev/runtime/pull/856))
- **Disk leases outlived dead sandboxes** - An app with a single-writer persistent disk could get stuck for ~an hour: a sandbox would bind the disk lease, fail its port health check, get marked DEAD with the lease still held, and each replacement would fail with `disk … has an active lease` until the lease aged out ~66 minutes later, repeating the cycle. The boot-failure path now releases leases like the graceful stop path already did, and the orphan-lease sweep runs on the 5-minute periodic instead of only at startup. ([#861](https://github.com/mirendev/runtime/pull/861))
- **Remote calls over Tailscale hung on the first RPC** - `miren` calls to any cluster reached over Tailscale hung with `timeout: no recent network activity`, while on-box and public-IP calls worked fine. quic-go's default initial packet size (1280 bytes, a 1308-byte IP packet with don't-fragment set) is too big for Tailscale's 1280-MTU interface, so the handshake's opening packet was rejected by the kernel and never hit the wire. Miren now pins the QUIC `InitialPacketSize` to 1200 so the handshake fits. ([#870](https://github.com/mirendev/runtime/pull/870))
- **Fixed unbounded memory growth on a giant log line** - An app that wrote a large blob to stdout without newlines could grow the control process's memory without limit, and on a busy host that could be enough to trip the OOM killer and disrupt other apps sharing the machine. A log line only flushes on `\n`, so a multi-gigabyte payload with no newline accumulated in a single unbounded buffer before being parsed and marshaled as one entry. The partial-line buffer is now capped at 1 MiB, so memory stays bounded no matter what an app emits. ([#875](https://github.com/mirendev/runtime/pull/875))
- **Fixed a BuildKit cache that grew past its configured cap** - The build cache could climb well past its configured size limit and eat into your disk, even though a manual prune always brought it right back down, which was the tell that it was collectable and GC just wasn't doing it. The generated `buildkitd.toml` set `keepBytes` (a floor that BuildKit migrates to `reservedSpace`) but left the real ceiling, `maxUsedSpace`, at zero, and a 7-day `keepDuration` transitively pinned nearly the whole cache through recently-touched descendants. The config now sets `maxUsedSpace` and uses age-less policy tiers so GC actually enforces the cap. ([#879](https://github.com/mirendev/runtime/pull/879))
- **Fixed a flannel log-spam loop that could fill a node's disk** - On a long-running node, flannel's networking could spin into a tight retry loop that wrote several gigabytes of logs a day until the disk filled and the node dropped out of the cluster. The cause was in flannel's legacy etcd subnet-watch: it never advanced its watch start revision, so once etcd compacted past that revision every reconnect failed with "required revision has been compacted" and immediately retried. Miren now pins a `mirendev/flannel` fork that re-lists at a current revision when the watch is compacted and advances the resume revision as events arrive, so a reconnect moves forward instead of hot-looping. (The same patch is being upstreamed.) ([#874](https://github.com/mirendev/runtime/pull/874))
- **Non-Go files dropped from pure-Go images** - The Go stack's default pure-Go path assembled a `distroless-static` runtime that copied only `/bin/app`, silently dropping READMEs, templates, and data dirs, which broke apps that read files at runtime relative to `/app`. Every other stack already shipped the full tree; the pure-Go branch now copies the built `/app` tree too, excluding only Go source and module build inputs the compiled binary never needs. ([#882](https://github.com/mirendev/runtime/pull/882))
- **Auto-mounted local storage orphaned duplicate pools** - An unattended reboot took an app with auto-mounted local storage offline for ~90 minutes while a healthy sandbox sat idle in a sibling pool. Auto-mounted storage was patched into the sandbox spec at build time rather than the resolved config, which spawned a replacement pool but left the stale-pool drain's gate off, so the superseded pool lingered forever; and the activator pinned each `(version, service)` to the first pool it saw and never re-pointed it. The auto-mount is now registered as a real disk so the drain reaps it, and the activator drops a binding once its pool stops referencing the version. ([#883](https://github.com/mirendev/runtime/pull/883))
- **buildkitd reuse failed after an ungraceful miren restart** - With `KillMode=process`, a crash, double-SIGTERM, or stop that exceeded the timeout left buildkitd orphaned but still running; the next boot reused that daemon without restarting it, but its client-side session state was bound to the exited process, so new builds failed with `NotFound: no such session`. Miren now rebinds the daemon to the current process on restart. ([#885](https://github.com/mirendev/runtime/pull/885))
- **Fixed an activator crash under heavy activation load** - When a lot of apps woke from idle at once, a mutex-handling bug in the activator's pool-lookup retry loop could crash the server with `fatal error: sync: Unlock of unlocked RWMutex`, which on a single-node cluster takes the whole process down with it. A branch that noticed another goroutine had already populated the pool cache did a bare `continue`, which re-entered the inner retry loop with the lock released instead of breaking back out to the outer wait logic; the next backoff then tried to unlock an already-unlocked mutex. The loop is now labeled so that branch breaks all the way out with the lock in the expected state, and a new stress test reconstructs the race. It surfaced now because [#883](https://github.com/mirendev/runtime/pull/883)'s pool-eviction work increased pool churn enough to push traffic through this path. ([#888](https://github.com/mirendev/runtime/pull/888))
- **Fixed an indexwatch panic on watch-stream reconnect** - A watch-stream reconnect could crash the process with `panic: send on closed channel` from the indexwatch layer, because the `updates` channel had two independent senders and a `select` on `ctx.Done()` doesn't protect against a send racing the close (a closed-channel send case is always ready, and `select` picks randomly). The send and close are now serialized behind a mutex plus a `closed` flag. ([#887](https://github.com/mirendev/runtime/pull/887))

**Documentation**
- Added a [Workload Identity](https://miren.md/workload-identity) guide covering the per-cluster OIDC issuer, the token file and on-demand token server, AWS/GCP/Azure federation, and external token verification. ([#855](https://github.com/mirendev/runtime/pull/855))
- Documented [password route protection](https://miren.md/route-protect) (adding a password provider, attaching it, the login/session flow, and rotation), which had existed in the CLI but only in the auto-generated command reference. ([#866](https://github.com/mirendev/runtime/pull/866))
- Removed `post_import` from the docs. It was a documented, parsed, and validated `app.toml` key that nothing in the runtime ever read, so a deploy with `post_import = "…rake db:migrate"` reported success while the migration silently never ran. Pulling it from the docs stops advertising a step that doesn't happen. ([#884](https://github.com/mirendev/runtime/pull/884))

---

## v0.10.0
*2026-06-09*

**Features**
- **Workload identity tokens for sandboxes** - Every sandbox now receives a signed OIDC workload identity token (GitHub Actions-style) at `/var/run/miren/identity-token`, with `MIREN_IDENTITY_TOKEN_PATH`, `MIREN_OIDC_ISSUER_URL`, and `MIREN_IDENTITY_TOKEN_URL` injected into the environment. Your cluster publishes standard `/.well-known/openid-configuration` and JWKS endpoints, so external systems like AWS STS can verify tokens and federate access — no long-lived cloud credentials baked into your app. Tokens default to RS256 (universally supported by federation verifiers), auto-refresh on a background loop, and an on-demand endpoint lets a sandbox request tokens with a custom audience or TTL. Works on both embedded and distributed runners. ([#834](https://github.com/mirendev/runtime/pull/834), [#846](https://github.com/mirendev/runtime/pull/846), [#852](https://github.com/mirendev/runtime/pull/852))
- **Admin API is now GA** - The admin API graduates out of Miren Labs and is always on — no more `--labs adminapi` flag needed. Expose and call admin methods on your app over JSON-RPC; see the [admin interface docs](https://miren.md/admin-interface) for the security model, auditing, and per-language examples. ([#832](https://github.com/mirendev/runtime/pull/832))
- **Automatic TLS for cloud-provisioned cluster hostnames** - When a cluster has a cloud-provisioned `*.miren.systems` hostname, Miren now provisions a real ACME certificate for it on startup instead of serving the self-signed fallback. The hostname is pinned in the allowed-hosts set so route deletions can't strip its cert. ([#836](https://github.com/mirendev/runtime/pull/836))

**Improvements**
- **Cleaner `miren logs` output** - Structured JSON log lines from your app are now parsed at ingress: the message becomes the log body, `time`/`level` noise is stripped, and your own fields are promoted to first-class attributes. Internal bookkeeping is namespaced under `miren.*` and hidden from text output, and log brackets show the real short ID (e.g. `[CBZ]`) instead of a truncated entity key. Existing `--service` / `sandbox` filters keep working against both old and new entries. ([#838](https://github.com/mirendev/runtime/pull/838))
- **`logs -f` collapses repeated lines** - When following logs, consecutive lines that differ only by their timestamp (e.g. a once-a-second health ping) now collapse into a single live-updated `[ Repeated 14x over 14s ]` summary instead of burying new output. Only engages for interactive text follow — JSON, piped, and non-follow output stay verbatim, so `grep` and machine consumers are unaffected. ([#845](https://github.com/mirendev/runtime/pull/845))

**Bug Fixes**
- **Fixed TLS failures reaching recreated distributed runners** - `miren app run` and `miren sandbox exec` against a distributed runner could fail with `certificate is valid for ... not <ip>` after the runner VM was recreated with a new internal IP but a persisted certificate. Runners now detect a stale certificate on start and re-issue it from the coordinator, self-healing on the next restart. ([#848](https://github.com/mirendev/runtime/pull/848))

**Documentation**
- Expanded the [admin interface](https://miren.md/admin-interface) page with the full security model, auditing behavior, JSON-RPC shape, and CLI usage. ([#831](https://github.com/mirendev/runtime/pull/831))
- Expanded the [terminology](https://miren.md/terminology) page from 12 to 35 canonical definitions. ([#850](https://github.com/mirendev/runtime/pull/850))

---

## v0.9.1
*2026-06-04*

**Bug Fixes**
- **New services could come up without a network address** - If the cluster hit a brief internal hiccup (etcd compaction, a leader change, or a network blip), a service created right around that moment could be left without an IP and stay unreachable until the server was restarted. If you've seen a freshly deployed service that never became reachable for no obvious reason, this was a likely cause. ([#841](https://github.com/mirendev/runtime/pull/841))
- **Ephemeral preview deploys would stop responding until a server restart** - A preview (ephemeral) deploy would work at first, then stop responding to requests, and never serve again until the miren server itself was restarted. That situation is now fixed; these deploys keep serving and recover on their own. ([#837](https://github.com/mirendev/runtime/pull/837))
- **NodePort services accumulated duplicate firewall rules** - NodePort services piled up duplicate iptables rules over time, and HTTP services that declared a `node_port` didn't always get one. Both are fixed, and existing duplicate rules are cleaned up automatically as sandboxes recycle. ([#840](https://github.com/mirendev/runtime/pull/840))

---

## v0.9.0
*2026-05-28*

**Breaking Changes**
- **`auth provider add` reshaped into per-type subcommands** - The asymmetric pair of `auth provider add NAME --provider-url ...` (OIDC) and the separate `auth provider add-password NAME ...` is gone, replaced by a single shape: `auth provider add oidc|github|password NAME [flags]`. Migration: prepend `oidc` to existing OIDC commands, and replace `add-password` with `add password` (with a space). The CLI now exposes three types directly instead of an "oidc with optional connector" indirection. ([#817](https://github.com/mirendev/runtime/pull/817))

**Features**
- **Route protection: native GitHub identity provider** - GitHub was the awkward gap in the v0.8.0 route protection story. It has no OIDC endpoint, so the answer was "stand up Dex yourself." Miren now talks to GitHub directly via an embedded Dex connector library, and adding a provider is just `miren auth provider add github my-gh --client-id $ID --client-secret $SECRET --org mirendev:platform,eng`. Org and team membership land in your app as `X-User-Login` and `X-User-Groups` headers. ([#817](https://github.com/mirendev/runtime/pull/817))

**Improvements**
- **`miren doctor server` checks QUIC reachability** - The endpoint probe now exercises QUIC alongside HTTPS/HTTP. A host firewall that allows TCP 8443 but blocks UDP 8443 (a common UFW misconfiguration) used to silently break every external `miren deploy` with all-green doctor output; the new probe surfaces it with a pointed "host firewall may be blocking inbound UDP" message. ([#819](https://github.com/mirendev/runtime/pull/819))
- **`miren server register` restarts the systemd unit for you** - Re-registering against a live cluster used to print a "you must now restart miren server" warning that was easy to miss. The CLI now detects an active `miren.service` and restarts it itself, while fresh `miren install` flows stay quiet because install owns the lifecycle. ([#824](https://github.com/mirendev/runtime/pull/824))
- **Better admin CLI per-method help** - `miren admin <method> --help` (or `-h`) now shows method help instead of erroring with `unknown parameter(s): help`. Calling a method that declares params with no args renders the help block instead of a raw JSON-RPC error. Missing-required and unknown-param errors embed the full method definition so you can see what was expected at the point of failure. ([#823](https://github.com/mirendev/runtime/pull/823))

**Bug Fixes**
- **Fixed `sandbox exec` stdout redirection and short-ID lookup** - `miren sandbox exec -i app cat /data/db > backup.db` used to print the database contents to the terminal because the TTY check only looked at stdin; redirected stdout now stays binary-clean and skips PTY allocation. The same command also now resolves short IDs like `7g7` (as displayed by `sandbox list`) instead of erroring with "no container found". ([#828](https://github.com/mirendev/runtime/pull/828))
- **Fixed deploy panic leaving the server lock stuck for 30 minutes** - An explain-mode deploy could race RPC stream-handler goroutines against the main goroutine closing the status channel, panicking the deploy and leaving the server-side deploy lock held until its TTL. The status channel is now serialized behind a mutex, and a panic-recovery guard releases the lock immediately on crash. ([#822](https://github.com/mirendev/runtime/pull/822))
- **Fixed ephemeral preview deploys scaling under load** - Ephemeral pools followed normal auto-mode scaling, so traffic bursts ratcheted the pool up instead of queuing on the single preview sandbox. Ephemeral pools are now pinned at `desired_instances = 1` and refuse to scale. The same PR fixes an activator race that returned leases with empty URLs (causing httpingress to fail with `unsupported protocol scheme ""`) and bumps the per-request wait cap from 50s to 120s to cover cold image pulls. ([#821](https://github.com/mirendev/runtime/pull/821))
- **Fixed `tls.additional_names` / `tls.additional_ips` rejected in `behind-proxy-http` mode** - The validator refused these fields under `behind-proxy-http`, but they also feed the API server and etcd certs, which exist regardless of ingress mode. Operators had no way to set just the API cert SANs, which broke external `miren deploy` with `leaf cert SAN doesn't match`. ([#820](https://github.com/mirendev/runtime/pull/820))

---

## v0.8.0
*2026-05-20*

**Breaking Changes**
- **Ingress configuration reshaped around named modes** - The ingress and TLS configuration is now organized as three explicit modes — `tls-autoprovision` (default, behavior unchanged), `behind-proxy-http`, and `behind-proxy-https` — with an optional `ingress.address` for custom bind addresses. The legacy `tls.standard_tls` knob is retired (dev environments already used `--self-signed-tls`). One small behavior change in autoprovision mode: requests to raw-IP or localhost Hosts on `:80` now follow the HTTPS redirect like any other request instead of being shortcut to the default route over plain HTTP. Pick `behind-proxy-http` if you want explicit plain-HTTP for a dev workflow. ([#799](https://github.com/mirendev/runtime/pull/799))

**Features**
- **Route protection: shared-password auth** - Protect any route behind a shared password. Run `miren auth provider add-password my-gate`, then `miren route protect blog.example.com --provider my-gate`, and your route gets a login form with a 24h encrypted session cookie on success. Good for staging gates, internal dashboards, and anywhere "the half-dozen people who know the password" is enough. ([#787](https://github.com/mirendev/runtime/pull/787))
- **Route protection: OIDC single sign-on** - Plug your identity provider into a Miren route and the authenticated user's identity arrives at your app as plain HTTP headers like `X-User-Email`. No OAuth library, JWT validation, or callback handler needed in your code. Google, GitLab, and self-hosted Keycloak work directly; GitHub via a Dex federation layer. ([#764](https://github.com/mirendev/runtime/pull/764), [#788](https://github.com/mirendev/runtime/pull/788))
- **Route protection: Web Application Firewall** - Inspect requests for attack payloads before they reach your app. `miren route waf <host> --level N` runs Coraza with the full OWASP Core Rule Set in front of any route, blocking SQL injection, XSS, path traversal, command injection, and friends. Levels 1-4 map to OWASP paranoia levels; level 1 is the right starting point for most apps. When both auth and WAF are on a route, WAF runs first. See the [route protection docs](https://miren.md/route-protect) and the [announcement post](https://miren.dev/blog/route-protection). ([#786](https://github.com/mirendev/runtime/pull/786))
- **Ephemeral deployments** - Deploy a labeled, time-boxed version of your app that lives alongside the active version and is reachable at `<label>.<your-route>` without touching production traffic. `miren deploy --ephemeral pr-33 --ttl 24h` creates one; `miren app versions` shows what's live, what's ephemeral, and when each expires. Expired versions are cleaned up automatically, and TLS certs provision on first hit so ephemeral subdomains get real certificates instead of the cluster's self-signed fallback. ([#745](https://github.com/mirendev/runtime/pull/745), [#807](https://github.com/mirendev/runtime/pull/807))
- **Smarter `miren init`** - `miren init` now scans your project for the environment variables your app actually needs and pre-sets them on the app before the first deploy, the same as if you'd run `miren config set` yourself. Generated secrets (Rails `SECRET_KEY_BASE`), file-backed keys (`RAILS_MASTER_KEY`), framework defaults, and source-detected reads across Python/Node/Bun/Go/Ruby/Rust are recognized; the ones we can resolve are picked up automatically by the first build. See [What `miren init` Does for You](/app-configuration#what-miren-init-does-for-you). ([#567](https://github.com/mirendev/runtime/pull/567))

**Improvements**
- **Automatic npm/bun installation for mixed-stack apps** - Rails, Django, Go, or Rust apps that ship a `package.json` (or `bun.lock`/`bun.lockb`) now get npm or bun installed onto the base image automatically and `npm install`/`bun install` run as the unprivileged `app` user. Apps that vendor their own `node_modules` still skip the install step. ([#810](https://github.com/mirendev/runtime/pull/810))
- **Per-service `port_timeout` in app.toml** - Services that need longer than the default 15s to bind their port (Prisma migrations on first boot are a classic culprit) can now declare `port_timeout = "120s"` in their service block. Web and worker services in the same app can hold different timeouts so a slow-booting worker doesn't make web fail-fast change too. ([#792](https://github.com/mirendev/runtime/pull/792))
- **Sandbox hostname in `/etc/hosts`** - Each sandbox now gets a real IP→hostname entry in `/etc/hosts` so processes that resolve their own hostname (e.g. Erlang's EPMD) see their sandbox IP instead of loopback. ([#784](https://github.com/mirendev/runtime/pull/784))
- **CLI top-level help grouped by user intent** - `miren --help` now organizes its 30+ top-level commands into five named buckets (Getting started, Monitoring your app, Configuring your app, Client operations, Server operations) so deployers don't have to scan past server-administration commands to find `deploy` and `rollback`. ([#812](https://github.com/mirendev/runtime/pull/812))
- **Honest upload progress and ETA in `miren deploy`** - The "total" in the upload progress line is no longer fictional and the line now shows a time-domain ETA (`eta ~8h 15m`) instead of a fluctuating projected total. ([#804](https://github.com/mirendev/runtime/pull/804))
- **`sandbox exec` accepts a positional sandbox ID** - `miren sandbox exec yKm -- hostname` now works the way you'd expect from copying the ID out of `sandbox list`. ([#800](https://github.com/mirendev/runtime/pull/800))
- **Trustworthy public-address advertising** - The coordinator now trusts per-family netcheck results so an IPv4 success doesn't drop IPv6 reachability, and CGNAT/Tailscale addresses in `100.64.0.0/10` are filtered out of discovered IPs by default. Operators who actually want a CGNAT address advertised can set it explicitly via `AdditionalIPs`. ([#808](https://github.com/mirendev/runtime/pull/808), [#809](https://github.com/mirendev/runtime/pull/809))
- **Better `.gitignore` handling in source uploads** - Switched the source-bundler to go-git's gitignore matcher, which correctly handles negations like Bridgetown's default `!/tmp/pids/` pattern. Vanilla `bridgetown new` apps now deploy without a missing-pidfile crash. ([#777](https://github.com/mirendev/runtime/pull/777))

**Bug Fixes**
- **Fixed containerd FIFO directory leak on sandbox teardown** - Sandbox cleanup paths now attach a non-nil `cio.NewAttach()` when fetching tasks for deletion so containerd actually removes its FIFO directories. The leak was exhausting `/run`'s inode budget on long-lived hosts. ([#797](https://github.com/mirendev/runtime/pull/797))
- **Fixed `miren upgrade --check --channel` ignoring `--channel`** - The version/channel resolution happened after the `--check` early return, so `miren upgrade --check --channel main` was silently reporting against `latest`. Resolution is now shared across `miren upgrade`, `server upgrade`, and `runner upgrade` so they can't drift apart again. ([#798](https://github.com/mirendev/runtime/pull/798))
- **Fixed `miren login` cancel being reported as a timeout** - Hitting Ctrl-C during login now distinguishes cancellation from `context.DeadlineExceeded` and reports it accordingly. ([#773](https://github.com/mirendev/runtime/pull/773))

---

## v0.7.1
*2026-04-22*

**Improvements**
- **Cut steady-state log noise** - Roughly 76% reduction in server log volume during normal operation. Removed per-request auth success logs, redundant controller reconcile lines, heartbeat spam, and other content-free chatter that drowned out useful signal. ([#767](https://github.com/mirendev/runtime/pull/767))

**Bug Fixes**
- **Fixed Valkey addon causing app connection errors** - The valkey sandbox was starting without `--requirepass` while apps received a URL containing the password, so every client got `ERR AUTH called without any password configured`. Password is now baked directly into the server command at provision time. ([#771](https://github.com/mirendev/runtime/pull/771))
- **Fixed short IDs not resolving for several commands** - `sandbox exec`, `logs sandbox`, and `deploy cancel` weren't resolving short IDs like you'd expect from seeing them in other output. Worst case was `deploy cancel` silently no-op'ing while reporting success, leaving the deploy lock stuck until its 30-minute timeout. ([#769](https://github.com/mirendev/runtime/pull/769))
- **Fixed Rust source-only deploys shipping stale binaries** - Changing only source files in a Rust app could silently ship the previously built binary. The workspace crate now force-rebuilds on every deploy while the dependency cache stays intact. ([#768](https://github.com/mirendev/runtime/pull/768))
- **Fixed activator fail-fast killing healthy replacement deploys** - When a crash-looping version was replaced with a fix, the activator counted dead sandboxes from the broken version and fail-fasted the fix before it got to boot a sandbox of its own. Fail-fast accounting is now scoped to the requesting version. ([#766](https://github.com/mirendev/runtime/pull/766))
- **Fixed silent app.toml parse errors during build** - A parse error in `app.toml` used to be logged and discarded, and the build continued as if no config existed — services, env vars, and addons silently gone. Parse errors now fail the build and surface the full diagnostic (file, line, suggestions) to the client. ([#765](https://github.com/mirendev/runtime/pull/765))

---

## v0.7.0
*2026-04-14*

**Breaking Changes**
- **Local shared storage is now opt-in** - Apps that need persistent local storage now declare it explicitly as a disk with `provider = "local"` in app.toml instead of getting an automatic bind mount at `/miren/data/local`. This makes storage dependencies visible to the scheduler, which needs to know about them as we build out multi-node cluster support. If your app has existing data in local storage, Miren detects it automatically, keeps mounting it, and shows a warning during deploy with a link to add the config. No data loss, just a nudge to make the dependency explicit at your own pace. See [Migrating from automatic local storage](https://miren.md/disks#migrating-from-automatic-local-storage) for details. ([#700](https://github.com/mirendev/runtime/pull/700), [#719](https://github.com/mirendev/runtime/pull/719))

**Features**
- **Managed addons** - Miren now provisions and manages backing services alongside your apps. Add a database or cache with `miren addon create`, and Miren handles the container lifecycle, networking, and credential injection. Launch includes PostgreSQL, MySQL, Valkey, Memcache, and RabbitMQ, with version selection and custom OCI image support. ([#688](https://github.com/mirendev/runtime/pull/688), [#706](https://github.com/mirendev/runtime/pull/706), [#720](https://github.com/mirendev/runtime/pull/720), [#726](https://github.com/mirendev/runtime/pull/726), [#727](https://github.com/mirendev/runtime/pull/727), [#743](https://github.com/mirendev/runtime/pull/743), [#755](https://github.com/mirendev/runtime/pull/755), [#758](https://github.com/mirendev/runtime/pull/758), [#760](https://github.com/mirendev/runtime/pull/760))
- **Short entity IDs** - Entities now get short 3-8 character identifiers that work anywhere a full ID does. CLI tables are easier to read and IDs are easy to type from memory. ([#696](https://github.com/mirendev/runtime/pull/696), [#721](https://github.com/mirendev/runtime/pull/721), [#741](https://github.com/mirendev/runtime/pull/741))
- **CLI aliases** - Define custom command shortcuts in `.miren/app.toml` under `[aliases]`. Supports multi-word names and appends extra arguments. ([#693](https://github.com/mirendev/runtime/pull/693))
- **Disk undelete** - Deleted disks are now soft-deleted with a 7-day retention window. `disk undelete` restores them, and `disk list-deleted` shows what's recoverable. ([#694](https://github.com/mirendev/runtime/pull/694))
- **`miren app restart`** - New command to restart an app immediately. Deploys also now reset crash cooldown, so a new deploy isn't blocked by stale backoff from a previous crash. ([#702](https://github.com/mirendev/runtime/pull/702))

**Improvements**
- **Better config error messages** - Typos and validation errors in `.miren/app.toml` now show file location, underline the problem, and suggest corrections with color output. ([#709](https://github.com/mirendev/runtime/pull/709))
- **Faster `app list`** - App list aggregation moved server-side, significantly reducing round trips for clusters with many apps. ([#701](https://github.com/mirendev/runtime/pull/701))
- **Disk-aware pool draining** - Services with disks now drain old pools before starting new ones, preventing data conflicts during deploys. ([#725](https://github.com/mirendev/runtime/pull/725))
- **Hardened embedded etcd** - Defenses against bbolt freelist bloat that could cause etcd to slow down over time. ([#733](https://github.com/mirendev/runtime/pull/733))
- **App name in sandbox list** - `sandbox list` now shows which app each sandbox belongs to. ([#757](https://github.com/mirendev/runtime/pull/757))
- **Dual-stack IP discovery** - Cluster address reporting now discovers both IPv4 and IPv6 public addresses, and TLS certificates include public IP SANs automatically. ([#708](https://github.com/mirendev/runtime/pull/708), [#712](https://github.com/mirendev/runtime/pull/712))

**Bug Fixes**
- **Fixed deploy from subdirectory using wrong source directory** ([#716](https://github.com/mirendev/runtime/pull/716))
- **Friendlier TLS certificate behavior when DNS isn't ready yet** - ACME challenges now back off gracefully instead of burning through rate limits when a domain's DNS isn't fully propagated. ([#710](https://github.com/mirendev/runtime/pull/710), [#730](https://github.com/mirendev/runtime/pull/730))
- **Fixed overlay IP allocator assigning duplicate IPs after restart** ([#707](https://github.com/mirendev/runtime/pull/707))
- **Fixed sandbox hostname resolution for addon sub-containers** ([#728](https://github.com/mirendev/runtime/pull/728))
- **Fixed stale pool reuse when volume/mount config changes** ([#718](https://github.com/mirendev/runtime/pull/718))
- **Self-healing loop-backed volumes after unclean shutdown** - After a SIGKILL, miren now detects and cleans up stale loop devices instead of mounting a second one and corrupting the filesystem. ([#756](https://github.com/mirendev/runtime/pull/756))
- **Fixed release bundle detection for CLI-only installs** ([#759](https://github.com/mirendev/runtime/pull/759))

---

## v0.6.1
*2026-03-24*

**Improvements**
- **Faster system log queries** - `miren logs system` now returns results in under a second instead of ~10 seconds by using VictoriaLogs' native `limit` parameter instead of server-side sorting. ([#681](https://github.com/mirendev/runtime/pull/681))
- **JSON output for more CLI commands** - `debug netdb list`, `debug netdb status`, and `doctor config` now support `--format json`. Added `--json` as a shorthand for `--format json` on all commands that support it. ([#687](https://github.com/mirendev/runtime/pull/687))

**Bug Fixes**
- **Fixed delta deploys failing when cached and changed files share directories** - Deploying after a cached delta could fail with `mkdir: file exists` when both the cached and changed file sets contained the same directory. ([#689](https://github.com/mirendev/runtime/pull/689))
- **Fixed CLI commands ignoring per-app cluster selection** - Commands like `route list`, `sandbox list`, and `app list` now respect the cluster chosen via `cluster switch` in an app directory, instead of silently falling back to the global active cluster. ([#683](https://github.com/mirendev/runtime/pull/683))

**Documentation**
- Added a top-level Deployment docs page covering the full deploy lifecycle. ([#684](https://github.com/mirendev/runtime/pull/684))

---

## v0.6.0
*2026-03-17*

**Features**
- **Disk storage system overhaul** - The disk subsystem has been rewritten with a new Universal mode backed by loop devices, replacing the previous LSVD/NBD architecture. Existing LSVD volumes are migrated automatically on first boot. Includes disk backup/restore support and a new cloud accelerator mode for segment upload/replay. ([#639](https://github.com/mirendev/runtime/pull/639))
- **System logs accessible from the CLI** - `miren logs system` queries server logs directly from the CLI — same interface as app logs, with `--follow`, `--last`, and `--format json` all working. Under the hood, server logs are ingested into VictoriaLogs through a structured handler, so all attributes (module, level, error fields) are preserved and queryable. `miren logs` is also restructured into proper subcommands (`app`, `sandbox`, `build`, `system`) — bare `miren logs` still works as shorthand for `miren logs app`. ([#645](https://github.com/mirendev/runtime/pull/645), [#662](https://github.com/mirendev/runtime/pull/662), [#679](https://github.com/mirendev/runtime/pull/679))
- **Delta file transfer for deploys** - The deploy client now sends a file manifest first; the server compares it against the cached source from the previous deploy and the client only uploads what changed. Subsequent deploys of unchanged code upload nothing. ([#670](https://github.com/mirendev/runtime/pull/670))
- **Wildcard routes** - Route all subdomains of a domain to a single app with `miren route set *.example.com myapp`. Wildcard routes match any subdomain and the bare domain, with exact routes taking priority. TLS certificates are provisioned automatically for each matching subdomain. ([#659](https://github.com/mirendev/runtime/pull/659))

**Improvements**
- **Upload progress bar and caching summary** - Deploy uploads show a real progress bar with percentage and speed. After upload, a summary shows how many files were cached from the previous deploy and estimated time saved. ([#680](https://github.com/mirendev/runtime/pull/680))
- **Eager TLS certificate provisioning** - Adding a route now triggers certificate provisioning immediately in the background. HTTPS connections no longer stall 30–90 seconds on first access. ([#661](https://github.com/mirendev/runtime/pull/661), [#664](https://github.com/mirendev/runtime/pull/664))
- **Deploy progress tracks build steps** - The progress bar now tracks build steps instead of layer downloads, so it stays active on cached-image deploys. ([#665](https://github.com/mirendev/runtime/pull/665))
- **JSON output for log and app commands** - `miren logs` (all subcommands), `miren app`, `miren app status`, and `miren app history` all support `--format json` for scripting and automation. ([#675](https://github.com/mirendev/runtime/pull/675), [#673](https://github.com/mirendev/runtime/pull/673))
- **Group help for CLI discovery** - `miren help --commands` lists all commands with synopses (supports `--format json`). `miren help app.list version sandbox.stop` shows help for multiple commands at once using dot notation. ([#676](https://github.com/mirendev/runtime/pull/676))
- **System requirements check at install** - The installer now verifies at least 4 GB memory and 50 GB storage before proceeding. ([#663](https://github.com/mirendev/runtime/pull/663))

**Bug Fixes**
- **Fixed rapid deploys causing orphaned sandboxes** - Deploying the same app multiple times in quick succession no longer leaves orphaned sandboxes running or stale routing entries. ([#672](https://github.com/mirendev/runtime/pull/672))
- **Fixed disk config silent failures** - Using `size` instead of `size_gb` in disk config no longer leaves sandboxes stuck in PENDING forever; unknown fields now surface as errors. ([#666](https://github.com/mirendev/runtime/pull/666))
- **Fixed app TUI showing wrong concurrency** - The `miren app` details view now correctly shows fixed instance counts instead of "auto" for services with explicit concurrency settings. ([#667](https://github.com/mirendev/runtime/pull/667))
- **Fixed env var file values including trailing newlines** - `KEY=@filepath` now trims trailing line endings from file contents. ([#671](https://github.com/mirendev/runtime/pull/671))

---

## v0.5.0
*2026-03-10*

**Features**
- **Multi-port and non-HTTP services** - Apps can now expose multiple ports with different protocols (TCP, UDP, HTTP) using `[[services.<name>.ports]]` in app.toml. Node ports are validated at deploy time to prevent conflicts. Enables use cases like IRC servers alongside HTTP endpoints. ([#641](https://github.com/mirendev/runtime/pull/641))
- **CI deployments with GitHub Actions** - Deploy from GitHub Actions and other CI systems using short-lived identity tokens instead of long-lived secrets. Bind repos to apps with `miren auth ci add --github OWNER/REPO -a APP`, and GitHub Actions will authenticate automatically. Target clusters from CI with the `MIREN_CLUSTER` env var. ([#631](https://github.com/mirendev/runtime/pull/631), [#633](https://github.com/mirendev/runtime/pull/633), [#635](https://github.com/mirendev/runtime/pull/635))
- **Required environment variables** - Declare env vars as `required = true` in app.toml and deploys will fail early with a clear message listing what's missing. Vars can also be marked `sensitive` and given a `description` that appears in `miren env list`. ([#638](https://github.com/mirendev/runtime/pull/638))

**Improvements**
- **Zero-config app initialization** - Running any app command without an `app.toml` now offers to run `miren init` for you interactively, inferring the app name from your directory. In CI, the error message tells you what to do. ([#654](https://github.com/mirendev/runtime/pull/654))
- **Bun runtime detection without lockfile** - Bun apps without dependencies are now detected via `bunfig.toml`, the `packageManager` field in package.json, or `bun`/`bunx` commands in package.json scripts. ([#656](https://github.com/mirendev/runtime/pull/656))
- **`env set`/`env delete` show deployment progress** - Environment variable changes now go through the deployment service and show activation progress and routes, matching the UX of `deploy` and `rollback`. ([#630](https://github.com/mirendev/runtime/pull/630))

**Bug Fixes**
- **Fixed 502 errors during deployment rollovers** - The HTTP ingress now retries on a stale connection instead of immediately returning 502, and the launcher waits for new sandboxes to be ready before scaling down old ones. ([#637](https://github.com/mirendev/runtime/pull/637))
- **Fixed streaming responses being buffered** - SSE, chunked downloads, and long-polling now work correctly. The previous timeout implementation buffered entire responses in memory before sending; timeouts are now handled at the transport level so data streams incrementally. ([#634](https://github.com/mirendev/runtime/pull/634))
- **Fixed `miren exec` panic after command finishes** - `miren exec` and `miren app run` no longer panic when the remote command completes. ([#655](https://github.com/mirendev/runtime/pull/655))
- **Fixed `app run` panic when stdin isn't a terminal** - `miren app run` no longer panics in non-interactive contexts like piped input or CI. ([#632](https://github.com/mirendev/runtime/pull/632))
- **Fixed firewall rules leaking on sandbox teardown** - Port forwarding rules are now cleaned up when sandboxes stop, preventing stale rules from making node ports unreachable after redeployment. ([#644](https://github.com/mirendev/runtime/pull/644))

---

## v0.4.1
*2026-02-18*

**Bug Fixes**

- **Fixed app startup directory regression** - Apps deployed before the WORKDIR fix (v0.4.0) would boot with CWD `/` instead of `/app`, causing crashes. The `/app` default is now restored as a fallback for existing app versions without a stored `start_directory`. ([#621](https://github.com/mirendev/runtime/pull/621), [#623](https://github.com/mirendev/runtime/pull/623))
- **Fixed noisy RPC error logs** - Deploying to a server without telemetry capabilities no longer produces a spurious `[ERROR] error resolving capability` message. Optional capability lookups now degrade quietly at Debug level. ([#622](https://github.com/mirendev/runtime/pull/622))

---

## v0.4.0
*2026-02-17*

**Features**

- **First-class rollbacks** - Redeploy a previous app version without rebuilding. Use `miren rollback` for an interactive picker that shows your recent versions, or `miren deploy --version <id>` to deploy a specific version directly. Each rollback creates a full deployment record with provenance tracking. ([#590](https://github.com/mirendev/runtime/pull/590))

- **OpenTelemetry tracing** - Comprehensive distributed tracing across the Miren runtime. Traces cover HTTP ingress, deploy/build pipelines, containerd operations, and CLI commands. CLI spans are shipped through the server via a proxy exporter. Cluster identity is included in trace resource attributes. See [Observability](/observability) for configuration details. ([#595](https://github.com/mirendev/runtime/pull/595), [#601](https://github.com/mirendev/runtime/pull/601), [#602](https://github.com/mirendev/runtime/pull/602), [#609](https://github.com/mirendev/runtime/pull/609))

**Improvements**

- **Per-app cluster pinning** - The CLI now remembers which cluster to use for each app. The first time you use `-C <cluster>` with a command, that cluster is saved locally for the app. Resolution order: explicit `-C` flag > per-app pin > global active cluster. Use `miren cluster current` to see which cluster the CLI will target. ([#596](https://github.com/mirendev/runtime/pull/596))
- **Better app.toml error messages** - Invalid `app.toml` files now surface the actual parse error instead of the confusing "app is required" message. ([#614](https://github.com/mirendev/runtime/pull/614))
- **Exclude dead sandboxes from listing** - `miren sandbox list` now hides dead sandboxes by default, showing only active ones. Use `--all` to see everything. ([#585](https://github.com/mirendev/runtime/pull/585))
- **Redact secrets from debug bundles** - `miren debug bundle` now redacts sensitive information and includes guidance on sharing bundles safely. ([#612](https://github.com/mirendev/runtime/pull/612))
- **`-v` flag moved to `-V`** - The version flag is now `-V`/`--version`, freeing `-v` for `--verbose` across all commands. ([#593](https://github.com/mirendev/runtime/pull/593), [#594](https://github.com/mirendev/runtime/pull/594))
- **Disk mount robustness** - Improved disk mount lifecycle handling with better error recovery and integration test coverage. ([#597](https://github.com/mirendev/runtime/pull/597))

**Bug Fixes**

- **Fixed Dockerfile WORKDIR ignored** - App containers now honor the `WORKDIR` directive from your Dockerfile instead of always defaulting to `/`. ([#617](https://github.com/mirendev/runtime/pull/617))
- **Fixed nested .gitignore files ignored in deploy** - Deploy tarballs now respect `.gitignore` files in subdirectories, not just the root. This prevents things like `web/node_modules/` from inflating your deploy upload. ([#608](https://github.com/mirendev/runtime/pull/608))
- **Fixed disk space GC** - Garbage collection for BuildKit cache and registry blobs now works correctly, preventing disk space from being consumed by stale build artifacts. ([#588](https://github.com/mirendev/runtime/pull/588))
- **Fixed activator pool cache lockout** - The activator pool cache no longer permanently locks out at MaxPoolSize, which was preventing scale-to-zero apps from recovering after hitting the pool size limit. ([#616](https://github.com/mirendev/runtime/pull/616))
- **Fixed disk lease leak in sandbox cleanup** - Periodic sandbox cleanup now properly releases disk leases before deleting sandboxes. ([#613](https://github.com/mirendev/runtime/pull/613))
- **Fixed empty host in route commands** - `miren route set` and `miren route show` now validate that a host is provided instead of accepting empty strings. ([#591](https://github.com/mirendev/runtime/pull/591))
- **Fixed deployment cluster filter** - Deployments are now correctly filtered by cluster, and `miren app history` defaults are improved. ([#589](https://github.com/mirendev/runtime/pull/589))

---

## v0.3.1
*2026-02-06*

**Features**

- **Build-time environment variables** - Environment variables are now available during the build process, so build commands like `npm run build` can access API keys, database URLs, and other configuration. Variables from `app.toml`, existing config, and `--env`/`--secret` CLI flags are all injected at build time. ([#581](https://github.com/mirendev/runtime/pull/581))

**Improvements**

- **Preserve disk mounts during server restart** - The LSVD disk manager now survives server restarts, keeping disk mounts active. Use `systemctl reload miren` for a soft restart that preserves mounts, or `systemctl restart miren` for a full restart. This significantly reduces disruption when updating the miren server. ([#554](https://github.com/mirendev/runtime/pull/554))

  **For existing installations:** To enable this feature, either re-run `sudo miren server install --force` to regenerate the systemd unit file, or manually add the following line to `/etc/systemd/system/miren.service` under the `[Service]` section and run `systemctl daemon-reload`:
  ```
  ExecReload=/bin/kill -USR1 $MAINPID
  ```

- **Batch env var setting** - Setting multiple environment variables now creates a single app version instead of N intermediate versions. ([#578](https://github.com/mirendev/runtime/pull/578))
- **Smarter deploy coalescing** - Rapid successive deploys are now coalesced so only the latest version is launched, avoiding unnecessary churn from intermediate sandbox pools. ([#579](https://github.com/mirendev/runtime/pull/579))
- **Default app name from app.toml** - CLI commands like `app delete`, `route set`, and `route set-default` now infer the app name from `.miren/app.toml` when not specified. ([#562](https://github.com/mirendev/runtime/pull/562))
- **Clearer scaling display** - Instance counts now show `(auto)` or `(fixed)` suffix, and sandbox pool listings include a MODE column. ([#566](https://github.com/mirendev/runtime/pull/566))
- **Deploy validation for services** - Deploys now fail early with a clear error when no services are defined, instead of silently deploying an unservable app. ([#563](https://github.com/mirendev/runtime/pull/563))
- **Smarter `miren upgrade`** - Upgrade now checks permissions upfront, offers interactive sudo vs user-directory install picker, and supports a `--user` flag for non-root installation. ([#564](https://github.com/mirendev/runtime/pull/564))

**Bug Fixes**

- **Fixed build log retention** - Build logs are now properly persisted when using the central BuildKit daemon, restoring the ability to retrieve build output after a deploy with `miren logs --build`. ([#561](https://github.com/mirendev/runtime/pull/561), [#570](https://github.com/mirendev/runtime/pull/570))
- **Fixed orphaned container shims** - Containerd shims are no longer left behind when app containers crash. ([#577](https://github.com/mirendev/runtime/pull/577))
- **Fixed env vars wiped by app.toml** - Adding the first `[[env]]` entry to `app.toml` no longer silently drops existing environment variables. ([#580](https://github.com/mirendev/runtime/pull/580))
- **Fixed DNS during sandbox startup** - DNS no longer returns empty responses during sandbox startup; unknown source IPs are now lazily resolved via entity store lookup. ([#576](https://github.com/mirendev/runtime/pull/576))
- **Fixed nil panic in `miren env list`** - Running `miren env list` when an app isn't deployed to the current cluster no longer crashes. ([#572](https://github.com/mirendev/runtime/pull/572))

---

## v0.3.0
*2026-01-27*

**Features**

- **Automatic image garbage collection** - Container images are now automatically garbage collected to prevent disk exhaustion. Images are kept if less than 30 days old or within the last 20 releases per app. Garbage collection runs weekly or immediately when disk usage exceeds 80%. ([#544](https://github.com/mirendev/runtime/pull/544))
- **Deploy-time environment variables** - Set environment variables atomically with your deployment using `miren deploy -e KEY=VALUE` or `-s SECRET=value` for sensitive values. Supports reading from files with `@file` syntax and interactive prompts. ([#521](https://github.com/mirendev/runtime/pull/521))
- **Graceful shutdown during redeploy** - Apps now receive proper graceful shutdown time during redeploy instead of being force-killed. Configure per-service with `shutdown_timeout` in app.toml (default 10 seconds). ([#520](https://github.com/mirendev/runtime/pull/520))
- **`miren deploy cancel`** - Cancel stuck in-progress deployments without waiting for the 30-minute lock expiry. ([#517](https://github.com/mirendev/runtime/pull/517))
- **`miren debug bundle`** - New diagnostic command that collects system info, logs, container state, and process info into a tar.gz archive for troubleshooting. ([#531](https://github.com/mirendev/runtime/pull/531))
- **Domain allow list for TLS** - Automatic TLS certificate provisioning is now restricted to domains with explicitly configured routes, preventing certificate issuance for arbitrary domains pointed at the server. ([#542](https://github.com/mirendev/runtime/pull/542))

**Improvements**

- **Better app history display** - The `miren app history` command now shows deployment IDs (useful for `deploy cancel`) and improved status formatting. ([#529](https://github.com/mirendev/runtime/pull/529), [#532](https://github.com/mirendev/runtime/pull/532))

**Bug Fixes**

- **Fixed server restart killing all sandboxes** - Sandbox recovery no longer incorrectly kills all sandboxes when the server restarts. ([#546](https://github.com/mirendev/runtime/pull/546))
- **Fixed disk lease transfers** - Disk leases are now properly released when sandboxes stop, allowing new sandboxes to acquire them. ([#516](https://github.com/mirendev/runtime/pull/516))
- **Fixed sandbox exec issues** - Fixed panic when running `sandbox exec` without a TTY and fixed wrong entrypoint being applied to service containers. ([#515](https://github.com/mirendev/runtime/pull/515), [#518](https://github.com/mirendev/runtime/pull/518))
- **Fixed deployment panic handling** - Panics during deployment are now properly marked as failed instead of leaving deployments stuck. ([#513](https://github.com/mirendev/runtime/pull/513))
- **Fixed WebSocket 502 errors** - WebSocket connections no longer fail with 502 Bad Gateway. ([#507](https://github.com/mirendev/runtime/pull/507))
- **Fixed cluster switch for multi-identity setups** - Improved identity handling and UX when switching between clusters. ([#535](https://github.com/mirendev/runtime/pull/535))

---

## v0.2.1
*2025-12-19*

**Improvements**

- **Improved `miren doctor`** - The diagnostic command now suggests commands for you to run instead of running them automatically. Includes better guidance for config, auth, and server issues, plus clickable routes in app listings. ([#491](https://github.com/mirendev/runtime/pull/491))
- **Smarter install defaults** - `miren server install` and `miren download` now default to the version matching your binary instead of always using `main`. ([#500](https://github.com/mirendev/runtime/pull/500), [#502](https://github.com/mirendev/runtime/pull/502))

**Bug Fixes**

- **Fixed scale-to-zero pool deletion** - Pools for apps at scale-to-zero are no longer prematurely deleted after being idle for an hour, which was causing "pool has reached maximum size" errors when traffic resumed. ([#497](https://github.com/mirendev/runtime/pull/497))
- **Fixed activator cache cleanup** - The activator now proactively cleans up cached pool references when pools are deleted, preventing stale cache errors. ([#498](https://github.com/mirendev/runtime/pull/498))
- **Fixed disk debug commands** - `miren debug disk status` and related commands now correctly parse disk IDs. ([#499](https://github.com/mirendev/runtime/pull/499))

---

## v0.2.0
*2025-12-17*

**Features**

- **`miren app run`** - Run commands in a one-off sandbox with your app's configuration. Great for debugging, migrations, or one-off tasks. ([#489](https://github.com/mirendev/runtime/pull/489))
- **Persistent BuildKit daemon** - Builds are now significantly faster thanks to a persistent BuildKit daemon that maintains layer caching across builds. No more cold starts! ([#490](https://github.com/mirendev/runtime/pull/490))
- **`miren doctor` command** - New diagnostic command to help troubleshoot your Miren setup. Includes `miren doctor apps` to check app status and `miren doctor auth` to verify authentication. ([#484](https://github.com/mirendev/runtime/pull/484))
- **`miren deploy --analyze`** - Preview what Miren will detect about your app before actually building it. Great for understanding how your project will be configured. ([#485](https://github.com/mirendev/runtime/pull/485))
- **Rust and uv support** - Miren now auto-detects Rust projects and Python projects using uv, and builds them appropriately. ([#485](https://github.com/mirendev/runtime/pull/485))
- **Log filtering** - Filter logs by service name with `miren logs --service <name>` and by content with `miren logs -g <pattern>`. Also includes a faster chunked log streaming API under the hood. ([#487](https://github.com/mirendev/runtime/pull/487), [#466](https://github.com/mirendev/runtime/pull/466))
- **Debug networking commands** - New `miren debug netdb` commands for inspecting IP allocations and cleaning up orphaned leases. Helpful for advanced troubleshooting. ([#478](https://github.com/mirendev/runtime/pull/478))

**Bug Fixes**

- **Fixed IP address leaks** - Resolved several issues where IP addresses could leak during sandbox lifecycle events, container cleanup, and entity patch failures. ([#479](https://github.com/mirendev/runtime/pull/479))
- **Fixed stale pool reference** - Deleting and recreating an IP pool no longer causes "error acquiring lease" failures. ([#483](https://github.com/mirendev/runtime/pull/483))
- **Fixed LSVD write handling** - LSVD now uses proper Go file writes instead of raw unix calls, improving reliability. ([#477](https://github.com/mirendev/runtime/pull/477))
- **Fixed deployment cancellation race** - Cancelling a deploy with Ctrl-C no longer causes a race condition between the main and UI goroutines. ([#482](https://github.com/mirendev/runtime/pull/482))
- **Fixed authentication bypass** - Local/non-cloud mode now properly requires client certificates. ([#469](https://github.com/mirendev/runtime/pull/469))
- **Fixed entity revision check** - Entity patches no longer incorrectly enforce revision checks when `fromRevision` is 0. ([#470](https://github.com/mirendev/runtime/pull/470))
- **Fixed IPv6 environments** - VictoriaMetrics and VictoriaLogs now listen on IPv6, fixing issues in environments with IPv6 enabled. ([#481](https://github.com/mirendev/runtime/pull/481))

**Documentation**

- Updated system requirements to 4GB RAM and 20GB disk ([#480](https://github.com/mirendev/runtime/pull/480))
- Improved getting started documentation ([#471](https://github.com/mirendev/runtime/pull/471))
- Fixed missing pages in docs sidebar navigation ([#467](https://github.com/mirendev/runtime/pull/467))

---

## v0.1.0
*2025-12-09*

Initial preview release.
