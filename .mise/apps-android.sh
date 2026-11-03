# shellcheck shell=bash
# The release's stamp (version, build, channel, commit) is .mise/apps.sh's.
# shellcheck disable=SC2154
#
# A release's Android half. .mise/apps.sh sources this file and calls
# android_release_build for every app its app.json says ships on Android
# (an apps/<app>/android/ folder and `android.applicationID`). The APK
# itself comes from one narrow interface, the `android:release:build <app>`
# task's program (.mise/android.sh release:build), which writes the app's
# signed release APK to SUBSTRATE_RELEASE_APK and is handed, in an
# environment made for it (android_builder_env) and nothing else:
#
#   SUBSTRATE_RELEASE_VERSION       the release's version, the APK's versionName
#   SUBSTRATE_RELEASE_VERSION_CODE  the versionCode allocated under the lease
#   SUBSTRATE_RELEASE_CHANNEL       prerelease or stable
#   SUBSTRATE_RELEASE_COMMIT        the commit, with .dirty for a dirty tree
#   SUBSTRATE_RELEASE_UPDATES_URL   the app's feed folder, where its updater reads index.json
#   SUBSTRATE_RELEASE_CLIENT_ID     its client ID metadata document's URL
#   SUBSTRATE_RELEASE_REDIRECT      the https callback it claims on the site
#   SUBSTRATE_RELEASE_SITE_HOST     the site's host, for its verified links
#
# and the signing key's SUBSTRATE_ANDROID_KEYSTORE, _KEYSTORE_PASSWORD,
# _KEY_ALIAS and _KEY_PASSWORD, which never enter the tree (apps/README.md).
# Everything after it is read back from the APK: the signature, the
# certificate it was signed with (what assetlinks.json names), the package,
# the versionCode and versionName, and the minimum SDK.

# The minutes from 2026-01-01T00:00Z to a build number (YYYYMMDD.HHMMSS, UTC):
# a versionCode's floor that rises with the build, on any machine.
version_code_minutes() {
	local stamp="$1" seconds
	seconds="$(TZ=UTC date -j -f %Y%m%d.%H%M%S "$stamp" +%s 2>/dev/null ||
		date -u -d "${stamp:0:4}-${stamp:4:2}-${stamp:6:2}T${stamp:9:2}:${stamp:11:2}:${stamp:13:2}Z" +%s)"
	echo $(((seconds - 1767225600) / 60))
}

# The build-tools apksigner and aapt2 come from: the pinned version
# (.mise/android.sh's sdk_packages) under ANDROID_HOME.
android_tool() {
	local home="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Library/Android/sdk}}" pinned
	pinned="$(sed -n 's/^  "build-tools;\(.*\)"$/\1/p' "$ROOT/.mise/android.sh" | head -1)"
	[ -x "$home/build-tools/$pinned/$1" ] || die "no $1 at $home/build-tools/$pinned; run \`mise run android:sdk\`"
	echo "$home/build-tools/$pinned/$1"
}

# A certificate digest as assetlinks.json spells it: upper-case hex pairs
# joined by colons.
colon_hex() {
	printf '%s' "$1" | tr '[:lower:]' '[:upper:]' | sed 's/../&:/g; s/:$//'
}

# The PATH entries of the android:release:build task's tools (its `tools =`
# in .mise.toml: the open-source Swift and the JDK), as mise resolves them,
# without running anything under mise's environment.
android_tools_path() {
	local tools paths entry
	tools="$(cd "$ROOT" && mise tasks info android:release:build --json | jq -r '.tools | to_entries[] | "\(.key)@\(.value)"')" ||
		die "mise could not say which tools android:release:build runs with"
	# shellcheck disable=SC2086
	paths="$(cd "$ROOT" && mise bin-paths $tools)" || die "mise could not say where android:release:build's tools are"
	while read -r entry; do
		[ -d "$entry" ] || die "$entry is not there; install android:release:build's tools with \`mise install ${tools//$'\n'/ }\`"
	done <<<"$paths"
	printf '%s\n' "$paths" | paste -sd: -
}

# Runs android:release:build's program, or $SUBSTRATE_APPS_RELEASE_ANDROID_BUILD
# in a dry run, in an environment made from nothing but what it needs: the
# release's stamp, the signing key's variables (which android.sh hands to
# apksigner alone), the Android SDK, the task's tools and the system's
# PATH, minus mise's shims. It is called directly rather than through `mise
# run`, because mise would load this machine's configuration into it,
# .mise.local.toml and the env files it names included, and with them the
# bucket's and App Store Connect's keys, which Gradle, its plugins and its
# daemon would then hold. The rest of this shell's environment (a dry run
# has unset the keys, a release has not) is left behind for the same
# reason. A secret never reaches a command line: the variables are exported
# into a subshell that clears the rest, never passed to env(1) as
# arguments.
android_builder_env() {
	local tools java keep var
	tools="$(android_tools_path)"
	java="$(printf '%s\n' "$tools" | tr ':' '\n' | grep '/java/' | head -1)"
	keep=" HOME USER LOGNAME TMPDIR LANG LC_ALL TERM ANDROID_HOME ANDROID_SDK_ROOT SUBSTRATE_ABIS
		SUBSTRATE_ANDROID_KEYSTORE SUBSTRATE_ANDROID_KEYSTORE_PASSWORD SUBSTRATE_ANDROID_KEY_ALIAS SUBSTRATE_ANDROID_KEY_PASSWORD "
	keep="$(printf '%s' "$keep" | tr '\n\t' '  ')"
	(
		local path
		path="$tools:$(printf '%s' "$PATH" | tr ':' '\n' | grep -v '/mise/shims$' | paste -sd: -)"
		for var in $(compgen -e); do
			case "$keep" in *" $var "*) ;; *) unset "$var" 2>/dev/null || true ;; esac
		done
		export PATH="$path"
		[ -z "$java" ] || export JAVA_HOME="${java%/bin}"
		export SUBSTRATE_RELEASE_APK="$1" SUBSTRATE_RELEASE_VERSION="$version" SUBSTRATE_RELEASE_VERSION_CODE="$2" \
			SUBSTRATE_RELEASE_CHANNEL="$channel" SUBSTRATE_RELEASE_COMMIT="$commit" SUBSTRATE_RELEASE_UPDATES_URL="$3" \
			SUBSTRATE_RELEASE_CLIENT_ID="$4" SUBSTRATE_RELEASE_REDIRECT="$5" SUBSTRATE_RELEASE_SITE_HOST="$6"
		shift 6
		cd "$ROOT" || exit 1
		exec "$@"
	)
}

# One app's Android build: the APK built and signed by android:release:build,
# checked, and staged into the release folder $3 (releases/<build>/ under
# its feed) under the versionCode $4, with its release.json entry written to
# $2/android.json. SUBSTRATE_APPS_RELEASE_ANDROID_BUILD names another
# program to call in its place, the same way, which only a dry run may.
android_release_build() {
	local app="$1" scratch="$2" dir="$3" code="$4" apk
	apk="$scratch/android/$app.apk"
	rm -rf "$scratch/android"
	mkdir -p "$scratch/android"
	local client redirect site
	client="$(client_url "$app")" || exit 1
	redirect="$(android_callback "$app")"
	site="$(site_url)"
	local builder=("$ROOT/.mise/android.sh" release:build)
	if [ -n "${SUBSTRATE_APPS_RELEASE_ANDROID_BUILD:-}" ]; then
		[ -n "$DRY" ] || die "SUBSTRATE_APPS_RELEASE_ANDROID_BUILD stands another build in for android:release:build, which only a dry run may"
		builder=("$SUBSTRATE_APPS_RELEASE_ANDROID_BUILD")
		say "dry run: $SUBSTRATE_APPS_RELEASE_ANDROID_BUILD builds $app's APK in place of android:release:build"
	fi
	android_builder_env "$apk" "$code" "$(feed_url "$app")/" "$client" "$redirect" "${site#https://}" \
		"${builder[@]}" "$app" || die "$app's Android build failed"
	[ -f "$apk" ] || die "$app's Android build wrote no APK to $apk"
	android_check "$app" "$apk" "$code"
	local file
	file="releases/$build/$(app_get "$app" .name | tr -d ' ')-${version%%+*}-android.apk"
	cp "$apk" "$dir/${file##*/}"
	jq -n --arg path "$file" --argjson length "$(stat -f %z "$dir/${file##*/}")" \
		--arg sum "$(shasum -a 256 "$dir/${file##*/}" | cut -d' ' -f1)" --arg minimum "$ANDROID_MINIMUM" \
		--arg package "$(app_get "$app" .android.applicationID)" --argjson code "$code" --arg certificate "$ANDROID_CERTIFICATE" \
		'{android: {path: $path, length: $length, sha256: $sum, minimumSystemVersion: $minimum,
			applicationId: $package, versionCode: $code, certificateSHA256: $certificate}}' >"$scratch/android.json"
}

# Holds a built APK to what the release asked for: a signature apksigner
# verifies, made with the certificate SUBSTRATE_ANDROID_CERT_SHA256 names
# (required for a release, so a debug key never ships; a dry run checks it
# where it is set), the application id app.json names, the versionCode
# allocated, the release's version, and not debuggable. Sets
# ANDROID_CERTIFICATE, read from the signed APK itself, and ANDROID_MINIMUM.
android_check() {
	local app="$1" apk="$2" code="$3" certs badging digest
	certs="$("$(android_tool apksigner)" verify --print-certs "$apk" 2>&1)" ||
		die "$app's APK does not verify: $(printf '%s' "$certs" | tail -1)"
	[ "$(printf '%s\n' "$certs" | grep -c 'certificate SHA-256 digest:')" = 1 ] ||
		die "$app's APK is signed by $(printf '%s\n' "$certs" | grep -c 'certificate SHA-256 digest:') signers, and a release by one"
	digest="$(printf '%s\n' "$certs" | sed -n 's/^Signer #1 certificate SHA-256 digest: *//p')"
	ANDROID_CERTIFICATE="$(colon_hex "$digest")"
	local expected="${SUBSTRATE_ANDROID_CERT_SHA256:-}"
	if [ -z "$expected" ] && [ -z "$DRY" ]; then
		die "set SUBSTRATE_ANDROID_CERT_SHA256 to the release key's certificate fingerprint, so an APK signed with any other key is refused"
	fi
	if [ -n "$expected" ] && [ "$(colon_hex "$(printf '%s' "$expected" | tr -d ':')")" != "$ANDROID_CERTIFICATE" ]; then
		die "$app's APK is signed with $ANDROID_CERTIFICATE, and SUBSTRATE_ANDROID_CERT_SHA256 names $expected"
	fi
	badging="$("$(android_tool aapt2)" dump badging "$apk")" || die "$app's APK has no manifest aapt2 reads"
	local package got_code got_name
	package="$(printf '%s\n' "$badging" | sed -n "s/^package: name='\([^']*\)'.*/\1/p")"
	got_code="$(printf '%s\n' "$badging" | sed -n "s/^package: .* versionCode='\([^']*\)'.*/\1/p")"
	got_name="$(printf '%s\n' "$badging" | sed -n "s/^package: .* versionName='\([^']*\)'.*/\1/p")"
	ANDROID_MINIMUM="$(printf '%s\n' "$badging" | sed -n "s/^minSdkVersion:'\([^']*\)'.*/\1/p")"
	[ "$package" = "$(app_get "$app" .android.applicationID)" ] || die "$app's APK is $package, and app.json names $(app_get "$app" .android.applicationID)"
	[ "$got_code" = "$code" ] || die "$app's APK has versionCode $got_code, and the release allocated $code"
	[ "$got_name" = "$version" ] || die "$app's APK has versionName $got_name, and the release is $version"
	[ -n "$ANDROID_MINIMUM" ] || die "$app's APK names no minSdkVersion"
	if printf '%s\n' "$badging" | grep -q '^application-debuggable'; then
		die "$app's APK is debuggable, which a release never is"
	fi
	say "$app's APK: $package $got_code, signed with $ANDROID_CERTIFICATE"
}
