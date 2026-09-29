#!/usr/bin/env bash
#
# Cuts a CI job's Go build cache down to what that one run read or wrote,
# before .github/workflows/ci.yml saves it.
#
# Why it exists: every push to main saves a fresh entry, and each run starts
# from the newest one. Without a cut, an entry keeps every object any earlier
# commit compiled, until the go command's own trim drops those unused for five
# days: `go test`'s entry grew 28 MB and `cross compile`'s 72 MB on one engine
# change, against a 10 GB budget shared by every job and every commit.
#
# `age` runs after the restore and sets every entry's mtime two hours back.
# The go command sets an entry's mtime to now when it reads it and the mtime
# is over an hour old (mtimeInterval in cmd/go/internal/cache), and writes new
# entries at now. `prune` runs before the save and removes every entry still
# older than 100 minutes, which is every entry this run did not touch: no
# job's timeout reaches 100 minutes.
#
# An entry is a child of one of the 256 two-hex-digit directories, never a
# file found by recursion: a cached `go run` executable is a `<id>-d`
# DIRECTORY whose own mtime the go command bumps while the file inside keeps
# its old one. Everything else at the top (README, trim.txt, a fuzz corpus
# under fuzz/) is not an entry and is left alone.
#
# The Go build cache alone. golangci-lint's cache is not cut: a cached package
# result is read without its dependencies' facts, so what one run reads is not
# what the next run needs. The module cache changes only with go.sum.
#
#   .mise/gocache.sh age ~/.cache/go-build
#   .mise/gocache.sh prune ~/.cache/go-build
set -euo pipefail

usage() {
  echo "usage: $0 age|prune <go build cache directory>" >&2
  exit 2
}

[ "$#" -eq 2 ] || usage
verb="$1"
dir="$2"
case "$dir" in
/*/*) ;;
*)
  echo "gocache: ${dir} is not an absolute path below /" >&2
  exit 2
  ;;
esac

# A job that ran no go command has no cache to cut, and the save after it
# says so itself.
if [ ! -d "$dir" ]; then
  echo "gocache: ${dir} does not exist; nothing to ${verb}"
  exit 0
fi

entries() { find "$dir" -mindepth 2 -maxdepth 2 -path "${dir}/[0-9a-f][0-9a-f]/*" "$@"; }
count() { entries | wc -l; }

case "$verb" in
age)
  entries -exec touch -m -d '2 hours ago' {} +
  echo "gocache: aged $(count) entries in ${dir}"
  ;;
prune)
  before="$(count)"
  # A fresh `-a` can name an output this run never bumped: when a new action's
  # output matches bytes already cached (the empty output, for one), the go
  # command writes the `-a` and leaves the `-d` as it found it (copyFile
  # returns early), still aged. So every `-d` a kept `-a` names is kept. An
  # `-a` is `v1 <action> <output> <size> <time>`, and the output's entry is
  # `<first two hex digits>/<output>-d`.
  entries -name '*-a' -mmin -100 -exec cat {} + |
    awk '$1 == "v1" { print substr($3, 1, 2) "/" $3 "-d" }' |
    (cd "$dir" && xargs -r touch -m -c --)
  entries -mmin +100 -exec rm -rf {} +
  after="$(count)"
  echo "gocache: kept ${after} of ${before} entries in ${dir}, $(du -sh "$dir" | cut -f1)"
  ;;
*) usage ;;
esac
