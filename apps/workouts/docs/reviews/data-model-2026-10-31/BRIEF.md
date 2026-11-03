# Data-model review: Spotter's training package

> **Where this came from.** Part of the data-model review of 2026-10-31, kept verbatim
> ([the index](../../README.md#reviews)). It reviewed the draft package `mneme.rxtx.dev/training`
> in rxtx.dev's `research/workouts/substrate/training/`, the package that became the
> app's own `~/training` in [`Package/`](../../../package/README.md). The worktree paths
> and `/tmp` paths it cites were on the build Mac and are not in this repository.

Review the data model of Spotter, an iPhone strength-training app whose data lives on substrate (the owner's personal data
server). Do NOT edit the repository. Write your report to the file named in your task. You may make scratch copies under
/tmp to test ideas.

## What to read
Repository worktree: /Users/geoah/.t3/worktrees/rxtx.dev/t3-9e79ae4f
- The package (draft, not applied anywhere yet): research/workouts/substrate/training/ (bundle.yaml, exercise.yaml,
  routine.yaml, session.yaml, set.yaml, and README.md, which maps every property to a requirement and holds the field
  mappings to HealthKit, Health Connect, FIT, Hevy and Strong).
- Example records: research/workouts/substrate/examples/ (exercises, routines, sessions with every set, coverage).
- Requirements and decisions: research/workouts/README.md (requirements 1 to 39, disagreements, open questions),
  proposals/087-spotter.md (decisions taken, rollout, open questions).
- Research behind it: research/workouts/01-formats-and-standards.md (FHIR, HealthKit, Health Connect, FIT, Hevy, Strong,
  wger, Terra, open drafts), 02-training-methods.md (set structures, rest), 03-progression.md (what the suggestion engine
  reads), 05-substrate-fit.md (substrate shapes and limits).
- How the app reads and writes it: apps/apple/spotter/SpotterKit/Sources/SpotterCore/{Model,Codec}/ and API.md.
- Substrate itself at the commit production runs: /tmp/spotter-substrate-main (docs/data-model.md, traits.md, bundles.md;
  first-party kinds in kinds/substrate.reamde.dev/{health,observations,labs,clinical}; the loader in internal/vocabulary).
  Offline checker: /tmp/spotter-bin/substratectl validate -f FILE ... (add kinds/substrate.reamde.dev/health/*.yaml for the
  health reference).

## Context that changes the model now
The owner chose the "Rows" session screen. It PRE-FILLS the planned RPE on the set being logged, so one tap logs the set with
the plan's RPE. Today a set record cannot tell an RPE the lifter chose from one that was pre-filled and accepted (the plan's
target RPE is also copied onto each set's `target`). Decide how the model should capture this.

## Questions
1. Standardisation. Where can names, types, units, enums and structures line up better with established standards and
   with substrate's own first-party kinds (health/workout, observations/metric, labs, clinical) so that imports, exports
   and other consumers need less mapping? Consider FHIR R5 / the HL7 PHR IG PGHD Workout profile, LOINC/SNOMED/UCUM, Apple
   HealthKit and WorkoutKit, Android Health Connect (ExerciseSessionRecord, ExerciseSegment, PlannedExerciseSessionRecord),
   Garmin FIT (session, lap, set, workout, workout_step, exercise_category), Open mHealth / IEEE 1752.1, schema.org
   (ExerciseAction, ExercisePlan), and app formats (Hevy API, Strong CSV, wger). Be concrete: which field, renamed or
   retyped to what, and why.
2. Flexibility. What will this model fail to express, or express awkwardly, as use grows? For example: conditioning work
   in a session (intervals, EMOM, AMRAP for time, Tabata, cardio machines), percent-of-1RM and RIR-based plans, bands and
   assisted machines, unilateral and alternating work, tempo, carries and distance, programs above routines (blocks,
   mesocycles, deloads, progression schemes), multiple gyms and machine setups, more than one device logging, imports from
   Hevy/Strong/FIT/Health Connect, coach or agent proposed changes, plan versioning, PRs and derived values, the RPE
   provenance issue above.
3. Substrate fit and evolution. Upgrade risk (substrate refuses narrowing changes once records exist), references and
   cascades, ids, indices, record volume, what must be decided before real data accumulates versus what is cheap to add
   later.

## Report format (markdown)
- A short verdict.
- A prioritised list of recommended changes. For each: severity (do before real data / do soon / later), the exact change
  (field-level: before -> after, as YAML where useful), which standard or consumer it aligns with, why, and what it costs
  to do later instead of now.
- What should stay as is, and why (so good decisions are not churned).
- Anything in the README's mapping tables that is wrong.
- The RPE provenance recommendation, concretely.
Evidence matters: cite file paths and line numbers in this tree, substrate docs, or a primary source URL for standards.
