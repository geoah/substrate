"""Turn the exercise library's sources into the records Substrate Workouts seeds.

Substrate Workouts writes its exercise library into the repository's training
package (movements, and their exercises, one per equipment version) and fills
what the repository lacks at each library version. This script builds that
library from two sources in apps/workouts/library/:

- movements.yaml, curated by hand: each movement, its muscles and their
  shares, and its exercises with their equipment, measurement and load
  conventions, the free-exercise-db entries that are the same exercise, and
  the Hevy template ids that map onto it.
- free-exercise-db.json, the strength entries of free-exercise-db (Unlicense),
  trimmed to the fields used here, at the commit pinned below. An entry no
  curated exercise claims becomes a movement and exercise of its own, with
  the muscle groups free-exercise-db gives and no shares.

Each record is named by a slug of its name (`chest-press`), its library
code, and its id is derived from that slug in the minted shape a server
accepts from an app (`derived_minted_id`), so every build writes a record
under the same id and a name is never an id. The references between them
name the derived ids.

The library's version is derived, never bumped by hand: library.lock.json
keeps the version, the hash of the records, and the version each record's
slug first appeared in (`since`), which the seeding reads so an entry the
owner deleted is never written back. A changed hash moves the version on the
next write, and --check fails until it has.

    uv run --with pyyaml apps/workouts/scripts/exercise-library.py            # rewrite
    uv run --with pyyaml apps/workouts/scripts/exercise-library.py --check    # exit 1 and name each stale file
    uv run --with pyyaml apps/workouts/scripts/exercise-library.py --fetch    # refresh free-exercise-db.json from the pinned commit
"""

from __future__ import annotations

import argparse
import hashlib
import json
import re
import sys
import urllib.request
from pathlib import Path

import yaml

APP = Path(__file__).resolve().parents[1]
LIBRARY = APP / "library"
MOVEMENTS = LIBRARY / "movements.yaml"
FEDB = LIBRARY / "free-exercise-db.json"
LOCK = LIBRARY / "library.lock.json"
RESOURCE = APP / "WorkoutsKit/Sources/WorkoutsCore/Resources/Bundled/library.json"
EXERCISE_KIND = APP / "package/exercise.yaml"

FEDB_REPOSITORY = "yuhonas/free-exercise-db"
FEDB_COMMIT = "f00c92c7dcf1216a928a52c3706c7ce8e2f71ed5"
FEDB_CATEGORIES = ("strength", "powerlifting", "olympic weightlifting", "strongman")
FEDB_FIELDS = ("id", "name", "equipment", "category", "force", "mechanic", "primaryMuscles", "secondaryMuscles")

MOVEMENT = "substrate.reamde.dev/training/movement"
EXERCISE = "substrate.reamde.dev/training/exercise"
# The code systems an entry carries (exercise.yaml, `codes`).
LIBRARY_SYSTEM = "substrate-workouts-library"
FEDB_SYSTEM = "free-exercise-db"
HEVY_SYSTEM = "hevy"
# The first part of every library id (`derived_minted_id`), so no other
# writer deriving from the same slug lands on the library's record.
ID_NAMESPACE = "workouts-library"
# The references a record holds, by property, and the kind each names.
REFERENCES = {"movement": MOVEMENT, "variationOf": EXERCISE}
CROCKFORD = "0123456789abcdefghjkmnpqrstvwxyz"

MOVEMENT_KEYS = {"id", "name", "aliases", "force", "mechanic", "muscles", "exercises"}
EXERCISE_KEYS = {"name", "aliases", "equipment", "measurement", "laterality", "loadBasis", "implements", "restClass", "hevy", "fedb", "muscles"}
SHARES = {0.25, 0.5, 0.75, 1}
# free-exercise-db's equipment and muscle groups in the package's spelling.
FEDB_EQUIPMENT = {
    "barbell": "barbell", "dumbbell": "dumbbell", "cable": "cable", "machine": "machine", "kettlebells": "kettlebell",
    "bands": "resistanceband", "body only": "none", "e-z curl bar": "barbell", "medicine ball": "other",
    "exercise ball": "other", "foam roll": "other", "other": "other",
}
FEDB_GROUPS = {"lower back": "lowerback", "middle back": "upperback"}


class GenerateError(Exception):
    pass


def slug(text: str) -> str:
    """ExerciseID.slug's rule: ASCII letters and digits, every other run one `-`."""
    return re.sub(r"[^a-z0-9]+", "-", text.lower()).strip("-") or "exercise"


def derived_minted_id(*parts: str) -> str:
    """substrate's DerivedMintedID (WorkoutsCore's too): the first 16 bytes
    of the SHA-256 of the parts joined by a NUL, marked as a UUIDv8, as 26
    lowercase Crockford base32 characters, the first 0 to 7."""
    digest = bytearray(hashlib.sha256("\x00".join(parts).encode("utf-8")).digest()[:16])
    digest[6] = digest[6] & 0x0F | 0x80
    digest[8] = digest[8] & 0x3F | 0x80
    value = int.from_bytes(digest, "big")
    return "".join(CROCKFORD[(value >> (5 * (25 - index))) & 31] for index in range(26))


def library_id(kind: str, slug_id: str) -> str:
    """A library record's id: derived from its kind's name and its slug."""
    return derived_minted_id(ID_NAMESPACE, kind.rsplit("/", 1)[1], slug_id)


def derive_ids(records: list[dict]) -> None:
    """Each record's slug id replaced by its derived id, in the record and in
    every reference to it, in place."""
    for record in records:
        for key, kind in REFERENCES.items():
            value = record["properties"].get(key)
            if value is None:
                continue
            if not value.startswith(kind + "/"):
                raise GenerateError(f"{record['id']}: {key} names {value}, not a {kind}")
            record["properties"][key] = f"{kind}/{library_id(kind, value[len(kind) + 1:])}"
        record["id"] = library_id(record["kind"], record["id"])


def decimal(value: float) -> str:
    """Substrate's decimal spelling: the shortest plain digits."""
    text = repr(float(value))
    return text[:-2] if text.endswith(".0") else text


def muscle_groups() -> dict[str, str]:
    """Each muscle's group, read from the app's MuscleDetail labels' source:
    the muscles' order in exercise.yaml and the groups in muscles.yaml."""
    with open(LIBRARY / "muscles.yaml", encoding="utf-8") as handle:
        rows = yaml.safe_load(handle)
    declared = yaml.safe_load(EXERCISE_KIND.read_text(encoding="utf-8"))["data"]["properties"]["muscles"]["fields"]["muscle"]["values"]
    names = [entry["value"] for entry in declared]
    if sorted(names) != sorted(rows):
        raise GenerateError(f"muscles.yaml and exercise.yaml name different muscles: {sorted(set(names) ^ set(rows))}")
    return rows


def group_lists(shares: dict[str, float], groups: dict[str, str]) -> tuple[list[str], list[str]]:
    """Exercise.groupLists' rule: a group's share is its largest muscle's;
    primary from 0.75, secondary from 0.25, in the muscles' order."""
    best: dict[str, float] = {}
    for muscle, share in shares.items():
        best[groups[muscle]] = max(best.get(groups[muscle], 0), share)
    primary = [group for group, share in best.items() if share >= 0.75]
    secondary = [group for group, share in best.items() if 0.25 <= share < 0.75]
    return primary, secondary


def shares_list(shares: dict[str, float]) -> list[dict]:
    return [{"muscle": muscle, "involvement": decimal(share)} for muscle, share in shares.items()]


def check_movement(movement: dict, groups: dict[str, str]) -> None:
    where = movement.get("name", "?")
    if unknown := set(movement) - MOVEMENT_KEYS:
        raise GenerateError(f"movement {where}: unknown keys {sorted(unknown)}")
    if movement.get("id") != slug(movement.get("name", "")):
        raise GenerateError(f"movement {where}: its id must be {slug(movement.get('name', ''))}")
    for muscle, share in (movement.get("muscles") or {}).items():
        if muscle not in groups or share not in SHARES:
            raise GenerateError(f"movement {where}: {muscle}: {share}")
    for exercise in movement.get("exercises") or []:
        if unknown := set(exercise) - EXERCISE_KEYS:
            raise GenerateError(f"exercise {exercise.get('name')}: unknown keys {sorted(unknown)}")
        for muscle, share in (exercise.get("muscles") or {}).items():
            if muscle not in groups or share not in SHARES:
                raise GenerateError(f"exercise {exercise.get('name')}: {muscle}: {share}")


def curated(fedb: dict[str, dict], groups: dict[str, str]) -> tuple[list[dict], list[dict], set[str]]:
    with open(MOVEMENTS, encoding="utf-8") as handle:
        movements = yaml.safe_load(handle) or []
    movement_records, exercise_records, claimed, hevy = [], [], set(), {}
    for movement in movements:
        check_movement(movement, groups)
        shares = movement.get("muscles") or {}
        primary, secondary = group_lists(shares, groups)
        properties = {"name": movement["name"]}
        if movement.get("aliases"):
            properties["aliases"] = movement["aliases"]
        for key in ("force", "mechanic"):
            if movement.get(key):
                properties[key] = movement[key]
        if shares:
            properties["muscles"] = shares_list(shares)
        if primary:
            properties["primaryMuscles"] = primary
        if secondary:
            properties["secondaryMuscles"] = secondary
        movement_records.append({"kind": MOVEMENT, "id": movement["id"], "properties": properties})
        for exercise in movement["exercises"]:
            own = exercise.get("muscles") or {}
            primary, secondary = group_lists(own or shares, groups)
            item = {"name": exercise["name"]}
            if exercise.get("aliases"):
                item["aliases"] = exercise["aliases"]
            item["measurement"] = exercise["measurement"]
            item["equipment"] = exercise["equipment"]
            for key in ("laterality", "loadBasis", "implements"):
                if key in exercise:
                    item[key] = exercise[key]
            if primary:
                item["primaryMuscles"] = primary
            if secondary:
                item["secondaryMuscles"] = secondary
            if own:
                item["muscles"] = shares_list(own)
            item["restClass"] = exercise["restClass"]
            item["movement"] = f"{MOVEMENT}/{movement['id']}"
            codes = []
            for entry in exercise.get("fedb") or []:
                if entry not in fedb:
                    raise GenerateError(f"exercise {exercise['name']}: free-exercise-db has no strength entry {entry}")
                if entry in claimed:
                    raise GenerateError(f"exercise {exercise['name']}: free-exercise-db's {entry} is claimed twice")
                claimed.add(entry)
                codes.append({"system": FEDB_SYSTEM, "code": entry, "display": fedb[entry]["name"]})
            for entry in exercise.get("hevy") or []:
                if entry in hevy:
                    raise GenerateError(f"exercise {exercise['name']}: Hevy's {entry} is mapped to {hevy[entry]} as well")
                hevy[entry] = exercise["name"]
                codes.append({"system": HEVY_SYSTEM, "code": entry})
            if codes:
                item["codes"] = codes
            exercise_records.append({"kind": EXERCISE, "id": slug(exercise["name"]), "properties": item})
    return movement_records, exercise_records, claimed


def uncurated(fedb: dict[str, dict], claimed: set[str], taken: set[str]) -> tuple[list[dict], list[dict]]:
    """Each free-exercise-db entry no curated exercise claims, as a movement
    and an exercise of its own named as free-exercise-db names it."""
    movements, exercises = [], []
    for entry in sorted(set(fedb) - claimed):
        source = fedb[entry]
        name = source["name"]
        ident = slug(name)
        while ident in taken:
            ident += "-fedb"
        taken.add(ident)
        lower = name.lower()
        equipment = FEDB_EQUIPMENT.get(source["equipment"] or "")
        static = source["force"] == "static"
        bodyweight = equipment in (None, "none")
        measurement = ("duration" if static else "bodyweightreps") if bodyweight else ("weightduration" if static else "weightreps")
        one_side = any(word in lower for word in ("one-arm", "one arm", "single-arm", "single arm", "one-leg", "one leg", "single-leg", "single leg"))
        groups = [FEDB_GROUPS.get(group, group) for group in source["primaryMuscles"]]
        also = [FEDB_GROUPS.get(group, group) for group in source["secondaryMuscles"] if FEDB_GROUPS.get(group, group) not in groups]
        movement = {"name": name}
        if source["force"]:
            movement["force"] = source["force"]
        if source["mechanic"]:
            movement["mechanic"] = source["mechanic"]
        if groups:
            movement["primaryMuscles"] = groups
        if also:
            movement["secondaryMuscles"] = also
        movements.append({"kind": MOVEMENT, "id": ident, "properties": movement})
        item = {"name": name, "measurement": measurement}
        if equipment:
            item["equipment"] = equipment
        if one_side:
            item["laterality"] = "unilateral"
        elif "alternat" in lower:
            item["laterality"] = "alternating"
        if equipment in ("dumbbell", "kettlebell") and not static:
            item["loadBasis"] = "perimplement"
            item["implements"] = 1 if one_side or "goblet" in lower else 2
        if groups:
            item["primaryMuscles"] = groups
        if also:
            item["secondaryMuscles"] = also
        category = source["category"]
        item["restClass"] = (
            "timed" if static else "power" if category == "olympic weightlifting" else "carry" if category == "strongman"
            else "heavycompound" if category == "powerlifting" else "isolation" if source["mechanic"] == "isolation" else "compound"
        )
        item["movement"] = f"{MOVEMENT}/{ident}"
        item["codes"] = [{"system": FEDB_SYSTEM, "code": entry, "display": name}]
        exercises.append({"kind": EXERCISE, "id": ident, "properties": item})
    return movements, exercises


def library() -> tuple[dict, str | None]:
    groups = muscle_groups()
    fedb = {entry["id"]: entry for entry in json.loads(FEDB.read_text(encoding="utf-8"))["exercises"]}
    movements, exercises, claimed = curated(fedb, groups)
    seen: dict[str, str] = {}
    for record in movements + exercises:
        key = f"{record['kind']}/{record['id']}"
        if key in seen:
            raise GenerateError(f"{key} is written by both {seen[key]} and {record['properties']['name']}")
        seen[key] = record["properties"]["name"]
    taken = {record["id"] for record in movements} | {record["id"] for record in exercises}
    more_movements, more_exercises = uncurated(fedb, claimed, taken)
    records = movements + more_movements + exercises + more_exercises
    # The lock and the library code stay keyed by the slug, so a record
    # first written under its slug id is the same entry under its derived one.
    slugs = [record["id"] for record in records]
    derive_ids(records)

    digest = hashlib.sha256(json.dumps(records, sort_keys=True).encode("utf-8")).hexdigest()
    lock = json.loads(LOCK.read_text(encoding="utf-8")) if LOCK.exists() else {"version": 0, "sha256": "", "since": {}}
    version, since = lock["version"], dict(lock["since"])
    if lock["sha256"] != digest:
        version += 1
    keys = [f"{record['kind']}/{slug_id}" for record, slug_id in zip(records, slugs)]
    for key in keys:
        since.setdefault(key, version)
    since = {key: since[key] for key in sorted(since) if key in set(keys)}
    new_lock = {"version": version, "sha256": digest, "since": since}
    for record, slug_id, key in zip(records, slugs, keys):
        coding = {"system": LIBRARY_SYSTEM, "code": slug_id, "version": str(since[key])}
        record["properties"]["codes"] = [coding] + record["properties"].get("codes", [])
    out = {
        "source": "apps/workouts/library",
        "version": version,
        "freeExerciseDB": {"repository": FEDB_REPOSITORY, "commit": FEDB_COMMIT},
        "records": records,
    }
    return out, (render(new_lock) if new_lock != lock else None)


def fetch() -> None:
    url = f"https://raw.githubusercontent.com/{FEDB_REPOSITORY}/{FEDB_COMMIT}/dist/exercises.json"
    with urllib.request.urlopen(url, timeout=60) as response:
        entries = json.load(response)
    kept = [{field: entry.get(field) for field in FEDB_FIELDS} for entry in entries if entry.get("category") in FEDB_CATEGORIES]
    FEDB.write_text(
        render({"source": f"https://github.com/{FEDB_REPOSITORY} at {FEDB_COMMIT}, dist/exercises.json, The Unlicense",
                "categories": list(FEDB_CATEGORIES), "exercises": sorted(kept, key=lambda entry: entry["id"])}),
        encoding="utf-8",
    )
    print(f"wrote {FEDB.relative_to(APP)}: {len(kept)} entries")


def render(value: dict) -> str:
    return json.dumps(value, ensure_ascii=False, indent=1) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument("--check", action="store_true", help="exit 1 if a generated file is stale")
    parser.add_argument("--fetch", action="store_true", help="refresh free-exercise-db.json from the pinned commit")
    args = parser.parse_args()
    try:
        if args.fetch:
            fetch()
        value, lock = library()
        files = {RESOURCE: render(value)}
        if lock is not None:
            files[LOCK] = lock
    except (GenerateError, OSError, yaml.YAMLError, KeyError) as error:
        print(f"exercise-library.py: {error}", file=sys.stderr)
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
