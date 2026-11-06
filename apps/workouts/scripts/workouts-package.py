"""Turn Substrate Workouts' YAML into the JSON the app and its tests read,
and into the closures its site publishes.

Two sources, both YAML in `substratectl apply -f` form under the authority
`substrate.reamde.dev`, the publisher's (decision record 0214):

- The packages' manifests in apps/workouts/package/: training,
  and the workout coach under package/coach/. The app carries each package's
  documents as JSON (Resources/Package/training.json and coach.json) for the
  version its approval asks for, and its codec drift test reads each kind's
  property names from kinds.json. The app applies none of them: a repository
  installs a package from the authority document, under this very name.
- The example records in apps/workouts/docs/examples/.
  sample.yaml is the sample routine and its exercises, the app's bundled
  catalogue (Resources/Bundled), written under ids derived from the YAML's
  slugs (`bundled_sample`); the other four files are the test vectors
  (Tests/WorkoutsCoreTests/Fixtures/examples). Each record becomes the flat
  shape substrate's REST API speaks, `{"kind", "id", "properties"}`. An
  exercise id in more than one file must be the same record in each.

It renders the workout coach's agent document (package/coach/workoutcoach.yaml)
from the instructions, the mechanics and a field guide generated from the
routine and exercise manifests, so the prompt and the kinds cannot drift.
The closures the site publishes, and closures.lock.json beside the package,
are every app's alike: .mise/closures.py builds them from the committed
files, after running this script with --check.

This script writes all of them:

    uv run --with pyyaml apps/workouts/scripts/workouts-package.py           # rewrite
    uv run --with pyyaml apps/workouts/scripts/workouts-package.py --check   # exit 1 and name each stale file

The output has no timestamps, so a run over unchanged YAML changes nothing.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path

import yaml

APP = Path(__file__).resolve().parents[1]
PACKAGE = APP / "package"
# The apply order: the package and its bundle, then each kind after the kinds
# it names (an exercise names a movement, a setup an exercise, a session a
# routine, a set the rest).
FILES = ("training", "movement", "exercise", "setup", "routine", "session", "set", "memory", "library", "bodymeasurement")
KINDS = ("movement", "exercise", "routine", "session", "set", "setup")
RESOURCE = APP / "WorkoutsKit/Sources/WorkoutsCore/Resources/Package/training.json"
COACH = PACKAGE / "coach"
COACH_RESOURCE = APP / "WorkoutsKit/Sources/WorkoutsCore/Resources/Package/coach.json"
# The rendered agent document, committed beside its sources so a diff shows
# what a release changes of the coach, and read back into the closure.
COACH_AGENT_FILE = COACH / "workoutcoach.yaml"
AUTHORITY = "substrate.reamde.dev"
TRAINING = f"{AUTHORITY}/training"
COACH_PACKAGE = f"{AUTHORITY}/trainingcoach"
COACH_AGENT_ID = f"{COACH_PACKAGE}/workoutCoach"
# What the coach reads: everything the app logs, its memories, the health
# catalogue's workouts and sleep, every observation and what a metric read
# needs beside it, and the clinical record where the owner holds one (owner,
# 2026-11-02). The clinical catalogue is not required, so its kinds are a
# glob (decision record 0080): nothing until the owner installs it, and
# every clinical kind after.
COACH_READS = [f"{TRAINING}/{name}" for name in ("exercise", "setup", "routine", "session", "set", "memory")] + [
    f"{AUTHORITY}/health/workout",
    f"{AUTHORITY}/health/sleep",
    f"{AUTHORITY}/observations/observation",
    f"{AUTHORITY}/observations/metric",
    f"{AUTHORITY}/observations/source",
    f"{AUTHORITY}/observations/sourcelink",
    f"{AUTHORITY}/observations/trace",
    f"{AUTHORITY}/clinical/*",
]
# What it writes: routines and what they reference, its memories, and the
# interaction the ask tool lands. Never sessions or sets, the app's log.
COACH_WRITES = [f"{TRAINING}/{name}" for name in ("exercise", "setup", "routine", "memory")] + [f"{AUTHORITY}/llm/interaction"]
# The models the coach runs on, best first, matched exactly or behind a
# gateway's prefix on the provider row the owner binds to the `model` input
# (decision record 0217).
COACH_MODELS = ["claude-sonnet-5-5", "claude-sonnet-5", "gpt-5"]
# The most the coach spends a day, in cents; a published agent declares one.
COACH_SPEND_CENTS_PER_DAY = 100
# The agent document, less its prompt, which `coach()` renders.
COACH_AGENT = {
    "purpose": "primary",
    "authority": AUTHORITY,
    "package": "trainingcoach",
    "description": (
        "Your strength coach: plans your workout routines from your goals, training and health data, "
        "and remembers what you tell it. Installed by Substrate Workouts."
    ),
    "providerInput": "model",
    "models": COACH_MODELS,
    "tools": [{"function": f"substrate.reamde.dev/core/{name}"} for name in ("query", "write", "ask")],
    "context": {
        "pinned": [
            {
                "kind": f"{TRAINING}/memory",
                # Only what the owner wrote or the coach stored from what the
                # owner said; `source` is the writer's own claim, so the prompt
                # still reads every pinned row as a note, never an order.
                "filter": '{"properties": {"active": {"eq": true}, "source": {"in": ["owner", "coach"]}}}',
                "orderBy": "createdAt:desc,id",
                "first": 60,
                "fields": ["text", "topic", "source", "exercise", "until"],
            }
        ],
        "maxBytes": 12288,
    },
    "permissions": {
        "reads": {"kinds": COACH_READS, "budgets": {"calls": 40, "rows": 5000}},
        "writes": COACH_WRITES,
    },
    "budgets": {
        "maxTurns": 16,
        "maxToolCalls": 48,
        "maxParallelTools": 4,
        "deadlineSeconds": 300,
        "maxToolResultBytes": 65536,
        "spendCentsPerDay": COACH_SPEND_CENTS_PER_DAY,
    },
}
# The coach's prompt is a cacheable prefix beside the kinds list and its
# pinned memories; past this it crowds them out.
COACH_PROMPT_MAX = 20480
FIXTURE = APP / "WorkoutsKit/Tests/WorkoutsCoreTests/Fixtures/kinds.json"
EXAMPLES = APP / "docs/examples"
SAMPLE = "sample"
# The first part of every bundled sample id (`derived_minted_id`), and the
# code system an exercise of the sample names its slug in, which is the id
# it was written under before the ids were derived.
SAMPLE_NAMESPACE = "workouts-sample"
SAMPLE_SYSTEM = "substrate-workouts-sample"
CROCKFORD = "0123456789abcdefghjkmnpqrstvwxyz"
BUNDLED = APP / "WorkoutsKit/Sources/WorkoutsCore/Resources/Bundled"
# The test vectors: the owner's Hevy history and the methods it leaves out.
VECTORS = ("exercises", "routines", "sessions", "coverage")
VECTOR_FIXTURES = APP / "WorkoutsKit/Tests/WorkoutsCoreTests/Fixtures/examples"

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


def manifest(docs: list[dict], where: str, expected: str) -> dict:
    """A package's manifest file: its id, its version and every document."""
    headers = [doc for doc in docs if doc["kind"] == "substrate.reamde.dev/core/package"]
    if len(headers) != 1:
        raise GenerateError(f"{where}: expected one package document")
    header = headers[0]
    if header["metadata"]["id"] != expected or header["data"].get("authority") != AUTHORITY:
        raise GenerateError(f"{where}: the package is {expected}, published under {AUTHORITY}")
    version = header["data"].get("version")
    if not isinstance(version, int) or version < 1:
        raise GenerateError(f"{where}: data.version is a positive integer")
    for doc in docs:
        text = json.dumps(doc)
        if "~/" in text or '"~"' in text:
            raise GenerateError(f"{where}: a published package names its authority in full, never `~`")
    return {"package": header["metadata"]["id"], "version": version, "documents": docs}


def package() -> dict:
    return manifest([doc for name in FILES for doc in documents(name)], "training.yaml", TRAINING)


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
    return {"source": "apps/workouts/package", "kinds": out}


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


def derived_minted_id(*parts: str) -> str:
    """substrate's DerivedMintedID, as exercise-library.py spells it: the
    first 16 bytes of the SHA-256 of the parts joined by a NUL, marked as a
    UUIDv8, as 26 lowercase Crockford base32 characters."""
    digest = bytearray(hashlib.sha256("\x00".join(parts).encode("utf-8")).digest()[:16])
    digest[6] = digest[6] & 0x0F | 0x80
    digest[8] = digest[8] & 0x3F | 0x80
    value = int.from_bytes(digest, "big")
    return "".join(CROCKFORD[(value >> (5 * (25 - index))) & 31] for index in range(26))


def bundled_sample(exercises: list[dict], routines: list[dict]) -> tuple[list[dict], list[dict]]:
    """The sample as the app carries it: each record under an id derived from
    its slug, the minted shape a server takes from an app, each exercise
    naming its slug under SAMPLE_SYSTEM, and the routines' slots naming the
    derived ids."""
    def sample_id(record: dict) -> str:
        return derived_minted_id(SAMPLE_NAMESPACE, record["kind"].rsplit("/", 1)[1], record["id"])

    paths = {f"{record['kind']}/{record['id']}": f"{record['kind']}/{sample_id(record)}" for record in exercises}
    out_exercises = []
    for record in exercises:
        properties = dict(record["properties"])
        properties["codes"] = [{"system": SAMPLE_SYSTEM, "code": record["id"]}] + properties.get("codes", [])
        out_exercises.append({"kind": record["kind"], "id": sample_id(record), "properties": properties})
    out_routines = []
    for record in routines:
        properties = dict(record["properties"])
        slots = []
        for slot in properties.get("slots", []):
            if slot.get("exercise") not in paths:
                raise GenerateError(f"{SAMPLE}.yaml: {record['id']} names {slot.get('exercise')}, which the sample does not hold")
            slots.append({**slot, "exercise": paths[slot["exercise"]]})
        properties["slots"] = slots
        out_routines.append({"kind": record["kind"], "id": sample_id(record), "properties": properties})
    return out_exercises, out_routines


def record_file(name: str, records: list[dict]) -> dict:
    return {"source": f"apps/workouts/docs/examples/{name}.yaml", "records": records}


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
    exercises, routines = bundled_sample(exercises, routines)
    out = {
        BUNDLED / "exercises.json": record_file(SAMPLE, exercises),
        BUNDLED / "routines.json": record_file(SAMPLE, routines),
    }
    for name in VECTORS:
        out[VECTOR_FIXTURES / f"{name}.json"] = record_file(name, files[name])
    return out


def field_line(name: str, spec: dict) -> str:
    """One field as `name[]*: type`; a string field is the name alone, which
    the guide's legend says, to keep the prompt under its cap."""
    kind = spec["type"]
    if "values" in spec:
        kind = "|".join(entry["value"] if isinstance(entry, dict) else str(entry) for entry in spec["values"])
    elif kind == "reference":
        kind = "ref " + spec.get("kind", "")
    head = f"{name}{'[]' if spec.get('repeated') else ''}{'*' if spec.get('required') else ''}"
    return head if kind == "string" else f"{head}: {kind}"


def field_guide(name: str, properties: dict, indent: str = "", skip: tuple = ()) -> list[str]:
    plain, said = [], {}
    for prop, spec in properties.items():
        if "fields" in spec or prop in skip:
            continue
        line = field_line(prop, spec)
        values = line.split(": ", 1)[1] if ": " in line else ""
        if "|" in values and values in said:
            line = line.split(": ", 1)[0] + f": as {said[values]}"
        said.setdefault(values, prop)
        plain.append(line)
    lines = [f"{indent}{name}: " + "; ".join(plain)]
    for prop, spec in properties.items():
        if "fields" in spec and prop not in GUIDE_SKIP:
            nested = GROUP_GUIDE_SKIP if prop == "groups" else ()
            lines += field_guide(f"{prop}[] (each)", spec["fields"], indent + "  ", nested)
    return lines


# Fields the coach never writes, left out of its guide: an exercise's codes;
# its muscles one by one, whose 34 values would put the prompt past its cap
# (it writes the two group lists, and the app's exercise library fills the
# muscles of an exercise it knows); and the suggestion engine's per-exercise
# tuning, since the app picks loads and reps (instructions.md, "Your job").
GUIDE_SKIP = ("codes", "muscles")
EXERCISE_GUIDE_SKIP = ("defaultRPE", "repGain", "dropPercent", "timeStep", "myoStopRule", "myoMaxMiniSets")
# A group's `pairing` only picks the default transition, which the coach
# writes outright (mechanics.md).
GROUP_GUIDE_SKIP = ("pairing",)


def coach_guide(version: int) -> str:
    lines = [
        f"# Fields you write (training package version {version})",
        "",
        "Fields go under `input.properties`. `*` required, `[]` a list, no type a",
        "string. A reference is a record path",
        "(`substrate.reamde.dev/training/exercise/<id>`); a quantity is",
        '`{value: "60", unit: kg}`; a decimal is a string ("7.5"); a duration is',
        "ISO 8601 (`PT2M30S`). Clear a property by patching it `null`.",
        "",
    ]
    for name in ("routine", "exercise"):
        doc = documents(name)[0]
        properties = doc["data"]["properties"]
        # A skip naming a field the kind no longer has would let a renamed
        # field back into the guide unnoticed, and past the prompt's cap.
        skips = {"exercise": (properties, EXERCISE_GUIDE_SKIP), "routine": (properties.get("groups", {}).get("fields", {}), GROUP_GUIDE_SKIP)}
        fields, skip = skips[name]
        if missing := [prop for prop in skip if prop not in fields]:
            raise GenerateError(f"the coach's guide skips {missing}, which {TRAINING}/{name} does not declare")
        lines += field_guide(doc["metadata"]["id"], properties, skip=EXERCISE_GUIDE_SKIP if name == "exercise" else ())
        lines.append("")
    return "\n".join(lines).rstrip() + "\n"


class Dumper(yaml.SafeDumper):
    """The tree's YAML shape (.yamlfmt), so `mise run fmt:check` finds nothing
    to change: sequences indented under their key, and a block scalar for
    any string the formatter would otherwise rewrap (the prompt, and a line
    past its 72 columns)."""

    def increase_indent(self, flow=False, indentless=False):
        return super().increase_indent(flow, False)


def _text(dumper: yaml.SafeDumper, value: str):
    style = "|" if "\n" in value or len(value) > 72 else None
    return dumper.represent_scalar("tag:yaml.org,2002:str", value, style=style)


Dumper.add_representer(str, _text)


def coach_agent() -> dict:
    """The agent document, its prompt rendered from the sources."""
    version = package()["version"]
    instructions = (COACH / "instructions.md").read_text(encoding="utf-8").strip()
    mechanics = (COACH / "mechanics.md").read_text(encoding="utf-8").strip()
    prompt = instructions + "\n\n" + mechanics + "\n\n" + coach_guide(version)
    if len(prompt.encode("utf-8")) > COACH_PROMPT_MAX:
        raise GenerateError(f"the coach's prompt is {len(prompt.encode())} bytes, over {COACH_PROMPT_MAX}")
    data = {"purpose": COACH_AGENT["purpose"], "authority": AUTHORITY, "package": "trainingcoach", "description": COACH_AGENT["description"], "prompt": prompt}
    data.update({key: value for key, value in COACH_AGENT.items() if key not in data})
    return {"kind": "substrate.reamde.dev/core/agent", "metadata": {"id": COACH_AGENT_ID}, "data": data}


def coach_agent_yaml(agent: dict) -> str:
    # The header names the script's old path and stays so: it is part of the
    # published trainingcoach closure, whose digest closures.lock.json holds,
    # so it changes only with the package's next version.
    head = (
        "# The workout coach's agent document, RENDERED by\n"
        "# apps/apple/scripts/workouts-package.py from instructions.md, mechanics.md,\n"
        "# the routine and exercise manifests and the script's agent template; edit\n"
        "# those and run the script. Its provider is the bundle's `model` input and\n"
        "# its models are tried best first (decision record 0217).\n"
    )
    return head + yaml.dump(agent, Dumper=Dumper, sort_keys=False, allow_unicode=True, width=10000)


def coach() -> dict:
    """The trainingcoach manifest: the package, its bundle and the agent."""
    with open(COACH / "trainingcoach.yaml", encoding="utf-8") as handle:
        docs = [doc for doc in yaml.load_all(handle, Loader=Loader) if doc is not None]
    agent = coach_agent()
    out = manifest(docs + [agent], "trainingcoach.yaml", COACH_PACKAGE)
    bundle = next(doc for doc in docs if doc["kind"] == "substrate.reamde.dev/core/bundle")
    inputs = bundle["data"].get("inputs") or {}
    if inputs.get(agent["data"]["providerInput"], {}).get("kind") != "substrate.reamde.dev/llm/provider":
        raise GenerateError("trainingcoach.yaml: the bundle's inputs lack the agent's providerInput, of kind substrate.reamde.dev/llm/provider")
    if bundle["data"].get("installs") != [COACH_AGENT_ID]:
        raise GenerateError(f"trainingcoach.yaml: the bundle installs {COACH_AGENT_ID} and nothing else")
    return out


def render(value: dict) -> str:
    return json.dumps(value, ensure_ascii=False, indent=2) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--check", action="store_true", help="exit 1 if a generated file is stale")
    args = parser.parse_args()
    try:
        agent = coach_agent()
        rendered = {COACH_AGENT_FILE: coach_agent_yaml(agent)}
        files = {RESOURCE: render(package()), FIXTURE: render(kinds()), COACH_RESOURCE: render(coach())}
        files.update(rendered)
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
        print(f"wrote {path.relative_to(APP)}")
    for path in stale:
        print(f"stale: {path.relative_to(APP)}", file=sys.stderr)
    return 1 if stale else 0


if __name__ == "__main__":
    sys.exit(main())
