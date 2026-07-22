#!/usr/bin/env python3
"""
Measure per-test execution time for the blackbox suite.

The blackbox suite is a single Go package, so unlike unit tests we time
individual test functions rather than packages. This runs the full suite once
with -v and parses the `--- PASS/FAIL/SKIP: TestName (N.NNs)` lines that
`go test` emits, writing hack/blackbox-test-times.json for the shard bin-packer
(hack/calc-blackbox-groups.py) to consume.

Requires a running dev environment (make dev) since the tests exercise a real
cluster. TestPOP and the distributed-runner tests are excluded: they run in
different environments and self-skip here.

You can also parse an existing `go test -v` log instead of running the suite,
which is handy for seeding from a CI run:

  ./hack/measure-blackbox-times.py --from-log blackbox.log
  ./hack/measure-blackbox-times.py                 # runs the suite itself
"""

import argparse
import json
import re
import subprocess
import sys

RESULT_RE = re.compile(r'^\s*--- (PASS|FAIL|SKIP): (Test\w+) \(([\d.]+)s\)')

# Excluded from the standalone timing set: TestPOP runs in its own job, and the
# distributed-runner tests self-skip outside the peers topology.
EXCLUDE_PREFIXES = ("TestPOP", "TestDistributedRunner")


def parse_log(lines):
    tests = {}
    for line in lines:
        m = RESULT_RE.match(line)
        if not m:
            continue
        status, name, elapsed = m.group(1), m.group(2), float(m.group(3))
        if name.startswith(EXCLUDE_PREFIXES):
            continue
        # Keep the last observation if a name somehow repeats.
        tests[name] = {
            "name": name,
            "environment": "standalone",
            "elapsed_s": elapsed,
            "status": status.lower(),
        }
    return sorted(tests.values(), key=lambda t: -t["elapsed_s"])


def run_suite():
    """Run the full blackbox suite with -v and stream its output back."""
    cmd = ["go", "test", "-tags", "blackbox", "-timeout", "15m", "-v",
           "-count=1", "-p", "1", "-skip", "^TestPOP$", "./blackbox/..."]
    print(f"Running: {' '.join(cmd)}\n", file=sys.stderr)
    proc = subprocess.run(cmd, capture_output=True, text=True)
    sys.stderr.write(proc.stderr)
    if proc.returncode != 0:
        print(f"\nWarning: suite exited {proc.returncode}; timing data may be "
              f"incomplete for failed tests.", file=sys.stderr)
    return proc.stdout.splitlines()


def main():
    parser = argparse.ArgumentParser(description="Measure blackbox per-test times")
    parser.add_argument("-o", "--output", default="hack/blackbox-test-times.json",
                        help="Output file (default: hack/blackbox-test-times.json)")
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
        lines = run_suite()
        source = args.source or "local `go test -tags blackbox -v` run"

    tests = parse_log(lines)
    if not tests:
        print("Error: no test timings found in output.", file=sys.stderr)
        sys.exit(1)

    out = {
        "summary": {
            "total_test_time_s": round(sum(t["elapsed_s"] for t in tests), 2),
            "test_count": len(tests),
            "source": source,
        },
        "tests": tests,
    }
    with open(args.output, "w") as f:
        json.dump(out, f, indent=2)
        f.write("\n")

    print(f"Wrote {len(tests)} test timings "
          f"({out['summary']['total_test_time_s']:.1f}s total) to {args.output}",
          file=sys.stderr)


if __name__ == "__main__":
    main()
