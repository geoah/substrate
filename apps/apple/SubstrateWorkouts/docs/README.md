# Substrate Workouts: the docs

Substrate Workouts is an iPhone app (iOS 27, Liquid Glass) for strength sessions in the gym.
It runs the owner's workout routines set by set, with supersets, drop sets, myo-reps, timed
holds, RPE, rest timers that alert when rest ends, and a suggested load and rep count for the
next set. It writes everything into the owner's substrate as records of a training package
the app keeps there, and saves each finished session to Apple Health as one strength
workout.

This folder holds what lies behind the app: the research, the designs it chose between, the
reviews and the decisions, so this repository stands on its own. How the app works today is
in [the app's README](../README.md), its build spec is [DESIGN.md](../DESIGN.md), and its
package is in [`Package/`](../Package/README.md).

## The documents

### Decisions

- [decisions.md](decisions.md): every product, design and data decision, with who took it,
  when, why and the source, and the questions still open.
- Decision record
  [0203](../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md):
  why the app keeps its own package under a `declare:` scope, the one server change it
  needed.

### Research

Written on 2026-10-30, before the first line of the app. Links and kind names were updated
for this repository; the text is as written.

- [research/README.md](research/README.md): the index and synthesis: short answers, the
  39 requirements, where the studies disagree, and the owner's open questions.
- [01 Formats and standards](research/01-formats-and-standards.md): FHIR, the HL7 IGs,
  HealthKit, WorkoutKit, Health Connect, FIT, Hevy, Strong, Fitbod, FitNotes, wger, Terra and
  the open drafts. Its verdict is that none carries strength work down to the set.
- [02 Training methods](research/02-training-methods.md): set structures, groups and the
  rest slots, the evidence on rest, RPE and RIR, and volume.
- [03 Progression](research/03-progression.md): estimating a 1RM, suggesting the next set,
  and the specification with test vectors that the suggestion engine implements.
- [04 The iOS platform](research/04-ios-platform.md): Liquid Glass, the rest timer with the
  phone locked, Live Activities, Apple Health, and keeping a session safe.
- [05 Fit with substrate](research/05-substrate-fit.md): the constraints, the precedents,
  one record per set, and linking a session to Apple Health.
- [06 App teardown](research/06-app-teardown.md): the owner's 13 Hevy screenshots, screen by
  screen, and what the app must do in the gym.

### Examples

- [examples/README.md](examples/README.md): 163 example records (the owner's exercises and
  routines, three sessions, and test vectors for every method). The page also says how they
  relate to the JSON the app bundles.
- [`exercises.yaml`](examples/exercises.yaml), [`routines.yaml`](examples/routines.yaml),
  [`sessions.yaml`](examples/sessions.yaml), [`coverage.yaml`](examples/coverage.yaml): the
  records, in `substratectl apply -f` form under `~/training`.

### Design

- [design/README.md](design/README.md): the six session-screen options, how Rows was chosen,
  the tap counts and the comparison page.
- [A. Table](design/table.md), the first build's layout; [B. Big type](design/large-text.md),
  rejected (its spec is [`design-spec.txt`](design/large-text/design-spec.txt));
  [C. Rows](design/rows/README.md), chosen; [D. Chips](design/chips/README.md);
  [E. Direct](design/direct/README.md); [F. Set list](design/codex/README.md).
- `screenshots/`: the app as built. `signed-*.jpg` show the signed-in flow
  against a substrate built from #1100. `rows-*.jpg` show the Rows layout from the rxtx.dev
  build. The app's README shows them.

### Reviews

- [reviews/ui-2026-10-31-astra.md](reviews/ui-2026-10-31-astra.md): Codex Astra's UI/UX
  review of the first build, and what was fixed.
- [reviews/data-model-2026-10-31/](reviews/data-model-2026-10-31/DECISIONS.md): Fable's and
  Astra's reviews of the data model, from the same [brief](reviews/data-model-2026-10-31/BRIEF.md).
  The reports are [fable.md](reviews/data-model-2026-10-31/fable.md) and
  [astra.md](reviews/data-model-2026-10-31/astra.md), and
  [DECISIONS.md](reviews/data-model-2026-10-31/DECISIONS.md) holds the decisions taken from
  them.
- [reviews/rows-build-2026-10-31.md](reviews/rows-build-2026-10-31.md): the data and UI
  reviewers' verdicts on the build of those decisions and of Rows, and the fixes.
- [reviews/pr-1100-2026-11-01/](reviews/pr-1100-2026-11-01/fixes.md): Codex Astra's and
  Fable's reviews of #1100, which brought the app here. It holds the
  [brief](reviews/pr-1100-2026-11-01/BRIEF.md), the reports
  ([astra.md](reviews/pr-1100-2026-11-01/astra.md),
  [fable.md](reviews/pr-1100-2026-11-01/fable.md)), and
  [fixes.md](reviews/pr-1100-2026-11-01/fixes.md), which goes finding by finding.

### History

- [history/087-spotter-proposal.md](history/087-spotter-proposal.md): proposal 087, written
  for rxtx.dev when the app was Spotter. It is kept as a record and superseded by this
  folder.

## History

Dates are UTC.

- **2026-10-30. The request.** The owner asked for research into a common format for workout
  data and for an iPhone app to run strength sessions on substrate. The routines would come
  from the console's agent, and the owner's Hevy screenshots served as inspiration. In
  rxtx.dev the lead had the six research slices written, drafted the package
  `mneme.rxtx.dev/training` with example records, wrote proposal 087 and the build spec, and
  built the first version. Its name was **Spotter**.
- **2026-10-31. The first build.** geoah/rxtx.dev#112 merged Spotter V1: bundled routines,
  sessions kept on the phone, and the Table layout. It was released as a pre-release on
  apps.internal.rxtx.dev. The owner set Apple's own strength features aside.
  geoah/rxtx.dev#113 fixed what the first simulator run and Codex Astra's UI/UX review found,
  and added screenshots.
- **2026-10-31. The UI exploration.** The owner asked for something less like Hevy and
  rejected the large-text redesign. Four options came from the second round, and a
  comparison page was published for a friend. The owner chose **Rows**.
- **2026-10-31. The data-model reviews.** Fable and Codex Astra reviewed the package, and the
  lead took decisions D1 to D13 from their reports. geoah/rxtx.dev#118 built them and Rows,
  then fixed what a data reviewer and a UI reviewer found. #113 and #118 were left open in
  rxtx.dev, since the app's work moved here.
- **2026-11-01. The move to substrate.** On the owner's request the app moved into this
  repository as **Substrate Workouts** (#1100). It signs in with the app authorization flow,
  and it owns its package: the owner ruled that the kinds are installed and managed by the
  app and do not ship with substrate. That needed one server change, the `declare:` scope
  (decision 0203). A pre-release went to the internal catalogue, which rxtx.dev lists
  (geoah/rxtx.dev#140).
- **2026-11-01. The reviews of #1100.** Codex Astra and Fable reviewed #1100. Every major was
  fixed (`a7723525`, `50742b51`, `22b23a47`, `492044ec`), and four questions were left for
  the owner.
- **2026-11-02. Merged.** The owner had #1100 merged once reviewed (`d3642e82`), then asked
  for this folder.
