#!/usr/bin/env bash
#
# The apps' release and their site: `mise run apps:release [channel] [app…]`
# and `mise run apps:site` are the two subcommands here
# (docs/plans/app-kit.md §9). Every app is named once by its apps/<app>/
# app.json, and nothing below knows an app by name. The platforms' builds
# are .mise/apple.sh and .mise/apps-android.sh, sourced; this file orders
# them:
#
#   1. one publisher at a time: a lease, taken with a conditional write
#      (lease_take), and the published state read after it;
#   2. every selected app built and checked on every platform it has a UI
#      for, from one commit under one version (build_all); nothing goes up
#      before all of them passed;
#   3. the artifacts staged under each feed's releases/<build>/, immutable
#      (stage_upload);
#   4. the site (site_build, site_publish): new closures only after the
#      server's own check admits them, closures before the authority
#      document and the document last;
#   5. each app activated last and alone (activate): its release.json, then
#      the feeds installed apps read today, at the addresses they read.
#
# A rerun of a staged build (SUBSTRATE_APPS_RELEASE_BUILD=<build>) builds
# nothing: it checks the staged files against their digests and carries on
# where the last run stopped, reading what is published before each write.
#
# SUBSTRATE_APPS_RELEASE_DRY_RUN=1 runs every step against build/apps: the
# release folder, the site and the lease are directories under
# build/apps/dry-run, which persist between dry runs so a rerun and a
# conflict can be tried, and nothing is written anywhere else, the closures'
# lock files included. The one function that writes to a bucket (upload) and
# the one that signs a request (s3) refuse to run in a dry run.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APPS_DIR="$ROOT/apps"
WORK="$ROOT/build/apps"
DRY="${SUBSTRATE_APPS_RELEASE_DRY_RUN:-}"
if [ -n "$DRY" ]; then
	# A dry run carries no credential, so no path through it can use one.
	unset AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY SUBSTRATE_APPLE_ASC_KEY_PATH
	# The simulated bucket's lock, left by a dry run that was killed inside
	# a write.
	rmdir "$WORK/dry-run/.mutex" 2>/dev/null || true
fi

# shellcheck source=.mise/apple.sh
source "$ROOT/.mise/apple.sh"
# shellcheck source=.mise/apps-android.sh
source "$ROOT/.mise/apps-android.sh"

say() { echo "apps: $*"; }
die() {
	echo "apps: $*" >&2
	exit 1
}

# --- the apps, from their app.json -----------------------------------------

app_json() { echo "$APPS_DIR/$1/app.json"; }
app_get() { jq -r "$2" "$(app_json "$1")"; }

# Every app folder, by id.
all_apps() {
	local file
	for file in "$APPS_DIR"/*/app.json; do
		basename "$(dirname "$file")"
	done
}

# The UI folder a platform is drawn in.
ui_folder() {
	case "$1" in
	ios | macos) echo apple ;;
	android) echo android ;;
	esac
}

# The platforms an app is released on: the ones its app.json names whose UI
# folder exists. An app's package can come before its UI (Substrate Tasks'
# did), and a platform it names but has not drawn yet is not one it ships.
app_platforms() {
	local app="$1" platform
	for platform in $(app_get "$app" '.platforms[]'); do
		[ -d "$APPS_DIR/$app/$(ui_folder "$platform")" ] && echo "$platform"
	done
	return 0
}

app_has() {
	case " $(app_platforms "$1" | tr '\n' ' ')" in
	*" $2 "*) return 0 ;;
	esac
	return 1
}
app_has_apple() { app_has "$1" ios || app_has "$1" macos; }

# The apps with a UI on any platform: the default release, and the apps the
# site speaks for. The one at the release folder's root (the console's, the
# first feed) first, then by id, so the site's documents keep their order.
ui_apps() {
	local app
	for app in $(all_apps); do
		[ -n "$(app_platforms "$app")" ] || continue
		printf '%s %s\n' "$([ -z "$(app_get "$app" .feed)" ] && echo 0 || echo 1)" "$app"
	done | sort | cut -d' ' -f2
}

# The release folder's key prefix of an app's feed: empty at the root.
feed_prefix() {
	local feed
	feed="$(app_get "$1" .feed)"
	[ -z "$feed" ] || echo "$feed/"
}

feed_url() {
	local feed
	feed="$(app_get "$1" .feed)"
	echo "${SUBSTRATE_APPLE_RELEASE_URL%/}${feed:+/$feed}"
}

# The build-setting and environment spelling of an app's id.
app_upper() { printf '%s' "$1" | tr '[:lower:]' '[:upper:]'; }

# --- the version -----------------------------------------------------------
#
# The version is Apple's three integers plus a build number that only rises
# (the UTC time, YYYYMMDD.HHMMSS); a pre-release is the next patch after the
# last released version, so it sorts after that one and before the next:
# 0.113.1-pre.20261021.153012+ac85863e. A stable release is the released
# version itself and needs a clean tree. Every app and platform of one
# release carries the one version.
#
# Sets the caller's `short`, `build`, `commit`, `commit_full` and `version`.
stamp() {
	local channel="$1"
	local dirty=""
	if [ -n "$(git -C "$ROOT" status --porcelain)" ]; then
		if [ "$channel" = stable ]; then
			die "a stable release is built from a clean tree"
		fi
		dirty=".dirty"
	fi
	local released major minor patch
	released="$(jq -r '."."' "$ROOT/.release-please-manifest.json")"
	IFS=. read -r major minor patch <<<"$released"
	build="$(date -u +%Y%m%d.%H%M%S)"
	commit_full="$(git -C "$ROOT" rev-parse HEAD)"
	commit="${commit_full:0:8}$dirty"
	if [ "$channel" = stable ]; then
		short="$released"
		version="$released"
	else
		short="$major.$minor.$((patch + 1))"
		version="$short-pre.$build+$commit"
	fi
}

# --- where things go -------------------------------------------------------

# The URL of an app's client ID metadata document, for the app named in $1.
# Its host heads the owner's approval page and names the app's actor
# (`app:<host>`), so it is a host the publisher alone serves: the site's
# `/apps/<app>/client.json` when SUBSTRATE_APPLE_SITE_URL is set (see
# site_build), else SUBSTRATE_APPLE_CLIENT_URL, an https folder there the
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
	local base
	base="$(client_base)" || return 1
	echo "$base/$app/client.json"
}

# The folder the client documents go under when there is no site.
client_base() {
	local base="${SUBSTRATE_APPLE_CLIENT_URL:-$SUBSTRATE_APPLE_RELEASE_URL}"
	base="${base%/}"
	case "$base" in
	https://*) ;;
	*)
		echo "apps: the client documents' folder is https: $base" >&2
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
		echo "apps: $host is a shared endpoint, where anyone may publish a client document the approval page would head with the same name; set SUBSTRATE_APPLE_CLIENT_URL to a folder on a host of your own" >&2
		return 1
	fi
	echo "$base"
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

# SUBSTRATE_APPLE_SITE_URL as the origin it must be: https, a host of its
# own and nothing after it, because Apple reads the association file from
# `https://<host>/.well-known/apple-app-site-association` and Android its
# from `https://<host>/.well-known/assetlinks.json`, and from nowhere else.
# A shared endpoint is refused as client_url refuses it.
site_url() {
	local site="${SUBSTRATE_APPLE_SITE_URL%/}"
	case "$site" in
	https://*) ;;
	*)
		echo "apps: SUBSTRATE_APPLE_SITE_URL is an https origin: $site" >&2
		return 1
		;;
	esac
	local host="${site#https://}"
	case "$host" in
	"" | */* | *:* | *@*)
		echo "apps: SUBSTRATE_APPLE_SITE_URL is an origin served at its root, with no path, port or user, because Apple and Android read the association files from the host's root: $site" >&2
		return 1
		;;
	esac
	host="$(printf '%s' "$host" | tr '[:upper:]' '[:lower:]')"
	host="${host%.}"
	if shared_host "$host"; then
		echo "apps: $host is a shared endpoint, where anyone may publish documents under the same host; set SUBSTRATE_APPLE_SITE_URL to a host of your own" >&2
		return 1
	fi
	echo "https://$host"
}

# The https callback each platform of an app claims on the site: Apple's
# through the association file, Android's through assetlinks.json and the
# app's verified link. Apart, so the approval page's redirect says which
# build is signing in, and Apple's keeps the address installed apps use.
apple_callback() { echo "$(site_url)/apps/$1/callback"; }
android_callback() { echo "$(site_url)/apps/$1/android/callback"; }

# The page an app's client document links to: its feed's install page, else
# the site's root.
release_page() {
	if [ -z "${SUBSTRATE_APPLE_RELEASE_URL:-}" ]; then
		site_url | sed 's|$|/|'
	else
		echo "$(feed_url "$1")/index.html"
	fi
}

# The client ID metadata document of the app named in $1, on stdout. With a
# site, its redirects are the https callbacks each platform it ships claims
# there and it names the authority that speaks for it
# (`substrate_authority`); without one, a loopback redirect (RFC 8252),
# which any program on the device may also use, so the approval page cannot
# tell the app from another.
client_document() {
	local app="$1" client page name
	client="$(client_url "$app")" || return 1
	page="$(release_page "$app")" || return 1
	name="$(app_get "$app" .name)"
	if [ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ]; then
		local site redirects="[]"
		site="$(site_url)" || return 1
		if app_has_apple "$app"; then
			redirects="$(jq -c --arg uri "$(apple_callback "$app")" '. + [$uri]' <<<"$redirects")"
		fi
		if app_has "$app" android; then
			redirects="$(jq -c --arg uri "$(android_callback "$app")" '. + [$uri]' <<<"$redirects")"
		fi
		jq -n --arg id "$client" --arg name "$name" --arg uri "$page" --argjson redirects "$redirects" \
			--arg authority "${SUBSTRATE_APPLE_SITE_AUTHORITY:-${site#https://}}" '{
			client_id: $id, client_name: $name, client_uri: $uri, redirect_uris: $redirects,
			token_endpoint_auth_method: "none", substrate_authority: $authority
		}'
	else
		jq -n --arg id "$client" --arg name "$name" --arg uri "$page" '{
			client_id: $id, client_name: $name, client_uri: $uri,
			redirect_uris: ["http://127.0.0.1/oauth/callback"], token_endpoint_auth_method: "none"
		}'
	fi
}

# --- the stores ------------------------------------------------------------
#
# Three places are written: the release folder (SUBSTRATE_APPLE_RELEASE_URL,
# every feed and the lease), the site (SUBSTRATE_APPLE_SITE_UPLOAD_URL, else
# the site itself) and, with no site, the client documents' folder. Each is
# a store, read and written by key; in a dry run each is a directory under
# build/apps/dry-run that keeps an S3 bucket's conditional semantics, so the
# lease and the immutable keys behave there as they do on the bucket.
# Every function sets STATUS (the HTTP status, or the simulated one) and
# ETAG, globals rather than output, so a refusal is never lost in a
# subshell.
STATUS=""
ETAG=""

store_base() {
	case "$1" in
	release) echo "${SUBSTRATE_APPLE_RELEASE_URL%/}" ;;
	site)
		local target="${SUBSTRATE_APPLE_SITE_UPLOAD_URL:-$(site_url)}"
		echo "${target%/}"
		;;
	client) client_base ;;
	esac
}

dry_path() { echo "$WORK/dry-run/$1/$2"; }

dry_etag() { printf '"%s"' "$(shasum -a 256 "$1" | cut -c1-32)"; }

# The simulated bucket's one writer at a time, so a conditional write and
# the renewer's are each atomic, as the bucket's are.
dry_lock() {
	until mkdir "$WORK/dry-run/.mutex" 2>/dev/null; do sleep 0.05; done
}
dry_unlock() { rmdir "$WORK/dry-run/.mutex"; }

# store_get <store> <key> <out>: the body to <out>; STATUS 200, 404 or what
# the bucket answered.
store_get() {
	local store="$1" key="$2" out="$3"
	ETAG=""
	if [ -n "$DRY" ]; then
		local path
		path="$(dry_path "$store" "$key")"
		if [ -f "$path" ]; then
			cp "$path" "$out"
			ETAG="$(dry_etag "$path")"
			STATUS=200
		else
			: >"$out"
			STATUS=404
		fi
		return 0
	fi
	local headers
	headers="$(mktemp)"
	STATUS="$(s3 -D "$headers" -o "$out" -w '%{http_code}' "$(store_base "$store")/$key")" || STATUS=000
	ETAG="$(header_value ETag <"$headers")"
	rm -f "$headers"
	# A bucket that forbids listing answers a missing key with 403 to an
	# anonymous read, never to a signed one, so a signed 403 is a refusal.
	[ "$STATUS" != 404 ] || : >"$out"
	return 0
}

header_value() {
	tr -d '\r' | sed -n "s/^$1: *//Ip" | tail -1
}

# store_sha <store> <key>: the sha256 of what the key holds, in SHA: the
# digest this tool stores beside each file it writes, else the body's.
store_sha() {
	local store="$1" key="$2"
	SHA=""
	if [ -n "$DRY" ]; then
		SHA="$(shasum -a 256 "$(dry_path "$store" "$key")" | cut -d' ' -f1)"
		return 0
	fi
	local headers
	headers="$(mktemp)"
	s3 -I -o /dev/null -D "$headers" "$(store_base "$store")/$key" || true
	SHA="$(header_value x-amz-meta-sha256 <"$headers")"
	rm -f "$headers"
	if [ -z "$SHA" ]; then
		local body
		body="$(mktemp)"
		s3 -f -o "$body" "$(store_base "$store")/$key" || {
			rm -f "$body"
			die "reading $(store_base "$store")/$key failed"
		}
		SHA="$(shasum -a 256 "$body" | cut -d' ' -f1)"
		rm -f "$body"
	fi
}

# store_write <store> <key> <file> <cache> [create | match:<etag>]: one
# write, conditional when asked: `create` only where the key is absent
# (If-None-Match: *), `match:` only where it still holds that ETag
# (If-Match). STATUS 200, or 412 where the condition failed. The lease's
# own writes come straight here; everything else goes through store_put.
store_write() {
	local store="$1" key="$2" file="$3" cache="$4" condition="${5:-}"
	if [ -n "$DRY" ]; then
		local path held
		path="$(dry_path "$store" "$key")"
		mkdir -p "$(dirname "$path")"
		dry_lock
		held=""
		[ -f "$path" ] && held="$(dry_etag "$path")"
		case "$condition" in
		create) [ -z "$held" ] && STATUS=200 || STATUS=412 ;;
		match:*) [ -n "$held" ] && [ "$held" = "${condition#match:}" ] && STATUS=200 || STATUS=412 ;;
		*) STATUS=200 ;;
		esac
		if [ "$STATUS" = 200 ]; then
			cp "$file" "$path.tmp"
			mv "$path.tmp" "$path"
			ETAG="$(dry_etag "$path")"
		fi
		dry_unlock
		case "$key" in
		.apps-release-*) ;;
		*) [ "$STATUS" != 200 ] || say "dry run: wrote $key to build/apps/dry-run/$store ($cache)" ;;
		esac
		return 0
	fi
	upload "$file" "$(store_base "$store")/$key" "$cache" "$condition"
}

store_delete() {
	local store="$1" key="$2"
	if [ -n "$DRY" ]; then
		rm -f "$(dry_path "$store" "$key")"
		return 0
	fi
	s3 -o /dev/null -X DELETE "$(store_base "$store")/$key" || true
}

# store_put: a write of the release's own, made only while this publisher
# still holds the lease (lease_check), and refused when the bucket refuses.
store_put() {
	lease_check
	store_write "$@"
	case "$STATUS" in
	200) ;;
	412) die "$1/$2 changed under this release (a conditional write answered 412); nothing more is written, and a rerun reads it again" ;;
	*) die "writing $1/$2 answered $STATUS" ;;
	esac
}

# put_immutable <store> <key> <file> <cache>: a key that never changes once
# written (a release's files, a closure). Created where absent; where
# present it must already hold these bytes, which is how a rerun passes
# over what the last run wrote, and anything else is refused.
put_immutable() {
	local store="$1" key="$2" file="$3" cache="$4" sha
	sha="$(shasum -a 256 "$file" | cut -d' ' -f1)"
	lease_check
	store_write "$store" "$key" "$file" "$cache" create
	case "$STATUS" in
	200) ;;
	412)
		store_sha "$store" "$key"
		if [ "$SHA" != "$sha" ]; then
			die "$store/$key is already published with digest $SHA, and this release holds $sha; a published file never changes"
		fi
		say "already published: $key"
		;;
	*) die "writing $store/$key answered $STATUS" ;;
	esac
}

# curl, signed for S3 with the key in the environment, which reaches curl on
# stdin rather than on a command line every process may read.
s3() {
	if [ -n "$DRY" ]; then
		echo "apps: a dry run reaches no bucket" >&2
		return 70
	fi
	printf 'user = "%s:%s"\n' "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY" |
		curl -sS -K - --aws-sigv4 "aws:amz:${SUBSTRATE_APPLE_RELEASE_REGION:-us-east-1}:s3" "$@"
}

# The one function that writes to a bucket. It refuses a dry run outright,
# whatever called it, and stores the file's sha256 beside it, which is how
# an immutable key is compared without reading it back.
upload() {
	local file="$1" url="$2" cache="$3" condition="${4:-}" type sha headers
	if [ -n "$DRY" ]; then
		echo "apps: a dry run uploads nothing, and something asked to upload $url" >&2
		exit 70
	fi
	case "$file" in
	*.html | */callback) type="text/html; charset=utf-8" ;;
	*.json | */apple-app-site-association) type="application/json" ;;
	*.xml | *.plist) type="application/xml" ;;
	*.png) type="image/png" ;;
	*.zip) type="application/zip" ;;
	*.apk) type="application/vnd.android.package-archive" ;;
	*.yaml) type="application/yaml" ;;
	*) type="application/octet-stream" ;;
	esac
	sha="$(shasum -a 256 "$file" | cut -d' ' -f1)"
	local args=(-o /dev/null -w '%{http_code}' -H "Content-Type: $type" -H "Cache-Control: $cache"
		-H "x-amz-meta-sha256: $sha" -T "$file")
	case "$condition" in
	create) args+=(-H 'If-None-Match: *') ;;
	match:*) args+=(-H "If-Match: ${condition#match:}") ;;
	esac
	headers="$(mktemp)"
	STATUS="$(s3 -D "$headers" "${args[@]}" "$url")" || STATUS=000
	ETAG="$(header_value ETag <"$headers")"
	rm -f "$headers"
	# A conflicting concurrent write is 409 on some buckets.
	[ "$STATUS" != 409 ] || STATUS=412
}

# --- one publisher at a time ------------------------------------------------
#
# The lease is one object at the root of the release folder, created only
# where absent (If-None-Match: *) and changed only where it still holds what
# was read (If-Match), holding an expiry and a fencing token that every
# taking raises by one. A publisher re-reads the published state after
# taking it, renews it while it works and checks it before every write; one
# that finds another's token, or its own lease past expiry, stops. A lease
# past its expiry may be taken over, and its taker reads everything again,
# since it starts from nothing. A bucket that answers conditional writes as
# if they were plain ones is refused, never raced.
readonly LEASE_KEY=".apps-release-lease.json"
readonly LEASE_TTL="${SUBSTRATE_APPS_RELEASE_LEASE_TTL:-900}"
readonly LEASE_RENEW="${SUBSTRATE_APPS_RELEASE_LEASE_RENEW:-120}"
# The least a lease must have left for a write to go ahead under it.
readonly LEASE_MARGIN=60
LEASE_TOKEN=""
LEASE_HOLDER=""
LEASE_PID=""

lease_body() {
	jq -n --arg holder "$LEASE_HOLDER" --argjson token "$1" --argjson expires "$2" --arg what "$3" \
		'{holder: $holder, token: $token, expires: $expires, release: $what}'
}

# Refuses a release folder whose bucket does not write conditionally: a
# create where the key exists must answer 412, as must a write on an ETag
# the key does not hold.
conditional_probe() {
	local key=".apps-release-probe-$LEASE_HOLDER" file="$WORK/lease/probe" first second third
	printf 'probe\n' >"$file"
	store_write release "$key" "$file" no-cache create
	first="$STATUS"
	store_write release "$key" "$file" no-cache create
	second="$STATUS"
	store_write release "$key" "$file" no-cache 'match:"0000"'
	third="$STATUS"
	store_delete release "$key"
	if [ "$first" != 200 ] || [ "$second" != 412 ] || [ "$third" != 412 ]; then
		die "the release folder's bucket does not write conditionally: a create answered $first, a second create $second and a write on a stale ETag $third, where 200, 412 and 412 are what conditional writes answer. One publisher at a time needs them, so nothing is published"
	fi
}

lease_take() {
	local what="$1" now token expires held_what
	mkdir -p "$WORK/lease"
	LEASE_HOLDER="$(openssl rand -hex 12)"
	conditional_probe
	now="$(date +%s)"
	store_get release "$LEASE_KEY" "$WORK/lease/held.json"
	case "$STATUS" in
	404)
		token=1
		lease_body "$token" $((now + LEASE_TTL)) "$what" >"$WORK/lease/mine.json"
		store_write release "$LEASE_KEY" "$WORK/lease/mine.json" no-cache create
		;;
	200)
		token="$(jq -r '.token // 0' "$WORK/lease/held.json")"
		expires="$(jq -r '.expires // 0' "$WORK/lease/held.json")"
		held_what="$(jq -r '.release // ""' "$WORK/lease/held.json")"
		if [ "$expires" -gt "$now" ]; then
			die "another publisher holds the release lease (token $token, $held_what) until $(date -u -r "$expires" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "@$expires" +%Y-%m-%dT%H:%M:%SZ); wait for it, or for it to expire if that publisher died"
		fi
		token=$((token + 1))
		# A lease given up at the end of a run is written expired at 0.
		[ "$expires" = 0 ] || say "taking over the lease past its expiry (token $((token - 1)), $held_what)"
		lease_body "$token" $((now + LEASE_TTL)) "$what" >"$WORK/lease/mine.json"
		store_write release "$LEASE_KEY" "$WORK/lease/mine.json" no-cache "match:$ETAG"
		;;
	*) die "reading the release lease answered $STATUS" ;;
	esac
	[ "$STATUS" = 200 ] || die "another publisher took the release lease first (answered $STATUS)"
	LEASE_TOKEN="$token"
	say "holding the release lease, token $LEASE_TOKEN"
	trap lease_end EXIT
	trap 'exit 130' INT TERM
	lease_renewer &
	LEASE_PID=$!
}

lease_renewer() {
	local pause=""
	trap - EXIT
	trap '[ -z "$pause" ] || kill "$pause" 2>/dev/null; exit 0' TERM
	while :; do
		sleep "$LEASE_RENEW" &
		pause=$!
		wait "$pause"
		pause=""
		lease_renew || {
			echo "apps: the release lease could not be renewed; the next write stops" >&2
			exit 1
		}
	done
}

lease_renew() {
	local file="$WORK/lease/renew.json"
	store_get release "$LEASE_KEY" "$file"
	[ "$STATUS" = 200 ] || return 1
	[ "$(jq -r '.holder' "$file")" = "$LEASE_HOLDER" ] || return 1
	[ "$(jq -r '.token' "$file")" = "$LEASE_TOKEN" ] || return 1
	lease_body "$LEASE_TOKEN" $(($(date +%s) + LEASE_TTL)) "$(jq -r '.release' "$file")" >"$file.next"
	store_write release "$LEASE_KEY" "$file.next" no-cache "match:$ETAG"
	[ "$STATUS" = 200 ]
}

# The fencing check, before every write: the lease is still this
# publisher's, under its token, with time left.
lease_check() {
	[ -n "$LEASE_TOKEN" ] || die "writing without the release lease"
	local file="$WORK/lease/check.json"
	store_get release "$LEASE_KEY" "$file"
	if [ "$STATUS" != 200 ] || [ "$(jq -r '.holder' "$file")" != "$LEASE_HOLDER" ] ||
		[ "$(jq -r '.token' "$file")" != "$LEASE_TOKEN" ]; then
		die "the release lease is no longer this publisher's (token $LEASE_TOKEN); stopping before the next write, and a rerun reads everything again"
	fi
	if [ $(($(jq -r '.expires' "$file") - $(date +%s))) -lt "$LEASE_MARGIN" ]; then
		die "the release lease is about to expire unrenewed; stopping before the next write"
	fi
}

# Gives the lease up on any exit, by writing it expired under the token it
# was taken with, so the next publisher takes it over at once and raises
# the token. A lease another publisher holds by now is left alone.
lease_end() {
	local code=$?
	trap - EXIT
	if [ -n "$LEASE_PID" ]; then
		kill "$LEASE_PID" 2>/dev/null || true
		wait "$LEASE_PID" 2>/dev/null || true
	fi
	if [ -n "$LEASE_TOKEN" ]; then
		local file="$WORK/lease/end.json"
		store_get release "$LEASE_KEY" "$file"
		if [ "$STATUS" = 200 ] && [ "$(jq -r '.holder' "$file")" = "$LEASE_HOLDER" ] &&
			[ "$(jq -r '.token' "$file")" = "$LEASE_TOKEN" ]; then
			lease_body "$LEASE_TOKEN" 0 "$(jq -r '.release' "$file")" >"$file.next"
			store_write release "$LEASE_KEY" "$file.next" no-cache "match:$ETAG"
		fi
		LEASE_TOKEN=""
	fi
	exit "$code"
}

# --- the site --------------------------------------------------------------

# The authority document the site serves now, on stdout: the body of a 200,
# `{}` for a key that is not there (a first publish), and a refusal for
# anything else, since a document that cannot be read could list versions
# this publish would then drop. A bucket that forbids listing answers an
# anonymous read of a missing key with 403, not 404, so a 403 is settled
# with the release's key: a signed HEAD of the key at `target` answers 404
# when it is absent and 200 when it is there, in which case the document is
# read signed. SUBSTRATE_APPLE_SITE_PUBLISHED names a file to read instead
# (empty: a first publish), for trying the merge by hand; a dry run reads its
# own simulated site.
published_document() {
	local site="$1" authority="$2" target="$3" body status
	if [ -n "${SUBSTRATE_APPLE_SITE_PUBLISHED+x}" ]; then
		if [ -z "$SUBSTRATE_APPLE_SITE_PUBLISHED" ]; then
			echo '{}'
			return
		fi
		body="$(cat "$SUBSTRATE_APPLE_SITE_PUBLISHED")" || return 1
	elif [ -n "$DRY" ]; then
		local file
		file="$(mktemp)"
		store_get site .well-known/substrate/authority.json "$file"
		body="$(cat "$file")"
		rm -f "$file"
		[ "$STATUS" = 200 ] || body='{}'
	else
		local file
		file="$(mktemp)"
		status="$(curl -sS -o "$file" -w '%{http_code}' --max-time 20 "$site/.well-known/substrate/authority.json")" || {
			rm -f "$file"
			echo "apps: reading $site/.well-known/substrate/authority.json failed" >&2
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
				echo "apps: reading $site/.well-known/substrate/authority.json anonymously answered 403 and the signed read of $target/.well-known/substrate/authority.json answered $signed" >&2
				return 1
				;;
			esac
			;;
		*)
			echo "apps: reading $site/.well-known/substrate/authority.json answered $status" >&2
			return 1
			;;
		esac
	fi
	if ! jq -e 'type == "object"' <<<"$body" >/dev/null 2>&1; then
		echo "apps: the published authority document is not a JSON object" >&2
		return 1
	fi
	local named
	named="$(jq -r '.authority // empty' <<<"$body")"
	if [ -n "$named" ] && [ "$named" != "$authority" ]; then
		echo "apps: the published authority document is $named's, not $authority's" >&2
		return 1
	fi
	echo "$body"
}

# A published closure, by its URL, to a file: a dry run reads its own
# simulated site.
fetch_closure() {
	local url="$1" out="$2" site
	site="$(site_url)"
	if [ -n "$DRY" ] && [ "${url#"$site"/}" != "$url" ]; then
		store_get site "${url#"$site"/}" "$out"
		[ "$STATUS" = 200 ]
		return
	fi
	curl -fsS --max-time 60 -o "$out" "$url"
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
	say "building substratectl from this tree to check the closures it publishes"
	(cd "$ROOT" && go build -o "$tmp/substratectl" ./cmd/substratectl) || {
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
			fetch_closure "$nurl" "$tmp/$pkg-$nv.yaml" || {
				echo "apps: fetching the published $authority/$pkg version $nv closure from $nurl failed; the candidate cannot be compared with it" >&2
				rm -rf "$tmp"
				return 1
			}
		fi
		args=(publish "$authority/$pkg" --check --file "$out/packages/$pkg-$version.yaml" --version "$version"
			--closure "$site/packages/$pkg-$version.yaml" --document "$doc")
		if [ "${SUBSTRATE_APPLE_SITE_ALLOW_NARROWING:-}" = 1 ]; then args+=(--allow-narrowing); fi
		if ! "$tmp/substratectl" "${args[@]}" >/dev/null; then
			echo "apps: $authority/$pkg version $version is not a closure the server would admit, so nothing is published; fix it, or ship it as the next version" >&2
			rm -rf "$tmp"
			return 1
		fi
		say "checked $authority/$pkg version $version as the server admits it"
	done <<<"$new"
	rm -rf "$tmp"
}

# The SHA-256 fingerprints an app's assetlinks.json entry names, as a JSON
# array: the certificate of the APK this release staged, read from the
# signed APK itself (android_check), else that of the newest Android build
# its feed lists, and any SUBSTRATE_ANDROID_PREVIOUS_CERT_SHA256 (a
# rotation's old key, while installs signed with it remain). Empty for an
# app with no Android build yet, whose links nothing can verify.
android_fingerprints() {
	local app="$1" staged="" published="" file
	if [ -n "${build:-}" ]; then
		file="$STAGE/$(feed_prefix "$app")releases/$build/release.json"
		[ -f "$file" ] && staged="$(jq -r '.android.certificateSHA256 // empty' "$file")"
	fi
	file="$WORK/published/$app.json"
	[ -f "$file" ] && published="$(jq -r '[.releases[]? | select(.android) | .android.certificateSHA256] | first // empty' "$file")"
	# shellcheck disable=SC2086
	printf '%s\n' $staged $published ${SUBSTRATE_ANDROID_PREVIOUS_CERT_SHA256:-} | jq -Rnc '[inputs | select(length > 0)] | unique'
}

# The site under SUBSTRATE_APPLE_SITE_URL, an https origin on a host of its
# own, served at its root, built under build/apps/site from the closures in
# $1 (closures.py --out): Apple's association file naming every Apple app
# id for `webcredentials` and for `applinks` on its own callback path;
# Android's assetlinks.json naming every Android app's package and the
# certificate it is signed with, for its links and its credentials; the
# authority document listing every app by client id with the packages it
# asks for, and every published package version with its closure and
# digest; each app's client document and callback pages; and the closures
# themselves under packages/<package>-<version>.yaml (decision record
# 0212). Every version the live document already lists is carried forward
# as it is, and a listed version whose closure this tree builds with
# another digest is refused, because a published version never changes. A
# closure published for the first time is checked as the server admits it
# before anything goes up (check_publications). The authority is
# SUBSTRATE_APPLE_SITE_AUTHORITY, else the site's host; the team id that
# signs the Apple apps is SUBSTRATE_APPLE_TEAM_ID. Sets SITE_PUBLISHED, what
# the site serves now, for site_publish.
site_build() {
	local closures="$1"
	: "${SUBSTRATE_APPLE_TEAM_ID:?set SUBSTRATE_APPLE_TEAM_ID to the team id that signs the apps}"
	local site host authority target out app id
	site="$(site_url)" || exit 1
	host="${site#https://}"
	authority="${SUBSTRATE_APPLE_SITE_AUTHORITY:-$host}"
	target="$(store_base site)"
	out="$WORK/site"
	rm -rf "$out"
	mkdir -p "$out/.well-known/substrate" "$out/packages"
	cp "$closures"/*.yaml "$out/packages/" 2>/dev/null || true

	local built
	built="$(for file in "$out"/packages/*.yaml; do [ -f "$file" ] && echo "$file $(shasum -a 256 "$file" | cut -d' ' -f1)"; done |
		jq -Rn --arg site "$site" '[inputs | select(length > 0) | split(" ") |
		(.[0] | split("/") | last) as $file |
		($file | sub("\\.yaml$"; "") | capture("^(?<name>.+)-(?<version>[0-9]+)$")) as $m |
		{package: $m.name, version: ($m.version | tonumber), closure: "\($site)/packages/\($file)", sha256: .[1]}]')"

	# What the site serves today: every version it lists stays listed.
	local published
	published="$(published_document "$site" "$authority" "$target")" || exit 1
	SITE_PUBLISHED="$published"
	local conflicts pkg version live mine newest
	conflicts="$(jq -r --argjson built "$built" '[.packages // [] | .[] as $lp | ($lp.versions // [])[] as $lv |
		$built[] | select(.package == $lp.package and .version == $lv.version and .sha256 != $lv.sha256) |
		"\(.package) \(.version) \($lv.sha256) \(.sha256)"] | .[]' <<<"$published")"
	if [ -n "$conflicts" ]; then
		while read -r pkg version live mine; do
			echo "apps: $authority/$pkg version $version is published with digest $live and this tree builds $mine; a published version never changes, so ship the change as the next version" >&2
		done <<<"$conflicts"
		exit 1
	fi
	local stale
	stale="$(jq -r --argjson built "$built" '[.packages // [] | .[] as $lp | ([$lp.versions[]?.version] | max) as $newest |
		$built[] | select(.package == $lp.package and .version < $newest and ([$lp.versions[]?.version] | index(.version) | not)) |
		"\(.package) \(.version) \($newest)"] | .[]' <<<"$published")"
	if [ -n "$stale" ]; then
		while read -r pkg version newest; do
			echo "apps: $authority/$pkg version $version is below the published $newest and was never published; publish from a tree at or past it" >&2
		done <<<"$stale"
		exit 1
	fi
	check_publications "$out" "$published" "$built" "$authority" "$site" || exit 1
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

	local details="[]" ids="[]" apps="[]" links="[]" entry fingerprints
	for app in $(ui_apps); do
		mkdir -p "$out/apps/$app"
		client_document "$app" >"$out/apps/$app/client.json"
		entry="$(jq -n --arg client "$site/apps/$app/client.json" --arg name "$(app_get "$app" .name)" \
			--argjson packages "$(jq -c '[.publishes[] | split("/") | last]' "$(app_json "$app")")" \
			'{client_id: $client, name: $name, packages: $packages}')"
		if app_has_apple "$app"; then
			id="$SUBSTRATE_APPLE_TEAM_ID.$(app_get "$app" .apple.bundleID)"
			callback_page "$app" >"$out/apps/$app/callback"
			details="$(jq -c --arg id "$id" --arg path "/apps/$app/callback" \
				'. + [{appIDs: [$id], components: [{"/": $path}]}]' <<<"$details")"
			ids="$(jq -c --arg id "$id" '. + [$id]' <<<"$ids")"
			entry="$(jq -c --arg id "$id" '. + {apple: {appIDs: [$id]}}' <<<"$entry")"
		fi
		if app_has "$app" android; then
			mkdir -p "$out/apps/$app/android"
			callback_page "$app" >"$out/apps/$app/android/callback"
			fingerprints="$(android_fingerprints "$app")"
			if [ "$fingerprints" = "[]" ]; then
				say "$app has no signed Android build yet, so assetlinks.json names none of its links"
			else
				links="$(jq -c --arg package "$(app_get "$app" .android.applicationID)" --argjson fingerprints "$fingerprints" \
					'. + [{relation: ["delegate_permission/common.handle_all_urls", "delegate_permission/common.get_login_creds"],
						target: {namespace: "android_app", package_name: $package, sha256_cert_fingerprints: $fingerprints}}]' <<<"$links")"
			fi
		fi
		apps="$(jq -c --argjson entry "$entry" '. + [$entry]' <<<"$apps")"
	done
	site_index "$authority" >"$out/index.html"
	jq -n --argjson details "$details" --argjson ids "$ids" \
		'{applinks: {details: $details}, webcredentials: {apps: $ids}}' >"$out/.well-known/apple-app-site-association"
	jq -n --argjson links "$links" '$links' >"$out/.well-known/assetlinks.json"
	jq -n --arg authority "$authority" --argjson apps "$apps" --argjson packages "$packages" \
		'{authority: $authority, name: "Substrate", packages: $packages, apps: $apps}' >"$out/.well-known/substrate/authority.json"
	say "the site is built under build/apps/site"
}

# The site built by site_build, published: the closures first, immutable;
# the documents the apps and the platforms read next; the authority
# document last, so it never names a closure that is not there yet. Then
# each app's closures.lock.json beside its package sources, with what is
# published, for the offline check (committed after a publish; a dry run
# prints it and writes none).
site_publish() {
	local site authority out file app
	site="$(site_url)"
	authority="${SUBSTRATE_APPLE_SITE_AUTHORITY:-${site#https://}}"
	out="$WORK/site"
	for file in "$out"/packages/*.yaml; do
		[ -f "$file" ] || continue
		if [ -n "$(jq -r --arg file "${file##*/}" '.packages // [] | .[].versions[]? | select(.closure | endswith("/packages/" + $file)) | .sha256' <<<"$SITE_PUBLISHED")" ]; then
			continue
		fi
		put_immutable site "packages/${file##*/}" "$file" "public, max-age=31536000, immutable"
	done
	store_put site .well-known/apple-app-site-association "$out/.well-known/apple-app-site-association" no-cache
	store_put site .well-known/assetlinks.json "$out/.well-known/assetlinks.json" no-cache
	for app in $(ui_apps); do
		store_put site "apps/$app/client.json" "$out/apps/$app/client.json" no-cache
		[ ! -f "$out/apps/$app/callback" ] || store_put site "apps/$app/callback" "$out/apps/$app/callback" no-cache
		[ ! -f "$out/apps/$app/android/callback" ] || store_put site "apps/$app/android/callback" "$out/apps/$app/android/callback" no-cache
	done
	store_put site index.html "$out/index.html" no-cache
	store_put site .well-known/substrate/authority.json "$out/.well-known/substrate/authority.json" no-cache
	say "published $site/.well-known/substrate/authority.json"

	local lock locked line
	for app in $(all_apps); do
		[ "$(app_get "$app" '.publishes | length')" -gt 0 ] || continue
		lock="$APPS_DIR/$app/package/closures.lock.json"
		locked="$(jq --arg authority "$authority" --argjson mine "$(jq -c .publishes "$(app_json "$app")")" \
			'[.packages[] | select("\($authority)/\(.package)" as $id | $mine | index($id)) |
			{key: "\($authority)/\(.package)", value: (.versions[0] | {version, sha256})}] | from_entries' \
			"$out/.well-known/substrate/authority.json")"
		if [ -n "$DRY" ]; then
			say "dry run: would write apps/$app/package/closures.lock.json:"
			while IFS= read -r line; do say "  $line"; done <<<"$locked"
		elif [ "$(cat "$lock" 2>/dev/null)" != "$locked" ]; then
			printf '%s\n' "$locked" >"$lock"
			say "wrote apps/$app/package/closures.lock.json; commit it"
		fi
	done
	jq -r '.packages[] | "apps: \(.package): versions \([.versions[].version] | map(tostring) | join(", "))"' "$out/.well-known/substrate/authority.json"
	say "the authority's DNS needs this record, once:"
	say "  _substrate.$authority. TXT \"v=substrate1 doc=$site/.well-known/substrate/authority.json\""
}

# The page at an app's claimed callback, for the browser that opens it
# without handing it to the app: macOS's fallback when the default browser
# does not match the callback, or a device the association has not reached
# yet. It reads nothing from the address and keeps nothing.
callback_page() {
	local name
	name="$(app_get "$1" .name)"
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

# Every app's closures, built and held to its lock (.mise/closures.py), to $1.
build_closures() {
	rm -rf "$1"
	mkdir -p "$1"
	(cd "$ROOT" && uv run --quiet --with pyyaml .mise/closures.py --out "$1" >/dev/null) || die "the closures did not build; nothing is published"
}

# --- the release -----------------------------------------------------------

# What each app's feed lists today, to build/apps/published/<app>.json:
# the history a new index extends and the highest Android versionCode
# published. A missing index is an app's first release; anything else that
# is not a 200 stops here rather than publish an index that forgot every
# release before.
read_published() {
	local app="$1" file="$WORK/published/$1.json"
	mkdir -p "$WORK/published"
	store_get release "$(feed_prefix "$app")index.json" "$file"
	case "$STATUS" in
	200) ;;
	404) : >"$file" ;;
	*) die "reading $(feed_url "$app")/index.json answered $STATUS" ;;
	esac
}

# The release's versionCode for an app's Android build, allocated under the
# lease: the minutes from 2026-01-01 UTC to the build's UTC time, or the
# highest code the app's feed lists plus one, whichever is larger. The
# minutes keep a code rising with the build number across machines; the
# floor keeps it above anything published even when a clock is behind.
allocate_version_code() {
	local app="$1" minutes highest
	minutes="$(version_code_minutes "$build")"
	highest="$(jq -s '[.[] | .releases[]? | .android.versionCode // empty] | max // 0' "$WORK/published/$app.json")"
	if [ "$minutes" -gt "$highest" ]; then echo "$minutes"; else echo $((highest + 1)); fi
}

# Every selected app built on every platform it ships and checked, its
# release.json written beside its staged files; the closures built and
# checked as the server admits them; the site built. Nothing is uploaded
# here, and nothing is uploaded at all unless every step passed.
build_all() {
	local app prefix dir scratch platforms code date
	if [ -n "${SUBSTRATE_APPS_RELEASE_PLACEHOLDERS:-}" ]; then
		[ -n "$DRY" ] || die "SUBSTRATE_APPS_RELEASE_PLACEHOLDERS stands placeholders in for the builds, which only a dry run may"
		say "dry run: placeholders stand in for every build"
	fi
	for app in "${RELEASE_APPS[@]}"; do
		if app_has "$app" android; then
			code="$(allocate_version_code "$app")"
			say "$app's Android versionCode is $code"
			echo "$code" >"$REL_DIR/work/$app.versionCode"
		fi
	done
	if apple_needed; then apple_prepare; fi
	for app in "${RELEASE_APPS[@]}"; do
		prefix="$(feed_prefix "$app")"
		dir="$STAGE/${prefix}releases/$build"
		scratch="$REL_DIR/work/$app"
		mkdir -p "$dir" "$scratch"
		client_url "$app" >/dev/null || exit 1
		platforms="$(app_platforms "$app" | paste -sd, - | sed 's/,/, /g')"
		say "building $app ($platforms) $version"
		echo '{}' >"$scratch/apple.json"
		echo '{}' >"$scratch/android.json"
		if app_has_apple "$app"; then apple_release_build "$app" "$scratch" "$dir"; fi
		if app_has "$app" android; then android_release_build "$app" "$scratch" "$dir" "$(cat "$REL_DIR/work/$app.versionCode")"; fi
		# The icons the iOS install manifest names.
		if app_has "$app" ios; then
			local icon
			icon="$APPS_DIR/$app/$(app_get "$app" .icon)"
			sips -z 57 57 "$icon" --out "$dir/icon-57.png" >/dev/null
			sips -z 512 512 "$icon" --out "$dir/icon-512.png" >/dev/null
		fi
	done
	date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	for app in "${RELEASE_APPS[@]}"; do
		dir="$STAGE/$(feed_prefix "$app")releases/$build"
		scratch="$REL_DIR/work/$app"
		jq -n --arg name "$(app_get "$app" .name)" --arg version "$version" --arg short "$short" --arg build "$build" \
			--arg channel "$channel" --arg commit "$commit" --arg date "$date" \
			--arg bundle "$(app_get "$app" '.apple.bundleID // .android.applicationID')" \
			--slurpfile apple "$scratch/apple.json" --slurpfile android "$scratch/android.json" '{
				name: $name, version: $version, shortVersion: $short, build: $build, channel: $channel,
				commit: $commit, date: $date, bundleIdentifier: $bundle
			} + $apple[0] + $android[0]' >"$dir/release.json"
	done
	build_closures "$REL_DIR/packages"
	if [ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ]; then site_build "$REL_DIR/packages"; fi
	stage_freeze
}

# The staged release, sealed: staged.json names the version, the apps and
# every file with its sha256, and the files are made read-only. A rerun of
# the build checks every file against it before it uploads one.
stage_freeze() {
	local files
	files="$(cd "$STAGE" && find . -type f | sed 's|^\./||' | sort | while read -r key; do
		printf '%s %s\n' "$key" "$(shasum -a 256 "$key" | cut -d' ' -f1)"
	done | jq -Rn '[inputs | split(" ") | {key: .[0], value: .[1]}] | from_entries')"
	local closures
	closures="$(cd "$REL_DIR/packages" && for f in *.yaml; do [ -f "$f" ] && printf '%s %s\n' "$f" "$(shasum -a 256 "$f" | cut -d' ' -f1)"; done |
		jq -Rn '[inputs | select(length > 0) | split(" ") | {key: .[0], value: .[1]}] | from_entries')"
	jq -n --arg version "$version" --arg short "$short" --arg build "$build" --arg channel "$channel" \
		--arg commit "$commit" --arg commitFull "$commit_full" --argjson files "$files" --argjson closures "$closures" \
		--argjson apps "$(printf '%s\n' "${RELEASE_APPS[@]}" | jq -Rn '[inputs]')" \
		'{version: $version, shortVersion: $short, build: $build, channel: $channel, commit: $commit,
			commitFull: $commitFull, apps: $apps, files: $files, closures: $closures}' >"$REL_DIR/staged.json"
	chmod -R a-w "$STAGE" "$REL_DIR/packages"
	say "staged $version under build/apps/releases/$build"
}

# A staged build, read back for a rerun: the version and the apps from
# staged.json, and every staged file held to its digest.
stage_load() {
	local staged="$REL_DIR/staged.json"
	[ -f "$staged" ] || die "build $build is not staged here (no build/apps/releases/$build/staged.json); a rerun resumes a build this machine staged"
	version="$(jq -r .version "$staged")"
	short="$(jq -r .shortVersion "$staged")"
	channel="$(jq -r .channel "$staged")"
	commit="$(jq -r .commit "$staged")"
	commit_full="$(jq -r .commitFull "$staged")"
	RELEASE_APPS=()
	local app key sha
	for app in $(jq -r '.apps[]' "$staged"); do RELEASE_APPS+=("$app"); done
	while read -r key sha; do
		[ -f "$STAGE/$key" ] || die "the staged $key is gone; build $build cannot be resumed"
		[ "$(shasum -a 256 "$STAGE/$key" | cut -d' ' -f1)" = "$sha" ] || die "the staged $key changed since it was staged; build $build cannot be resumed"
	done < <(jq -r '.files | to_entries[] | "\(.key) \(.value)"' "$staged")
	while read -r key sha; do
		[ "$(shasum -a 256 "$REL_DIR/packages/$key" | cut -d' ' -f1)" = "$sha" ] || die "the staged closure $key changed since it was staged; build $build cannot be resumed"
	done < <(jq -r '.closures | to_entries[] | "\(.key) \(.value)"' "$staged")
	say "resuming $version, staged for ${RELEASE_APPS[*]}"
}

# Every staged file but the release.json files, created under its feed's
# releases/<build>/, where no feed names it yet: immutable, so a rerun
# passes over what is there with the same bytes and refuses anything else.
stage_upload() {
	local key
	while read -r key; do
		case "$key" in */release.json | release.json) continue ;; esac
		put_immutable release "$key" "$STAGE/$key" "public, max-age=31536000, immutable"
	done < <(jq -r '.files | keys[]' "$REL_DIR/staged.json")
	say "staged ${RELEASE_APPS[*]} under releases/$build"
}

# One app made current, last: its release.json, written after every file it
# names, then the feeds its installed builds read today, at the addresses
# they read (index.json, Sparkle's appcast.xml, the install page and the
# iOS manifest), rewritten from the index published now and this release.
# The index is written only over the version of it that was read, so a
# writer that ignored the lease is refused, not overwritten. Per app: a
# release of three apps can stop with one activated, and a rerun activates
# the rest.
activate() {
	local app="$1" prefix out base staged
	prefix="$(feed_prefix "$app")"
	base="$(feed_url "$app")"
	staged="$STAGE/${prefix}releases/$build/release.json"
	out="$REL_DIR/feeds/$app"
	rm -rf "$out"
	mkdir -p "$out"
	read_published "$app"
	local index_etag="$ETAG" index_status="$STATUS"
	put_immutable release "${prefix}releases/$build/release.json" "$staged" "public, max-age=31536000, immutable"
	swift "$APPS_DIR/xcode/scripts/feed.swift" "$base/" "$WORK/published/$app.json" "$staged" "$out"
	if [ -f "$out/releases/$build/manifest.plist" ]; then
		put_immutable release "${prefix}releases/$build/manifest.plist" "$out/releases/$build/manifest.plist" "public, max-age=31536000, immutable"
	fi
	if [ "$index_status" = 200 ]; then
		store_put release "${prefix}index.json" "$out/index.json" no-cache "match:$index_etag"
	else
		store_put release "${prefix}index.json" "$out/index.json" no-cache create
	fi
	store_put release "${prefix}appcast.xml" "$out/appcast.xml" no-cache
	store_put release "${prefix}index.html" "$out/index.html" no-cache
	say "activated $(app_get "$app" .name) $version: $base/index.html"
}

# With no site, each app's client document goes to the client documents'
# folder alone, as releases before the site did.
publish_clients() {
	local app client base
	base="$(client_base)" || exit 1
	for app in "${RELEASE_APPS[@]}"; do
		client_document "$app" >"$REL_DIR/work/$app/client.json"
		client="$(client_url "$app")"
		store_put client "${client#"$base"/}" "$REL_DIR/work/$app/client.json" no-cache
	done
}

# SUBSTRATE_APPS_RELEASE_STOP_AFTER=stage|site|<app> stops a dry run there,
# as a failure would, to try the rerun that resumes it.
stop_point() {
	if [ "${SUBSTRATE_APPS_RELEASE_STOP_AFTER:-}" = "$1" ]; then
		[ -n "$DRY" ] || die "SUBSTRATE_APPS_RELEASE_STOP_AFTER is a dry run's"
		echo "apps: stopping after $1, as SUBSTRATE_APPS_RELEASE_STOP_AFTER asks; SUBSTRATE_APPS_RELEASE_BUILD=$build resumes it" >&2
		exit 3
	fi
}

release_env() {
	: "${SUBSTRATE_APPLE_RELEASE_URL:?set SUBSTRATE_APPLE_RELEASE_URL to the https address of the release folder}"
	case "$SUBSTRATE_APPLE_RELEASE_URL" in
	https://*) ;;
	*) die "SUBSTRATE_APPLE_RELEASE_URL is an https address: $SUBSTRATE_APPLE_RELEASE_URL" ;;
	esac
	if [ -z "$DRY" ]; then
		[ -z "${SUBSTRATE_APPLE_SITE_PUBLISHED+x}" ] || die "SUBSTRATE_APPLE_SITE_PUBLISHED tries the merge against a saved document, which only a dry run may"
		: "${AWS_ACCESS_KEY_ID:?set AWS_ACCESS_KEY_ID to a key that may write the bucket}"
		: "${AWS_SECRET_ACCESS_KEY:?set AWS_SECRET_ACCESS_KEY}"
		[ -z "${SUBSTRATE_APPS_RELEASE_ANDROID_BUILD:-}" ] || die "SUBSTRATE_APPS_RELEASE_ANDROID_BUILD stands a test build in for android:release:build, which only a dry run may"
		[ -z "${SUBSTRATE_APPS_RELEASE_PLACEHOLDERS:-}" ] || die "SUBSTRATE_APPS_RELEASE_PLACEHOLDERS stands placeholders in for the builds, which only a dry run may"
		[ -z "${SUBSTRATE_APPS_RELEASE_STOP_AFTER:-}" ] || die "SUBSTRATE_APPS_RELEASE_STOP_AFTER is a dry run's"
	fi
	if [ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ]; then site_url >/dev/null || exit 1; fi
}

release() {
	local channel="prerelease"
	case "${1:-}" in
	prerelease | stable)
		channel="$1"
		shift
		;;
	esac
	release_env
	RELEASE_APPS=("$@")
	local app resume="${SUBSTRATE_APPS_RELEASE_BUILD:-}"
	short="" build="" commit="" commit_full="" version=""
	if [ -n "$resume" ]; then
		build="$resume"
		REL_DIR="$WORK/releases/$build"
		STAGE="$REL_DIR/stage"
		local asked=("$@")
		stage_load
		if [ "${#asked[@]}" -gt 0 ] && [ "${asked[*]}" != "${RELEASE_APPS[*]}" ]; then
			die "build $build was staged for ${RELEASE_APPS[*]}, not ${asked[*]}; a rerun releases what was staged"
		fi
		[ "$(git -C "$ROOT" rev-parse HEAD)" = "$commit_full" ] || die "build $build was built from $commit_full, and this tree is at $(git -C "$ROOT" rev-parse HEAD); a rerun runs from the commit it built"
	else
		if [ "${#RELEASE_APPS[@]}" -eq 0 ]; then
			for app in $(ui_apps); do RELEASE_APPS+=("$app"); done
		fi
		for app in "${RELEASE_APPS[@]}"; do
			[ -f "$(app_json "$app")" ] || die "$app is no app: apps/$app/app.json is not there"
			[ -n "$(app_platforms "$app")" ] || die "$app has no UI on any platform its app.json names, so there is nothing of it to release"
			if app_has "$app" android && [ -z "${SUBSTRATE_APPLE_SITE_URL:-}" ]; then
				die "$app ships on Android, whose sign-in callback and links the site verifies; set SUBSTRATE_APPLE_SITE_URL"
			fi
			client_url "$app" >/dev/null || exit 1
		done
		stamp "$channel"
		REL_DIR="$WORK/releases/$build"
		STAGE="$REL_DIR/stage"
		[ ! -e "$REL_DIR" ] || die "build $build is already staged here; SUBSTRATE_APPS_RELEASE_BUILD=$build resumes it"
	fi
	say "releasing ${RELEASE_APPS[*]} as $version ($channel)${DRY:+, a dry run}"

	lease_take "$version"
	[ -n "$resume" ] || mkdir -p "$STAGE" "$REL_DIR/work"
	for app in "${RELEASE_APPS[@]}"; do read_published "$app"; done
	if [ -z "$resume" ]; then
		build_all
	elif [ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ]; then
		site_build "$REL_DIR/packages"
	fi
	[ "$(git -C "$ROOT" rev-parse HEAD)" = "$commit_full" ] || die "HEAD moved while $version was built; nothing is published"

	stage_upload
	stop_point stage
	if [ -n "${SUBSTRATE_APPLE_SITE_URL:-}" ]; then
		site_publish
	else
		publish_clients
	fi
	stop_point site
	for app in "${RELEASE_APPS[@]}"; do
		activate "$app"
		stop_point "$app"
	done
	say "released ${RELEASE_APPS[*]} as $version ($channel)${DRY:+, a dry run: the release folder and the site are under build/apps/dry-run}"
}

# The site alone, with no build: the closures this tree ships, every app's
# client document and both association files, under the lease.
site() {
	: "${SUBSTRATE_APPLE_SITE_URL:?set SUBSTRATE_APPLE_SITE_URL to the https origin of the site}"
	site_url >/dev/null || exit 1
	if [ -z "$DRY" ]; then
		[ -z "${SUBSTRATE_APPLE_SITE_PUBLISHED+x}" ] || die "SUBSTRATE_APPLE_SITE_PUBLISHED tries the merge against a saved document, which only a dry run may"
		: "${AWS_ACCESS_KEY_ID:?set AWS_ACCESS_KEY_ID to a key that may write the site}"
		: "${AWS_SECRET_ACCESS_KEY:?set AWS_SECRET_ACCESS_KEY}"
	fi
	# The lease is in the release folder, and so are the feeds the Android
	# apps' signing certificates are read from.
	: "${SUBSTRATE_APPLE_RELEASE_URL:?set SUBSTRATE_APPLE_RELEASE_URL: the release lease, and the feeds the Android certificates are read from, are there}"
	local app
	mkdir -p "$WORK"
	lease_take site
	for app in $(ui_apps); do
		if app_has "$app" android; then read_published "$app"; fi
	done
	build_closures "$WORK/site-packages"
	site_build "$WORK/site-packages"
	site_publish
	[ -z "$DRY" ] || say "dry run: the site is under build/apps/site, and what it published under build/apps/dry-run/site"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	case "${1:-}" in
	release)
		shift
		release "$@"
		;;
	site) site ;;
	*)
		echo "usage: $0 release [prerelease|stable] [app…] | site" >&2
		exit 2
		;;
	esac
fi
