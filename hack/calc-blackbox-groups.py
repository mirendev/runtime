#!/usr/bin/env python3
"""
Calculate balanced blackbox test groupings for parallel CI shards.

The blackbox suite is a single Go package, so it can't be split across
runners by package the way unit tests are (see hack/calc-test-groups.py).
Instead we split by *test name*: each shard runs a subset via
`go test -run '^(TestA|TestB|...)$'`.

Tests are partitioned first by the environment they need (standalone, pop,
peers, ...) and then LPT-bin-packed within each environment, since a single
shard boots exactly one environment. Today only the standalone pool is large
enough to shard and wired into CI; the environment dimension keeps the tooling
ready to shard the others by config if they ever grow.

Discovers the current test set via `go test -list` so newly added tests are
included even before they have timing data. Untimed tests are assigned to the
lightest shard so they always run somewhere.

Usage:
  ./hack/calc-blackbox-groups.py hack/blackbox-test-times.json \
      --env standalone -n 3 -o hack/blackbox-groups.json
"""

import argparse
import json
import subprocess
import sys

# Cloud-backed tests never participate in any sharded job, in either
# environment: they need the cloud repo + secret and restart the server
# mid-run, so they run in their own CI job (test-blackbox-pop). This mirrors the
# `-skip` list in the Makefile and the ungrouped-test filter in
# .github/workflows/test.yml; without it, `-list` discovery would fold them into
# the lightest shard as untimed tests.
ALWAYS_SKIP = {"TestPOP", "TestRPCViaCloud", "TestDeployViaCloud"}


def load_times(path):
    with open(path) as f:
        data = json.load(f)
    return data["tests"]


def discover_tests():
    """Run `go test -list` to find all blackbox test functions.

    Compiles the test binary (needs the blackbox build tag) but does not run
    any test, so it needs no cluster.
    """
    result = subprocess.run(
        ["go", "test", "-tags", "blackbox", "-list", ".*", "./blackbox/..."],
        capture_output=True, text=True,
    )
    if result.returncode != 0:
        print(f"Warning: go test -list failed: {result.stderr}", file=sys.stderr)
        return []
    names = []
    for line in result.stdout.strip().splitlines():
        line = line.strip()
        if line.startswith("Test"):
            names.append(line)
    return sorted(names)


def pack_lpt(tests, n_shards, new_tests=None):
    """Longest Processing Time first bin-packing.

    Sort tests by descending time, assign each to the currently lightest
    shard. Untimed (new) tests are appended to the lightest shard as we go.
    """
    tests = sorted(tests, key=lambda t: t["elapsed_s"], reverse=True)

    shards = [[] for _ in range(n_shards)]
    shard_times = [0.0] * n_shards

    for t in tests:
        i = shard_times.index(min(shard_times))
        shards[i].append(t)
        shard_times[i] += t["elapsed_s"]

    for name in (new_tests or []):
        i = shard_times.index(min(shard_times))
        shards[i].append({"name": name, "elapsed_s": 0.0})
        # a fresh test's real cost is unknown; nudge so they spread out
        shard_times[i] += 0.01

    return shards, shard_times


def build_output(shards, shard_times, environment):
    groups = []
    for i, (tests, total) in enumerate(zip(shards, shard_times)):
        groups.append({
            "shard": i + 1,
            "estimated_s": round(total, 2),
            "tests": [t["name"] for t in
                      sorted(tests, key=lambda t: -t["elapsed_s"])],
        })
    return {
        "environment": environment,
        "n_shards": len(shards),
        "makespan_s": round(max(shard_times), 2),
        "groups": groups,
    }


def print_groups(output):
    print(f"\n{'='*66}")
    print(f"  {output['environment']}: {output['n_shards']} shards | "
          f"makespan (test time only): {output['makespan_s']:.1f}s")
    print(f"{'='*66}")
    for g in output["groups"]:
        print(f"\n  Shard {g['shard']}: {g['estimated_s']:.1f}s "
              f"({len(g['tests'])} tests)")
        for name in g["tests"]:
            print(f"    {name}")


def main():
    parser = argparse.ArgumentParser(description="Calculate blackbox shard groupings")
    parser.add_argument("input", help="blackbox-test-times.json")
    parser.add_argument("--env", default="standalone",
                        help="Environment pool to shard (default: standalone)")
    parser.add_argument("-n", "--shards", type=int, default=3,
                        help="Number of parallel shards (default: 3)")
    parser.add_argument("-o", "--output", help="Write groups JSON to file")
    parser.add_argument("--no-discover", action="store_true",
                        help="Skip go test -list discovery (use only timing data)")
    args = parser.parse_args()

    all_tests = load_times(args.input)
    tests = [t for t in all_tests
             if t.get("environment", "standalone") == args.env
             and t["name"] not in ALWAYS_SKIP]

    new_tests = []
    if not args.no_discover:
        known = {t["name"] for t in all_tests} | ALWAYS_SKIP
        discovered = discover_tests()
        new = [n for n in discovered if n not in known]
        if new:
            print(f"Discovered {len(new)} test(s) with no timing data "
                  f"(assigned to lightest shard):")
            for n in new:
                print(f"  {n}")
            new_tests = new

        # Warn about timed tests that no longer exist.
        discovered_set = set(discovered)
        if discovered_set:
            stale = [t["name"] for t in tests if t["name"] not in discovered_set]
            if stale:
                print(f"\nWarning: {len(stale)} timed test(s) no longer exist:")
                for n in stale:
                    print(f"  {n}")
                tests = [t for t in tests if t["name"] in discovered_set]

    shards, shard_times = pack_lpt(tests, args.shards, new_tests=new_tests)
    output = build_output(shards, shard_times, args.env)
    print_groups(output)

    if args.output:
        with open(args.output, "w") as f:
            json.dump(output, f, indent=2)
            f.write("\n")
        print(f"\nGroups written to {args.output}")


if __name__ == "__main__":
    main()
