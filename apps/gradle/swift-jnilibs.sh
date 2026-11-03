#!/usr/bin/env bash
# Builds one SwiftPM dynamic library product for one Android ABI and puts it
# in <jniLibs>/<abi> with every shared library it loads that a device does
# not ship: the Swift runtime and Foundation from the Swift SDK for Android,
# and the NDK's libc++_shared.so. What a device ships (libc, libm, libdl,
# liblog, libz, libandroid) is never copied. The packages' resource bundles
# go to <assets>/swift-resources, which kit-core copies out at start and
# hands Swift as the resource folder (SwiftPM's Bundle.module looks beside
# /system/bin/app_process64 on Android, docs/plans/app-kit.md §3.2).
#
#   swift-jnilibs.sh <abi> <jniLibs> <assets>
#
# The SwiftConvention task (build-logic/) sets the rest:
#   SWIFT_PACKAGE, SWIFT_PRODUCT        the package's folder and the product
#   SWIFT_CONFIGURATION                 debug or release
#   SWIFT_MODE                          dynamic, or static: the Swift runtime
#                                       and Foundation linked into the product
#   SWIFT_VERSION, SWIFT_MIN_SDK        the catalog's swift and minSdk pins
#   SWIFT_SCRATCH, ANDROID_NDK_HOME
#
# The toolchain is SUBSTRATE_SWIFT, else mise's swift@SWIFT_VERSION: the
# open-source build the Swift SDK for Android was made for, never Xcode's,
# and refused unless its version is exactly the pin.
set -euo pipefail

abi=$1
jnilibs=$2
assets=$3
: "${SWIFT_PACKAGE:?}" "${SWIFT_PRODUCT:?}" "${SWIFT_VERSION:?}" "${SWIFT_MIN_SDK:?}" "${ANDROID_NDK_HOME:?}"
configuration=${SWIFT_CONFIGURATION:-release}
mode=${SWIFT_MODE:-dynamic}

case $abi in
arm64-v8a) arch=aarch64 ;;
x86_64) arch=x86_64 ;;
*)
  echo "swift-jnilibs.sh: unsupported ABI $abi" >&2
  exit 1
  ;;
esac
triple=$arch-unknown-linux-android$SWIFT_MIN_SDK

swift=${SUBSTRATE_SWIFT:-}
if [[ -z $swift ]]; then
  swift="$(mise where "swift@$SWIFT_VERSION" 2>/dev/null)/bin/swift" ||
    swift=""
fi
if [[ ! -x $swift ]]; then
  echo "swift-jnilibs.sh: no Swift $SWIFT_VERSION toolchain; run \`mise run android:sdk\`" >&2
  exit 1
fi
version=$("$swift" --version 2>/dev/null | head -1)
if [[ $version != *"(swift-$SWIFT_VERSION-RELEASE)"* ]]; then
  echo "swift-jnilibs.sh: $swift is \"$version\", and the Swift SDK for Android needs the open-source swift-$SWIFT_VERSION-RELEASE" >&2
  exit 1
fi

bundle=swift-$SWIFT_VERSION-RELEASE_android.artifactbundle
sdk=""
for root in "${SWIFT_SDKS_PATH:-}" "$HOME/Library/org.swift.swiftpm/swift-sdks" "$HOME/.swiftpm/swift-sdks"; do
  if [[ -n $root && -d $root/$bundle/swift-android ]]; then
    sdk=$root/$bundle/swift-android
    break
  fi
done
if [[ -z $sdk ]]; then
  echo "swift-jnilibs.sh: the Swift SDK for Android ($bundle) is not installed; run \`mise run android:sdk\`" >&2
  exit 1
fi
runtime=$sdk/swift-resources/usr/lib/swift-$arch/android
prebuilt=$(echo "$ANDROID_NDK_HOME"/toolchains/llvm/prebuilt/*)
readelf=$prebuilt/bin/llvm-readelf
libcxx=$prebuilt/sysroot/usr/lib/$arch-linux-android/libc++_shared.so

# 16 KB pages: Android 15 devices may use them, and a library aligned to
# 4 KB does not load there.
flags=(--package-path "$SWIFT_PACKAGE" --product "$SWIFT_PRODUCT" --swift-sdk "$triple" -c "$configuration"
  --scratch-path "${SWIFT_SCRATCH:-$SWIFT_PACKAGE/.build/android}"
  -Xlinker -z -Xlinker max-page-size=16384)
if [[ $mode == static ]]; then
  flags+=(--static-swift-stdlib)
fi
"$swift" build "${flags[@]}"
bin=$("$swift" build "${flags[@]}" --show-bin-path)

rm -rf "${jnilibs:?}/$abi"
mkdir -p "$jnilibs/$abi"
queue=("$bin/lib$SWIFT_PRODUCT.so")
seen=" "
while ((${#queue[@]})); do
  lib=${queue[0]}
  queue=("${queue[@]:1}")
  cp "$lib" "$jnilibs/$abi/"
  for needed in $("$readelf" -d "$lib" | sed -n 's/.*(NEEDED).*\[\(.*\)\]/\1/p'); do
    [[ $seen == *" $needed "* ]] && continue
    seen+="$needed "
    if [[ -f $runtime/$needed ]]; then
      queue+=("$runtime/$needed")
    elif [[ $needed == libc++_shared.so ]]; then
      queue+=("$libcxx")
    fi
  done
done

# The resources are the same for every ABI; the last one built writes them.
rm -rf "${assets:?}/swift-resources"
mkdir -p "$assets/swift-resources"
shopt -s nullglob
# SwiftPM names a target's bundle <Package>_<Target>.resources, or .bundle
# where its build system is swift-build.
for resources in "$bin"/*.resources "$bin"/*.bundle; do
  cp -R "$resources" "$assets/swift-resources/"
done
ls -l "$jnilibs/$abi"
