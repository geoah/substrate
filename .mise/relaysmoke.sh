#!/usr/bin/env bash
#
# Boot the push relay's image and prove it serves.
#
# The image is the Dockerfile's `push-relay` target. Its keys are generated
# here, for this run: a P-256 key in the shape Apple issues a .p8 and a
# random seal key. Neither is an Apple key, and nothing here calls Apple:
# readiness is every environment's key parsed and a provider token minted,
# which needs no network.
#
# What is held:
#   /healthz and /readyz answer 200 within the timeout, and the container
#   never exits
#   the relay runs as uid 65532, never root
#   the image's own HEALTHCHECK command passes inside it
#   `substrate-push-relay check --help` runs and exits 0
#   a POST to /v1/push with no body is refused with the wire's envelope
#
# Usage: .mise/relaysmoke.sh <image ref>
#   `mise run ci:image` builds $IMAGE-push-relay:ci and runs this over it; run
#   it over any other ref, a pulled main-<sha12> among them, the same way.
set -euo pipefail

image="${1:?usage: relaysmoke.sh <image ref>}"
timeout="${SMOKE_TIMEOUT:-60}"

name="substrate-relay-smoke-$(od -An -N4 -tx4 /dev/urandom | tr -d ' ')"
keys="$(mktemp -d)"

cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "relay:smoke: FAILED; the relay container's output follows" >&2
    docker logs "$name" >&2 || true
  fi
  docker rm -f "$name" >/dev/null 2>&1 || true
  rm -rf "$keys"
  exit "$status"
}
trap cleanup EXIT

openssl ecparam -name prime256v1 -genkey -noout |
  openssl pkcs8 -topk8 -nocrypt -out "$keys/apns.p8"
# The container's uid reads it through the bind mount; it is this run's
# throwaway key.
chmod 0755 "$keys"
chmod 0644 "$keys/apns.p8"

docker run -d --name "$name" \
  -p 127.0.0.1::8080 \
  -v "$keys:/keys:ro" \
  -e SUBSTRATE_PUSH_RELAY_APNS_TEAM_ID=SMOKE00001 \
  -e SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_FILE=/keys/apns.p8 \
  -e SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_ID=SMOKE00001 \
  -e SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_FILE=/keys/apns.p8 \
  -e SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_ID=SMOKE00001 \
  -e SUBSTRATE_PUSH_RELAY_APNS_TOPICS=dev.example.smoke \
  -e "SUBSTRATE_PUSH_RELAY_SEAL_KEY=$(openssl rand -base64 32)" \
  "$image" >/dev/null

port="$(docker port "$name" 8080/tcp | head -n1 | sed 's/.*://')"
base="http://127.0.0.1:${port}"

for path in /healthz /readyz; do
  deadline=$((SECONDS + timeout))
  until curl -fsS -o /dev/null "${base}${path}"; do
    if [ "$(docker inspect -f '{{.State.Running}}' "$name")" != "true" ]; then
      echo "relay:smoke: the relay exited before ${path} answered" >&2
      exit 1
    fi
    if [ "$SECONDS" -ge "$deadline" ]; then
      echo "relay:smoke: ${path} did not answer 200 within ${timeout}s" >&2
      exit 1
    fi
    sleep 1
  done
  echo "relay:smoke: ${path} answers 200"
done

uid="$(docker exec "$name" id -u)"
if [ "$uid" != "65532" ]; then
  echo "relay:smoke: the relay runs as uid ${uid}, not 65532" >&2
  exit 1
fi
echo "relay:smoke: runs as uid 65532"

probe="$(docker inspect -f '{{join .Config.Healthcheck.Test " "}}' "$name")"
probe="${probe#CMD-SHELL }"
docker exec "$name" sh -c "$probe"
echo "relay:smoke: the image's health check passes"

docker exec "$name" substrate-push-relay check --help >/dev/null
echo "relay:smoke: check --help runs"

code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${base}/v1/push")"
body="$(curl -sS -X POST "${base}/v1/push")"
if [ "$code" != "400" ] || ! grep -q '"code":"bad_request"' <<<"$body"; then
  echo "relay:smoke: an empty push answered ${code} ${body}, not 400 bad_request" >&2
  exit 1
fi
echo "relay:smoke: an empty push is refused 400 bad_request"

if [ "$(docker inspect -f '{{.State.Running}}' "$name")" != "true" ]; then
  echo "relay:smoke: the relay exited" >&2
  exit 1
fi
echo "relay:smoke: ok"
