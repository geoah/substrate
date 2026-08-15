#!/usr/bin/env bash
#
# The toolchain-pin guard: the versions .mise.toml installs are the versions
# the images build on.
#
# Why it exists: these pins are written twice, in files no compiler reads
# together. `.mise.toml` is what a laptop and every CI job run; the Dockerfiles
# are what ships. Dependabot manages the FROM lines and does NOT manage
# `.mise.toml`, so the drift arrives on its own, and when it does both halves
# still pass every check. It surfaces later as behaviour that reproduces in one
# place and not the other, which is the expensive kind of bug to chase.
#
# What is held:
#   node    .mise.toml against the console build stage
#   go      .mise.toml against every golang stage, in both Dockerfiles
#   pnpm    .mise.toml against web/console/package.json's packageManager
#   alpine  Dockerfile against Dockerfile.release, which must agree
#
# A tag is compared as a PREFIX of the pin, because the tag spells only as much
# as it pins: `golang:1.26-alpine` is satisfied by 1.26.6, `node:26-alpine` by
# any 26.x. Pinning the image tag more tightly is a separate decision, and this
# guard does not make it.
#
# No `-e`: every rule runs and reports, so one pass names everything that has
# drifted rather than the first thing.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 2

dockerfiles=(Dockerfile Dockerfile.release)
fail=0

flag() {
  fail=1
  printf 'lint:toolchain: %s\n' "$*" >&2
}

for f in "${dockerfiles[@]}"; do
  [ -f "$f" ] || flag "${f} is missing; this guard checked nothing for it"
done

# The version mise installs: `name = "x.y.z"` in the [tools] table. Anchored at
# the line start with the bare key, so the quoted backend keys (the `go:` one
# that builds govulncheck) cannot match.
mise_pin() {
  sed -n "s/^${1} = \"\([^\"]*\)\".*/\1/p" .mise.toml | head -1
}

# Every distinct tag the Dockerfiles pull for one image, one per line. The
# image is matched at a word start so `alpine:` does not also find the
# `-alpine` variant suffix of node: and golang:, and anywhere on the line
# because a FROM may carry --platform before it and an AS alias after.
image_tags() {
  grep -hoE "(^|[[:space:]])${1}:[^[:space:]]+" "${dockerfiles[@]}" 2>/dev/null |
    sed "s/.*${1}://" | sort -u
}

# The numeric head of a tag: 1.26-alpine -> 1.26, 26-alpine -> 26, 3.24 -> 3.24.
tag_version() {
  printf '%s' "${1%%-*}"
}

# One tool, pinned in .mise.toml, against every tag the Dockerfiles pull for it.
check_pin() {
  local tool="$1" image="$2" pin tags tag version
  pin="$(mise_pin "$tool")"
  if [ -z "$pin" ]; then
    flag "no ${tool} pin in .mise.toml's [tools]; nothing to hold the images to"
    return
  fi
  tags="$(image_tags "$image")"
  if [ -z "$tags" ]; then
    flag "no ${image}: image in ${dockerfiles[*]}; the ${tool} pin holds nothing"
    return
  fi
  while IFS= read -r tag; do
    version="$(tag_version "$tag")"
    case "$pin" in
      "$version" | "$version".*) ;;
      *) flag "the Dockerfiles build on ${image}:${tag}, but .mise.toml pins ${tool} ${pin}" ;;
    esac
  done <<<"$tags"
}

check_pin node node
check_pin go golang

# --- pnpm, pinned in two places for two different readers ----------------
#
# mise installs it for a laptop and for CI; `packageManager` is what the image's
# pinned corepack activates in its web stage. A disagreement means the console is
# resolved by one pnpm here and a different one in the image, off the same
# lockfile.
pnpm_pin="$(mise_pin pnpm)"
package_manager="$(sed -n 's/.*"packageManager": "pnpm@\([^"]*\)".*/\1/p' web/console/package.json)"
if [ -z "$package_manager" ]; then
  flag "web/console/package.json declares no pnpm packageManager"
elif [ "$pnpm_pin" != "$package_manager" ]; then
  flag "web/console/package.json activates pnpm ${package_manager}, but .mise.toml pins ${pnpm_pin}"
fi

# --- alpine, which nothing outside the Dockerfiles pins -------------------
#
# There is no mise pin to hold these to, so the invariant is that the two
# runtimes are the same runtime. Dockerfile.release ships what a release
# installs and Dockerfile is what every PR builds and what compose runs; a
# release standing on a different base than the one CI exercised is the drift
# worth refusing.
alpine_tags="$(image_tags alpine)"
if [ -z "$alpine_tags" ]; then
  flag "no alpine: runtime base in ${dockerfiles[*]}"
elif [ "$(printf '%s\n' "$alpine_tags" | wc -l)" -gt 1 ]; then
  flag "the Dockerfiles stand on different alpine bases: $(printf '%s' "$alpine_tags" | tr '\n' ' ')"
fi

exit "$fail"
