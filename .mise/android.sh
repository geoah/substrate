#!/usr/bin/env bash
# The Android build of the apps (docs/plans/app-kit.md §5.5, §8): one
# subcommand per `android:*` task in .mise.toml.
#
#   sdk             install the pinned SDK packages and the Swift SDK for
#                   Android, and link the bundle to the NDK
#   generate        write bridge.golden.json, the Kotlin and the fingerprint
#   generate:check  fail when any of them is stale
#   build           every module, debug and release, for SUBSTRATE_ABIS
#   test            the generator's tests and check, the bridge's Swift tests
#                   on this host, the Kotlin unit tests, and the round trip
#                   (Swift encodes, Kotlin decodes and encodes, Swift decodes)
#   smoke           on a booted emulator or device: the release build's
#                   runtime checks, three cold starts, the instrumented tests
#                   (the Gallery's: every fixture Swift makes there decodes)
#   gallery <dir>   kit-ui's gallery test, keeping each fixture's PNG in <dir>,
#                   and each app's (its :<app> module's), in <dir>/<app>
#   lock            rewrite the Gradle dependency locks and the dependency
#                   verification metadata (every artifact's sha256)
#   release:build   one app's release APK, signed, for .mise/apps.sh
#
# The tasks run with the open-source Swift and the JDK as task tools
# (`tools =` on each task), so `swift` here is never Xcode's and the
# apple:* tasks keep Xcode's. lint:toolchain holds the pins below to the
# Gradle catalog and to those task tools.
set -euo pipefail

root=$(git rev-parse --show-toplevel)
cd "$root"

# --- the pins ---------------------------------------------------------------
swift_version=6.4.2
swift_sdk_bundle=swift-${swift_version}-RELEASE_android.artifactbundle
swift_sdk_url=https://download.swift.org/swift-${swift_version}-release/android-sdk/swift-${swift_version}-RELEASE/${swift_sdk_bundle}.tar.gz
swift_sdk_checksum=4d6ad63ea74becd58e123ff58b1b48c3be5ec0db0dff0ea5abe37ac007dd3233
ndk_version=30.0.16248370
sdk_packages=(
  "platform-tools"
  "platforms;android-37.0"
  "build-tools;36.0.0"
  "ndk;${ndk_version}"
)

abis=${SUBSTRATE_ABIS:-arm64-v8a,x86_64}

if [[ -z ${ANDROID_HOME:-} ]]; then
  if [[ -d $HOME/Library/Android/sdk ]]; then
    ANDROID_HOME=$HOME/Library/Android/sdk
  elif [[ -n ${ANDROID_SDK_ROOT:-} ]]; then
    ANDROID_HOME=$ANDROID_SDK_ROOT
  fi
fi
export ANDROID_HOME
ndk_home=${ANDROID_HOME:-}/ndk/$ndk_version
adb=${ANDROID_HOME:-}/platform-tools/adb

die() {
  printf 'android: %s\n' "$*" >&2
  exit 1
}

need_sdk() {
  [[ -n ${ANDROID_HOME:-} && -d $ANDROID_HOME ]] || die "no Android SDK; set ANDROID_HOME, then run \`mise run android:sdk\`"
  [[ -d $ndk_home ]] || die "no NDK $ndk_version; run \`mise run android:sdk\`"
}

# The task's swift, refused unless it is the open-source build of the pin.
need_swift() {
  local version
  version=$(swift --version 2>/dev/null | grep -m1 -o 'swift-[0-9.]*-RELEASE' || true)
  [[ $version == "swift-${swift_version}-RELEASE" ]] ||
    die "swift on PATH is not the open-source ${swift_version} (run this through \`mise run android:…\`)"
  SUBSTRATE_SWIFT=$(command -v swift)
  export SUBSTRATE_SWIFT
}

# The release key's variables, which reach apksigner alone (through env:,
# never its command line) and never Gradle: Gradle runs the build scripts,
# their plugins and a daemon that outlives the build, any of which could
# read them.
signing_vars=(SUBSTRATE_ANDROID_KEYSTORE SUBSTRATE_ANDROID_KEYSTORE_PASSWORD SUBSTRATE_ANDROID_KEY_ALIAS SUBSTRATE_ANDROID_KEY_PASSWORD)

gradle() {
  local scrub=() var
  for var in "${signing_vars[@]}"; do scrub+=(-u "$var"); done
  (cd apps/gradle && env "${scrub[@]}" ./gradlew --console=plain "-Psubstrate.abis=$abis" "$@")
}

sdkmanager_bin() {
  local candidate
  for candidate in "$ANDROID_HOME"/cmdline-tools/latest/bin/sdkmanager "$ANDROID_HOME"/cmdline-tools/*/bin/sdkmanager; do
    [[ -x $candidate ]] && {
      printf '%s' "$candidate"
      return
    }
  done
  command -v sdkmanager || die "no sdkmanager under $ANDROID_HOME/cmdline-tools; install the Android command-line tools"
}

swift_sdks_dir() {
  local dir
  for dir in "$HOME/Library/org.swift.swiftpm/swift-sdks" "$HOME/.swiftpm/swift-sdks"; do
    [[ -d $dir/$swift_sdk_bundle ]] && {
      printf '%s' "$dir"
      return
    }
  done
}

cmd_sdk() {
  [[ -n ${ANDROID_HOME:-} ]] || die "set ANDROID_HOME to where the Android SDK goes"
  need_swift
  local sdkmanager
  sdkmanager=$(sdkmanager_bin)
  yes | "$sdkmanager" --licenses >/dev/null || true
  "$sdkmanager" --install "${sdk_packages[@]}"

  if [[ -z $(swift_sdks_dir) ]]; then
    swift sdk install "$swift_sdk_url" --checksum "$swift_sdk_checksum"
  fi
  local bundle
  bundle=$(swift_sdks_dir)/$swift_sdk_bundle
  [[ -d $bundle ]] || die "the Swift SDK for Android did not install"
  # The bundle's script links its sysroot to this NDK; run again whenever the
  # link names another one.
  local linked
  linked=$(readlink "$bundle/swift-android/ndk-sysroot/usr/include" 2>/dev/null || true)
  if [[ $linked != "$ndk_home"/* ]]; then
    ANDROID_NDK_HOME=$ndk_home "$bundle/swift-android/scripts/setup-android-sdk.sh"
  fi
  printf 'android: SDK at %s, NDK %s, Swift SDK %s\n' "$ANDROID_HOME" "$ndk_version" "$bundle"
}

# Swift for this host, not for Android: Xcode's on a Mac, whose SDK the
# kit's Apple libraries and their macros need and whose .build the apple:*
# tasks share; the task's open-source one elsewhere.
host_swift() {
  if [[ $(uname -s) == Darwin ]]; then
    xcrun swift "$@"
  else
    swift "$@"
  fi
}

# The kit's configuration first, then each app's (apps/<app>/android/bridge.json),
# which uses the kit's declarations and generates only its own.
bridgegen() {
  host_swift build --package-path apps/kit/Tools/BridgeGen --product bridgegen --quiet
  local bin config
  bin="$(host_swift build --package-path apps/kit/Tools/BridgeGen --show-bin-path)/bridgegen"
  "$bin" "$@" apps/kit/android/bridge.json
  for config in apps/*/android/bridge.json; do
    [[ $config == apps/kit/* ]] || "$bin" "$@" "$config"
  done
}

# The apps with an Android module (apps/<app>/android/build.gradle.kts), by id.
android_apps() {
  local module
  for module in apps/*/android/build.gradle.kts; do
    [[ $module == apps/kit/* ]] || basename "$(dirname "$(dirname "$module")")"
  done
}

# `:<app>:<task>` for every app's module and each task named, one a line.
app_tasks() {
  local app task
  for app in $(android_apps); do
    for task in "$@"; do printf ':%s:%s\n' "$app" "$task"; done
  done
}

# What `build`, `test` and `smoke` ask Gradle for, named once, since the
# verification metadata `lock` writes must cover every artifact they resolve.
build_tasks=(:kit-core:assembleDebug :kit-core:assembleRelease :kit-device:assembleDebug :kit-device:assembleRelease
  :kit-ui:assembleDebug :kit-ui:assembleRelease :bridge-check:assembleDebug :bridge-check:assembleRelease
  :gallery:assembleDebug :gallery:assembleRelease)
# An app's unit tests are its gallery check: every fixture its core wrote,
# drawn by its own composables.
test_tasks=(:kit-core:testDebugUnitTest :kit-device:testDebugUnitTest :kit-ui:testDebugUnitTest)
while IFS= read -r task; do build_tasks+=("$task"); done < <(app_tasks assembleDebug assembleRelease)
while IFS= read -r task; do test_tasks+=("$task"); done < <(app_tasks testDebugUnitTest)
device_test_tasks=(:kit-device:assembleDebugAndroidTest :bridge-check:assembleDebugAndroidTest :gallery:assembleDebugAndroidTest)
# An app's release refuses to build without an https client id and callback
# (build-logic's requireReleaseSignIn); `build` and `lock` build every
# release, so they stamp it with these, which name no real app.
placeholder_stamp=("-Psubstrate.release.clientID=https://example.com/app/client.json"
  "-Psubstrate.release.redirect=https://example.com/app/callback")

cmd_build() {
  need_sdk
  need_swift
  gradle "${build_tasks[@]}" "${placeholder_stamp[@]}"
  # And unstamped, every app's release is refused before any of it is built.
  local app out
  for app in $(android_apps); do
    if out=$(gradle ":$app:preReleaseBuild" 2>&1); then
      die "apps/$app's release built without its sign-in stamp"
    fi
    grep -q 'A release signs in only through its claimed https callback' <<<"$out" ||
      die "apps/$app's unstamped release failed, but not on its sign-in stamp: $out"
    printf 'android: %s release refused without its stamp\n' "$app"
  done
}

cmd_test() {
  need_sdk
  need_swift
  bridgegen --check
  host_swift test --package-path apps/kit/Tools/BridgeGen
  # The bridge's envelope, handles, calls, streams and keyed lists, and the
  # Swift half of the round trip, on this host.
  host_swift test --package-path apps/kit --filter SubstrateBridgeTests
  local kotlin_out=$root/apps/kit/android/kit-core/build/roundtrip/testDebugUnitTest/kotlin.json
  local screens_out=$root/apps/kit/android/kit-core/build/roundtrip/testDebugUnitTest/screens.json
  # kit-core's RoundTripTest and ScreenFixturesTest write Kotlin's encoding
  # of the fixtures there.
  gradle "${test_tasks[@]}"
  [[ -f $kotlin_out ]] || die "the Kotlin round trip wrote nothing to $kotlin_out"
  [[ -f $screens_out ]] || die "the screens' round trip wrote nothing to $screens_out"
  SUBSTRATE_ROUNDTRIP_KOTLIN=$kotlin_out SUBSTRATE_ROUNDTRIP_KOTLIN_SCREENS=$screens_out \
    host_swift test --package-path apps/kit --filter 'KotlinRoundTrip|ScreenFixturesFileTests'
}

cmd_smoke() {
  need_sdk
  need_swift
  [[ $($adb devices | grep -c 'device$') -ge 1 ]] || die "no booted emulator or device; start one (\`emulator -avd <name>\`)"
  gradle :bridge-check:assembleRelease
  local apk=apps/kit/android/bridge-check/build/outputs/apk/release/bridge-check-release.apk
  local app=dev.reamde.substrate.bridgecheck
  "$adb" install -r "$apk" >/dev/null
  "$adb" logcat -c
  "$adb" shell am start -W -n "$app/.MainActivity" >/dev/null
  local waited=0
  until "$adb" logcat -d -s bridge-check:I | grep -q 'done:'; do
    ((waited++ < 300)) || die "the runtime checks did not finish in five minutes"
    sleep 1
  done
  "$adb" logcat -d -s bridge-check:I | sed -n 's/.*bridge-check: //p'
  "$adb" logcat -d -s bridge-check:I | grep -q 'done: PASS' || die "a runtime check failed on the release build"
  for run in 1 2 3; do
    "$adb" shell am force-stop "$app"
    "$adb" logcat -c
    printf 'cold start %s: TotalTime %s ms, ' "$run" "$("$adb" shell am start -W -n "$app/.MainActivity" --ez checks false | sed -n 's/^TotalTime: //p' | tr -d '\r')"
    "$adb" logcat -d -s bridge-check:I | sed -n 's/.*bridge-check: start: .* in \(.*\)/Bridge.start \1/p'
  done
  printf 'release APK: %s bytes\n' "$(wc -c <"$apk" | tr -d ' ')"
  unzip -lv "$apk" | awk '/\.so$/ { printf "  %-48s %10d\n", $8, $1 }' | sort -k2 -n -r
  gradle :kit-device:connectedDebugAndroidTest :bridge-check:connectedDebugAndroidTest :gallery:connectedDebugAndroidTest
}

# One app's release APK, built by its module (`:<app>`, from apps/<app>/android)
# with the release's stamp as Gradle properties, then aligned and signed here
# with the key in the environment, which Gradle never sees (gradle scrubs
# it), and written to SUBSTRATE_RELEASE_APK. .mise/apps.sh calls this
# directly, with an environment it builds (apps-android.sh), and reads
# everything else back from the APK; its header lists what it hands in.
cmd_release_build() {
  local app=${1:-}
  [[ -n $app ]] || die "usage: release:build <app>"
  [[ -n ${SUBSTRATE_RELEASE_APK:-} ]] || die "SUBSTRATE_RELEASE_APK names where the signed APK goes; .mise/apps.sh sets it"
  [[ -d apps/$app/android ]] || die "apps/$app has no android/ module, so there is no APK to build"
  local var
  for var in "${signing_vars[@]}"; do
    [[ -n ${!var:-} ]] || die "set $var; the release key never enters the tree (apps/README.md)"
  done
  need_sdk
  need_swift
  gradle ":$app:assembleRelease" \
    "-Psubstrate.release.version=$SUBSTRATE_RELEASE_VERSION" \
    "-Psubstrate.release.versionCode=$SUBSTRATE_RELEASE_VERSION_CODE" \
    "-Psubstrate.release.channel=$SUBSTRATE_RELEASE_CHANNEL" \
    "-Psubstrate.release.commit=$SUBSTRATE_RELEASE_COMMIT" \
    "-Psubstrate.release.updatesURL=$SUBSTRATE_RELEASE_UPDATES_URL" \
    "-Psubstrate.release.clientID=$SUBSTRATE_RELEASE_CLIENT_ID" \
    "-Psubstrate.release.redirect=$SUBSTRATE_RELEASE_REDIRECT" \
    "-Psubstrate.release.siteHost=$SUBSTRATE_RELEASE_SITE_HOST"
  local built=(apps/"$app"/android/build/outputs/apk/release/*-unsigned.apk)
  [[ ${#built[@]} -eq 1 && -f ${built[0]} ]] || die "apps/$app's release build left ${#built[@]} unsigned APKs, and one is signed"
  local package tools=""
  for package in "${sdk_packages[@]}"; do
    [[ $package == "build-tools;"* ]] && tools=$ANDROID_HOME/build-tools/${package#build-tools;}
  done
  local aligned
  aligned=$(mktemp -d)/aligned.apk
  "$tools/zipalign" -f -p 4 "${built[0]}" "$aligned"
  mkdir -p "$(dirname "$SUBSTRATE_RELEASE_APK")"
  "$tools/apksigner" sign --ks "$SUBSTRATE_ANDROID_KEYSTORE" --ks-pass env:SUBSTRATE_ANDROID_KEYSTORE_PASSWORD \
    --ks-key-alias "$SUBSTRATE_ANDROID_KEY_ALIAS" --key-pass env:SUBSTRATE_ANDROID_KEY_PASSWORD \
    --out "$SUBSTRATE_RELEASE_APK" "$aligned"
  rm -rf "$(dirname "$aligned")"
  printf 'android: %s\n' "$SUBSTRATE_RELEASE_APK"
}

# Every fixture of apps/kit/android/fixtures/screens.json drawn by kit-ui's
# composables under Robolectric, each kept as <fixture>.png in the folder
# named (apps:gallery's Android half).
cmd_gallery() {
  local folder=${1:-}
  [[ -n $folder ]] || die "usage: gallery <folder>"
  need_sdk
  need_swift
  mkdir -p "$folder"
  gradle :kit-ui:testDebugUnitTest --tests '*KitGalleryTest' "-Psubstrate.gallery=$folder"
  local app
  for app in $(android_apps); do
    mkdir -p "$folder/$app"
    gradle ":$app:testDebugUnitTest" "-Psubstrate.gallery=$folder/$app"
  done
}

# The locks pin every version; the verification metadata
# (apps/gradle/gradle/verification-metadata.xml) pins every artifact's
# sha256, and Gradle refuses, in its default strict mode, any artifact or
# metadata file the file does not list. Written fresh from what the build,
# the tests and the device tests resolve here, so an entry nothing uses is
# dropped, then once more for aapt2, which the Android plugin fetches for
# the host it runs on: CI builds on Linux, a laptop on macOS, and the file
# must hold both. One Gradle run per set, each adding to the file, because
# the whole set in one run outgrows the daemon's heap. The runs share an
# empty Gradle home, because Gradle verifies what it fetches and CI's runner
# starts with nothing: a cold resolution fetches metadata a warm cache never
# reads again (kotlinx-coroutines-bom 1.8.0's POM, under build-logic's
# classpath), so a file written from a warm home passes here and fails there.
cmd_lock() {
  need_sdk
  need_swift
  local apps=()
  while IFS= read -r task; do apps+=("$task"); done < <(app_tasks dependencies)
  gradle :dependencies :kit-core:dependencies :kit-device:dependencies :kit-ui:dependencies :bridge-check:dependencies :gallery:dependencies \
    "${apps[@]}" --write-locks >/dev/null
  (cd apps/gradle && ./gradlew --console=plain -p build-logic dependencies --write-locks >/dev/null)
  local metadata=apps/gradle/gradle/verification-metadata.xml
  # Stopped and removed on any exit: a failed run leaves the home's daemon
  # running and its downloads on disk.
  lock_home=$(mktemp -d)
  export GRADLE_USER_HOME=$lock_home
  trap '(cd apps/gradle && ./gradlew --stop >/dev/null 2>&1) || true; rm -rf "$lock_home"' EXIT
  rm -f "$metadata"
  gradle --no-configuration-cache --write-verification-metadata sha256 "${build_tasks[@]}" "${placeholder_stamp[@]}" >/dev/null
  gradle --no-configuration-cache --write-verification-metadata sha256 "${test_tasks[@]}" >/dev/null
  gradle --no-configuration-cache --write-verification-metadata sha256 "${device_test_tasks[@]}" >/dev/null
  # The root's ktfmt, which `fmt:kotlin` runs (.mise/appsfmt.sh).
  gradle --no-configuration-cache --write-verification-metadata sha256 :ktfmt --args=--version >/dev/null
  local aapt2
  aapt2=$(sed -n 's|.*<component group="com.android.tools.build" name="aapt2" version="\([^"]*\)">.*|\1|p' "$metadata" | head -1)
  [[ -n $aapt2 ]] || die "the build resolved no aapt2, so its other hosts' artifacts cannot be added to $metadata"
  local init
  init=$(mktemp -d)/aapt2-hosts.gradle.kts
  cat >"$init" <<'KTS'
gradle.rootProject {
    tasks.register("resolveAapt2Hosts") {
        val version = providers.gradleProperty("substrate.aapt2").get()
        val hosts = listOf("linux", "osx", "windows").map {
            project.dependencies.create("com.android.tools.build:aapt2:$version:$it")
        }
        val files = project.configurations.detachedConfiguration(*hosts.toTypedArray())
        doLast { files.resolve() }
    }
}
KTS
  gradle --no-configuration-cache --write-verification-metadata sha256 -I "$init" "-Psubstrate.aapt2=$aapt2" \
    resolveAapt2Hosts >/dev/null
  rm -rf "$(dirname "$init")"
  grep -q '<verify-metadata>true</verify-metadata>' "$metadata" || die "$metadata does not verify metadata files"
  printf 'android: %s written; review its diff like a lock file\n' "$metadata"
}

case ${1:-} in
sdk) cmd_sdk ;;
generate) bridgegen ;;
generate:check) bridgegen --check ;;
build) cmd_build ;;
test) cmd_test ;;
smoke) cmd_smoke ;;
gallery) cmd_gallery "${2:-}" ;;
lock) cmd_lock ;;
release:build) cmd_release_build "${2:-}" ;;
*) die "unknown subcommand ${1:-}; see the header of .mise/android.sh" ;;
esac
