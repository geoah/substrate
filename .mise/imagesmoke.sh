#!/usr/bin/env bash
#
# Boot an image under compose.yaml and prove it serves.
#
# Why it exists: the image job built the image and stopped there, and a
# release image that BUILT fine crash-looped for everyone who ran it. Its
# runtime stage created no /var/lib/substrate, a fresh named volume mounted
# over a path the image lacks comes up root-owned, and the server (uid 65532)
# died at boot on `mkdir /var/lib/substrate/repositories: permission denied`.
# Nothing short of running the image sees that, so this runs it: compose.yaml
# as shipped, the image under test in place of `build: .`, fresh volumes, a
# throwaway Postgres, and the answers a person would check by hand.
#
# What is held:
#   /healthz answers 200 within the timeout, and the container never exits
#   the server runs as uid 65532, never root
#   the data root's repositories/ directory exists and that uid owns it
#   the credential key compose's entrypoint mints landed in /keys
#   GET /.well-known/substrate/server.json names a version
#   uv runs, and is a static binary: Alpine's dynamically linked package
#   aborts in jemalloc on 16 KB page kernels (arm64, the Raspberry Pi 5)
#
# Usage: .mise/imagesmoke.sh <image ref>
#   `mise run ci:image` builds $IMAGE:ci and runs this over it;
#   `mise run image:smoke ghcr.io/geoah/substrate:0.85.0` runs it over a pull.
#
# A compose project name of its own, so it never touches the project a laptop
# may be running from this tree, and `down -v` on exit deletes only what it
# created. The host port is whatever Docker assigns, read back rather than
# chosen, so a box with :8080 taken is not a failure here.
set -euo pipefail

image="${1:?usage: imagesmoke.sh <image ref>}"
timeout="${SMOKE_TIMEOUT:-120}"

cd "$(git rev-parse --show-toplevel)"

project="substrate-smoke-$(od -An -N4 -tx4 /dev/urandom | tr -d ' ')"
override="$(mktemp)"
compose=(docker compose -p "$project" -f compose.yaml -f "$override")

# The one override: the image under test instead of a build, pulled only when
# it is not already local (a `ci` build exists in no registry, a release ref
# exists in no local store), and a host port Docker picks. `!override`
# replaces compose.yaml's port list rather than appending to it.
cat >"$override" <<YAML
services:
  substrate:
    image: ${image}
    pull_policy: missing
    ports: !override
      - "127.0.0.1::8080"
YAML

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "image:smoke: FAILED; the substrate container's output follows" >&2
    "${compose[@]}" logs --no-color substrate >&2 || true
  fi
  "${compose[@]}" down -v --remove-orphans --timeout 5 >/dev/null 2>&1 || true
  rm -f "$override"
  exit "$status"
}
trap cleanup EXIT

fail() {
  echo "image:smoke: $*" >&2
  exit 1
}

"${compose[@]}" up -d --no-build --quiet-pull

container="$("${compose[@]}" ps -q substrate)"
[ -n "$container" ] || fail "compose started no substrate container"

# The state first, then the port, then the probe. A container that exited or
# is restarting is a failure now, not at the timeout, and cleanup prints its
# output, which is where the refusal is; a container between restarts has no
# published port, and asking for one would report that instead of the exit.
deadline=$((SECONDS + timeout))
base=""
while :; do
  state="$(docker inspect -f '{{.State.Status}}' "$container" 2>/dev/null || echo gone)"
  case "$state" in
  running | created) ;;
  *) fail "the substrate container is ${state}, not running" ;;
  esac
  if [ -z "$base" ]; then
    addr="$("${compose[@]}" port substrate 8080 2>/dev/null || true)"
    [ -z "$addr" ] || base="http://${addr}"
  fi
  if [ -n "$base" ] && curl -fsS -o /dev/null "${base}/healthz" 2>/dev/null; then
    break
  fi
  [ "$SECONDS" -lt "$deadline" ] || fail "no 200 from /healthz within ${timeout}s (container ${state})"
  sleep 1
done

uid="$("${compose[@]}" exec -T substrate id -u)"
[ "$uid" = "65532" ] || fail "the server runs as uid ${uid}, not 65532"

owner="$("${compose[@]}" exec -T substrate stat -c %u /var/lib/substrate/repositories)"
[ "$owner" = "65532" ] || fail "/var/lib/substrate/repositories is owned by uid ${owner}, not 65532"

"${compose[@]}" exec -T substrate test -s /keys/credential.key ||
  fail "the entrypoint minted no credential key into /keys"

# The uv on PATH, the one the runner execs, must be the release the
# Dockerfile pins, and static. The upstream release is static-pie: musl's ldd
# lists only the loader, with no `=>` line. Alpine's package links
# libc.musl, libgcc_s and libbz2, and names itself `alpine-linux-musl` in
# its version string. ldd failing is a failure here, not a pass, because a
# check that cannot run proves nothing.
uv_pin="$(sed -n 's/^ARG UV_VERSION=\([^[:space:]]*\).*/\1/p' Dockerfile | head -1)"
[ -n "$uv_pin" ] || fail "the Dockerfile declares no ARG UV_VERSION= to hold the image's uv to"
uv_path="$("${compose[@]}" exec -T substrate sh -c 'command -v uv')" ||
  fail "no uv on the image's PATH"
uv_version="$("${compose[@]}" exec -T substrate "$uv_path" --version)" ||
  fail "${uv_path} does not run in the image"
case "$uv_version" in
*alpine*) fail "the image ships Alpine's uv (${uv_version}) at ${uv_path}, not the upstream release" ;;
"uv ${uv_pin}" | "uv ${uv_pin} "*) ;;
*) fail "${uv_path} reports ${uv_version}, but the Dockerfile pins UV_VERSION ${uv_pin}" ;;
esac
uv_libs="$("${compose[@]}" exec -T substrate ldd "$uv_path" 2>&1)" ||
  fail "ldd could not read ${uv_path}: ${uv_libs}"
case "$uv_libs" in
*"=>"*) fail "${uv_path} links shared libraries, so it is not the static upstream release: ${uv_libs}" ;;
esac

doc="$(curl -fsS "${base}/.well-known/substrate/server.json")"
version="$(printf '%s' "$doc" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')"
[ -n "$version" ] || fail "server.json names no version: ${doc}"

echo "image:smoke: ${image} boots as uid 65532, owns its data root, minted a key, reports ${version}, runs a static ${uv_version} at ${uv_path}"
