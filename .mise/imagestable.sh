#!/usr/bin/env bash
#
# Point $IMAGE:stable at a release's image, if that release is the newest one.
#
# `stable` is the tag a deployment that wants releases, and not the tip of
# main, follows. release.yml runs this after goreleaser has pushed the
# release's own tags, so the image it copies already exists under the git tag.
#
# Why it is not a goreleaser tag: goreleaser would push `stable` for whatever
# tag it builds, and the recovery path for a release that died halfway is a
# re-run of its workflow (docs/releasing.md). A re-run of v0.113.0 after
# v0.114.0 shipped would move `stable` back a release. So the newest release
# is read here, from the remote's tags at the moment of the push, and any
# other tag leaves `stable` alone. A prerelease (`v1.0.0-rc.1`) is never the
# newest release, because only `vX.Y.Z` tags count.
#
# Nothing is rebuilt: `imagetools create` writes a new tag over the manifests
# the release already pushed, so `stable` is the same bytes as the version.
#
# Usage: .mise/imagestable.sh <tag>
#   `mise run image:stable v0.113.0`; DRY_RUN=1 prints instead of pushing.
set -euo pipefail

tag="${1:?usage: imagestable.sh <tag>}"
image="${IMAGE:?IMAGE must name the image repository}"

if [[ ! "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "$tag is not a release tag of the form vX.Y.Z; stable stays where it is" >&2
  exit 0
fi

# The remote, not the checkout: a tag cut after this checkout was made is
# exactly the newer release that must win.
newest="$(git ls-remote --tags --refs origin 'v*' |
  sed 's#.*refs/tags/##' |
  grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' |
  sort -V |
  tail -n 1)"

if [ -z "$newest" ]; then
  echo "found no release tags on origin; refusing to guess what stable is" >&2
  exit 1
fi

if [ "$tag" != "$newest" ]; then
  echo "$tag is not the newest release ($newest is); stable stays where it is" >&2
  exit 0
fi

if [ -n "${DRY_RUN:-}" ]; then
  echo "would point $image:stable at $image:$tag" >&2
  exit 0
fi

docker buildx imagetools create --tag "$image:stable" "$image:$tag"
echo "$image:stable now names $image:$tag" >&2
docker buildx imagetools inspect "$image:stable" >&2
