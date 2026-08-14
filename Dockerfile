# substrate — one image serving the v1 REST/GraphQL API, the door
# (/register, /login, /tokens) and the built console.
#
# Build context is the repo root (Go module github.com/geoah/substrate):
#   docker buildx build --platform linux/amd64,linux/arm64 \
#     --provenance=false --sbom=false \
#     -t ghcr.io/geoah/substrate:<tag> --push .

# ---- web build ----------------------------------------------------------
# The console is web/console (React + Vite + shadcn/ui + Tailwind), a
# self-contained app with only an `@`→src alias — no workspace package to
# stage. dist/ is arch-independent; build it once on the native arch, never qemu.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /web/console
RUN corepack enable
COPY web/console/package.json web/console/pnpm-lock.yaml* web/console/pnpm-workspace.yaml* ./
RUN corepack prepare --activate && pnpm install --frozen-lockfile
COPY web/console/ ./
RUN pnpm build

# ---- go build -----------------------------------------------------------
# Runs on the native build arch and cross-compiles, so the multi-arch build
# never emulates the Go toolchain.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/substrate ./cmd/substrated

# ---- go toolchain for the runtime (TARGET arch) -------------------------
# Pulled at the target platform (no BUILDPLATFORM override) so the toolchain
# binaries actually run in the final image — the build stage above is native
# build-arch for cross-compile speed and its toolchain is the wrong arch.
FROM golang:1.26-alpine AS gotoolchain

# ---- runtime ------------------------------------------------------------
# The shared function runner executes inline bundle code as child processes,
# so the image must carry the languages it runs: python3 (the Python host)
# and the Go toolchain (Go functions compile at registration). The runner
# builds guests hermetically (GOPROXY=off) and cannot download a toolchain,
# so an apk `go` (older) would break Go-function registration — the exact
# 1.26 toolchain is copied instead.
#
# uv (the Astral installer/runner, a single static binary) is the connector
# runtime: a Python function body that carries a PEP 723 `# /// script` block
# declaring `dependencies` is executed via `uv run`, which provisions a cached
# venv with those deps and runs the body — so a connector can `import
# googleapiclient` by declaring it inline, with NO pip in the base image and no
# change to the fast dependency-free python host. uv resolves at provision time
# (network); the cache lives under HOME. Dependency-free Python and all Go
# functions never touch uv. To warm first-run of common connectors, a build may
# optionally pre-populate uv's cache with the common provider SDKs
# (google-api-python-client, requests, …) via `uv cache` — a documented cache
# warm, NOT a hard dependency: nothing in the base image imports them.
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata python3 uv

# The runner spawns bundle code as child processes, and NONE of it needs root.
# uv's cache, the python host and the Go toolchain all write under HOME, so the
# unprivileged user owns one; GOCACHE sits in /tmp, which it can also write.
RUN addgroup -g 65532 -S substrate \
    && adduser -u 65532 -S -G substrate -h /home/substrate substrate \
    && install -d -o substrate -g substrate /home/substrate

COPY --from=gotoolchain /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH \
    GOTOOLCHAIN=local \
    GOFLAGS=-mod=mod \
    GOCACHE=/tmp/gocache \
    HOME=/home/substrate
COPY --from=build /out/substrate /usr/local/bin/substrate
COPY --from=web /web/console/dist /web
ENV WEB_DIR=/web \
    PORT=8080
EXPOSE 8080

# Shell form, so a PORT override is the port probed. busybox wget is in the
# base image; nothing else here is a shell dependency.
HEALTHCHECK --interval=30s --timeout=3s --start-period=15s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${PORT}/healthz" || exit 1

USER substrate
ENTRYPOINT ["/usr/local/bin/substrate"]
