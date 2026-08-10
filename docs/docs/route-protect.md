---
title: Protecting Routes
description: Put a login in front of your app's HTTP routes, either single sign-on with OIDC or a shared password.
keywords: [route protection, oidc, sso, authentication, single sign-on, identity provider, password protection, password protect]
---

import CliCommand from '@site/src/components/CliCommand';

# Protecting Routes

:::tip[Looking for CI/CD OIDC?]
If you want to **deploy from GitHub Actions or other CI systems** using OIDC tokens (no stored secrets), see [CI/CD Deployment with OIDC](/ci-deploy). This page covers a different feature: protecting your app's HTTP routes with a login.
:::

Route protection puts a login in front of an application at the routing layer, without you writing any auth code in the app. There are two ways to do it:

- **[OIDC single sign-on](#quick-start)** sends unauthenticated requests off to an identity provider like Google or GitHub, then hands the resulting JWT claims to your app as HTTP headers. Reach for this when you want to know who each user is and gate access per person.
- **[Password protection](#password-protection)** shows visitors a login form and lets them in once they type a shared password. It's a good fit for staging sites and internal tools, where you just want to keep the public out without standing up an identity provider.

Both work the same way: add an auth provider, then attach it to a route with `miren route protect`. OIDC comes first below, then password protection.

With OIDC, your app reads identity straight off plain HTTP headers like `X-User-Email`, so there's no auth code to write. Any standards-compliant identity provider works.

## Minimum working example

The fastest way to put a login in front of a route is a shared password:

<CliCommand context="client">
```miren
miren auth provider add password staging-pw
miren route protect staging.example.com --provider staging-pw
```
</CliCommand>

Visitors now get a login form; entering the right password sets a 24-hour session cookie. For per-user identity from a provider like Google or GitHub, use the OIDC [Quick Start](#quick-start) below instead.

## Quick Start

**Step 1: Add an identity provider**

<CliCommand context="client">
```miren
miren auth provider add oidc my-google \
  --provider-url https://accounts.google.com \
  --client-id $GOOGLE_CLIENT_ID \
  --client-secret $GOOGLE_CLIENT_SECRET \
  --scope email --scope profile
```
</CliCommand>

**Step 2: Protect a route**

<CliCommand context="client">
```miren
miren route protect myapp.example.com \
  --provider my-google \
  --claim-header email:X-User-Email \
  --claim-header name:X-User-Name
```
</CliCommand>

That's it. Unauthenticated requests to `myapp.example.com` are now redirected to Google for login. After authentication, your app receives `X-User-Email` and `X-User-Name` headers.

## Authentication vs Authorization

This feature handles **authentication**: it confirms who a user is. It doesn't decide what they're allowed to do, which is **authorization** and stays in your app's hands.

Once a user is authenticated, your app gets their claims as headers and takes it from there. If you set up Google as the provider, for example, your app sees the user's email address and can check the domain before letting them do anything.

## Trust Model

Your app can trust the claim headers like `X-User-Email` because Miren is the only network path into the sandbox. External clients can't reach your app directly. Miren validates the JWT from the identity provider and sets the headers before it proxies the request along.

This "trust the proxy" pattern is the same approach OAuth2 Proxy and nginx's `auth_request` take. It works well when the platform controls the network, which Miren does through sandbox isolation.

So your app doesn't have to verify signatures or validate tokens. It can treat the claim headers as trusted input from the platform.

## How Claim Mappings Work

The `--claim-header` option maps JWT claims from the identity provider to HTTP headers your app receives:

<CliCommand context="client">
```miren
miren route protect myapp.example.com \
  --provider my-google \
  --claim-header email:X-User-Email \
  --claim-header sub:X-User-ID \
  --claim-header name:X-User-Name
```
</CliCommand>

- Each mapping takes the form `CLAIM:HEADER`.
- Pass `--claim-header` as many times as you need.
- Claims that aren't in the JWT are skipped.
- Your app sees the rest as ordinary request headers.

## Example Provider Setups

### Google

Google's identity provider is a good pick when you want to limit access to a particular email domain.

<CliCommand context="client">
```miren
miren auth provider add oidc my-google \
  --provider-url https://accounts.google.com \
  --client-id $GOOGLE_CLIENT_ID \
  --client-secret $GOOGLE_CLIENT_SECRET \
  --scope email --scope profile
```
</CliCommand>

Your app can then look at the domain on the `X-User-Email` header for basic access control, for example only allowing `@yourcompany.com`.

### GitHub

GitHub doesn't expose a native OIDC endpoint, so Miren talks to it through a connector instead of the OIDC discovery path. You still get the same end result: identity arrives at your app as plain HTTP headers.

Register an OAuth App in GitHub (Settings → Developer settings → OAuth Apps) with the callback URL `https://your-route.example.com/.well-known/miren/oidc/callback`, then add the provider with `add github` and the orgs you want to allow:

<CliCommand context="client">
```miren
miren auth provider add github my-github \
  --client-id $GITHUB_CLIENT_ID \
  --client-secret $GITHUB_CLIENT_SECRET \
  --org mirendev
```
</CliCommand>

To restrict by team membership, suffix the org with one or more team slugs:

<CliCommand context="client">
```miren
miren auth provider add github my-github \
  --client-id $GITHUB_CLIENT_ID \
  --client-secret $GITHUB_CLIENT_SECRET \
  --org mirendev:engineering,platform
```
</CliCommand>

Repeat `--org` for multiple organizations. Attach the provider to a route as usual:

<CliCommand context="client">
```miren
miren route protect myapp.example.com \
  --provider my-github \
  --claim-header email:X-User-Email \
  --claim-header preferred_username:X-User-Login \
  --claim-header groups:X-User-Groups
```
</CliCommand>

The connector exposes the user's GitHub login as `preferred_username`, and any org/team memberships as `groups` (in the form `org:team`). The full set of claims available is `sub`, `email`, `email_verified`, `name`, `preferred_username`, and `groups`. Mix and match `--claim-header` mappings to surface the bits your app cares about.

A wrinkle worth knowing: the `groups` claim only carries entries for *teams* the user belongs to within configured orgs. If you set `--org NAME` with no team suffix, the user is authorized (they have to be in NAME to get in) but `groups` comes back empty because there are no team memberships to surface. For an org membership signal, lean on the fact that the request reached your app at all, or set team filters with `--org NAME:team1,team2` to populate `groups`.

### GitLab

GitLab has a built-in OIDC provider, so there's no federation layer to set up. Register an application in your GitLab instance and point Miren straight at it.

<CliCommand context="client">
```miren
miren auth provider add oidc my-gitlab \
  --provider-url https://gitlab.com \
  --client-id $GITLAB_CLIENT_ID \
  --client-secret $GITLAB_CLIENT_SECRET \
  --scope email --scope profile
```
</CliCommand>

### Keycloak (self-hosted)

[Keycloak](https://www.keycloak.org/) is an open-source identity provider you run yourself. It gives you a lot of control over how users federate in and how claims are configured.

<CliCommand context="client">
```miren
miren auth provider add oidc my-keycloak \
  --provider-url https://keycloak.example.com/realms/myrealm \
  --client-id $KEYCLOAK_CLIENT_ID \
  --client-secret $KEYCLOAK_CLIENT_SECRET \
  --scope email --scope profile
```
</CliCommand>

## Password Protection

When you don't need to know who a visitor is and just want to keep an app behind a shared secret, protect the route with a password instead of an OIDC provider. Visitors get a login form and type the password to get in. There are no usernames and no per-user identity; anyone with the password is let through.

**Step 1: Add a password provider**

<CliCommand context="client">
```miren
miren auth provider add password staging-pw
```
</CliCommand>

Leave off `--password` and Miren prompts you for it (the input isn't shown), which keeps the password out of your shell history. You can also pass it inline or read it from a file:

<CliCommand context="client">
```miren
# Provide the password inline
miren auth provider add password staging-pw --password s3cret

# Read the password from a file (handy for scripts and CI)
miren auth provider add password staging-pw --password @./password.txt
```
</CliCommand>

Miren only stores a bcrypt hash of the password, never the plaintext.

**Step 2: Protect a route**

<CliCommand context="client">
```miren
miren route protect staging.example.com --provider staging-pw
```
</CliCommand>

`route protect` figures out on its own whether the named provider is OIDC or password, so the command is the same for both. That's all there is to it. Unauthenticated requests to `staging.example.com` now get a login form, and anyone who enters the right password is let through.

`--claim-header` has nothing to map for a password provider (there are no JWT claims), so it's ignored if you pass it.

### How the Login Flow Works

- An unauthenticated visitor gets a **"Password Required"** form, whatever path they asked for.
- When they enter the right password, Miren drops a session cookie (`miren_pw_session`) and sends them back to the page they were after. The session is good for **24 hours**, then they log in again.
- A wrong password reloads the form with an "Incorrect password." message.
- The cookie is tied to its route, so it can't be reused against a different protected host.

To sign out, send the visitor to `/.well-known/miren/auth/logout`. That clears the session cookie and redirects to `/`.

### Rotating the Password

Provider names have to be unique, so adding one that already exists is rejected unless you pass `--update`. That overwrites the provider and rotates the password:

<CliCommand context="client">
```miren
miren auth provider add password staging-pw --update
```
</CliCommand>

Sessions that are already open stay valid until they expire (24 hours). New logins need the new password.

### Default Route

Like OIDC, password protection works on the default route via `--default`:

<CliCommand context="client">
```miren
miren route protect --default --provider staging-pw
```
</CliCommand>

## Reusing Providers Across Routes

Once you've added a provider, you can use it to protect any number of routes:

<CliCommand context="client">
```miren
miren route protect app1.example.com --provider my-google --claim-header email:X-User-Email
miren route protect app2.example.com --provider my-google --claim-header email:X-User-Email
```
</CliCommand>

## Default Route Support

`route protect` and `route unprotect` support the `--default` flag for the default route, which has no static hostname. On the default route, Miren works out the OAuth2 redirect URL from the request's `Host` header at runtime.

<CliCommand context="client">
```miren
miren route protect --default \
  --provider my-google \
  --claim-header email:X-User-Email
```
</CliCommand>

## Managing Providers

The same commands manage both OIDC and password providers:

<CliCommand context="client">
```miren
# List all providers
miren auth provider list

# Show details of a provider
miren auth provider show my-google

# Remove a provider
miren auth provider remove my-google
```
</CliCommand>

Removing a provider that's still attached to a route is refused. Pass `--force` to remove it anyway, which leaves those routes unprotected:

<CliCommand context="client">
```miren
miren auth provider remove staging-pw --force
```
</CliCommand>

## Removing Route Protection

<CliCommand context="client">
```miren
# Remove protection from a route (provider is preserved for reuse)
miren route unprotect myapp.example.com

# Check route status including protection info
miren route show myapp.example.com
```
</CliCommand>

See the [CLI reference](/command/route-protect) for the full list of options.
