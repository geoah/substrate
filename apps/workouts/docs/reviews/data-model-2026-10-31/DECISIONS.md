# Data-model decisions after the Fable and Astra reviews (2026-10-31)

> **Where this came from.** Part of the data-model review of 2026-10-31, kept verbatim
> ([the index](../../README.md#reviews)). It reviewed the draft package `mneme.rxtx.dev/training`
> in rxtx.dev's `research/workouts/substrate/training/`, the package that became the
> app's own `~/training` in [`Package/`](../../../Package/README.md). The worktree paths
> and `/tmp` paths it cites were on the build Mac and are not in this repository.

Reports: /tmp/spotter-data-review/fable/report.md and /tmp/spotter-data-review/astra/report.md. Read both; they cite
file and line evidence. The package is still a draft (never applied), so it stays at version 1 and every change below is
free now. Where the two reviews differ, the decision and the reason are given.

## Do now (before any real set is written)

D1. RPE provenance (both). Add `set.rpeMethod`, enum `entered` (rated by the lifter), `prefilled` (the plan's value shown
    pre-filled and logged unchanged), `copied` (copied from last time), `imported` (from another app, how it was entered
    unknown). Written whenever `rpe` is written; absent with `rpe`. Same idea for `rir`: `set.rirMethod`, same values.
    Fable's name (it reads like substrate's own `observation.recordingMethod`), Astra's fuller value list.
    Rules (Astra §1): tapping a value is `entered` even if it equals the plan; clearing removes rpe and rpeMethod; copying
    PREVIOUS is `copied`; equality never decides provenance. The suggestion engine treats a `prefilled` RPE as absent
    (falls back to the target as today, and the reason text says "assuming RPE 8", never "at RPE 8"); PRs/e1RM must not
    treat a prefilled value as a reported one. Chain folding keeps provenance per record.
D2. How a set was completed (Astra §1). `set.completionMethod`: `individual` (ticked or logged on its own), `bulk`
    (Finish's "mark them done as shown"), `imported`. Written on every set Spotter writes.
D3. Effort targets as ranges (Fable A2; Astra §11 agrees ranges are needed). In the planned set (routine.slots[].sets[])
    and in set.target, replace `rpe`/`rir` with `rpeMin`/`rpeMax` and `rirMin`/`rirMax` (one value = min equals max),
    matching repsMin/repsMax. The suggestion engine reads the range's top (rpeMax) where it read rpe. Nested names are
    permanent, so decide now.
D4. Setup identity (both). New fifth kind `mneme.rxtx.dev/training/setup` (Fable A3's shape: name, exercise reference
    mustExist, gym string, loadBase, loadStep, loadMax quantities kg/lb, notes; purpose supporting). `set.setup` becomes a
    reference to it (mustExist). Deleting a setup never cascades to sets. The exercise's load grid stays the default; a
    setup overrides it. Optional `resistanceLabel` string on the set for bands/levels that are not kilograms (Astra §6).
    V1 writes no setup yet; PREVIOUS and suggestions keep filtering by setup as designed.
D5. Relative loads (both). Planned set gains `loadPercent` (decimal 0..200) and `loadReference` (enum onerm, e1rm,
    trainingmax, bodymass). set.target gains `loadPercent`, `loadReference` and `referenceLoad` (the resolved reference
    quantity). Rule written into set.yaml and the README: `target.load` is always the resolved absolute load.
D6. Snapshot load meaning on each set (Astra §2): add top-level `loadBasis` (total | perimplement | perside),
    `implements` (int min 1), `laterality` (bilateral | unilateral | alternating), `repsBasis` (total | perside),
    `bodyShare` (decimal) copied from the exercise when logged, so editing the exercise never reinterprets history.
D7. Full prescription on the set (Astra §3): set.target gains `tempo`, `timerMode`, `myoStopRule`, `dropReps`,
    `intraSetPause` (the effective values), alongside what it already copies.
D8. Plan snapshot on the session (Astra §3): `session.slots[].sets[]`, the planned sets as resolved at Start (including
    ones never performed), with their keys; slot and group rests already live on the session. Written at Finish from the
    engine's state. Fits the four-level nesting limit (check with the loader).
D9. Effort scale (Astra §4): add `rirbased` to `rpeScale` and write it on every Spotter set. Fix the README's
    unconditional "RIR = 10 − RPE" (not valid for cr10).
D10. Time and rest provenance (Astra §5): `set.timeBasis` (completion | session | estimated) and `set.restMethod`
    (start | interaction | imported | estimated). Spotter writes `completion` and the method its rest measurement used.
D11. Sport (Astra §10, with Fable's "keep activityType"): add `session.sport` using health/workout's enum plus
    `sportSource` string; Spotter writes `strength`. Keep `activityType` (the Apple export choice) and document it as
    export-only.
D12. `timeZone` → `timezone` (Fable B2), matching core's recurring trait and the Apple Health provider.
D13. `set.continues` gets `mustExist: true` (Astra §8); the writer puts a chain's head before its members.

## App code now (correctness before V2)

A1. Codec reads substrate's canonical reference objects `{ref: "kind/id"}` (and decorated ones) as well as the string
    shorthand, recursively (Astra §7.1).
A2. Codec keeps unknown fields at every nesting level (an `extra` per nested model) and re-encodes them, so an older app
    never erases newer fields; an unknown enum value in an execution field means "unsupported", not a silent default
    (Astra §7.2).
A3. The app writes every new field above; the suggestion engine and presenters honour D1, D3 and D5; Apple Health never
    sends a sessionRPE of 0 as an effort score (Fable B1).

## Documentation now

- README mapping tables: apply every correction from both reports (Astra "Mapping-table corrections" 1-10, Fable "Errors
  and gaps"), including: target RPE is not Health Connect's AmrapTarget; setIndex maps to the chain head; FIT message_index
  is file-wide and wkt_step_index needs a step map; HKMetadataKeyWorkoutBrandName is not a title; Hevy updated_at is not a
  revision; Strong Workout # is an export sequence; FIT pauses need timer events; CR10 notes.
- codes.system registry with URIs (FHIR PHR pghd codes, healthconnect, fit, hevy, strong, wger, free-exercise-db).
- Soften "no standard carries strength down to the set" to "none of the evaluated standards carries all of it".
- Multi-writer rules for V2 (ifVersion on every edit, revision per record, one owning writer until a protocol exists,
  agent edits as proposals against a base version).

## Later (document as reserved, do not build)

Conditioning groups (emom, amraptime, intervals; interval, work, timeCap), cardio machines, programs above routines,
derived PRs and 1RMs (versioned, keyed by algorithm), import source preservation (`session.file` blobref, account-scoped
ids, reconcile rather than purge), fractional-second durations for FIT imports, per-set `source`/`adapter` when a second
device writes.
