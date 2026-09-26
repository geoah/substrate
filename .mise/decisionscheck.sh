#!/usr/bin/env bash
#
# A decision record's number, held against every other branch before the
# merge (decision 0109, issue #587).
#
# lint:docs refuses two records with one number, but only inside one tree, so
# two branches off the same base that each take the next free number are both
# green until the second merges. By then the number has been cited in commit
# messages, tickets and other records. This check reads the numbers the other
# branches have already taken and refuses the collision while it is still
# cheap to renumber.
#
# The rules, for each record this branch adds (a file under docs/decisions/
# that neither the base branch's tip nor this branch's fork point has):
#
# - Its number is taken on the base branch's tip by another record: refused.
#   main's numbers are final.
# - Another branch adds a different record with the same number, and added it
#   first: refused. "Added" is the author date of the commit that added the
#   file on that branch (a rebase keeps it). A merge commit that creates or
#   renumbers the record, as resolving a merge of main often does, counts as
#   the commit that added it. A record not yet committed here was added now. Equal dates fall to the file name, so the two branches
#   always agree on who keeps the number.
#
# Each refused record is told a different number nobody holds yet. That
# number is free only until another branch pushes it, and the renumbering
# belongs in a new commit: an amended or rebased commit keeps its old author
# date, and would then displace a record that took the new number meanwhile.
#
# What another branch holds is not a claim when:
# - the branch tip is older than DECISIONS_CHECK_STALE_DAYS (30): an
#   abandoned branch does not reserve a number forever;
# - its record's title (the name after the number) is already on the base
#   branch or in this tree: that is a record that merged or was renumbered;
# - the branch is this one, or its tip is already inside HEAD.
#
# NOT in `lint`, for the reason kinds:check is not: it reads refs, and the
# answer depends on what has been pushed, not on the files.
#
# DECISIONS_CHECK_BASE overrides the base ref and DECISIONS_CHECK_REFS the
# namespace the other branches are read from (refs/remotes/origin):
#   DECISIONS_CHECK_BASE=origin/main mise run decisions:check
#
# Exit 0 clean, 1 a collision, 2 the check could not run.
set -uo pipefail
# One byte order for the file name tie-break, on a laptop and in CI alike.
export LC_ALL=C

cd "$(git rev-parse --show-toplevel)" || exit 2

dir=docs/decisions
namespace="${DECISIONS_CHECK_REFS:-refs/remotes/origin}"
stale_days="${DECISIONS_CHECK_STALE_DAYS:-30}"

base="${DECISIONS_CHECK_BASE:-}"
if [ -n "$base" ]; then
  if ! git rev-parse --verify --quiet "${base}^{commit}" >/dev/null; then
    echo "decisions:check: base ${base} names no commit" >&2
    exit 2
  fi
else
  base_branch="${GITHUB_BASE_REF:-main}"
  if git rev-parse --verify --quiet "origin/${base_branch}^{commit}" >/dev/null; then
    base="origin/${base_branch}"
  elif git rev-parse --verify --quiet "${base_branch}^{commit}" >/dev/null; then
    base="${base_branch}"
  elif [ -n "${CI:-}" ]; then
    # In CI a base it cannot resolve is a failed fetch, and passing then is a
    # green job that checked nothing.
    echo "decisions:check: cannot resolve base branch ${base_branch}; refusing to pass without checking" >&2
    exit 2
  else
    echo "decisions:check: no base branch to compare against; skipping" >&2
    exit 0
  fi
fi
base_commit="$(git rev-parse "${base}^{commit}")"

# records <commit>: the record names in that commit's tree, one per line.
records() {
  git ls-tree --name-only "${1}:${dir}" 2>/dev/null | grep -E '^[0-9]{4}-.*\.md$'
}

# in_lines says whether $2 is one of the newline-separated lines of $1. Not
# `printf | grep -q`: under pipefail that pipeline reports 141 whenever grep
# exits on an early match before printf has written its later lines, and a
# record on main then reads as one this branch adds, refused against itself.
# CI did exactly that on this check's own PR (and docscheck.sh has the same
# helper for the same reason).
in_lines() {
  case $'\n'"$1"$'\n' in *$'\n'"$2"$'\n'*) return 0 ;; esac
  return 1
}

is_record() { [[ "$1" =~ ^[0-9]{4}-[a-z0-9]+(-[a-z0-9]+)*\.md$ ]]; }
title_of() {
  local t="${1#*-}"
  printf '%s' "${t%.md}"
}

base_records="$(records "$base_commit")"
base_titles="$(sed -E 's/^[0-9]{4}-//; s/\.md$//' <<<"$base_records")"
on_base() { in_lines "$base_records" "$1"; }
title_on_base() { in_lines "$base_titles" "$(title_of "$1")"; }

current_branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
own_names=" ${current_branch} ${GITHUB_HEAD_REF:-} ${GITHUB_REF_NAME:-} "

# The other branches: "<ref> <tip date>" per ref in the namespace, less HEAD,
# this branch and the base itself.
others=""
while read -r ref tip_date; do
  [ -n "$ref" ] || continue
  branch="${ref#"$namespace"/}"
  [ "$branch" = HEAD ] && continue
  case "$own_names" in *" ${branch} "*) continue ;; esac
  [ "$(git rev-parse "$ref")" = "$base_commit" ] && continue
  others+="${ref} ${tip_date}"$'\n'
done < <(git for-each-ref --format='%(refname) %(committerdate:unix)' "$namespace")

here_base="$(git merge-base HEAD "$base_commit" 2>/dev/null || echo "$base_commit")"
here_records="$(records "$here_base")"

# This tree's records that neither the fork point nor the base's tip has. The
# fork point, not only the tip: a record main renamed after this branch forked
# is still here under its old name, and this branch did not add it.
mine=()
for path in "$dir"/*.md; do
  [ -f "$path" ] || continue
  file="$(basename "$path")"
  is_record "$file" || continue
  on_base "$file" && continue
  in_lines "$here_records" "$file" && continue
  mine+=("$file")
done
[ "${#mine[@]}" -gt 0 ] || exit 0

# In CI a namespace with no other branch is a checkout that did not fetch
# them (the lint job's fetch-depth: 0 is what does), and passing then checks
# against main only. Below the exit above: a branch that adds no record has
# nothing to check, whatever was fetched.
if [ -z "$others" ] && [ -n "${CI:-}" ]; then
  echo "decisions:check: ${namespace} holds no branch but the base; refusing to pass without checking" >&2
  exit 2
fi

mine_titles=" "
for file in "${mine[@]}"; do mine_titles+="$(title_of "$file") "; done

now="$(date +%s)"

# added <from> <to-ref> <file>: the author date of the commit in from..to that
# added the record, empty when none did. --no-renames: a record renumbered on
# a branch was added, under this number, by the renumbering commit. -m: git
# log skips merge commits otherwise, and a merge that renumbers the record
# while resolving a merge of main is that commit. With the path limit, -m only
# adds a merge whose copy of the file neither parent has. --no-patch keeps
# the output to bare dates.
added() {
  git log -m --no-patch --no-renames --diff-filter=A --format=%at "$1..$2" -- "$dir/$3" 2>/dev/null | tail -n 1
}

# The claims: "<number> <added> <file> <branch>" per record another live
# branch adds. One diff per branch lists only the records it adds.
claims=""
while read -r ref tip_date; do
  [ -n "$ref" ] || continue
  branch="${ref#"$namespace"/}"
  [ $((now - tip_date)) -gt $((stale_days * 86400)) ] && continue
  git merge-base --is-ancestor "$ref" HEAD 2>/dev/null && continue
  their_base="$(git merge-base "$ref" "$base_commit" 2>/dev/null)" || continue
  while read -r path; do
    [ -n "$path" ] || continue
    file="${path##*/}"
    [ "$path" = "$dir/$file" ] || continue
    is_record "$file" || continue
    on_base "$file" && continue
    title_on_base "$file" && continue
    case "$mine_titles" in *" $(title_of "$file") "*) continue ;; esac
    when="$(added "$their_base" "$ref" "$file")"
    [ -n "$when" ] || continue
    claims+="${file%%-*} ${when} ${file} ${branch}"$'\n'
  done < <(git diff --name-only --no-renames --diff-filter=A "$their_base" "$ref" -- "$dir" 2>/dev/null)
done <<<"$others"

# The next number nobody holds: past the base's, this tree's and every claim.
highest=0
for number in $(
  printf '%s\n' "$base_records" "${mine[@]}" | sed -n -E 's/^([0-9]{4})-.*/\1/p'
  printf '%s' "$claims" | cut -d' ' -f1
); do
  [ $((10#$number)) -gt "$highest" ] && highest=$((10#$number))
done

# suggest sets $next to the next number nobody holds, a different one per
# refusal so two records refused in one run are not told the same number. Not
# a $(...) call: the increment would stay in the subshell.
suggest() {
  highest=$((highest + 1))
  printf -v next '%04d' "$highest"
}
advice="in a new commit (an amended one keeps its old date); it is free only until another branch pushes it"

fail=0
for file in "${mine[@]}"; do
  number="${file%%-*}"
  taken=""
  while read -r name; do
    case "$name" in "${number}-"*) taken="$name" && break ;; esac
  done <<<"$base_records"
  if [ -n "$taken" ]; then
    suggest
    echo "decisions:check: ${dir}/${file}: ${number} is ${taken} on ${base#refs/heads/}, and ${base#refs/heads/}'s number is final; renumber to ${next} ${advice}" >&2
    fail=1
    continue
  fi
  when="$(added "$here_base" HEAD "$file")"
  [ -n "$when" ] || when="$now"
  while read -r c_number c_when c_file c_branch; do
    [ "$c_number" = "$number" ] || continue
    [ "$c_file" = "$file" ] && continue
    if [ "$c_when" -lt "$when" ] || { [ "$c_when" -eq "$when" ] && [[ "$c_file" < "$file" ]]; }; then
      suggest
      echo "decisions:check: ${dir}/${file}: ${number} is already ${c_file} on ${c_branch}, added first; renumber to ${next} ${advice}" >&2
      fail=1
      break
    fi
  done < <(printf '%s' "$claims")
done

exit "$fail"
