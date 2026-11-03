"""Build and lock the closures every app's site publishes.

An app names the packages it publishes in its app.json (`publishes`), and
their manifests are the YAML files under its package/ folder, in
`substratectl apply -f` form under the publisher's authority (decision
records 0214 and 0221). A package's closure is the multi-document YAML the
site serves at /packages/<package>-<version>.yaml: the file holding the
package's `core/package` document (its bundle document beside it) first,
then, for each entry of the bundle's `installs` in order, the file that
declares it. The bundle already lists its declarations in apply order, so
the closure needs no order of its own, and every YAML file under package/
belongs to exactly one closure.

A published version is immutable (decision record 0212), so each app's
package/closures.lock.json keeps every package's newest published version
and its closure's sha256, written by the release after a publish and
committed: a closure that changed under the version the lock holds, or a
version below it, is refused here, offline, before the site could refuse it
against the live document.

An app whose package is partly generated keeps its generator at
scripts/<app>-package.py (Workouts renders its coach's agent document): it
runs with --check first, so a closure is never built from a stale file.

    uv run --with pyyaml .mise/closures.py --check [app ...]
    uv run --with pyyaml .mise/closures.py --out build/apps/site/packages [app ...]

--out writes DIR/<package>-<version>.yaml and prints `path sha256` for each.
--json writes DIR/<package>.json, the closure an app carries as a resource
(`{"package", "version", "documents"}`, the documents in closure order), so
its build knows the version it asks for; with --check beside it, a file that
differs is named and nothing is written. With no app named, every app under
apps/ that publishes one.

    uv run --with pyyaml .mise/closures.py --json apps/tasks/TasksKit/Sources/TasksCore/Resources/Package tasks
"""

from __future__ import annotations

import argparse
import hashlib
import json
import subprocess
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[1]
APPS = ROOT / "apps"
PACKAGE_KIND = "substrate.reamde.dev/core/package"
BUNDLE_KIND = "substrate.reamde.dev/core/bundle"


class Loader(yaml.SafeLoader):
    """SafeLoader without the implicit timestamp: a datetime stays its string."""


Loader.yaml_implicit_resolvers = {
    first: [(tag, regexp) for tag, regexp in resolvers if tag != "tag:yaml.org,2002:timestamp"]
    for first, resolvers in yaml.SafeLoader.yaml_implicit_resolvers.items()
}


class ClosureError(Exception):
    pass


def app_json(app: str) -> dict:
    path = APPS / app / "app.json"
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (OSError, ValueError) as error:
        raise ClosureError(f"apps/{app}/app.json: {error}") from error


def publishing_apps() -> list[str]:
    return sorted(path.parent.name for path in APPS.glob("*/app.json") if app_json(path.parent.name).get("publishes"))


def generate_check(app: str) -> None:
    """Runs the app's own generator with --check, where it has one."""
    script = APPS / app / "scripts" / f"{app}-package.py"
    if not script.exists():
        return
    done = subprocess.run([sys.executable, str(script), "--check"], check=False)
    if done.returncode != 0:
        raise ClosureError(f"apps/{app}: {script.relative_to(ROOT)} --check failed; run it and commit what it writes")


def load(path: Path) -> list[dict]:
    with open(path, encoding="utf-8") as handle:
        docs = [doc for doc in yaml.load_all(handle, Loader=Loader) if doc is not None]
    for doc in docs:
        if not isinstance(doc, dict) or not isinstance(doc.get("kind"), str):
            raise ClosureError(f"{path.relative_to(ROOT)}: a document with no kind")
        if not isinstance(doc.get("metadata"), dict) or not isinstance(doc["metadata"].get("id"), str):
            raise ClosureError(f"{path.relative_to(ROOT)}: a document with no metadata.id")
    return docs


def text(path: Path) -> str:
    out = path.read_text(encoding="utf-8")
    return out if out.endswith("\n") else out + "\n"


def closures(app: str) -> dict[str, tuple[int, str]]:
    """Each package the app publishes: its version and closure text, by id."""
    publishes = app_json(app).get("publishes") or []
    if not publishes:
        return {}
    folder = APPS / app / "package"
    files = {path: load(path) for path in sorted(folder.rglob("*.yaml"))}
    declared: dict[str, Path] = {}
    headers: dict[str, Path] = {}
    for path, docs in files.items():
        for doc in docs:
            if doc["kind"] in (PACKAGE_KIND, BUNDLE_KIND):
                if doc["kind"] == PACKAGE_KIND:
                    headers.setdefault(doc["metadata"]["id"], path)
                continue
            if doc["metadata"]["id"] in declared:
                raise ClosureError(f"{doc['metadata']['id']} is declared twice under apps/{app}/package")
            declared[doc["metadata"]["id"]] = path
    used: dict[Path, str] = {}
    out = {}
    for package_id in publishes:
        authority, _, name = package_id.partition("/")
        header = headers.get(package_id)
        if header is None:
            raise ClosureError(f"apps/{app}/app.json publishes {package_id}, and no file under apps/{app}/package declares it")
        docs = files[header]
        package = [doc for doc in docs if doc["kind"] == PACKAGE_KIND and doc["metadata"]["id"] == package_id]
        bundle = [doc for doc in docs if doc["kind"] == BUNDLE_KIND and doc["metadata"]["id"] == package_id]
        where = header.relative_to(ROOT)
        if len(package) != 1 or len(bundle) != 1:
            raise ClosureError(f"{where}: expected one package and one bundle document for {package_id}")
        data = package[0].get("data") or {}
        if data.get("authority") != authority or data.get("package") != name:
            raise ClosureError(f"{where}: the package is {package_id}, published under {authority}")
        version = data.get("version")
        if not isinstance(version, int) or isinstance(version, bool) or version < 1:
            raise ClosureError(f"{where}: data.version is a positive integer")
        installs = (bundle[0].get("data") or {}).get("installs") or []
        if not isinstance(installs, list):
            raise ClosureError(f"{where}: the bundle's installs is a list")
        order = [header]
        for entry in installs:
            path = declared.get(entry)
            if path is None:
                raise ClosureError(f"{where}: the bundle installs {entry}, and no file under apps/{app}/package declares it")
            if path in order:
                raise ClosureError(f"{path.relative_to(ROOT)} holds more than one of {package_id}'s installs; one file per declaration")
            order.append(path)
        for path in order:
            if path in used:
                raise ClosureError(f"{path.relative_to(ROOT)} is in both {used[path]} and {package_id}")
            used[path] = package_id
            for doc in files[path]:
                dumped = json.dumps(doc)
                if "~/" in dumped or '"~"' in dumped:
                    raise ClosureError(f"{path.relative_to(ROOT)}: a published package names its authority in full, never `~`")
        out[package_id] = (version, "---\n".join(text(path) for path in order))
    for path in files:
        if path not in used:
            raise ClosureError(f"{path.relative_to(ROOT)} is in no closure apps/{app}/app.json publishes: a package its bundle installs, or no file there")
    return out


def sha256(value: str) -> str:
    return hashlib.sha256(value.encode("utf-8")).hexdigest()


def check_lock(app: str, built: dict[str, tuple[int, str]]) -> None:
    """Refuses a closure that changed under the version the lock says is
    published, or a version below it."""
    lock = APPS / app / "package" / "closures.lock.json"
    held = json.loads(lock.read_text(encoding="utf-8")) if lock.exists() else {}
    for package_id in sorted(built):
        version, closure = built[package_id]
        before = held.get(package_id)
        if not before:
            continue
        if version < before["version"]:
            raise ClosureError(f"{package_id}: version {version} is below the published {before['version']}")
        if version == before["version"] and sha256(closure) != before["sha256"]:
            raise ClosureError(f"{package_id}: the closure changed under the published version {version}; bump data.version, a published version never changes")


def write_json(folder: Path, built: dict[str, tuple[int, str]], check: bool) -> list[Path]:
    """Each closure as the JSON resource an app carries; answers the files
    that differ, written unless `check`."""
    stale = []
    for package_id, (version, closure) in sorted(built.items()):
        docs = [doc for doc in yaml.load_all(closure, Loader=Loader) if doc is not None]
        rendered = json.dumps({"package": package_id, "version": version, "documents": docs}, ensure_ascii=False, indent=2) + "\n"
        path = folder / f"{package_id.rsplit('/', 1)[1]}.json"
        if path.exists() and path.read_text(encoding="utf-8") == rendered:
            continue
        stale.append(path)
        if not check:
            folder.mkdir(parents=True, exist_ok=True)
            path.write_text(rendered, encoding="utf-8")
    return stale


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--check", action="store_true", help="hold every closure to its app's lock, and write nothing")
    parser.add_argument("--out", metavar="DIR", help="write each closure as DIR/<package>-<version>.yaml")
    parser.add_argument("--json", metavar="DIR", help="write each closure as DIR/<package>.json, the resource an app carries")
    parser.add_argument("apps", nargs="*", help="the apps, by folder name; every app that publishes a package when none")
    args = parser.parse_args()
    if not args.json and args.check == bool(args.out):
        parser.error("one of --check and --out")
    if args.json and args.out:
        parser.error("--json writes the resource alone")
    try:
        names = args.apps or publishing_apps()
        built = {}
        for app in names:
            generate_check(app)
            mine = closures(app)
            # The lock first on every path: a closure the site is about to
            # publish is held to it before the site reads the live document.
            check_lock(app, mine)
            for package_id in mine:
                if package_id in built:
                    raise ClosureError(f"{package_id} is published by two apps")
            built.update(mine)
        if args.out:
            out = Path(args.out)
            out.mkdir(parents=True, exist_ok=True)
            for package_id, (version, closure) in built.items():
                path = out / f"{package_id.rsplit('/', 1)[1]}-{version}.yaml"
                path.write_text(closure, encoding="utf-8")
                print(f"{path} {sha256(closure)}")
        if args.json:
            stale = write_json(Path(args.json), built, check=args.check)
            for path in [] if args.check else stale:
                print(f"wrote {path}")
            if args.check and stale:
                for path in stale:
                    print(f"closures.py: stale: {path}", file=sys.stderr)
                return 1
    except (ClosureError, OSError, yaml.YAMLError) as error:
        print(f"closures.py: {error}", file=sys.stderr)
        return 2
    return 0


if __name__ == "__main__":
    sys.exit(main())
