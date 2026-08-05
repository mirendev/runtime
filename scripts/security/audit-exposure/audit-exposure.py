#!/usr/bin/env python3
#
# Post-incident review helper for the :8443 cert-auth bypass
# (GHSA-8fh7-7q4q-cq52). Once you've closed the exposure (upgrade to the
# patched release and restrict :8443 ingress), this helps you look over
# what the bypass could have touched on a cluster. See the README in this
# directory for the full walkthrough.
#
# Read-only -- it only calls `miren debug entity list`. Stdlib Python 3,
# no deps. Run it from a host with the `miren` CLI configured for the
# cluster.
#
# What it shows:
#   A. standing access grants (oidc_binding, runner_invite, auth
#      providers) to look over -- an entry you don't recognize is worth
#      a closer look.
#   B. workloads/routes created or changed in the window, to cross-check
#      against your own deploys.
#   C. a checklist of secrets that were reachable via the bypass, for
#      rotation.
#
# What it can't show: whether anything was actually read. Reads leave no
# trace, so a quiet result isn't a clean bill of health -- just the
# absence of anything left behind. If :8443 was reachable from untrusted
# networks during the window, it's prudent to presume the secrets in
# Section C may have been read, and rotate them.
#
# It reads `debug entity list --json`, which is the CLI's complete and
# unelided output. The human-readable view pages and shortens long values,
# so an audit must never read that one.
#
# USAGE
#   audit-exposure.py --since 2026-06-01T00:00:00Z [--until ...] [--miren miren]
"""audit-exposure: post-incident review helper for GHSA-8fh7-7q4q-cq52."""
import argparse
import json
import datetime as dt
import math
import re
import subprocess
import sys

# Section A: small, fully-enumerable kinds that grant standing access or route
# traffic -- review every entry by hand.
STANDING_KINDS = [
    "oidc_binding",       # standing OIDC access (e.g. from a GitHub repo)
    "runner_invite",      # lets a (rogue) runner join the cluster
    "oidc_provider",      # route auth provider
    "password_provider",  # route auth provider
    "waf_profile",        # web-app-firewall rules (weakening = tamper)
    "http_route",         # ingress routing (a rogue route can redirect traffic)
]

# Section B: the workload chain an operator actually authors. app -> app_version
# is the meaningful unit of change; config_version / deployment / artifact /
# sandbox_pool / sandbox are subordinate byproducts and aren't listed here
# (config_version is still read for the Section C rotation list).
WORKLOAD_KINDS = ["app", "app_version"]

# Redact secret-bearing fields when dumping specs so the report is safe to
# share. Matches keys like client_secret, password_hash, code_hash, api_key,
# access_key, *_token, *_credential, and anything ending in _key (but not a
# bare `key:`, which shows up in non-secret places like claim_conditions).
SECRET_KEY_RE = re.compile(
    r"^-?\s*[A-Za-z0-9_.-]*"
    r"(?:secret|password|passwd|hash|token|credential|bearer|apikey|_key)"
    r"[A-Za-z0-9_.-]*\s*:",
    re.I,
)


def redact_fields(doc):
    """Display lines for a record's facets, with secret-bearing fields redacted.

    A redacted key drops its whole value, nested structure included, so a PEM
    or a credentials object cannot leak through a child field. Values under a
    redacted key are never read.
    """
    out = []
    for facet in doc.get("facets") or []:
        if facet.get("label", "").endswith("/metadata"):
            continue  # name/labels add noise here, not evidence
        for f in facet.get("fields") or []:
            out.extend(_render_field(f["name"], f.get("value"), 0))

    # Attributes no schema claims are still evidence, and are exactly what a
    # facets-only dump would hide.
    for f in doc.get("unclaimed") or []:
        out.extend(_render_field(f["name"], f.get("value"), 0))

    return out


def _render_field(name, value, depth):
    pad = "  " * depth

    if SECRET_KEY_RE.match(f"{name}:"):
        return [f"{pad}{name}: <redacted>"]

    if isinstance(value, dict):
        lines = [f"{pad}{name}:"]
        for k in sorted(value):
            lines.extend(_render_field(k, value[k], depth + 1))
        return lines

    if isinstance(value, list):
        lines = [f"{pad}{name}:"]
        for item in value:
            if isinstance(item, dict):
                lines.append(f"{pad}  -")
                for k in sorted(item):
                    lines.extend(_render_field(k, item[k], depth + 2))
            else:
                lines.append(f"{pad}  - {item}")
        return lines

    return [f"{pad}{name}: {value}"]


def run_list(miren, kind):
    """Return the entity documents for a kind, or None if the query failed.

    A failed query must NOT be mistaken for an empty result -- otherwise a
    partial scan reads as clean, the exact false-negative this tool exists to
    avoid. Callers surface None as an explicit failure. The timeout keeps a
    hung CLI from stalling the scan mid-incident.

    --json is load-bearing, and not just for parsing convenience. The text view
    is deliberately bounded for humans (it pages and elides long values), while
    --json is the complete, unelided contract. An audit reading the human view
    would silently under-report. --limit 0 says the same thing about rows.
    """
    try:
        r = subprocess.run(
            [miren, "debug", "entity", "list", "-k", kind, "--limit", "0", "--json"],
            capture_output=True, text=True, timeout=120,
        )
    except (subprocess.TimeoutExpired, OSError) as e:
        sys.stderr.write(f"  ! list -k {kind} errored: {e}\n")
        return None
    if r.returncode != 0:
        sys.stderr.write(f"  ! list -k {kind} failed: {r.stderr.strip()[:200]}\n")
        return None
    try:
        docs = json.loads(r.stdout)
    except json.JSONDecodeError as e:
        sys.stderr.write(f"  ! list -k {kind} returned unparseable JSON: {e}\n")
        return None
    if not isinstance(docs, list):
        # Surface it as a failed query rather than crashing partway through the
        # scan; a half-finished section reads as a clean one.
        sys.stderr.write(f"  ! list -k {kind} returned {type(docs).__name__}, expected a list\n")
        return None
    return docs


def facet_fields(doc, suffix):
    """Fields of the facet whose label ends with suffix, as a name->value dict.

    Entities compose several kinds at once (a config_version also carries
    core/metadata), so a field has to be looked up under the facet that owns it
    rather than in one flat namespace.

    Anything in `unclaimed` is folded in under its attribute id. That group
    exists precisely for attributes no schema accounts for, and a scan that
    only reads facets would step straight past them -- the under-reporting this
    tool is built to avoid. Facet fields win a name collision, since those are
    the schema-resolved ones.
    """
    fields = {f["name"]: f.get("value") for f in doc.get("unclaimed") or []}

    for facet in doc.get("facets") or []:
        if facet.get("label", "").endswith(suffix):
            fields.update({f["name"]: f.get("value") for f in facet.get("fields") or []})
            break

    return fields


def entity_name(doc):
    return facet_fields(doc, "/metadata").get("name") or ""


def iter_vars(spec):
    """(key, sensitive, values) for every variable in a config_version spec.

    Covers both top-level variables and per-service env, which share a shape. A
    key can recur across services, so sensitive merges as OR (marked anywhere ->
    treated sensitive) and every value is kept so all of them get shape-tested.
    """
    spec = spec or {}

    groups = [spec.get("variables") or []]
    for svc in spec.get("services") or []:
        groups.append((svc or {}).get("env") or [])

    merged = {}
    for group in groups:
        for var in group:
            if not isinstance(var, dict):
                continue
            key = var.get("key")
            if not key:
                continue
            m = merged.setdefault(key, {"sensitive": False, "values": []})
            m["sensitive"] = m["sensitive"] or bool(var.get("sensitive"))
            value = var.get("value")
            if value is not None:
                m["values"].append(str(value))

    return [(k, m["sensitive"], m["values"]) for k, m in merged.items()]


def sensitive_var_keys(doc):
    """Names of variables flagged sensitive in a record (never values)."""
    spec = facet_fields(doc, "/config_version").get("spec")
    return {key for key, sensitive, _ in iter_vars(spec) if sensitive}


# -- Unmarked-secret heuristics (Section C2) --------------------------------
# Section C lists only variables explicitly flagged sensitive:true. But that
# flag is display-only (a reader saw every value regardless), and people forget
# to set it constantly -- so unflagged-but-secret config is a silent hole in
# the rotation list. This pass re-scans ALL variables and flags ones that look
# secret but were NOT marked sensitive, by two independent signals: the key
# NAME, and the VALUE shape (which catches misnamed secrets a name check would
# miss). Values are inspected to compute a signal but, as everywhere else in
# this tool, never printed -- output is key name + reason only. It's a review
# list, not an assertion: entropy heuristics have false positives, so an
# operator confirms, then rotates AND sets sensitive:true.

# Key names that read as secret-bearing...
UNMARKED_NAME_RE = re.compile(
    r"(secret|password|passwd|token|api[_-]?key|private[_-]?key|credential|"
    r"access[_-]?key|client[_-]?secret|passphrase|bearer|signing[_-]?key|"
    r"encryption[_-]?key|_key$|_dsn$)", re.I)
# ...minus names that merely reference a secret without being one: public keys,
# ids (client_id, key_id), endpoints/urls, file paths, usernames, toggles.
NAME_DENY_RE = re.compile(
    r"(public|client[_-]?id|_id$|_url$|_uri$|_endpoint$|_host$|_name$|"
    r"_file$|_path$|_user$|_username$|_enabled$|_timeout$|_ttl$|_sid$|"
    r"_count$|key_id)", re.I)
# Identifier-ish keys whose values are high-entropy but not secret (account /
# org / billing / rule IDs, SIDs). Suppresses ONLY the weak entropy signal for
# these; a strong shape (PEM/JWT/url-creds/provider-prefix) still fires.
ENTROPY_KEY_DENY_RE = re.compile(
    r"(_id$|_sid$|_uid$|_guid$|_number$|account|organization|billing)", re.I)

# Strong value shapes: unambiguous credential material.
PROVIDER_TOKEN_RE = re.compile(
    r"(gh[opsu]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|"
    r"xox[baprs]-[A-Za-z0-9-]{10,}|sk_(live|test)_[A-Za-z0-9]{16,}|"
    r"AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_\-]{35}|glpat-[A-Za-z0-9_\-]{16,}|"
    r"dop_v1_[a-f0-9]{40,}|SG\.[A-Za-z0-9_\-]{16,})")
URL_CRED_RE = re.compile(r"://[^/\s:@]+:[^/\s@]+@")
JWT_RE = re.compile(r"^eyJ[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]+$")
HIGH_ENTROPY_CHARSET_RE = re.compile(r"^[A-Za-z0-9+/=_\-.]{24,}$")


def _looks_non_secret(v):
    """Cheap excluders so the entropy check doesn't flag urls/emails/numbers/hosts/paths."""
    return bool(
        re.match(r"^https?://", v)
        or re.match(r"^[\w.+-]+@[\w-]+\.[\w.-]+$", v)          # email
        or re.match(r"^-?\d+(\.\d+)?$", v)                      # number
        or re.match(r"^\d{4}-\d\d-\d\d[T ]", v)                # timestamp
        or re.match(r"^([A-Za-z0-9-]+\.){2,}[A-Za-z]{2,}$", v)  # dotted hostname
        or v.startswith("/")                                    # path
    )


def _shannon(s):
    counts = {}
    for c in s:
        counts[c] = counts.get(c, 0) + 1
    n = len(s)
    return -sum((k / n) * math.log2(k / n) for k in counts.values()) if n else 0.0


def value_signal(v):
    """Reason string if a value LOOKS like a credential, else None. Never emits the value."""
    if v is None:
        return None
    v = v.strip().strip('"').strip("'")
    if not v:
        return None
    if "-----BEGIN" in v:
        return "PEM/key material"
    if JWT_RE.match(v):
        return "JWT-shaped value"
    if URL_CRED_RE.search(v):
        return "URL with embedded credentials"
    if PROVIDER_TOKEN_RE.search(v):
        return "known provider token prefix"
    if HIGH_ENTROPY_CHARSET_RE.match(v) and not _looks_non_secret(v) and _shannon(v) >= 3.8:
        return "high-entropy value"
    return None


def name_signal(key):
    if NAME_DENY_RE.search(key):
        return None
    return "name looks secret" if UNMARKED_NAME_RE.search(key) else None


def unmarked_candidates(spec):
    """(key, reasons, strong) for vars NOT flagged sensitive that look secret.
    strong=True means a value-shape signal fired (high confidence)."""
    out = []
    for key, sensitive, values in iter_vars(spec):
        if sensitive:
            continue
        reasons, strong = [], False
        for value in values:  # any one of a key's values could be the secret
            vs = value_signal(value)
            if vs == "high-entropy value" and ENTROPY_KEY_DENY_RE.search(key):
                vs = None  # identifier, not a credential -- drop the weak signal
            if vs:
                reasons.append(vs)
                strong = True
                break
        ns = name_signal(key)
        if ns:
            reasons.append(ns)
        if reasons:
            out.append((key, reasons, strong))
    return out


def record_app(doc):
    return facet_fields(doc, "/config_version").get("app") or "?"


def parse_ts(s):
    if not s:
        return None
    s = s.strip().replace("Z", "+00:00")
    try:
        ts = dt.datetime.fromisoformat(s)
    except ValueError:
        return None
    # A bare timestamp (no offset) parses naive; stamp UTC so it stays
    # comparable to the always-aware entity timestamps in in_window().
    if ts.tzinfo is None:
        ts = ts.replace(tzinfo=dt.timezone.utc)
    return ts


def entity_id(doc):
    return doc.get("id") or "?"


def summarize(miren, kind):
    """List of entity dicts for a kind, or None if the query failed."""
    docs = run_list(miren, kind)
    if docs is None:
        return None
    out = []
    for doc in docs:
        out.append({
            "id": entity_id(doc),
            "name": entity_name(doc),
            "created": parse_ts(doc.get("created_at")),
            "updated": parse_ts(doc.get("updated_at")),
            "doc": doc,
        })
    return out


def main():
    ap = argparse.ArgumentParser(description="Post-incident exposure review (GHSA-8fh7-7q4q-cq52).")
    ap.add_argument("--since", required=True, help="exposure-window start, ISO8601 (e.g. 2026-06-01T00:00:00Z)")
    ap.add_argument("--until", default=None, help="exposure-window end, ISO8601 (default: now)")
    ap.add_argument("--miren", default="miren", help="path to miren CLI (default: miren)")
    args = ap.parse_args()

    since = parse_ts(args.since)
    if since is None:
        sys.exit(f"bad --since: {args.since!r}")
    if args.until:
        until = parse_ts(args.until)
        if until is None:
            sys.exit(f"bad --until: {args.until!r}")
    else:
        until = dt.datetime.now(dt.timezone.utc)

    def in_window(ts):
        return ts is not None and since <= ts <= until

    failures = []  # kinds whose query failed; non-empty means a PARTIAL scan

    print("=" * 74)
    print(" miren post-incident review  (GHSA-8fh7-7q4q-cq52)")
    print(f" window: {since.isoformat()}  ..  {until.isoformat()}")
    print("=" * 74)
    print(" This reviews what the bypass could have left behind or changed.")
    print(" It can't tell you what was read -- reads leave no trace -- so a")
    print(" quiet result isn't a clean bill of health. If :8443 was reachable")
    print(" from untrusted networks in this window, it's prudent to presume")
    print(" the secrets in Section C may have been read, and rotate them.")

    # -- Section A: standing-access artifacts (review ALL, any age) --
    print("\n" + "#" * 74)
    print("# A. STANDING ACCESS & ROUTING  --  worth looking over by hand.")
    print("#    These grant ongoing access or route traffic. An entry you")
    print("#    don't recognize (an OIDC subject/repo that isn't yours, an")
    print("#    invite you didn't cut, a route you didn't add) is worth a")
    print("#    closer look.")
    print("#" * 74)
    for kind in STANDING_KINDS:
        rows = summarize(args.miren, kind)
        if rows is None:
            failures.append(kind)
            print(f"\n-- {kind}  (FAILED TO QUERY -- result unreliable) "
                  + "-" * max(0, 25 - len(kind)))
            continue
        print(f"\n-- {kind}  ({len(rows)}) " + "-" * (60 - len(kind)))
        for e in rows:
            flag = "  <-- created in window" if in_window(e["created"]) else ""
            c = e["created"].isoformat() if e["created"] else "?"
            print(f"  {e['id']}   created={c}{flag}")
            for line in redact_fields(e["doc"]):
                print(f"      {line}")

    # -- Section B: routing + workloads changed during the window --
    print("\n" + "#" * 74)
    print("# B. WORKLOAD CHANGES  --  new or changed apps and versions.")
    print("#    app -> app_version is the unit that matters. Subordinate")
    print("#    byproducts (deployments, config versions, artifacts, pools)")
    print("#    aren't listed. Cross-check these against your own deploys.")
    print("#" * 74)
    hits = []
    for kind in WORKLOAD_KINDS:
        res = summarize(args.miren, kind)
        if res is None:
            failures.append(kind)
            print(f"\n  ! FAILED to query {kind} -- this section is incomplete")
            continue
        for e in res:
            if in_window(e["created"]):
                hits.append((e["created"], kind, "created", e["id"], e["name"]))
            elif in_window(e["updated"]):
                hits.append((e["updated"], kind, "updated", e["id"], e["name"]))
    hits.sort(key=lambda h: h[0])
    if not hits:
        print("\n  (nothing created/updated in the window)")
    else:
        print(f"\n  {len(hits)} entities touched in window (oldest first):\n")
        for ts, kind, what, eid, name in hits:
            print(f"  {ts.isoformat()}  {what:7}  {kind:15} {eid}  {name}")
    # -- Section C: rotation worklist (secrets an attacker could have read) --
    print("\n" + "#" * 74)
    print("# C. ROTATION CHECKLIST  --  secrets that were reachable via the")
    print("#    bypass. Not 'what was taken' (reads leave no trace) -- just")
    print("#    what's prudent to rotate, since you can't confirm it wasn't")
    print("#    read. (variable names only; values are never printed.)")
    print("#" * 74)
    by_app = {}
    cfg_docs = run_list(args.miren, "config_version")
    if cfg_docs is None:
        failures.append("config_version")
        print("\n  ! FAILED to query config_version -- rotation list is INCOMPLETE")
    else:
        unmarked = {}  # app -> {key: (reasons, strong)}
        for doc in cfg_docs:
            app = record_app(doc)
            keys = sensitive_var_keys(doc)
            if keys:
                by_app.setdefault(app, set()).update(keys)
            spec = facet_fields(doc, "/config_version").get("spec")
            for key, reasons, strong in unmarked_candidates(spec):
                slot = unmarked.setdefault(app, {})
                prev_reasons, prev_strong = slot.get(key, ([], False))
                slot[key] = (sorted(set(prev_reasons + reasons)), prev_strong or strong)
        if by_app:
            print("\n  app env vars flagged sensitive (rotate these):\n")
            for app in sorted(by_app):
                print(f"  {app}")
                for k in sorted(by_app[app]):
                    print(f"      - {k}")
        else:
            print("\n  (no variables flagged sensitive found)")

        # C2: variables that look secret but were NOT flagged sensitive. Heuristic
        # review list (values never printed). '!' marks a value-shape hit (high
        # confidence); an unmarked line means a name-only hit (worth a glance).
        print("\n  MAYBE-MISSED SECRETS -- these look like secrets but nobody marked")
        print("  them sensitive, so the list above skipped them. Look at each one:")
        print("  if it really is a secret, rotate it too (and mark it sensitive so")
        print("  it's not missed next time). A \"!\" means the value itself looks like")
        print("  a credential; otherwise it's the name that looks secret. As always,")
        print("  the actual values are never shown.")
        if unmarked:
            for app in sorted(unmarked):
                rows = unmarked[app]
                print(f"\n  {app}")
                for key in sorted(rows, key=lambda k: (not rows[k][1], k)):
                    reasons, strong = rows[key]
                    print(f"    {'!' if strong else ' '} {key}  ({', '.join(reasons)})")
        else:
            print("\n  (none detected)")
    print("\n  ALSO rotate (stored readable in the entity store, see Section A):")
    print("    - oidc_provider client_secret(s)")
    print("    - password_provider password_hash(es)")
    print("    - any addon (postgres/mysql/valkey/...) credentials")
    print("    - app config values NOT flagged sensitive but secret in practice")

    print("\n" + "=" * 74)
    if failures:
        uniq = ", ".join(sorted(set(failures)))
        print(f" INCOMPLETE: could not query {len(set(failures))} kind(s): {uniq}")
        print(" Some queries failed, so this run did NOT cover everything. Treat it")
        print(" as unreliable, not clean, and re-run once the CLI/cluster is healthy.")
        print("-" * 74)
    print(" done. Section A: look it over. Section B: cross-check your deploys.")
    print(" Section C: rotate as a precaution. A quiet result isn't proof that")
    print(" nothing happened -- reads leave no trace -- so if :8443 was reachable")
    print(" from untrusted networks in the window, presuming a leak and rotating")
    print(" is the safe call.")
    print("=" * 74)


if __name__ == "__main__":
    main()
