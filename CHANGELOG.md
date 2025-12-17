# Changelog

All notable changes to Miren Runtime will be documented in this file.

## [v0.2.0] - 2025-12-17

### Features

- **`miren app exec` command** - Open an interactive shell in your app's environment. Creates an ephemeral sandbox with your app's configuration for debugging and exploration. ([#489](https://github.com/mirendev/runtime/pull/489))

- **Persistent BuildKit daemon** - Builds are now significantly faster thanks to a persistent BuildKit daemon that maintains layer caching across builds. No more cold starts! ([#490](https://github.com/mirendev/runtime/pull/490))

- **`miren doctor` command** - New diagnostic command to help troubleshoot your Miren setup. Includes `miren doctor apps` to check app status and `miren doctor auth` to verify authentication. ([#484](https://github.com/mirendev/runtime/pull/484))

- **`miren deploy --analyze`** - Preview what Miren will detect about your app before actually building it. Great for understanding how your project will be configured. ([#485](https://github.com/mirendev/runtime/pull/485))

- **Rust and uv support** - Miren now auto-detects Rust projects and Python projects using uv, and builds them appropriately. ([#485](https://github.com/mirendev/runtime/pull/485))

- **Log filtering** - Filter logs by service name with `miren logs --service <name>` and by content with `miren logs -g <pattern>`. Also includes a faster chunked log streaming API under the hood. ([#487](https://github.com/mirendev/runtime/pull/487), [#466](https://github.com/mirendev/runtime/pull/466))

- **Debug networking commands** - New `miren debug netdb` commands for inspecting IP allocations and cleaning up orphaned leases. Helpful for advanced troubleshooting. ([#478](https://github.com/mirendev/runtime/pull/478))

### Bug Fixes

- **Fixed IP address leaks** - Resolved several issues where IP addresses could leak during sandbox lifecycle events, container cleanup, and entity patch failures. ([#479](https://github.com/mirendev/runtime/pull/479))

- **Fixed stale pool reference** - Deleting and recreating an IP pool no longer causes "error acquiring lease" failures. ([#483](https://github.com/mirendev/runtime/pull/483))

- **Fixed LSVD write handling** - LSVD now uses proper Go file writes instead of raw unix calls, improving reliability. ([#477](https://github.com/mirendev/runtime/pull/477))

- **Fixed deployment cancellation race** - Cancelling a deploy with Ctrl-C no longer causes a race condition between the main and UI goroutines. ([#482](https://github.com/mirendev/runtime/pull/482))

- **Fixed authentication bypass** - Local/non-cloud mode now properly requires client certificates. ([#469](https://github.com/mirendev/runtime/pull/469))

- **Fixed entity revision check** - Entity patches no longer incorrectly enforce revision checks when `fromRevision` is 0. ([#470](https://github.com/mirendev/runtime/pull/470))

- **Fixed IPv6 environments** - VictoriaMetrics and VictoriaLogs now listen on IPv6, fixing issues in environments with IPv6 enabled. ([#481](https://github.com/mirendev/runtime/pull/481))

### Documentation

- Updated system requirements to 4GB RAM and 20GB disk ([#480](https://github.com/mirendev/runtime/pull/480))

- Improved getting started documentation ([#471](https://github.com/mirendev/runtime/pull/471))

- Fixed missing pages in docs sidebar navigation ([#467](https://github.com/mirendev/runtime/pull/467))

---

## [v0.1.0] - 2025-12-09

Initial preview release.
