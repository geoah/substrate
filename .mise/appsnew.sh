#!/usr/bin/env bash
#
# mise run apps:new <id> [--apps DIR]
#
# A new app, apps/<id>/, copied from apps/template (docs/plans/app-kit.md
# §7.3): its app.json, its core <Id>Kit (a Swift package named for it, so no
# two apps' package folders collide), apple/ with its targets, Info.plists and
# entitlements, package/ with one example kind, and its README. The template
# is a whole app named `template` that lint:apps and the server's reach test
# hold like any other, so what this writes passes both before anyone edits
# it; the release and the site skip the template by name.
#
# Every name the template spells is renamed: `Template` to <Id> (the id with
# its first letter upper case), `template` to <id>, `TEMPLATE` to the id in
# upper case. A kind's `displayTemplate` key is the dialect's and keeps its
# spelling. Then apps/xcode/project.yml includes the new apple/targets.yml.
#
# --apps DIR writes into another apps folder, one holding kit/ and
# xcode/project.yml (CI's scratch copy, which builds the result's core on
# Linux); the template is always this tree's. Plain sed, find and cp, so it
# runs in the swift container as on a Mac.

set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
template="$root/apps/template"
apps="$root/apps"
id=""

die() {
	echo "apps:new: $*" >&2
	exit 1
}

while [ $# -gt 0 ]; do
	case "$1" in
	--apps)
		[ $# -ge 2 ] || die "--apps needs a folder"
		apps="$(cd "$2" && pwd)"
		shift 2
		;;
	-*) die "no option $1; usage: apps:new <id> [--apps DIR]" ;;
	*)
		[ -z "$id" ] || die "one id, not two: $id and $1"
		id="$1"
		shift
		;;
	esac
done

[ -n "$id" ] || die "usage: apps:new <id> [--apps DIR]"
# One lowercase word: the app's folder, its feed, its bundle id's last label
# and its package's name, which a package id holds to one word.
case "$id" in
*[!a-z0-9]* | [!a-z]*) die "$id is not one lowercase word starting with a letter" ;;
esac
case "$id" in
template | kit | xcode | gradle) die "$id is a folder of apps/ that is no app" ;;
esac
[ ! -e "$apps/$id" ] || die "apps/$id is there already"
[ -d "$apps/kit" ] || die "$apps has no kit/: an app's core depends on ../../kit"
project="$apps/xcode/project.yml"
[ -f "$project" ] || die "$apps has no xcode/project.yml to include the app's targets in"

upper_first="$(printf '%s' "${id:0:1}" | tr '[:lower:]' '[:upper:]')${id:1}"
upper_all="$(printf '%s' "$id" | tr '[:lower:]' '[:upper:]')"

rename() {
	sed -e 's/displayTemplate/@@DISPLAY@@/g' \
		-e "s/Template/${upper_first}/g" -e "s/template/${id}/g" -e "s/TEMPLATE/${upper_all}/g" \
		-e 's/@@DISPLAY@@/displayTemplate/g'
}

(cd "$template" && find . -type f ! -path '*/.build/*' ! -path '*/build/*' ! -path '*/.swiftpm/*' ! -name '.DS_Store') |
	sort | while IFS= read -r file; do
	target="$apps/$id/$(printf '%s' "${file#./}" | rename)"
	mkdir -p "$(dirname "$target")"
	case "$file" in
	*.png) cp "$template/$file" "$target" ;;
	*) rename <"$template/$file" >"$target" ;;
	esac
done

# Every name the template spells is renamed: a line of the new app that
# still says it, a kind's `displayTemplate` aside, is a rename this script
# missed, and the app is not left half named.
left="$(grep -rIi template "$apps/$id" | sed 's/displayTemplate//g' | grep -i template || true)"
if [ -n "$left" ]; then
	rm -rf "${apps:?}/$id"
	die "the rename left the template named; apps/$id is removed:
$left"
fi

include="  - ../$id/apple/targets.yml"
if ! grep -qxF -- "$include" "$project"; then
	# The include list is the file's last block; a file that ends otherwise
	# is refused rather than guessed at.
	tail -n 1 "$project" | grep -q '^  - \.\./.*/apple/targets\.yml$' ||
		die "$project does not end with its include list; add $include to it"
	printf '%s\n' "$include" >>"$project"
fi

echo "apps:new: apps/$id, Substrate $upper_first, from apps/template"
echo "  the core:     swift test --package-path apps/$id/${upper_first}Kit"
echo "  the app:      mise run apple:project, then its scheme, Substrate${upper_first}"
echo "  its package:  apps/$id/package (substrate.reamde.dev/$id), published through the authority document"
# The template's look is grey on purpose, so an app nobody has given its own
# colour yet is plain to see.
echo "  its look:     it is the template's grey until you give it its own:"
echo "                1. its accent: \"accent\" in apps/$id/app.json and apps/$id/apple/Assets.xcassets/AccentColor.colorset, the same light and dark #RRGGBB (lint:apps holds the two equal)"
echo "                2. its icon: an entry for $id in APPS in apps/xcode/scripts/icons.mjs (a hue no app uses, a glyph in GLYPHS; docs/app-icons.md), then"
echo "                   node apps/xcode/scripts/icons.mjs appiconset $id apps/$id/apple/Assets.xcassets/AppIcon.appiconset --mac"
