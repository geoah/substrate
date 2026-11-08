#!/usr/bin/env bash
#
# The web kit's npm packages, behind `apps:web:pack`:
#
#   pack <dir>      build every package under apps/kit/web and pack each
#                   one not marked private into <dir>, as npm will receive it
#   publish <dir>   publish every tarball in <dir> to npm, skipping a
#                   version npm already holds (release.yml's `npm` job)
#
# A tarball is what `pnpm pack` makes: `workspace:*` dependencies pinned to
# the kit's version and `publishConfig` applied, so its exports name dist/.
# Every package carries the substrate release's version (release-please
# writes it, release-please-config.json's extra-files), so the kit is
# released with the server and never on its own.
#
# Packing and publishing are apart on purpose: release.yml packs on a runner
# whose token only reads, where pnpm's install scripts and Vite run, and
# publishes the tarballs from a runner that runs none of that code.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cmd="${1:-}"

case "$cmd" in
pack)
	out="${2:?usage: webkit.sh pack <dir>}"
	mkdir -p "$out"
	out="$(cd "$out" && pwd)"
	cd "$root/apps"
	pnpm install --frozen-lockfile
	pnpm build:kit
	packed=0
	for pkg in kit/web/*/package.json; do
		dir="$(dirname "$pkg")"
		if [ "$(node -p "require('./$pkg').private === true")" = "true" ]; then
			echo "webkit: $dir is private, not packed" >&2
			continue
		fi
		(cd "$dir" && pnpm pack --pack-destination "$out" >/dev/null)
		packed=$((packed + 1))
	done
	echo "webkit: packed $packed package(s) into $out" >&2
	;;
publish)
	dir="${2:?usage: webkit.sh publish <dir>}"
	# Trusted publishing (an OIDC token traded for a one-time credential)
	# needs npm 11.5.1 or later.
	npm_version="$(npm --version)"
	if [ "$(printf '%s\n' 11.5.1 "$npm_version" | sort -V | head -n1)" != 11.5.1 ]; then
		echo "webkit: npm $npm_version cannot use trusted publishing; it needs 11.5.1 or later" >&2
		exit 1
	fi
	provenance="--provenance=false"
	if [ "${PROVENANCE:-}" = "true" ]; then provenance="--provenance"; fi
	shopt -s nullglob
	manifests="$(mktemp -d)"
	i=0
	for tgz in "$dir"/*.tgz; do
		tar -xzOf "$tgz" package/package.json >"$manifests/$i.json"
		printf '%s' "$tgz" >"$manifests/$i.path"
		i=$((i + 1))
	done
	# Each package after the kit packages it depends on: npm takes a version
	# whose dependency it does not hold yet, and that version stays
	# uninstallable until the dependency follows, which a failed run may
	# never do. One name, version and tarball per line.
	order="$(node -e '
		const fs = require("node:fs"), path = require("node:path")
		const dir = process.argv[1]
		const pkgs = fs.readdirSync(dir).filter((f) => f.endsWith(".json")).map((f) => {
			const m = JSON.parse(fs.readFileSync(path.join(dir, f), "utf8"))
			return {
				name: m.name,
				version: m.version,
				needs: Object.keys({ ...m.dependencies, ...m.peerDependencies }),
				tgz: fs.readFileSync(path.join(dir, f.replace(/json$/, "path")), "utf8"),
			}
		})
		const names = new Set(pkgs.map((p) => p.name))
		const done = new Set()
		const out = []
		while (out.length < pkgs.length) {
			const next = pkgs.find(
				(p) => !done.has(p.name) && p.needs.every((d) => !names.has(d) || done.has(d))
			)
			if (!next) throw new Error("the kit packages depend on each other in a cycle")
			done.add(next.name)
			out.push([next.name, next.version, next.tgz].join("\t"))
		}
		process.stdout.write(out.join("\n"))
	' "$manifests")"
	rm -rf "$manifests"
	while IFS=$'\t' read -r name version tgz; do
		[ -n "$name" ] || continue
		if [ -n "$(npm view "$name@$version" version 2>/dev/null)" ]; then
			echo "webkit: $name@$version is already on npm" >&2
			continue
		fi
		npm publish "$tgz" --access public "$provenance"
	done <<<"$order"
	;;
*)
	echo "usage: webkit.sh pack <dir> | publish <dir>" >&2
	exit 2
	;;
esac
