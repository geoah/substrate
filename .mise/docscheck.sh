#!/usr/bin/env bash
#
# The prose half of `lint:docs`: the rules about what the documentation SAYS,
# where lychee holds what it POINTS AT. Both run under the one task, because
# "is the documentation still true" is one question and splitting it across two
# entry points is how one half stops being run.
#
# What is here is only what a grep can decide. Everything else — a metaphor
# used before it is defined, a mechanism explained on three pages, a claim the
# code contradicts — is a reading pass, and no script is going to hold it.
#
# Scope is docs/*.md plus README.md: the pages a reader is handed. AGENTS.md is
# the working guide and speaks to a different audience, so it is not held to
# the reader-facing vocabulary.
# No `-e`: every rule below runs and reports, so one pass names everything
# wrong rather than the first thing wrong.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 2

files=(docs/*.md README.md)
fail=0

flag() {
  fail=1
  printf 'lint:docs: %s\n' "$*" >&2
}

# grep says 0 for a match, 1 for none, and anything above that for a real
# error (an unreadable file, a bad pattern). Without `-e`, that error would
# read exactly like "nothing found" and the rule would pass having checked
# nothing, which is the one outcome worse than failing.
grep_docs() {
  grep "$@" "${files[@]}"
  local status=$?
  [ "$status" -le 1 ] || flag "grep failed (status ${status}) — a rule checked nothing"
  return "$status"
}

# --- the dead words -----------------------------------------------------
#
# docs/terms.md is the vocabulary, and it says a dead word is a bug wherever it
# survives. Only the words with NO honest surviving sense are grep-able: `type`,
# `schema`, `group`, `log`, `extension` and `capability` all have a legitimate
# sense (a GraphQL schema, a Postgres extension, a function's `capabilities`
# envelope), so they are the reading pass's, not this script's.
#
# terms.md itself is exempt: it names the dead words on purpose, to retire them.
dead='entit(y|ies)|tenants?|relationships?|singletons?|config[ -]?[Tt]ype'
if grep_docs -rniE "\b(${dead})\b" | grep -v '^docs/terms.md:'; then
  flag "a dead word survives; docs/terms.md names the live one"
fi

# --- the envelope keys the server refuses -------------------------------
#
# The envelope is four keys, `kind`/`metadata`/`data`/`status`, and the loader
# refuses a document carrying the retired ones. An example that still writes
# them is a paste that fails for the reader. Only `apiVersion` and `spec` are
# checked: `type:` and `group:` appear legitimately inside property
# declarations, so a grep for them would cry wolf on every kind document.
if grep_docs -rnE '^[[:space:]]*(apiVersion|spec):'; then
  flag "an example writes a retired envelope key; the four are kind/metadata/data/status"
fi

# --- the task names the pages tell people to run ------------------------
#
# A page naming `mise run something-that-was-renamed` is a broken instruction
# that no link checker can see. mise is a hard dependency of every other task
# here, so its absence is a broken environment rather than a reason to skip.
tasks="$(mise tasks ls --no-header 2>/dev/null | awk '{print $1}')"
if [ -z "$tasks" ]; then
  flag "cannot list mise tasks; refusing to pass without checking the names"
else
  while read -r name; do
    # `mise run ci:<job>` is prose about a family, not a command to run. The
    # placeholder is matched so it can be skipped HERE rather than by exempting
    # every name ending in a colon, which would wave through a real `mise run
    # lint:` typo as well.
    case "$name" in *'<'*) continue ;; esac
    printf '%s\n' "$tasks" | grep -qx -- "$name" ||
      flag "'mise run ${name}' names a task that does not exist"
  done < <(grep -rhoE 'mise run [A-Za-z0-9:_.-]+(<[A-Za-z-]+>)?' "${files[@]}" |
    sed 's/^mise run //' | sort -u)
fi

# --- the index names every page -----------------------------------------
#
# docs/README.md is the way in. A page it does not list is a page nobody is
# walked to, which is the same as not shipping it.
#
# The index's own link targets are read rather than grepped for the filename:
# a bare `grep -F "(page.md)"` would miss a legitimate `(./page.md)` and would
# equally accept the filename sitting in prose outside any link.
linked="$(grep -oE '\]\([^)]+\)' docs/README.md |
  sed -E 's/^\]\(//; s/\)$//; s/#.*$//; s|^\./||')"
for path in docs/*.md; do
  page="$(basename "$path")"
  [ "$page" = "README.md" ] && continue
  printf '%s\n' "$linked" | grep -qx -- "$page" ||
    flag "docs/README.md does not link ${page}"
done

exit "$fail"
