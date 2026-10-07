#!/usr/bin/env python3
"""
Calculate optimal test package groupings for parallel CI runners.

Uses LPT (Longest Processing Time) bin-packing to distribute Go test packages
across N runners, minimizing total wall-clock time.

Discovers all current test packages via `go list` so that newly added packages
are included even if they have no timing data yet. New packages are distributed
across runners 2..N to keep runner 1's estimate stable.

Each runner pays the harness overhead once, so grouping many fast packages
together saves significant time.

Usage:
  ./hack/calc-test-groups.py test-times.json -n 4 -o test-groups.json
  ./hack/calc-test-groups.py test-times.json --sweep        # try 1-12 runners
"""

import argparse
import json
import subprocess
import sys


def load_times(path):
    with open(path) as f:
        data = json.load(f)
    overhead = data["summary"]["harness_overhead_s"]
    packages = data["packages"]
    return overhead, packages


def discover_test_packages():
    """Run `go list` to find all packages that contain test files."""
    result = subprocess.run(
        ["go", "list", "-f",
         "{{.ImportPath}} {{len .TestGoFiles}} {{len .XTestGoFiles}}",
         "./...", "miren.dev/runtime/x/..."],
        capture_output=True, text=True,
    )
    if result.returncode != 0:
        print(f"Warning: go list failed: {result.stderr}", file=sys.stderr)
        return []
    pkgs = []
    for line in result.stdout.strip().splitlines():
        parts = line.split()
        if len(parts) == 3 and (int(parts[1]) > 0 or int(parts[2]) > 0):
            pkgs.append(parts[0])
    return sorted(pkgs)


def pure_test_time(pkg, overhead):
    """Estimate actual test time by subtracting harness overhead."""
    if isinstance(pkg, str):
        return 0.01
    return max(pkg["elapsed_s"] - overhead, 0.01)


def pack_lpt(packages, n_runners, overhead, new_packages=None):
    """
    Longest Processing Time first bin-packing.

    Sort packages by descending pure test time, assign each to the
    runner with the lowest current total. Each runner gets one overhead
    charge for its harness startup.

    New packages (no timing data) are distributed across runners 2..N only.
    """
    pkgs = sorted(packages, key=lambda p: pure_test_time(p, overhead), reverse=True)

    runners = [[] for _ in range(n_runners)]
    runner_times = [0.0] * n_runners

    for pkg in pkgs:
        t = pure_test_time(pkg, overhead)
        # assign to least-loaded runner
        i = runner_times.index(min(runner_times))
        runners[i].append(pkg)
        runner_times[i] += t

    # Distribute new packages across runners 2..N (or the sole runner).
    if new_packages:
        first = 1 if n_runners > 1 else 0
        for pkg in new_packages:
            subset = runner_times[first:]
            i = subset.index(min(subset)) + first
            runners[i].append(pkg)
            runner_times[i] += pure_test_time(pkg, overhead)

    # Add overhead once per runner
    runner_totals = [t + overhead for t in runner_times]
    return runners, runner_totals


def short_name(pkg_path):
    return pkg_path.removeprefix("miren.dev/runtime/")


def pkg_name(pkg):
    """Extract the package path string from either a dict or string."""
    if isinstance(pkg, dict):
        return pkg["package"]
    return pkg


def print_groups(runners, runner_totals, overhead):
    makespan = max(runner_totals)

    print(f"\n{'='*70}")
    print(f"  {len(runners)} runners | makespan: {makespan:.1f}s | "
          f"harness overhead: {overhead:.1f}s/runner")
    print(f"{'='*70}")

    for i, (pkgs, total) in enumerate(zip(runners, runner_totals)):
        test_time = total - overhead
        print(f"\n  Runner {i+1}: {total:.1f}s total "
              f"({overhead:.1f}s overhead + {test_time:.1f}s tests, "
              f"{len(pkgs)} packages)")
        for p in sorted(pkgs, key=lambda p: p.get("elapsed_s", 0) if isinstance(p, dict) else 0, reverse=True):
            if isinstance(p, dict):
                t = p["elapsed_s"] - overhead
                print(f"    {t:6.1f}s  {short_name(p['package'])}")
            else:
                print(f"      new  {short_name(p)}")


def print_sweep(packages, overhead, max_runners=12):
    serial_time = sum(p["elapsed_s"] for p in packages)
    print(f"\nSerial time (all packages, no grouping): {serial_time:.1f}s")
    print(f"Harness overhead per invocation: {overhead:.1f}s")
    print(f"Total packages: {len(packages)}")
    print(f"\n{'Runners':>8} {'Makespan':>10} {'Speedup':>9} {'Efficiency':>11}")
    print(f"{'':>8} {'':>10} {'vs serial':>9} {'':>11}")
    print(f"{'-'*42}")

    for n in range(1, max_runners + 1):
        _, totals = pack_lpt(packages, n, overhead)
        makespan = max(totals)
        speedup = serial_time / makespan
        efficiency = speedup / n * 100
        print(f"{n:>8} {makespan:>9.1f}s {speedup:>8.1f}x {efficiency:>9.0f}%")

        # Stop if makespan is dominated by the single largest package
        if makespan <= packages[0]["elapsed_s"] - overhead + overhead + 1:
            remaining = max_runners - n
            if remaining > 0:
                print(f"  ... ({remaining} more runners won't help, "
                      f"bottleneck is {short_name(packages[0]['package'])})")
            break


def build_output(runners, runner_totals, overhead):
    """Build JSON output with runner groups."""
    groups = []
    for i, (pkgs, total) in enumerate(zip(runners, runner_totals)):
        groups.append({
            "runner": i + 1,
            "estimated_s": round(total, 2),
            "packages": [pkg_name(p) for p in pkgs],
        })
    return {
        "n_runners": len(runners),
        "makespan_s": round(max(runner_totals), 2),
        "overhead_per_runner_s": overhead,
        "groups": groups,
    }


def main():
    parser = argparse.ArgumentParser(description="Calculate CI test groupings")
    parser.add_argument("input", help="test-times.json from measure-test-times.py")
    parser.add_argument("-n", "--runners", type=int, default=None,
                        help="Number of parallel runners")
    parser.add_argument("--sweep", action="store_true",
                        help="Show makespan for 1..12 runners")
    parser.add_argument("--max-sweep", type=int, default=12,
                        help="Max runners for sweep (default: 12)")
    parser.add_argument("-o", "--output", help="Write groups JSON to file")
    parser.add_argument("--overhead", type=float, default=None,
                        help="Override harness overhead (default: from input)")
    parser.add_argument("--no-discover", action="store_true",
                        help="Skip go list discovery (use only test-times.json)")
    args = parser.parse_args()

    overhead, packages = load_times(args.input)
    if args.overhead is not None:
        overhead = args.overhead

    # Discover new packages via go list
    new_packages = []
    if not args.no_discover:
        known = {p["package"] for p in packages}
        discovered = discover_test_packages()
        new_pkgs = [p for p in discovered if p not in known]
        if new_pkgs:
            print(f"Discovered {len(new_pkgs)} new package(s) not in timing data:")
            for p in new_pkgs:
                print(f"  {short_name(p)}")
            # Represent new packages as strings (no timing data)
            new_packages = new_pkgs

        # Also warn about packages in timing data that no longer exist
        discovered_set = set(discovered)
        stale = [p for p in packages if p["package"] not in discovered_set]
        if stale:
            print(f"\nWarning: {len(stale)} package(s) in timing data no longer exist:")
            for p in stale:
                print(f"  {short_name(p['package'])}")
            packages = [p for p in packages if p["package"] in discovered_set]

    if args.sweep or args.runners is None:
        print_sweep(packages, overhead, args.max_sweep)
        if args.runners is None and not args.sweep:
            print("\nUse -n N to see detailed groupings for N runners.")
            return

    if args.runners:
        runners, totals = pack_lpt(packages, args.runners, overhead,
                                   new_packages=new_packages)
        print_groups(runners, totals, overhead)

        if args.output:
            out = build_output(runners, totals, overhead)
            with open(args.output, "w") as f:
                json.dump(out, f, indent=2)
            print(f"\nGroups written to {args.output}")


if __name__ == "__main__":
    main()
