#!/usr/bin/env bash
# `mise run apps:gallery` (docs/plans/app-kit.md §7.4): every shared screen's
# fixtures drawn by both renderers, side by side. SwiftUI's under `swift
# test` (SubstrateKitTests' Snapshots, Xcode's toolchain) into
# build/gallery/apple, Compose's under Robolectric (kit-ui's KitGalleryTest)
# into build/gallery/android, each fixture as <name>.png in both, and
# build/gallery/index.html showing each fixture's two next to each other;
# beside them, the kit's views as the apps call them (apple/apps), and
# Substrate Tasks' own screens on both (apple/tasks, android/tasks, from
# TasksFixtures.all, which its Android module reads as
# apps/tasks/android/fixtures/tasks.json).
# Both sets draw from one source: ScreenFixtures.all, which kit-ui reads as
# apps/kit/android/fixtures/screens.json. A fixture either side cannot draw
# fails its test, and so this.
set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "$root"
out=$root/build/gallery
fixtures=apps/kit/android/fixtures/screens.json
tasks_fixtures=apps/tasks/android/fixtures/tasks.json

rm -rf "$out"
mkdir -p "$out/apple" "$out/android"

SUBSTRATEKIT_SNAPSHOTS="$out/apple" xcrun swift test --package-path apps/kit --filter 'Snapshots|GalleryRenderers'
# Substrate Tasks' screens, drawn by its own views in its UI framework's
# tests, which run on the Mac with no host app.
mkdir -p "$out/apple/tasks"
.mise/apple.sh project
(cd apps/xcode && TEST_RUNNER_SUBSTRATE_TASKS_GALLERY="$out/apple/tasks" xcodebuild -project Substrate.xcodeproj \
  -scheme SubstrateTasks -derivedDataPath build/dd -quiet -destination 'platform=macOS' test -only-testing:SubstrateTasksTests/GalleryTests)
.mise/android.sh gallery "$out/android"

html() {
  local text=$1
  text=${text//&/&amp;}
  text=${text//</&lt;}
  text=${text//>/&gt;}
  printf '%s' "$text"
}

{
  cat <<'HEAD'
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>The shared screens, on both systems</title>
<style>
  body { font: 15px/1.4 -apple-system, system-ui, sans-serif; margin: 24px; color: #1c1c1e; background: #f2f2f7; }
  h1 { font-size: 22px; }
  .fixture { background: #fff; border-radius: 16px; padding: 16px; margin: 0 0 24px; }
  .fixture h2 { font: 600 15px/1.3 ui-monospace, monospace; margin: 0 0 12px; }
  .pair { display: flex; gap: 24px; flex-wrap: wrap; }
  figure { margin: 0; }
  figcaption { color: #6c6c70; font-size: 13px; margin: 0 0 6px; }
  img { width: 393px; border: 0.5px solid #c6c6c8; border-radius: 12px; }
  .missing { width: 393px; height: 120px; display: grid; place-items: center; color: #ff3b30; border: 1px dashed #ff3b30; border-radius: 12px; }
</style>
</head>
<body>
<h1>The shared screens, on both systems</h1>
<p>Each fixture of <code>ScreenFixtures.all</code>, drawn by SubstrateUI (SwiftUI) and by kit-ui (Compose) from the same presentation.</p>
HEAD
  while IFS= read -r name; do
    printf '<section class="fixture" id="%s">\n<h2>%s</h2>\n<div class="pair">\n' "$(html "$name")" "$(html "$name")"
    for side in apple android; do
      label=$([[ $side == apple ]] && echo "Apple (SwiftUI)" || echo "Android (Compose)")
      if [[ -f $out/$side/$name.png ]]; then
        printf '<figure><figcaption>%s</figcaption><img src="%s/%s.png" alt="%s on %s"></figure>\n' "$label" "$side" "$(html "$name")" "$(html "$name")" "$side"
      else
        printf '<figure><figcaption>%s</figcaption><div class="missing">not drawn</div></figure>\n' "$label"
      fi
    done
    printf '</div>\n</section>\n'
  done < <(jq -r '.[].name' "$fixtures")
  # Substrate Tasks' own screens, drawn by its SwiftUI views and its
  # Compose screens from TasksKit's fixtures, and the wide layout of two.
  printf '<h1>Substrate Tasks</h1>\n<p>Each fixture of <code>TasksFixtures.all</code>, drawn by the Apple app (SwiftUI) and the Android app (Compose).</p>\n'
  while IFS= read -r name; do
    printf '<section class="fixture" id="%s">\n<h2>%s</h2>\n<div class="pair">\n' "$(html "$name")" "$(html "$name")"
    for side in apple android; do
      label=$([[ $side == apple ]] && echo "Apple (SwiftUI)" || echo "Android (Compose)")
      if [[ -f $out/$side/tasks/$name.png ]]; then
        printf '<figure><figcaption>%s</figcaption><img src="%s/tasks/%s.png" alt="%s on %s"></figure>\n' "$label" "$side" "$(html "$name")" "$(html "$name")" "$side"
      else
        printf '<figure><figcaption>%s</figcaption><div class="missing">not drawn</div></figure>\n' "$label"
      fi
    done
    printf '</div>\n</section>\n'
  done < <(jq -r '.[].name' "$tasks_fixtures"; printf '%s\n' tasks-detail-wide tasks-today-wide)
  printf '</body>\n</html>\n'
} >"$out/index.html"

count() { find "$1" -maxdepth 1 -name '*.png' | wc -l | tr -d ' '; }
echo "apps:gallery: $(count "$out/apple") Apple and $(count "$out/android") Android screens, and $(count "$out/apple/tasks") and $(count "$out/android/tasks") of Substrate Tasks, side by side in $out/index.html"
