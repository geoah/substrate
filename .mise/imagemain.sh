#!/usr/bin/env bash
#
# The images of main: $IMAGE:main-<sha12> for one commit, and $IMAGE:latest
# for main's tip. .github/workflows/latest.yml runs `push` and then `latest`
# after every green push run of ci on main.
#
#   push    Build and push $IMAGE:main-<sha12> for HEAD (the first twelve hex
#           digits of its commit), unless the registry already has that tag.
#   latest  Point $IMAGE:latest at the image of the commit main points at
#           now, if that image is published.
#
# `main-<sha12>` is pushed once and never written again, because a deployment
# pins it and must pull the same bytes every time. A rebuild of the same
# commit is not the same image (the layers carry build timestamps), so a
# re-run of a ci run, or a second event for the same commit, finds the tag and
# builds nothing. For the same reason HEAD must be a clean checkout of a
# commit on main: the name says which commit the image is.
#
# `latest` is a second name written over an image `push` already published,
# never a build of its own, so it is the same bytes as one `main-<sha12>`. It
# names main's tip as the remote reports it immediately before the write, not
# the commit of the run that got here: ci runs every push to main to the end,
# so an older commit's run can finish after a newer one's, and anyone can
# re-run an old ci run. Either one arriving here leaves `latest` on the tip.
# A tip whose image is not published yet (its ci run is still going, or red)
# leaves `latest` where it is, and the tip's own run moves it later.
#
# Usage: .mise/imagemain.sh push|latest
#   `mise run image:main` and `mise run image:latest`. Exits 1 on a checkout
#   it refuses to tag, 2 when the registry cannot be read or the usage is
#   wrong.
set -euo pipefail

image="${IMAGE:?IMAGE must name the image repository}"

# published <ref>: 0 when the registry has <ref>, 1 when it answers that it
# does not, 2 on any other answer. Anything but the registry's own answer
# must fail the run, never read as "not published": for `push` that would
# rebuild over an immutable tag. So the answer is matched as the whole line
# ghcr.io writes, `ERROR: <ref>: not found`, and not as the phrase, which a
# missing credential helper also prints (`executable file not found`).
published() {
  local out
  if out="$(docker buildx imagetools inspect "$1" 2>&1)"; then
    return 0
  fi
  if grep -qxF "ERROR: $1: not found" <<<"$out"; then
    return 1
  fi
  echo "could not read $1, so cannot tell whether it is published: $out" >&2
  return 2
}

push() {
  local sha tag dirty status=0
  # Untracked files count: the build context is the directory, so one would
  # ship under the commit's name. Files .gitignore names are not counted, so
  # a cache or build output in the checkout does not refuse the run.
  dirty="$(git status --porcelain --untracked-files=all)"
  if [ -n "$dirty" ]; then
    printf 'the checkout differs from its commit, and main-<sha12> must be exactly the commit it names:\n%s\n' "$dirty" >&2
    exit 1
  fi
  sha="$(git rev-parse HEAD)"
  # The remote, not the checkout's own idea of main, which may be stale or
  # absent.
  git fetch --quiet origin refs/heads/main
  if ! git merge-base --is-ancestor "$sha" FETCH_HEAD; then
    echo "$sha is not on origin's main; a main-<sha12> tag names only commits on main" >&2
    exit 1
  fi
  tag="main-${sha:0:12}"
  published "$image:$tag" || status=$?
  case "$status" in
  0)
    echo "$image:$tag is already published; a commit's tag is pushed once" >&2
    return 0
    ;;
  1) ;;
  *) exit 2 ;;
  esac
  TAG="$tag" mise run image:push
  echo "pushed $image:$tag" >&2
}

latest() {
  local tip tag status=0
  tip="$(git ls-remote origin refs/heads/main | cut -f1)"
  if [ -z "$tip" ]; then
    echo "origin has no main branch; refusing to guess what latest names" >&2
    exit 1
  fi
  tag="main-${tip:0:12}"
  published "$image:$tag" || status=$?
  case "$status" in
  0) ;;
  1)
    echo "main is at $tip and $image:$tag is not published yet (its ci run is still going, or red); latest stays where it is" >&2
    return 0
    ;;
  *) exit 2 ;;
  esac
  docker buildx imagetools create --tag "$image:latest" "$image:$tag"
  echo "$image:latest now names $image:$tag" >&2
}

case "${1:-}" in
push) push ;;
latest) latest ;;
*)
  echo "usage: imagemain.sh push|latest" >&2
  exit 2
  ;;
esac
