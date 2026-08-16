#!/usr/bin/env bash
#
# The shape of internal/engine/migrations/: what the runner needs to be true
# before it will ever see a database.
#
# IN `lint`, unlike `frozen:check`, because it reads the FILES and not the
# diff: the answer is the same on a laptop with no base branch as it is on a
# merge.
#
# What it holds, and why each rule is worth a job:
#
#   - `NNNN_name.up.sql`, four digits and lower_snake. loadMigrations parses
#     the version off the first underscore and ignores anything not ending
#     `.up.sql`, so a misnamed file is not a failure, it is a migration that
#     silently never runs.
#   - One file per version. Two files claiming 0008 is a coin toss over which
#     schema a database ends up with, decided by directory order.
#   - One file per name. Two spellings of the same step read as two steps in
#     schema_migrations, where the name is all an operator has.
#   - Versions 1..N with no gaps. A gap means a migration was deleted, and a
#     deleted migration is a database nothing can reproduce from empty.
#   - No empty file. An empty migration records itself as applied and changes
#     nothing, which is the hardest kind of missing step to find later.
#
# No `-e`: every rule runs and reports, so one pass names everything wrong.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 2

dir=internal/engine/migrations
fail=0

flag() {
  fail=1
  printf 'lint:migrations: %s\n' "$*" >&2
}

[ -d "$dir" ] || {
  printf 'lint:migrations: %s is not a directory\n' "$dir" >&2
  exit 2
}

versions=""
names=""
count=0

for path in "$dir"/*; do
  [ -e "$path" ] || continue
  file="$(basename "$path")"

  if [ ! -f "$path" ]; then
    flag "${dir}/${file} is not a file"
    continue
  fi
  if [[ ! "$file" =~ ^[0-9]{4}_[a-z0-9]+(_[a-z0-9]+)*\.up\.sql$ ]]; then
    flag "${dir}/${file} is not NNNN_name.up.sql; the runner would skip it"
    continue
  fi
  if [ ! -s "$path" ]; then
    flag "${dir}/${file} is empty; it would record itself as applied and change nothing"
  fi

  version="${file%%_*}"
  name="${file#*_}"
  name="${name%.up.sql}"

  # Counting DISTINCT versions, not files: a duplicate 0006 would otherwise
  # push the expected sequence one past the end and report a phantom gap
  # beside the real fault.
  case "$versions" in
  *"|${version}|"*) flag "${dir}: version ${version} numbers two migrations" ;;
  *)
    versions="${versions}|${version}|"
    count=$((count + 1))
    ;;
  esac
  case "$names" in
  *"|${name}|"*) flag "${dir}: '${name}' names two migrations" ;;
  *) names="${names}|${name}|" ;;
  esac
done

if [ "$count" -eq 0 ]; then
  flag "${dir} holds no migrations; the runner would build nothing"
fi

# Contiguity is checked against the count rather than by sorting, so a gap and
# a duplicate are reported as the separate faults they are.
i=1
while [ "$i" -le "$count" ]; do
  padded="$(printf '%04d' "$i")"
  case "$versions" in
  *"|${padded}|"*) ;;
  *) flag "${dir}: no migration is numbered ${padded}; the sequence has a gap" ;;
  esac
  i=$((i + 1))
done

exit "$fail"
