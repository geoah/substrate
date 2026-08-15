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

# --- the decision records -----------------------------------------------
#
# docs/decisions/ is contributor-facing history: one record is one choice made
# at one time, frozen once it is accepted. It is deliberately NOT in `files`
# above, so the dead-word rule never reaches it: a record written today used
# today's words, and a vocabulary change two years from now must not force an
# edit into a document that is supposed to be frozen. What IS held is the shape
# the corpus is read through, because a corpus nobody can query is a directory.
# The rules are listed in docs/decisions/README.md, in the same order.
#
# The frontmatter is read as a BLOCK, never grepped: only the lines between the
# `---` on line 1 and the next `---` count, so a `status:` inside a code fence
# or inside a sentence cannot satisfy a rule. A grep would pass a record that
# is malformed, and a rule that passes what it cannot parse is worse than no
# rule.
decision_frontmatter() {
  awk '
    NR == 1 { if ($0 != "---") exit 1; next }
    $0 == "---" { closed = 1; exit 0 }
    { print }
    END { if (!closed) exit 1 }
  ' "$1"
}

# One frontmatter key's value, or empty. The block handed in is already bounded
# by the reader above, which is what makes a plain match safe here.
decision_field() {
  printf '%s\n' "$1" | sed -n -E "s/^${2}:[[:space:]]*//p" | head -n 1
}

decision_status() {
  local block
  block="$(decision_frontmatter "$1")" || return 1
  decision_field "$block" status
}

decision_numbers=""
for path in docs/decisions/*; do
  [ -f "$path" ] || continue
  file="$(basename "$path")"
  case "$file" in README.md | template.md) continue ;; esac

  # Name and number. Four digits and kebab, so the corpus sorts by number and
  # a stray note cannot masquerade as a record.
  if [[ ! "$file" =~ ^[0-9]{4}-[a-z0-9]+(-[a-z0-9]+)*\.md$ ]]; then
    flag "docs/decisions/${file} is not NNNN-kebab-title.md, and only README.md and template.md are exempt"
    continue
  fi
  number="${file%%-*}"
  # Numbers are permanent once merged, so two branches that took the same one
  # have to be caught here: the second to merge renumbers.
  case "$decision_numbers" in
  *"|${number}|"*) flag "docs/decisions: ${number} numbers two records" ;;
  *) decision_numbers="${decision_numbers}|${number}|" ;;
  esac

  block="$(decision_frontmatter "$path")" || {
    flag "docs/decisions/${file} has no frontmatter block: --- on line 1, --- again below it"
    continue
  }
  status="$(decision_field "$block" status)"
  dated="$(decision_field "$block" date)"
  case "$status" in
  proposed | accepted | rejected | superseded) ;;
  "") flag "docs/decisions/${file} declares no status" ;;
  *) flag "docs/decisions/${file} has status '${status}'; the four are proposed, accepted, rejected, superseded" ;;
  esac
  [[ "$dated" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] ||
    flag "docs/decisions/${file} has date '${dated}'; the form is YYYY-MM-DD"

  # The supersede link is stored in ONE direction, forward, and the successor
  # must be accepted. That is what makes a cycle impossible.
  superseded_by="$(decision_field "$block" superseded-by)"
  if [ "$status" = superseded ]; then
    successor=""
    if [ -n "$superseded_by" ]; then
      for candidate in docs/decisions/"${superseded_by}"-*.md; do
        [ -f "$candidate" ] && successor="$candidate"
      done
    fi
    if [ -z "$superseded_by" ]; then
      flag "docs/decisions/${file} is superseded and names no superseded-by"
    elif [ -z "$successor" ]; then
      flag "docs/decisions/${file} is superseded by ${superseded_by}, which is not a record here"
    elif [ "$successor" = "$path" ]; then
      flag "docs/decisions/${file} supersedes itself"
    else
      successor_status="$(decision_status "$successor")"
      [ "$successor_status" = accepted ] ||
        flag "docs/decisions/${file} points at $(basename "$successor"), whose status is '${successor_status}'; a successor is accepted"
    fi
  elif [ -n "$superseded_by" ]; then
    flag "docs/decisions/${file} carries superseded-by while its status is '${status}'"
  fi

  # The index, and the status it repeats. A row that says something the record
  # does not is a second source of truth, so the two are held together or the
  # column does not ship. Rows only: a mention in prose is not an index entry.
  row="$(grep -F "(${file})" docs/decisions/README.md | grep '^|' | head -n 1)"
  if [ -z "$row" ]; then
    flag "docs/decisions/README.md does not list ${file}"
  else
    row_status="$(printf '%s\n' "$row" |
      awk -F'|' '{ gsub(/^[[:space:]]+|[[:space:]]+$/, "", $(NF - 1)); print $(NF - 1) }')"
    [ "$row_status" = "$status" ] ||
      flag "docs/decisions/README.md lists ${file} as '${row_status}'; its frontmatter says '${status}'"
  fi
done

exit "$fail"
