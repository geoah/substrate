#!/usr/bin/env bash
#
# mise run apps:new:check (docs/plans/app-kit.md §7.3): the template cannot
# rot. `apps:new example` writes an app into a scratch copy of apps/, and
# what it wrote is built as an app is:
#
#   - its core on Linux, in the swift container apps:test:linux pins, as CI
#     runs it;
#   - on a Mac, its SwiftUI side too: the project generated over the scratch
#     copy, the new app built for the Mac and its views drawn from its
#     fixtures (its UI framework's tests). Elsewhere this half says it was
#     skipped; `mise run apple:test:template` is the same check on the
#     template itself.
#
# Either half fails as well on a name the rename missed, which apps:new
# itself refuses.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

readonly IMAGE="swift:6.4@sha256:119ff7d833ea622851b459513333bb4bda5ebb32b7acd05dd05077d5b0383129"

linux() {
	# The scratch copy takes the kit's sources and the project file alone,
	# never a Mac's build output.
	docker run --rm -v "$root:/src:ro" -w /src "$IMAGE" bash -euc '
		mkdir -p /tmp/apps/xcode
		tar -C apps --exclude=.build --exclude=build --exclude=.gradle --exclude=.swiftpm -cf - kit | tar -C /tmp/apps -xf -
		cp apps/xcode/project.yml /tmp/apps/xcode/project.yml
		.mise/appsnew.sh --apps /tmp/apps example
		grep -qxF "  - ../example/apple/targets.yml" /tmp/apps/xcode/project.yml
		swift build --package-path /tmp/apps/example/ExampleKit --scratch-path /tmp/build/example
		swift test --package-path /tmp/apps/example/ExampleKit --scratch-path /tmp/build/example'
}

apple() {
	if [ "$(uname -s)" != Darwin ] || ! command -v xcodebuild >/dev/null; then
		echo "apps:new:check: no Xcode here, so the new app's SwiftUI side is not built; mise run apple:test:template on a Mac"
		return 0
	fi
	local scratch
	scratch="$(mktemp -d)"
	# shellcheck disable=SC2064 # the folder is fixed now
	trap "rm -rf '$scratch'" EXIT
	# Every app: the project includes each app's targets, and xcodegen reads
	# them all.
	rsync -a --exclude .build --exclude build --exclude .gradle --exclude .swiftpm --exclude .kotlin --exclude '*.xcodeproj' \
		--exclude .DS_Store apps/ "$scratch/apps/"
	.mise/appsnew.sh --apps "$scratch/apps" example
	mise install --quiet xcodegen@2.46.0 >/dev/null
	(
		cd "$scratch/apps/xcode"
		"$(mise bin-paths xcodegen@2.46.0 | head -1)/xcodegen" generate --quiet
		xcodebuild -project Substrate.xcodeproj -scheme SubstrateExample -derivedDataPath build/dd -quiet \
			-destination 'platform=macOS' test -only-testing:SubstrateExampleTests
	)
	echo "apps:new:check: apps/example built on the Mac, its views drawn from its fixtures"
}

linux
apple
