"""Turn Substrate Workouts' package manifests into the JSON the app bundles.

The manifests in apps/apple/SubstrateWorkouts/Package/ are the source: YAML
in `substratectl apply -f` form, the training kinds under the authority `~`
(decision record 0203). The app applies them as the documents of one
`POST /api/v1/vocabulary/apply`, so it bundles them as JSON, and its codec
drift test reads each kind's property names from kinds.json. This script
writes both:

    uv run --with pyyaml apps/apple/scripts/workouts-package.py           # rewrite
    uv run --with pyyaml apps/apple/scripts/workouts-package.py --check   # exit 1 and name each stale file

The output has no timestamps, so a run over unchanged YAML changes nothing.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

import yaml

APPLE = Path(__file__).resolve().parents[1]
PACKAGE = APPLE / "SubstrateWorkouts/Package"
# The apply order: the package and its bundle, then each kind after the kinds
# it names (a setup names an exercise, a session a routine, a set the rest).
FILES = ("training", "exercise", "setup", "routine", "session", "set")
KINDS = ("exercise", "routine", "session", "set", "setup")
RESOURCE = APPLE / "WorkoutsKit/Sources/WorkoutsCore/Resources/Package/training.json"
FIXTURE = APPLE / "WorkoutsKit/Tests/WorkoutsCoreTests/Fixtures/kinds.json"

# The keys of a kind's property that kinds.json keeps. Descriptions, labels
# and bounds are the server's business, not the codec's.
SPEC_KEYS = ("type", "required", "repeated", "default", "kind")


class Loader(yaml.SafeLoader):
    """SafeLoader without the implicit timestamp: a datetime stays its string."""


Loader.yaml_implicit_resolvers = {
    first: [(tag, regexp) for tag, regexp in resolvers if tag != "tag:yaml.org,2002:timestamp"]
    for first, resolvers in yaml.SafeLoader.yaml_implicit_resolvers.items()
}


class GenerateError(Exception):
    pass


def documents(name: str) -> list[dict]:
    path = PACKAGE / f"{name}.yaml"
    with open(path, encoding="utf-8") as handle:
        docs = [doc for doc in yaml.load_all(handle, Loader=Loader) if doc is not None]
    for doc in docs:
        if not isinstance(doc, dict) or not isinstance(doc.get("kind"), str):
            raise GenerateError(f"{path.name}: a document with no kind")
    return docs


def package() -> dict:
    docs = [doc for name in FILES for doc in documents(name)]
    headers = [doc for doc in docs if doc["kind"] == "substrate.reamde.dev/core/package"]
    if len(headers) != 1:
        raise GenerateError("training.yaml: expected one package document")
    header = headers[0]
    return {"package": header["metadata"]["id"], "version": header["data"]["version"], "documents": docs}


def property_spec(name: str, spec: dict, where: str) -> dict:
    if not isinstance(spec, dict) or not isinstance(spec.get("type"), str):
        raise GenerateError(f"{where}.{name}: a property needs a type")
    out = {"name": name}
    for key in SPEC_KEYS:
        if key in spec:
            out[key] = spec[key]
    if "values" in spec:
        out["values"] = [entry["value"] for entry in spec["values"]]
    if "states" in spec:
        out["values"] = list(spec["states"])
    if "fields" in spec:
        out["fields"] = [property_spec(field, fspec, f"{where}.{name}") for field, fspec in spec["fields"].items()]
    return out


def kinds() -> dict:
    out = []
    for name in KINDS:
        docs = documents(name)
        if len(docs) != 1 or docs[0]["kind"] != "substrate.reamde.dev/core/kind":
            raise GenerateError(f"{name}.yaml: expected one kind document")
        data = docs[0]["data"]
        out.append(
            {
                "kind": docs[0]["metadata"]["id"],
                "source": f"{name}.yaml",
                "traits": list(data.get("traits", [])),
                "properties": [property_spec(prop, spec, name) for prop, spec in data["properties"].items()],
            }
        )
    return {"source": "apps/apple/SubstrateWorkouts/Package", "kinds": out}


def render(value: dict) -> str:
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--check", action="store_true", help="exit 1 if a generated file is stale")
    args = parser.parse_args()
    try:
        files = {RESOURCE: render(package()), FIXTURE: render(kinds())}
    except (GenerateError, OSError, yaml.YAMLError) as error:
        print(f"workouts-package.py: {error}", file=sys.stderr)
        return 2
    stale = []
    for path, text in files.items():
        if path.exists() and path.read_text(encoding="utf-8") == text:
            continue
        if args.check:
            stale.append(path)
            continue
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text, encoding="utf-8")
        print(f"wrote {path.relative_to(APPLE)}")
    for path in stale:
        print(f"stale: {path.relative_to(APPLE)}", file=sys.stderr)
    return 1 if stale else 0


if __name__ == "__main__":
    sys.exit(main())
