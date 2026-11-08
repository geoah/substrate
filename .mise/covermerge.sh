#!/usr/bin/env bash
#
# Merge Go coverage profiles into one, block by block.
#
#   .mise/covermerge.sh coverage.out part1.out part2.out ...
#
# A text profile is a `mode:` line, then one line per block a test binary
# reports: `file:start.col,end.col statements count`. Built with -coverpkg,
# every binary reports every block of the module, so `go test ./...` over
# 46 packages writes 46 copies of each, and concatenating the CI jobs' parts
# made a 160 MB file of 47,000 blocks. `go tool cover` adds up a block it
# reads more than once, so summing each block's count here gives the same
# report from a file one fiftieth the size: `go tool cover -func` over the
# concatenated profile of main's run of 2026-11-08 and over this merge of it
# were byte for byte the same.
#
# The modes must agree: a `set` count is 0 or 1 and an `atomic` one is a
# count, and a sum of the two means neither. The output may be one of the
# inputs. Prints the total, which is `go tool cover -func`'s last line.
set -euo pipefail

[ "$#" -ge 2 ] || { echo "usage: $0 <out> <profile>..." >&2; exit 2; }
out="$1"
shift
for f in "$@"; do
  [ -s "$f" ] || { echo "covermerge: ${f} is missing or empty" >&2; exit 1; }
done

tmp="$(mktemp "${out}.XXXXXX")"
trap 'rm -f "$tmp"' EXIT
awk '
  function fail(msg) { print "covermerge: " msg > "/dev/stderr"; failed = 1; exit 1 }
  FNR == 1 {
    if ($1 != "mode:") fail(FILENAME " is not a coverage profile")
    if (mode == "") mode = $0
    else if ($0 != mode) fail(FILENAME " is " $0 ", the first profile " mode)
    next
  }
  NF != 3 { fail(FILENAME ":" FNR " is not a block") }
  {
    block = $1 " " $2
    if (!(block in count)) order[++n] = block
    # set: covered or not, so a block is 1 if any profile ran it.
    count[block] = mode == "mode: set" ? (count[block] || $3 > 0) : count[block] + $3
  }
  END {
    if (failed) exit 1
    print mode
    for (i = 1; i <= n; i++) print order[i], count[order[i]]
  }
' "$@" >"$tmp"
chmod 0644 "$tmp"
mv "$tmp" "$out"
trap - EXIT
echo "covermerge: $# profile(s) into ${out}"
go tool cover -func="$out" | tail -1
