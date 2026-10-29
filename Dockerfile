# substrate — one image serving the v1 REST API, the door
# (/register, /login, /tokens) and the built console.
#
# ONE Dockerfile for both ways the image is built, so the runtime stage a
# release ships is the runtime stage every PR builds, compose runs and
# `ci:image` boots. ARTIFACTS picks where the binaries and the console come
# from:
#
#   source    (default) compiled here, from the tree: compose, `ci:image`,
#             `image:push` and the `latest` workflow.
#   prebuilt  copied from the build context, laid out as goreleaser stages
#             it: <os>/<arch>/substrated, <os>/<arch>/substratectl and
#             web/console/dist. Only .goreleaser.yaml passes this, with the
#             context only goreleaser lays out.
#
# BuildKit builds only the stages the final image reaches, so `prebuilt` never
# runs the web or go stages and `source` never COPYs paths only the release
# context has. The release image used to be a second Dockerfile that mirrored
# this file's runtime stage by hand, and the two drifted: that runtime created
# neither /keys nor /var/lib/substrate, a fresh named volume mounted over a
# path the image lacks comes up root-owned, and every published image
# crash-looped at boot on `mkdir /var/lib/substrate/repositories: permission
# denied` while the source build worked. The runtime stage is written once now
# and `.mise/imagesmoke.sh` boots it.
#
# Build context is the repo root (Go module github.com/geoah/substrate):
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     --provenance=false --sbom=false \
#     -t ghcr.io/geoah/substrate:<tag> --push .
#
# Every base image is an exact tag plus its index digest (the multi-arch
# manifest list), so a base refresh arrives as a Dependabot pull request,
# never unannounced on the next build. The tag is what `lint:toolchain` holds
# to .mise.toml; the digest is what the build pulls. The Alpine packages the
# runtime stage upgrades and adds are the branch's current ones at build
# time, so a security fix ships without waiting for a new digest.
ARG ARTIFACTS=source

# ---- web build ----------------------------------------------------------
# The console is web/console (React + Vite + shadcn/ui + Tailwind), a
# self-contained app with only an `@`→src alias — no workspace package to
# stage. dist/ is arch-independent; build it once on the native arch, never qemu.
# The version here is held to .mise.toml's node pin by `lint:toolchain`,
# exactly, because this stage must build the console on the node the console
# is TESTED on.
FROM --platform=$BUILDPLATFORM node:26.12.0-alpine3.24@sha256:6f08a523443ddedf52941874c203475b0ad503e859ad70cb26ab8be9a517dbd5 AS web
WORKDIR /web/console
RUN npm install --global corepack@0.34.0
RUN corepack enable
COPY web/console/package.json web/console/pnpm-lock.yaml* web/console/pnpm-workspace.yaml* ./
RUN corepack prepare --activate && pnpm install --frozen-lockfile
COPY web/console/ ./
RUN pnpm build

# ---- go build -----------------------------------------------------------
# Runs on the native build arch and cross-compiles, so the multi-arch build
# never emulates the Go toolchain. The version is held to .mise.toml's go pin
# and to go.mod's `go` line by `lint:toolchain`: this image sets
# GOTOOLCHAIN=local, so a go.mod that asks for a newer Go fails here.
FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
ARG TARGETOS TARGETARCH
# What this image will call itself when asked (discovery's `server.version`).
# It has to be handed in: .dockerignore drops .git, so the toolchain records
# no version of its own here and an unstamped build reports "dev". That is the
# right answer for a bare `docker build` and the wrong one for anything
# published, so `mise run image:push` passes both and the `latest` workflow
# does too. Never a default that names a version: an image that lies about
# which build it is costs more than one that says "dev".
ARG VERSION=""
ARG COMMIT=""
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w \
      -X github.com/geoah/substrate/internal/build.version=${VERSION} \
      -X github.com/geoah/substrate/internal/build.commit=${COMMIT}" \
      -o /out/substrate ./cmd/substrated
# The CLI ships beside the server because the operator hat speaks the DSN, not
# HTTP, and compose publishes no Postgres port. Without this binary in the
# image, `repository verify`, `repository rebuild` and `user reset` are
# unreachable in the deployment the README tells people to run, so a user who
# loses their authenticator stays locked out. See docs/operations.md.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w \
      -X github.com/geoah/substrate/internal/build.version=${VERSION} \
      -X github.com/geoah/substrate/internal/build.commit=${COMMIT}" \
      -o /out/substratectl ./cmd/substratectl

# ---- artifacts: source --------------------------------------------------
# What the two stages above built, in the one layout the runtime copies from.
FROM scratch AS source
COPY --from=build /out/substrate /out/substratectl /out/
COPY --from=web /web/console/dist /web

# ---- artifacts: prebuilt ------------------------------------------------
# goreleaser has already cross-compiled every platform and built the console
# once by the time it builds the image, so compiling again per architecture
# under emulation would only be slower and less reproducible. It lays the
# binaries out under <os>/<arch>/, which is exactly what TARGETPLATFORM
# spells, and stages the console beside them (its extra_files). Both binaries
# are here because .goreleaser.yaml's image names both build ids.
FROM scratch AS prebuilt
ARG TARGETPLATFORM
COPY --chmod=0755 $TARGETPLATFORM/substrated /out/substrate
COPY --chmod=0755 $TARGETPLATFORM/substratectl /out/substratectl
COPY web/console/dist /web

FROM ${ARTIFACTS} AS artifacts

# ---- uv -----------------------------------------------------------------
# Astral's static musl release, never Alpine's `uv` package. Alpine's build
# links a jemalloc configured for 4 KB pages, and on an arm64 kernel with
# 16 KB pages (the Raspberry Pi 5's) it aborts at start with `<jemalloc>:
# Unsupported system page size`, so every function body fails to prepare and
# every delivery parks. Upstream builds its aarch64 binary with
# JEMALLOC_SYS_WITH_LG_PAGE=16, which runs on 4, 16 and 64 KB pages.
#
# UV_VERSION is held to .mise.toml's uv pin by `lint:toolchain`, because the
# image must run the uv the provider suite is TESTED against. The checksums
# are the release's own `.sha256` files for that version; a bumped pin with
# stale checksums fails this stage rather than shipping an unverified binary.
# It runs on the native build arch and only downloads, so no stage emulates.
FROM --platform=$BUILDPLATFORM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS uv
ARG TARGETARCH
ARG UV_VERSION=0.11.33
ARG UV_SHA256_AMD64=3e95b84d8a8b3390c91584d4fd0c4d326e951da2b8fb15a76719368af2795424
ARG UV_SHA256_ARM64=e256e5cf23f8c2e7d4d83f029acd662cbe7275ff24268ce1d6d2ba9016855268
RUN set -eu; \
    case "$TARGETARCH" in \
      amd64) target=x86_64-unknown-linux-musl; sum="$UV_SHA256_AMD64" ;; \
      arm64) target=aarch64-unknown-linux-musl; sum="$UV_SHA256_ARM64" ;; \
      *) echo "no uv release pinned for $TARGETARCH" >&2; exit 1 ;; \
    esac; \
    wget -q -T 60 -O /tmp/uv.tar.gz \
      "https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/uv-${target}.tar.gz"; \
    echo "${sum}  /tmp/uv.tar.gz" | sha256sum -c -; \
    mkdir /out; \
    tar -xzf /tmp/uv.tar.gz -C /out --strip-components=1 "uv-${target}/uv" "uv-${target}/uvx"

# ---- slack-socket -------------------------------------------------------
# The Socket Mode bridge (cmd/slack-socket), a second image built from this
# file: `docker build --target slack-socket .`. BuildKit builds only the
# stages a target reaches, so the default image never compiles it and this
# image never carries the server, the console or python. Its own build stage,
# on the same pinned Go as `build`, so the target compiles the bridge and
# nothing else; it runs on the same pinned alpine as the server, as the same
# unprivileged uid. VERSION and COMMIT are handed in as for `build`.
FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine3.24@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS slack-socket-build
ARG TARGETOS TARGETARCH
ARG VERSION=""
ARG COMMIT=""
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w \
      -X github.com/geoah/substrate/internal/build.version=${VERSION} \
      -X github.com/geoah/substrate/internal/build.commit=${COMMIT}" \
      -o /out/slack-socket ./cmd/slack-socket

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS slack-socket
# ca-certificates for the wss:// connection to Slack and an https webhook.
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY --from=slack-socket-build /out/slack-socket /usr/local/bin/slack-socket
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/slack-socket"]

# ---- runtime ------------------------------------------------------------
# The shared function runner executes inline bundle code as child processes, so
# the image must carry the language it runs: python3, the host every function
# body is exec'd into.
#
# uv (the Astral installer/runner, a single static binary, from the uv stage
# above) is the connector runtime: a Python function body that carries a
# PEP 723 `# /// script` block declaring `dependencies` is executed via
# `uv run`, which provisions a cached
# venv with those deps and runs the body — so a connector can `import
# googleapiclient` by declaring it inline, with NO pip in the base image and no
# change to the fast dependency-free python host. uv resolves at provision time
# (network); the cache lives under HOME. A dependency-free body never touches
# uv. To warm first-run of common connectors, a build may optionally
# pre-populate uv's cache with the common provider SDKs
# (google-api-python-client, requests, …) via `uv cache` — a documented cache
# warm, NOT a hard dependency: nothing in the base image imports them.
#
# `python3~3.14` holds the minor every function body runs on: an Alpine
# refresh that moves python3 to another minor fails this build instead of
# shipping a different Python with no reviewed edit.
FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6
# Alpine publishes fixed packages before a new base digest carries them.
RUN apk upgrade --no-cache && apk add --no-cache ca-certificates tzdata python3~3.14
# The tarball's entries carry the release builder's uid, so the copy sets
# root as owner: a binary on PATH writable by some other uid is one it can
# replace.
COPY --from=uv --chown=0:0 --chmod=0755 /out/uv /out/uvx /usr/local/bin/

# The runner spawns bundle code as child processes, and NONE of it needs root.
# uv's cache and the python host both write under HOME, so the unprivileged
# user owns one. /keys is where a deployment that mints its own credential key
# keeps it and /var/lib/substrate is the data root it mounts (see
# compose.yaml). Docker copies a directory's ownership onto a fresh named
# volume mounted over it, and a path the image lacks comes up root-owned,
# which is what lets the unprivileged user write the key it mints and create
# the repositories directory under the data root. A BIND mount inherits
# nothing from the image: the host directory must already be owned by this
# uid (docs/operations.md, "The published image").
RUN addgroup -g 65532 -S substrate \
    && adduser -u 65532 -S -G substrate -h /home/substrate substrate \
    && install -d -o substrate -g substrate /home/substrate /keys /var/lib/substrate

ENV HOME=/home/substrate
COPY --from=artifacts /out/substrate /usr/local/bin/substrate
COPY --from=artifacts /out/substratectl /usr/local/bin/substratectl
COPY --from=artifacts /web /web
# The binary binds loopback unless told otherwise, and a container's loopback
# is unreachable from the port mapping, so the image binds every interface of
# the container and says so with SUBSTRATE_INSECURE_ALLOW_CLEARTEXT. What
# reaches that interface is the runtime's decision: compose.yaml publishes the
# port on the host's loopback, a Kubernetes Service answers inside the
# cluster. The server still speaks plain HTTP, so publishing the port beyond
# a host's loopback needs a TLS terminator in front (docs/operations.md, "TLS
# and the reverse proxy"). Go opens 0.0.0.0 as its dual-stack wildcard, the
# socket the old ":8080" was, so IPv6 still reaches it where the host has it.
ENV WEB_DIR=/web \
    PORT=8080 \
    SUBSTRATE_BIND_ADDRESS=0.0.0.0 \
    SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true
EXPOSE 8080

# Shell form, so a PORT or SUBSTRATE_BIND_ADDRESS override is what is probed:
# every interface is probed on 127.0.0.1, and an IPv6 literal inside
# brackets. busybox wget is in the base image; nothing else here is a shell
# dependency.
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
    CMD host="${SUBSTRATE_BIND_ADDRESS#[}"; host="${host%]}"; \
        case "$host" in ""|0.0.0.0|::|localhost) host=127.0.0.1 ;; *:*) host="[$host]" ;; esac; \
        wget -q -O /dev/null "http://${host}:${PORT}/healthz" || exit 1

# The `substrate` user created above, by number: a Kubernetes pod with
# runAsNonRoot and no runAsUser refuses an image whose user is a name, because
# the kubelet cannot tell from a name that it is not root.
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/substrate"]
