#!/usr/bin/env bash
#
# The two things this tree writes once and never edits, held against the merge
# base: a landed migration, and the body of an accepted decision record.
#
# NOT in `lint`, for the same reason `kinds:check` is not: it reads the DIFF,
# so it needs a base branch and has no answer on a laptop without one.
#
# A LANDED MIGRATION IS NEVER EDITED. The runner records each migration's
# sha256 as it applies it and refuses a database whose recorded hash no longer
# matches the file, because a new migration must not land on a schema its
# predecessors did not build. Editing a merged migration therefore does not
# change anybody's schema: it locks every database that already ran the old
# text out of every binary built after. That happened here (0005 gained a
# CHECK four minutes before it merged), which is why the rule is now a job and
# not a paragraph.
#
# AN ACCEPTED RECORD'S BODY IS NEVER EDITED. docs/decisions/ is one choice made
# at one time; a record that gets rewritten as opinion moves stops being
# evidence of what was decided and why. What a record IS allowed to do is
# change status, so only the body below the frontmatter is compared: marking a
# record superseded edits `status:` and `superseded-by:` and nothing else.
#
# FROZEN_CHECK_BASE overrides the base commit, for trying the check by hand:
#   FROZEN_CHECK_BASE=HEAD~1 mise run frozen:check
#
# No `-e`: every rule runs and reports, so one pass names everything wrong.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 2

migrations=internal/engine/migrations
decisions=docs/decisions
fail=0

flag() {
  fail=1
  printf 'frozen:check: %s\n' "$*" >&2
}

base_commit="${FROZEN_CHECK_BASE:-}"
if [ -z "$base_commit" ]; then
  base_branch="${GITHUB_BASE_REF:-main}"
  # CI checkouts are shallow (fetch-depth 1); the merge base needs history.
  if [ -f "$(git rev-parse --git-dir)/shallow" ]; then
    git fetch --quiet --unshallow origin || git fetch --quiet origin
  fi
  if git rev-parse --verify --quiet "origin/${base_branch}" >/dev/null; then
    base_commit="$(git merge-base HEAD "origin/${base_branch}")"
  elif git rev-parse --verify --quiet "${base_branch}" >/dev/null; then
    base_commit="$(git merge-base HEAD "${base_branch}")"
  elif [ -n "${CI:-}" ]; then
    # In CI this is the merge gate. A base it cannot resolve means the fetch
    # above failed, and passing then would be a green job that checked
    # nothing, which is the one outcome worse than a red one.
    echo "frozen:check: cannot resolve base branch ${base_branch}; refusing to pass without checking" >&2
    exit 1
  else
    echo "frozen:check: no base branch to diff against; skipping" >&2
    exit 0
  fi
fi

# --- a landed migration is never edited ---------------------------------
#
# Added is the only status a migration file may have. A rename shows up as
# delete plus add, and it is refused for the delete: the recorded name is what
# an operator reads out of schema_migrations.
while read -r status path; do
  [ -n "$path" ] || continue
  case "$status" in
  A) ;;
  M) flag "${path} is a landed migration and this branch edits it; add a new migration instead" ;;
  D) flag "${path} is a landed migration and this branch deletes it; a database that ran it could never be rebuilt from empty" ;;
  *) flag "${path} is a landed migration and this branch changes it (${status})" ;;
  esac
done < <(git diff --name-status --diff-filter=AMDRT "$base_commit" -- "$migrations" |
  awk '{ print $1, $NF }' | sed -E 's/^R[0-9]+/D/; s/^T/M/')

# --- an accepted record's body is never edited --------------------------
#
# The body is everything below the closing `---` of the frontmatter block, so
# a status change is invisible here by construction. The status that decides
# whether the record was frozen is the one AT THE BASE: a record this branch
# accepts for the first time is being written, not rewritten.
decision_body() {
  awk '
    NR == 1 && $0 != "---" { exit }
    NR == 1 { next }
    !closed && $0 == "---" { closed = 1; next }
    closed { print }
  '
}

decision_base_status() {
  awk '
    NR == 1 { if ($0 != "---") exit; next }
    $0 == "---" { exit }
    /^status:/ { sub(/^status:[[:space:]]*/, ""); print; exit }
  '
}

while read -r path; do
  [ -n "$path" ] || continue
  file="$(basename "$path")"
  case "$file" in README.md | template.md) continue ;; esac
  [ -f "$path" ] || continue

  base_blob="$(git show "${base_commit}:${path}" 2>/dev/null)" || continue
  status="$(printf '%s\n' "$base_blob" | decision_base_status)"
  [ "$status" = accepted ] || continue

  if ! diff -q <(printf '%s\n' "$base_blob" | decision_body) <(decision_body <"$path") >/dev/null; then
    flag "${path} was accepted and this branch rewrites its body; supersede it with a new record instead"
  fi
done < <(git diff --name-only --diff-filter=M "$base_commit" -- "$decisions")

exit "$fail"
