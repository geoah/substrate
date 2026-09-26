#!/usr/bin/env bash
#
# Holds each GitHub release's upgrade notes to docs/changes/. The release job
# writes a release's notes once, as goreleaser's header; a note edited after
# that, or written after its release with a `release:` key, reaches the page
# only through this. The notes sit between two markers that
# `changelog.sh --release` prints, so a rerun replaces the block, a release
# that lost its last note loses the block, and goreleaser's commit list below
# it is never touched.
#
#   releasenotes.sh              every release whose notes differ from its page
#   releasenotes.sh <tag>...     those releases only
#   releasenotes.sh --dry-run    say what would change, change nothing
#
# Needs `gh` with write on the repository: the `release-notes` workflow runs
# it on every push to main that touches docs/changes/, and a person can run
# it by hand.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

dry_run=0
tags=()
for arg in "$@"; do
  case "$arg" in
  --dry-run) dry_run=1 ;;
  *) tags+=("$arg") ;;
  esac
done

start='<!-- upgrade-notes:start -->'
end='<!-- upgrade-notes:end -->'
repo="${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# One listing for every release body, keyed by tag, instead of a call per tag.
gh api --paginate "repos/${repo}/releases?per_page=100" \
  --jq '.[] | [.tag_name, (.body // "" | @base64)] | @tsv' >"$work/releases.tsv"

if [ "${#tags[@]}" -eq 0 ]; then
  mapfile -t tags < <(cut -f1 "$work/releases.tsv")
fi

changed=0
for tag in "${tags[@]}"; do
  if ! grep -qP "^\Q${tag}\E\t" "$work/releases.tsv"; then
    echo "release-notes: ${tag} has no GitHub release; skipping" >&2
    continue
  fi
  encoded="$(awk -F'\t' -v t="$tag" '$1 == t { print $2 }' "$work/releases.tsv")"
  printf '%s' "$encoded" | base64 -d >"$work/body"

  # The page without its notes block: everything outside the markers, with
  # the blank line that followed the block dropped.
  awk -v s="$start" -v e="$end" '
    $0 == s { skip = 1; next }
    $0 == e { skip = 0; drop_blank = 1; next }
    skip { next }
    drop_blank && /^\r?$/ { drop_blank = 0; next }
    { drop_blank = 0; print }
  ' "$work/body" >"$work/rest"

  .mise/changelog.sh --release "$tag" >"$work/notes"
  if [ -s "$work/notes" ]; then
    { cat "$work/notes"; printf '\n'; cat "$work/rest"; } >"$work/new"
  else
    cp "$work/rest" "$work/new"
  fi

  # GitHub strips a trailing newline and may store CRLF, so compare the two
  # bodies normalized; an unchanged page is never written.
  if diff -q <(tr -d '\r' <"$work/body" | sed -e :a -e '/^\n*$/{$d;N;ba' -e '}') \
    <(sed -e :a -e '/^\n*$/{$d;N;ba' -e '}' "$work/new") >/dev/null; then
    continue
  fi

  changed=$((changed + 1))
  if [ "$dry_run" -eq 1 ]; then
    echo "release-notes: ${tag} would change" >&2
    diff -u <(tr -d '\r' <"$work/body") "$work/new" | head -n 40 >&2 || true
    continue
  fi
  gh release edit "$tag" --repo "$repo" --notes-file "$work/new" >/dev/null
  echo "release-notes: ${tag} updated" >&2
done

echo "release-notes: ${changed} release(s) $([ "$dry_run" -eq 1 ] && echo 'would change' || echo 'changed')" >&2
