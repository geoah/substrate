#!/usr/bin/env bash
# Mechanical docs checks: everything here is a rule a grep can hold, so the
# reading pass in SKILL.md can spend itself on the rules a grep cannot.
# Exit 1 if anything is flagged.
set -uo pipefail

cd "$(dirname "$0")/../../.."
FILES=(docs/*.md README.md)
fail=0

flag() { fail=1; printf '%s\n' "$*"; }

section() { printf '\n== %s ==\n' "$*"; }

section "dead words (docs/terms.md is canonical)"
# Only the words with no honest surviving sense are grep-able; type, schema,
# group, log, extension and capability have allowed senses and are left to
# the reading pass. terms.md is exempt: it names the dead words on purpose.
dead='entit(y|ies)|tenants?|relationships?|singletons?|config[ -]?[Tt]ype'
if grep -rniE "\b($dead)\b" "${FILES[@]}" | grep -v '^docs/terms.md:'; then
  flag "dead words found (see docs/terms.md for the live word)"
fi

section "envelope keys the server refuses"
# apiVersion and spec are dead keys; type: and group: appear legitimately in
# property declarations and prose, so only these two are grep-able.
if grep -rnE '^\s*(apiVersion|spec):' "${FILES[@]}"; then
  flag "dead envelope key in an example: the four keys are kind/metadata/data/status"
fi

section "relative links and anchors resolve"
# Every markdown link to a repo-relative path must point at a real file, and
# a #fragment must match a heading slug in the target (GitHub slugging:
# lowercase, punctuation dropped, spaces to hyphens).
slugs() {
  sed -n 's/^#\{1,6\} //p' "$1" | tr '[:upper:]' '[:lower:]' \
    | sed 's/[^a-z0-9 -]//g; s/ /-/g'
}
while IFS=: read -r file _ target; do
  frag=""
  case "$target" in *'#'*) frag="${target#*#}"; target="${target%%#*}" ;; esac
  case "$target" in http*|mailto*) continue ;; esac
  dir="$(dirname "$file")"
  if [ -n "$target" ] && [ ! -e "$dir/$target" ]; then
    flag "$file: broken link -> $target"
    continue
  fi
  if [ -n "$frag" ]; then
    tfile="$dir/${target:-$(basename "$file")}"
    if [ -f "$tfile" ] && ! slugs "$tfile" | grep -qx "$frag"; then
      flag "$file: broken anchor -> ${target}#${frag}"
    fi
  fi
done < <(grep -rnoE '\]\([^)]+\)' "${FILES[@]}" | sed -E 's/\]\(([^)]*)\)/\1/')

section "mise tasks named in docs exist"
if command -v mise >/dev/null 2>&1; then
  tasks="$(mise tasks ls 2>/dev/null | awk '{print $1}')"
  while IFS=: read -r file _ name; do
    if ! printf '%s\n' "$tasks" | grep -qx "$name"; then
      flag "$file: 'mise run $name' names a task that does not exist"
    fi
  done < <(grep -rnoE 'mise run [A-Za-z0-9:_.-]+' "${FILES[@]}" | sed 's/mise run //')
else
  printf 'mise not on PATH, skipped\n'
fi

section "index covers every page"
for f in docs/*.md; do
  base="$(basename "$f")"
  [ "$base" = README.md ] && continue
  if ! grep -qF "($base)" docs/README.md; then
    flag "docs/README.md: $base is not in the index"
  fi
done

if [ "$fail" -eq 0 ]; then
  printf '\nall mechanical checks pass\n'
fi
exit "$fail"
