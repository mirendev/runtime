---
title: Miren Labs
description: Experimental features available for early access and feedback before stable release.
keywords: [labs, experimental, features, opt-in, preview]
---

import CliCommand from '@site/src/components/CliCommand';

# Miren Labs

Miren Labs is where we ship experimental features that aren't quite ready for prime time. These are capabilities we're actively developing and want to get into your hands early for feedback.

## What to Expect

Labs features are:

- **Experimental** — APIs and behavior may change based on feedback
- **Opt-in** — Off by default, you choose when to try them
- **Reversible** — Anything you turn on you can turn back off
- **Supported** — We want to hear about bugs and rough edges
- **On a path** — Most labs features are headed toward stable release

When a feature graduates to stable it flips on by default and stops being opt-in, but we leave its flag in place for a release so you can turn it back off if the new behavior causes trouble. After that release the flag and the old behavior both go away.

## Enabling Labs Features

Labs features are controlled via the `--labs` flag or `MIREN_LABS` environment variable when starting the Miren server.

<CliCommand context="server">
```miren
# Enable a labs feature
miren server --labs appvisibility

# Via environment variable
MIREN_LABS=appvisibility miren server
```
</CliCommand>

To name more than one feature, repeat `--labs` or comma-separate the
environment variable.

## Turning a Feature Off

Prefix a feature name with `-` to disable it, using the same flag and
environment variable as enabling. This is mainly how you back out of a feature
that has graduated to on by default, for the one release its flag sticks around
as an escape hatch.

<CliCommand context="server">
```miren
# Turn a feature off
miren server --labs -appvisibility

# Via environment variable
MIREN_LABS=-appvisibility miren server
```
</CliCommand>

Check the server log to confirm the setting landed. Every boot logs a `labs features` line listing where each feature ended up, whether or not you named it.

## Giving Feedback

We'd love to hear how labs features work for you:

- **What's working well** — Helps us know we're on the right track
- **What's confusing** — Documentation gaps, unclear behavior
- **What's broken** — Bugs, crashes, unexpected behavior
- **What's missing** — Features that would make it useful for your use case

Reach out via [Discord](https://miren.dev/discord) or open an issue on [GitHub](https://github.com/mirendev/runtime/issues).

## Current Labs Features

Individual labs features are documented alongside their related functionality. Look for the "Labs Feature" callout in the docs.
