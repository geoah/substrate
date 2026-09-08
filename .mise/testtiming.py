"""Print where a `go test -json` run spent its time.

    go test -count=1 -p 1 -json ./internal/engine/... > timing.json
    mise run test:timing -- timing.json          # or: python3 .mise/testtiming.py timing.json 30

One line per package (wall time, test count, sum and median of the top-level
tests), then the N slowest top-level tests across every package. Subtests are
folded into their parent: the parent's Elapsed already includes them.
"""

import json
import statistics
import sys


def main(argv):
    path = argv[1] if len(argv) > 1 else "/dev/stdin"
    top = int(argv[2]) if len(argv) > 2 else 30
    tests = {}
    packages = {}
    with open(path) as f:
        for line in f:
            try:
                e = json.loads(line)
            except ValueError:
                continue
            action = e.get("Action")
            if action not in ("pass", "fail", "skip"):
                continue
            pkg = e.get("Package", "")
            name = e.get("Test")
            if not name:
                packages[pkg] = (action, e.get("Elapsed", 0.0))
            elif "/" not in name:
                tests[(pkg, name)] = (action, e.get("Elapsed", 0.0))
    if not packages:
        sys.exit("no package result in %s: is it the output of go test -json?" % path)
    for pkg in sorted(packages):
        action, wall = packages[pkg]
        mine = [t[1] for (p, _), t in tests.items() if p == pkg]
        median = statistics.median(mine) if mine else 0.0
        print(
            "%-60s %-4s wall %7.1fs  tests %4d  sum %7.1fs  median %5.2fs"
            % (pkg, action, wall, len(mine), sum(mine), median)
        )
    print()
    slowest = sorted(tests.items(), key=lambda kv: -kv[1][1])[:top]
    for (pkg, name), (action, elapsed) in slowest:
        short = pkg.rsplit("/", 1)[-1]
        print("%8.2fs  %-4s %s (%s)" % (elapsed, action, name, short))


if __name__ == "__main__":
    main(sys.argv)
