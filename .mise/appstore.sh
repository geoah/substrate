#!/usr/bin/env bash
#
# The apps' App Store listings: `mise run apps:store [--check] [app…]`
# (docs/plans/app-store.md, apps/README.md §App Store). Each app's
# apps/<app>/store/listing.json is made App Store Connect's editable iOS
# version of it, through `asc` (pinned in .mise.toml): the texts, the
# categories, the age rating, the content rights, the copyright, the release
# type, the price and territories, the review details and the screenshots in
# apps/<app>/store/screenshots/<device type>/. It never submits for review.
#
# What names a deployment comes from the environment, never this tree: the
# support and privacy policy URLs, the copyright holder, and the review
# server, its sign-in and the review contact. `--check` writes nothing and
# prints what App Store Connect says still stands between the version and a
# submission.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
APPS_DIR="$ROOT/apps"

say() { echo "store: $*"; }
die() {
	echo "store: $*" >&2
	exit 1
}

: "${SUBSTRATE_APPLE_ASC_KEY_PATH:?set SUBSTRATE_APPLE_ASC_KEY_PATH to the App Store Connect API key (AuthKey_….p8)}"
: "${SUBSTRATE_APPLE_ASC_KEY_ID:?set SUBSTRATE_APPLE_ASC_KEY_ID beside the key path}"
: "${SUBSTRATE_APPLE_ASC_ISSUER_ID:?set SUBSTRATE_APPLE_ASC_ISSUER_ID beside the key path}"
# asc reads the same key, from its own names, and keeps nothing in a keychain;
# its telemetry is off.
export ASC_KEY_ID="$SUBSTRATE_APPLE_ASC_KEY_ID" ASC_ISSUER_ID="$SUBSTRATE_APPLE_ASC_ISSUER_ID" \
	ASC_PRIVATE_KEY_PATH="$SUBSTRATE_APPLE_ASC_KEY_PATH" ASC_BYPASS_KEYCHAIN=1 DO_NOT_TRACK=1

asc() { mise x -- asc "$@" --output json; }

app_get() { jq -r "$2" "$APPS_DIR/$1/app.json"; }
listing() { jq -r "$2 // empty" "$APPS_DIR/$1/store/listing.json"; }

# The App Store Connect record of an app, by its bundle id.
record() {
	xcrun swift "$APPS_DIR/xcode/scripts/appstoreconnect.swift" records "$1" | awk '{print $2}'
}

# The app's iOS version still open to edits, as "<id> <version>"; none when
# every version is in review or released.
editable_version() {
	asc versions list --app "$1" --platform IOS | jq -r '
		[.data[] | select(.attributes.appStoreState | IN("PREPARE_FOR_SUBMISSION", "DEVELOPER_REJECTED", "REJECTED", "METADATA_REJECTED", "INVALID_BINARY"))]
		| first | select(.) | "\(.id) \(.attributes.versionString)"'
}

# The listing's texts in asc's canonical layout, with the deployment's URLs.
metadata_dir() {
	local app="$1" version="$2" dir="$3"
	mkdir -p "$dir/app-info" "$dir/version/$version"
	jq -n --arg name "$(app_get "$app" .name)" --arg subtitle "$(listing "$app" .subtitle)" \
		--arg privacy "$SUBSTRATE_APPLE_PRIVACY_URL" \
		'{name: $name, subtitle: $subtitle, privacyPolicyUrl: $privacy}' >"$dir/app-info/en-US.json"
	jq --arg support "$SUBSTRATE_APPLE_SUPPORT_URL" '{
		description, promotionalText, keywords: (.keywords | join(",")),
		supportUrl: $support, marketingUrl: $support
	}' "$APPS_DIR/$app/store/listing.json" >"$dir/version/$version/en-US.json"
}

# The review notes, with the review server and its sign-in filled in.
review_notes() {
	listing "$1" .reviewNotes |
		sed -e "s|{server}|$SUBSTRATE_APPLE_REVIEW_SERVER|g" -e "s|{user}|$SUBSTRATE_APPLE_REVIEW_USER|g" \
			-e "s|{password}|$SUBSTRATE_APPLE_REVIEW_PASSWORD|g" -e "s|{privacy}|$SUBSTRATE_APPLE_PRIVACY_URL|g"
}

store() {
	local app="$1" id version_id version scratch flags=() detail
	id="$(record "$(app_get "$app" .apple.bundleID)")"
	[ -n "$id" ] || die "$app has no App Store Connect record"
	read -r version_id version <<<"$(editable_version "$id")"
	[ -n "${version_id:-}" ] || die "$app has no iOS version open to edits (in review, or released)"
	say "$(app_get "$app" .name): version $version"
	scratch="$(mktemp -d)"
	trap 'rm -rf "$scratch"' RETURN

	metadata_dir "$app" "$version" "$scratch/metadata"
	asc metadata apply --app "$id" --version "$version" --platform IOS --dir "$scratch/metadata" --confirm >/dev/null
	say "  texts, URLs"

	flags=(--app "$id" --primary "$(listing "$app" .primaryCategory)")
	[ -z "$(listing "$app" .secondaryCategory)" ] || flags+=(--secondary "$(listing "$app" .secondaryCategory)")
	asc app-setup categories set "${flags[@]}" >/dev/null
	say "  categories"

	# Every answer the listing does not give is none.
	flags=(--app "$id")
	while IFS=$'\t' read -r key value; do
		if [ "$key" = all-none ]; then flags+=(--all-none); else flags+=("--$key" "$value"); fi
	done < <(jq -r '.ageRating | to_entries[] | "\(.key)\t\(.value)"' "$APPS_DIR/$app/store/listing.json")
	asc age-rating edit "${flags[@]}" >/dev/null
	say "  age rating"

	asc apps content-rights edit --app "$id" --uses-third-party-content=false >/dev/null
	# MANUAL: a version that passes review waits for its owner to release it.
	asc versions update --version-id "$version_id" --copyright "$SUBSTRATE_APPLE_COPYRIGHT" --release-type MANUAL >/dev/null
	say "  content rights, copyright, manual release"

	asc pricing availability create --app "$id" --all-territories --available true --available-in-new-territories true \
		--if-exists update >/dev/null
	if [ "$(asc pricing current --app "$id" 2>/dev/null | jq -r '[.. | objects | select(has("customerPrice"))] | length')" = 0 ]; then
		asc app-setup pricing set --app "$id" --free --start-date "$(date -u +%F)" >/dev/null
	fi
	say "  free, in every territory"

	if [ -n "${SUBSTRATE_APPLE_REVIEW_SERVER:-}" ] && [ -n "${SUBSTRATE_APPLE_REVIEW_CONTACT_PHONE:-}" ]; then
		flags=(--contact-first-name "$SUBSTRATE_APPLE_REVIEW_CONTACT_FIRST" --contact-last-name "$SUBSTRATE_APPLE_REVIEW_CONTACT_LAST"
			--contact-email "$SUBSTRATE_APPLE_REVIEW_CONTACT_EMAIL" --demo-account-required true
			--demo-account-name "$SUBSTRATE_APPLE_REVIEW_USER" --demo-account-password "$SUBSTRATE_APPLE_REVIEW_PASSWORD"
			--contact-phone "$SUBSTRATE_APPLE_REVIEW_CONTACT_PHONE" --notes "$(review_notes "$app")")
		detail="$(asc review details-for-version --version-id "$version_id" | jq -r '.data.id // empty')"
		if [ -n "$detail" ]; then
			asc review details-update --id "$detail" "${flags[@]}" >/dev/null
		else
			asc review details-create --version-id "$version_id" "${flags[@]}" >/dev/null
		fi
		say "  review details"
	else
		# App Store Connect refuses review details without a contact phone.
		say "  review details skipped: set SUBSTRATE_APPLE_REVIEW_SERVER, the rest of its sign-in and SUBSTRATE_APPLE_REVIEW_CONTACT_PHONE (apps/README.md §App Store)"
	fi

	local shots type
	for shots in "$APPS_DIR/$app/store/screenshots"/*/; do
		[ -d "$shots" ] || continue
		type="$(basename "$shots")"
		asc screenshots upload --app "$id" --version "$version" --locale en-US --path "$shots" \
			--device-type "$type" --replace --confirm >/dev/null
		say "  screenshots: $type"
	done
}

check() {
	local app="$1" id version_id version
	id="$(record "$(app_get "$app" .apple.bundleID)")"
	read -r version_id version <<<"$(editable_version "$id")"
	say "$(app_get "$app" .name) $version:"
	# asc exits non-zero on a blocking issue, which is what this lists.
	{ asc validate --app "$id" --version-id "$version_id" || true; } | jq -r '
		[.. | objects | select(has("severity") and has("message")) | "  \(.severity): \(.message)"] | unique | .[]'
}

mode=store
if [ "${1:-}" = --check ]; then
	mode=check
	shift
fi
if [ "$mode" = store ]; then
	for name in SUBSTRATE_APPLE_PRIVACY_URL SUBSTRATE_APPLE_SUPPORT_URL SUBSTRATE_APPLE_COPYRIGHT; do
		[ -n "${!name:-}" ] || die "set $name (apps/README.md §App Store)"
	done
	if [ -n "${SUBSTRATE_APPLE_REVIEW_SERVER:-}" ]; then
		for name in SUBSTRATE_APPLE_REVIEW_USER SUBSTRATE_APPLE_REVIEW_PASSWORD SUBSTRATE_APPLE_REVIEW_CONTACT_FIRST \
			SUBSTRATE_APPLE_REVIEW_CONTACT_LAST SUBSTRATE_APPLE_REVIEW_CONTACT_EMAIL; do
			[ -n "${!name:-}" ] || die "set $name beside SUBSTRATE_APPLE_REVIEW_SERVER"
		done
	fi
fi
apps=("$@")
if [ "${#apps[@]}" -eq 0 ]; then
	for listing_file in "$APPS_DIR"/*/store/listing.json; do apps+=("$(basename "$(dirname "$(dirname "$listing_file")")")"); done
fi
for app in "${apps[@]}"; do
	[ -f "$APPS_DIR/$app/store/listing.json" ] || die "$app has no store/listing.json"
	"$mode" "$app"
done
