#!/usr/bin/env bash
#
# The CI scripts' own tests.
#
# .mise/llmliveissue.sh is the fifth: it keeps the weekly `llm live` job's
# one tracking issue, so a wrong answer is a second issue every week or a
# failure nobody is told about. Its scenarios run against a fake gh.
#
# .mise/decisionscheck.sh is the fourth: it decides whether a decision
# record's number is already taken on another branch, so a wrong pass is the
# collision issue #587 describes, found only after the merge.
#
# .mise/commitscheck.sh is the third: it decides whether a pull request's
# titles can be read into the changelog, so a wrong pass is a change the
# release never lists or a break that ships without its steps. Its scenarios
# are at the end.
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
commitscheck="$PWD/.mise/commitscheck.sh"
decisionscheck="$PWD/.mise/decisionscheck.sh"
llmliveissue="$PWD/.mise/llmliveissue.sh"

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
scenario compose false touch_file compose.yaml
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
# A Fuzz target and an Example are in the list, because dbshard.sh keeps
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


# --- the llm live tracking issue ---------------------------------------------

# .mise/llmliveissue.sh keeps ONE tracking issue for the weekly `llm live` job.
# A fake gh first on PATH answers the lookup with FAKE_GH_FOUND (what the real
# lookup's --jq prints: "<number> <state>" or nothing) and logs every call as
# "<command> <subcommand>" and the first all-digit argument. The repository
# name is one GitHub cannot hold, so a real gh reached by mistake fails
# instead of writing.
fakebin="$tmp/fakebin"
mkdir -p "$fakebin"
cat >"$fakebin/gh" <<'FAKE'
#!/usr/bin/env bash
n=""
for a in "$@"; do
  case "$a" in
  "" | *[!0-9]*) ;;
  *)
    n=" $a"
    break
    ;;
  esac
done
printf '%s %s%s\n' "$1" "$2" "$n" >>"$FAKE_GH_LOG"
if [ "$1 $2" = "issue list" ]; then
  printf '%s\n' "${FAKE_GH_FOUND:-}"
fi
FAKE
chmod +x "$fakebin/gh"

# issue <name> <verdict> <found> <expected writes, one per line>: the writes
# are the logged calls other than the lookup.
issue() {
  local name="$1" verdict="$2" found="$3" want="$4" got
  : >"$tmp/gh.log"
  if ! PATH="$fakebin:$PATH" FAKE_GH_LOG="$tmp/gh.log" FAKE_GH_FOUND="$found" \
    GITHUB_SERVER_URL=https://example.com GITHUB_REPOSITORY="-/-" GITHUB_RUN_ID=1 \
    "$llmliveissue" "$verdict" >/dev/null 2>"$tmp/stderr"; then
    flag "llm live issue ${name}: exit non-zero: $(cat "$tmp/stderr")"
    return
  fi
  got="$(grep -v '^issue list' "$tmp/gh.log")"
  [ "$got" = "$want" ] || flag "llm live issue ${name}: wrote '${got}', expected '${want}'"
  grep -q '^issue list' "$tmp/gh.log" || flag "llm live issue ${name}: no lookup before the write"
}

issue first-failure failure "" "issue create"
issue open-failure failure "7 OPEN" "issue comment 7"
issue closed-failure failure "7 CLOSED" "$(printf 'issue reopen 7\nissue comment 7')"
issue open-success success "7 OPEN" "issue close 7"
issue closed-success success "7 CLOSED" ""
issue none-success success "" ""
PATH="$fakebin:$PATH" FAKE_GH_LOG="$tmp/gh.log" GITHUB_SERVER_URL=https://example.com GITHUB_REPOSITORY="-/-" GITHUB_RUN_ID=1 "$llmliveissue" maybe >/dev/null 2>&1
[ $? -eq 2 ] || flag "llm live issue: an unknown verdict was not refused with exit 2"

# --- the lint jobs cover every linter --------------------------------------

# CI splits `lint` and `fmt:check` across `ci:lint` and `ci:lint:go`. A linter
# added to the aggregate and to neither job would run on a laptop and never on
# a pull request.
missing="$(python3 - <<'PY'
import tomllib
t = tomllib.load(open(".mise.toml", "rb"))["tasks"]
want = set(t["lint"]["depends"]) | set(t["fmt:check"]["depends"])
have = set(t["ci:lint"]["depends"]) | set(t["ci:lint:go"]["depends"])
print(" ".join(sorted(want - have)))
PY
)"
[ -z "$missing" ] || flag "in lint or fmt:check but in neither ci:lint nor ci:lint:go: ${missing}"

# --- the commit and title check -----------------------------------------

# One repository, main with one commit; each scenario is a branch off it with
# its commits, the check run with an optional PR title, and the exit status
# it must give.
crepo="$tmp/commits"
git init --quiet --initial-branch=main "$crepo"
cg() { git -C "$crepo" -c user.name=ci -c user.email=ci@example.com -c commit.gpgsign=false "$@"; }
printf '# Changelog\n' >"$crepo/CHANGELOG.md"
printf 'seed\n' >"$crepo/README.md"
cg add -A && cg commit --quiet -m 'chore: seed'

# commits <name> <expected exit> <title or -> <item...>: `footer:<text>` is a
# `feat: change it` commit whose body is `BREAKING CHANGE: <text>`,
# `file:<path>` is a `chore: touch it` commit that writes <path>, and every
# other word is an empty commit with that subject.
commits() {
  local name="$1" expected="$2" title="$3" item status
  shift 3
  cg checkout --quiet -b "$name" main
  for item in "$@"; do
    case "$item" in
    footer:*)
      cg commit --quiet --allow-empty -m 'feat: change it' -m "BREAKING CHANGE: ${item#footer:}"
      ;;
    file:*)
      mkdir -p "$(dirname "$crepo/${item#file:}")"
      printf 'x\n' >>"$crepo/${item#file:}"
      cg add -A && cg commit --quiet -m 'chore: touch it'
      ;;
    *)
      cg commit --quiet --allow-empty -m "$item"
      ;;
    esac
  done
  if [ "$title" = "-" ]; then
    (cd "$crepo" && env -u PR_TITLE -u GITHUB_BASE_REF -u CI COMMITS_CHECK_BASE=main "$commitscheck" 2>"$tmp/stderr")
  else
    (cd "$crepo" && env -u GITHUB_BASE_REF -u CI PR_TITLE="$title" COMMITS_CHECK_BASE=main "$commitscheck" 2>"$tmp/stderr")
  fi
  status=$?
  [ "$status" -eq "$expected" ] ||
    flag "commits ${name}: exit ${status}, expected ${expected}: $(cat "$tmp/stderr")"
  cg checkout --quiet main
}

commits title-and-commits-ok 0 'feat(api): add a thing' 'feat(api): add a thing' 'test: cover it'
commits scope-list-ok 0 'refactor(cli,api): move it' 'refactor(cli,api): move it'
commits bad-title 1 'Add a thing' 'feat: add a thing'
commits unknown-type 1 'perf: faster' 'perf: faster'
commits bad-commit 1 'fix: it' 'fix: it' 'address review'
commits laptop-no-title 0 - 'fix: it'
commits break-in-title-no-steps 1 'feat!: drop it' 'feat: drop it'
commits break-in-commit-no-steps 1 'feat: drop it' 'feat(api)!: drop it'
commits break-with-steps 0 'feat!: drop it' 'feat!: drop it' 'footer:delete every trigger without a source, then restart'
# The footer alone is a break, and its text is the steps.
commits footer-is-a-break-with-steps 0 'feat: drop it' 'footer:it is gone; read the new field instead'
# A footer with no text is a break with no steps.
commits footer-without-steps 1 'feat: drop it' 'footer:'
# release-please reads the token at the START of a line. The phrase quoted
# mid-line in prose is prose, and lowercase is not the token.
cg checkout --quiet -b quoted main
cg commit --quiet --allow-empty -m 'ci: explain it' -m 'The check refuses a BREAKING CHANGE: footer with no steps.'
(cd "$crepo" && env -u GITHUB_BASE_REF -u CI PR_TITLE='ci: explain it' COMMITS_CHECK_BASE=main "$commitscheck" >/dev/null 2>&1) ||
  flag "commits quoted: a body quoting 'BREAKING CHANGE:' mid-line was read as a break"
cg checkout --quiet -b lowercase main
cg commit --quiet --allow-empty -m 'ci: say it softly' -m 'breaking change: lowercase is prose'
(cd "$crepo" && env -u GITHUB_BASE_REF -u CI PR_TITLE='ci: say it softly' COMMITS_CHECK_BASE=main "$commitscheck" >/dev/null 2>&1) ||
  flag "commits lowercase: a lowercase 'breaking change:' was read as a break"
cg checkout --quiet main
# The release pull request touches the changelog and the manifest only.
commits release-pr-ok 0 'chore(release): v0.112.0' 'file:CHANGELOG.md' 'file:.release-please-manifest.json'
commits release-pr-touches-code 1 'chore(release): v0.112.0' 'file:CHANGELOG.md' 'file:internal/x.go'


# --- the decision number check -------------------------------------------

# One repository whose local branches stand in for origin's: the check reads
# refs/heads, main is the base. Each record is committed with a fixed author
# date, so "added first" is decided by the scenario, not by the clock.
drepo="$tmp/decisions"
git init --quiet --initial-branch=main "$drepo"
dg() { git -C "$drepo" -c user.name=ci -c user.email=ci@example.com -c commit.gpgsign=false "$@"; }
mkdir -p "$drepo/docs/decisions"
# record <branch> <from> <file> <author-epoch>: a branch off <from> adding one
# record, committed at that author date.
record() {
  dg checkout --quiet -b "$1" "$2"
  printf -- '---\nstatus: proposed\n---\n' >"$drepo/docs/decisions/$3"
  dg add -A
  GIT_AUTHOR_DATE="@$4 +0000" GIT_COMMITTER_DATE="@$(date +%s) +0000" dg commit --quiet -m "$1"
  dg checkout --quiet main
}
printf -- '---\nstatus: accepted\n---\n' >"$drepo/docs/decisions/0001-one.md"
printf -- '---\nstatus: accepted\n---\n' >"$drepo/docs/decisions/0002-two.md"
dg add -A && dg commit --quiet -m base

day=86400
t0=$(($(date +%s) - 5 * day))
record first main 0003-first.md "$t0"
record second main 0003-second.md $((t0 + day))
record stacked first 0004-stacked.md $((t0 + 2 * day))
record taken-on-main main 0002-late.md "$t0"
# A branch that merged: its 0005 landed on main renumbered to 0006.
record merged main 0005-merged.md "$t0"
dg checkout --quiet main
printf -- '---\nstatus: accepted\n---\n' >"$drepo/docs/decisions/0006-merged.md"
dg add -A && dg commit --quiet -m 'land merged as 0006'
record after-merge main 0005-fresh.md $((t0 + day))
# An abandoned branch: its tip is sixty days old, so its 0007 is no claim.
dg checkout --quiet -b abandoned main
printf -- '---\nstatus: proposed\n---\n' >"$drepo/docs/decisions/0007-abandoned.md"
dg add -A
GIT_AUTHOR_DATE="@$((t0 - 60 * day)) +0000" GIT_COMMITTER_DATE="@$((t0 - 60 * day)) +0000" dg commit --quiet -m abandoned
dg checkout --quiet main
record fresh-seven main 0007-fresh.md $((t0 + day))
# A branch adding two records main already numbers, in one commit.
dg checkout --quiet -b double main
printf -- '---\nstatus: proposed\n---\n' >"$drepo/docs/decisions/0001-also-one.md"
printf -- '---\nstatus: proposed\n---\n' >"$drepo/docs/decisions/0002-also-two.md"
dg add -A
GIT_AUTHOR_DATE="@$t0 +0000" dg commit --quiet -m double
dg checkout --quiet main
# A branch that adds nothing, forked before main renamed 0001: its tree still
# has 0001-one.md, which it did not add.
dg branch before-rename main
dg mv docs/decisions/0001-one.md docs/decisions/0001-uno.md
dg commit --quiet -m 'rename 0001'

# decisions <branch> <expected exit> [expected text]: the check on <branch>
# of the repository in $check_repo.
check_repo="$drepo"
decisions() {
  local name="$1" expected="$2" want="${3:-}" status
  git -C "$check_repo" checkout --quiet "$name"
  (cd "$check_repo" && env -u CI -u GITHUB_BASE_REF -u GITHUB_HEAD_REF -u GITHUB_REF_NAME \
    DECISIONS_CHECK_BASE=main DECISIONS_CHECK_REFS=refs/heads "$decisionscheck" 2>"$tmp/stderr")
  status=$?
  [ "$status" -eq "$expected" ] ||
    flag "decisions ${name}: exit ${status}, expected ${expected}: $(cat "$tmp/stderr")"
  if [ -n "$want" ] && ! grep -qF "$want" "$tmp/stderr"; then
    flag "decisions ${name}: expected '${want}' in: $(cat "$tmp/stderr")"
  fi
  git -C "$check_repo" checkout --quiet main
}

decisions main 0
decisions first 0
decisions second 1 '0003 is already 0003-first.md on first, added first; renumber to 0008'
decisions stacked 0
decisions taken-on-main 1 "0002 is 0002-two.md on main, and main's number is final"
decisions after-merge 0
decisions fresh-seven 0
decisions double 1 'renumber to 0009'
decisions before-rename 0

# A record not yet committed is added now, so it loses to every branch.
dg checkout --quiet -b uncommitted main
printf -- '---\nstatus: proposed\n---\n' >"$drepo/docs/decisions/0004-mine.md"
(cd "$drepo" && env -u CI -u GITHUB_BASE_REF -u GITHUB_HEAD_REF -u GITHUB_REF_NAME \
  DECISIONS_CHECK_BASE=main DECISIONS_CHECK_REFS=refs/heads "$decisionscheck" 2>"$tmp/stderr") &&
  flag "decisions uncommitted: an untracked 0004 passed beside stacked's 0004"
grep -qF '0004 is already 0004-stacked.md on stacked' "$tmp/stderr" ||
  flag "decisions uncommitted: expected the stacked collision in: $(cat "$tmp/stderr")"
rm -f "$drepo/docs/decisions/0004-mine.md"
dg checkout --quiet main

# A base that names no commit is a failure, never a pass.
(cd "$drepo" && env -u CI DECISIONS_CHECK_BASE=definitely-not-a-commit "$decisionscheck" >/dev/null 2>&1)
[ $? -eq 2 ] || flag "decisions: an unresolvable base was not refused with exit 2"

# In CI, a namespace with no branch but the base is a checkout that fetched
# none, never a pass for a branch that adds a record. A branch that adds none
# has nothing to check and passes.
dg checkout --quiet first
(cd "$drepo" && env -u GITHUB_BASE_REF -u GITHUB_HEAD_REF -u GITHUB_REF_NAME CI=true \
  DECISIONS_CHECK_BASE=main DECISIONS_CHECK_REFS=refs/nothing "$decisionscheck" >/dev/null 2>&1)
[ $? -eq 2 ] || flag "decisions: an empty namespace in CI was not refused with exit 2"
dg checkout --quiet main
(cd "$drepo" && env -u GITHUB_BASE_REF -u GITHUB_HEAD_REF -u GITHUB_REF_NAME CI=true \
  DECISIONS_CHECK_BASE=main DECISIONS_CHECK_REFS=refs/nothing "$decisionscheck" >/dev/null 2>&1) ||
  flag "decisions: an empty namespace in CI refused a branch that adds no record"

# A base whose record names are over 64 KiB, more than a pipe holds: a record
# on main must still read as on main. `printf | grep -q` under pipefail
# failed this every time, refusing main's own records against themselves.
brepo="$tmp/decisions-big"
git init --quiet --initial-branch=main "$brepo"
bg() { git -C "$brepo" -c user.name=ci -c user.email=ci@example.com -c commit.gpgsign=false "$@"; }
mkdir -p "$brepo/docs/decisions"
long="$(printf 'a%.0s' {1..230})"
for i in $(seq 1 300); do
  : >"$brepo/docs/decisions/$(printf '%04d' "$i")-r${i}-${long}.md"
done
bg add -A && bg commit --quiet -m base
bg checkout --quiet -b adds-one main
: >"$brepo/docs/decisions/0301-new.md"
bg add -A && bg commit --quiet -m adds-one
(cd "$brepo" && env -u CI -u GITHUB_BASE_REF -u GITHUB_HEAD_REF -u GITHUB_REF_NAME \
  DECISIONS_CHECK_BASE=main DECISIONS_CHECK_REFS=refs/heads "$decisionscheck" 2>"$tmp/stderr") ||
  flag "decisions big base: a non-colliding record was refused: $(head -c 400 "$tmp/stderr")"

# A record created in a merge commit, as renumbering while resolving a merge
# of main does: git log skips merges unless told otherwise, and the record
# then had no date here and no claim elsewhere, so a later branch kept its
# number. Its own repository, so the scenarios above keep their numbers.
mrepo="$tmp/decisions-merge"
git init --quiet --initial-branch=main "$mrepo"
mg() { git -C "$mrepo" -c user.name=ci -c user.email=ci@example.com -c commit.gpgsign=false "$@"; }
mkdir -p "$mrepo/docs/decisions"
printf -- '---\nstatus: accepted\n---\n' >"$mrepo/docs/decisions/0001-one.md"
printf -- '---\nstatus: accepted\n---\n' >"$mrepo/docs/decisions/0002-two.md"
mg add -A && mg commit --quiet -m base
mt0=$(($(date +%s) - 5 * day))
mg checkout --quiet -b merge-made main
: >"$mrepo/work.txt"
mg add -A
GIT_AUTHOR_DATE="@$mt0 +0000" mg commit --quiet -m work
mg checkout --quiet main
: >"$mrepo/main.txt"
mg add -A && mg commit --quiet -m 'main moves'
mg checkout --quiet merge-made
mg merge --quiet --no-commit --no-ff main >/dev/null 2>&1
printf -- '---\nstatus: proposed\n---\n' >"$mrepo/docs/decisions/0003-merged-in.md"
mg add -A
GIT_AUTHOR_DATE="@$((mt0 + day)) +0000" mg commit --quiet --no-edit
mg checkout --quiet -b later main
printf -- '---\nstatus: proposed\n---\n' >"$mrepo/docs/decisions/0003-later.md"
mg add -A
GIT_AUTHOR_DATE="@$((mt0 + 2 * day)) +0000" mg commit --quiet -m later
# Two branches adding 0004 at one author date: the file name decides, so both
# agree that 0004-aaa keeps it.
for name in aaa bbb; do
  mg checkout --quiet -b "tie-${name}" main
  printf -- '---\nstatus: proposed\n---\n' >"$mrepo/docs/decisions/0004-${name}.md"
  mg add -A
  GIT_AUTHOR_DATE="@$mt0 +0000" mg commit --quiet -m "tie ${name}"
done
mg checkout --quiet main
check_repo="$mrepo"
decisions merge-made 0
decisions later 1 '0003 is already 0003-merged-in.md on merge-made, added first'
decisions tie-aaa 0
decisions tie-bbb 1 '0004 is already 0004-aaa.md on tie-aaa, added first'

exit "$fail"
