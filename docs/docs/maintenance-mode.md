---
title: Maintenance Mode
description: Take a route out of service, show visitors a holding page, and bring it back — one command each way.
keywords: [maintenance mode, holding page, planned downtime, migration, 503, retry-after, route down, route up]
---

import CliCommand from '@site/src/components/CliCommand';

# Maintenance Mode

Maintenance mode takes a hostname out of service and shows visitors a holding page instead. It's for the moments you plan for: a database migration that can't run under live traffic, a dependency cutover, an incident where you want the site quiet while you look at it.

It's one command in, and one command out.

## Minimum working example

<CliCommand context="client">
```miren
miren route down app.example.com --reason "Upgrading the database"
miren route up app.example.com
```
</CliCommand>

Between those two commands, anyone visiting `app.example.com` gets a holding page that says the app's name and your reason. Your app keeps running the whole time.

## What it does and doesn't touch

Maintenance is a routing decision and only a routing decision. That's what makes it useful:

- **Your app keeps running.** Sandboxes aren't stopped or scaled down, so `miren app run` and one-shot migrations work normally during the window. Taking traffic off the route first is the entire point.
- **Other hostnames keep serving.** If `app.example.com` and `admin.example.com` both point at the same app, taking one down leaves the other alone.
- **Internal calls keep working.** Service-to-service requests inside the cluster resolve by app, not by hostname, so they're unaffected.
- **Preview URLs are covered.** Ephemeral preview subdomains like `feat-x.app.example.com` resolve through their base route, so they go down with it. Previews share the app's database, so a migration window that left them serving wouldn't be much of a window.

## Telling visitors what's happening

`--reason` is shown on the holding page, so write it for your visitors rather than for your team.

`--back-at` sets an expected return time. It takes a clock time, a duration, or a full timestamp:

<CliCommand context="client">
```miren
miren route down app.example.com --reason "DB migration" --back-at 15:00
miren route down app.example.com --reason "DB migration" --back-at 30m
miren route down app.example.com --reason "DB migration" --back-at 2027-03-04T15:00:00Z
```
</CliCommand>

A bare clock time is read in your own timezone, and one that has already passed resolves to tomorrow, so `--back-at 09:00` at 3pm means 9am the next morning. An absolute timestamp has to be in the future; the server refuses one that has already passed rather than putting a stale estimate on the holding page.

:::warning[`--back-at` is a promise to visitors, not a timer]
It tells people when you expect to be back. It does not bring the route back. The route stays in maintenance until you run `miren route up`, however long that takes.

If the window runs past your estimate, the `Retry-After` header stops being sent, but the page keeps showing the time you gave. That is deliberate: a page still naming a time that has passed reads as overdue, which is the truth, where a page quietly dropping the estimate would read as if nothing were wrong.
:::

## What clients see

Visitors get **HTTP 503 Service Unavailable**. This is the right status for deliberate, temporary downtime, and it's the one search crawlers and uptime monitors already know how to read — a maintenance window served as a 500, or as a 200 with an apology, is how sites lose ranking and how on-call gets paged for a planned event.

Alongside it:

- `Retry-After`, when you gave a `--back-at` that is still in the future. Monitors and crawlers use it to decide when to come back.
- `Cache-Control: no-store`, so no browser or CDN holds the holding page past the end of the window.

Clients that ask for JSON get JSON rather than a web page, which keeps API consumers from having to parse HTML to find out what happened:

```json
{
  "error": "maintenance",
  "reason": "Upgrading the database",
  "back_at": "2027-03-04T15:00:00Z"
}
```

## The default route

The default route catches every hostname that doesn't match a route of its own, so taking it down is the widest single command available. Because of that it asks for confirmation:

<CliCommand context="client">
```miren
miren route down --default --reason "Cluster upgrade"
miren route up --default
```
</CliCommand>

Pass `--yes` to skip the prompt in a script. JSON output can't carry a prompt, so `--json` on the default route requires `--yes` too, and refuses without it rather than quietly becoming the way around the confirmation.

## Seeing what's down

`miren route list` has a SERVING column, and `miren route show` gives the full record — the reason, the expected return, who set it, and when:

<CliCommand context="client">
```miren
miren route show app.example.com
```
</CliCommand>

`miren app status` also names any of the app's routes that are currently serving a holding page, so you don't have to already suspect maintenance to find it.

## A migration, start to finish

<CliCommand context="client">
```miren
miren route down app.example.com --reason "Upgrading the database" --back-at 15:00
miren app run -a web -- ./bin/migrate
miren route up app.example.com
```
</CliCommand>

The `miren app run` in the middle works because maintenance never touched the app's sandboxes — it only changed what the router does with inbound requests.

:::note[Verifying before you reopen]
There's no way to browse the real app from behind the holding page yet. Bring the route up and watch, or check the app through `miren app run` and `miren logs` while the window is still open.
:::

## What's not covered

:::warning[Background work keeps running]
Maintenance mode only affects HTTP traffic. Background workers and scheduled jobs carry on, so a window opened for a migration does not stop them writing to the database you are migrating. Stop them the way you normally would.
:::

Custom holding-page HTML and scheduled windows (announce a future window, enter and leave automatically) aren't supported yet.
