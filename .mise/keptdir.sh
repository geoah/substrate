#!/usr/bin/env bash
#
# Write the kept directory of one released tag (docs/stored-format.md, "Kept
# directories"; decision 0260).
#
# Why it exists: the stored format is the export's contract and the upgrade
# path, and the only honest evidence that the current code reads what a
# release wrote is a directory that release wrote. A test that writes its
# history with the current code proves only that the current code reads
# itself. So this builds the TAG, not the tree, runs it, drives it through
# every sort of change the format holds, and keeps what it wrote:
#
#   internal/testenv/testdata/kept/<tag>/
#     repositories/kept.example.com/   the repository directory, as the
#                                      stopped server left it
#     records.json                     every record the release served,
#                                      live and tombstoned, read over HTTP
#                                      before the stop
#     triggers.json                    each trigger's status and parked
#                                      failures, as the release served them
#     fixture.json                     the repository, its password, the
#                                      token registration minted and the
#                                      credential key the directory opens
#                                      under: made here, for this fixture,
#                                      and nothing but test material
#
# TestKeptDirectories (internal/testenv) imports each one under the current
# code and compares every record with records.json.
#
# How: the tag is checked out with `git archive` into a scratch directory (its
# own .mise.toml dropped, so this tree's toolchain builds it) and built for
# Linux. The server runs in the CI image, which carries the python3 and uv a
# function body needs and is Linux, where bodies are confined, against a
# throwaway Postgres on a network of its own. The driver, `cmd/keptdir`, is
# this tree's: it only speaks HTTP to the running release, and every byte in
# the directory is the release's.
#
# Usage: .mise/keptdir.sh <tag>      (mise run release:keep <tag>)
# Takes about six minutes: the release's GC sweep runs every five, and the
# directory holds a purge.
set -euo pipefail

tag="${1:?usage: keptdir.sh <tag>}"
cd "$(git rev-parse --show-toplevel)"
here="$(pwd)"
git rev-parse --verify --quiet "refs/tags/$tag" >/dev/null || {
  echo "keptdir: no tag $tag here; git fetch --tags first" >&2
  exit 1
}

out="$here/internal/testenv/testdata/kept/$tag"
go="$(mise which go)"
ci_image="ghcr.io/geoah/substrate:ci"
pg_image="pgvector/pgvector:pg16"
authority="kept.example.com"

work="$(mktemp -d)"
name="substrate-keep-$(od -An -N4 -tx4 /dev/urandom | tr -d ' ')"
cleanup() {
  docker rm -f "$name-server" "$name-pg" >/dev/null 2>&1 || true
  docker network rm "$name" >/dev/null 2>&1 || true
  # The server wrote as this user (--user below), so nothing is root-owned.
  rm -rf "$work"
}
trap cleanup EXIT

case "$(docker info --format '{{.Architecture}}')" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "keptdir: no build for docker architecture $(docker info --format '{{.Architecture}}')" >&2; exit 1 ;;
esac

echo "keptdir: building $tag for linux/$arch"
mkdir -p "$work/src" "$work/bin" "$work/data" "$work/kept"
git archive "$tag" | tar -x -C "$work/src"
rm -f "$work/src/.mise.toml"
(
  cd "$work/src"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOFLAGS=-buildvcs=false "$go" build -trimpath \
    -ldflags "-X github.com/geoah/substrate/internal/build.version=$tag" \
    -o "$work/bin/substrated" ./cmd/substrated
)

# The credential key is made here and kept beside the directory: it wraps the
# repository's DEK, so the test cannot open the sealed store without it, and
# it was never anybody's.
key="$(head -c 32 /dev/urandom | base64 | tr -d '\n')"

docker network create "$name" >/dev/null
docker run -d --name "$name-pg" --network "$name" \
  -e POSTGRES_PASSWORD=keep -e POSTGRES_DB=substrate "$pg_image" >/dev/null
for _ in $(seq 1 60); do
  docker exec "$name-pg" pg_isready -h 127.0.0.1 -U postgres -d substrate >/dev/null 2>&1 && break
  sleep 1
done

# seccomp and apparmor unconfined: the server confines each body itself
# (landlock and seccomp), and Docker's default profile refuses those calls.
docker run -d --name "$name-server" --network "$name" \
  --user "$(id -u):$(id -g)" \
  --security-opt seccomp=unconfined --security-opt apparmor=unconfined \
  -p 127.0.0.1::8080 \
  -v "$work/bin/substrated:/usr/local/bin/substrated:ro" \
  -v "$work/data:/data" \
  -e HOME=/tmp -e UV_CACHE_DIR=/tmp/uv \
  -e DATABASE_URL="postgres://postgres:keep@$name-pg:5432/substrate?sslmode=disable" \
  -e PORT=8080 -e SUBSTRATE_BIND_ADDRESS=0.0.0.0 \
  -e SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true \
  -e SUBSTRATE_INSECURE_DISABLE_TOTP=true \
  -e SUBSTRATE_CREDENTIAL_KEY="$key" \
  -e SUBSTRATE_DATA_ROOT=/data \
  -e SUBSTRATE_TRIGGER_INTERVAL=1s \
  --entrypoint /usr/local/bin/substrated "$ci_image" >/dev/null
port="$(docker port "$name-server" 8080/tcp | head -1 | sed 's/.*://')"
server="http://127.0.0.1:$port"
for _ in $(seq 1 120); do
  curl -fsS "$server/healthz" >/dev/null 2>&1 && break
  sleep 1
done
curl -fsS "$server/healthz" >/dev/null || {
  docker logs "$name-server" >&2
  exit 1
}

echo "keptdir: driving $tag at $server"
"$go" run ./cmd/keptdir -server "$server" -repository "$authority" -out "$work/kept" || {
  docker logs "$name-server" 2>&1 | tail -50 >&2
  exit 1
}

# A stop, never a kill: the directory is kept as a stopped server leaves it.
docker stop -t 60 "$name-server" >/dev/null

rm -rf "$out"
mkdir -p "$out/repositories"
cp -R "$work/data/repositories/$authority" "$out/repositories/$authority"
# The writer's lock is the process's, not the repository's, and git keeps no
# empty directory: the test holds the import to the tree as it is checked out.
rm -f "$out/repositories/$authority/changelog/.lock"
find "$out" -type d -empty -delete
cp "$work/kept/records.json" "$work/kept/triggers.json" "$out/"
jq -n --arg tag "$tag" --arg repository "$authority" \
  --arg password "$(cat "$work/kept/password")" --arg token "$(cat "$work/kept/token")" --arg key "$key" \
  '{tag: $tag, repository: $repository, password: $password, token: $token, credentialKey: $key,
    note: "test material, made for this fixture by .mise/keptdir.sh; it opens nothing else"}' \
  >"$out/fixture.json"
du -sh "$out"
echo "keptdir: wrote $out"
