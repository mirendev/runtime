#!/usr/bin/env python3
"""
Measure per-test execution time for the blackbox suite.

The blackbox suite is a single Go package, so unlike unit tests we time
individual test functions rather than packages. This runs the full suite once
with -v and parses the `--- PASS/FAIL/SKIP: TestName (N.NNs)` lines that
`go test` emits, writing hack/blackbox-test-times.json for the shard bin-packer
(hack/calc-blackbox-groups.py) to consume.

Requires a running dev environment (make dev) since the tests exercise a real
cluster. Cloud-backed tests are always excluded from the shard timing pool.

Times are tagged with the environment they were measured in (`standalone` or
`peers`), and rows for that one environment are replaced on each run while rows
for other environments are preserved, so a single times file holds both pools
for hack/calc-blackbox-groups.py to shard by `--env`. The environment defaults
to `peers` when BLACKBOX_MODE=peers is set (matching the distributed suite),
else `standalone`; override with `--env`.

You can also parse an existing `go test -v` log instead of running the suite,
which is handy for seeding from a CI run:

  ./hack/measure-blackbox-times.py --from-log blackbox.log
  ./hack/measure-blackbox-times.py                 # runs the suite itself
  BLACKBOX_MODE=peers ./hack/measure-blackbox-times.py --from-log dist-ci.log
"""

import argparse
import json
import os
import re
import subprocess
import sys

# Matched anywhere in a line, not anchored at the start, so a raw `go test -v`
# log ("    --- PASS: TestX (1.2s)") and a `gh run view --log` dump (each line
# prefixed with "job\tUNKNOWN STEP\ttimestamp ") both parse.
RESULT_RE = re.compile(r'--- (PASS|FAIL|SKIP): (Test\w+) \(([\d.]+)s\)')

# Cloud-backed tests need a cloud checkout, so they're excluded from every
# shard environment's timing set. The TestDistributed* tests self-skip outside
# the peers topology: in standalone they're noise (a 0s skip), but in peers
# they're real work and must be timed, so they're only excluded for standalone.
def excluded_prefixes(environment):
    cloud = ("TestPOP", "TestRPCViaCloud", "TestDeployViaCloud",
             "TestServerEnrollWithToken", "TestServerUnregister")
    if environment == "peers":
        return cloud
    return cloud + ("TestDistributed",)


def parse_log(lines, environment):
    exclude = excluded_prefixes(environment)
    tests = {}
    for line in lines:
        m = RESULT_RE.search(line)
        if not m:
            continue
        status, name, elapsed = m.group(1), m.group(2), float(m.group(3))
        if name.startswith(exclude):
            continue
        # A skip carries no timing signal; the shard packer assigns untimed
        # tests to the lightest shard anyway, so drop skips rather than pin them
        # at 0s.
        if status == "SKIP":
            continue
        # Keep the last observation if a name somehow repeats.
        tests[name] = {
            "name": name,
            "environment": environment,
            "elapsed_s": elapsed,
            "status": status.lower(),
        }
    return sorted(tests.values(), key=lambda t: -t["elapsed_s"])


def run_suite(environment):
    """Run the full blackbox suite with -v and stream its output back.

    The subprocess topology is driven by `environment` so the tests actually
    run where their timings are recorded: peers sets BLACKBOX_MODE=peers,
    standalone clears it. The cloud-backed tests are excluded (same set as the
    `test-blackbox` target): they need the cloud repo and restart the server.
    """
    cmd = ["go", "test", "-tags", "blackbox", "-timeout", "15m", "-v",
           "-count=1", "-p", "1",
           "-skip", "^(TestPOP|TestRPCViaCloud|TestDeployViaCloud|TestServerEnrollWithToken|TestServerUnregister)$",
           "./blackbox/..."]
    env = os.environ.copy()
    if environment == "peers":
        env["BLACKBOX_MODE"] = "peers"
    else:
        env.pop("BLACKBOX_MODE", None)
    print(f"Running: {' '.join(cmd)}\n", file=sys.stderr)
    proc = subprocess.run(cmd, capture_output=True, text=True, env=env)
    sys.stderr.write(proc.stderr)
    if proc.returncode != 0:
        print(f"\nWarning: suite exited {proc.returncode}; timing data may be "
              f"incomplete for failed tests.", file=sys.stderr)
    return proc.stdout.splitlines()


def load_existing(path):
    """Load the times file if present, tolerating the old flat-summary shape.

    Older files stored a single `summary` object (implicitly standalone); newer
    ones key `summary` by environment. Normalize to the keyed shape so merging
    is uniform.
    """
    try:
        with open(path) as f:
            data = json.load(f)
    except FileNotFoundError:
        return {"summary": {}, "tests": []}
    summary = data.get("summary", {})
    if summary and "total_test_time_s" in summary:
        summary = {"standalone": summary}
    return {"summary": summary, "tests": data.get("tests", [])}


def main():
    default_env = "peers" if os.environ.get("BLACKBOX_MODE") == "peers" else "standalone"
    parser = argparse.ArgumentParser(description="Measure blackbox per-test times")
    parser.add_argument("-o", "--output", default="hack/blackbox-test-times.json",
                        help="Output file (default: hack/blackbox-test-times.json)")
    parser.add_argument("--env", default=default_env, choices=["standalone", "peers"],
                        help="Environment the times are measured in "
                             f"(default: {default_env}, from BLACKBOX_MODE)")
    parser.add_argument("--from-log", metavar="FILE",
                        help="Parse an existing `go test -v` log instead of running")
    parser.add_argument("--source", default=None,
                        help="Provenance note recorded in the output summary")
    args = parser.parse_args()

    if args.from_log:
        with open(args.from_log) as f:
            lines = f.read().splitlines()
        source = args.source or f"parsed from {args.from_log}"
    else:
        lines = run_suite(args.env)
        source = args.source or "local `go test -tags blackbox -v` run"

    tests = parse_log(lines, args.env)
    if not tests:
        print("Error: no test timings found in output.", file=sys.stderr)
        sys.exit(1)

    # Replace only this environment's rows; preserve the other environments'
    # timings so one file holds every shard pool.
    existing = load_existing(args.output)
    kept = [t for t in existing["tests"]
            if t.get("environment", "standalone") != args.env]
    merged = sorted(kept + tests,
                    key=lambda t: (t.get("environment", "standalone"), -t["elapsed_s"]))

    summary = existing["summary"]
    summary[args.env] = {
        "total_test_time_s": round(sum(t["elapsed_s"] for t in tests), 2),
        "test_count": len(tests),
        "source": source,
    }

    out = {"summary": summary, "tests": merged}
    with open(args.output, "w") as f:
        json.dump(out, f, indent=2)
        f.write("\n")

    print(f"Wrote {len(tests)} {args.env} test timings "
          f"({summary[args.env]['total_test_time_s']:.1f}s total) to {args.output}",
          file=sys.stderr)


if __name__ == "__main__":
    main()
