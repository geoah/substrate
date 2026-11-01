#!/usr/bin/env bash
#
# The Apple apps (apps/apple): the console, bundled, in a native shell for
# macOS, iPadOS and iOS. Every `mise run apple:*` task is one subcommand here.
#
# The console is built by `console:build` and copied into the bundle by the
# Xcode target's own script phase, so an app always carries the dist/ beside
# it. Signing is Xcode's automatic signing for the team in project.yml; the
# account signed in to Xcode is what -allowProvisioningUpdates registers the
# app id and the device profile with.
set -euo pipefail

cd "$(dirname "$0")/../apps/apple"

readonly XCODEGEN="xcodegen@2.46.0"
readonly PROJECT="Substrate.xcodeproj"
readonly SCHEME="Substrate"
readonly DERIVED="build/dd"
readonly INSTALL_DIR="${SUBSTRATE_APPLE_INSTALL_DIR:-/Applications}"
# The simulator `sim` boots when none is booted: the newest iPhone installed.
readonly SIMULATOR="${SUBSTRATE_APPLE_SIMULATOR:-}"
readonly SPARKLE="$DERIVED/SourcePackages/artifacts/sparkle/Sparkle/bin"
# The login keychain item Sparkle's tools keep the update-signing key under.
readonly SPARKLE_ACCOUNT="${SUBSTRATE_APPLE_SPARKLE_ACCOUNT:-substrate}"
# An App Store Connect API key stands in for an Apple ID signed in to Xcode,
# whose session expires and cannot be renewed from a shell: with one,
# -allowProvisioningUpdates registers app ids, capabilities and profiles on
# its own. Unset, the account signed in to Xcode is used, as before. The key
# needs the Admin role to register an app id's capabilities (HealthKit).
AUTH=()
if [ -n "${SUBSTRATE_APPLE_ASC_KEY_PATH:-}" ]; then
	AUTH=(-authenticationKeyPath "$SUBSTRATE_APPLE_ASC_KEY_PATH"
		-authenticationKeyID "${SUBSTRATE_APPLE_ASC_KEY_ID:?set SUBSTRATE_APPLE_ASC_KEY_ID beside the key path}"
		-authenticationKeyIssuerID "${SUBSTRATE_APPLE_ASC_ISSUER_ID:?set SUBSTRATE_APPLE_ASC_ISSUER_ID beside the key path}")
fi

project() {
	mise x "$XCODEGEN" -- xcodegen generate --quiet
}

xcb() {
	xcodebuild -project "$PROJECT" -scheme "$SCHEME" -derivedDataPath "$DERIVED" \
		-allowProvisioningUpdates ${AUTH[@]+"${AUTH[@]}"} -quiet "$@"
}

build_macos() {
	xcb -destination 'platform=macOS' -configuration "${1:-Release}" build
	echo "apple: $DERIVED/Build/Products/${1:-Release}/Substrate.app"
}

build_sim() {
	xcb -destination 'generic/platform=iOS Simulator' -configuration Debug build
	echo "apple: $DERIVED/Build/Products/Debug-iphonesimulator/Substrate.app"
}

build_sync() {
	xcodebuild -project "$PROJECT" -scheme SubstrateSync -derivedDataPath "$DERIVED" \
		-allowProvisioningUpdates ${AUTH[@]+"${AUTH[@]}"} -quiet -destination 'generic/platform=iOS Simulator' -configuration Debug build
	echo "apple: $DERIVED/Build/Products/Debug-iphonesimulator/SubstrateSync.app"
}

# Substrate Chat: one target for both platforms, like the console app's,
# with its own scheme and no console to bundle.
chat() {
	xcodebuild -project "$PROJECT" -scheme SubstrateChat -derivedDataPath "$DERIVED" \
		-allowProvisioningUpdates ${AUTH[@]+"${AUTH[@]}"} -quiet "$@"
}

build_chat() {
	chat -destination 'platform=macOS' -configuration Debug build
	chat -destination 'generic/platform=iOS Simulator' -configuration Debug build
	echo "apple: $DERIVED/Build/Products/Debug/SubstrateChat.app"
	echo "apple: $DERIVED/Build/Products/Debug-iphonesimulator/SubstrateChat.app"
}

sim_chat() {
	chat -destination 'generic/platform=iOS Simulator' -configuration Debug build
	local udid
	udid="$(simulator_udid)"
	if [ -z "$udid" ]; then
		echo "apple: no iOS simulator is installed; add one in Xcode > Settings > Components" >&2
		return 1
	fi
	xcrun simctl boot "$udid" 2>/dev/null || true
	open -b com.apple.iphonesimulator 2>/dev/null || true
	xcrun simctl install "$udid" "$DERIVED/Build/Products/Debug-iphonesimulator/SubstrateChat.app"
	xcrun simctl launch --terminate-running-process "$udid" dev.reamde.substrate.chat
	echo "apple: running on simulator $udid"
}

build_device() {
	xcb -destination 'generic/platform=iOS' -configuration Release build
	echo "apple: $DERIVED/Build/Products/Release-iphoneos/Substrate.app"
}

install_macos() {
	build_macos Release
	local app="$DERIVED/Build/Products/Release/Substrate.app"
	osascript -e 'tell application id "dev.reamde.substrate.console" to quit' >/dev/null 2>&1 || true
	rm -rf "${INSTALL_DIR:?}/Substrate.app"
	ditto "$app" "$INSTALL_DIR/Substrate.app"
	open "$INSTALL_DIR/Substrate.app"
	echo "apple: installed $INSTALL_DIR/Substrate.app"
}

simulator_udid() {
	if [ -n "$SIMULATOR" ]; then
		echo "$SIMULATOR"
		return
	fi
	local booted
	booted="$(xcrun simctl list devices booted | sed -nE 's/.*\(([0-9A-F-]{36})\) \(Booted\).*/\1/p' | head -n1)"
	if [ -n "$booted" ]; then
		echo "$booted"
		return
	fi
	xcrun simctl list devices available | sed -nE 's/^ +iPhone.*\(([0-9A-F-]{36})\) \(Shutdown\).*/\1/p' | head -n1
}

sim() {
	build_sim
	local udid
	udid="$(simulator_udid)"
	if [ -z "$udid" ]; then
		echo "apple: no iOS simulator is installed; add one in Xcode > Settings > Components" >&2
		return 1
	fi
	xcrun simctl boot "$udid" 2>/dev/null || true
	# The window, where Simulator.app is installed; the run needs none.
	open -b com.apple.iphonesimulator 2>/dev/null || true
	xcrun simctl install "$udid" "$DERIVED/Build/Products/Debug-iphonesimulator/Substrate.app"
	# A debug build reads SUBSTRATE_SHELL_SERVER and starts past the picker.
	SIMCTL_CHILD_SUBSTRATE_SHELL_SERVER="${SUBSTRATE_SHELL_SERVER:-}" \
		xcrun simctl launch --terminate-running-process "$udid" dev.reamde.substrate.console
	echo "apple: running on simulator $udid"
}

# A release: both apps archived, exported, packaged and published to
# SUBSTRATE_APPLE_RELEASE_URL, a folder on an S3 bucket the public may read.
# That one path-style address is where the files are PUT, signed with
# AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY, and what the apps and the
# install page read anonymously; it names a deployment, so it lives in the
# environment and never in the tree.
#
# The version is Apple's three integers plus a build number that only rises
# (the UTC time, YYYYMMDD.HHMMSS); a pre-release is the next patch after the
# last released version, so it sorts after that one and before the next:
# 0.113.1-pre.20261021.153012+ac85863e. A stable release is the released
# version itself and needs a clean tree. Signing is the archive's identity
# unless SUBSTRATE_APPLE_EXPORT_IOS / _MACOS names another export method
# (release-testing for ad hoc, developer-id for a notarizable Mac build).
#
# Sets the caller's `short`, `build`, `commit` and `version` for a channel,
# after checking the release folder and the key are in the environment.
stamp() {
	local channel="$1"
	case "$channel" in
	prerelease | stable) ;;
	*)
		echo "apple: the channel is prerelease or stable" >&2
		return 2
		;;
	esac
	: "${SUBSTRATE_APPLE_RELEASE_URL:?set SUBSTRATE_APPLE_RELEASE_URL to the https address of the release folder}"
	: "${AWS_ACCESS_KEY_ID:?set AWS_ACCESS_KEY_ID to a key that may write the bucket}"
	: "${AWS_SECRET_ACCESS_KEY:?set AWS_SECRET_ACCESS_KEY}"

	local dirty=""
	if [ -n "$(git status --porcelain)" ]; then
		if [ "$channel" = stable ]; then
			echo "apple: a stable release is built from a clean tree" >&2
			return 1
		fi
		dirty=".dirty"
	fi
	local released major minor patch
	released="$(jq -r '."."' ../../.release-please-manifest.json)"
	IFS=. read -r major minor patch <<<"$released"
	build="$(date -u +%Y%m%d.%H%M%S)"
	commit="$(git rev-parse --short=8 HEAD)$dirty"
	if [ "$channel" = stable ]; then
		short="$released"
		version="$released"
	else
		short="$major.$minor.$((patch + 1))"
		version="$short-pre.$build+$commit"
	fi
}

# The URL of an app's client ID metadata document, for the app named in $1.
# Its host heads the owner's approval page and names the app's actor
# (`app:<host>`), so it is a host the publisher alone serves:
# SUBSTRATE_APPLE_CLIENT_URL, an https folder there the same key may write
# (a custom domain, or a virtual-hosted bucket), else the release folder.
# Both apps published under one host share that actor. A shared endpoint,
# where anyone may publish a document under the same host, is refused.
client_url() {
	local app="$1" base="${SUBSTRATE_APPLE_CLIENT_URL:-$SUBSTRATE_APPLE_RELEASE_URL}"
	base="${base%/}"
	case "$base" in
	https://*) ;;
	*)
		echo "apple: the client documents' folder is https: $base" >&2
		return 1
		;;
	esac
	local host="${base#https://}"
	host="${host%%/*}"
	host="${host%%:*}"
	host="$(printf '%s' "$host" | tr '[:upper:]' '[:lower:]')"
	# A fully qualified spelling (`host.`) names the same host.
	host="${host%.}"
	if shared_host "$host"; then
		echo "apple: $host is a shared endpoint, where anyone may publish a client document the approval page would head with the same name; set SUBSTRATE_APPLE_CLIENT_URL to a folder on a host of your own" >&2
		return 1
	fi
	echo "$base/$app/client.json"
}

# A host anyone may publish under at a path of their own: a path-style
# object store endpoint, or a raw file host.
shared_host() {
	case "$1" in
	s3.amazonaws.com | s3.*.amazonaws.com | s3-*.amazonaws.com | \
		s3.*.backblazeb2.com | s3.wasabisys.com | s3.*.wasabisys.com | \
		storage.googleapis.com | raw.githubusercontent.com | gist.githubusercontent.com)
		return 0
		;;
	esac
	return 1
}

release() {
	local channel="${1:-prerelease}" short build commit version
	stamp "$channel"
	local base="${SUBSTRATE_APPLE_RELEASE_URL%/}"

	xcodebuild -resolvePackageDependencies -project "$PROJECT" -scheme "$SCHEME" \
		-derivedDataPath "$DERIVED" -quiet
	# The EdDSA key Sparkle verifies every update with: the private half stays
	# in this login keychain, made on the first release. Losing it means every
	# installed Mac app must be replaced by hand once.
	local key
	key="$("$SPARKLE/generate_keys" --account "$SPARKLE_ACCOUNT" -p 2>/dev/null)" || {
		"$SPARKLE/generate_keys" --account "$SPARKLE_ACCOUNT" >/dev/null
		key="$("$SPARKLE/generate_keys" --account "$SPARKLE_ACCOUNT" -p)"
	}

	local out="build/release/$build" rel="releases/$build"
	local stage="$out/stage"
	rm -rf "$out"
	mkdir -p "$stage/$rel"
	local settings=(
		MARKETING_VERSION="$short" CURRENT_PROJECT_VERSION="$build"
		SUBSTRATE_VERSION="$version" SUBSTRATE_CHANNEL="$channel" SUBSTRATE_COMMIT="$commit"
		SUBSTRATE_UPDATES_URL="$base/" SUBSTRATE_SPARKLE_KEY="$key"
	)
	local platform
	for platform in macOS iOS; do
		xcb -destination "generic/platform=$platform" -configuration Release archive \
			-archivePath "$out/$platform.xcarchive" "${settings[@]}"
	done
	export_archive macOS "${SUBSTRATE_APPLE_EXPORT_MACOS:-debugging}" "$out"
	export_archive iOS "${SUBSTRATE_APPLE_EXPORT_IOS:-debugging}" "$out"
	codesign --verify --deep --strict "$out/macOS/Substrate.app"

	local name="Substrate-${version%%+*}" mac ipa
	mac="$rel/$name-macOS.zip"
	ipa="$rel/$name-iOS.ipa"
	ditto -c -k --sequesterRsrc --keepParent "$out/macOS/Substrate.app" "$stage/$mac"
	# The export names the IPA after the bundle name, "Substrate Console".
	local exported=("$out/iOS"/*.ipa)
	cp "${exported[0]}" "$stage/$ipa"
	local icon="Substrate/Assets.xcassets/AppIcon.appiconset/icon-1024.png"
	sips -z 57 57 "$icon" --out "$stage/$rel/icon-57.png" >/dev/null
	sips -z 512 512 "$icon" --out "$stage/$rel/icon-512.png" >/dev/null

	jq -n --arg version "$version" --arg short "$short" --arg build "$build" \
		--arg channel "$channel" --arg commit "$commit" \
		--arg date "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		--arg mac "$mac" --argjson macLength "$(stat -f %z "$stage/$mac")" \
		--arg macSum "$(shasum -a 256 "$stage/$mac" | cut -d' ' -f1)" \
		--arg macSig "$("$SPARKLE/sign_update" --account "$SPARKLE_ACCOUNT" -p "$stage/$mac")" \
		--arg ipa "$ipa" --argjson ipaLength "$(stat -f %z "$stage/$ipa")" \
		--arg ipaSum "$(shasum -a 256 "$stage/$ipa" | cut -d' ' -f1)" \
		--arg rel "$rel" '{
			name: "Substrate Console", version: $version, shortVersion: $short, build: $build, channel: $channel,
			commit: $commit, date: $date, bundleIdentifier: "dev.reamde.substrate.console",
			macos: {path: $mac, length: $macLength, sha256: $macSum, minimumSystemVersion: "26.0",
				edSignature: $macSig},
			ios: {path: $ipa, length: $ipaLength, sha256: $ipaSum, minimumSystemVersion: "26.0",
				manifest: "\($rel)/manifest.plist", icon: "\($rel)/icon-512.png",
				smallIcon: "\($rel)/icon-57.png"}
		}' >"$stage/$rel/release.json"

	publish "$base" "$out" "$stage" "$rel"
	echo "apple: released $version ($channel)"
	echo "apple: $base/index.html"
}

# Substrate Sync: the iPhone and iPad app alone, archived, exported the way
# the console app's iOS build is, and published to its own folder, `sync/`
# under the console app's, with feeds of its own. Its version is stamped the
# same way, so the two apps built from one commit carry one version.
release_sync() {
	local channel="${1:-prerelease}" short build commit version client
	stamp "$channel"
	# Its OAuth client id is its client ID metadata document, as Substrate
	# Chat's is: without one a release signs in as a local app, which the
	# approval page cannot name.
	client="$(client_url sync)" || return 1
	local base="${SUBSTRATE_APPLE_RELEASE_URL%/}/sync"
	local out="build/release-sync/$build" rel="releases/$build"
	local stage="$out/stage"
	rm -rf "$out"
	mkdir -p "$stage/$rel"
	jq -n --arg id "$client" --arg uri "$base/index.html" '{
		client_id: $id, client_name: "Substrate Sync", client_uri: $uri,
		redirect_uris: ["http://127.0.0.1/oauth/callback"], token_endpoint_auth_method: "none"
	}' >"$out/client.json"
	xcodebuild -project "$PROJECT" -scheme SubstrateSync -derivedDataPath "$DERIVED" \
		-allowProvisioningUpdates ${AUTH[@]+"${AUTH[@]}"} -quiet -destination 'generic/platform=iOS' -configuration Release archive \
		-archivePath "$out/iOS.xcarchive" \
		MARKETING_VERSION="$short" CURRENT_PROJECT_VERSION="$build" \
		SUBSTRATE_VERSION="$version" SUBSTRATE_CHANNEL="$channel" SUBSTRATE_COMMIT="$commit" \
		SUBSTRATE_UPDATES_URL="$base/" SUBSTRATE_SYNC_CLIENT_ID="$client"
	export_archive iOS "${SUBSTRATE_APPLE_EXPORT_IOS:-debugging}" "$out"

	local ipa="$rel/SubstrateSync-${version%%+*}-iOS.ipa"
	# The export names the IPA after the app's display name, "Substrate Sync".
	local exported=("$out/iOS"/*.ipa)
	cp "${exported[0]}" "$stage/$ipa"
	local icon="SubstrateSync/Assets.xcassets/AppIcon.appiconset/icon-1024.png"
	sips -z 57 57 "$icon" --out "$stage/$rel/icon-57.png" >/dev/null
	sips -z 512 512 "$icon" --out "$stage/$rel/icon-512.png" >/dev/null

	jq -n --arg version "$version" --arg short "$short" --arg build "$build" \
		--arg channel "$channel" --arg commit "$commit" \
		--arg date "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		--arg ipa "$ipa" --argjson ipaLength "$(stat -f %z "$stage/$ipa")" \
		--arg ipaSum "$(shasum -a 256 "$stage/$ipa" | cut -d' ' -f1)" \
		--arg rel "$rel" '{
			name: "Substrate Sync", version: $version, shortVersion: $short, build: $build,
			channel: $channel, commit: $commit, date: $date, bundleIdentifier: "dev.reamde.substrate.sync",
			ios: {path: $ipa, length: $ipaLength, sha256: $ipaSum, minimumSystemVersion: "26.0",
				manifest: "\($rel)/manifest.plist", icon: "\($rel)/icon-512.png",
				smallIcon: "\($rel)/icon-57.png"}
		}' >"$stage/$rel/release.json"

	upload "$out/client.json" "$client" "no-cache"
	publish "$base" "$out" "$stage" "$rel"
	echo "apple: released Substrate Sync $version ($channel)"
	echo "apple: $base/index.html"
}

# Substrate Chat: both platforms, archived and exported the way the console
# app's are, the Mac build signed for Sparkle with the same key, published to
# its own folder, `chat/` under the console app's.
release_chat() {
	local channel="${1:-prerelease}" short build commit version client
	stamp "$channel"
	client="$(client_url chat)" || return 1
	local base="${SUBSTRATE_APPLE_RELEASE_URL%/}/chat"

	xcodebuild -resolvePackageDependencies -project "$PROJECT" -scheme SubstrateChat \
		-derivedDataPath "$DERIVED" -quiet
	local key
	key="$("$SPARKLE/generate_keys" --account "$SPARKLE_ACCOUNT" -p 2>/dev/null)" || {
		"$SPARKLE/generate_keys" --account "$SPARKLE_ACCOUNT" >/dev/null
		key="$("$SPARKLE/generate_keys" --account "$SPARKLE_ACCOUNT" -p)"
	}

	local out="build/release-chat/$build" rel="releases/$build"
	local stage="$out/stage"
	rm -rf "$out"
	mkdir -p "$stage/$rel"
	# The app's OAuth client id is its client ID metadata document, so the
	# approval page names the host that serves it. The redirect is a loopback
	# one (RFC 8252), which a native app can listen on; the server admits it
	# on any port.
	jq -n --arg id "$client" --arg uri "$base/index.html" '{
		client_id: $id, client_name: "Substrate Chat", client_uri: $uri,
		redirect_uris: ["http://127.0.0.1/oauth/callback"], token_endpoint_auth_method: "none"
	}' >"$out/client.json"
	local settings=(
		MARKETING_VERSION="$short" CURRENT_PROJECT_VERSION="$build"
		SUBSTRATE_VERSION="$version" SUBSTRATE_CHANNEL="$channel" SUBSTRATE_COMMIT="$commit"
		SUBSTRATE_UPDATES_URL="$base/" SUBSTRATE_SPARKLE_KEY="$key" SUBSTRATE_CHAT_CLIENT_ID="$client"
	)
	local platform
	for platform in macOS iOS; do
		chat -destination "generic/platform=$platform" -configuration Release archive \
			-archivePath "$out/$platform.xcarchive" "${settings[@]}"
	done
	export_archive macOS "${SUBSTRATE_APPLE_EXPORT_MACOS:-debugging}" "$out"
	export_archive iOS "${SUBSTRATE_APPLE_EXPORT_IOS:-debugging}" "$out"
	codesign --verify --deep --strict "$out/macOS/SubstrateChat.app"

	local name="SubstrateChat-${version%%+*}" mac ipa
	mac="$rel/$name-macOS.zip"
	ipa="$rel/$name-iOS.ipa"
	ditto -c -k --sequesterRsrc --keepParent "$out/macOS/SubstrateChat.app" "$stage/$mac"
	# The export names the IPA after the app's display name, "Substrate Chat".
	local exported=("$out/iOS"/*.ipa)
	cp "${exported[0]}" "$stage/$ipa"
	local icon="SubstrateChat/Assets.xcassets/AppIcon.appiconset/icon-1024.png"
	sips -z 57 57 "$icon" --out "$stage/$rel/icon-57.png" >/dev/null
	sips -z 512 512 "$icon" --out "$stage/$rel/icon-512.png" >/dev/null

	jq -n --arg version "$version" --arg short "$short" --arg build "$build" \
		--arg channel "$channel" --arg commit "$commit" \
		--arg date "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		--arg mac "$mac" --argjson macLength "$(stat -f %z "$stage/$mac")" \
		--arg macSum "$(shasum -a 256 "$stage/$mac" | cut -d' ' -f1)" \
		--arg macSig "$("$SPARKLE/sign_update" --account "$SPARKLE_ACCOUNT" -p "$stage/$mac")" \
		--arg ipa "$ipa" --argjson ipaLength "$(stat -f %z "$stage/$ipa")" \
		--arg ipaSum "$(shasum -a 256 "$stage/$ipa" | cut -d' ' -f1)" \
		--arg rel "$rel" '{
			name: "Substrate Chat", version: $version, shortVersion: $short, build: $build,
			channel: $channel, commit: $commit, date: $date, bundleIdentifier: "dev.reamde.substrate.chat",
			macos: {path: $mac, length: $macLength, sha256: $macSum, minimumSystemVersion: "26.0",
				edSignature: $macSig},
			ios: {path: $ipa, length: $ipaLength, sha256: $ipaSum, minimumSystemVersion: "26.0",
				manifest: "\($rel)/manifest.plist", icon: "\($rel)/icon-512.png",
				smallIcon: "\($rel)/icon-57.png"}
		}' >"$stage/$rel/release.json"

	upload "$out/client.json" "$client" "no-cache"
	publish "$base" "$out" "$stage" "$rel"
	echo "apple: released Substrate Chat $version ($channel)"
	echo "apple: $base/index.html"
}

# Uploads one staged release beside the ones `base` already holds and
# rewrites its feeds. The published index is the history the new one
# extends: a missing one is the first release, anything else that is not a
# 200 stops here rather than publish an index that forgot every release
# before.
publish() {
	local base="$1" out="$2" stage="$3" rel="$4"
	local status
	status="$(s3 -o "$out/published.json" -w '%{http_code}' "$base/index.json")"
	case "$status" in
	200) ;;
	404) : >"$out/published.json" ;;
	*)
		echo "apple: reading $base/index.json answered $status" >&2
		return 1
		;;
	esac
	swift scripts/feed.swift "$base/" "$out/published.json" "$stage/$rel/release.json" "$stage"

	# The release's files first and the feeds last, so no feed ever names a
	# file that is not there yet.
	local file
	for file in "$stage/$rel"/*; do
		upload "$file" "$base/$rel/${file##*/}" "public, max-age=31536000, immutable"
	done
	for file in index.json appcast.xml index.html; do
		upload "$stage/$file" "$base/$file" "no-cache"
	done
}

export_archive() {
	local platform="$1" method="$2" out="$3"
	plutil -create xml1 "$out/$platform-export.plist"
	plutil -insert method -string "$method" "$out/$platform-export.plist"
	plutil -insert signingStyle -string automatic "$out/$platform-export.plist"
	xcodebuild -exportArchive -archivePath "$out/$platform.xcarchive" -exportPath "$out/$platform" \
		-exportOptionsPlist "$out/$platform-export.plist" -allowProvisioningUpdates ${AUTH[@]+"${AUTH[@]}"} -quiet
}

# curl, signed for S3 with the key in the environment, which reaches curl on
# stdin rather than on a command line every process may read.
s3() {
	printf 'user = "%s:%s"\n' "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY" |
		curl -sS -K - --aws-sigv4 "aws:amz:${SUBSTRATE_APPLE_RELEASE_REGION:-us-east-1}:s3" "$@"
}

upload() {
	local file="$1" url="$2" cache="$3" type
	case "$file" in
	*.html) type="text/html; charset=utf-8" ;;
	*.json) type="application/json" ;;
	*.xml | *.plist) type="application/xml" ;;
	*.png) type="image/png" ;;
	*.zip) type="application/zip" ;;
	*) type="application/octet-stream" ;;
	esac
	s3 -f -o /dev/null -H "Content-Type: $type" -H "Cache-Control: $cache" -T "$file" "$url"
}

case "${1:-}" in
project) project ;;
build)
	project
	build_macos Release
	build_sim
	;;
build:macos)
	project
	build_macos "${2:-Release}"
	;;
build:sim)
	project
	build_sim
	;;
build:device)
	project
	build_device
	;;
build:sync)
	project
	build_sync
	;;
build:chat)
	project
	build_chat
	;;
sim:chat)
	project
	sim_chat
	;;
test:kit)
	# SubstrateKit is a Swift package: its tests run on the Mac with swift
	# test, no project or simulator.
	swift test --package-path SubstrateKit
	;;
test:chat)
	project
	chat -destination 'platform=macOS' test
	;;
install)
	project
	install_macos
	;;
sim)
	project
	sim
	;;
release)
	project
	release "${2:-}"
	;;
release:sync)
	project
	release_sync "${2:-}"
	;;
release:chat)
	project
	release_chat "${2:-}"
	;;
*)
	echo "usage: $0 project|build|build:macos|build:sim|build:device|build:sync|build:chat|sim:chat|test:chat|test:kit|install|sim|release|release:sync|release:chat [prerelease|stable]" >&2
	exit 2
	;;
esac
