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
# Its host heads the owner's approval page and, for an app no authority
# document lists, names the app's actor (`app:<host>`; a verified app writes
# as `app:<authority>` and a loopback build as `app:local`, decision 0209),
# so it is a host the publisher alone serves: the site's
# `/apps/<app>/client.json` when SUBSTRATE_APPLE_SITE_URL is set (see
# publish_site), else SUBSTRATE_APPLE_CLIENT_URL, an https folder there the
# same key may write (a custom domain, or a virtual-hosted bucket), else the
# release folder. Every app published under one host shares that actor. A
# shared endpoint, where anyone may publish a document under the same host,
# is refused.
client_url() {
	local app="$1"
	if [ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ]; then
		local site
		site="$(site_url)" || return 1
		echo "$site/apps/$app/client.json"
		return
	fi
	local base="${SUBSTRATE_APPLE_CLIENT_URL:-$SUBSTRATE_APPLE_RELEASE_URL}"
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

# The apps the site speaks for, by the folder name each is published under.
readonly APPS="console chat sync workouts"

app_name() {
	case "$1" in
	console) echo "Substrate Console" ;;
	chat) echo "Substrate Chat" ;;
	sync) echo "Substrate Sync" ;;
	workouts) echo "Substrate Workouts" ;;
	esac
}

# Every app's bundle id is the project's prefix and its folder name.
app_bundle() {
	echo "dev.reamde.substrate.$1"
}

# SUBSTRATE_APPLE_SITE_URL as the origin it must be: https, a host of its
# own and nothing after it, because Apple reads the association file from
# `https://<host>/.well-known/apple-app-site-association` and from nowhere
# else. A shared endpoint is refused as client_url refuses it.
site_url() {
	local site="${SUBSTRATE_APPLE_SITE_URL%/}"
	case "$site" in
	https://*) ;;
	*)
		echo "apple: SUBSTRATE_APPLE_SITE_URL is an https origin: $site" >&2
		return 1
		;;
	esac
	local host="${site#https://}"
	case "$host" in
	"" | */* | *:* | *@*)
		echo "apple: SUBSTRATE_APPLE_SITE_URL is an origin served at its root, with no path, port or user, because Apple reads the association file from the host's root: $site" >&2
		return 1
		;;
	esac
	host="$(printf '%s' "$host" | tr '[:upper:]' '[:lower:]')"
	host="${host%.}"
	if shared_host "$host"; then
		echo "apple: $host is a shared endpoint, where anyone may publish documents under the same host; set SUBSTRATE_APPLE_SITE_URL to a host of your own" >&2
		return 1
	fi
	echo "https://$host"
}

# The build settings that make a release claim its site's redirect, for the
# app named in $1, in the global SITE (an array, since bash 3.2 has no
# mapfile): the redirect the manifest reads, the site host its entitlements
# name, and the switch to the `-site` entitlements. Empty without a site.
site_settings() {
	SITE=()
	[ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ] || return 0
	local site upper
	site="$(site_url)" || return 1
	upper="$(printf '%s' "$1" | tr '[:lower:]' '[:upper:]')"
	SITE=("SUBSTRATE_${upper}_REDIRECT=$site/apps/$1/callback" "SUBSTRATE_SITE_HOST=${site#https://}"
		SUBSTRATE_ASSOCIATED_DOMAINS=YES)
}

# The page an app's client document links to: its release folder's install
# page, else the site's root.
release_page() {
	local base="${SUBSTRATE_APPLE_RELEASE_URL:-}"
	base="${base%/}"
	if [ -z "$base" ]; then
		site_url | sed 's|$|/|'
	elif [ "$1" = console ]; then
		echo "$base/index.html"
	else
		echo "$base/$1/index.html"
	fi
}

# The client ID metadata document of the app named in $1, on stdout. With a
# site, its one redirect is the https callback the app claims there and it
# names the authority that speaks for it (`substrate_authority`); without
# one, a loopback redirect (RFC 8252), which any program on the device may
# also use, so the approval page cannot tell the app from another.
client_document() {
	local app="$1" client page
	client="$(client_url "$app")" || return 1
	page="$(release_page "$app")" || return 1
	if [ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ]; then
		local site
		site="$(site_url)" || return 1
		jq -n --arg id "$client" --arg name "$(app_name "$app")" --arg uri "$page" \
			--arg redirect "$site/apps/$app/callback" \
			--arg authority "${SUBSTRATE_APPLE_SITE_AUTHORITY:-${site#https://}}" '{
			client_id: $id, client_name: $name, client_uri: $uri, redirect_uris: [$redirect],
			token_endpoint_auth_method: "none", substrate_authority: $authority
		}'
	else
		jq -n --arg id "$client" --arg name "$(app_name "$app")" --arg uri "$page" '{
			client_id: $id, client_name: $name, client_uri: $uri,
			redirect_uris: ["http://127.0.0.1/oauth/callback"], token_endpoint_auth_method: "none"
		}'
	fi
}

# Publishes one release's client document: the whole site when there is
# one, since the association file names every app, else the one document.
publish_client() {
	if [ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ]; then
		publish_site
	else
		upload "$1" "$2" "no-cache"
	fi
}

# The packages an app's authority document entry lists, by the folder name
# each app is published under: Workouts' two published packages, built from
# apps/apple/SubstrateWorkouts/Package/; the rest bring none of their own.
app_packages() {
	case "$1" in
	workouts) echo '["training","trainingcoach"]' ;;
	*) echo '[]' ;;
	esac
}

# The authority document the site serves now, on stdout: the body of a 200,
# `{}` for a key that is not there (a first publish), and a refusal for
# anything else, since a document that cannot be read could list versions
# this publish would then drop. A bucket that forbids listing answers an
# anonymous read of a missing key with 403, not 404, so a 403 is settled
# with the release's key: a signed HEAD of the key at `target` answers 404
# when it is absent and 200 when it is there, in which case the document is
# read signed. SUBSTRATE_APPLE_SITE_PUBLISHED names a file to read instead
# (empty: a first publish), for trying the merge by hand.
published_document() {
	local site="$1" authority="$2" target="$3" body status
	if [ -n "${SUBSTRATE_APPLE_SITE_PUBLISHED+x}" ]; then
		if [ -z "$SUBSTRATE_APPLE_SITE_PUBLISHED" ]; then
			echo '{}'
			return
		fi
		body="$(cat "$SUBSTRATE_APPLE_SITE_PUBLISHED")" || return 1
	else
		local file
		file="$(mktemp)"
		status="$(curl -sS -o "$file" -w '%{http_code}' --max-time 20 "$site/.well-known/substrate/authority.json")" || {
			rm -f "$file"
			echo "apple: reading $site/.well-known/substrate/authority.json failed" >&2
			return 1
		}
		body="$(cat "$file")"
		rm -f "$file"
		case "$status" in
		200) ;;
		404)
			echo '{}'
			return
			;;
		403)
			if [ -z "${AWS_ACCESS_KEY_ID:-}" ] || [ -z "${AWS_SECRET_ACCESS_KEY:-}" ]; then
				echo "apple: reading $site/.well-known/substrate/authority.json anonymously answered 403, which a bucket also answers for a key that is not there; set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY to settle it with a signed read, or SUBSTRATE_APPLE_SITE_PUBLISHED to try the merge against a saved document" >&2
				return 1
			fi
			local signed
			signed="$(s3 -I -o /dev/null -w '%{http_code}' --max-time 20 "$target/.well-known/substrate/authority.json")" || signed="unreachable"
			case "$signed" in
			404)
				echo '{}'
				return
				;;
			200)
				body="$(s3 -f --max-time 20 "$target/.well-known/substrate/authority.json")" || return 1
				;;
			*)
				echo "apple: reading $site/.well-known/substrate/authority.json anonymously answered 403 and the signed read of $target/.well-known/substrate/authority.json answered $signed" >&2
				return 1
				;;
			esac
			;;
		*)
			echo "apple: reading $site/.well-known/substrate/authority.json answered $status" >&2
			return 1
			;;
		esac
	fi
	if ! jq -e 'type == "object"' <<<"$body" >/dev/null 2>&1; then
		echo "apple: the published authority document is not a JSON object" >&2
		return 1
	fi
	local named
	named="$(jq -r '.authority // empty' <<<"$body")"
	if [ -n "$named" ] && [ "$named" != "$authority" ]; then
		echo "apple: the published authority document is $named's, not $authority's" >&2
		return 1
	fi
	echo "$body"
}

# Every closure this tree publishes for the first time (a package and
# version the live document does not list under this digest), checked as the
# server admits it before anything is uploaded, since a published version
# never changes: substratectl, built from this tree, parses the closure the
# way the server's catalog does (`publish --check`), refuses a version the
# document lists under another digest, one below the newest listed, and a
# narrowing of the newest published closure unless
# SUBSTRATE_APPLE_SITE_ALLOW_NARROWING=1 passes `--allow-narrowing`. The
# document goes to it as a file, with each package's newest published
# closure fetched beside it under the name the file lists, because a
# file-backed document's closures are read from the document's directory
# and never fetched. Runs in a dry run too: it uploads nothing.
check_publications() {
	local out="$1" published="$2" built="$3" authority="$4" site="$5"
	local new
	new="$(jq -r --argjson published "$published" '.[] as $b |
		select([($published.packages // [])[] | select(.package == $b.package) | (.versions // [])[] |
			select(.version == $b.version and (.sha256 | ascii_downcase) == ($b.sha256 | ascii_downcase))] | length == 0) |
		"\($b.package) \($b.version)"' <<<"$built")"
	[ -n "$new" ] || return 0
	local tmp
	tmp="$(mktemp -d)"
	echo "apple: building substratectl from this tree to check the closures it publishes"
	(cd ../.. && go build -o "$tmp/substratectl" ./cmd/substratectl) || {
		rm -rf "$tmp"
		return 1
	}
	local doc="$tmp/authority.json"
	jq --arg authority "$authority" '{authority: $authority, name: (.name // "Substrate"),
		packages: [(.packages // [])[] | . as $p | . + {versions: [($p.versions // [])[] | . + {closure: "\($p.package)-\(.version).yaml"}]}]}' \
		<<<"$published" >"$doc"
	local pkg version newest nv nurl args
	while read -r pkg version; do
		newest="$(jq -r --arg pkg "$pkg" '[(.packages // [])[] | select(.package == $pkg) | (.versions // [])[]] | sort_by(-.version) | first // empty |
			"\(.version) \(.closure)"' <<<"$published")"
		if [ -n "$newest" ]; then
			read -r nv nurl <<<"$newest"
			curl -fsS --max-time 60 -o "$tmp/$pkg-$nv.yaml" "$nurl" || {
				echo "apple: fetching the published $authority/$pkg version $nv closure from $nurl failed; the candidate cannot be compared with it" >&2
				rm -rf "$tmp"
				return 1
			}
		fi
		args=(publish "$authority/$pkg" --check --file "$out/packages/$pkg-$version.yaml" --version "$version"
			--closure "$site/packages/$pkg-$version.yaml" --document "$doc")
		if [ "${SUBSTRATE_APPLE_SITE_ALLOW_NARROWING:-}" = 1 ]; then args+=(--allow-narrowing); fi
		if ! "$tmp/substratectl" "${args[@]}" >/dev/null; then
			echo "apple: $authority/$pkg version $version is not a closure the server would admit, so nothing is published; fix it, or ship it as the next version" >&2
			rm -rf "$tmp"
			return 1
		fi
		echo "apple: checked $authority/$pkg version $version as the server admits it"
	done <<<"$new"
	rm -rf "$tmp"
}

# The site under SUBSTRATE_APPLE_SITE_URL, an https origin on a host of its
# own, served at its root: Apple's association file naming every app id
# for `webcredentials` and for `applinks` on its own callback path, the
# authority document listing every app by client id with the packages it
# asks for, and every published package version with its closure and
# digest, each app's client document, and the closures themselves under
# packages/<package>-<version>.yaml (decision record 0212). The closures
# this tree ships are built by apps/apple/scripts/workouts-package.py; every
# version the live document already lists is carried forward as it is, and
# a listed version whose closure this tree builds with another digest is
# refused, because a published version never changes. A closure published
# for the first time is checked as the server admits it before anything goes
# up (check_publications). The closures go up
# first and the document last, so the document never names a closure that is
# not there yet, and closures.lock.json beside the package sources is
# written last of all with what is published, for the generator's offline
# check. SUBSTRATE_APPLE_SITE_UPLOAD_URL is where the files are PUT
# when that is not the site itself (a bucket behind the host), signed with
# the same key as a release. The authority is SUBSTRATE_APPLE_SITE_AUTHORITY,
# else the site's host; the team id that signs the apps is
# SUBSTRATE_APPLE_TEAM_ID. With SUBSTRATE_APPLE_SITE_DRY_RUN=1 everything is
# built under build/site and what would be uploaded is printed, and nothing
# is written anywhere but there. The release builds stay in their own
# folder.
publish_site() {
	: "${SUBSTRATE_APPLE_SITE_URL:?set SUBSTRATE_APPLE_SITE_URL to the https origin of the site}"
	: "${SUBSTRATE_APPLE_TEAM_ID:?set SUBSTRATE_APPLE_TEAM_ID to the team id that signs the apps}"
	local dry="${SUBSTRATE_APPLE_SITE_DRY_RUN:-}"
	if [ -z "$dry" ]; then
		: "${AWS_ACCESS_KEY_ID:?set AWS_ACCESS_KEY_ID to a key that may write the site}"
		: "${AWS_SECRET_ACCESS_KEY:?set AWS_SECRET_ACCESS_KEY}"
	fi
	local site host authority target out app id
	site="$(site_url)" || return 1
	host="${site#https://}"
	authority="${SUBSTRATE_APPLE_SITE_AUTHORITY:-$host}"
	target="${SUBSTRATE_APPLE_SITE_UPLOAD_URL:-$site}"
	target="${target%/}"
	out="build/site"
	rm -rf "$out"
	mkdir -p "$out/.well-known/substrate" "$out/packages"

	# The closures this tree ships, as `path sha256` lines, then as JSON.
	local built lines
	lines="$(uv run --quiet --with pyyaml scripts/workouts-package.py --closures "$out/packages")" || return 1
	built="$(jq -Rn --arg site "$site" --arg authority "$authority" '[inputs | select(length > 0) | split(" ") |
		(.[0] | split("/") | last) as $file |
		($file | sub("\\.yaml$"; "") | capture("^(?<name>.+)-(?<version>[0-9]+)$")) as $m |
		{package: $m.name, version: ($m.version | tonumber), closure: "\($site)/packages/\($file)", sha256: .[1]}]' <<<"$lines")"

	# What the site serves today: every version it lists stays listed.
	local published
	published="$(published_document "$site" "$authority" "$target")" || return 1
	local conflicts
	conflicts="$(jq -r --argjson built "$built" '[.packages // [] | .[] as $lp | ($lp.versions // [])[] as $lv |
		$built[] | select(.package == $lp.package and .version == $lv.version and .sha256 != $lv.sha256) |
		"\(.package) \(.version) \($lv.sha256) \(.sha256)"] | .[]' <<<"$published")"
	if [ -n "$conflicts" ]; then
		while read -r pkg version live mine; do
			echo "apple: $authority/$pkg version $version is published with digest $live and this tree builds $mine; a published version never changes, so ship the change as the next version" >&2
		done <<<"$conflicts"
		return 1
	fi
	local stale
	stale="$(jq -r --argjson built "$built" '[.packages // [] | .[] as $lp | ([$lp.versions[]?.version] | max) as $newest |
		$built[] | select(.package == $lp.package and .version < $newest and ([$lp.versions[]?.version] | index(.version) | not)) |
		"\(.package) \(.version) \($newest)"] | .[]' <<<"$published")"
	if [ -n "$stale" ]; then
		while read -r pkg version newest; do
			echo "apple: $authority/$pkg version $version is below the published $newest and was never published; publish from a tree at or past it" >&2
		done <<<"$stale"
		return 1
	fi
	check_publications "$out" "$published" "$built" "$authority" "$site" || return 1
	# Every published entry carried forward whole (its `addonFor` and
	# whatever else the document says of it), its versions joined with this
	# tree's, newest first; a package the document does not list yet is
	# added.
	local packages
	packages="$(jq -c --argjson built "$built" '
		($built | group_by(.package) | map({key: .[0].package, value: map({version, closure, sha256})}) | from_entries) as $new
		| (.packages // []) as $old
		| ($old | map(.package)) as $names
		| ([$old[] | . + {versions: (((.versions // []) + ($new[.package] // [])) | group_by(.version) | map(first) | sort_by(-.version))}]
			+ [$new | to_entries[] | select(.key as $name | $names | index($name) | not) | {package: .key, versions: (.value | sort_by(-.version))}])
		| sort_by(.package)' <<<"$published")"

	local details="[]" ids="[]" apps="[]"
	for app in $APPS; do
		id="$SUBSTRATE_APPLE_TEAM_ID.$(app_bundle "$app")"
		mkdir -p "$out/apps/$app"
		client_document "$app" >"$out/apps/$app/client.json"
		callback_page "$app" >"$out/apps/$app/callback"
		details="$(jq -c --arg id "$id" --arg path "/apps/$app/callback" \
			'. + [{appIDs: [$id], components: [{"/": $path}]}]' <<<"$details")"
		ids="$(jq -c --arg id "$id" '. + [$id]' <<<"$ids")"
		apps="$(jq -c --arg client "$site/apps/$app/client.json" --arg name "$(app_name "$app")" --arg id "$id" \
			--argjson packages "$(app_packages "$app")" \
			'. + [{client_id: $client, name: $name, packages: $packages, apple: {appIDs: [$id]}}]' <<<"$apps")"
	done
	site_index "$authority" >"$out/index.html"
	jq -n --argjson details "$details" --argjson ids "$ids" \
		'{applinks: {details: $details}, webcredentials: {apps: $ids}}' >"$out/.well-known/apple-app-site-association"
	jq -n --arg authority "$authority" --argjson apps "$apps" --argjson packages "$packages" \
		'{authority: $authority, name: "Substrate", packages: $packages, apps: $apps}' >"$out/.well-known/substrate/authority.json"

	# The closures first, immutable; the documents the apps read next; the
	# authority document last.
	local file
	for file in "$out"/packages/*.yaml; do
		if [ -n "$(jq -r --arg file "${file##*/}" '.packages // [] | .[].versions[]? | select(.closure | endswith("/packages/" + $file)) | .sha256' <<<"$published")" ]; then
			continue
		fi
		site_upload "$dry" "$file" "$target/packages/${file##*/}" "public, max-age=31536000, immutable"
	done
	site_upload "$dry" "$out/.well-known/apple-app-site-association" "$target/.well-known/apple-app-site-association" "no-cache"
	for app in $APPS; do
		site_upload "$dry" "$out/apps/$app/client.json" "$target/apps/$app/client.json" "no-cache"
		site_upload "$dry" "$out/apps/$app/callback" "$target/apps/$app/callback" "no-cache"
	done
	site_upload "$dry" "$out/index.html" "$target/index.html" "no-cache"
	site_upload "$dry" "$out/.well-known/substrate/authority.json" "$target/.well-known/substrate/authority.json" "no-cache"
	# What is published now, for the generator's offline check: each
	# package's newest version and its digest. Committed after a publish.
	local lock="SubstrateWorkouts/Package/closures.lock.json"
	local locked
	locked="$(jq --arg authority "$authority" '[.packages[] | {key: "\($authority)/\(.package)", value: (.versions[0] | {version, sha256})}] | from_entries' \
		"$out/.well-known/substrate/authority.json")"
	if [ -n "$dry" ]; then
		echo "apple: dry run: nothing was uploaded; the site is built under apps/apple/$out"
		echo "apple: would write apps/apple/$lock:"
		local line
		while IFS= read -r line; do echo "apple:   $line"; done <<<"$locked"
	else
		echo "apple: published $site/.well-known/apple-app-site-association"
		echo "apple: published $site/.well-known/substrate/authority.json"
		printf '%s\n' "$locked" >"$lock"
		echo "apple: wrote apps/apple/$lock; commit it"
	fi
	jq -r '.packages[] | "apple: \(.package): versions \([.versions[].version] | map(tostring) | join(", "))"' "$out/.well-known/substrate/authority.json"
	echo "apple: the authority's DNS needs this record, once:"
	echo "apple:   _substrate.$authority. TXT \"v=substrate1 doc=$site/.well-known/substrate/authority.json\""
}

# The page at an app's claimed callback, for the browser that opens it
# without handing it to the app: macOS's fallback when the default browser
# does not match the callback, or a device the association has not reached
# yet. It reads nothing from the address and keeps nothing.
callback_page() {
	local name
	name="$(app_name "$1")"
	cat <<HTML
<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="referrer" content="no-referrer">
<title>Return to $name</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:36rem;margin:4rem auto;padding:0 1rem;color:#222}</style>
<h1>Return to $name</h1>
<p>Your server approved $name. If $name did not open by itself, close this page,
open $name and sign in again: it asks for a fresh approval, and this one expires
unused.</p>
</html>
HTML
}

# The site's own front page.
site_index() {
	cat <<HTML
<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>$1</title>
<style>body{font:16px/1.5 system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem;color:#222}code{font-size:.9em}</style>
<h1>$1</h1>
<p>The authority the Substrate apps and packages publish under. Servers read
<a href="/.well-known/substrate/authority.json"><code>/.well-known/substrate/authority.json</code></a>,
which the <code>_substrate.$1</code> TXT record names.</p>
<p>Source: <a href="https://github.com/geoah/substrate">github.com/geoah/substrate</a>.</p>
</html>
HTML
}

# One site file: uploaded, or in a dry run named with where it would go.
site_upload() {
	local dry="$1" file="$2" url="$3" cache="$4"
	if [ -n "$dry" ]; then
		echo "apple: would upload $file -> $url ($cache)"
	else
		upload "$file" "$url" "$cache"
	fi
}

release() {
	local channel="${1:-prerelease}" short build commit version client
	stamp "$channel"
	client="$(client_url console)" || return 1
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
	# The console app signs in for the page with full access (decision
	# 0204), approved on the server's own page that names this host.
	client_document console >"$out/client.json"
	site_settings console
	local settings=(
		MARKETING_VERSION="$short" CURRENT_PROJECT_VERSION="$build"
		SUBSTRATE_VERSION="$version" SUBSTRATE_CHANNEL="$channel" SUBSTRATE_COMMIT="$commit"
		SUBSTRATE_UPDATES_URL="$base/" SUBSTRATE_SPARKLE_KEY="$key" SUBSTRATE_CONSOLE_CLIENT_ID="$client"
		${SITE[@]+"${SITE[@]}"}
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

	publish_client "$out/client.json" "$client"
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
	client_document sync >"$out/client.json"
	site_settings sync
	xcodebuild -project "$PROJECT" -scheme SubstrateSync -derivedDataPath "$DERIVED" \
		-allowProvisioningUpdates ${AUTH[@]+"${AUTH[@]}"} -quiet -destination 'generic/platform=iOS' -configuration Release archive \
		-archivePath "$out/iOS.xcarchive" \
		MARKETING_VERSION="$short" CURRENT_PROJECT_VERSION="$build" \
		SUBSTRATE_VERSION="$version" SUBSTRATE_CHANNEL="$channel" SUBSTRATE_COMMIT="$commit" \
		SUBSTRATE_UPDATES_URL="$base/" SUBSTRATE_SYNC_CLIENT_ID="$client" ${SITE[@]+"${SITE[@]}"}
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

	publish_client "$out/client.json" "$client"
	publish "$base" "$out" "$stage" "$rel"
	echo "apple: released Substrate Sync $version ($channel)"
	echo "apple: $base/index.html"
}

# Substrate Workouts: the iPhone app and its Live Activity extension,
# archived, exported and published as Substrate Sync is, to its own folder,
# `workouts/`, with a client ID metadata document of its own.
build_workouts() {
	xcodebuild -project "$PROJECT" -scheme SubstrateWorkouts -derivedDataPath "$DERIVED" \
		-allowProvisioningUpdates ${AUTH[@]+"${AUTH[@]}"} -quiet -destination 'generic/platform=iOS Simulator' -configuration Debug build
	echo "apple: $DERIVED/Build/Products/Debug-iphonesimulator/SubstrateWorkouts.app"
}

release_workouts() {
	local channel="${1:-prerelease}" short build commit version client
	stamp "$channel"
	client="$(client_url workouts)" || return 1
	local base="${SUBSTRATE_APPLE_RELEASE_URL%/}/workouts"
	local out="build/release-workouts/$build" rel="releases/$build"
	local stage="$out/stage"
	rm -rf "$out"
	mkdir -p "$stage/$rel"
	client_document workouts >"$out/client.json"
	site_settings workouts
	xcodebuild -project "$PROJECT" -scheme SubstrateWorkouts -derivedDataPath "$DERIVED" \
		-allowProvisioningUpdates ${AUTH[@]+"${AUTH[@]}"} -quiet -destination 'generic/platform=iOS' -configuration Release archive \
		-archivePath "$out/iOS.xcarchive" \
		MARKETING_VERSION="$short" CURRENT_PROJECT_VERSION="$build" \
		SUBSTRATE_VERSION="$version" SUBSTRATE_CHANNEL="$channel" SUBSTRATE_COMMIT="$commit" \
		SUBSTRATE_UPDATES_URL="$base/" SUBSTRATE_WORKOUTS_CLIENT_ID="$client" ${SITE[@]+"${SITE[@]}"}
	export_archive iOS "${SUBSTRATE_APPLE_EXPORT_IOS:-debugging}" "$out"

	local ipa="$rel/SubstrateWorkouts-${version%%+*}-iOS.ipa"
	local exported=("$out/iOS"/*.ipa)
	cp "${exported[0]}" "$stage/$ipa"
	local icon="SubstrateWorkouts/Assets.xcassets/AppIcon.appiconset/icon-1024.png"
	sips -z 57 57 "$icon" --out "$stage/$rel/icon-57.png" >/dev/null
	sips -z 512 512 "$icon" --out "$stage/$rel/icon-512.png" >/dev/null

	jq -n --arg version "$version" --arg short "$short" --arg build "$build" \
		--arg channel "$channel" --arg commit "$commit" \
		--arg date "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
		--arg ipa "$ipa" --argjson ipaLength "$(stat -f %z "$stage/$ipa")" \
		--arg ipaSum "$(shasum -a 256 "$stage/$ipa" | cut -d' ' -f1)" \
		--arg rel "$rel" '{
			name: "Substrate Workouts", version: $version, shortVersion: $short, build: $build,
			channel: $channel, commit: $commit, date: $date, bundleIdentifier: "dev.reamde.substrate.workouts",
			ios: {path: $ipa, length: $ipaLength, sha256: $ipaSum, minimumSystemVersion: "27.0",
				manifest: "\($rel)/manifest.plist", icon: "\($rel)/icon-512.png",
				smallIcon: "\($rel)/icon-57.png"}
		}' >"$stage/$rel/release.json"

	publish_client "$out/client.json" "$client"
	publish "$base" "$out" "$stage" "$rel"
	echo "apple: released Substrate Workouts $version ($channel)"
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
	# approval page names the host that serves it.
	client_document chat >"$out/client.json"
	site_settings chat
	local settings=(
		MARKETING_VERSION="$short" CURRENT_PROJECT_VERSION="$build"
		SUBSTRATE_VERSION="$version" SUBSTRATE_CHANNEL="$channel" SUBSTRATE_COMMIT="$commit"
		SUBSTRATE_UPDATES_URL="$base/" SUBSTRATE_SPARKLE_KEY="$key" SUBSTRATE_CHAT_CLIENT_ID="$client"
		${SITE[@]+"${SITE[@]}"}
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

	publish_client "$out/client.json" "$client"
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

# curl, signed for S3 with the key in the environment, which reaches curl on
# stdin rather than on a command line every process may read.
s3() {
	printf 'user = "%s:%s"\n' "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY" |
		curl -sS -K - --aws-sigv4 "aws:amz:${SUBSTRATE_APPLE_RELEASE_REGION:-us-east-1}:s3" "$@"
}

upload() {
	local file="$1" url="$2" cache="$3" type
	case "$file" in
	*.html | */callback) type="text/html; charset=utf-8" ;;
	*.json | */apple-app-site-association) type="application/json" ;;
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
build:workouts)
	project
	build_workouts
	;;
test:workouts)
	# WorkoutsKit is a Swift package too: the model, both engines and the
	# sync, on the Mac. The package's manifests and their JSON must agree,
	# and so must the exercise library and its sources.
	uv run --quiet --with pyyaml scripts/workouts-package.py --check
	uv run --quiet --with pyyaml scripts/exercise-library.py --check
	swift test --package-path WorkoutsKit
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
release:workouts)
	project
	release_workouts "${2:-}"
	;;
site)
	# The site alone: the association file, the authority document with
	# every published package version, the closures and every client
	# document, with no build.
	publish_site
	;;
*)
	echo "usage: $0 project|build|build:macos|build:sim|build:device|build:sync|build:chat|build:workouts|sim:chat|test:chat|test:kit|test:workouts|install|sim|release|release:sync|release:chat|release:workouts|site [prerelease|stable]" >&2
	exit 2
	;;
esac
