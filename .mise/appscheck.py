"""The apps' layering, held: `mise run lint:apps`.

    python3 .mise/appscheck.py

The apps under apps/ share a Swift package, apps/kit, whose libraries each
depend only on the ones before them, and every app is one folder that
imports no other app (docs/plans/app-kit.md, §5.3, §6 and §7.1). Nothing a
compiler runs holds either: a kit library that reaches up the list, or an
app's core that imports UIKit, builds on a Mac and fails only later, on
Linux or on Android, or in the next app that tries to reuse it. So each rule
below reads the files themselves, with no Swift toolchain, which is what lets
it run in the Linux `lint` job.

Each rule is one `# --- <what it holds> ---` section below, with its reason,
so `grep '^# ---' .mise/appscheck.py` lists them. apps/README.md points here
rather than repeating them.

Every rule runs and reports, so one pass names everything wrong rather than
the first thing wrong. Stdlib only, and no YAML parser: it runs on whatever
python3 the machine has, and the YAML it reads (xcodegen's) is held to a
shape by yamlfmt.
"""

import json
import math
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
APPS = os.path.join(ROOT, "apps")
KIT = os.path.join(APPS, "kit")

# The folders under apps/ that are not apps: the kit, the Xcode project and
# the Gradle root.
NOT_APPS = {"kit", "xcode", "gradle"}

# Build output a laptop leaves in the tree, which no rule reads.
SKIP_DIRS = {".build", "build", ".swiftpm", "DerivedData", "node_modules"}

failures = []


def flag(message):
    failures.append(message)


def rel(path):
    return os.path.relpath(path, ROOT)


def walk(top):
    for dirpath, dirnames, filenames in os.walk(top):
        dirnames[:] = sorted(d for d in dirnames if d not in SKIP_DIRS)
        for name in sorted(filenames):
            yield os.path.join(dirpath, name)


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def bracketed(text, start):
    """The text from `start` (an opening bracket) to its matching close."""
    pairs = {"(": ")", "[": "]"}
    stack = []
    in_string = False
    i = start
    while i < len(text):
        c = text[i]
        if in_string:
            if c == "\\":
                i += 2
                continue
            if c == '"':
                in_string = False
        elif c == '"':
            in_string = True
        elif c in pairs:
            stack.append(pairs[c])
        elif stack and c == stack[-1]:
            stack.pop()
            if not stack:
                return text[start : i + 1]
        i += 1
    return text[start:]


def strip_comments(swift):
    """Swift with its line comments cut, a `//` inside a string kept."""
    out = []
    for line in swift.splitlines():
        in_string = False
        i = 0
        while i < len(line):
            c = line[i]
            if c == "\\" and in_string:
                i += 2
                continue
            if c == '"':
                in_string = not in_string
            elif not in_string and line.startswith("//", i):
                line = line[:i]
                break
            i += 1
        out.append(line)
    return "\n".join(out)


class Package:
    """A Package.swift, read for what the rules need: its targets, each
    one's dependencies, and the packages it depends on."""

    def __init__(self, path):
        self.path = path
        self.dir = os.path.dirname(path)
        text = strip_comments(read(path))
        self.targets = {}
        for m in re.finditer(r"\.(target|testTarget|executableTarget)\(", text):
            call = bracketed(text, m.end() - 1)
            name = re.search(r'name:\s*"([^"]+)"', call)
            if not name:
                continue
            deps = []
            d = re.search(r"dependencies:\s*\[", call)
            if d:
                array = bracketed(call, d.end() - 1)
                for p in re.finditer(r'\.product\(\s*name:\s*"([^"]+)",\s*package:\s*"([^"]+)"', array):
                    deps.append((p.group(1), p.group(2)))
                rest = re.sub(r"\.product\([^)]*\)", "", array)
                for s in re.finditer(r'"([^"]+)"', rest):
                    deps.append((s.group(1), None))
            # A dependency list built outside the call (`["X"] + portableKit`)
            # is read where the name it adds is declared.
            for ref in re.findall(r"\+\s*([A-Za-z_]\w*)", call):
                decl = re.search(r"let\s+" + ref + r"\b[^=]*=\s*\[([^\]]*)\]", text)
                if decl:
                    package = re.search(r'package:\s*"([^"]+)"', text[decl.end() :])
                    for s in re.findall(r'"([^"]+)"', decl.group(1)):
                        deps.append((s, package.group(1) if package else None))
            path = re.search(r'path:\s*"([^"]+)"', call)
            self.targets[name.group(1)] = {
                "kind": m.group(1),
                "deps": deps,
                "path": path.group(1) if path else None,
            }
        self.urls = re.findall(r'\.package\(\s*url:\s*"([^"]+)"', text)
        self.paths = re.findall(r'\.package\(\s*path:\s*"([^"]+)"', text)

    def source_dir(self, target):
        info = self.targets[target]
        if info["path"]:
            return os.path.join(self.dir, info["path"])
        top = "Tests" if info["kind"] == "testTarget" else "Sources"
        return os.path.join(self.dir, top, target)


def packages_under(top):
    return [Package(p) for p in walk(top) if os.path.basename(p) == "Package.swift"]


def app_folders():
    out = []
    for name in sorted(os.listdir(APPS)):
        path = os.path.join(APPS, name)
        if os.path.isdir(path) and name not in NOT_APPS and not name.startswith("."):
            out.append(name)
    return out


# An import's attributes and access level come before the keyword in any
# order Swift 6 takes (`@preconcurrency`, `@_exported public`, `@_spi(X)
# internal`), and its kind (`struct`, `func`) after it.
IMPORT = re.compile(
    r"(?:(?:@\w+(?:\([^)]*\))?|public|package|internal|fileprivate|private)\s+)*"
    r"import\s+(?:(?:struct|class|enum|protocol|func|var|let|typealias)\s+)?([A-Za-z_]\w*)"
)


def imports(path, text=None):
    """Each import in a Swift file, with the `canImport` conditions it sits
    under: (line number, module, conditions)."""
    out = []
    conditions = []
    for number, line in enumerate((read(path) if text is None else text).splitlines(), 1):
        stripped = line.strip()
        m = re.match(r"#if\s+(.*)", stripped)
        if m:
            conditions.append(m.group(1))
            continue
        if stripped.startswith("#elseif"):
            if conditions:
                conditions[-1] = stripped[len("#elseif") :].strip()
            continue
        if stripped.startswith("#else"):
            if conditions:
                conditions[-1] = "!(" + conditions[-1] + ")"
            continue
        if stripped.startswith("#endif"):
            if conditions:
                conditions.pop()
            continue
        for statement in stripped.split(";"):
            m = IMPORT.match(statement.strip())
            if m:
                out.append((number, m.group(1), list(conditions)))
    return out


def swift_files(top):
    return [p for p in walk(top) if p.endswith(".swift")]


kit = Package(os.path.join(KIT, "Package.swift"))
apps = app_folders()


# --- the import reader reads every spelling of an import -----------------
#
# Every rule about imports below sees only what `imports` reads, so a
# spelling it misses is an import no rule holds: `public import UIKit` in an
# app's core passed until Swift 6's access-level imports were read. The
# reader runs here over each spelling first, those that are imports and
# those that only look like one, and a miss is a failure of its own.

IMPORT_SPELLINGS = """\
import UIKit
public import UIKit
internal import SwiftUI
package import AppKit
fileprivate import WatchKit
private import HealthKit
@preconcurrency import Combine
@preconcurrency public import CoreData
@_exported public import MapKit
@_exported import SubstrateCore
@_spi(Internal) internal import StoreKit
@_implementationOnly import Security
@testable import WidgetKit
import struct CoreLocation.CLLocation
public import func Darwin.sqrt
import Foundation; import ActivityKit
#if os(iOS)
    import EventKit
#elseif os(macOS)
    public import Contacts
#else
    @preconcurrency internal import Photos
#endif
// import NotAnImport
let importer = "import NotAnImport"
func important() {}
imported(UIKit)
"""
IMPORT_EXPECTED = [
    (1, "UIKit", []),
    (2, "UIKit", []),
    (3, "SwiftUI", []),
    (4, "AppKit", []),
    (5, "WatchKit", []),
    (6, "HealthKit", []),
    (7, "Combine", []),
    (8, "CoreData", []),
    (9, "MapKit", []),
    (10, "SubstrateCore", []),
    (11, "StoreKit", []),
    (12, "Security", []),
    (13, "WidgetKit", []),
    (14, "CoreLocation", []),
    (15, "Darwin", []),
    (16, "Foundation", []),
    (16, "ActivityKit", []),
    (18, "EventKit", ["os(iOS)"]),
    (20, "Contacts", ["os(macOS)"]),
    (22, "Photos", ["!(os(macOS))"]),
]
read_back = imports("<the spellings>", IMPORT_SPELLINGS)
if read_back != IMPORT_EXPECTED:
    for want in IMPORT_EXPECTED:
        if want not in read_back:
            flag(f"the import reader misses line {want[0]} of its spellings, which imports {want[1]}")
    for got in read_back:
        if got not in IMPORT_EXPECTED:
            flag(f"the import reader reads line {got[0]} of its spellings as importing {got[1]}, which it does not")


# --- every package folder name under apps/ is unique ---------------------
#
# SwiftPM names a local package by its folder, and the one Xcode project
# resolves every app's packages together, so two apps' packages in folders
# of one name (`core/`) collide in a way the project reports as a missing
# product. An app's core is named for the app (WorkoutsKit/, TasksKit/).

seen = {}
for package in packages_under(APPS):
    name = os.path.basename(package.dir)
    if name in seen:
        flag(f"two Swift packages are in folders named {name}: {rel(seen[name])} and {rel(package.dir)}")
    else:
        seen[name] = package.dir


# --- every kit library depends only on the ones before it ----------------
#
# The kit's libraries are one list, each allowed only those above it, so a
# portable library never reaches an Apple-only one, and an app that takes one
# library takes only what is under it. The order is §5.3's: a library added
# to the package is added here too, in its place, or this refuses it.
# SubstrateResolv is the C half of SubstrateDeviceApple, SubstrateJNI the C
# half of SubstrateBridge; SubstrateKit is the umbrella every app imports
# today, over every library but the test fakes. The Android rows come last
# and take no Apple row: they build where no Apple framework is.

KIT_ORDER = [
    "SubstrateCore",
    "SubstrateDevice",
    "SubstrateAPI",
    "SubstrateAuth",
    "SubstrateRuntime",
    "SubstrateLocal",
    "SubstrateScreens",
    "SubstrateTesting",
    "SubstrateDeviceApple",
    "SubstrateUI",
    "SubstrateBridge",
    "SubstrateBridgeCheck",
]
KIT_HELPERS = {"SubstrateResolv": {"SubstrateDeviceApple"}, "SubstrateJNI": {"SubstrateBridge"}}
APPLE_ROWS = {"SubstrateDeviceApple", "SubstrateUI"}
ANDROID_ROWS = {"SubstrateBridge", "SubstrateBridgeCheck"}
UMBRELLA = "SubstrateKit"
# The third-party products a kit library may take, by library.
KIT_EXTERNAL = {("SubstrateAuth", "Crypto", "swift-crypto")}

for target, info in sorted(kit.targets.items()):
    if info["kind"] == "testTarget":
        continue
    if target in KIT_HELPERS or target == UMBRELLA:
        continue
    if target not in KIT_ORDER:
        flag(f"apps/kit's target {target} is not in the libraries' order; add it to KIT_ORDER in {rel(__file__)} at its place")
for target, info in sorted(kit.targets.items()):
    if info["kind"] == "testTarget":
        continue
    for dep, package in info["deps"]:
        if package is not None:
            if (target, dep, package) not in KIT_EXTERNAL:
                flag(f"apps/kit's {target} depends on {package}'s {dep}, which no rule allows it")
            continue
        if dep in KIT_HELPERS:
            if target not in KIT_HELPERS[dep]:
                flag(f"apps/kit's {target} depends on {dep}, which only {', '.join(sorted(KIT_HELPERS[dep]))} may")
            continue
        if target == UMBRELLA:
            if dep == "SubstrateTesting":
                flag(f"apps/kit's {UMBRELLA} exports SubstrateTesting, which only tests take")
            continue
        if target not in KIT_ORDER or dep not in KIT_ORDER:
            continue
        if KIT_ORDER.index(dep) >= KIT_ORDER.index(target):
            flag(f"apps/kit's {target} depends on {dep}, which comes after it: a library depends only on the ones before it")
        if dep == "SubstrateTesting":
            flag(f"apps/kit's {target} depends on SubstrateTesting, which only tests take")
        if target in ANDROID_ROWS and dep in APPLE_ROWS:
            flag(f"apps/kit's {target} depends on {dep}, an Apple library, and it builds for Android")


# --- the portable code imports only what Linux and Android have ----------
#
# The kit's portable libraries and every app's core build for Linux and
# Android, where Apple's frameworks are not. They may import Foundation
# (FoundationNetworking under `#if canImport(FoundationNetworking)`, where
# URLSession lives off Apple systems), Observation, Dispatch, the kit's
# libraries and their own package's targets, and SHA-256 as CryptoKit or
# swift-crypto's Crypto, either under `#if canImport(CryptoKit)`. The Linux
# job catches what this misses, later and slower.

PORTABLE_KIT = ["SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateAuth", "SubstrateRuntime", "SubstrateLocal", "SubstrateScreens", "SubstrateTesting"]
ALLOWED = {"Foundation", "Observation", "Dispatch"}
GUARDED = {
    "FoundationNetworking": "canImport(FoundationNetworking)",
    "CryptoKit": "canImport(CryptoKit)",
    "Crypto": "canImport(CryptoKit)",
}
KIT_MODULES = set(KIT_ORDER)


def check_portable(directory, own):
    for path in swift_files(directory):
        for number, module, conditions in imports(path):
            if module in ALLOWED or module in KIT_MODULES - {"SubstrateDeviceApple", "SubstrateUI"} or module in own:
                continue
            guard = GUARDED.get(module)
            if guard and any(guard in c for c in conditions):
                continue
            where = f" outside `#if {guard}`" if guard else ""
            flag(f"{rel(path)}:{number} imports {module}{where}, which portable code may not")


for target in PORTABLE_KIT:
    if target in kit.targets:
        check_portable(kit.source_dir(target), set())

# An app's core is its Swift package named for it (WorkoutsKit/, ChatKit/):
# its libraries are held as the kit's portable ones are.
for package in packages_under(APPS):
    if os.path.relpath(package.dir, APPS).split(os.sep)[0] == "kit" or not os.path.basename(package.dir).endswith("Kit"):
        continue
    for target, info in sorted(package.targets.items()):
        if info["kind"] == "target":
            check_portable(package.source_dir(target), set(package.targets))


# --- the Android bridge imports only what Android has --------------------
#
# SubstrateBridge and SubstrateBridgeCheck build into the library an Android
# app loads, and on every host the kit's tests run on. They import what the
# portable code may, SubstrateJNI (the NDK's headers, empty off Android),
# and the NDK's `Android` module only under `#if os(Android)`, where the
# JNI half of the bridge lives.

ANDROID_GUARDED = {"Android": "os(Android)"}

for target in sorted(ANDROID_ROWS):
    if target not in kit.targets:
        continue
    for path in swift_files(kit.source_dir(target)):
        for number, module, conditions in imports(path):
            if module in ALLOWED or module in KIT_MODULES - APPLE_ROWS or module == "SubstrateJNI":
                continue
            guard = GUARDED.get(module) or ANDROID_GUARDED.get(module)
            if guard and any(guard in c for c in conditions):
                continue
            where = f" outside `#if {guard}`" if guard else ""
            flag(f"{rel(path)}:{number} imports {module}{where}, which the Android bridge may not")


# --- only kit-device and an app's android/ touch an Android service ------
#
# A device service is a protocol in SubstrateDevice and an implementation
# per system (§5.4); on Android the implementations are kit-device's, and an
# app's own device code lives in its android/ folder. Kotlin anywhere else
# in the kit that reaches a system service (`getSystemService`, the network,
# the Keystore, location, notifications, background work) is device code in
# the wrong module, and the next app would not find it there.

SERVICE_KOTLIN = re.compile(
    r"getSystemService\(|^import\s+(android\.(net|security|location|hardware|bluetooth|telephony|app\.(NotificationManager|job))\b|androidx\.work\b)",
    re.M,
)
for path in walk(os.path.join(KIT, "android")):
    if not path.endswith(".kt"):
        continue
    if os.path.relpath(path, os.path.join(KIT, "android")).split(os.sep)[0] == "kit-device":
        continue
    for number, line in enumerate(read(path).splitlines(), 1):
        if SERVICE_KOTLIN.search(line):
            flag(f"{rel(path)}:{number} touches an Android service, which only kit-device and an app's android/ may")


# --- the kit's Gradle modules depend only on the ones before them --------
#
# kit-core (the Swift library and the bridge), then kit-device (the device
# services), then kit-ui (the composables): the same order as the Swift
# rows, so the bridge never reaches the services that answer it. Every
# module apps/gradle/settings.gradle.kts includes exists.

GRADLE_ORDER = ["kit-core", "kit-device", "kit-ui"]
settings = os.path.join(APPS, "gradle", "settings.gradle.kts")
if os.path.exists(settings):
    for name, folder in re.findall(r'module\(\s*":([\w-]+)",\s*"([^"]+)"\s*\)', read(settings)):
        build = os.path.join(APPS, "gradle", folder, "build.gradle.kts")
        if not os.path.exists(build):
            flag(f"apps/gradle/settings.gradle.kts includes :{name} from {folder}, which has no build.gradle.kts")
            continue
        if name not in GRADLE_ORDER:
            continue
        for dep in re.findall(r'project\(\s*":([\w-]+)"\s*\)', read(build)):
            if dep in GRADLE_ORDER and GRADLE_ORDER.index(dep) >= GRADLE_ORDER.index(name):
                flag(f"{rel(build)} depends on :{dep}, which comes after :{name}")
for app in apps:
    for package in packages_under(os.path.join(APPS, app)):
        own = set(package.targets)
        for target, info in package.targets.items():
            if info["kind"] != "testTarget":
                check_portable(package.source_dir(target), own)


# --- no app imports another app -------------------------------------------
#
# An app is one folder (§6): its code, scripts and docs move, build and
# release with it. A module of another app's, or a package in another app's
# folder, would tie two apps' releases together and is the kit's to hold
# instead.


def xcode_targets(path):
    """The target names an xcodegen targets.yml declares."""
    out = set()
    in_targets = False
    for line in read(path).splitlines():
        if re.match(r"\S", line):
            in_targets = line.rstrip() == "targets:"
            continue
        m = re.match(r"  ([A-Za-z0-9_]+):\s*$", line)
        if in_targets and m:
            out.add(m.group(1))
    return out


owner = {}
for app in apps:
    folder = os.path.join(APPS, app)
    for package in packages_under(folder):
        for target in package.targets:
            owner[target] = app
        owner[os.path.basename(package.dir)] = app
    targets_yml = os.path.join(folder, "apple", "targets.yml")
    if os.path.exists(targets_yml):
        for target in xcode_targets(targets_yml):
            owner[target] = app

for app in apps:
    folder = os.path.join(APPS, app)
    for path in swift_files(folder):
        for number, module, _ in imports(path):
            if owner.get(module, app) != app:
                flag(f"{rel(path)}:{number} imports {module}, which is {owner[module]}'s")
    for package in packages_under(folder):
        for dep in package.paths:
            target = os.path.normpath(os.path.join(package.dir, dep))
            inside = os.path.relpath(target, APPS).split(os.sep)[0]
            if inside not in NOT_APPS and inside != app:
                flag(f"{rel(package.path)} depends on {rel(target)}, which is {inside}'s")
    targets_yml = os.path.join(folder, "apple", "targets.yml")
    if os.path.exists(targets_yml):
        for number, line in enumerate(read(targets_yml).splitlines(), 1):
            m = re.match(r"\s*-\s*(package|target):\s*([A-Za-z0-9_]+)", line)
            if m and owner.get(m.group(2), app) != app:
                flag(f"{rel(targets_yml)}:{number} uses {m.group(2)}, which is {owner[m.group(2)]}'s")


# --- every app folder names its app in a readable app.json ---------------
#
# app.json names an app once (§6, apps/README.md): the server's reach test,
# each app's manifest test and the release (.mise/apps.sh) read it, and a
# reader that finds a field missing fails far from the file. So every app
# folder has one, it parses, and every field is there with its shape. What
# an app publishes and what it asks for are two lists: Workouts asks for
# health, which another authority publishes, and a request names a version
# floor a published package does not have. `apple` is there for an app that
# names an Apple platform and `android` for one that names Android, since
# the release reads each platform's ids from them; the scheme the release
# archives is a target of the app's own apple/targets.yml; and two apps
# never share a feed folder, where one's releases would replace the other's.
# `accent` is optional here and held below, with the asset catalogs.

FIELDS = {
    "id": str,
    "name": str,
    "platforms": list,
    "publishes": list,
    "asks": list,
    "scopes": list,
    "icon": str,
    "feed": str,
}
# By platform, the object an app that names it carries.
PLATFORM_FIELDS = {
    "apple": {"bundleID": str, "scheme": str, "extensions": list, "clientIDKey": str, "redirectKey": str},
    "android": {"applicationID": str},
}
PLATFORMS = {"ios": "apple", "macos": "apple", "android": "android"}
# An Android application id: two or more dot-separated Java identifiers.
APPLICATION_ID = re.compile(r"[a-zA-Z][a-zA-Z0-9_]*(?:\.[a-zA-Z][a-zA-Z0-9_]*)+")
feeds = {}
# A package id: an authority with a dot, a slash, one lowercase word.
PACKAGE_ID = re.compile(r"[a-z0-9-]+(?:\.[a-z0-9-]+)+/[a-z][a-z0-9]*")

for app in apps:
    path = os.path.join(APPS, app, "app.json")
    if not os.path.exists(path):
        flag(f"apps/{app} has no app.json")
        continue
    try:
        doc = json.loads(read(path))
    except ValueError as e:
        flag(f"{rel(path)} is not JSON: {e}")
        continue
    if not isinstance(doc, dict):
        flag(f"{rel(path)} is not an object")
        continue
    for key, kind in FIELDS.items():
        if not isinstance(doc.get(key), kind):
            flag(f"{rel(path)}: {key} is missing or not a {kind.__name__}")
    for key in sorted(set(doc) - set(FIELDS) - set(PLATFORM_FIELDS) - {"accent"}):
        flag(f"{rel(path)}: {key} is no app.json field (apps/README.md lists them)")
    if doc.get("id") != app:
        flag(f"{rel(path)}: id is {doc.get('id')!r}, and the app's folder is {app}")
    if isinstance(doc.get("name"), str) and not doc["name"].strip():
        flag(f"{rel(path)}: name is empty")
    platforms = doc.get("platforms") if isinstance(doc.get("platforms"), list) else []
    if not platforms or set(platforms) - set(PLATFORMS):
        flag(f"{rel(path)}: platforms is {platforms}, and each is one of {sorted(PLATFORMS)}")
    named = {PLATFORMS[p] for p in platforms if p in PLATFORMS}
    for block, fields in PLATFORM_FIELDS.items():
        if block not in named:
            if block in doc:
                flag(f"{rel(path)}: {block} is there, and platforms names no platform it is for")
            continue
        value = doc.get(block) if isinstance(doc.get(block), dict) else {}
        if not isinstance(doc.get(block), dict):
            flag(f"{rel(path)}: {block} is missing or not a dict, and platforms names {sorted(p for p in platforms if PLATFORMS.get(p) == block)}")
        for key, kind in fields.items():
            if not isinstance(value.get(key), kind):
                flag(f"{rel(path)}: {block}.{key} is missing or not a {kind.__name__}")
        for key in sorted(set(value) - set(fields)):
            flag(f"{rel(path)}: {block}.{key} is no app.json field (apps/README.md lists them)")
    android = doc.get("android") if isinstance(doc.get("android"), dict) else {}
    if isinstance(android.get("applicationID"), str) and not APPLICATION_ID.fullmatch(android["applicationID"]):
        flag(f"{rel(path)}: android.applicationID {android['applicationID']!r} is no Android application id")
    scheme = (doc.get("apple") or {}).get("scheme") if isinstance(doc.get("apple"), dict) else None
    targets_yml = os.path.join(APPS, app, "apple", "targets.yml")
    if isinstance(scheme, str) and os.path.exists(targets_yml) and not re.search(rf"^  {re.escape(scheme)}:\s*$", read(targets_yml), re.M):
        flag(f"{rel(path)}: apple.scheme {scheme} is no target in {rel(targets_yml)}")
    feed = doc.get("feed")
    if isinstance(feed, str):
        if feed in feeds:
            flag(f"{rel(path)}: feed {feed!r} is {feeds[feed]}'s too, and two apps' releases would replace each other's")
        feeds.setdefault(feed, app)
    scopes = doc.get("scopes") if isinstance(doc.get("scopes"), list) else []
    if not all(isinstance(v, str) and v for v in scopes):
        flag(f"{rel(path)}: scopes holds something other than names")
    publishes = doc.get("publishes") if isinstance(doc.get("publishes"), list) else []
    for package in publishes:
        if not isinstance(package, str) or not PACKAGE_ID.fullmatch(package):
            flag(f"{rel(path)}: publishes holds {package!r}, which is no package id")
    if publishes and not os.path.isdir(os.path.join(APPS, app, "package")):
        flag(f"{rel(path)}: publishes {len(publishes)} packages, and apps/{app} has no package/ folder to publish them from")
    asks = doc.get("asks") if isinstance(doc.get("asks"), list) else []
    for ask in asks:
        if (
            not isinstance(ask, dict)
            or set(ask) != {"package", "atLeast"}
            or not isinstance(ask.get("package"), str)
            or not PACKAGE_ID.fullmatch(ask["package"])
            or type(ask.get("atLeast")) is not int
            or ask["atLeast"] < 1
        ):
            flag(f"{rel(path)}: asks holds {ask!r}, and each is {{\"package\": <id>, \"atLeast\": <version from 1>}}")
    asked = [a.get("package") for a in asks if isinstance(a, dict)]
    if len(asked) != len(set(asked)):
        flag(f"{rel(path)}: asks names a package twice")
    if not doc.get("scopes"):
        flag(f"{rel(path)}: scopes is empty, and every app asks for something")
    # An app whose UI is not written yet (its package first) has no icon to
    # point at; the icon is held once a UI folder exists.
    icon = doc.get("icon")
    has_ui = any(os.path.isdir(os.path.join(APPS, app, ui)) for ui in ("apple", "android"))
    if isinstance(icon, str) and has_ui and not os.path.isfile(os.path.join(APPS, app, icon)):
        flag(f"{rel(path)}: icon {icon} is not a file in apps/{app}")


# --- an app's accent is named once, in app.json --------------------------
#
# The accent is the app's colour, as its icon has it: what its own fills and
# graphics are drawn in. Its text tone, `accent.text`, is the colour the
# system draws its text, links and controls in, the accent itself where it
# reads as text and a tone of it where it does not (the rule after this
# one). Apple reads the text tone from an asset catalog's AccentColor, the
# global accent, and the accent from its BrandAccent; Android from the two
# resources build-logic writes out of app.json (kit-ui's
# `substrate_accent_text`, which is Material's primary, and
# `substrate_accent`). So the systems agree only while app.json and every
# colour set under the app hold the same values, light and dark, exact. An
# AccentColor without an accent is a colour Android never draws, an accent
# without one a colour Apple never draws, and an app on Android without one
# is drawn in kit-ui's Substrate blue, which says another app. An app whose
# text tone is its accent needs no BrandAccent; one whose text tone differs
# has one beside every AccentColor, or its graphics are drawn in the text
# tone.

HEX = re.compile(r"#[0-9A-Fa-f]{6}")


def is_pair(value):
    return isinstance(value, dict) and set(value) == {"light", "dark"} and all(isinstance(v, str) and HEX.fullmatch(v) for v in value.values())


def colorset_value(color):
    """A colorset entry's sRGB colour as #RRGGBB, or None for any other
    spelling (another colour space, a system colour, a missing channel)."""
    if not isinstance(color, dict) or color.get("color-space") != "srgb":
        return None
    components = color.get("components") or {}
    out = []
    for channel in ("red", "green", "blue"):
        value = components.get(channel)
        if isinstance(value, str) and value.lower().startswith("0x"):
            byte = int(value, 16)
        elif isinstance(value, (str, int, float)):
            number = float(value)
            byte = round(number * 255) if "." in str(value) else int(number)
        else:
            return None
        if not 0 <= byte <= 255:
            return None
        out.append(byte)
    return "#" + "".join(f"{b:02X}" for b in out)


def colorset_accent(path):
    """A colorset as {"light", "dark"}, or the reason it is not one."""
    try:
        doc = json.loads(read(path))
    except ValueError as e:
        return f"is not JSON: {e}"
    found = {}
    for entry in doc.get("colors") or []:
        appearances = entry.get("appearances") or []
        if not appearances:
            mode = "light"
        elif appearances == [{"appearance": "luminosity", "value": "dark"}]:
            mode = "dark"
        else:
            return f"has an entry for {appearances}, and an accent is a light and a dark colour"
        value = colorset_value(entry.get("color"))
        if value is None:
            return f"spells its {mode} colour as other than sRGB channels"
        if mode in found:
            return f"has two {mode} colours"
        found[mode] = value
    if set(found) != {"light", "dark"}:
        return f"has {sorted(found) or 'no colours'}, and an accent is a light and a dark colour"
    return found


accents = {}
for app in apps:
    path = os.path.join(APPS, app, "app.json")
    try:
        doc = json.loads(read(path))
    except (OSError, ValueError):
        continue
    if not isinstance(doc, dict):
        continue
    accent = doc.get("accent")
    if "accent" in doc and not (
        isinstance(accent, dict) and set(accent) == {"light", "dark", "text"} and is_pair({m: accent[m] for m in ("light", "dark")}) and is_pair(accent["text"])
    ):
        flag(f'{rel(path)}: accent is {accent!r}, and it is {{"light": "#RRGGBB", "dark": "#RRGGBB", "text": {{"light": "#RRGGBB", "dark": "#RRGGBB"}}}}')
        accent = None
    platforms = doc.get("platforms") if isinstance(doc.get("platforms"), list) else []
    if "android" in platforms and "accent" not in doc:
        flag(f"{rel(path)}: platforms names android and there is no accent, which the Android build draws the app in")
    if accent:
        accents[app] = (path, platforms, accent)
    sets = [p for p in walk(os.path.join(APPS, app)) if p.endswith(os.path.join(".colorset", "Contents.json"))]
    named = {"AccentColor": "text tone", "BrandAccent": "accent"}
    for name, what in named.items():
        for colorset in (p for p in sets if os.path.basename(os.path.dirname(p)) == f"{name}.colorset"):
            read_back = colorset_accent(colorset)
            if isinstance(read_back, str):
                flag(f"{rel(colorset)} {read_back}")
            elif "accent" not in doc:
                flag(f"{rel(colorset)} is a colour ({read_back['light']}, dark {read_back['dark']}) that {rel(path)} names no accent for")
            elif accent:
                values = accent["text"] if name == "AccentColor" else accent
                for mode in ("light", "dark"):
                    if read_back[mode] != values[mode].upper():
                        field = f"accent.text.{mode}" if name == "AccentColor" else f"accent.{mode}"
                        flag(f"{rel(colorset)}: the {mode} {what} is {read_back[mode]}, and {rel(path)}'s {field} is {values[mode]}")
    accent_colors = [p for p in sets if os.path.basename(os.path.dirname(p)) == "AccentColor.colorset"]
    if "accent" in doc and not accent_colors and {"ios", "macos"} & set(platforms):
        flag(f"{rel(path)}: accent is there, and apps/{app} has no AccentColor in an asset catalog to draw it on Apple systems")
    if accent and any(accent["text"][m].upper() != accent[m].upper() for m in ("light", "dark")):
        for colorset in accent_colors:
            catalog = os.path.dirname(os.path.dirname(colorset))
            if not os.path.exists(os.path.join(catalog, "BrandAccent.colorset", "Contents.json")):
                flag(f"{rel(catalog)} has no BrandAccent, and {rel(path)}'s text tone is not its accent, so its graphics would be drawn in the text tone")


# --- an app's text tone reads at 4.5:1 on Apple's backgrounds ------------
#
# Accent text is as small as body text (a toolbar's Save, a link, a
# picker's value, New List), so WCAG AA asks 4.5:1 of it, not a control's
# 3:1. These are the backgrounds an Apple system draws that text on: a
# screen, a grouped list's background and, in dark mode, its rows and a
# sheet's rows (elevated); macOS's window, under-page and grouped fills
# fall between them. The text tone is the accent where the accent reads on
# every one; elsewhere it is the accent's hue at the nearest tone that
# does, darker in light mode and lighter in dark, which the failure names.
# The tones are Material's and KitScheme's: CIELAB lightness at the
# accent's OKLCH hue and chroma, the chroma lowered where sRGB ends.
# Android's surfaces are KitSchemeTest's, which makes the scheme they are.

APPLE_BACKGROUNDS = {
    "light": {"systemBackground": "#FFFFFF", "systemGroupedBackground": "#F2F2F7"},
    "dark": {"systemBackground": "#000000", "secondarySystemGroupedBackground": "#1C1C1E", "a sheet's grouped rows": "#2C2C2E"},
}


def linear(channel):
    return channel / 12.92 if channel <= 0.04045 else ((channel + 0.055) / 1.055) ** 2.4


def channels(hex_):
    return [int(hex_[i : i + 2], 16) / 255 for i in (1, 3, 5)]


def luminance(hex_):
    r, g, b = (linear(c) for c in channels(hex_))
    return 0.2126 * r + 0.7152 * g + 0.0722 * b


def contrast(a, b):
    la, lb = luminance(a), luminance(b)
    return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)


def oklch(hex_):
    r, g, b = (linear(c) for c in channels(hex_))
    l, m, s = (
        (0.4122214708 * r + 0.5363325363 * g + 0.0514459929 * b) ** (1 / 3),
        (0.2119034982 * r + 0.6806995451 * g + 0.1073969566 * b) ** (1 / 3),
        (0.0883024619 * r + 0.2817188376 * g + 0.6299787005 * b) ** (1 / 3),
    )
    a = 1.9779984951 * l - 2.4285922050 * m + 0.4505937099 * s
    bb = 0.0259040371 * l + 0.7827717662 * m - 0.8086757660 * s
    return math.hypot(a, bb), math.degrees(math.atan2(bb, a))


def oklab_rgb(lightness, chroma, hue):
    a, b = chroma * math.cos(math.radians(hue)), chroma * math.sin(math.radians(hue))
    l3, m3, s3 = (
        (lightness + 0.3963377774 * a + 0.2158037573 * b) ** 3,
        (lightness - 0.1055613458 * a - 0.0638541728 * b) ** 3,
        (lightness - 0.0894841775 * a - 1.2914855480 * b) ** 3,
    )
    return [
        4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3,
        -1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3,
        -0.0041960863 * l3 - 0.7034186147 * m3 + 1.7076147010 * s3,
    ]


def tone(value, chroma, hue):
    """KitScheme's `Oklch.tone`: CIELAB tone `value` at an OKLCH hue and
    chroma, or at the most chroma sRGB holds there, as #RRGGBB."""

    def at(chroma):
        target = ((value + 16) / 116) ** 3 if value > 8 else value / 903.2962962
        lo, hi = 0.0, 1.0
        for _ in range(40):
            mid = (lo + hi) / 2
            rgb = oklab_rgb(mid, chroma, hue)
            lo, hi = (mid, hi) if 0.2126 * rgb[0] + 0.7152 * rgb[1] + 0.0722 * rgb[2] < target else (lo, mid)
        return oklab_rgb((lo + hi) / 2, chroma, hue)

    rgb = at(chroma)
    if not all(-1e-4 <= c <= 1.0001 for c in rgb):
        lo, hi = 0.0, chroma
        for _ in range(30):
            mid = (lo + hi) / 2
            lo, hi = (mid, hi) if all(-1e-4 <= c <= 1.0001 for c in at(mid)) else (lo, mid)
        rgb = at(lo)
    encoded = [12.92 * c if c <= 0.0031308 else 1.055 * c ** (1 / 2.4) - 0.055 for c in (min(max(c, 0.0), 1.0) for c in rgb)]
    return "#" + "".join(f"{int(c * 255 + 0.5):02X}" for c in encoded)


def readable_tone(accent, mode, backgrounds):
    """The accent's hue at the tone nearest its own that reads 4.5:1 on
    every background: darker in light mode, lighter in dark."""
    chroma, hue = oklch(accent)
    y = luminance(accent)
    value = 116 * y ** (1 / 3) - 16 if y > 0.008856 else 903.2962962 * y
    step = 0.05 if mode == "dark" else -0.05
    while 0 < value < 100:
        candidate = tone(value, chroma, hue)
        if all(contrast(candidate, b) >= 4.5 for b in backgrounds):
            return candidate
        value += step
    return "#FFFFFF" if mode == "dark" else "#000000"


for path, platforms, accent in accents.values():
    if not {"ios", "macos"} & set(platforms):
        continue
    for mode in ("light", "dark"):
        text, brand = accent["text"][mode], accent[mode]
        if (luminance(text) > luminance(brand)) if mode == "light" else (luminance(text) < luminance(brand)):
            flag(f"{rel(path)}: accent.text.{mode} {text} is {'lighter' if mode == 'light' else 'darker'} than the accent {brand}, and a {mode} text tone is the accent or {'darker' if mode == 'light' else 'lighter'}")
        for name, background in APPLE_BACKGROUNDS[mode].items():
            ratio = contrast(text, background)
            if ratio < 4.5:
                derived = readable_tone(brand, mode, APPLE_BACKGROUNDS[mode].values())
                flag(
                    f"{rel(path)}: accent.text.{mode} {text} reads {ratio:.2f}:1 on {name} ({background}), under 4.5:1;"
                    f" {derived} is the accent's hue at the nearest tone that reads on each of Apple's {mode} backgrounds"
                )


# --- every third-party package is listed with its reason -----------------
#
# Each package the apps take from outside the tree is code every app ships
# or every build fetches, so adding one is a decision with a reason, written
# where a reader of the apps finds it: apps/README.md's "Third-party
# packages" table, one row per package with why. A row whose package nothing
# uses any longer goes too.

used = {}
for package in packages_under(APPS):
    for url in package.urls:
        used.setdefault(url.rstrip("/").split("/")[-1].removesuffix(".git"), rel(package.path))
for path in [os.path.join(APPS, "xcode", "project.yml")] + [os.path.join(APPS, a, "apple", "targets.yml") for a in apps]:
    if not os.path.exists(path):
        continue
    for url in re.findall(r"^\s+url:\s*(\S+)", read(path), re.M):
        used.setdefault(url.rstrip("/").split("/")[-1].removesuffix(".git"), rel(path))

# The Android build's: every library in the Gradle catalog, by its Maven
# group (`androidx.*` as one, AndroidX).
catalog = os.path.join(APPS, "gradle", "gradle", "libs.versions.toml")
if os.path.exists(catalog):
    for group in re.findall(r'module\s*=\s*"([^:"]+):', read(catalog)):
        used.setdefault("androidx" if group.startswith("androidx.") else group, rel(catalog))

readme = read(os.path.join(APPS, "README.md"))
section = re.search(r"^## Third-party packages\n(.*?)(?=^## |\Z)", readme, re.M | re.S)
listed = {}
if not section:
    flag("apps/README.md has no \"## Third-party packages\" section")
else:
    for m in re.finditer(r"^\|\s*`([^`]+)`\s*\|\s*([^|]*)\|", section.group(1), re.M):
        listed[m.group(1)] = m.group(2).strip()
for name, where in sorted(used.items()):
    if name not in listed:
        flag(f"{where} takes {name}, which apps/README.md's third-party packages do not list")
    elif not listed[name]:
        flag(f"apps/README.md lists {name} with no reason")
for name in sorted(set(listed) - set(used)):
    flag(f"apps/README.md lists {name}, which no package or project takes")


for failure in failures:
    print(f"lint:apps: {failure}", file=sys.stderr)
sys.exit(1 if failures else 0)
