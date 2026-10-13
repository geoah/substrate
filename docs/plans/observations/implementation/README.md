# Observations: the implementation brief

Status: built, and reviewed on 2026-10-12 and 2026-10-13. This page is
what every agent building the observations work read first. It fixed the
decisions the plan left open, listed the work packages and their order,
pinned the wire contract of the aggregate read so the engine and the console
agreed before either existed, and named the rules every change was held to.
The spec itself is the proposal under [`../proposal/`](../proposal/README.md)
(the end-to-end proposal, with worked examples) and
[`../../observations.md`](../../observations.md) (the plan, whose section 13
supersedes its earlier sections where they differ). Where this page and the
proposal disagree, this page wins, because it records the decisions taken to
build.

Everything [in scope](#scope-of-this-build) is built: the `quantity` type
and the unit table, the `observed` trait, the `observations` and `health`
samples, the observed index, the aggregate read with its CLI command, host
call and agent tool, the four providers and their adapters, the console's
observations pages, synthetic data and decision records 0153 to 0164. The
review added to the aggregate read's contract (marked in
[the wire contract](#the-aggregate-read-wire-contract)) and changed the
adapters, the catalogue and the index; the reference pages
([observations](../../../observations.md), [units](../../../units.md),
[the aggregate read](../../../api.md#the-aggregate-read)) describe the
result. What is left:

- The decision records are `proposed`; the owner accepts or amends each.
- Nothing expires a point yet, so `unavailable` is always `false`.
- Nothing refuses `sum` over a level (`dB`, `[pH]`).
- The Garmin and Google Fit providers are tested against mock upstreams
  only, and Garmin and Apple Health push no deletion.
- A WHOOP night (`health/sleep`) carries no reference to its cycle's
  recovery, and WHOOP's nightly values (heart rate variability, resting
  heart rate, SpO2, skin temperature) are summaries of the cycle that
  begins at waking, so a day bucket by `place` `end` puts them a day after
  their night, where Garmin's nightly summaries need `end`.
- The console has no raw-line view of a downsample, no reference-range
  marker on a lab chart, no "today" view and no y scale on a stream's
  sparkline.
- Every item listed [out of scope](#scope-of-this-build).

The four `map-*.md` pages beside this one are snapshots of the codebase taken
on 2026-10-11 by read-only agents: where value types are validated, where the
fold derives tables, how a provider is built, how the console is laid out.
Line numbers drift; the file names and function names are the point.

## Decisions taken for this build

Each open question of the plan (section 12) and the proposal's "Decisions
still open" table is closed here with the recommended option, so building can
start. A decision record is written for each in work package 7; the owner may
reverse any of them there, and the code is written so a reversal is a rename.

| Question | Decision |
| --- | --- |
| the declaration key for a quantity's kind | `measures:` (a named quantity kind such as `mass`; never a bare dimension vector) |
| the name of the thing observed | `about` |
| the form names | `gauge`, `amount`, `counter`, `summary` |
| a persistent stream record | none; `observed_latest` is the stream list |
| money as an observed value | no; a balance is a typed kind read through the records path |
| the arbitrary-unit prefix rule | kept, as a labelled substrate rule: the same arbitrary atom with the same annotation may be rescaled by a metric prefix (`m[IU]/L` to `[IU]/L`) |
| `dBm` | a local code with a documented definition |
| retention tiers | deferred; every point is kept; the response shape already carries `unavailable` |
| the word for a vendor-method summary | `statistic: vendor` |
| the `prefer-raw` coverage floor | `minCoverage`, default 0.9 |
| how the trait grows | a second trait bound beside `observed`, never a property added to it |
| a `null` inside a trace | unavailable from here: it ends the hold and counts in nothing |
| counter contracts | `monotonic`, `signed`, `wrap`; `wrapAt` on the value; no drop tolerance |
| the second domain | out of scope for this build (health only); the engine pieces stay domain-neutral |
| the shared Python module for adapters | not a copied module: the host SDK (`host.py`) gains `host.units.*` (served by the Go table over the host call) and `host.observed.*` (a pure-Python trace builder and local-day helper), so there is one unit table |

All fourteen verdicts of the 2026-10-10 review (observations.md section 13)
are in force.

## Scope of this build

In: the `quantity` type and unit table; the `observed` trait; the
`observations` and `health` samples; write validation and managed `stats`;
the three derived tables with their fold hooks, RLS and rebuild; the
aggregate read with the four time modes, the typed reducers (including
`twa`, `timeInState`, `increase`, `rate`, `circularMean`, `percentile`,
`durationIn`), `groupBy`, the latest read, `from`, source policies, `fill`,
`rolling`, budgets and `downsample`; the CLI `aggregate` command;
`host.records.aggregate` and the agent `query` tool's `aggregate` argument;
four providers and adapters (WHOOP adapter over the existing provider, Apple
Health through the Health Auto Export app, Garmin, Google Fit) with mock
upstreams and tests; console pages for observations; decision records; a
benchmark of the write and rebuild path.

Out, each a follow-up with its own consumer: the `home` sample and Home
Assistant; `geopoint` and `track`; FIT, TCX and GPX file import (blocked on
`host.blobs.get`); the Apple `export.xml` import job; lab PDF and FHIR
import; TimescaleDB; percentile sketches; retention tiers; a second person in
health.

Two providers carry a dated caveat the README of each must state: Google
announced the Fit REST API ends with 2026 and closed new sign-ups in 2024, and
Garmin's Connect Developer Program was reported closed to new partners through
mid-2026. Both are built against the documented shapes and tested against
mock upstreams; neither is verified against a live account here.

## Work packages, in order

| WP | Builds | Depends on | Worktree |
| --- | --- | --- | --- |
| 1 | `quantity` value type, the unit table and conversions, filters and ordering on quantities, the console's quantity rendering and editing, docs | nothing | this one |
| 2 | the `observed` core trait; the `observations` and `health` samples; write validation of the trait's cross-property rules; managed `stats`; `host.units.*` and `host.observed.*` in the host SDK; docs | 1 | this one |
| 3 | migration 0012 with `observed_points`, `observed_rollups`, `observed_latest`; fold hooks on every path; rebuild; the refold test seed; the benchmark | 2 | this one |
| 4 | the aggregate read: contract, engine, API mode, fake, wire golden, CLI, host call, agent tool; docs | 3 | this one |
| 5a | `applehealth` provider (Health Auto Export receiver) and `healthapple` adapter, fixtures, tests | 2 | isolated |
| 5b | `healthwhoop` adapter over the existing WHOOP provider, fixtures, tests | 2 | isolated |
| 5c | `garmin` provider and `healthgarmin` adapter, fixtures, tests | 2 | isolated |
| 5d | `googlefit` provider and `healthgooglefit` adapter, fixtures, tests | 2 | isolated |
| 6 | console: observations pages (streams table with filters, timeline grouped by metric and filtered by source, rollup charts, trace and observation detail) | 4 for integration; the contract below to start | isolated |
| 7 | decision records for the decisions above and the plan's section 10 | 4 | this one |
| 8 | independent Codex review, fixes, full suites, pull requests | all | this one |

## The aggregate read: wire contract

Field names below are the contract. Work package 4 implemented them in Go
(`internal/substrate`: `Aggregate`, `AggregateMetricRead`, `AggregateProperty`,
`AggregateBucket`, `AggregateRecords`, `AggregateRecordSources`,
`AggregateSources`, `AggregateRolling`, `AggregateBudget`, `Downsample`,
`AggregateResult`, `AggregateRow`, `AggregateValue`, `AggregateGroup`) and
work package 6 mirrors them in `types.ts`, held together by
`wire.golden.json`. An implementer may add a field, never rename one.
Everything optional is `omitempty`. [The API reference](../../../api.md#the-aggregate-read)
says what each field does; this section lists them, and marks each field
that arrived after this brief was written: *build* for one work package 4
added, *review* for one the 2026-10-13 review added.

Request: the `aggregate` query parameter on `GET /api/v1/records`, strict
JSON, refused beside `q`, `watch`, `count`, `orderBy`, `first`, `after`,
`offset`, `expand` and `withAnnotations`.
[The API reference's first example](../../../api.md#the-aggregate-read) is
a request and its answer.

| Key | Fields | Arrived |
| --- | --- | --- |
| selector | exactly one of `metric` (`{ref}`), `metrics` (`[{ref, unit?, reducers?, from?, statistic?}]`, at most 16) and `property` (`{name, anchor?, utcOffset?, groupBy?, sources?}`) | `metrics`, `property.utcOffset`, `property.groupBy`, `property.sources`: review |
| `from`, `minCoverage` | `raw` (default), `summaries` or `prefer-raw`; prefer-raw's floor, default 0.9 | |
| `range` | `{gte, lt}` | |
| `bucket` | `{every, origin?, place?}`; `{calendar, timezone?, fallbackTimezone?, weekStart?, offset?, place?}` (`calendar` in `minute`, `hour`, `day`, `week`, `month`, `year`; no `timezone` is the recorded-local day); `{records: {filter, around?: {before, after}, maxRecords?, sources?, utcOffsets?: {start, end?}}}` | `fallbackTimezone`, `place`, `records.sources`, `records.utcOffsets`: review |
| `groupBy` | `source`, `about`, `code`, `metric`, `kind`, `channel`, `statistic` | `channel`: build; `statistic`: review |
| `reducers` | `count`, `sum`, `avg`, `twa`, `min`, `max`, `first`, `last`, `stddev`, `{"percentile": q}`, `{"durationIn": [lo, hi]}`, `{"timeInState": [...]}`, `increase`, `rate`, `circularMean` | |
| `sources` | `{policy, order?}`: `separate` (default), `priority` or `summaries` | |
| `unit`, `maxGap`, `fill` | a unit; an ISO 8601 duration; `null`, `zero` or `locf` | |
| `rolling` | `{buckets, reducers?, through?}` | `through`: review |
| `budget` | `{maxBuckets?, maxSamples?, maxGroups?}` | `maxGroups`: build |
| `downsample` | its own query parameter, `{points, method?}` (`m4`) | |
| `filter.properties.statistic` | narrows a metric read's summaries | review |

- No `bucket`, no `range` and the one reducer `last` is the latest read: the
  newest point of each group.
- A records source policy (`bucket.records.sources`, `property.sources`) is
  `{policy, order?, property?, match?}`, `policy` being `separate` or
  `priority`.
- `downsample` is refused beside `reducers`, `rolling`, `fill` and `bucket`.

Response:

| Key | Fields | Arrived |
| --- | --- | --- |
| result | `metric`, `property`, `unit`, `units`, `bucket`, `from`, `exact`, `rows`, `sources`, `head: {changelog, generation}` | `property`: build; `units`: review |
| row | `start`, `end`, `form`, `group`, `record: {ref, version}`, `from`, `sources`, `coverage`, `values`, `count`, `offsetless`, `prorated`, `bounded`, `synthetic`, `unavailable`, `nonAdditive` | `nonAdditive`: build; `offsetless`, `prorated`: review |
| `group` | `source`, `about`, `code`, `metric`, `kind`, `channel`, `statistic`, `currency`, `properties` | `channel`, `currency`: build; `statistic`, `properties`: review |
| value | `reducer`, `value`, `unit`, `code`, `at`, `rolling`, `of`, `n` | `rolling`, `of`: build; `at`, `n`: review |

- `values[].value` is a decimal string, or `null` when the reducer has no
  answer for the row; it is always present. `values[].unit` is absent for
  `count`.
- `form` on every row is the form of the points that fed it (`gauge`,
  `amount`, `counter` or `summary`); a row never mixes forms.
- `values[].code` carries the code for `first` and `last` over coded
  values, with `value` `null`; a numeric reducer over a coded stream is a
  `422` naming the form.
- `group` is present only with `groupBy`, and on a several-metric read,
  whose every row names its `metric`; its values are record paths
  (`<kind>/<id>`) for `source`, `about` and `metric`, the kind reference for
  `kind`, the code for `code`.
- The latest read returns one row per group whose `start` is the newest
  point's `ts`, whose `end` is its `ts_end` (equal to `start` for an
  instant), and whose `count` is the group's point count.
- `{"timeInState": []}` and the bare word `timeInState` mean every code the
  stream holds, one value per code.
- Coded gauges admit `count`, `first`, `last` and `timeInState`; coded
  amounts admit `sum`, `count`, `min` and `max`, grouped by `code`.
- `record` is present only in the `records` time mode.
- `unavailable: true` marks a row whose range has expired index points and a
  reducer that needs them; its `values` are `null`.
- A request over budget, a reducer illegal for the form, or a unit outside
  the metric's measure is a `422` naming the limit, the form or the measure,
  before any point is reduced.
- The downsample response reuses `AggregateResult` with one row per column
  and the four reducers `first`, `last`, `min`, `max`, each carrying `at`.

## Where the build differs

### From the proposal's data model

The proposal's worked examples ([proposal](../proposal/README.md)) were
written before this build; these points differ, each for a stated reason:

- A lab result's code as received is `coding`, an object, not `code`: the
  trait contracts `code` as a string, the categorical key the index reads
  (`not-detected`).
- A lab result's `specimen` names its material `material`, not `type`, a word
  the vocabulary has retired.
- The metric names its measure with `measures`, not `dimension`, and keeps no
  `delta`, `circular` or `wrapAt`: they live on each value.
- A metric's `reducers` and a lab's `status` are free strings rather than
  enums, because an enum value is one lowercase word and `durationIn` and
  `entered-in-error` are not.
- The engine holds the result-shape rule for any observed kind that declares
  `codedValue`, `ratio` or `text`, since a kind declaration has no
  "exactly one of" key; `code` beside `codedValue` counts as one shape.
- Garmin's Pulse Ox is `health/spo2` with Garmin's code, not a metric of its
  own; Apple's heart rate variability is SDNN, so the catalogue has
  `hrv_sdnn` beside `hrv_rmssd`; Health Connect's skin temperature is a
  deviation, so it is `skin_temperature_deviation`.
- The write rules a [decision](../../../decisions/README.md) did not name are this
  build's choices: a `statistic` or a `period` outside a summary is refused,
  a removed `revision` is refused, a coded trace with a `unit` is refused,
  and a trace needs at least one point.
- The rollup key adds `unit` and `delta` to the stream key, and a rollup row
  keeps its statistics in its own unit, so two units that share no base
  never add. `first_value` and `last_value` are the stored digits
  (`numeric`), so a counter's endpoints stay exact.
- A counter's step from the previous bucket is in the later row's
  `increase`, computed when the row is, not at read time with a window
  function as the plan had it, so the read needs no counter contract from
  the rows. A correction recomputes the next bucket holding a reading too.
  Decision 0157 states this rule and its cost.
- A rollup row gains `start_gap_ms`, where the bucket's first point lies, and
  `bounded`, its count of bounded points, so the read can carry a hold across
  rows and report bounded values without the raw points. It also keeps
  `span_end`, the record id and index of the first and last point, and the
  stored digits of the smallest and largest number (`min_value`,
  `max_value`), so a read served from rows joins amount coverage, breaks ties
  at one instant and answers a `min` or `max` as the raw points do. The
  table's CHECK constraints refuse a row without them, and `increase` is null
  on a row that holds no step.
- `observed_points` carries each point's hold, counter contract, `delta`,
  `circular` and `revision`, and its index `ord` in its record; the plan
  named neither, and the rollups are computed from the points alone.
- `observed_latest` names its hold `hold_ms` and adds `ts_end`, `delta`,
  `statistic`, `bounded` and `points`.

### From this brief's wire contract

The contract above as first written differs from what is built in these
points, each for a stated reason:

- `head.generation` is a string, the history generation a list page carries,
  not a number.
- `sources` lists record paths, not bare ids, as `group.source` does; `order`
  takes either.
- `values[].value` is always present: `null` for no answer and for a coded
  `first` or `last`, whose answer is `code`.
- A value written with a comparator is counted in `bounded` and is in every
  average and sum: the rollup rows do not keep bounded values apart.
- A gauge's value holding into a bucket with no point of its own makes a row
  there, with `count` 0, the way it makes held time.
- A records-mode window is not clipped to the range: the record is the
  bucket.
- A refusal for a form, a code count or a bucket budget comes after the
  statements that select the streams, read the metric's unit, read the
  records mode's records and find the range's forms (the rollup rows' keys,
  the points at the range's cut ends, its summaries, and each gauge's value
  held into it), not before anything runs: which forms and codes a range
  holds is a fact of its points, and the newest point's form is not. The
  points at the cut ends and in the summaries count against `maxSamples`
  before they are read.
- A `maxSamples` refusal for the raw spans an amount's coverage is joined
  from comes once the rollup rows are merged, before the spans are read:
  which rows overlap is known from the rows.
- `exact` is `false` whenever a float64 reduction was involved, so an `avg`
  is never exact.

## The timeline chart's design

The console's timeline ([the console](../../../console.md#observations))
follows Grafana's time series panel: the series of one unit share a y axis,
a second unit gets an axis on the right, one crosshair, an "all series"
tooltip, and legend entries that hide a series. Datadog puts a request on
the right axis the same way and splits a graph into small multiples when one
would hold too much, which is what the timeline does at a third unit rather
than adding a third axis. Observable Plot has no second y axis and
recommends charts side by side, and the fitness apps (Oura on the web,
WHOOP's trends, Garmin Connect's reports) overlay one trend on a second
metric with its own scale: two units is the case they all draw together.
Two scales on one plot line up arbitrarily, so each axis carries its unit,
each metric's row names its side, and its numbers are one click below.

## Rules every work package is held to

- Read `CLAUDE.md` (the agent guide) and `docs/terms.md` before writing. Dead
  words are bugs in code, columns, comments and docs. No em or en dashes
  anywhere.
- The changelog is the truth. Nothing writes `records` or a derived table
  outside the fold; `internal/engine/fold.go` is the one path. Derived rows
  are a function of the row and the declaration alone, never of a clock or
  another record.
- `internal/api` never imports `internal/engine`; a new read is a method on
  `substrate.Dataset`, faked in `internal/api/fake_test.go`.
- A changed declaration ships a changed version (`mise run kinds:check`). A
  new core trait bumps core's package version in `core.yaml`. A new
  `propertytype` base value bumps `propertytype.yaml`.
- A landed migration is never edited; the next number is 0012. Row-level
  security and grants copy `0011_index_reprojections.up.sql`.
- Wire structs live in `internal/substrate`; regenerate the golden with
  `go test ./internal/substrate/ -run TestWireGolden -update`, then mirror
  in `web/console/src/lib/api/types.ts`.
- Tests: run what you changed with `-count=1 -run`; never the full database
  suite from a worktree (one machine-wide run at the end, by the
  orchestrator). `mise run lint` and `mise run fmt:check` stay at zero.
  Declaration changes run `mise run kinds:check`; a decision number runs
  `mise run decisions:check` after `git fetch --prune`.
- Commit titles are conventional commits (`feat(engine): ...`), under 72
  characters, imperative. A package in an isolated worktree commits on its
  own branch and never pushes; the orchestrator merges.
- Write for a tired reader: plain statements, the identifiers verbatim, no
  slogans. Comments carry constraints, not narration.
- Keep the proposal honest: when an implementation detail must differ from
  the proposal, say so in the work package's report and in
  [where the build differs](#where-the-build-differs), never silently.
