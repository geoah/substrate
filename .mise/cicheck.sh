#!/usr/bin/env bash
#
# The CI scripts' own tests.
#
# Two scripts decide what the database suite runs: .mise/changescheck.sh
# decides whether it runs at all, and .mise/shardselect.sh decides which tests
# each engine shard runs. A wrong answer from either is a green build that
# tested nothing, which no other check would see. So each scenario below is
# a throwaway git repository the gate is run against, with the verdict it must
# give, and the partition is run over a fixed list that every shard together
# must reproduce exactly once.
#
# No `-e` around the assertions: every scenario is reported, not just the
# first to break. The scripts under test run with their own `set -e`.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 2
changescheck="$PWD/.mise/changescheck.sh"
shardselect="$PWD/.mise/shardselect.sh"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

fail=0
flag() {
  fail=1
  printf 'lint:ci: %s\n' "$*" >&2
}

# --- the path gate -------------------------------------------------------

# A repository with one commit on main holding a file of every class the
# gate distinguishes, so a scenario is one branch and one commit off it.
repo="$tmp/repo"
git init --quiet --initial-branch=main "$repo"
g() { git -C "$repo" -c user.name=ci -c user.email=ci@example.com -c commit.gpgsign=false "$@"; }
seed() {
  mkdir -p "$repo/$(dirname "$1")"
  printf '%s\n' "${2:-seed}" >"$repo/$1"
}
seed internal/engine/x.go 'package engine'
seed docs/a.md
seed kinds/core/k.yaml 'kind: x'
seed web/console/p.json '{}'
seed .github/workflows/ci.yml 'name: ci'
seed .github/workflows/release.yml 'name: release'
seed README.md
seed Dockerfile 'FROM scratch'
seed Dockerfile.release 'FROM scratch'
seed .goreleaser.yaml 'version: 2'
seed compose.yaml 'services: {}'
seed web/console/src/lib/api/wire.golden.json '{}'
seed web/console/src/lib/record-schema.ts 'export {}'
g add -A && g commit --quiet -m base

# gate <env...>: run the gate in the repository's working tree with a
# CONTROLLED environment; capture stdout, the exit status and the
# GITHUB_OUTPUT lines. Every variable changescheck.sh reads is unset first and
# a scenario sets exactly what it means to: this test runs inside the lint job
# on a push to main, where the runner's own GITHUB_EVENT_NAME=push would make
# every scenario answer "a push runs every job" and its CI=true would turn the
# laptop fallback into a refusal. The PR runs never saw that, main did.
scrub=(env -u GITHUB_EVENT_NAME -u GITHUB_BASE_REF -u GITHUB_REF -u CI -u GITHUB_OUTPUT -u CHANGES_CHECK_BASE)
gate_out=""
gate_status=0
gate_output_lines=0
gate() {
  local output="$tmp/output"
  : >"$output"
  gate_out="$(cd "$repo" && "${scrub[@]}" GITHUB_OUTPUT="$output" "$@" "$changescheck" 2>"$tmp/stderr")"
  gate_status=$?
  gate_output_lines="$(grep -c . "$output")"
}

# scenario <name> <expected go=> <setup...>: a branch off main, the setup
# applied and committed, the gate run against `main`, the tree reset.
scenario() {
  local name="$1" expected="$2"
  shift 2
  g checkout --quiet -b "$name" main
  "$@"
  g add -A && g commit --quiet --allow-empty -m "$name"
  gate CHANGES_CHECK_BASE=main
  case "$gate_out" in
  *"go=${expected}"*) ;;
  *) flag "${name}: expected go=${expected}, got '${gate_out}' (exit ${gate_status})" ;;
  esac
  [ "$gate_status" -eq 0 ] || flag "${name}: exit ${gate_status}, expected 0"
  [ "$gate_output_lines" -eq 1 ] || flag "${name}: GITHUB_OUTPUT has ${gate_output_lines} lines, expected exactly one go= line"
  g checkout --quiet main
}

touch_file() { printf 'changed\n' >>"$repo/$1"; }
# Invoked through `scenario`'s arguments, which shellcheck cannot follow.
# shellcheck disable=SC2329
touch_two() {
  touch_file "$1"
  touch_file "$2"
}

scenario docs-only false touch_file docs/a.md
scenario console-and-other-workflow false touch_two web/console/p.json .github/workflows/release.yml
scenario root-markdown false touch_file README.md
scenario go-file true touch_file internal/engine/x.go
scenario kinds-readme true seed kinds/core/README.md
scenario the-workflow-itself true touch_file .github/workflows/ci.yml
scenario new-unlisted-class true seed .mise/new.sh
scenario release-image-and-compose false touch_two Dockerfile.release compose.yaml
# Files a test reads across package lines: internal/build reads the
# Dockerfile and .goreleaser.yaml, internal/substrate and internal/vocabulary
# read two files under the console tree that is otherwise inert.
scenario dockerfile true touch_file Dockerfile
scenario goreleaser true touch_file .goreleaser.yaml
scenario wire-golden true touch_file web/console/src/lib/api/wire.golden.json
scenario record-schema true touch_file web/console/src/lib/record-schema.ts
# A rename onto an inert path: git's rename detection would report only the
# destination, docs/x.md, and the gate would wave a deleted Go file through.
scenario rename-go-onto-docs true g mv internal/engine/x.go docs/x.md

# The base resolved from the branch rather than CHANGES_CHECK_BASE: `main`
# exists and origin/main does not, which is the laptop case.
g checkout --quiet -b resolved-base main
touch_file docs/a.md
g add -A && g commit --quiet -m resolved-base
gate
case "$gate_out" in *"go=false"*) ;; *) flag "resolved-base: expected go=false via the merge base with main, got '${gate_out}'" ;; esac
g checkout --quiet main

# An untracked file counts too: a hand run sees the file just created.
seed samples/new/thing.yaml 'kind: y'
gate CHANGES_CHECK_BASE=main
case "$gate_out" in *"go=true"*) ;; *) flag "untracked samples file: expected go=true, got '${gate_out}'" ;; esac
rm -rf "$repo/samples/new"

# A push answers true without a diff, whatever the tree holds.
gate GITHUB_EVENT_NAME=push
case "$gate_out" in *"go=true"*) ;; *) flag "push: expected go=true, got '${gate_out}'" ;; esac

# A base that names no commit is a FAILURE, never a verdict: exit non-zero
# and no go= line written, so the workflow's gate goes red.
gate CHANGES_CHECK_BASE=definitely-not-a-commit
[ "$gate_status" -ne 0 ] || flag "unresolvable base: exit 0, expected a failure"
[ "$gate_output_lines" -eq 0 ] || flag "unresolvable base: GITHUB_OUTPUT got a go= line, expected none"
case "$gate_out" in *"go="*) flag "unresolvable base: printed a verdict '${gate_out}'" ;; esac

# In CI with no base branch at all (no origin/main, no main): refuse.
lone="$tmp/lone"
git init --quiet --initial-branch=work "$lone"
printf 'x\n' >"$lone/f"
git -C "$lone" -c user.name=ci -c user.email=ci@example.com -c commit.gpgsign=false add -A
git -C "$lone" -c user.name=ci -c user.email=ci@example.com -c commit.gpgsign=false commit --quiet -m lone
if (cd "$lone" && "${scrub[@]}" CI=1 GITHUB_OUTPUT="$tmp/lone-output" "$changescheck" >/dev/null 2>&1); then
  flag "CI without a base branch: exit 0, expected a refusal"
fi

# --- the shard partition ------------------------------------------------

# 23 names, handed over in reverse: the partition sorts, and 23 does not
# divide by 8, so the shards are uneven by one and the last ones are short.
# A Fuzz target and an Example are in the list, because engineshard.sh keeps
# them and they must land in a shard like any Test.
names="$(printf 'FuzzParse\nExampleOpen\n'; for i in $(seq 21 -1 1); do printf 'Test%02d\n' "$i"; done)"
sorted="$(printf '%s\n' "$names" | LC_ALL=C sort)"
union=""
for k in 1 2 3 4 5 6 7 8; do
  if ! part="$(printf '%s\n' "$names" | SHARD=$k SHARDS=8 "$shardselect" 2>"$tmp/stderr")"; then
    flag "shard ${k}/8 failed: $(cat "$tmp/stderr")"
    continue
  fi
  count="$(printf '%s\n' "$part" | grep -c .)"
  [ "$count" -eq 3 ] || [ "$count" -eq 2 ] || flag "shard ${k}/8 has ${count} names, expected 2 or 3"
  union="${union}${part}"$'\n'
done
union_sorted="$(printf '%s' "$union" | grep . | LC_ALL=C sort)"
[ "$union_sorted" = "$sorted" ] || flag "the eight shards together are not the list: $(diff <(printf '%s\n' "$sorted") <(printf '%s\n' "$union_sorted") | head -5)"
dupes="$(printf '%s\n' "$union_sorted" | uniq -d)"
[ -z "$dupes" ] || flag "a name is in two shards: ${dupes}"

# The same list cuts the same way whatever order it arrives in.
a="$(printf '%s\n' "$names" | SHARD=3 SHARDS=8 "$shardselect")"
b="$(printf '%s\n' "$sorted" | SHARD=3 SHARDS=8 "$shardselect")"
[ "$a" = "$b" ] || flag "shard 3/8 depends on the input order"

# Refusals: out of range and not a number exit 2, an empty shard exits 1.
printf '%s\n' "$names" | SHARD=9 SHARDS=8 "$shardselect" >/dev/null 2>&1
[ $? -eq 2 ] || flag "SHARD=9 SHARDS=8 was not refused with exit 2"
printf '%s\n' "$names" | SHARD=x SHARDS=8 "$shardselect" >/dev/null 2>&1
[ $? -eq 2 ] || flag "SHARD=x was not refused with exit 2"
printf '%s\n' "$names" | SHARD=0 SHARDS=8 "$shardselect" >/dev/null 2>&1
[ $? -eq 2 ] || flag "SHARD=0 was not refused with exit 2"
printf '%s\n' "$names" | SHARD=30 SHARDS=30 "$shardselect" >/dev/null 2>&1
[ $? -eq 1 ] || flag "an empty shard (30/30 of 23 names) did not exit 1"

exit "$fail"
