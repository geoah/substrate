# Widening the health model to every observed quantity: a design assessment

Status: assessment, 2026-10-08. Read-only against the worktree at
`/home/geoah/.t3/worktrees/substrate/t3-d9d5090b` (branch `t3/d9d5090b`).
Inputs: `docs/plans/health-data.md` (the revised health proposal, called "the
proposal" below), the three reviews under `docs/plans/health-data/review/`,
`CLAUDE.md`, `docs/terms.md`, decision records 0012, 0015, 0020, 0034, 0044,
0047, 0048, 0049, 0055, 0070, 0077, 0078, 0080, 0085, 0099, 0100 and 0140, and
the code cited inline. No web research: where a fact about Home Assistant or
UCUM is used, it was checked against the source files another agent fetched
into `/tmp/health-research/v2/raw-iot/` and `/tmp/health-research/v2/units/`,
and is marked so.

## The answer

Yes to the engine, not yet to the domains. The engine pieces the proposal
introduces (the `quantity` datatype and its unit table, the trait that marks
an indexed measurement, the derived index and its rollups, the aggregate read,
the `form` of a value) contain nothing health-specific, and two facts in the
code make it cheap to name and shape them neutrally now and expensive later:

1. **A core trait cannot gain a property after it ships.** The loader requires
   a binding kind to declare every non-hot property its trait contracts
   (`internal/vocabulary/load.go:2789`, "trait %s requires property %s"); there
   are no optional trait properties. The boot upgrade rewrites core but never
   a user's imported sample, so adding `about` or `code` to the trait in a later
   core version would fail to load every user's copy of `observation` until
   the owner took a sample upgrade by hand. The trait's contract must be
   complete in its first version.
2. **A sample's kind can never be moved by an upgrade.** `movedFrom` is live
   only on the shipped boot upgrade of a seeded package; the apply door and
   the catalog refuse it (`CLAUDE.md`, record 0078,
   `internal/engine/vocabularywrite.go:706`). Every stored `metric`, `source`
   and trace reference spells the package, so where the base kinds live is
   settled by the first record written. Moving them later is a repository
   migration in Go (record 0099) that rewrites every reference.

The recommendation, in one list:

- The engine facilities ship neutral from day one: the trait is
  `substrate.reamde.dev/core/observed` (not `samples`, which collides with the
  catalog tier), its contract includes `about`, `code`, `hold` and the counter
  fields even though health sets none of them, and the index and rollup keys
  carry `about` and `code` from the first migration.
- The four base kinds (`metric`, `observation`, `trace`, `source`) live in a
  small base SAMPLE, `samples.substrate.reamde.dev/observations`, that `health`
  requires, the way `calendar` requires `people`. Not seeded, not in core.
- There is no generic `subject` kind and no `location` property. The observed
  thing is a reference, `about`, pinned to `kind: any`; absent means the
  repository's owner. Rooms, cars and plants are typed kinds in their domain
  samples.
- `health` ships first, unchanged in scope. One second domain, a minimal
  `home` sample fed by Home Assistant, proves the generality, because it
  exercises the four things health does not: categorical values, counters
  that reset, many sources, and an observed thing that is not the user.
  `energy`, `weather` and `vehicle` wait until `home` has a consumer.
- The unit table is an engine facility like the currency table (record 0140):
  release-only growth, a UCUM subset plus annotations and an alias table, no
  user-declared units.

## 1. What is already neutral and what is health's

### 1.1 The engine pieces

| Piece (proposal section) | Neutral? | What in it is health-shaped today |
| --- | --- | --- |
| `quantity` datatype (5.2) | yes | Only the examples. The rules on affine temperature, deltas, comparators, arbitrary units and "dimension is not equivalence" all apply to a room thermometer or a fuel gauge. The proposal itself names "an expenses kind that measures fuel". |
| unit table (5.2) | yes, if seeded by dimension | The starting list of about 100 codes is chosen from sections 2.3 and 2.6 (wearables and labs). A neutral table is organised by dimension, and home and vehicle need `kW.h`, `V`, `A`, `Hz`, `[ppm]`, `lx`, `bar`, `m3`, `[gal_us]`, `[mi_i]` added before they ship. |
| `form` (5.1) | yes | Nothing. `counter` is described as "differenced at ingest by the canonicaliser", which is the health choice; a neutral index needs counters indexed (section 2.5 below). |
| `samples` trait (5.3) | the mechanism yes, the name no | The name collides with the catalog tier `sample` (section 4). The contract lacks `about`, `code`, `hold` and the counter reset fields, which must be in the first version (fact 1 above). |
| derived index and rollups (5.4) | yes | The key lacks `about` and `code`. `covered_ms` and the duration-weighted sum need a stated hold rule for on-change data. |
| aggregate read (5.5) | mostly | The reducer list has no `timeInState`, `increase`, `rate`, `twa` or circular mean; `groupBy` is not specified beyond source; there is no last-value read. Section 6 below. |
| `host.blobs.get`, the durable import job, cheaper changelog lines (5.6) | yes | Nothing. |

### 1.2 `metric`, property by property

"Base" means the property belongs on the neutral `observations/metric`;
"health" means it moves to a health-side kind; "new" is added for the neutral
layer. No `metric` property is read by the engine (record 0077's test is not
met, and codex review P1.3 requires that meaning live on the value), so the
metric is a catalogue entry and a guide for writers and readers.

| Property | Where | Note |
| --- | --- | --- |
| `name`, `description` | base | |
| `codes` (`{system, code, version, display}`) | base | Neutral as is. Systems include LOINC, IEEE 11073 MDC, HealthKit, Health Connect, a Home Assistant device class, an OBD-II PID, a SenML name, or the package's own. |
| `dimension`, `unit` (canonical), `delta` | base | |
| `form` (default), `reducers` (recommended) | base | Defaults a writer copies onto each value; never read by the engine. |
| `valueKind` (`quantity`, `code`, `ratio`) | base | `ratio` is a lab shape; keep the enum value, it costs nothing. |
| `compatibility` | base | Neutral: real power, apparent power and reactive power share a dimension and must never pool. |
| `algorithm` (`{vendor, name, version}`) | base, replaces `vendor` and `algorithmVersion` | A Tesla battery-health percentage or a provider's air-quality index is a vendor score too. |
| `group` | base | A navigation grouping; LOINC Groups is one source of ids among others. |
| `loincParts` | health | Lab-only. |
| `molarMass`, `conversions` | health | Mass-to-molar and method formulas are lab derivations. |
| `referenceLow`, `referenceHigh` | health | Clinical. |
| `codeSet` | new, base | The admitted codes of a categorical metric (`open`, `closed`). Guidance in v1: validating a write against another record is codex's "metric profile" and is deferred. |
| `displayPrecision` | new, base | Home Assistant's suggested display precision; a display hint, so a mutable metric is the right place. |
| `hold` | new, base | The default validity of a gauge value, copied onto each value by the writer. |

The lab-only properties need a home that does not edit the base kind. The
clean shape is a health kind that points at the metric: `health/analyte` with
`metric` (reference), `loincParts`, `molarMass`, `conversions` and the
fallback reference range. Editing `observations/metric` instead would make a
`health` upgrade depend on an `observations` upgrade, and every edit to the
base discards on re-import (section 3).

### 1.3 `observation`

| Property | Where | Note |
| --- | --- | --- |
| `metric`, `source`, `form`, `value` (quantity), `at`/`endsAt`, `utcOffset` | base | |
| `statistic`, `period`, `periodId`, `denominator` | base | A utility bill is a `summary` of `sum` over a billing period whose `periodId` is the bill number. |
| `recordingMethod` | base | |
| `components` | base | Wind speed and direction, a three-axis acceleration, blood pressure. |
| `origin`, `derivedFrom`, `conversion`, `revision` | base | |
| `context` (meal relation, body position, specimen, site) | replaced by `qualifiers` | An object with health keys cannot sit on a neutral kind, and a domain cannot add keys to a base kind without editing it. `qualifiers` is a repeated `{system, code}`: `{system: health, code: meal-after}`, `{system: home, code: window-open}`. Bounded, filterable with `contains`, and extended by code, not by declaration. |
| `about` | new | Section 2.1. |
| `code` | new | The categorical value; exactly one of `value` and `code`, or neither with `sourceValue` set. |
| `hold` | new | Section 2.5. |
| `resetRule`, `resetAt` | new | Section 2.4. |
| `sourceValue`, `sourceUnit` | new, generalised from `labresult` | The verbatim reading when the unit is not in the table, so a row with an unknown unit is still listed by time and metric. |

### 1.4 `trace`

Everything in proposal section 5.3 is neutral. Additions: `about`, `codes`
(a coded trace: exactly one of `values` and `codes`), `hold`, `resetRule`,
`resetAt`. One rename: the regular spacing property `interval` becomes
`every`, because the same kind carries a `form` whose value list contains
`interval`, and one kind must not use one word for two things. `channel` and
`acquisition` stay (vibration and audio sensors have them too).

### 1.5 `source`

| Property | Where | Note |
| --- | --- | --- |
| `name`, `manufacturer`, `account` (trait pin to `accountconfig`), `priorities` | base | |
| `kind` (`watch`, `ring`, ...) | base, renamed `class` | A property called `kind` on a record whose envelope has a `kind` key is two meanings of a term in `docs/terms.md`. The same applies to `tag.kind`. |
| `model` | base, renamed `hardwareModel` | `model` is a term (`docs/terms.md`, the LLM model id). Low severity; it is a property name. |
| `about` | new | The thing the device is fixed to. A writer copies it onto each value; the engine never reads it from the source record. |
| `resolution` | new | The device's step (0.1 Cel), as a quantity. |
| `firmware` | new | |

### 1.6 What is health's alone

The `health` sample keeps `sleep`, `workout`, `lap`, `set`, `plan`, `step`,
`gear`, `exercise`, `labreport`, `labresult`, `tag`, the new `analyte`, the
sleep-stage set and its mapping versions, the sport hierarchy, the comparator
truth table for lab values, the LOINC synonym table, and the catalogue of
about 70 wearable metrics and about 240 markers. `tag` is arguably neutral (a
note on the timeline: "vacation mode", "window open for airing"); leave it in
health until a second domain asks for it.

## 2. What a neutral layer needs that the health version lacks

| Need | Where it lives | Index cost | Aggregate request cost |
| --- | --- | --- | --- |
| the observed thing | `about` on `observation` and `trace`, in the trait | one text column in `observed_points` and in the rollup key | `groupBy: ["about"]`; a filter on `about` stays rollup-eligible |
| location | nothing new; `about` names a place, or position is its own metric | none | none |
| categorical values | `code` / `codes` on the value, `codeSet` on the metric | one nullable text column; `code` joins the rollup key; one `held_ms` column | reducers `timeInState`, `count` by code, `last` |
| counters that reset | `form: counter` plus `resetRule` and `resetAt` on the value | rollups add `increase` and `resets`; first and last already there | reducers `increase`, `rate`; read-time stitching across rows |
| on-change sampling | `hold` on the value, irregular `offsets` (exist) | rollups add `twa_sum`, `held_ms`, `last_hold_ms` | reducer `twa`; request `maxGap` |
| device precision | `resolution` on `source`, `displayPrecision` on `metric` | none | none |
| many sources | nothing new | series count, not sample count, sets rollup size | `maxBuckets` must count groups times buckets |

### 2.1 The observed thing: `about`

A reference property, `kind: any`, optional, not cascading. Absent means the
repository's owner, which is every health value, so health pays nothing. A
living-room temperature is `about` the room, a fuel level `about` the car, a
soil moisture `about` the plant, a weather reading `about` the place it was
taken. `kind: any` is already used (`kinds/substrate.reamde.dev/core/trigger.yaml:156`)
and stores the full `<kind>/<id>` path (record 0034), which is what the index
needs as a series key.

Why a property on the value and not a declaration on the metric: the same
metric (`air_temperature`) is observed about sixty different things, and the
engine must not read another record to know which (principle 1 of the
proposal). Why not a trait-pinned reference (record 0034): no contract is
common to a room, a car and a person, so a trait would be a marker with no
properties, which is a kind of "anything" by another name.

Why in the rollup key: codex review P1.2 found that rollups can serve a
filter only when every predicate is constant over the rollup's grouping keys.
Putting `about` in the key is what lets "filter to the living room" and
"group by room" read rollups instead of raw points. The cardinality cost is
the number of series, which for a fixed sensor is one per (metric, source)
anyway, since a fixed sensor observes one thing.

### 2.2 Location

No `location` property. Three cases cover it:

- A fixed sensor: the location is a fact about the thing observed (the room
  is on the ground floor of the house), held on the `about` record. Grouping
  by floor is a join through `about` (section 6.4), not an index column.
- A moving source (a car, a phone, a walk): position is a measurement. The
  proposal's catalogue already has `latitude` and `longitude` traces. "Where
  was the car when the battery hit 20 percent" is an alignment of two traces
  (P11), not a property.
- A weather observation: `about` the place record (a `places` sample is
  listed in `docs/plans/providers-and-samples.md` and does not exist yet).

A `location` reference on every observation would denormalise the room onto
millions of points and go stale when a sensor moves; a portable sensor that
moves gets a new `about` from the moment it moves, which history keeps.

### 2.3 Categorical values

A door's open or closed, presence, an HVAC mode, a sleep stage, a
`Not detected` lab result. Categorical is a value kind, not a form: a door
state is a level held until the next change (a gauge), a sleep stage is a
coded amount over `[at, endsAt)`. So the value carries `code` beside `value`,
and the form stays one of four. Reducers legal on a coded gauge: `count`
(grouped by code), `last`, `first`, and `timeInState` (the held duration per
code, under the hold rule of 2.5). `avg`, `sum`, `min` and `max` are `422`
naming the value kind.

Not named `state`: `state` is a datatype (a state machine, records 0013 and
0019), and a door's code is data the substrate does not police.

Index cost: one nullable `code text` column in `observed_points`; `code`
joins the rollup key (a door produces two rollup rows per bucket at most);
rollups gain `held_ms`. A boolean sensor is a code (`on`/`off`, or the
device class's own words), never `0` and `1` in `value`, because a mean of
door states is meaningless and the form check should refuse it.

A doorbell press or a motion trigger is a coded gauge with `hold: PT0S`:
countable, holding no time. No fifth form is needed.

### 2.4 Counters that reset

The proposal differences a counter into `interval` amounts at ingest. For
health that is fine (FIT distance, Terra steps). For meters it is fragile:
the delta at the start of a period needs the previous period's last reading,
so a late or corrected earlier reading changes two deltas, and an adapter
must read before it writes. The neutral layer indexes counters as they are
and differences at read time, the way Prometheus does.

On the value: `form: counter`, `resetRule` and `resetAt`.

- `resetRule: decrease`: any drop is a reset to zero, then counting resumes
  (Home Assistant `total_increasing`, an odometer rollover, a smart plug that
  reboots).
- `resetRule: declared`: the value may fall (a net meter exporting solar);
  only a changed `resetAt` starts a new accumulation (Home Assistant `total`
  with `last_reset`).

Both state classes and `last_reset` were checked in
`/tmp/health-research/v2/raw-iot/homeassistant_components_sensor_const.py:572`
and `homeassistant_components_recorder_db_schema.py:643`.

Index: each point is projected as is. Each rollup row stores `increase` (the
reset-aware sum of positive steps between consecutive points inside the
bucket) and `resets`, beside the existing first and last values. The step
from one row's last point to the next row's first point is computed at read
time with a window function over the series, which keeps the fold free of
cross-record dependencies: projecting one trace never reads another. A
correction to an earlier trace re-derives only that trace's buckets, and the
next read stitches correctly.

### 2.5 On-change sampling and the hold rule

Home Assistant writes a state when it changes; a temperature that sits at
21.5 for three hours has one point. An arithmetic mean over points then
weights the minutes when the temperature moved, which is wrong for "average
temperature today". Two additions:

- `hold` on the value (a duration): how long a gauge value stands after its
  timestamp when no newer point exists. Absent means "until the next point",
  capped at the request's `maxGap`. A periodic sensor sets twice its period;
  an on-change sensor with a heartbeat sets the heartbeat; a press sets
  `PT0S`. It lives on the value, not the metric, for the reason codex P1.3
  gives: an edit to a metric must not change what an indexed number means.
- `twa`, a time-weighted mean, as a reducer legal on gauges. Rollups add
  `twa_sum` (value times held milliseconds, inside the bucket) and `held_ms`.
  The tail from a bucket's last point to the next point is again a read-time
  step, using `last_hold_ms` stored on the row.

The proposal's `durationIn` reducer (time with a value inside a range) and
`timeInState` are the same computation on numbers and codes.

### 2.6 Device precision

Two different things. A device's resolution (0.1 Cel steps) is a fact about
the device: `source.resolution`. A display precision is a hint:
`metric.displayPrecision`. Neither changes the stored value, which keeps the
digits the device sent as an exact decimal (record 0012). Measurement
uncertainty per value is a real need (a lab's coefficient of variation, a GPS
accuracy radius) with no consumer yet; do not reserve a key for it (record
0020 says an unsettled shape is not reserved).

### 2.7 Very many sources

A house with 60 devices has perhaps 150 series (a multisensor reports four
channels). Sources are cheap: 60 `source` records. The costs are elsewhere:

- **Rollup rows.** 150 series times 96 quarter-hours is 14,400 rows a day,
  5.3 million a year (estimate). A heavy health user is about 20 series,
  0.7 million a year. Postgres holds that, but a second, daily rollup level is
  likely needed for multi-year home charts; it is the same derivation at a
  coarser key and can come later.
- **Changelog lines.** This is the real cost. Hourly traces for 150 series is
  3,600 lines a day, 1.3 million a year (estimate), against 2.17 million
  entries in the owner's whole repository today (proposal section 3). The
  trace period must be chosen per series by rate (the proposal's open question
  8 becomes mandatory): hourly for a power sensor every few seconds, daily for
  a thermometer every five minutes. A daily trace is written when the day
  closes, so it is a day stale. The changelog is append-only, so consolidating
  hourly traces into a daily one later saves nothing.
- **Freshness.** Low-rate categorical series (doors, presence, modes) are
  better as one `observation` per change: a door changes about 20 times a day,
  30 binary sensors make about 600 lines a day (estimate), and the current
  state is fresh. The trait's two variants exist for exactly this split, and
  the adapter chooses per series.
- **Aggregate budgets.** "Average temperature per room per day for a year"
  over 60 rooms is 21,900 output rows, past the proposal's 10,000 cap. The cap
  must count groups times buckets and say so in the refusal.

The position this implies, and that the plan should state: substrate is the
archive and the analytics store for a home, not its live state bus. Home
Assistant keeps doing the second-by-second work; substrate receives series in
bounded batches.

## 3. Package layout

### 3.1 What the machinery does today

- **Cross-sample references work.** `samples/calendar/bundle.yaml` requires
  `people` and `scheduling` with `requiresAtLeast` floors and pins
  `samples.substrate.reamde.dev/people/person`; the import rehomes the pin to
  `<home>/people/person` (record 0048, `internal/vocabulary/rehome.go`).
- **A sample may ship data records of another package's kind, and they are
  rehomed too.** The catalog splits a closure into declarations and data
  documents (`internal/catalog/catalog.go:283`) and rehomes both
  (`catalog.go:545` and `:549`). `samples/llm/providers.yaml` ships rows of
  the seeded `substrate.reamde.dev/llm/provider` kind. So a `home` sample can
  ship `samples.substrate.reamde.dev/observations/metric` records and they land
  as `<home>/observations/metric/...`. Data documents are PUT (merge, never
  prune) after the closure lands.
- **Removing a sample does not remove its shipped rows of another package's
  kind.** `PurgeBundle` tombstones the live data records of the bundle's own
  kinds (`internal/engine/bundles.go:612`), so `home`'s metric rows survive
  `home`'s removal. Acceptable for catalogue rows; it should be documented.
- **A re-import replaces the package whole** and discards the owner's edits
  after a confirmation (records 0048, 0070). A floor binds a copy, not a
  hand-declared package (record 0100).
- **Seeded packages are closed to the user.** `authorizeDeclarationWrite`
  refuses any non-substrate write to a `builtin` package
  (`internal/engine/seed.go:66`). Seeded packages are rebuilt as one unit with
  core; a failure there is a refusal to open the repository, not a quarantine
  (record 0077).
- **The engine recognizes traits, never sample kinds.** Every host-recognized
  behavior today keys on a core trait identity: `accountconfig`, `sync`
  (record 0085), `temporal`, `recurring`. A records read narrows by trait with
  `filter.implements`. A reference may pin a trait (record 0034).

### 3.2 The options

| Option | Engine fit | Upgrade | User extension | Cross-domain sharing | Reversibility |
| --- | --- | --- | --- | --- | --- |
| (a) base kinds in a seeded `substrate.reamde.dev/observations`, domains as samples | good | boot upgrade, unattended | none: `builtin` refuses user writes | one `metric` kind for every domain | `movedFrom` is live for seeded kinds |
| (b) one big `observations` sample with every domain inside | good | one origin-stamped offer for everything | yes, but every upgrade discards edits | full | none without a repository migration |
| (c) `health` as is, a parallel copy of the base kinds per domain | works, the index carries `kind` | per domain | yes | none: a home observation cannot name a weather metric | none |
| (d) the four base kinds in core | good | boot upgrade | none | full | `movedFrom` |
| (e) recommended: core traits and datatype; base kinds in a small `observations` sample; domain samples require it | good | base and domains upgrade separately | in the domains; the base stays pristine | one `metric` kind | none without a repository migration, so get it right first |

Reasoning:

- **(d) is wrong by the house's own rule.** `core.yaml`'s header says core is
  "what exists because the substrate exists", and record 0077 moved even the
  agent runtime's data out of it. A bad declaration in a seeded package stops
  every repository from opening. Record 0015, amended by 0048, keeps unproven
  kinds out of the stable set, which is core plus the providers.
- **(a) fails record 0077's own test.** 0077 seeded `llm` because "the engine
  writes threads and messages from Go constants". The aggregate read and the
  index address a trait and a reference path, never a kind, so nothing forces
  the base kinds into the seed. Seeding would also forbid the user from adding
  a property to `observation`, and put an unproven shape into the boot
  upgrade of every repository, including ones that will never hold a reading.
  Its one real advantage is that a seeded kind can still be moved later.
- **(b) makes every domain a hostage of every other.** One package means one
  version line: a `weather` fix offered as an upgrade discards the owner's
  edits to `health`. 0048's whole-package replace was accepted for packages
  of a few kinds, not for one holding thirty.
- **(c) duplicates the base and breaks sharing.** The engine copes, because
  the index carries `kind` and `filter.implements` finds every binding kind.
  The user does not: two `metric` kinds mean two `air_temperature` records,
  and "indoor versus outdoor temperature" is two metrics that no rule says are
  the same. The base kinds would drift apart in their optional properties
  within a release.
- **(e) follows the `people` precedent**, which records 0070 and 0100 were
  written to make work. The base sample stays small and stable, so its
  version line rarely moves and floors rarely bind. Domain samples hold
  everything a user might edit, and a domain re-import discards only that
  domain's edits.

### 3.3 Rules that make (e) work

- **The base ships few metrics.** The neutral physical metrics several
  domains share (`air_temperature`, `relative_humidity`, `energy`, `power`,
  `illuminance`, `atmospheric_pressure`, `latitude`, `longitude`) are data
  records in `observations`. A domain ships its own with a package-prefixed id
  (`health/heart_rate`, `home/contact_state`), so two domains never write the
  same row and a re-import of one never overwrites the other's. Supplied ids
  admit `/` (proposal section 7).
- **A domain never edits a base kind.** Health-only definition data goes on
  `health/analyte`; health-only context goes in `qualifiers` codes. A
  user who wants a property on `observation` may add it, knowing the next
  base upgrade discards it; the plan should say so.
- **Adapters are samples that require both ends**, as the proposal's section 7
  says. A provider (published) still names no user kind (records 0048, 0049),
  and a function grant cannot name a trait (record 0080 admits globs, not
  traits), so an adapter's `writes` names `<home>/observations/observation`
  and `<home>/observations/trace` exactly.
- **Floors.** `health` declares `requiresAtLeast` on `observations`. Record
  0100 drops the floor where the owner declared their own `observations`
  package by hand, which is the right answer for a user who builds their own.
- **Retiring.** A base kind or property name is retired in the base's header
  and never reused (record 0055). Because user copies cannot receive a move,
  a renamed base kind is a new kind plus a repository migration, so the plan
  should freeze the four kind names before the first import.

## 4. Naming

### 4.1 Collisions found in `docs/terms.md`

| Word in the proposal | Collides with | Severity | Proposal |
| --- | --- | --- | --- |
| `samples` (trait), `samples` (index table), `sample_rollups`, "a sample is one point in a trace" | **sample**, the catalog tier (record 0048). "The health sample's samples trait" is unreadable. | high | trait `observed`; tables `observed_points`, `observed_rollups`, `observed_latest`; one value in a trace is a `point` |
| `subject` (in the owner's question) | the mapping's `subject: true` reference (records 0049, 0096, `docs/projection.md`) | medium | `about` |
| `source` (kind) | a trigger's source (`record`, `schedule`, `webhook`), a package row's `source` (`builtin`, `published`, `installed`), a mapping's source kind | medium | keep, define in `docs/terms.md` (section 4.3) |
| `source.kind`, `tag.kind` | **kind** | medium | `class` |
| `source.model` | **model** (the LLM model id) | low | `hardwareModel` |
| `trace.interval` (spacing) beside `form: interval` | itself | medium | `every` for the spacing |
| `form` values `instant`, `interval` | the `temporal` trait's `point` and `range` variants, which describe the same time shapes with other words | low | `gauge` and `amount` (section 4.2) |
| aggregate `"window": {"before", "after"}` | **window read** | low | `around` |
| "feature of interest" | **feature** (a `server.json` entry) | avoided | `about` |
| `state` for door values | the `state` datatype | avoided | `code` |
| `series` | **series** | avoided already | the proposal uses `trace` |

### 4.2 The base kinds and the trait

| Candidate | Home sensor | Blood test | Verdict |
| --- | --- | --- | --- |
| `observation` | "a temperature observation" | FHIR's word for a lab result | keep for one value. Covers categorical and self-reported values, which `measurement` does not. |
| `reading` | natural | odd for a lab panel | no |
| `measurement` | natural | natural | no: a door state or a mood is not measured. OMOP needs both words for this reason. |
| `metric` | "the metric `air_temperature`" | "the metric `ldl_cholesterol`" | keep for the definition. `property` is taken (a declared value), `signal` suggests waveforms, `parameter` is vague. |
| `trace` | "the 14:00 trace of the living-room temperature" | ECG traces | keep for the packed record. |
| `signal` | | | no; reserve nothing |
| `source` | a sensor | a lab | keep, defined (4.3) |
| trait `observed` | | | an adjective, like `recurring`: "a kind whose records carry observed values the engine indexes". Variants `single` (an observation) and `packed` (a trace). |

The proposal's trait variants `point`, `interval` and `packed` mixed layout
with meaning. Once the form lives on the value, the variants describe only
layout: one value per record, or many.

**The forms.** `instant` and `interval` describe time; the form's job is to
say what the number means, which decides the legal reducers. The
Prometheus and OpenMetrics names say that directly and every IoT reader knows
them:

| Form | Was | Means | Legal reducers |
| --- | --- | --- | --- |
| `gauge` | `instant` | a level at an instant, held for `hold` | `avg`, `twa`, `min`, `max`, `first`, `last`, `count`, `stddev`, `percentile`, `durationIn`; on codes `count`, `last`, `timeInState` |
| `amount` | `interval` | a quantity accrued over `[at, endsAt)` | `sum`, `count`, `min`, `max` |
| `counter` | `counter` | a running total that may reset | `increase`, `rate`, `last` |
| `summary` | `summary` | a statistic over a stated period | read as is |

This is a cheap change before anything is built and a dialect event after.

### 4.3 Defining `source`

`source` reads well for a watch, a lab, a manual entry and a weather service,
which `device` and `sensor` do not. Its collisions are with fields and
prose, not with a term or a kind. Add it to `docs/terms.md` as "a device,
app, service or lab that produces observations; the `source` of a trigger and
a package row's `source` are other fields". If the owner prefers zero
ambiguity, `device` is the alternative every IoT model uses, at the cost of
"a manual device" and "a lab device".

## 5. The unit table

### 5.1 A core facility, like the currency table

Yes. The arguments of record 0140 carry over directly: a stored value is read
through the table, so a code is added and never removed or redefined; the
console holds a copy equal by test; the Python module for functions is
generated from the same source. Differences from currencies that the record
must state:

- **Conversion, not just a minor unit.** Each entry is (code, dimension,
  exact rational factor to the dimension's base, optional offset, flags:
  `metric` for prefixes, `special` for non-linear). Conversion uses exact
  rationals (proposal 5.2).
- **The table is a set of atoms plus a bounded grammar, not a list of
  strings.** Home and vehicle data is full of compound units: `kW.h`, `m3/h`,
  `ug/m3`, `L/(100.km)`, `km/h`. Listing each compound does not scale. The
  table holds atoms and prefixes, and the engine accepts products, quotients
  and integer powers of them, which UCUM's limited conformance allows
  (codex review P2.22).
- **Annotations are free.** UCUM's `{annotation}` is dimensionless and does
  not change meaning, so `{steps}`, `{beats}/min` and `{dose}` cost no table
  entry. This is the answer to most "let a user declare a unit" requests,
  and it is why dimension can never decide that steps and beats differ: the
  metric does.
- **Gaps checked against `ucum-essence.xml`.** Present: `[ppm]`, `[ppb]`,
  `lx`, `Cel`, `[degF]`, `bar`, `[psi]`, `[gal_us]`, `[mi_i]`, `[pH]`, `%`,
  `deg`, `Ohm`, `V`, `A`, `Hz`, `B[W]`, `B[kW]`, `B[SPL]`. Absent: `var`
  (reactive power), `VA` (UCUM spells it `V.A`), `Wh` (UCUM spells it
  `W.h`), and any spelling of dBm (there is `B[W]` and `B[kW]`, no
  `B[mW]`). Log units (`B[SPL]`, `B[W]`) are `special` and need a function,
  not a factor: store them, refuse `sum` and `avg` across them, convert
  nothing in v1. Reactive power is a power by dimension; the metric's
  `compatibility` keeps it apart from real power, which is the same rule the
  proposal already states for HbA1c.
- **An alias table on input.** Home Assistant writes `°C`, `kWh`, `m³`,
  `µg/m³`; the table maps them to `Cel`, `kW.h`, `m3`, `ug/m3` and stores the
  code alone.

### 5.2 Who may add a unit

A release, only. A user-declared `unit` record was considered and should be
refused for v1:

- The derived index computes `value_base` in SQL and the console converts in
  TypeScript; a user unit must reach both with identical exact arithmetic, so
  it would be a declaration the index reads (legal, the index is a function
  of rows and declarations) and a second, runtime-loaded conversion table on
  every surface.
- A unit is global by nature (a UCUM code means one thing everywhere) and a
  declaration is repository-scoped: an export from one repository would carry
  `[smoot]` meaning something another repository's `[smoot]` does not.
- New declaration keys are reserved by name and land validated (record
  0020). A `unit` meta-kind is a dialect event for every binary, spent on a
  need nobody has shown.

What covers the real cases instead: annotations for counts; the alias table
for spellings; `sourceValue` and `sourceUnit` for a reading whose unit the
table refuses, so the row is kept, listed and later normalised when a release
adds the unit. Reopen trigger for a user unit: a deployment must aggregate a
quantity whose unit the table refuses and no release can add in time.

### 5.3 Can a `propertytype` carry the unit instead?

As a narrowing, yes; as a definition, no. `propertytype` refines a base
datatype with `pattern`, `min`, `max` or `values`
(`kinds/substrate.reamde.dev/core/propertytype.yaml`), its `base` enum has no
`quantity` today, and it is package-local. Adding `quantity` to `base` and
admitting the same `dimension` and `units` keys a `quantity` property takes
lets a package say `airtemperature: {base: quantity, dimension: temperature}`
once. It cannot define a unit (the engine must know the factor), and being
package-local it cannot be the cross-domain seam. Worth doing with the
datatype, since the keys are the same reservation.

## 6. The aggregate read beyond proposal section 5.5

### 6.1 New reducers

| Reducer | Legal on | Computed from | Notes |
| --- | --- | --- | --- |
| `twa` | numeric gauge | rollup `twa_sum`, `held_ms`, plus the tail step | the default a console should pick for on-change data |
| `timeInState` | coded gauge | rollup `held_ms` per code, plus the tail | `{"timeInState": ["open"]}` or all codes; result is a duration per code |
| `increase` | counter | rollup `increase`, plus the step between rows, reset-aware | no extrapolation to the window edges (Prometheus extrapolates); a bucket with partial coverage says so. Exact or refused, record 0012. |
| `rate` | counter | `increase` divided by the bucket's covered duration | result unit is the counter's unit per time (`kW.h/h` reads as `kW`) |
| `circularMean` | gauge with dimension `angle` | rollups add the sums of sine and cosine | wind direction, heading; `avg` on an angle is refused. Home Assistant has the same split (`StatisticMeanType.CIRCULAR`, checked in `raw-iot/rec_models_stats.py:53`). |

### 6.2 `groupBy`

`groupBy` takes index columns only: `source`, `about`, `code`, `metric`,
`kind`. Each is in the rollup key, so grouping never forces a raw scan.
Cardinality is capped and counts against `maxBuckets` as groups times
buckets. Grouping by a property of the `about` record (the room's floor) is a
join through `about`: resolve the small set of `about` records that match a
filter first, then group by a mapping from `about` to the label. Leave it out
of v1; a function can do it from rows grouped by `about`.

### 6.3 `latest`: the current state of everything

"The current state of every sensor in the house" is the commonest home read
and the cheapest: one row per series. It should not be a fourth mode:

- It is an aggregate with no `bucket` and the reducer `last`, grouped by
  series. Using the aggregate mode keeps one allow-list, one budget and one
  response struct.
- It needs `metric` to accept a list or be omitted when the only reducer is
  `last`, because "every sensor" spans metrics.
- It is served by a third derived table, `observed_latest`, keyed by
  (repository, kind, metric, source, about), holding the last point's time,
  value, code and record id. The fold updates it when a point is newer than
  the stored one; a tombstone or purge of the record holding the latest point
  recomputes that series from `observed_points`.

A list filter cannot express it: "the newest record per (metric, source,
about)" is a group-wise maximum, not a predicate on one record.

### 6.4 Time

Home data has no per-point offset problem: one house, one zone. The named-zone
calendar mode (`{"calendar": "day", "timezone": "Europe/Athens"}`) is the
normal request, and the adapter writes the house's offset on each trace so
the recorded-offset mode agrees with it.

## 7. Risks and the recommendation

### 7.1 Everything is an observation

The failure: a generic `observation` table that nobody can query without the
catalogue, with every domain's things flattened into (metric, value) pairs.
That is the entity-attribute-value pattern, and its cost is that nothing has a
shape a filter can name. The design avoids it by three rules the plan should
state as rules:

1. **A thing with identity or a lifecycle is a kind.** A car, a room, a
   bill, a workout, a sleep, a lab report: typed kinds in their domain. An
   observation is only a number or a code over time about a thing.
2. **The value carries its meaning.** Form, unit, statistic, period and hold
   are on the value; an observation read alone is interpretable without the
   catalogue (proposal principle 6).
3. **The index is the query surface, not the catalogue.** The aggregate reads
   `observed_points` by metric reference; a list filter on
   `properties.metric` is an indexed scalar reference. A reader needs the
   metric's id, not its definition.

### 7.2 Over-generalising the catalogue

No universal catalogue. The base ships a few shared physical metrics; each
domain ships what its own adapters write and nothing else. A catalogue row
with no adapter writing it is a promise nobody keeps, which is the lesson of
the deleted `samples/health` and `samples/fitness` (#498, proposal section 3).

### 7.3 Two half-done domains

The real risk. Health alone is four phases in the proposal. A second domain
built in parallel doubles the adapters, catalogues and console work, and the
engine gets tested by neither to the end. So: one domain to done, one minimal
domain to prove the seams, and nothing else.

### 7.4 Volume

Section 2.7. A home is a write-heavy source and substrate's write path costs
about 1 KB of changelog per line (proposal section 3). The plan must state
that the home domain archives in batches and that live state is Home
Assistant's.

### 7.5 The recommendation, as the smallest change set

Changes to the health proposal, all before anything is built:

1. Rename the trait to `core/observed` with variants `single` and `packed`;
   rename the index tables to `observed_points`, `observed_rollups`; call a
   value inside a trace a `point`.
2. Put `about` (reference, `kind: any`), `code`/`codes`, `hold`,
   `resetRule` and `resetAt` in the trait's first version and in the binding
   kinds. Health writes none of them. (Fact 1: a trait cannot grow later
   without breaking every user's copy.)
3. Put `about` and `code` in the rollup key, and `held_ms`, `twa_sum`,
   `last_hold_ms`, `increase` and `resets` in the rollup row, in the first
   migration. Derived tables rebuild, so this is cheaper to change than (2),
   but it shapes the fold hook.
4. Rename the forms to `gauge`, `amount`, `counter`, `summary`, keep counters
   in the index, and rename `trace.interval` to `every`.
5. Move `metric`, `observation`, `trace` and `source` into a base sample,
   `samples.substrate.reamde.dev/observations`, before the first record is
   written. `health` requires it and adds `analyte` for the lab-only
   definition properties. (Fact 2: a sample's kind cannot be moved later.)
6. Rename `source.kind` and `tag.kind` to `class`, `source.model` to
   `hardwareModel`; replace `observation.context` with coded `qualifiers`.
7. Organise the unit table by dimension with atoms, prefixes and a bounded
   product grammar, plus annotations and an alias table; release-only growth.

Then, unchanged: health phases 1 to 4. After health phase 2 (the index and
the aggregate exist), a `home` sample with `area` and `fixture` kinds and a
Home Assistant adapter, scoped to one acceptance list: average temperature
per room per day, kWh per day from a resetting meter, hours a door was open
per week, and the current state of every series. Each of those exercises one
seam health never touches. `energy`, `weather` and `vehicle` wait until `home`
has a consumer.

What this adds to the engine work of health phase 1 and 2: three trait
properties and two columns health leaves empty, and three reducers that can
wait for the `home` phase (`twa`, `timeInState`, `increase`) because their
rollup columns already exist. Nothing else.

## 8. Sketch

All four base kinds are in the base sample, authored under the placeholder
authority and rehomed on import. Comments say why; descriptions say what.

### 8.1 The trait (core)

```yaml
# what makes a kind's records indexed observations: the fold projects every
# binding kind's values into observed_points and observed_rollups. Every
# property here is contracted from the first version, because a trait cannot
# gain one later without refusing every copy that binds it.
kind: substrate.reamde.dev/core/trait
metadata:
  id: substrate.reamde.dev/core/observed
data:
  authority: substrate.reamde.dev
  package: core
  description: "observed values the substrate indexes: one per record, or
    many packed into one record over a fixed period"
  oneOf:
    - name: single
      properties:
        at: datetime
        endsAt: datetime
        metric: reference
        source: reference
        about: reference
        form: string
        value: quantity
        code: string
        statistic: string
        period: string
        hold: duration
        resetRule: string
        resetAt: datetime
        utcOffset: int
        revision: int
    - name: packed
      properties:
        at: datetime
        endsAt: datetime
        metric: reference
        source: reference
        about: reference
        form: string
        unit: string
        every: duration
        offsets: int
        values: decimal
        codes: string
        hold: duration
        resetRule: string
        resetAt: datetime
        utcOffsetStart: int
        utcOffsetEnd: int
        revision: int
        stats: json
```

The host, not the trait, validates what a trait cannot express (codex P2.17):
exactly one of `value` and `code` (or neither with `sourceValue`), equal
lengths of `offsets` and `values` or `codes`, ordered offsets inside the
period, a `form` from the four words, `statistic` and `period` present on a
summary, `resetRule` only on a counter.

### 8.2 `metric`

```yaml
kind: substrate.reamde.dev/core/kind
metadata:
  id: samples.substrate.reamde.dev/observations/metric
data:
  authority: samples.substrate.reamde.dev
  package: observations
  description: >-
    The definition of something that can be observed: a room's temperature, a
    meter's energy, a door's open or closed state, a heart rate. A catalogue
    entry and a guide for writers; the meaning of a stored value is on the
    value itself.
  version: 1
  purpose: primary
  names:
    singular: metric
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
      description: the metric's heading, one line
    description:
      type: markdown
      description: what is observed and how to read it
    codes:
      type: object
      repeated: true
      description: the same metric in other code systems
      properties:
        system:
          type: string
          description: the code system, such as loinc, mdc, homeassistant
        code:
          type: string
        version:
          type: string
        display:
          type: string
    valueKind:
      type: enum
      values: [quantity, code, ratio]
      default: quantity
      description: whether a value is a number with a unit, a code, or a ratio
    dimension:
      type: string
      description: the dimension of the canonical unit, such as temperature
    unit:
      type: string
      description: the canonical UCUM code writers convert to
    delta:
      type: bool
      description: true when values are differences, such as a deviation
    codeSet:
      type: string
      repeated: true
      description: the codes a categorical metric admits, such as open, closed
    form:
      type: enum
      values: [gauge, amount, counter, summary]
      description: the form writers stamp on each value by default
    hold:
      type: duration
      description: how long a gauge value stands, copied onto each value
    reducers:
      type: string
      repeated: true
      description: the reducers that make sense for this metric
    compatibility:
      type: string
      description: the class within which values may be converted or pooled
    displayPrecision:
      type: int
      description: decimal places a reader shows
    algorithm:
      type: object
      description: the vendor algorithm behind a computed score
      properties:
        vendor:
          type: string
        name:
          type: string
        version:
          type: string
```

### 8.3 `observation`

```yaml
kind: substrate.reamde.dev/core/kind
metadata:
  id: samples.substrate.reamde.dev/observations/observation
data:
  authority: samples.substrate.reamde.dev
  package: observations
  description: >-
    One observed value of one metric, about one thing, from one source: a
    weighing, a door opening, a monthly bill's total, a nightly HRV.
  version: 1
  purpose: supporting
  names:
    singular: observation
  traits:
    - substrate.reamde.dev/core/temporal(range)
    - substrate.reamde.dev/core/observed(single)
  displayTemplate: "{metric} {value}{code}"
  properties:
    metric:
      type: reference
      kind: samples.substrate.reamde.dev/observations/metric
      required: true
      mustExist: true
      description: what was observed
    source:
      type: reference
      kind: samples.substrate.reamde.dev/observations/source
      description: the device, app, service or lab it came from
    # any kind: a room, a car, a plant, a place. Absent means the owner.
    about:
      type: reference
      kind: any
      description: the thing observed; absent means the repository's owner
    form:
      type: string
      required: true
      description: gauge, amount, counter or summary
    value:
      type: quantity
      description: the number and its unit
    code:
      type: string
      description: the categorical value, such as open
    statistic:
      type: string
      description: for a summary, the statistic it is (sum, avg, min, max, last)
    period:
      type: string
      description: for a summary, the period it covers (hour, day, bill)
    periodId:
      type: string
      description: the source's own identity for that period
    hold:
      type: duration
      description: how long a gauge value stands without a newer one
    resetRule:
      type: string
      description: for a counter, decrease or declared
    resetAt:
      type: datetime
      description: for a counter, when the current accumulation began
    utcOffset:
      type: int
      description: minutes east of UTC where the value was observed
    qualifiers:
      type: object
      repeated: true
      description: coded context, such as after a meal or window open
      properties:
        system:
          type: string
        code:
          type: string
    components:
      type: object
      repeated: true
      description: the parts of a value observed together
      properties:
        metric:
          type: reference
          kind: samples.substrate.reamde.dev/observations/metric
        value:
          type: quantity
    sourceValue:
      type: string
      description: the reading as received, kept when its unit is unknown
    sourceUnit:
      type: string
      description: the unit as received
    recordingMethod:
      type: enum
      values: [automatic, active, manual]
    origin:
      type: reference
      kind: any
      description: the mirror row this was derived from
    revision:
      type: int
      description: rises with each rewrite; a lower one is refused
```

### 8.4 `trace` and `source`

```yaml
kind: substrate.reamde.dev/core/kind
metadata:
  id: samples.substrate.reamde.dev/observations/trace
data:
  authority: samples.substrate.reamde.dev
  package: observations
  description: >-
    The points of one metric, about one thing, from one source, over one fixed
    period, packed into one record.
  version: 1
  purpose: supporting
  names:
    singular: trace
  traits:
    - substrate.reamde.dev/core/temporal(range)
    - substrate.reamde.dev/core/observed(packed)
  properties:
    # metric, source, about, form, hold, resetRule, resetAt, revision and
    # origin as on observation; omitted here.
    unit:
      type: string
      description: the UCUM code every point is in
    every:
      type: duration
      description: the spacing of regular points from at
    offsets:
      type: int
      repeated: true
      description: milliseconds from at, for irregular points
    values:
      type: decimal
      repeated: true
      description: the numbers; a null is a gap
    codes:
      type: string
      repeated: true
      description: the codes, for a categorical trace
    utcOffsetStart:
      type: int
    utcOffsetEnd:
      type: int
    stats:
      type: json
      managed: true
      description: count, min, max, mean, m2, first, last, coverage
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: samples.substrate.reamde.dev/observations/source
data:
  authority: samples.substrate.reamde.dev
  package: observations
  description: >-
    A device, app, service or lab that produces observations.
  version: 1
  purpose: primary
  names:
    singular: source
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
    class:
      type: string
      description: watch, ring, scale, thermometer, meter, contact, app, lab
    manufacturer:
      type: string
    hardwareModel:
      type: string
    firmware:
      type: string
    resolution:
      type: quantity
      description: the smallest step the device reports
    about:
      type: reference
      kind: any
      description: the thing the device is fixed to; writers copy it
    account:
      type: reference
      trait: substrate.reamde.dev/core/accountconfig
      description: the connected account it syncs through
    priorities:
      type: int
      keyed: true
      description: this source's rank per metric when sources overlap
```

There is no `subject` kind. The `home` sample declares the things:

```yaml
kind: substrate.reamde.dev/core/kind
metadata:
  id: samples.substrate.reamde.dev/home/area
data:
  authority: samples.substrate.reamde.dev
  package: home
  description: >-
    A room or zone of a home: the living room, the garden.
  version: 1
  names:
    singular: area
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
    floor:
      type: string
---
kind: substrate.reamde.dev/core/kind
metadata:
  id: samples.substrate.reamde.dev/home/fixture
data:
  authority: samples.substrate.reamde.dev
  package: home
  description: >-
    A fixed part of a home that is observed: a door, a window, a meter, a
    boiler.
  version: 1
  names:
    singular: fixture
  displayTemplate: "{name}"
  properties:
    name:
      type: string
      required: true
    area:
      type: reference
      kind: samples.substrate.reamde.dev/home/area
```

### 8.5 Three records

As they land in `ada.example.com` after import. `ha` is the Home Assistant
account record's id.

A living-room temperature, one hour from an on-change sensor, packed:

```yaml
kind: ada.example.com/observations/trace
metadata:
  id: homeassistant/ha/sensor.living_room_temperature/2026-10-07T14
data:
  properties:
    metric: {ref: ada.example.com/observations/metric/air_temperature}
    source: {ref: ada.example.com/observations/source/ha-sensor.living_room_temperature}
    about: {ref: ada.example.com/home/area/living_room}
    form: gauge
    at: 2026-10-07T14:00:00Z
    endsAt: 2026-10-07T15:00:00Z
    utcOffsetStart: 180
    utcOffsetEnd: 180
    unit: Cel
    offsets: [0, 1260000, 2710000]       # on change: three points this hour
    values: ["21.5", "21.6", "21.4"]
    hold: PT2H                            # the sensor reports at least every hour
    revision: 1
    origin: {ref: providers.substrate.reamde.dev/homeassistant/state/...}
```

The first offset is 0 because the adapter carries the last value of the
previous hour in at the boundary; without it the hour would start with a gap
and `twa` would lean on the read-time tail step.

A house energy meter, cumulative, one hour:

```yaml
kind: ada.example.com/observations/trace
metadata:
  id: homeassistant/ha/sensor.house_energy_import/2026-10-07T14
data:
  properties:
    metric: {ref: ada.example.com/observations/metric/energy_imported}
    source: {ref: ada.example.com/observations/source/ha-sensor.house_energy_import}
    about: {ref: ada.example.com/home/fixture/main_meter}
    form: counter
    resetRule: decrease                   # Home Assistant total_increasing
    at: 2026-10-07T14:00:00Z
    endsAt: 2026-10-07T15:00:00Z
    utcOffsetStart: 180
    utcOffsetEnd: 180
    unit: kW.h
    every: PT15M
    values: ["18342.112", "18342.391", "18342.655", "0.204"]   # a reset at 14:45
    revision: 1
```

The index stores four points. The rollup row for 14:45 has `increase`
0.204 and `resets` 1; the step from 18342.655 to 0.204 is read as a reset,
not as a negative 18342 kW.h.

A front-door state change, one record:

```yaml
kind: ada.example.com/observations/observation
metadata:
  id: homeassistant/ha/binary_sensor.front_door/2026-10-07T17:42:05Z
data:
  properties:
    metric: {ref: ada.example.com/observations/metric/home/contact_state}
    source: {ref: ada.example.com/observations/source/ha-binary_sensor.front_door}
    about: {ref: ada.example.com/home/fixture/front_door}
    form: gauge
    code: open
    at: 2026-10-07T17:42:05Z
    utcOffset: 180
    revision: 1
```

`hold` is absent: the state stands until the next change, capped at the
request's `maxGap`.

### 8.6 Four requests

All are `GET /api/v1/records?filter=...&aggregate=...`; the filter selects
eligible records, the aggregate reduces their points.

Average temperature per room per day, one month:

```json
{
  "filter": {"kinds": ["ada.example.com/observations/trace"]},
  "aggregate": {
    "metric": {"ref": "ada.example.com/observations/metric/air_temperature"},
    "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
    "bucket": {"calendar": "day", "timezone": "Europe/Athens"},
    "groupBy": ["about"],
    "reducers": ["twa", "min", "max"],
    "maxGap": "PT3H",
    "unit": "Cel"
  }
}
```

kWh per day, one month:

```json
{
  "filter": {"properties": {"about": {"eq": "ada.example.com/home/fixture/main_meter"}}},
  "aggregate": {
    "metric": {"ref": "ada.example.com/observations/metric/energy_imported"},
    "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
    "bucket": {"calendar": "day", "timezone": "Europe/Athens"},
    "reducers": ["increase"],
    "unit": "kW.h"
  }
}
```

Hours the front door was open per week:

```json
{
  "filter": {"properties": {"about": {"eq": "ada.example.com/home/fixture/front_door"}}},
  "aggregate": {
    "metric": {"ref": "ada.example.com/observations/metric/home/contact_state"},
    "range": {"gte": "2026-07-06T00:00:00+03:00", "lt": "2026-10-05T00:00:00+03:00"},
    "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
    "reducers": [{"timeInState": ["open"]}],
    "maxGap": "P1D",
    "unit": "h"
  }
}
```

The current state of every series in the house:

```json
{
  "filter": {"implements": "substrate.reamde.dev/core/observed"},
  "aggregate": {
    "groupBy": ["metric", "source", "about"],
    "reducers": ["last"]
  }
}
```

No `metric`, no `bucket`, no `range`: served from `observed_latest`, one row
per series, capped by the series budget.

## 9. Open questions for the owner

1. `about` or `subject` for the observed thing? `about` avoids the mapping's
   `subject: true`; `subject` is the standard word (FHIR, SOSA).
2. `source` defined in `docs/terms.md`, or `device`?
3. The form names: `gauge`/`amount` (recommended) or keep `instant`/`interval`?
4. Base sample (recommended) or a seeded package for the four base kinds?
   Seeding buys the ability to move kinds later and costs user extension and
   every repository's boot.
5. Which second domain: `home` through Home Assistant (recommended, it tests
   four seams), or `energy` alone (tests one)?
6. Should the home adapter ingest Home Assistant's raw state history, its
   five-minute or hourly statistics, or both? Statistics are low volume and
   already time-weighted but arrive as `summary` values; raw history is
   exact and costs more lines.
