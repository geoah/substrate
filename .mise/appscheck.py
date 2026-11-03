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
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
APPS = os.path.join(ROOT, "apps")
KIT = os.path.join(APPS, "kit")

# The folders under apps/ that are not apps: the kit and the Xcode project.
NOT_APPS = {"kit", "xcode"}

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
# SubstrateResolv is the C half of SubstrateDeviceApple; SubstrateKit is the
# umbrella every app imports today, over every library but the test fakes.

KIT_ORDER = [
    "SubstrateCore",
    "SubstrateDevice",
    "SubstrateAPI",
    "SubstrateAuth",
    "SubstrateTesting",
    "SubstrateDeviceApple",
    "SubstrateUI",
]
KIT_HELPERS = {"SubstrateResolv": {"SubstrateDeviceApple"}}
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


# --- the portable code imports only what Linux and Android have ----------
#
# The kit's portable libraries and every app's core build for Linux and
# Android, where Apple's frameworks are not. They may import Foundation
# (FoundationNetworking under `#if canImport(FoundationNetworking)`, where
# URLSession lives off Apple systems), Observation, Dispatch, the kit's
# libraries and their own package's targets, and SHA-256 as CryptoKit or
# swift-crypto's Crypto, either under `#if canImport(CryptoKit)`. The Linux
# job catches what this misses, later and slower.

PORTABLE_KIT = ["SubstrateCore", "SubstrateDevice", "SubstrateAPI", "SubstrateAuth", "SubstrateTesting"]
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
# each app's manifest test and, next, the release read it, and a reader that
# finds a field missing fails far from the file. So every app folder has one,
# it parses, and every field is there with its shape. What an app publishes
# and what it asks for are two lists: Workouts asks for health, which another
# authority publishes, and a request names a version floor a published
# package does not have.

FIELDS = {
    "id": str,
    "name": str,
    "platforms": list,
    "apple": dict,
    "publishes": list,
    "asks": list,
    "scopes": list,
    "icon": str,
    "feed": str,
}
APPLE_FIELDS = {"bundleID": str, "extensions": list, "clientIDKey": str, "redirectKey": str}
PLATFORMS = {"ios", "macos"}
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
    for key in sorted(set(doc) - set(FIELDS)):
        flag(f"{rel(path)}: {key} is no app.json field (apps/README.md lists them)")
    if doc.get("id") != app:
        flag(f"{rel(path)}: id is {doc.get('id')!r}, and the app's folder is {app}")
    if isinstance(doc.get("name"), str) and not doc["name"].strip():
        flag(f"{rel(path)}: name is empty")
    platforms = doc.get("platforms") if isinstance(doc.get("platforms"), list) else []
    if not platforms or set(platforms) - PLATFORMS:
        flag(f"{rel(path)}: platforms is {platforms}, and each is one of {sorted(PLATFORMS)}")
    apple = doc.get("apple") if isinstance(doc.get("apple"), dict) else {}
    for key, kind in APPLE_FIELDS.items():
        if not isinstance(apple.get(key), kind):
            flag(f"{rel(path)}: apple.{key} is missing or not a {kind.__name__}")
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
