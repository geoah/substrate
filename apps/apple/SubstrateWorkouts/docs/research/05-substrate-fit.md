# Spotter on substrate: where the data lives

> **Where this came from.** Written on 2026-10-30 in rxtx.dev, as `research/workouts/`, for
> the app then called Spotter (Substrate Workouts now); the version here is the one the
> app's second build used (geoah/rxtx.dev#118). It was carried into this repository on
> 2026-11-02 as written, with two changes. The links resolve inside this repository. The
> draft kind paths `mneme.rxtx.dev/training/<kind>` now read as the app-owned
> `~/training/<kind>` they became (decision record [0203](../../../../../docs/decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)). Where the
> text names the package home it recommended, a Mneme package `mneme.rxtx.dev/training`,
> that name is left as written: the home changed later, and the reasons are in
> [decisions.md](../decisions.md). Mentions of Mneme, rxtx.dev paths or
> `apps/apple/spotter` describe the plan at the time. The package now lives in
> [`Package/`](../../Package/README.md), and the app's spec is [DESIGN.md](../../DESIGN.md).

Research slice 05. Date: 2026-10-30.

The question: how should Spotter's routines, sessions and sets live in
substrate, the owner's personal data server, so that the app can write a
session set by set, read the history of one exercise, link each session to
the strength workout Apple Health holds, and leave room for imports from
other systems later.

Scope. Substrate is read at the commit production runs (`main` `ffd7b967`,
exported read-only at `/tmp/spotter-substrate-main`). Mneme v6 is read in this
worktree (`svc/mneme-v6`). Nothing was written to any substrate. Candidate
manifests were checked with the offline checker built from production's
commit (`/tmp/spotter-bin/substratectl validate`); they are sketches in a
scratch directory, not files in this repository.

Evidence markers:

- **[src]** checked today in source code, manifests or docs in this machine's
  checkouts (substrate at `ffd7b967`, this rxtx.dev worktree, the substrate
  git history read with `git show`).
- **[sdk]** checked today in the iOS 27.2 SDK headers.
- **[web]** checked today on the web.
- **[recalled]** from memory, not re-checked.
- **[unverified]** an estimate or a claim I could not confirm.

This builds on the prior research in
`/tmp/spotter-substrate-main/docs/plans/health-data/`, chiefly
`02-workouts.md` (platforms and formats, 2026-10-08) and `06-substrate-fit.md`
(the engine's write and read costs, 2026-10-08, on an older branch). Where
those documents say something the engine has since changed, this one says so.

## Summary

- **Sets must be their own kind to be queryable by value.** A list filter
  reaches only a kind's top-level properties. Objects and keyed maps are
  documented as outside the filter grammar, there are no dotted paths, and the
  aggregate read's property mode refuses a repeated or nested property
  [src]. The one thing that does reach inside a repeated object is the
  `referencing` reverse read, which is index-backed and can answer "which
  sessions included exercise X", but not "which sets of X were over 100 kg".
- **The engine has grown what the October research said it lacked.** It now
  has a `quantity` type with exact unit conversion (kg and lb join), and an
  aggregate read whose property mode reduces a top-level number, quantity or
  duration per calendar bucket, grouped by a reference such as `exercise`
  [src]. Both work for a set kind and for neither embedded shape.
- **Three shapes were compared**: one record per session with everything
  embedded; one record per set beside a session that embeds its exercise
  slots; one record per exercise performance with its sets embedded. At 3 to
  4 sessions a week they make about 150 to 210, 3,900 to 5,200, and 1,100 to
  1,500 records a year. All three are small next to what the owner's
  repository already holds (2.17 M changelog entries, 336k records, per
  `06-substrate-fit.md`).
- **Recommended: one record per set** (`session` + `set`, with `exercise` and
  `routine`). It is the only shape where the server answers per-set questions
  (an indexed list per exercise, weekly heaviest load and set counts by
  aggregate), where phone and watch write without contending for one array,
  and where every import source (Strong CSV rows, FIT `set` messages, Health
  Connect segments, Hevy's own per-set exercise history) maps one to one.
  Its costs: a session takes two reads to render, an offline flush is one
  request per set (there is no batch write), and after about two years the
  set kind passes the 10,000-record ceiling on in-place vocabulary
  conversions, so later renames go through expand-and-contract.
- **Package home: a Mneme package now, `mneme.rxtx.dev/training`**, shaped to
  the first-party conventions (`about`, `source`, `origin`, `revision`,
  `utcOffsetStart`, quantities with declared units, FHIR-style enum spelling)
  so that a later move into a first-party `substrate.reamde.dev` catalogue
  is a row copy with the same ids, as Mneme's hand-written health rows moved
  on 2026-10-27 (T-119, section 4.2). Move when a substrate provider needs to
  write it.
- **Apple Health link: Spotter never writes `health/workout` itself.** It saves
  one HealthKit strength workout per session, keeps its UUID on the session,
  and points at the `health/workout` that the Apple Health provider will build
  from it. That row's id is computable from the UUID
  (`applehealth/<account>/<UUID>`) [src]. Writing a second `health/workout`
  would duplicate the session in every list. Today nothing on the substrate
  side carries the session id back from HealthKit: Substrate Sync forwards
  only `HKTimeZone` in a workout's metadata [src].

## 1. The constraints that bind the design

### 1.1 Records, kinds, packages, ids

- A record's identity is `(kind, id)`; an id is unique per kind, never per
  repository. A kind is `<authority>/<package>/<name>`, named in full on every
  surface [src: `docs/data-model.md`].
- A package is one word under an authority, versioned as a unit; a broken
  closure quarantines its package alone [src: `docs/data-model.md`].
- **A writer may choose ids on create**, up to 128 characters of RFC 3986
  unreserved characters plus `:`, `@` and `/` [src: `docs/data-model.md`].
  The exception is a kind that a `recordmapping` targets: its ids are
  server-assigned and a create carrying an id is refused (decision 0049)
  [src: `docs/api.md`, Idempotency and retries]. Spotter's kinds must stay
  out of every mapping's target list to keep client-chosen ids.
- An id holding `/` must have each `/` percent-encoded, so the whole id
  travels as one path segment that the API decodes once [src: `docs/api.md`,
  REST resources]. Substrate Sync does this already (`Client.escape` drops
  `/` from the allowed set) [src].

### 1.2 Property types that matter here

All from `docs/data-model.md` unless marked [src].

- **`quantity`**: `{value: "72.5", unit: kg}`, the digits a string, the unit
  the id of a unit record in the repository. A property may declare
  `unit: kg` and `units: [kg, lb]`; a write in `lb` is stored in `lb` and
  converts exactly for filters, order and aggregates. `kg` and `lb` ship as
  units joined through `g` [src: `docs/units.md`]. This is the fix for issue
  #125 ("`workoutset.weight` carries no unit ... kg vs lb is ambiguous from
  the first record written", quoted in `06-substrate-fit.md`).
- **`duration`**: ISO 8601 without years or months. A list filter compares
  it as text, so `PT10M` sorts before `PT9M`: `typedValueSQL` has no case for
  durations [src: `internal/engine/query.go`]. The aggregate property read
  reduces durations exactly [src: `docs/api.md`, The property read].
- **`decimal`**: exact digits in a string; suits RPE (6, 7.5, 9.5).
- **`enum`**: values `[a-z][a-z0-9]*` with optional labels; a new value is an
  additive change [src: `svc/mneme-v6/docs/kind-design.md` §2].
- **`state`**: a declared machine. A create may name any state; a `put` that
  would move a state is refused ("patch does transitions"); a patch naming
  the current state is a no-op.
- **`object`**: declared `fields:`, nesting to level 4 (a kind's own
  property is level 1) [src: `internal/vocabulary/types.go`,
  `MaxFieldDepth = 4`]. Objects "validate recursively on write and stay out
  of the filter grammar until a consumer needs them". A field may not be a
  `state`, `secret`, `digest`, `blobref` or `json`.
- **`repeated`** holds a list; **`keyed`** holds a map; neither may be both.
- **`reference`**: stored as `{ref: "<kind>/<id>"}`. A pin (`kind:` or
  `trait:`) constrains the target. The target need not exist unless
  `mustExist: true`. `onDelete: cascade` makes the referent own the record;
  it is declarable only on a top-level single reference. Link `properties:`
  are flat scalars and are "not filterable today"
  [src: `svc/mneme-v6/docs/kind-design.md` §2].

### 1.3 What a list filter can reach

- **Top-level properties only.** `filter.properties` names one property per
  condition. A single reference filters by one indexed equality on its path,
  "an index the engine builds for every kind's single references without a
  declaration" [src: `docs/api.md`, The filter grammar].
- **Inside a repeated object: not by contract.** The docs keep objects and
  keyed maps out of the grammar. In code a filter on an object property
  falls through to `condJSON`, whose `contains` compiles to
  `props->'name' @> '[<operand>]'::jsonb` [src: `internal/engine/query.go`
  `condProp`, `condJSON`]. So
  `{"slots": {"contains": {"exercise": {"ref": "…"}}}}` would match today by
  JSON containment, unindexed, untested and undocumented. No comparison
  (`gt`) reaches a nested value, and there are no dotted paths. A design must
  not rely on it. `06-substrate-fit.md` reached the same reading on the older
  branch.
- **`referencing` does reach nested references.** The refs index derives a
  row for "every SITE a reference is declared at ... inside a repeated
  object's elements ... to the declared depth" [src:
  `internal/engine/refs.go`, `deriveRefs`]. The reverse read
  `filter.referencing: {ref, property}` lists the records pointing at a
  target and returns a `matches` sidecar with each site's path [src:
  `docs/api.md`, Who points at a record]. This is index-backed.
- **`expand` follows top-level reference properties only**
  [src: `internal/engine/expand.go`, `expandProperties`].
- **A list that bounds `at` on both ends becomes the window read**: `orderBy`
  must be `at`, and `count` and `offset` are refused [src: `docs/api.md`,
  The window read]. Reading a session's sets "between start and end" and
  ordering them by `order` therefore fails; filter by the session reference
  instead.
- Page size is 50 by default and 500 at most [src: `docs/api.md`,
  Pagination].

### 1.4 The aggregate read

`GET /api/v1/records?aggregate=` (decision 0161) [src: `docs/api.md`, The
aggregate read]:

- The **property read** reduces "a declared `int`, `float`, `decimal`,
  `quantity`, `money` or `duration` property of the filtered kinds" with
  `count`, `sum`, `avg`, `min`, `max`, `stddev`, in exact arithmetic, bucketed
  by an anchor (`at` by default) per `every`, calendar hour, day, week or
  month in a zone, or the recorded-local calendar through a declared
  UTC-offset property. It takes the whole filter grammar and
  `property.groupBy` over declared string, enum or reference properties.
  Discovery lists the `aggregate` and `units` features as `alpha`
  [src: `internal/api/discovery.go`].
- It **refuses a repeated or keyed property** ("holds many values; a
  property read reduces one per record") and looks the name up as a top-level
  property only [src: `internal/engine/aggregateproperty.go`]. Records
  without the property are skipped (`jsonb_exists`) [src: same file].
- It cannot multiply. Volume (load times reps) is not readable unless a
  record carries it.
- Budgets: `maxBuckets` 1,440 by default (series times buckets, cap 10,000),
  `maxGroups` 200, `maxSamples` 1,000,000; one deadline of 30 seconds; two
  aggregate reads at once per server [src: `docs/api.md`, Budgets]. A year of
  weekly rows for 60 exercises at once needs a raised `maxBuckets`: calendar
  2026 cut into Monday weeks is 53 buckets (the first and last clipped), so
  60 times 53 is 3,180 cells.
- The same body is available to functions (`host.records.aggregate`), to
  agents (the `query` tool's `aggregate` argument) and to the CLI
  (`substratectl aggregate --property …`) [src: `docs/api.md`,
  `docs/substratectl.md`].

### 1.5 Writes

From `docs/api.md` (The five mutations, Idempotency and retries, The
canonical envelope) unless marked.

- **One record per request.** There is no multi-record REST write;
  `health-apps.md` lists it as an open item (C-14) [src].
- `PUT /api/v1/{authority}/{package}/{kind}/{id}` creates or upserts; it
  merges per property and never prunes. A retried `PUT` lands the same row.
- `PATCH` replaces each named property **whole** (merge at depth one). A
  `null` deletes. A patch to one set inside an embedded list resends the whole
  list.
- `ifVersion` on `put` and `patch` is compare-and-set; a mismatch is `409`.
- `POST /api/v1/records` mints an id; it is safe to retry only with
  `Idempotency-Key`. Spotter does not need it if it chooses ids.
- Request bodies are capped at 1 MiB [src: `internal/api/core.go`,
  `maxRequestBody`].
- A function call commits all its effects in one transaction, so a batch
  writer is possible as a Mneme function called with `Idempotency-Key`
  [src: `docs/api.md`; `06-substrate-fit.md` §8].

### 1.6 Reading changes

- Every list answers `head` and `generation`; `GET /api/v1/changes?watch=1`
  with `from={head}&generation={generation}` resumes with no gap and no
  double-see. `GET /api/v1/records?watch=1&filter={"kinds":[…]}` tails the
  same frames for a set of kinds [src: `docs/changelog.md`, Watching;
  `docs/api.md`].
- A deleted record leaves ordinary lists at once and is a tombstone, listed
  only by `filter.deleted: true`, until the collector's next pass (every five
  minutes) purges it [src: `docs/api.md`, The five mutations, The filter
  grammar]. A poller that wants deletions must read the change feed, not
  `deleted: true` lists.

### 1.7 Tokens and addressing

- `POST /login` with repository, password and TOTP code mints a token
  record and returns its secret once; the label names the device [src:
  `docs/auth.md`].
- "A token has full access to its repository. There are no scopes, no
  roles, no ACLs" [src: `docs/auth.md`]. A lost phone's token is revoked with
  `DELETE /tokens/{id}`.
- There is no repository segment in any URL: the bearer token names the
  repository [src: `docs/api.md`]. Production is `https://substrate.rxtx.dev`,
  repository `geoah.me` [src: `svc/mneme-v6/CLAUDE.md`].
- Writes under a token are actor `api` at the owner tier; each token is its
  own principal in the changelog [src: `docs/changelog.md`].

### 1.8 Changing the vocabulary later

- Adding a kind, an optional property, an enum value or an optional field is
  additive.
- Narrowing is refused while records hold the old shape, with a count
  [src: `docs/data-model.md`; `internal/engine/schemadiff.go`].
- `renamedFrom:` renames a kind's own property in place. **It is refused on
  an object field**: "renamedFrom is only for a type's own property, not a
  field" (checked with the offline validator) [src].
- A `default:` beside a new `required:` backfills a kind's own property, but
  "nothing walks a list or a map inside a row to rewrite one position of it"
  [src: `internal/engine/schemadiff.go`, `fieldBackfillHint`]. A nested field
  that becomes required must be rewritten record by record by the client.
- **A conversion that rewrites more than 10,000 live records is refused**
  (`SUBSTRATE_CONVERSION_CEILING`, default 10,000); the way through is
  expand-and-contract: declare the new shape beside the old, move records
  through ordinary writes, then contract [src: `docs/vocabulary.md`;
  `internal/config/config.go`]. Issue #157 (chunked conversions) was open on
  2026-10-08 per `06-substrate-fit.md`.
- A declared index on a quantity property is refused by the server ("a
  quantity takes no index: its order converts through each repository's
  unit graph") [src: `internal/engine/indices.go`]. The offline validator
  does not catch this; the server does.

### 1.9 What one write costs

`06-substrate-fit.md` §1 measured a changelog line at roughly 1,900 to 2,500
bytes for a 9-property record and synthesized about 1,000 bytes for a
two-field one,
dominated by one `manager` effect of 160 to 230 bytes per written property.
Verify ran at about 1 ms per entry on the owner's repository (issue #761).
At Spotter's volumes these costs do not decide anything; they are used below
only to size the shapes.

## 2. Precedents

### 2.1 The deleted `samples/fitness` package

Substrate commit `f47c21f5` (2026-09-10, "remove the seven sample packages
nothing imports") deleted `samples/fitness` [src: `git show f47c21f5^:…`].
It held five kinds:

- `exercise`: name, `activityType` enum, equipment string, primary and
  secondary muscles as repeated strings, `defaultWeightIncrement` float,
  details.
- `workouttemplate`: name, `estimatedDuration`, `defaultRest`, and
  `exercises` as a repeated reference. "Starting a workout from a template
  copies nothing; the workout just points back here, and set order lives on
  the sets."
- `workout`: "One training session. With an RRULE it is the recurring plan
  ... without one it is a single session", plus tracker totals as floats
  (`strain`, `averageHeartRate`, `energyKcal`, `distanceMeters`).
- `workoutset`: **its own kind**, `temporal(point)` at when it was performed,
  `number`, `setType` (normal, warmup, dropset, amrap, failure), `weightKg`
  float, `reps`, `duration`, `distanceMeters`, `rpe` float, a required
  cascading `workout` reference and an `exercise` reference.
- `workoutlog`: done or skipped against an occurrence of a recurring
  workout.

Two lessons were paid for there (quoted in `06-substrate-fit.md`):

- **#125**: the load had no unit. Version 5 renamed `weight` to `weightKg`,
  which fixes the ambiguity by name only. A `quantity` with `unit: kg` fixes
  it in the type.
- **#275**: `workout` was both the plan and the session; "tracker totals on
  a plan row mean nothing". Plan and session must be two kinds.

Mneme's own copy (`mneme.rxtx.dev/health/workoutset`, removed in T-110,
commit `311b7fee`) kept the same split: one record per set, `weight: float`
"in kg", `durationSeconds` and `distanceMeters` floats, a declared index on
`[workout, order]`, `purpose: supporting` [src: `git show 311b7fee^:…`]. Both
precedents chose a set kind; neither had units, quantities or an aggregate
read to use it with.

### 2.2 The first-party `health/workout`

`substrate.reamde.dev/health/workout` (package `health` version 2) is one
workout from any source [src: `kinds/substrate.reamde.dev/health/workout.yaml`]:

- Traits `temporal(range)` and core's `revised` (a rewrite never lowers
  `revision`).
- `sport` enum of 19 values including `strength`; `sportSource` keeps the
  source's word.
- `source` (pinned at `observations/source`), `about` (absent means the
  owner), `origin` (the provider record it was built from, any kind),
  `revision`, `adapter` (`<package>@<version>`).
- `utcOffsetStart` and `utcOffsetEnd` in minutes, which the aggregate read's
  recorded-local calendar uses.
- `duration`, and quantities with declared units: `distance` (m),
  `activeEnergy` and `totalEnergy` (kJ), heart rates (`1/min`),
  `elevationGain` (m); `strain` decimal; `zoneDurations` keyed durations;
  `notes`; `file` blobref.
- **No exercise, set or rep fields.** This matches HealthKit, which has no
  set, rep or load type: the iOS 27.2 headers mention strength only in the
  activity types `TraditionalStrengthTraining` (50) and
  `FunctionalStrengthTraining` (20) [sdk].

### 2.3 The FHIR-shaped clinical catalogue

`substrate.reamde.dev/clinical` (decision 0193) follows FHIR R4 resources
[src: `docs/clinical.md`]. What it sets as precedent for Spotter:

- **Ids are the upstream's**, `<provider>/<account>/<upstream id>`, so a second
  export rewrites in place. Every kind binds `revised`.
- **`about` names the subject**; absent means the owner.
- **A code is one coding**: `system`, `version`, `code`, `display`. This is
  how an exercise can carry its FIT, Health Connect or Hevy identity.
- **An enum value is FHIR's code without hyphens**, with a label;
  `sourceStatus` keeps a status no value fits.
- **Plan and execution are two records linked by a reference**:
  `medicationrequest` with `intent` plan versus order and `basedOn` naming
  the plan. A routine and a session are the same pair; Health Connect's
  `plannedExerciseSessionId` and Hevy's `routine_id` (below) say the same.
- **Objects stay out of search and embeddings**, so a kind whose detail lives
  in objects also writes a text property for search (`encounter.text`).

### 2.4 Mneme conventions

From `svc/mneme-v6/CLAUDE.md`, `docs/kind-design.md`, `docs/plan.md` and
`kinds/health/` [src]:

- **Package names are one plain domain word** (`memory`, `capture`,
  `health`, `commerce`, `places`, `coffee`); the authority is literally
  `mneme.rxtx.dev`.
- **"Declare as little as possible. A kind we declare is a kind we own
  forever"**, and narrowing is refused once records hold the old shape.
- **"Version every declaration you change. A changed declaration shipping
  the same version reaches no repository."** Production stores its own
  number per package, so a new version must exceed it (health went to 17
  "because geoah.me stores 16").
- **Root and log**: `health/routine` (a rule, `temporal(point)` +
  `recurring`, `status` state active, paused, archived) and
  `health/routinelog` (one occurrence, `temporal(point)`, `occurrencelog`,
  a required cascading `routine` reference, an index on `[routine, at]`, and
  an unpinned `evidence` reference "the workout that satisfied the training
  routine"). A Spotter session can be that evidence.
- **`routine` is taken** in Mneme's `health` package for "something the owner
  should do on a repeat", and its comment says "The gym template kept the
  other half of the old name and is `workouttemplate`". A Spotter kind named
  `routine` in another package is legal (names are always full) but two
  "routines" will confuse an agent unless labels differ.
- **`purpose`**: `primary` for what a person browses, `supporting` for a
  detail reached from its parent, `internal` for machinery; search ranks the
  last two lower [src: `docs/vocabulary.md`].
- **`displayTemplate` pitfalls**: Mneme measured on 2026-09-19 that `{status}`
  (a state) and `{at}` (a hot column) rendered empty
  (`kinds/health/routinelog.yaml`); today's docs say a state renders its state
  and an instant renders RFC 3339 [src: `docs/vocabulary.md`]. Either way an
  instant renders in UTC, so a session should carry a `name` heading the app
  writes on the local clock, as `health/workout` does ("Running, Sat 11 Oct
  08:43", `host.observed.workout_name`).
- **Descriptions are tooltips**: at most 200 characters for a property and
  400 for a kind, no newlines; the rest goes in manifest comments.
- **Agents write through grants**: Mneme's assistant reads `*` (a row budget
  of 2,000; 10,000 is the most a grant may raise it to) and names a list of
  kinds in `writes` (`kinds/assistant/mneme.yaml`; `docs/agents.md`,
  `docs/functions.md`). It has no `write` tool of its own: it writes through
  five sub-agents (tasks, people, products, places, coffee) and `propose`,
  and a sub-agent writes only what both its list and the assistant's name
  (T-116). Routines authored "through the console and its agent" need a
  sub-agent that writes the training kinds, with those kinds in its own
  `writes` and in the assistant's.

### 2.5 What the outside systems do with sets

From `02-workouts.md` §1.8, §1.9, §2.1 and §3 (checked there on 2026-10-08),
with Hevy re-checked today:

- **Hevy** [web, OpenAPI at `api.hevyapp.com/docs`, version 0.0.1]: a
  `Workout` holds `exercises[]` (index, title, notes, `exercise_template_id`,
  `superset_id`, a nullable number) each holding `sets[]` (index, `type` warmup, normal,
  failure or dropset, `weight_kg`, `reps`, `distance_meters`,
  `duration_seconds`, `rpe` limited on write to 6, 7, 7.5, 8, 8.5, 9, 9.5 or 10,
  `custom_metric`). **A workout now carries `routine_id`**, which
  `02-workouts.md` had not found. A routine exercise has `rest_seconds`; a
  routine set has `rep_range {start, end}`. **Hevy's own
  `/v1/exercise_history/{exerciseTemplateId}` answers one entry per set**
  (`workout_id`, workout times, `weight_kg`, `reps`, `rpe`, `set_type` and the
  rest): the history of one exercise is a flat list of sets.
- **Strong**: CSV only, one row per set (`02-workouts.md`, secondary source).
- **FIT**: a `set` message per set (duration, repetitions, weight in kg at
  scale 16, so in steps of 1/16 kg, `set_type` active or rest, start time,
  exercise category and name, `wkt_step_index`) [web, Garmin FIT JavaScript
  SDK `profile.js`].
- **Health Connect**: one `ExerciseSegment` per set, with repetitions, and on
  `androidx-main` weight, set index and RPE (unverified as shipped).
- **HealthKit**: no sets at all [sdk].

Every source that carries sets either nests them in one document (Hevy) or
emits them as one row or message each (Strong, FIT, Health Connect, Hevy's
history endpoint). None needs anything between "set" and "session" except an
ordered list of exercises with a superset id and a rest.

## 3. Candidate shapes

The exercise catalogue and the routine are the same in every shape:

- **`exercise`**: one movement. Name, which numbers a set of it records
  (Hevy's eight template types [web]: weight and reps, reps only, bodyweight
  reps, assisted bodyweight reps, duration, weight and duration, distance and
  duration, short distance and weight), equipment,
  muscles, the load increment as a quantity, a default rest, `codings` (one
  coding per outside system), notes. Ids chosen by the writer
  (`barbell-back-squat`), so a reference reads well and survives a rename of
  `name`. A duplicate created by an agent is joined with `merge`; a filter on
  a reference also matches merged-away ids (`06-substrate-fit.md` §2).
- **`routine`**: the plan, one record, edited whole by the console or the
  agent. Name, folder, notes, and `slots[]`: key, `exercise` reference,
  superset `group`, `rest` inside the group and `restAfterGroup`, notes, and
  `sets[]` targets (set type, reps or a rep range, load, duration, RPE).
  Depth 3 of 4. One record per routine is right because a plan is written and
  read whole and never aggregated.

The shapes differ in where a performed set lives.

### Shape A: one record per session, everything embedded

`session` holds `slots[]`, each with its `sets[]` of performed values. It is
Hevy's `Workout` document as one record.

- Mirrors Hevy's payload directly; one `GET` renders a session; the agent
  reads one record to summarise a workout.
- Every set completion is a `PATCH` of the whole `slots` array, with
  `ifVersion`. Phone and watch editing at once contend for one array.
- History of an exercise: only through `referencing` (which sessions), then
  the client digs the sets out.
- No per-set aggregate, no per-set filter. Nested fields cannot be renamed or
  backfilled by declaration (section 1.8).

### Shape B: one record per set (recommended)

`session` holds the header and `slots[]` (key, exercise, order, superset
group, rests, notes), without sets. `set` is a kind: required cascading
`session` reference (`mustExist: true`), `exercise` reference, `slot` key,
`order`, `setType`, `load` (quantity, kg or lb), `reps`, `duration`,
`distance` (quantity, m), `rpe` (decimal), `restAfter`, and `at`/`endsAt`
through `temporal(range)`.

- History of an exercise is one indexed list; trends are one aggregate read.
- Each set is written once, at its own id, by whichever device performed it.
- Rendering a session is two reads; a flush is one request per set.

### Shape C: one record per exercise performance

`session` holds the header; `exerciselog` is one exercise inside one
session (order, superset group, rests, notes) with its `sets[]` embedded.
This is FIT's lap-like middle level and the shape of Hevy's
"exercise inside a workout".

- History of an exercise is one indexed list of performances, each carrying
  its sets: the exact shape of a "previous" column.
- A set completion patches one small array.
- No per-set aggregate; nested set fields have Shape A's upgrade limits.
- A superset spans records (a shared `group` on each); reordering exercises
  is a write per moved record.

All three were written as manifests in a scratch directory and passed the
offline validator built from production's commit, together with
`substrate.reamde.dev/health` for the `workout` pin [src]. The validator
refused `renamedFrom` on an object field, as section 1.8 says.

### 3.1 Records per year

Assumptions [unverified]: 6 exercises and 24 sets (one warm-up each) per
session, 52 weeks; a plausible range is 15 to 35 sets. Catalogue records
(50 to 150 exercises, 5 to 20 routines) are written once and left out.

| Shape               | Records per year, 3 sessions a week | Records per year, 4 sessions a week | Records after 10 years | Years until one kind holds 10,000 live records |
| ------------------- | ----------------------------------- | ----------------------------------- | ---------------------- | ---------------------------------------------- |
| A, embedded         | 156                                 | 208                                 | 1,560 to 2,080         | about 48 (4 a week) to 64 (3 a week)           |
| B, set kind         | 3,900 (156 + 3,744 sets)            | 5,200 (208 + 4,992 sets)            | 39,000 to 52,000       | about 2 (4 a week) to 2.7 (3 a week)           |
| C, exerciselog kind | 1,092 (156 + 936)                   | 1,456 (208 + 1,248)                 | 10,900 to 14,600       | about 8 to 10.7                                |

With 15 to 35 sets per session, Shape B's sets range from 2,340 to 7,280 a
year.

Changelog entries per session are about the same in every shape (one per
set, plus start, a few slot edits, finish and the Apple Health link): about
29 for A and B and 33 for C, so 4,500 to 6,900 entries a year. The bytes
differ, because a patch carries the whole new value of the property it
writes. Sized with `06-substrate-fit.md`'s measured line lengths, these are
estimates [unverified]:

| Shape | What one set completion writes                                          | Changelog per session | Changelog per year |
| ----- | ----------------------------------------------------------------------- | --------------------- | ------------------ |
| A     | the whole `slots` array, about 4.5 KB on average, growing to about 9 KB | about 130 KB          | 20 to 27 MB        |
| B     | one new 10-property record, about 2.4 KB                                | about 65 KB           | 10 to 14 MB        |
| C     | one exercise's `sets`, about 0.75 KB plus overhead                      | about 50 KB           | 8 to 10 MB         |

All three are under 0.2 percent of the owner's 15.8 GB of segments (#761),
and add seconds a year to `verify`. Volume does not choose the shape.

### 3.2 Query paths

Kind paths below use the recommended package, `mneme.rxtx.dev/training`
(written `T/` in the table for brevity, meaning `~/training/`).

| Question                                                             | Shape A                                                                                                                                     | Shape B                                                                                                                                                                                          | Shape C                                                                                 |
| -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------- |
| Last time's sets for exercise X (the "previous" column, suggestions) | `referencing {ref: T/exercise/X, property: slots}`, `kinds [T/session]`, `orderBy at:desc`, `first 3`; client extracts the sets; refs index | `kinds [T/set]`, `exercise eq T/exercise/X`, `orderBy at:desc`, `first 30`, `expand session`; reference index                                                                                    | `kinds [T/exerciselog]`, `exercise eq X`, `orderBy at:desc`, `first 1`; reference index |
| Every set of X in a year (X in 1 to 4 sessions a week, 4 sets each)  | the same walk, about 50 to 200 whole sessions                                                                                               | the same list, about 200 to 830 rows, one or two pages of 500                                                                                                                                    | about 50 to 200 rows, each with its sets                                                |
| Working sets of X over 100 kg                                        | client only                                                                                                                                 | add `setType in [normal, failure, drop]`, `load gte {value: "100", unit: kg}`                                                                                                                    | client only                                                                             |
| Heaviest load, set count and reps of X per week                      | client only                                                                                                                                 | aggregate property read, `name: load`, `reducers [max, count]`, `groupBy [exercise]` (and `reps` with `sum`); `count` on `load` skips sets with no load, so bodyweight work is counted on `reps` | client only                                                                             |
| One session with its sets                                            | `GET` the session                                                                                                                           | `GET` the session, then `session eq S`, `orderBy order` (never a two-sided `at` bound)                                                                                                           | `GET` the session, then `session eq S`                                                  |
| A session in progress on any device                                  | `status eq active` on `T/session`                                                                                                           | same                                                                                                                                                                                             | same                                                                                    |
| An agent asked "how is my squat going"                               | reads whole sessions inside its row budget (2,000 for Mneme's assistant)                                                                    | one aggregate call through the `query` tool                                                                                                                                                      | reads performances, does the arithmetic                                                 |

A worked Shape B read for the "previous" column, and the weekly trend:

```http
GET /api/v1/records?filter={"kinds":["~/training/set"],
                             "properties":{"exercise":{"eq":"~/training/exercise/barbell-back-squat"}}}
                   &orderBy=at:desc&first=30&expand=session

GET /api/v1/records?filter={"kinds":["~/training/set"],
                             "properties":{"exercise":{"eq":"~/training/exercise/barbell-back-squat"},
                                           "setType":{"in":["normal","failure","drop"]}}}
  &aggregate={"property":{"name":"load","groupBy":["exercise"]},
              "range":{"gte":"2026-01-01T00:00:00+02:00","lt":"2027-01-01T00:00:00+02:00"},
              "bucket":{"calendar":"week","weekStart":"monday","timezone":"Europe/Athens"},
              "reducers":["max","count"],"unit":"kg"}
```

The second has the form of the property read `health-apps.md` uses for
weekly training load over `health/workout` (`property.groupBy`, weekly
buckets) [src], applied to a kind the app owns. That read places workouts on
their recorded-local calendar through `utcOffsetStart` and counts each once
under `property.sources`; this one uses a named zone, because the sketched
`set` carries no offset. It was not run against a server.

### 3.3 Write paths from the app

| Step                                     | Shape A                                                     | Shape B                                                                                                                                                                | Shape C                                       |
| ---------------------------------------- | ----------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------- |
| Start from a routine or empty            | `PUT` session at a client id, slots copied from the routine | same                                                                                                                                                                   | `PUT` session; `PUT` one exerciselog per slot |
| Complete a set                           | `PATCH` session `slots` whole, `ifVersion`                  | `PUT` set at a client id                                                                                                                                               | `PATCH` exerciselog `sets` whole, `ifVersion` |
| Edit an earlier set                      | `PATCH` `slots` whole                                       | `PATCH` that set                                                                                                                                                       | `PATCH` that exerciselog's `sets`             |
| Add, reorder, replace, superset          | `PATCH` `slots`                                             | `PATCH` session `slots` (one write)                                                                                                                                    | a write per moved exerciselog                 |
| Finish                                   | `PATCH` `status: finished`, `endsAt`                        | same                                                                                                                                                                   | same                                          |
| Discard                                  | `DELETE` the session                                        | `DELETE` the session; its sets stay live until the collector takes the session (up to five minutes) and are tombstoned then, or in the same request with `?purge=true` | same as B                                     |
| Two devices write at once                | conflict on one array                                       | none for sets; slots by `ifVersion`                                                                                                                                    | conflict per exercise                         |
| Offline flush after a gym with no signal | one patch, the latest array                                 | one `PUT` per set, session first (`mustExist`)                                                                                                                         | one patch per exercise                        |

Rules common to every shape:

- **Never put `status` in a `PUT`.** A replayed create after the session
  finished would try to move the state back and be refused. Create without
  it (born `active`), move it only by `PATCH`.
- **Choose ids on the device** (a lowercase UUIDv7 has no `/` and sorts by
  time), so every retry is a `PUT` that lands the same row.
- **`PUT` never clears a property**; clearing RPE is a `PATCH` with `null`.

### 3.4 Upgrade risk

- **Shape A**: every set field is a nested field. A rename is impossible by
  declaration, a new required field cannot be backfilled by declaration, and
  either needs the app to rewrite every session. With a few hundred sessions
  that is feasible but manual. Adding optional fields is free.
- **Shape B**: set fields are top-level, so `renamedFrom:`, `default:` with
  `required:` and enum remaps (decision 0066) work in place, until the set
  kind holds 10,000 live records, about two years in. After that a rename is
  expand-and-contract through ordinary writes, unless the ceiling is raised on
  the deployment or chunked conversion lands. The session's `slots` are nested
  and carry Shape A's limits, but they are few and rarely change shape.
- **Shape C**: both costs, milder: the exerciselog kind reaches the ceiling
  late, and its nested sets cannot be renamed in place.

### 3.5 Recommendation: Shape B

1. It is the only shape the server can query by set value: an indexed list
   per exercise, value filters on load, reps and set type, and the aggregate
   property read grouped by exercise. Shapes A and C push all of that into
   every client, the agent included.
2. Hevy, the reference app, answers "history of exercise X" with one entry
   per set [web]; Strong, FIT and Health Connect emit one row, message or
   segment per set. An importer writes one set record per upstream set, at an
   id composed from the upstream key, and re-imports converge.
3. Each set is an independent, idempotent `PUT`. The watch app (later) can
   write its sets without a merge protocol, and an offline queue needs no
   coalescing.
4. Both precedents (`samples/fitness/workoutset`, Mneme's `workoutset`) chose a
   set kind; the shape is not new to this codebase, only the units and the
   aggregate read are.
5. The ceiling cost is real but bounded: get the set kind's shape right
   before it holds 10,000 rows, add only optional properties after.

Variants considered and rejected:

- **Embedded truth plus a derived set index** (Shape A, with a Mneme
  function writing set records on every session change): two copies of one
  fact and a function in the write path, for a benefit Shape B gets directly.
- **Sets as observations** (core's `observed` trait, a metric per lift): an
  observed value is a measurement of `about`, and a lift is not a
  measurement of the exercise. A derived estimate (estimated one-rep maximum
  per exercise per session) could become observations later, written by a
  function, if a chart needs the observed index's rollups.
- **A record mapping** from an import mirror into sets: a mapping copies one
  source row to one target row and cannot explode a workout into sets
  (`06-substrate-fit.md` §7); importers are functions.

### 3.6 The recommended kinds, sketched

Descriptions are omitted here; Mneme requires one on every kind and
property. This passed the offline validator with
`substrate.reamde.dev/health` beside it [src].

```yaml
# mneme.rxtx.dev/training, version 1: exercise, routine, session, set
session:   # purpose primary; temporal(range) + core/revised
  name: string                      # heading on the local clock, written by the app
  status: state [active, finished]  # active -> finished, finished -> active
  routine: reference -> training/routine        # the plan it started from; no mustExist
  slots: object, repeated           # key (required), exercise -> training/exercise (required),
                                    # group (superset), rest, restAfterGroup, notes
  utcOffsetStart, utcOffsetEnd: int (-1080..1080)
  workout: reference -> substrate.reamde.dev/health/workout   # the Apple Health copy
  healthKitWorkout: string          # HKWorkout UUID, as HealthKit spells it
  source: reference -> substrate.reamde.dev/observations/source
  about: reference (any); origin: reference (any); revision: int
  notes: markdown
  indices: [[routine, at]]
set:       # purpose supporting; temporal(range) + core/revised
  session: reference -> training/session, required, mustExist, onDelete cascade
  exercise: reference -> training/exercise, required
  slot: string; order: int (min 1)
  setType: enum [warmup, normal, failure, drop, myo]
  load: quantity, unit kg, units [kg, lb]
  reps: int (min 0); duration: duration; distance: quantity, unit m
  rpe: decimal (1..10); restAfter: duration
  source, about, origin, revision as on session
  indices: [[exercise, at], [session, at]]
```

The set-type values and the myo-rep, drop-set and superset semantics are the
data-model question, not this document's; the sketch only fixes where they
live. Notes on the sketch:

- `rpe` as `decimal` keeps 7.5 exact; `duration` as `duration` matches
  `health/workout` and the aggregate read, at the cost of text comparison in
  list filters (section 1.2).
- No derived values (volume, estimated one-rep maximum, session totals).
  Mneme's kind-design rules forbid derived properties on mirrors; for an
  app's own kind the owner decides (open question).
- `utcOffset` per set is left out; a per-set read in a named timezone does
  not need it, and the recorded-local calendar can use the session's
  offsets through a session-level read.

## 4. Package home

### 4.1 Now: `mneme.rxtx.dev/training`

- **The owner applies it.** A Mneme package is applied with `substratectl
  apply -f` to `geoah.me` when the owner says, tested first in a scratch
  repository (`svc/mneme-v6/CLAUDE.md`). A first-party package ships in the
  substrate binary: a substrate PR, a release and an engine roll through
  `docs/substrate-upgrade.md`, for every change while the model settles.
- **Mneme already holds what Spotter sits beside**: `health/routine` and
  `routinelog` (a session can be the `evidence` of a "train three times a
  week" routine), the assistant agent that will author routines, and the
  surfaces that read `health/workout`.
- **The word**: `training` fits strength work and the conditioning that
  shares a gym session (rower intervals, carries, timed holds) without a
  rename. `strength` is narrower and more exact; `gym` names a place. The
  rule only asks for one plain domain word.
- **Kind names**: `exercise`, `routine` (the owner's word and Hevy's) or
  `workouttemplate` (Mneme's earlier name, which avoids two "routines"),
  `session` (Health Connect and FIT's word, distinct from `health/workout`),
  and `set`. The validator accepts `set` as a kind name [src].

### 4.2 Later: a first-party `substrate.reamde.dev` catalogue

Decision 0192 made the well-known catalogues first-party "because measured
data is something every repository and every provider needs under one
name" [src: `docs/decisions/0192-…`]. A provider published under
`providers.substrate.reamde.dev` cannot name a Mneme kind, so the move is
due when a substrate provider (a Hevy, Strong or FIT importer, or another app
than Spotter) needs to write sessions and sets. Until then a Mneme function
can adapt a provider's mirror into Mneme's kinds, which is how Mneme already
handles provider data (`svc/mneme-v6/CLAUDE.md`, "Mneme-specific behaviour
lives only in our packages").

How the move would go:

- **T-119 is the precedent.** Mneme's health rows moved to the first-party
  `health` and `labs` packages "with the same ids" on 2026-10-27, the day
  production took decision 0192. The hand-written rows (lab reports and
  results, sources, metrics, 18 observations) were re-written by
  `substratectl apply` with their references rewritten; the provider-derived
  observations, sleeps and workouts were rebuilt by the providers' adapters
  from their mirrors. T-119 then pointed Mneme at the new kinds and retired
  the old ones on 2026-10-28 [src: `svc/mneme-v6/docs/tickets/T-119-…`;
  `docs/substrate-deployments.md`, 2026-10-27 20:13]. Spotter's rows are all
  hand-written, so theirs would be the `apply` half.
- `movedFrom:` (decision 0078) moves rows declaratively only through the
  shipped tree's boot upgrade, requires identical properties, and leaves the
  old kind declared and empty forever [src: `docs/vocabulary.md`]. A
  first-party package naming a `mneme.rxtx.dev` kind is unlikely, so expect a
  T-119-style copy.
- **To keep the copy mechanical**, the Mneme kinds should use the first-party
  property names where they overlap (`about`, `source`, `origin`,
  `revision`, `utcOffsetStart`, `utcOffsetEnd`, `name`, `notes`, `duration`,
  `distance`), with the same types (`health/workout` declares `notes` as
  `text`, where the sketch in 3.6 says `markdown`), quantities with declared
  units, enums spelled by the FHIR rule, and codings shaped like clinical's.
  The app should read kind names from one
  configuration point so that a later rename is one change.

## 5. Linking a session to Apple Health and `health/workout`

### 5.1 How an HKWorkout reaches substrate today

- **Substrate Sync** (`apps/apple/SubstrateSync`) reads every `HKWorkout` of a
  day, whatever app wrote it, and posts it to the `applehealth-webhook`
  trigger in Health Auto Export's JSON [src: `Sync/HealthReader.swift`,
  `Sync/HealthEncoder.swift`]. Production arms that trigger from the app
  (`svc/mneme-v6/CLAUDE.md`, T-119).
- It sends `id` as the HKWorkout UUID uppercased, `name` from the activity
  type (`traditionalStrengthTraining` becomes "Traditional Strength
  Training"), start, end, duration, indoor or outdoor, active energy,
  distance, heart rate, elevation, a per-minute heart rate series, the route,
  and `metadata` holding only `HKTimeZone` [src: `HealthEncoder.swift`,
  `WorkoutSummary.json(_:)`]. **It sends no source bundle identifier and no
  other metadata key.**
- The provider writes the mirror row `providers.substrate.reamde.dev/applehealth/workout`
  at id `applehealth/<account>/<UUID>`, keeping `workoutId` (the UUID) and
  `metadata` as JSON "as the app sent them". "Workouts carry no `source`"
  [src: `kinds/providers.substrate.reamde.dev/applehealth/README.md`,
  `bundle.yaml`].
- The adapter `adaptapplehealth` (`applehealth@5`) writes
  `substrate.reamde.dev/health/workout` at the same composed id, with
  `origin` pointing at the mirror row, `sport` from the name
  ("traditional strength training" and "functional strength training" map
  to `strength`), the source taken from the most frequent device label on the
  workout's sample rows (from Substrate Sync, only heart-rate rows carry one),
  and `revision` from the delivery's arrival [src: `workout_outputs`,
  `SPORTS`, `Writer.compose`]. `docs/health-apps.md` shows such an id:
  `applehealth/iphone/9931515A-49DA-4850-A20D-956A5228C096` [src].
- **Deletions do not travel.** "A sample or workout deleted on the phone
  keeps its mirror row, and the records built from it ... Delete the mirror
  row to remove them" [src: applehealth `README.md`]. Deleting the mirror row
  makes the adapter delete its outputs [src: adapter docstring].

### 5.2 What Spotter should do

1. **Write one HKWorkout per finished session**, activity type traditional
   strength training, with `HKMetadataKeyExternalUUID` set to the session id
   [sdk: "a unique identifier for an HKObject that is set by its source";
   uniqueness not enforced] and `HKMetadataKeyTimeZone`. HealthKit returns the
   saved workout from `finishWorkout`, and every `HKObject` has a `UUID`
   [sdk].
2. **Store the UUID on the session** (`healthKitWorkout`), and set
   `workout` to `substrate.reamde.dev/health/workout/applehealth/<account>/<UUID>`.
   The reference has no `mustExist`, so it may name the row before Substrate
   Sync delivers it; reads resolve it once it exists. The account id is the
   one the owner chose for the phone (`applehealth/account` records), read
   once.
3. **Never write `health/workout` from Spotter.** The Apple Health path
   already makes one; a second, with Spotter as source, would show the session
   twice in every list and need a priority policy on every read. The records
   source policy can drop an overlapping lower-ranked record per `sport`
   [src: `docs/api.md`, The records' source policy], but a plain list does not
   apply it.
4. **Avoid editing a saved HKWorkout.** HealthKit replaces an object only
   through delete and re-save (or `HKMetadataKeySyncIdentifier` with a higher
   `HKMetadataKeySyncVersion`, which replaces the old object) [sdk]; either way
   substrate keeps the old mirror row and its `health/workout`. An edit to a
   finished session changes the session in substrate, not the HealthKit
   workout, unless the owner accepts cleaning up the stale mirror row.
5. **Proposed upstream change (a substrate PR, Substrate Sync)**: forward
   `HKExternalUUID` (the string `HKMetadataKeyExternalUUID` holds, printed
   from HealthKit on this machine [sdk]) and the source bundle identifier in the workout's
   `metadata`. The mirror already stores `metadata` as JSON, so no kind
   changes. A Mneme function on `applehealth/workout` could then link a
   session by its id even after a re-save, and tell a Spotter workout from
   others. What Health Auto Export puts in `metadata` for custom keys is
   [unverified].

The reverse question, "which session is this Apple Health workout",
needs no extra property: `referencing` on the `health/workout` path lists the
session that points at it.

### 5.3 WHOOP

WHOOP's adapter (`whoop@25`) writes `health/workout` from WHOOP's workout
mirror, mapping `weightlifting`, `powerlifting`, `strength-trainer` and
`functional-fitness` to `strength`, with WHOOP's strain, energy, heart rates
and zone durations [src: `kinds/providers.substrate.reamde.dev/whoop/bundle.yaml`,
`SPORTS`, `workout`]. It carries no exercises: the WHOOP mirror kind
declares no exercise, set or rep field [src], and the public API publishes
none [recalled]. A strength session worn with a WHOOP band therefore produces two
`health/workout` rows (Apple Health and WHOOP) and one Spotter session. They
share no id; only the time span links them. Reads that should count each
session once already have the tool: a priority policy with `match: ["sport"]`
drops the lower-ranked overlapping record, as `health-apps.md` does for weekly
load [src]. Linking the WHOOP row to the session explicitly (by overlap, in a
function) is optional.

## 6. How the app talks to substrate

- **Sign-in**: `POST /login` with repository, password, TOTP code and a
  label such as "Spotter on iPhone"; keep the secret in the Keychain. A
  separate token per app and device gives each its own principal in the
  changelog and its own revocation [src: `docs/auth.md`].
- **Discovery**: `GET /.well-known/substrate/server.json` lists features,
  `aggregate` and `units` among them (both `alpha`), before any token exists
  [src: `docs/api.md`, Discovery; `internal/api/discovery.go`].
- **Writes**: `PUT` at client ids for session and set creation, `PATCH` with
  `ifVersion` for session slots and transitions, `DELETE` for discard. Keep an
  ordered local outbox; replay it after the gym's dead zone; treat `409` on a
  session patch as "re-read, merge, retry".
- **Reads**: list routines and exercises on launch; per-exercise set lists
  for history; aggregate for charts. Cache everything locally: the app must
  work offline mid-session.
- **Staying current**: keep the `head` and `generation` of the last list,
  then read `/api/v1/changes?watch=1&from=…&generation=…&kinds=…` in the
  foreground, so routines edited in the console arrive and deletions are not
  missed after the five-minute sweep. A `410` means the history was replaced
  (a restore): list again.
- **What to copy from Substrate Sync's client** (`Sources/API/Client.swift`)
  [src]: origin parsing (bare host means HTTPS), the error envelope reader
  (`error.message` and `error.problems`), retry on `503` and `429` honouring
  `Retry-After` (capped at 10 s) up to three times, percent-encoding every
  `/` in an id so it stays one path segment, the
  timeout classes (20 s quick, 30 s read, 120 s upload), `get` that treats a
  tombstone as absent, `put`, null-patch `clear`, and `delete` that accepts
  `404`.
- **What to add**: `patch` with `ifVersion` returning the new version, a
  typed `list` (`filter`, `orderBy`, `first`, `after`, `expand`; decoding
  `records`, `cursor`, `head`, `generation`, `included`), an `aggregate`
  call, and an ndjson change-feed reader.

## Design implications for Spotter

- Use Shape B: kinds `exercise`, `routine`, `session` and `set` in one new
  Mneme package, `mneme.rxtx.dev/training`, version 1, applied by the owner
  after a scratch-repository test.
- Sets are their own kind. Do not rely on filtering inside an embedded list;
  it is outside the contract.
- Load is a `quantity` with `unit: kg` and `units: [kg, lb]`, never a float.
  Plan and session are separate kinds; `session.routine` points at the plan.
- The session embeds its ordered exercise slots (superset group and rests
  live there); each set carries `exercise`, `slot` and `order`, so reordering
  is one patch and history needs no join.
- `set.session` is required, `mustExist` and `onDelete: cascade`: the outbox
  must write the session before its first set.
- Choose ids on the device; create with `PUT`; never send `status` in a
  `PUT`; move it only with `PATCH`; use `ifVersion` on every session patch.
- Bind `temporal(range)` and core's `revised` on session and set, and declare
  `source`, `about`, `origin`, `revision`, `utcOffsetStart`/`utcOffsetEnd`
  with the first-party names, so imports and a later move are mechanical.
- Write a `name` heading on the local clock; do not build titles from
  `{at}`.
- Read history as `set` lists filtered by `exercise` (`orderBy at:desc`);
  read trends with the aggregate property read grouped by `exercise`. Never
  bound `at` on both ends when another order is needed. Do not filter lists
  on `duration`.
- Treat the server as the sync target, not the live store: the app keeps a
  local copy, an outbox and a change-feed cursor.
- Save one HealthKit strength workout per session with
  `HKMetadataKeyExternalUUID` = session id; store its UUID on the session;
  point `session.workout` at `health/workout/applehealth/<account>/<UUID>`;
  never write `health/workout` from the app.
- Get the set kind's shape right early: after about 10,000 sets (two years)
  a rename or a new required field needs expand-and-contract.
- Give the console's agent a training sub-agent that writes the training
  kinds, and add those kinds to the Mneme assistant's `writes` too (the
  assistant has no `write` tool; a sub-agent writes only what both lists
  name), so it can author exercises and routines. Write kind and property
  descriptions that guide an agent through the nested routine shape.
- Leave derived numbers (volume, estimated one-rep maximum, session totals)
  to readers until a reader needs them on the server.

## Open questions for the owner

1. Package and kind names: `training`, `strength` or `gym`? `routine` beside
   Mneme's `health/routine`, or `workouttemplate`? `session` or another
   word?
2. Is one record per set acceptable, given about 5,000 records a year and
   two reads to render a session?
3. Should Spotter ever write `health/workout` itself, for example on a device
   where Substrate Sync is not installed? The recommendation is no.
4. When a finished session is edited, should Spotter re-save the HealthKit
   workout, and if so, may it delete the stale
   `providers.substrate.reamde.dev/applehealth/workout` mirror row so its
   `health/workout` goes too?
5. Should Substrate Sync forward `HKExternalUUID` and the source bundle
   identifier in workout metadata (a substrate PR)?
6. Should the WHOOP strength workout be linked to the session explicitly, or
   is read-time deduplication enough?
7. Store derived values on records (per-set volume, estimated one-rep
   maximum, session tonnage) so the aggregate read and the agent can use them,
   or keep them in clients?
8. Should a session keep a copy of the routine's targets as they were when it
   started, so a later routine edit does not change what "the plan" meant?
   Should skipped planned sets be recorded?
9. Where should the exercise catalogue come from: written by the agent as
   needed, or seeded from a public list (free-exercise-db is public domain per
   `02-workouts.md`), with codings to FIT, Health Connect and Hevy?
10. Should sessions appear on timelines beside the Apple Health
    `health/workout` row, or should one of them be `supporting`?
11. One sign-in per app (separate tokens), or a shared Keychain item with
    Substrate Sync?
12. When is an import from Hevy or Strong wanted? That decides when the
    package moves to a first-party catalogue.
13. Where does the kg or lb display preference live? No record holds a unit
    preference today (`health-apps.md`, G-34).

## Sources

In this machine's checkouts (substrate at `ffd7b967`, read-only at
`/tmp/spotter-substrate-main`):

- `docs/data-model.md`, `docs/api.md`, `docs/vocabulary.md`, `docs/units.md`,
  `docs/changelog.md`, `docs/auth.md`, `docs/functions.md`,
  `docs/observations.md`, `docs/health-apps.md`, `docs/clinical.md`,
  `docs/console.md`, `docs/substratectl.md`, `docs/agents.md`,
  `docs/traits.md`
- `docs/decisions/0192-observations-is-seeded-and-the-well-known-catalogues-are-first-party-packages.md`
- `docs/plans/health-data/02-workouts.md`, `docs/plans/health-data/06-substrate-fit.md`
- `kinds/substrate.reamde.dev/core/core.yaml` (traits `revised`, `metric`, `source`)
- `kinds/substrate.reamde.dev/health/health.yaml`, `health/workout.yaml`
- `kinds/substrate.reamde.dev/observations/source.yaml`
- `kinds/substrate.reamde.dev/clinical/encounter.yaml`, `clinical/medicationrequest.yaml`
- `kinds/providers.substrate.reamde.dev/applehealth/README.md`, `applehealth/bundle.yaml`
- `kinds/providers.substrate.reamde.dev/whoop/bundle.yaml`
- `internal/engine/query.go`, `refs.go`, `expand.go`, `aggregateproperty.go`,
  `schemadiff.go`, `indices.go`; `internal/vocabulary/types.go`, `load.go`;
  `internal/api/core.go`, `discovery.go`; `internal/config/config.go`;
  `internal/engine/quantitygraph.go`
- `apps/apple/SubstrateSync/Sources/API/Client.swift`,
  `Sources/Sync/HealthReader.swift`, `Sources/Sync/HealthEncoder.swift`
- `git -C ~/src/github.com/geoah/substrate show f47c21f5^:samples/fitness/{bundle,exercise,workout,workoutlog,workoutset,workouttemplate}.yaml`

In this rxtx.dev worktree:

- `svc/mneme-v6/CLAUDE.md`, `svc/mneme-v6/docs/kind-design.md`,
  `svc/mneme-v6/docs/kind-design-amendments.md`, `svc/mneme-v6/docs/plan.md`,
  `svc/mneme-v6/docs/not-ported.md`
- `svc/mneme-v6/kinds/health/bundle.yaml`, `routine.yaml`, `routinelog.yaml`
- `svc/mneme-v6/kinds/assistant/mneme.yaml`
- `svc/mneme-v6/docs/tickets/T-119-mneme-reads-substrates-first-party-observations-health-and-labs.md`
- `docs/substrate-deployments.md` (the 2026-10-27 20:13 roll)
- `git show 311b7fee^:svc/mneme-v6/kinds/health/workoutset.yaml` and `workout.yaml`

SDK:

- iOS 27.2 SDK, `HealthKit.framework/Headers/HKMetadata.h`
  (`HKMetadataKeyExternalUUID`, `HKMetadataKeySyncIdentifier`,
  `HKMetadataKeySyncVersion`), `HKWorkoutBuilder.h` (`finishWorkoutWithCompletion:`),
  `HKObject.h` (`UUID`), `HKSource.h` (`bundleIdentifier`), `HKQuery.h`
  (`predicateForObjectsWithMetadataKey:`), `HKWorkoutActivityType.h`

Web (checked 2026-10-30):

- [Hevy public API, OpenAPI document](https://api.hevyapp.com/docs/)
  (embedded in `swagger-ui-init.js`)
- [Garmin FIT JavaScript SDK, `src/profile.js`](https://github.com/garmin/fit-javascript-sdk/blob/main/src/profile.js)
  (`set` message fields and the `set_type` enum)

Issues and pages cited through `06-substrate-fit.md` and `02-workouts.md`
(not re-read today):

- [geoah/substrate#125](https://github.com/geoah/substrate/issues/125),
  [#275](https://github.com/geoah/substrate/issues/275),
  [#157](https://github.com/geoah/substrate/issues/157),
  [#761](https://github.com/geoah/substrate/issues/761)
- [Garmin FIT JavaScript SDK](https://github.com/garmin/fit-javascript-sdk),
  [Health Connect records source](https://github.com/androidx/androidx/tree/androidx-main/health/connect/connect-client/src/main/java/androidx/health/connect/client/records),
  [Strong CSV guide](https://thetaperapp.com/articles/how-to-export-strong-data/),
  [free-exercise-db](https://github.com/yuhonas/free-exercise-db)
- Apple documentation for the HealthKit symbols above:
  [HKMetadataKeyExternalUUID](https://developer.apple.com/documentation/healthkit/hkmetadatakeyexternaluuid),
  [HKMetadataKeySyncIdentifier](https://developer.apple.com/documentation/healthkit/hkmetadatakeysyncidentifier)
