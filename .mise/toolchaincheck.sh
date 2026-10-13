#!/usr/bin/env bash
#
# The toolchain-pin guard: the versions .mise.toml installs are the versions
# the images build on, and every other copy of a pin agrees with its source.
#
# Why it exists: these pins are written twice, in files no compiler reads
# together. `.mise.toml` is what a laptop and every CI job run; the Dockerfile
# is what ships. Dependabot manages the FROM lines and does NOT manage
# `.mise.toml`, so the drift arrives on its own, and when it does both halves
# still pass every check. It surfaces later as behaviour that reproduces in one
# place and not the other, which is the expensive kind of bug to chase.
#
# What is held:
#   node      .mise.toml against the console build stage
#   go        .mise.toml against every golang stage the Dockerfile declares,
#             and against go.mod's `go` and `toolchain` lines
#   pnpm      .mise.toml against web/console/package.json's packageManager
#   uv        .mise.toml against the Dockerfile's UV_VERSION, and no uv from
#             apk
#   alpine    one runtime base, because the file has one runtime stage
#   pgvector  every pgvector/pgvector image in the tree against compose.yaml's
#   mise      every jdx/mise-action `version:` input against each other and
#             against .mise.toml's min_version
#
# Every comparison is exact. A FROM line names an exact tag plus a digest
# (`node:26.11.1-alpine3.24@sha256:...`): the digest is cut off and the
# version is the tag's head, which must equal the pin. A tag that spelled only
# a major let the image build on whatever patch it floated to while CI tested
# the pin, so a Dependabot bump of the tag fails here, by name, until
# .mise.toml follows.
#
# No `-e`: every rule runs and reports, so one pass names everything that has
# drifted rather than the first thing.
set -uo pipefail

cd "$(git rev-parse --show-toplevel)" || exit 2

dockerfiles=(Dockerfile)
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

# Every distinct reference the Dockerfile pulls for one image, one per line,
# digest included. The image is matched at a word start so `alpine:` does not
# also find the `-alpine` variant suffix of node: and golang:, and anywhere on
# the line because a FROM may carry --platform before it and an AS alias after.
image_tags() {
  grep -hoE "(^|[[:space:]])${1}:[^[:space:]]+" "${dockerfiles[@]}" 2>/dev/null |
    sed "s/.*${1}://" | sort -u
}

# The version a reference names: the digest cut off, then the tag's head.
# 26.11.1-alpine3.24@sha256:... -> 26.11.1, 3.24@sha256:... -> 3.24.
tag_version() {
  local tag="${1%%@*}"
  printf '%s' "${tag%%-*}"
}

# One tool, pinned in .mise.toml, against every tag the Dockerfile pulls for it.
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
    [ "$version" = "$pin" ] ||
      flag "the Dockerfile builds on ${image}:${tag%%@*}, but .mise.toml pins ${tool} ${pin}"
  done <<<"$tags"
}

check_pin node node
check_pin go golang

# --- go.mod, which picks the Go that builds when it is newer than the pin --
#
# GOTOOLCHAIN is `auto` under mise, so a `go` or `toolchain` line raised past
# the pin (a `go get`, a Dependabot gomod bump) makes every CI job and the
# release download and build with that Go instead, with nothing red. Only the
# image, whose golang base sets GOTOOLCHAIN=local, would notice, and with a
# toolchain error instead of a pin mismatch.
go_pin="$(mise_pin go)"
if [ -n "$go_pin" ]; then
  # Read as Go reads it: leading space trimmed, a `//` comment after the
  # directive dropped.
  go_line="$(sed -e 's|//.*||' go.mod | sed -n 's/^[[:space:]]*go[[:space:]]\{1,\}\([^[:space:]]*\)[[:space:]]*$/\1/p')"
  toolchain_line="$(sed -e 's|//.*||' go.mod | sed -n 's/^[[:space:]]*toolchain[[:space:]]\{1,\}go\([^[:space:]]*\)[[:space:]]*$/\1/p')"
  if [ -z "$go_line" ]; then
    flag "go.mod has no go line; nothing holds the module to the go pin"
  elif [ "$go_line" != "$go_pin" ]; then
    flag "go.mod says go ${go_line}, but .mise.toml pins go ${go_pin}"
  fi
  if [ -n "$toolchain_line" ] && [ "$toolchain_line" != "$go_pin" ]; then
    flag "go.mod says toolchain go${toolchain_line}, but .mise.toml pins go ${go_pin}"
  fi
fi

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

# --- uv, the runtime every function body with dependencies prepares on ----
#
# The image downloads Astral's static release at UV_VERSION, so the pin is a
# full version and compared whole. Alpine's `uv` package
# is refused by name: its jemalloc aborts on 16 KB page kernels (arm64, the
# Raspberry Pi 5), and it floats with the base image instead of the pin.
uv_pin="$(mise_pin uv)"
uv_image="$(sed -n 's/^ARG UV_VERSION=\([^[:space:]]*\).*/\1/p' "${dockerfiles[@]}" | sort -u)"
if [ -z "$uv_pin" ]; then
  flag "no uv pin in .mise.toml's [tools]; nothing to hold the image's uv to"
elif [ -z "$uv_image" ]; then
  flag "no ARG UV_VERSION= in ${dockerfiles[*]}; the image's uv is unpinned"
elif [ "$uv_image" != "$uv_pin" ]; then
  flag "the Dockerfile downloads uv $(printf '%s' "$uv_image" | tr '\n' ' '), but .mise.toml pins uv ${uv_pin}"
fi
# One instruction per record: comment lines dropped, backslash continuations
# joined, so a `uv` on any line of a multi-line `RUN apk add \` is seen. Then
# each command of it (split on &&, || ; and |) that runs `apk add` is
# searched for `uv` as a whole package argument.
apk_uv="$(awk '
  /^[[:space:]]*#/ { next }
  { line = line " " $0 }
  /\\[[:space:]]*$/ { sub(/\\[[:space:]]*$/, "", line); next }
  {
    n = split(line, cmds, /&&|\|\||;|\|/)
    for (i = 1; i <= n; i++)
      if (cmds[i] ~ /apk[[:space:]]+add/ && cmds[i] ~ /(^|[[:space:]])uv([[:space:]]|$)/) print FILENAME
    line = ""
  }
' "${dockerfiles[@]}")"
if [ -n "$apk_uv" ]; then
  flag "the Dockerfile installs uv from apk; take the pinned upstream release (the uv stage)"
fi

# --- alpine, which nothing outside the Dockerfile pins --------------------
#
# There is no mise pin to hold this to. The release and the source build share
# one runtime stage (the Dockerfile's ARTIFACTS switch), so a release standing
# on a base CI never exercised is no longer possible by drift; what is left to
# refuse is a second alpine reference, by tag or by digest, anywhere in the
# file.
alpine_tags="$(image_tags alpine)"
if [ -z "$alpine_tags" ]; then
  flag "no alpine: runtime base in ${dockerfiles[*]}"
elif [ "$(printf '%s\n' "$alpine_tags" | wc -l)" -gt 1 ]; then
  flag "the Dockerfile pulls more than one alpine base: $(printf '%s' "$alpine_tags" | tr '\n' ' ')"
fi

# --- pgvector, the database every suite and every deployment runs on ------
#
# Written in compose.yaml, the CI Postgres, the dev box's container, the test
# containers and the `llm live` service, and no compiler reads them together.
# compose.yaml's is the one a deployment runs, so it is the one the rest are
# held to. The whole tree is searched rather than a list of files, because a
# new copy is the one a list would miss, binaries aside (a compiled test
# carries the testdb copy). This script is left out: it spells the image in
# its patterns.
pg_image='pgvector/pgvector'
pg_compose="$(sed -n "s|^[[:space:]]*image:[[:space:]]*[\"']\{0,1\}${pg_image}:\([^[:space:]\"']*\).*|\1|p" compose.yaml | head -1)"
if [ -z "$pg_compose" ]; then
  flag "compose.yaml runs no ${pg_image} image; nothing to hold the other copies to"
else
  pg_drift="$(git grep --untracked -I -n -o -E "${pg_image}:[^[:space:]\"'\`),]+" -- ':!.mise/toolchaincheck.sh' |
    awk -v want="${pg_image}:${pg_compose}" '{ ref = $0; sub(/^[^:]*:[0-9]+:/, "", ref); if (ref != want) print }')"
  [ -z "$pg_drift" ] ||
    flag "compose.yaml runs ${pg_image}:${pg_compose}, and these differ: $(printf '%s' "$pg_drift" | tr '\n' ' ')"
fi

# --- mise, the runner of every task ----------------------------------------
#
# Each workflow installs its mise through jdx/mise-action's `version:` input,
# written once per step, and a laptop runs whatever it has. Dependabot reads
# neither, so the copies drift one workflow at a time. Every step must name
# a version, all of them the same, and .mise.toml's min_version is that
# version, so a laptop behind CI is told.
#
# Steps are read by indentation, which yamlfmt holds. A step starts at a
# `- ` no deeper than the step before it (a deeper one is a list inside the
# step) and runs until a line shallower than its keys. Its `uses:` and its
# `with:` are keys at that column, in either order, and the version is the
# `version:` one level under `with:`, nowhere else. Quotes around a value and
# a trailing comment are dropped before it is read. A `uses:` may anchor the
# action (`&name`) and a later one alias it (`*name`); an alias in place of
# `with:` is not followed, so that step reads as unpinned. GitHub runs a
# workflow under either extension.
shopt -s nullglob
workflows=(.github/workflows/*.yml .github/workflows/*.yaml)
shopt -u nullglob
mise_steps=""
[ "${#workflows[@]}" -eq 0 ] || mise_steps="$(awk -v quote="'" '
  function finish() {
    if (instep && mise) print file ":" start ": " (version == "" ? "none" : version)
    instep = 0
  }
  FNR == 1 { finish(); file = FILENAME; split("", anchors) }
  /^[[:space:]]*#/ || /^[[:space:]]*$/ { next }
  {
    match($0, /[^ ]/)
    ind = RSTART - 1
    text = substr($0, RSTART)
    gsub(/"/, "", text)
    gsub(quote, "", text)
    sub(/[[:space:]]+#.*/, "", text)
    if (instep && ind < keycol && !(text ~ /^- / && ind > stepind)) finish()
    if (text ~ /^- / && (!instep || ind <= stepind)) {
      finish()
      instep = 1; mise = 0; inwith = 0; version = ""
      stepind = ind; keycol = ind + 2; start = FNR
      text = substr(text, 3)
      ind = keycol
    }
    if (!instep) next
    if (ind == keycol) {
      inwith = text ~ /^with:[[:space:]]*$/
      if (text ~ /^uses:/) {
        action = text
        sub(/^uses:[[:space:]]*/, "", action)
        name = ""
        if (action ~ /^&/) {
          name = substr(action, 2)
          sub(/[[:space:]].*/, "", name)
          sub(/^&[^[:space:]]*[[:space:]]*/, "", action)
        }
        if (action ~ /^jdx\/mise-action@/) {
          mise = 1
          if (name != "") anchors[name] = 1
        } else if (action ~ /^\*/ && (substr(action, 2) in anchors)) {
          mise = 1
        }
      }
    } else if (inwith && ind == keycol + 2 && text ~ /^version:/) {
      sub(/^version:[[:space:]]*/, "", text)
      version = text
    }
  }
  END { finish() }
' "${workflows[@]}")"
mise_min_line="$(grep -E '^min_version[[:space:]]*=' .mise.toml | head -1)"
case "$mise_min_line" in
*soft*) mise_min="$(printf '%s' "$mise_min_line" | sed -n 's/.*soft[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p')" ;;
*hard*) mise_min="$(printf '%s' "$mise_min_line" | sed -n 's/.*hard[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p')" ;;
*) mise_min="$(printf '%s' "$mise_min_line" | sed -n 's/^min_version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p')" ;;
esac
if [ -z "$mise_steps" ]; then
  flag "no jdx/mise-action step in .github/workflows; the mise version holds nothing"
else
  unpinned="$(printf '%s\n' "$mise_steps" | grep ': none$' || true)"
  [ -z "$unpinned" ] ||
    flag "jdx/mise-action with no version: input installs whatever mise is newest: $(printf '%s' "$unpinned" | sed 's/: none$//' | tr '\n' ' ')"
  mise_versions="$(printf '%s\n' "$mise_steps" | grep -v ': none$' | sed 's/^[^ ]* //' | sort -u)"
  if [ "$(printf '%s\n' "$mise_versions" | grep -c .)" -gt 1 ]; then
    flag "the workflows install more than one mise: $(printf '%s\n' "$mise_steps" | grep -v ': none$' | tr '\n' ' ')"
  elif [ -z "$mise_min" ]; then
    flag ".mise.toml sets no min_version; nothing tells a laptop it runs an older mise than CI's ${mise_versions}"
  elif [ -n "$mise_versions" ] && [ "$mise_min" != "$mise_versions" ]; then
    flag ".mise.toml's min_version is ${mise_min}, but the workflows install mise ${mise_versions}"
  fi
fi

exit "$fail"
