#!/usr/bin/env bash
#
# The Apple apps (each app's apple/ folder under apps/, built from the one
# project in apps/xcode): the console, bundled, in a native shell for
# macOS, iPadOS and iOS. Every `mise run apple:*` task is one subcommand here,
# and a release's Apple half is the functions .mise/apps.sh calls once it has
# sourced this file.
#
# The console is built by `console:build` and copied into the bundle by the
# Xcode target's own script phase, so an app always carries the dist/ beside
# it. Signing is Xcode's automatic signing for the team in project.yml; the
# account signed in to Xcode is what -allowProvisioningUpdates registers the
# app id and the device profile with.
set -euo pipefail

APPLE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$APPLE_ROOT/apps/xcode"

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

# xcodegen run from where mise installed it, not through `mise x`, which
# would load this machine's configuration into it, and with it the keys a
# release reads from the environment (and a dry run has unset).
project() {
	local bin
	bin="$(mise bin-paths "$XCODEGEN" 2>/dev/null | head -1)"
	if [ ! -x "$bin/xcodegen" ]; then
		mise install --quiet "$XCODEGEN" >/dev/null
		bin="$(mise bin-paths "$XCODEGEN" | head -1)"
	fi
	"$bin/xcodegen" generate --quiet
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

# Substrate Workouts: the iPhone app and its Live Activity extension.
build_workouts() {
	xcodebuild -project "$PROJECT" -scheme SubstrateWorkouts -derivedDataPath "$DERIVED" \
		-allowProvisioningUpdates ${AUTH[@]+"${AUTH[@]}"} -quiet -destination 'generic/platform=iOS Simulator' -configuration Debug build
	echo "apple: $DERIVED/Build/Products/Debug-iphonesimulator/SubstrateWorkouts.app"
}

# --- a release's Apple half ------------------------------------------------
#
# .mise/apps.sh sources this file and calls what follows for every app its
# app.json says ships on iOS or macOS: the scheme to archive, the bundle id,
# the Info.plist keys the client id and the redirect are stamped into, and
# the icon. The rest of a release (the version, the lease, the site, the
# staging and the feeds) is apps.sh's.

# xcodebuild's leave to register app ids and refresh profiles with the
# account signed in to Xcode, or the App Store Connect key: a release's
# alone. A dry run never passes it, so it registers nothing.
PROVISION=()

# A dry run's builds are never signed: every setting that would have Xcode
# pick an identity or a profile is switched off for the archive, which
# reads no keychain then, and the export (which signs whatever it exports)
# is skipped for the app inside the archive. Each build is still stamped and
# checked as a release's; the one thing it lacks is a signature.
UNSIGNED=(CODE_SIGNING_ALLOWED=NO CODE_SIGNING_REQUIRED=NO "CODE_SIGN_IDENTITY=" DEVELOPMENT_TEAM=
	"PROVISIONING_PROFILE_SPECIFIER=" CODE_SIGN_STYLE=Manual)

# Whether any app this release builds ships on an Apple system.
apple_needed() {
	local app
	for app in "${RELEASE_APPS[@]}"; do
		app_has_apple "$app" && return 0
	done
	return 1
}

# Once per release: the project generated, Sparkle's tools resolved, and
# the EdDSA key Sparkle verifies every update with, whose private half stays
# in this login keychain, made on the first release. Losing it means every
# installed Mac app must be replaced by hand once. A dry run reads no
# keychain and writes none: its builds carry a placeholder key.
apple_prepare() {
	if [ -z "$DRY" ]; then
		PROVISION=(-allowProvisioningUpdates ${AUTH[@]+"${AUTH[@]}"})
	fi
	SPARKLE_KEY="dry-run"
	[ -z "${SUBSTRATE_APPS_RELEASE_PLACEHOLDERS:-}" ] || return 0
	project
	local app mac=""
	for app in "${RELEASE_APPS[@]}"; do
		if app_has "$app" macos; then
			mac="$(app_get "$app" .apple.scheme)"
			break
		fi
	done
	[ -n "$mac" ] || return 0
	xcodebuild -resolvePackageDependencies -project "$PROJECT" -scheme "$mac" -derivedDataPath "$DERIVED" -quiet
	[ -z "$DRY" ] || return 0
	SPARKLE_KEY="$("$SPARKLE/generate_keys" --account "$SPARKLE_ACCOUNT" -p 2>/dev/null)" || {
		"$SPARKLE/generate_keys" --account "$SPARKLE_ACCOUNT" >/dev/null
		SPARKLE_KEY="$("$SPARKLE/generate_keys" --account "$SPARKLE_ACCOUNT" -p)"
	}
}

# The build settings that make a release claim its site's redirect, for the
# app named in $1, in the global SITE (an array, since bash 3.2 has no
# mapfile): the redirect the manifest reads, the site host its entitlements
# name, and the switch to the `-site` entitlements. Empty without a site.
site_settings() {
	SITE=()
	[ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ] || return 0
	local site
	site="$(site_url)" || return 1
	SITE=("SUBSTRATE_$(app_upper "$1")_REDIRECT=$(apple_callback "$1")" "SUBSTRATE_SITE_HOST=${site#https://}"
		SUBSTRATE_ASSOCIATED_DOMAINS=YES)
}

# One app's Apple builds, archived, exported, checked and packaged into the
# staged release folder $3 (releases/<build>/ under its feed), with the
# release.json entries they make written to $2/apple.json: `macos`,
# Sparkle's signed zip, and `ios`, the IPA its install manifest names. Its
# version is the one every other app and platform of the release carries.
# Signing is the archive's identity unless SUBSTRATE_APPLE_EXPORT_IOS /
# _MACOS names another export method (release-testing for ad hoc,
# developer-id for a notarizable Mac build). The release's stamp (short,
# build, version, channel, commit) is .mise/apps.sh's.
# shellcheck disable=SC2154
apple_release_build() {
	local app="$1" scratch="$2" dir="$3" scheme
	scheme="$(app_get "$app" .apple.scheme)"
	APPLE_BUNDLE="$(app_get "$app" .apple.bundleID)"
	APPLE_CLIENT="$(client_url "$app")" || exit 1
	APPLE_REDIRECT=""
	[ -z "${SUBSTRATE_APPLE_SITE_URL:-}" ] || APPLE_REDIRECT="$(apple_callback "$app")"
	site_settings "$app" || exit 1
	SETTINGS=(
		MARKETING_VERSION="$short" CURRENT_PROJECT_VERSION="$build"
		SUBSTRATE_VERSION="$version" SUBSTRATE_CHANNEL="$channel" SUBSTRATE_COMMIT="$commit"
		SUBSTRATE_UPDATES_URL="$(feed_url "$app")/" "SUBSTRATE_$(app_upper "$app")_CLIENT_ID=$APPLE_CLIENT"
		${SITE[@]+"${SITE[@]}"}
	)
	if app_has "$app" macos; then SETTINGS+=(SUBSTRATE_SPARKLE_KEY="$SPARKLE_KEY"); fi
	local name="$scheme-${version%%+*}" entries='{}'
	if app_has "$app" macos; then
		apple_archive "$app" "$scheme" macOS "$scratch" "${SUBSTRATE_APPLE_EXPORT_MACOS:-debugging}"
		local bundle="$scratch/macOS/$scheme.app" mac="releases/$build/$name-macOS.zip" signature="dry-run"
		apple_check "$app" macOS "$bundle/Contents/Info.plist"
		# A dry run's build was held to carrying no signature (unsigned_export).
		[ -n "$DRY" ] || codesign --verify --deep --strict "$bundle"
		ditto -c -k --sequesterRsrc --keepParent "$bundle" "$dir/${mac##*/}"
		[ -n "$DRY" ] || signature="$("$SPARKLE/sign_update" --account "$SPARKLE_ACCOUNT" -p "$dir/${mac##*/}")"
		entries="$(jq -c --arg path "$mac" --argjson length "$(stat -f %z "$dir/${mac##*/}")" \
			--arg sum "$(shasum -a 256 "$dir/${mac##*/}" | cut -d' ' -f1)" --arg minimum "$APPLE_MINIMUM" \
			--arg signature "$signature" \
			'. + {macos: {path: $path, length: $length, sha256: $sum, minimumSystemVersion: $minimum, edSignature: $signature}}' <<<"$entries")"
	fi
	if app_has "$app" ios; then
		apple_archive "$app" "$scheme" iOS "$scratch" "${SUBSTRATE_APPLE_EXPORT_IOS:-debugging}"
		# The export names the IPA after the app's display name.
		local exported=("$scratch/iOS"/*.ipa) ipa="releases/$build/$name-iOS.ipa" plist="$scratch/iOS-Info.plist" entry
		entry="$(zipinfo -1 "${exported[0]}" | grep -E '^Payload/[^/]+\.app/Info\.plist$' | head -1)"
		unzip -p "${exported[0]}" "$entry" >"$plist"
		apple_check "$app" iOS "$plist"
		cp "${exported[0]}" "$dir/${ipa##*/}"
		entries="$(jq -c --arg path "$ipa" --argjson length "$(stat -f %z "$dir/${ipa##*/}")" \
			--arg sum "$(shasum -a 256 "$dir/${ipa##*/}" | cut -d' ' -f1)" --arg minimum "$APPLE_MINIMUM" \
			--arg rel "releases/$build" \
			'. + {ios: {path: $path, length: $length, sha256: $sum, minimumSystemVersion: $minimum,
				manifest: "\($rel)/manifest.plist", icon: "\($rel)/icon-512.png", smallIcon: "\($rel)/icon-57.png"}}' <<<"$entries")"
	fi
	echo "$entries" >"$scratch/apple.json"
}

# One platform's archive and export, to $4/<platform>. A dry run archives
# unsigned and takes the app out of the archive in place of an export
# (UNSIGNED, unsigned_export), and one that cannot archive stands a
# placeholder in.
apple_archive() {
	local app="$1" scheme="$2" platform="$3" scratch="$4" method="$5"
	if [ -n "${SUBSTRATE_APPS_RELEASE_PLACEHOLDERS:-}" ]; then
		apple_placeholder "$app" "$scheme" "$platform" "$scratch"
		return
	fi
	if [ -n "$DRY" ]; then
		if xcodebuild -project "$PROJECT" -scheme "$scheme" -derivedDataPath "$DERIVED" -quiet \
			-destination "generic/platform=$platform" -configuration Release archive \
			-archivePath "$scratch/$platform.xcarchive" "${SETTINGS[@]}" "${UNSIGNED[@]}" &&
			unsigned_export "$platform" "$scratch"; then
			return 0
		fi
		say "dry run: $app did not archive unsigned for $platform; a placeholder stands in for it"
		apple_placeholder "$app" "$scheme" "$platform" "$scratch"
		return
	fi
	if ! xcodebuild -project "$PROJECT" -scheme "$scheme" -derivedDataPath "$DERIVED" ${PROVISION[@]+"${PROVISION[@]}"} -quiet \
		-destination "generic/platform=$platform" -configuration Release archive \
		-archivePath "$scratch/$platform.xcarchive" "${SETTINGS[@]}" ||
		! export_archive "$platform" "$method" "$scratch"; then
		die "$app did not archive and export for $platform"
	fi
}

# A dry run's export: the app the unsigned archive holds, as the export
# would have laid it out in $2/<platform> (the Mac app itself, the iOS app
# zipped into an IPA's Payload/), with no signature added.
unsigned_export() {
	local platform="$1" out="$2" archived=() name
	archived=("$out/$platform.xcarchive/Products/Applications"/*.app)
	[ "${#archived[@]}" = 1 ] && [ -d "${archived[0]}" ] || return 1
	name="$(basename "${archived[0]}")"
	rm -rf "${out:?}/$platform" "$out/$platform-payload"
	mkdir -p "$out/$platform"
	if [ "$platform" = macOS ]; then
		ditto "${archived[0]}" "$out/macOS/$name"
	else
		mkdir -p "$out/$platform-payload/Payload"
		ditto "${archived[0]}" "$out/$platform-payload/Payload/$name"
		(cd "$out/$platform-payload" && zip -qry "../$platform/${name%.app}.ipa" Payload)
	fi
	apple_unsigned "$APPLE_BUNDLE for $platform" "${archived[0]}"
}

# Holds a dry run's build to what a dry run promises: signed by no identity.
# The linker's own ad hoc signature on an arm64 executable is no identity,
# and is all codesign may find.
apple_unsigned() {
	local what="$1" bundle="$2" info
	info="$(codesign -dv "$bundle" 2>&1 || true)"
	if printf '%s\n' "$info" | grep -q '^Authority='; then
		die "dry run: $what was signed with $(printf '%s\n' "$info" | sed -n 's/^Authority=//p' | head -1), and a dry run signs nothing"
	fi
	say "dry run: $what is unsigned: $(printf '%s\n' "$info" | grep -E '^Signature=|^CodeDirectory|code object is not signed' | head -1)"
}

# A dry run's stand-in for one platform's export: the app bundle's
# Info.plist alone, holding what the build would have been stamped with and
# the system its target is deployed to, so every step after it runs as it
# would.
apple_placeholder() {
	local app="$1" scheme="$2" platform="$3" scratch="$4" minimum plist key
	rm -rf "${scratch:?}/$platform" "$scratch/$platform-placeholder"
	mkdir -p "$scratch/$platform"
	touch "$scratch/$platform/.placeholder"
	minimum="$(uv run --quiet --with pyyaml python3 - "$APPS_DIR/$app/apple/targets.yml" "$scheme" "$platform" \
		"$APPS_DIR/xcode/project.yml" <<'PY'
import sys

import yaml

targets, scheme, platform, project = sys.argv[1:]
target = (yaml.safe_load(open(targets)).get("targets") or {}).get(scheme) or {}
minimum = target.get("deploymentTarget")
if isinstance(minimum, dict):
    minimum = minimum.get(platform)
if not minimum:
    minimum = ((yaml.safe_load(open(project)).get("options") or {}).get("deploymentTarget") or {}).get(platform)
print(minimum)
PY
	)"
	if [ "$platform" = macOS ]; then
		plist="$scratch/macOS/$scheme.app/Contents/Info.plist"
		key=LSMinimumSystemVersion
	else
		plist="$scratch/$platform-placeholder/Payload/$scheme.app/Info.plist"
		key=MinimumOSVersion
	fi
	mkdir -p "$(dirname "$plist")"
	plutil -create xml1 "$plist"
	plutil -insert CFBundleIdentifier -string "$APPLE_BUNDLE" "$plist"
	plutil -insert CFBundleShortVersionString -string "$short" "$plist"
	plutil -insert CFBundleVersion -string "$build" "$plist"
	plutil -insert "$key" -string "$minimum" "$plist"
	plutil -insert "$(app_get "$app" .apple.clientIDKey)" -string "$APPLE_CLIENT" "$plist"
	plutil -insert "$(app_get "$app" .apple.redirectKey)" -string "$APPLE_REDIRECT" "$plist"
	if [ "$platform" = iOS ]; then
		(cd "$scratch/$platform-placeholder" && zip -qr "../iOS/$scheme.ipa" Payload)
	fi
}

# Holds a built app's Info.plist to what the release stamped: the bundle id
# app.json names, the version, and the client id and redirect under the
# keys app.json names, so a build that would sign in as another app, or as
# a local one, never reaches a feed. Sets APPLE_MINIMUM, the system the
# build needs, which its feed entry carries.
apple_check() {
	local app="$1" platform="$2" plist="$3" client_key redirect_key key expected got
	client_key="$(app_get "$app" .apple.clientIDKey)"
	redirect_key="$(app_get "$app" .apple.redirectKey)"
	for key in CFBundleIdentifier CFBundleShortVersionString CFBundleVersion "$client_key" "$redirect_key"; do
		case "$key" in
		CFBundleIdentifier) expected="$APPLE_BUNDLE" ;;
		CFBundleShortVersionString) expected="$short" ;;
		CFBundleVersion) expected="$build" ;;
		"$client_key") expected="$APPLE_CLIENT" ;;
		*) expected="$APPLE_REDIRECT" ;;
		esac
		got="$(plutil -extract "$key" raw -o - "$plist" 2>/dev/null || true)"
		[ "$got" = "$expected" ] || die "$app's $platform build has $key '$got', and the release stamped '$expected'"
	done
	if [ "$platform" = macOS ]; then key=LSMinimumSystemVersion; else key=MinimumOSVersion; fi
	APPLE_MINIMUM="$(plutil -extract "$key" raw -o - "$plist" 2>/dev/null || true)"
	[ -n "$APPLE_MINIMUM" ] || die "$app's $platform build names no $key"
}

export_archive() {
	local platform="$1" method="$2" out="$3"
	plutil -create xml1 "$out/$platform-export.plist"
	plutil -insert method -string "$method" "$out/$platform-export.plist"
	plutil -insert signingStyle -string automatic "$out/$platform-export.plist"
	xcodebuild -exportArchive -archivePath "$out/$platform.xcarchive" -exportPath "$out/$platform" \
		-exportOptionsPlist "$out/$platform-export.plist" ${PROVISION[@]+"${PROVISION[@]}"} -quiet || return 1
	# A release that claims its sign-in callback on the site needs the
	# associated domain in its signed entitlements, which a profile without
	# the capability drops: such a build could never receive a sign-in.
	if [ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ]; then
		require_associated_domains "$out/$platform"
	fi
}

# Refuses an exported app (an .ipa, or a Mac .app) whose signature carries no
# associated domain.
require_associated_domains() {
	local dir="$1" app tmp="" ipa
	ipa="$(find "$dir" -maxdepth 1 -name '*.ipa' | head -1)"
	if [ -n "$ipa" ]; then
		tmp="$(mktemp -d)"
		unzip -q -o "$ipa" -d "$tmp"
		app="$(find "$tmp/Payload" -maxdepth 1 -name '*.app' | head -1)"
	else
		app="$(find "$dir" -maxdepth 1 -name '*.app' | head -1)"
	fi
	if [ -z "$app" ] || ! codesign -d --entitlements - --xml "$app" 2>/dev/null | grep -q 'com.apple.developer.associated-domains'; then
		[ -n "$tmp" ] && rm -rf "$tmp"
		echo "apple: ${app:-$dir} is signed without the associated domain its sign-in needs; turn Associated Domains on for its App ID and release again" >&2
		return 1
	fi
	[ -n "$tmp" ] && rm -rf "$tmp"
	return 0
}

# Run, not sourced: .mise/apps.sh sources this file for the functions above.
if [ "${BASH_SOURCE[0]}" = "$0" ]; then
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
	build:workouts)
		project
		build_workouts
		;;
	test:workouts)
		# WorkoutsKit is a Swift package too: the model, both engines and the
		# sync, on the Mac. The package's manifests and their JSON must agree,
		# and so must the exercise library and its sources.
		uv run --quiet --with pyyaml ../workouts/scripts/workouts-package.py --check
		uv run --quiet --with pyyaml ../workouts/scripts/exercise-library.py --check
		uv run --quiet --with pyyaml ../../.mise/closures.py --check workouts
		swift test --package-path ../workouts/WorkoutsKit
		;;
	test:kit)
		# SubstrateKit is a Swift package: its tests run on the Mac with swift
		# test, no project or simulator.
		swift test --package-path ../kit
		;;
	test:chat)
		# ChatKit is a Swift package: the chat's rules, on the Mac with swift
		# test; then what only the Apple app holds, in its test target.
		swift test --package-path ../chat/ChatKit
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
	# One release's aliases, so the old names keep working while hands and
	# docs move to `apps:release` and `apps:site`: each is its one app's
	# release (apple:release was the console's), or the site. They go with the
	# release after the first joint one.
	release) exec "$APPLE_ROOT/.mise/apps.sh" release "${2:-prerelease}" console ;;
	release:sync) exec "$APPLE_ROOT/.mise/apps.sh" release "${2:-prerelease}" sync ;;
	release:chat) exec "$APPLE_ROOT/.mise/apps.sh" release "${2:-prerelease}" chat ;;
	release:workouts) exec "$APPLE_ROOT/.mise/apps.sh" release "${2:-prerelease}" workouts ;;
	site)
		# apple:site's dry run was SUBSTRATE_APPLE_SITE_DRY_RUN.
		SUBSTRATE_APPS_RELEASE_DRY_RUN="${SUBSTRATE_APPS_RELEASE_DRY_RUN:-${SUBSTRATE_APPLE_SITE_DRY_RUN:-}}" \
			exec "$APPLE_ROOT/.mise/apps.sh" site
		;;
	*)
		echo "usage: $0 project|build|build:macos|build:sim|build:device|build:sync|build:chat|build:workouts|sim:chat|test:chat|test:kit|test:workouts|install|sim|release|release:sync|release:chat|release:workouts|site [prerelease|stable]" >&2
		exit 2
		;;
	esac
fi
