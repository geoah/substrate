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

# Substrate Chat run against its own fixture and timed (apps/chat/DESIGN.md,
# "Measuring"): the Debug configuration, which carries the fixture and the
# driver a release does not, built with the optimizer on so the figures are
# a release's, into a folder of its own so the build the tests use stays as
# it is. The app prints its table and ends.
measure_chat() {
	local derived="build/dd-measure" which="${1:-all}"
	local label="debug-optimized"
	measured() {
		xcodebuild -project "$PROJECT" -scheme SubstrateChat -derivedDataPath "$derived" -quiet -configuration Debug \
			SWIFT_OPTIMIZATION_LEVEL=-O SWIFT_COMPILATION_MODE=wholemodule GCC_OPTIMIZATION_LEVEL=s ENABLE_TESTABILITY=NO "$@"
	}
	table() { sed -n '/measure:begin/,/measure:end/p' | sed '/measure:/d'; }
	if [ "$which" = all ] || [ "$which" = mac ]; then
		measured -destination 'platform=macOS' build
		SUBSTRATE_CHAT_MEASURE="$label" "$derived/Build/Products/Debug/SubstrateChat.app/Contents/MacOS/SubstrateChat" \
			-ApplePersistenceIgnoreState YES 2>/dev/null | table
	fi
	if [ "$which" = all ] || [ "$which" = sim ]; then
		measured -destination 'generic/platform=iOS Simulator' build
		local udid
		udid="$(simulator_udid)"
		if [ -z "$udid" ]; then
			echo "apple: no iOS simulator is installed; add one in Xcode > Settings > Components" >&2
			return 1
		fi
		xcrun simctl boot "$udid" 2>/dev/null || true
		xcrun simctl install "$udid" "$derived/Build/Products/Debug-iphonesimulator/SubstrateChat.app"
		SIMCTL_CHILD_SUBSTRATE_CHAT_MEASURE="$label" xcrun simctl launch --console-pty --terminate-running-process \
			"$udid" dev.reamde.substrate.chat 2>/dev/null | tr -d '\r' | table
	fi
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

# Substrate Tasks: one target for iPhone, iPad and Mac, built for the Mac
# and the simulator. Nothing here registers its bundle id: no
# -allowProvisioningUpdates, so the Mac build is signed with the
# development certificate the machine holds and no profile, which runs it
# here and nowhere else.
tasks() {
	xcodebuild -project "$PROJECT" -scheme SubstrateTasks -derivedDataPath "$DERIVED" -quiet "$@"
}

build_tasks() {
	tasks -destination 'platform=macOS' -configuration Debug build
	tasks -destination 'generic/platform=iOS Simulator' -configuration Debug build
	echo "apple: $DERIVED/Build/Products/Debug/SubstrateTasks.app"
	echo "apple: $DERIVED/Build/Products/Debug-iphonesimulator/SubstrateTasks.app"
}

# Substrate Today: one target for iPhone, iPad and Mac, built as Tasks is.
today() {
	xcodebuild -project "$PROJECT" -scheme SubstrateToday -derivedDataPath "$DERIVED" -quiet "$@"
}

build_today() {
	today -destination 'platform=macOS' -configuration Debug build
	today -destination 'generic/platform=iOS Simulator' -configuration Debug build
	echo "apple: $DERIVED/Build/Products/Debug/SubstrateToday.app"
	echo "apple: $DERIVED/Build/Products/Debug-iphonesimulator/SubstrateToday.app"
}

# Substrate Template, what `apps:new` copies: built and tested like Tasks, so
# the views a new app starts from compile and draw. Never released.
template() {
	xcodebuild -project "$PROJECT" -scheme SubstrateTemplate -derivedDataPath "$DERIVED" -quiet "$@"
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
# name, and the switch to the `-site` entitlements. Every release sets them
# (release_env requires the site), so no released build signs in through a
# loopback port.
site_settings() {
	SITE=()
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
	APPLE_REDIRECT="$(apple_callback "$app")" || exit 1
	site_settings "$app" || exit 1
	SETTINGS=(
		MARKETING_VERSION="$short" CURRENT_PROJECT_VERSION="$build"
		SUBSTRATE_VERSION="$version" SUBSTRATE_CHANNEL="$channel" SUBSTRATE_COMMIT="$commit"
		SUBSTRATE_UPDATES_URL="$(feed_url "$app")/" "SUBSTRATE_$(app_upper "$app")_CLIENT_ID=$APPLE_CLIENT"
		# The feed's build alone installs a newer one from it (project.yml).
		SUBSTRATE_FEED_SWIFT_FLAGS=-DSUBSTRATE_FEED_INSTALLS
		SUBSTRATE_PRIVACY_POLICY="${SUBSTRATE_APPLE_PRIVACY_URL:-}"
		${SITE[@]+"${SITE[@]}"}
	)
	if app_has "$app" macos; then SETTINGS+=(SUBSTRATE_SPARKLE_KEY="$SPARKLE_KEY"); fi
	if app_push "$app"; then SETTINGS+=("SUBSTRATE_PUSH_RELAY=$(push_relay_stamp)"); fi
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

# One app's iPhone and iPad build for TestFlight, archived and exported for
# App Store Connect into $2/iOS and checked as a release's is, with the same
# client id, claimed redirect and associated domain. It carries no feed
# address (SUBSTRATE_UPDATES_URL stays empty), so it never offers the feed's
# development-signed build over itself: TestFlight is what updates it.
# Sets APPLE_IPA, the IPA's path.
# shellcheck disable=SC2154
apple_testflight_build() {
	local app="$1" scratch="$2" scheme entry plist="$2/iOS-Info.plist"
	scheme="$(app_get "$app" .apple.scheme)"
	APPLE_BUNDLE="$(app_get "$app" .apple.bundleID)"
	APPLE_CLIENT="$(client_url "$app")" || exit 1
	APPLE_REDIRECT="$(apple_callback "$app")" || exit 1
	site_settings "$app" || exit 1
	SETTINGS=(
		MARKETING_VERSION="$short" CURRENT_PROJECT_VERSION="$build"
		SUBSTRATE_VERSION="$version" SUBSTRATE_CHANNEL="$channel" SUBSTRATE_COMMIT="$commit"
		"SUBSTRATE_$(app_upper "$app")_CLIENT_ID=$APPLE_CLIENT"
		# The team the site's association file was checked against, so the
		# build is signed as the team that claims its callback.
		DEVELOPMENT_TEAM="$SUBSTRATE_APPLE_TEAM_ID"
		SUBSTRATE_PRIVACY_POLICY="$SUBSTRATE_APPLE_PRIVACY_URL"
		${SITE[@]+"${SITE[@]}"}
	)
	if app_push "$app"; then SETTINGS+=("SUBSTRATE_PUSH_RELAY=$(push_relay_stamp)"); fi
	apple_archive "$app" "$scheme" iOS "$scratch" app-store-connect
	APPLE_IPA="$(find "$scratch/iOS" -maxdepth 1 -name '*.ipa' | head -1)"
	[ -n "$APPLE_IPA" ] || die "$app's App Store Connect export made no IPA"
	entry="$(zipinfo -1 "$APPLE_IPA" | grep -E '^Payload/[^/]+\.app/Info\.plist$' | head -1)" ||
		die "$app's IPA holds no app"
	unzip -p "$APPLE_IPA" "$entry" >"$plist"
	apple_check "$app" iOS "$plist"
	# App Review refuses a binary that can install an app (2.5.2), whether
	# or not the code ever runs: neither the scheme nor the feed's check
	# that leads to it (its "newest release" answer) may be in it at all.
	local payload="$2/iOS-payload-check" found="$2/updater-strings.txt"
	rm -rf "$payload" && unzip -q "$APPLE_IPA" 'Payload/*' -d "$payload"
	if grep -rlaF -e 'itms-services' -e 'This is the newest release' "$payload" >"$found"; then
		die "$app's App Store build holds the feed's install or its check, which App Review refuses (2.5.2): $(tr '\n' ' ' <"$found")"
	fi
	# A build that names a feed says its channel, a pre-release's version
	# and Check for Updates in About (BuildFacts.stamped), which App Review
	# reads as a beta or an updater (2.2, 2.5.2).
	[ -z "$(plutil -extract SubstrateUpdates raw -o - "$plist" 2>/dev/null || true)" ] ||
		die "$app's App Store build names an update feed; SUBSTRATE_UPDATES_URL stays empty for a build Apple updates"
	# App Store Connect refuses an upload that calls a required-reason API
	# its privacy manifest does not declare (ITMS-91053): an app whose
	# target names the manifest must carry it.
	if grep -qF 'PrivacyInfo.xcprivacy' "$APPS_DIR/$app/apple/targets.yml" &&
		[ -z "$(find "$payload/Payload" -maxdepth 2 -name PrivacyInfo.xcprivacy -path '*.app/*')" ]; then
		die "$app's App Store build holds no PrivacyInfo.xcprivacy, which its targets.yml names"
	fi
	rm -rf "$payload"
	# App Review wants the privacy policy inside the app (5.1.1(i)): About
	# opens the address the build carries.
	[ "$(plutil -extract SubstratePrivacyPolicy raw -o - "$plist" 2>/dev/null || true)" = "$SUBSTRATE_APPLE_PRIVACY_URL" ] ||
		die "$app's iOS build does not carry the privacy policy $SUBSTRATE_APPLE_PRIVACY_URL; its Info.plist needs SubstratePrivacyPolicy = \$(SUBSTRATE_PRIVACY_POLICY)"
}

# An exported IPA sent to App Store Connect with the release's API key.
# App Store Connect processes it after this returns, and gives it to the
# TestFlight groups that take every build.
apple_upload() {
	xcrun altool --upload-package "$1" --api-key "$SUBSTRATE_APPLE_ASC_KEY_ID" \
		--api-issuer "$SUBSTRATE_APPLE_ASC_ISSUER_ID" --p8-file-path "$SUBSTRATE_APPLE_ASC_KEY_PATH"
}

# One platform's archive and export, to $4/<platform>. A dry run archives
# unsigned and takes the app out of the archive in place of an export
# (UNSIGNED, unsigned_export), and one that cannot archive stands a
# placeholder in, unless the dry run is strict
# (SUBSTRATE_APPS_RELEASE_DRY_RUN=strict), which fails there as a release
# does, so it passes only where every build was made.
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
		[ "$DRY" != strict ] || die "strict dry run: $app did not archive unsigned for $platform, and a strict dry run stands no placeholder in for a build"
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
	minimum="$("$APPLE_ROOT/.mise/python.sh" - "$APPS_DIR/$app/apple/targets.yml" "$scheme" "$platform" \
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
	if [ "$platform" = iOS ] && app_push "$app"; then plutil -insert SubstratePushRelay -string "$(push_relay_stamp)" "$plist"; fi
	if [ "$platform" = iOS ]; then
		(cd "$scratch/$platform-placeholder" && zip -qr "../iOS/$scheme.ipa" Payload)
	fi
}

# Holds a built app's Info.plist to what the release stamped: the bundle id
# app.json names, the version, the client id and redirect under the keys
# app.json names, and a push app's relay, so a build that would sign in as
# another app, or as a local one, or register with another relay, never
# reaches a feed. Sets APPLE_MINIMUM, the system the
# build needs, which its feed entry carries.
apple_check() {
	local app="$1" platform="$2" plist="$3" client_key redirect_key key expected got
	client_key="$(app_get "$app" .apple.clientIDKey)"
	redirect_key="$(app_get "$app" .apple.redirectKey)"
	# A released build's sign-in is its https client id and the https
	# callback it claims; with either missing the kit would refuse to sign in.
	case "$APPLE_CLIENT" in https://?*/*) ;; *) die "$app's $platform build was stamped with client id '$APPLE_CLIENT', and a release's is an https URL" ;; esac
	case "$APPLE_REDIRECT" in https://?*/?*) ;; *) die "$app's $platform build was stamped with redirect '$APPLE_REDIRECT', and a release claims an https callback" ;; esac
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
	# A push app's iPhone and iPad build registers with the relay it was
	# stamped with, or nowhere where that is `none`; the Mac's takes none yet.
	if [ "$platform" = iOS ] && app_push "$app"; then
		expected="$(push_relay_stamp)"
		got="$(plutil -extract SubstratePushRelay raw -o - "$plist" 2>/dev/null || true)"
		[ "$got" = "$expected" ] || die "$app's $platform build has SubstratePushRelay '$got', and the release stamped '$expected'"
	fi
	if [ "$platform" = macOS ]; then key=LSMinimumSystemVersion; else key=MinimumOSVersion; fi
	APPLE_MINIMUM="$(plutil -extract "$key" raw -o - "$plist" 2>/dev/null || true)"
	[ -n "$APPLE_MINIMUM" ] || die "$app's $platform build names no $key"
}

export_archive() {
	local platform="$1" method="$2" out="$3"
	plutil -create xml1 "$out/$platform-export.plist"
	plutil -insert method -string "$method" "$out/$platform-export.plist"
	if [ "$method" = app-store-connect ]; then
		# An App Store Connect export may otherwise give the build a number of
		# its own, and the build the checks and the upload hold is the stamped one.
		plutil -insert manageAppVersionAndBuildNumber -bool NO "$out/$platform-export.plist"
		store_profiles "$out/$platform.xcarchive" "$out/$platform-export.plist" || return 1
	else
		plutil -insert signingStyle -string automatic "$out/$platform-export.plist"
	fi
	xcodebuild -exportArchive -archivePath "$out/$platform.xcarchive" -exportPath "$out/$platform" \
		-exportOptionsPlist "$out/$platform-export.plist" ${PROVISION[@]+"${PROVISION[@]}"} -quiet || return 1
	# A release claims its sign-in callback on the site, which needs the
	# associated domain in its signed entitlements, and a profile without
	# the capability drops it: such a build could never receive a sign-in.
	require_associated_domains "$out/$platform"
}

# An App Store export signed by hand, into the export plist $2, for every
# bundle the archive $1 holds (the app and each extension inside it): the
# keychain's Apple Distribution identity and an App Store profile each, made
# or reused through the API (appstoreconnect.swift profiles) and installed
# where Xcode reads profiles. Automatic signing would sign with the team's
# cloud-managed certificate, which an API key may not use.
store_profiles() {
	local archive="$1" plist="$2" hashes bundle uuid bundles=() mapping='{}' dir lines
	hashes="$(security find-identity -v -p codesigning | awk '/"Apple Distribution: / {print $2}' | paste -sd, -)"
	[ -n "$hashes" ] || { echo "apple: no Apple Distribution identity is in the keychain; import the one in the release secrets" >&2; return 1; }
	while IFS= read -r -d '' path; do
		bundles+=("$(plutil -extract CFBundleIdentifier raw -o - "$path/Info.plist")")
	done < <(find "$archive/Products/Applications" \( -name '*.app' -o -name '*.appex' \) -type d -print0)
	dir="$HOME/Library/Developer/Xcode/UserData/Provisioning Profiles"
	mkdir -p "$dir"
	# The App ID of an app that takes pushes needs Push Notifications before
	# its App Store profile is made, so a release never depends on a
	# development build having turned it on first.
	local push=() app
	for bundle in "${bundles[@]}"; do
		for app in $(ui_apps); do
			if [ "$(app_get "$app" .apple.bundleID)" = "$bundle" ] && app_push "$app"; then push+=("$bundle"); fi
		done
	done
	lines="$(SUBSTRATE_APPLE_PUSH_BUNDLES="${push[*]-}" xcrun swift "$APPLE_ROOT/apps/xcode/scripts/appstoreconnect.swift" profiles "$dir" "$hashes" "${bundles[@]}")" || return 1
	while read -r bundle uuid; do
		mapping="$(jq -c --arg b "$bundle" --arg u "$uuid" '. + {($b): $u}' <<<"$mapping")"
	done <<<"$lines"
	plutil -insert signingStyle -string manual "$plist"
	plutil -insert signingCertificate -string "Apple Distribution" "$plist"
	plutil -insert teamID -string "${SUBSTRATE_APPLE_TEAM_ID:?set SUBSTRATE_APPLE_TEAM_ID}" "$plist"
	plutil -insert provisioningProfiles -json "$mapping" "$plist"
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
		../../.mise/python.sh ../workouts/scripts/workouts-package.py --check
		../../.mise/python.sh ../workouts/scripts/exercise-library.py --check
		../../.mise/python.sh ../../.mise/closures.py --check workouts
		swift test --package-path ../workouts/WorkoutsKit
		;;
	build:tasks)
		project
		build_tasks
		;;
	test:tasks)
		# TasksKit on the Mac, then the app's views over its fixtures. The
		# closure the app carries must be the one the site publishes.
		../../.mise/python.sh ../../.mise/closures.py --check --json ../tasks/TasksKit/Sources/TasksCore/Resources/Package tasks
		swift test --package-path ../tasks/TasksKit
		project
		tasks -destination 'platform=macOS' test -only-testing:SubstrateTasksTests
		;;
	build:today)
		project
		build_today
		;;
	test:today)
		# TodayKit on the Mac, then the app's views over its fixtures and
		# the design guide's components.
		swift test --package-path ../today/TodayKit
		project
		today -destination 'platform=macOS' test -only-testing:SubstrateTodayTests
		;;
	test:template)
		# TemplateKit on the Mac, then the app's views over its fixtures.
		swift test --package-path ../template/TemplateKit
		project
		template -destination 'platform=macOS' test -only-testing:SubstrateTemplateTests
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
	test:chat:sim)
		# The app target's tests in the iOS app, on a simulator: what an
		# iPhone's scroll view does and a Mac's does not (a chat that opens
		# on its rows at every text size).
		project
		udid="$(simulator_udid)"
		if [ -z "$udid" ]; then
			echo "apple: no iOS simulator is installed; add one in Xcode > Settings > Components" >&2
			exit 1
		fi
		chat -destination "platform=iOS Simulator,id=$udid" test
		;;
	measure:chat)
		project
		measure_chat "${2:-all}"
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
		echo "usage: $0 project|build|build:macos|build:sim|build:device|build:sync|build:chat|build:workouts|build:tasks|build:today|sim:chat|test:chat|measure:chat|test:kit|test:workouts|test:tasks|test:today|test:template|install|sim|release|release:sync|release:chat|release:workouts|site [prerelease|stable]" >&2
		exit 2
		;;
	esac
fi
