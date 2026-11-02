"""Turn Substrate Workouts' YAML into the JSON the app and its tests read.

Two sources, both YAML in `substratectl apply -f` form under the authority
`~` (decision record 0203):

- The package's manifests in apps/apple/SubstrateWorkouts/Package/. The app
  applies them as the documents of one `POST /api/v1/vocabulary/apply`, so it
  bundles them as JSON, and its codec drift test reads each kind's property
  names from kinds.json.
- The example records in apps/apple/SubstrateWorkouts/docs/examples/.
  sample.yaml is the sample routine and its exercises, the app's bundled
  catalogue (Resources/Bundled); the other four files are the test vectors
  (Tests/WorkoutsCoreTests/Fixtures/examples). Each record becomes the flat
  shape substrate's REST API speaks, `{"kind", "id", "properties"}`. An
  exercise id in more than one file must be the same record in each.

It also renders the workout coach (decision record 0206, Package/coach/): the
coach package's documents and the agent's prompt, from the instructions, the
mechanics and a field guide generated from the routine and exercise
manifests, so the prompt and the kinds cannot drift.

This script writes all of them:

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
FILES = ("training", "exercise", "setup", "routine", "session", "set", "memory")
KINDS = ("exercise", "routine", "session", "set", "setup")
RESOURCE = APPLE / "WorkoutsKit/Sources/WorkoutsCore/Resources/Package/training.json"
COACH = PACKAGE / "coach"
COACH_RESOURCE = APPLE / "WorkoutsKit/Sources/WorkoutsCore/Resources/Package/coach.json"
# The coach's prompt revision: bump it with every change to the rendered
# prompt, so an older app never writes its prompt over a newer one's.
COACH_REVISION = 1
# The coach's prompt is a cacheable prefix beside the kinds list and its
# pinned memories; past this it crowds them out.
COACH_PROMPT_MAX = 18432
FIXTURE = APPLE / "WorkoutsKit/Tests/WorkoutsCoreTests/Fixtures/kinds.json"
EXAMPLES = APPLE / "SubstrateWorkouts/docs/examples"
SAMPLE = "sample"
BUNDLED = APPLE / "WorkoutsKit/Sources/WorkoutsCore/Resources/Bundled"
# The test vectors: the owner's Hevy history and the methods it leaves out.
VECTORS = ("exercises", "routines", "sessions", "coverage")
VECTOR_FIXTURES = APPLE / "WorkoutsKit/Tests/WorkoutsCoreTests/Fixtures/examples"

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


def example_records(name: str) -> list[dict]:
    path = EXAMPLES / f"{name}.yaml"
    with open(path, encoding="utf-8") as handle:
        docs = [doc for doc in yaml.load_all(handle, Loader=Loader) if doc is not None]
    out = []
    for doc in docs:
        try:
            out.append({"kind": doc["kind"], "id": doc["metadata"]["id"], "properties": doc["data"]["properties"]})
        except (KeyError, TypeError) as error:
            raise GenerateError(f"{path.name}: a record without {error}") from error
    return out


def record_file(name: str, records: list[dict]) -> dict:
    return {"source": f"apps/apple/SubstrateWorkouts/docs/examples/{name}.yaml", "records": records}


def examples() -> dict[Path, dict]:
    files = {name: example_records(name) for name in (SAMPLE, *VECTORS)}
    seen: dict[tuple[str, str], tuple[str, dict]] = {}
    for name, records in files.items():
        for record in records:
            if not record["kind"].endswith("/exercise"):
                continue
            key = (record["kind"], record["id"])
            if key in seen and seen[key][1] != record["properties"]:
                raise GenerateError(
                    f"exercise {record['id']} differs between {seen[key][0]}.yaml and {name}.yaml: make the two records equal"
                )
            seen.setdefault(key, (name, record["properties"]))
    sample = files[SAMPLE]
    exercises = [record for record in sample if record["kind"].endswith("/exercise")]
    routines = [record for record in sample if record["kind"].endswith("/routine")]
    if len(exercises) + len(routines) != len(sample):
        raise GenerateError(f"{SAMPLE}.yaml: only exercises and routines are bundled")
    out = {
        BUNDLED / "exercises.json": record_file(SAMPLE, exercises),
        BUNDLED / "routines.json": record_file(SAMPLE, routines),
    }
    for name in VECTORS:
        out[VECTOR_FIXTURES / f"{name}.json"] = record_file(name, files[name])
    return out


def field_line(name: str, spec: dict) -> str:
    kind = spec["type"]
    if "values" in spec:
        kind = "|".join(entry["value"] if isinstance(entry, dict) else str(entry) for entry in spec["values"])
    elif kind == "reference":
        kind = "ref " + spec.get("kind", "")
    return f"{name}{'[]' if spec.get('repeated') else ''}{'*' if spec.get('required') else ''}: {kind}"


def field_guide(name: str, properties: dict, indent: str = "") -> list[str]:
    plain, said = [], {}
    for prop, spec in properties.items():
        if "fields" in spec:
            continue
        line = field_line(prop, spec)
        values = line.split(": ", 1)[1]
        if "|" in values and values in said:
            line = line.split(": ", 1)[0] + f": as {said[values]}"
        said.setdefault(values, prop)
        plain.append(line)
    lines = [f"{indent}{name}: " + "; ".join(plain)]
    for prop, spec in properties.items():
        if "fields" in spec and prop not in GUIDE_SKIP:
            lines += field_guide(f"{prop}[] (each)", spec["fields"], indent + "  ")
    return lines


# Fields the coach never writes, left out of its guide.
GUIDE_SKIP = ("codes",)


def coach_guide(version: int) -> str:
    lines = [
        f"# Fields you write (training package version {version})",
        "",
        "`*` required, `[]` a list. A reference is a record path such as",
        "`<authority>/training/exercise/bench-press`; a quantity is",
        '`{value: "60", unit: kg}`; a decimal is a string ("7.5"); a duration is',
        "ISO 8601 (`PT2M30S`). Clear a property with a patch naming it `null`.",
        "",
    ]
    for name in ("routine", "exercise"):
        doc = documents(name)[0]
        lines += field_guide(doc["metadata"]["id"], doc["data"]["properties"])
        lines.append("")
    return "\n".join(lines).rstrip() + "\n"


def coach() -> dict:
    version = package()["version"]
    instructions = (COACH / "instructions.md").read_text(encoding="utf-8").strip()
    mechanics = (COACH / "mechanics.md").read_text(encoding="utf-8").strip()
    prompt = (
        f"Workout coach instructions, revision {COACH_REVISION}.\n\n"
        + instructions
        + "\n\n"
        + mechanics
        + "\n\n"
        + coach_guide(version)
    )
    if len(prompt.encode("utf-8")) > COACH_PROMPT_MAX:
        raise GenerateError(f"the coach's prompt is {len(prompt.encode())} bytes, over {COACH_PROMPT_MAX}")
    with open(COACH / "trainingcoach.yaml", encoding="utf-8") as handle:
        docs = [doc for doc in yaml.load_all(handle, Loader=Loader) if doc is not None]
    return {
        "package": docs[0]["metadata"]["id"],
        "revision": COACH_REVISION,
        "trainingVersion": version,
        "prompt": prompt,
        "documents": docs,
    }


def render(value: dict) -> str:
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--check", action="store_true", help="exit 1 if a generated file is stale")
    args = parser.parse_args()
    try:
        files = {RESOURCE: render(package()), FIXTURE: render(kinds()), COACH_RESOURCE: render(coach())}
        files.update({path: render(value) for path, value in examples().items()})
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
