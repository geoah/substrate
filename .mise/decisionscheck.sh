#!/usr/bin/env bash
#
# A decision record's number, held against every other branch before the
# merge (decision 0106, issue #587).
#
# lint:docs refuses two records with one number, but only inside one tree, so
# two branches off the same base that each take the next free number are both
# green until the second merges. By then the number has been cited in commit
# messages, tickets and other records. This check reads the numbers the other
# branches have already taken and refuses the collision while it is still
# cheap to renumber.
#
# The rules, for each record this branch adds (a file under docs/decisions/
# that the base branch's tip does not have):
#
# - Its number is taken on the base branch's tip by another record: refused.
#   main's numbers are final.
# - Another branch adds a different record with the same number, and added it
#   first: refused, with the next number nobody holds. "Added" is the author
#   date of the commit that added the file on that branch (a rebase keeps it);
#   a record not yet committed here was added now. Equal dates fall to the
#   file name, so the two branches always agree on who keeps the number.
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

# The base's record names, one per line.
base_records="$(git ls-tree --name-only "${base_commit}:${dir}" 2>/dev/null | grep -E '^[0-9]{4}-.*\.md$')"

is_record() { [[ "$1" =~ ^[0-9]{4}-[a-z0-9]+(-[a-z0-9]+)*\.md$ ]]; }
title_of() {
  local t="${1#*-}"
  printf '%s' "${t%.md}"
}
on_base() { printf '%s\n' "$base_records" | grep -qxF "$1"; }
title_on_base() { printf '%s\n' "$base_records" | grep -qE "^[0-9]{4}-$(title_of "$1")\.md$"; }

# This tree's records that the base does not have: name and number.
mine=()
for path in "$dir"/*.md; do
  [ -f "$path" ] || continue
  file="$(basename "$path")"
  is_record "$file" || continue
  on_base "$file" && continue
  mine+=("$file")
done
[ "${#mine[@]}" -gt 0 ] || exit 0

mine_titles=" "
for file in "${mine[@]}"; do mine_titles+="$(title_of "$file") "; done

now="$(date +%s)"
here_base="$(git merge-base HEAD "$base_commit" 2>/dev/null || echo "$base_commit")"

# added <from> <to-ref> <file>: the author date of the commit in from..to that
# added the record, empty when none did. --no-renames: a record renumbered on
# a branch was added, under this number, by the renumbering commit.
added() {
  git log --no-renames --diff-filter=A --format=%at "$1..$2" -- "$dir/$3" 2>/dev/null | tail -n 1
}

current_branch="$(git symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
own_names=" ${current_branch} ${GITHUB_HEAD_REF:-} ${GITHUB_REF_NAME:-} "

# The claims: "<number> <added> <file> <branch>" per record another live
# branch adds.
claims=""
while read -r ref tip_date; do
  [ -n "$ref" ] || continue
  branch="${ref#"$namespace"/}"
  [ "$branch" = HEAD ] && continue
  case "$own_names" in *" ${branch} "*) continue ;; esac
  [ "$(git rev-parse "$ref")" = "$base_commit" ] && continue
  [ $((now - tip_date)) -gt $((stale_days * 86400)) ] && continue
  git merge-base --is-ancestor "$ref" HEAD 2>/dev/null && continue
  their_base="$(git merge-base "$ref" "$base_commit" 2>/dev/null)" || continue
  while read -r file; do
    is_record "$file" || continue
    on_base "$file" && continue
    title_on_base "$file" && continue
    case "$mine_titles" in *" $(title_of "$file") "*) continue ;; esac
    when="$(added "$their_base" "$ref" "$file")"
    [ -n "$when" ] || continue
    claims+="${file%%-*} ${when} ${file} ${branch}"$'\n'
  done < <(git ls-tree --name-only "${ref}:${dir}" 2>/dev/null)
done < <(git for-each-ref --format='%(refname) %(committerdate:unix)' "$namespace")

# The next number nobody holds: past the base's, this tree's and every claim.
highest=0
for number in $(
  printf '%s\n' "$base_records" "${mine[@]}" | sed -n -E 's/^([0-9]{4})-.*/\1/p'
  printf '%s' "$claims" | cut -d' ' -f1
); do
  [ $((10#$number)) -gt "$highest" ] && highest=$((10#$number))
done
next="$(printf '%04d' $((highest + 1)))"

fail=0
for file in "${mine[@]}"; do
  number="${file%%-*}"
  taken="$(printf '%s\n' "$base_records" | grep -E "^${number}-" | head -n 1)"
  if [ -n "$taken" ]; then
    echo "decisions:check: ${dir}/${file}: ${number} is ${taken} on ${base#refs/heads/}, and ${base#refs/heads/}'s number is final; renumber to ${next}" >&2
    fail=1
    continue
  fi
  when="$(added "$here_base" HEAD "$file")"
  [ -n "$when" ] || when="$now"
  while read -r c_number c_when c_file c_branch; do
    [ "$c_number" = "$number" ] || continue
    [ "$c_file" = "$file" ] && continue
    if [ "$c_when" -lt "$when" ] || { [ "$c_when" -eq "$when" ] && [[ "$c_file" < "$file" ]]; }; then
      echo "decisions:check: ${dir}/${file}: ${number} is already ${c_file} on ${c_branch}, added first; renumber to ${next}" >&2
      fail=1
      break
    fi
  done < <(printf '%s' "$claims")
done

exit "$fail"
