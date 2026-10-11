# Observations: the implementation brief

Status: in progress, started 2026-10-11. This page is what every agent
building the observations work reads first. It fixes the decisions the plan
left open, lists the work packages and their order, pins the wire contract of
the aggregate read so the engine and the console agree before either exists,
and names the rules every change is held to. The spec itself is the proposal
under [`../proposal/`](../proposal/README.md) (the current end-to-end
proposal, with worked examples) and [`../../observations.md`](../../observations.md)
(the plan, whose section 13 supersedes its earlier sections where they
differ). Where this page and the proposal disagree, this page wins, because
it records the decisions taken to build.

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
| the arbitrary-unit prefix rule | kept, as a labelled substrate extension: the same arbitrary atom with the same annotation may be rescaled by a metric prefix (`m[IU]/L` to `[IU]/L`) |
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

Field names below are the contract. Work package 4 implements them in Go
(`internal/substrate`, struct names `Aggregate`, `AggregateBucket`,
`AggregateSources`, `AggregateRolling`, `AggregateBudget`, `Downsample`,
`AggregateResult`, `AggregateRow`, `AggregateValue`, `AggregateGroup`) and
work package 6 mirrors them in `types.ts`. An implementer may add a field,
never rename one. Everything optional is `omitempty`.

Request, the `aggregate` query parameter on `GET /api/v1/records`, strict
JSON, refused beside `q`, `watch`, `count`, `orderBy`, `first`, `after`,
`offset` and `expand`:

```json
{
  "metric": {"ref": "ada.example.com/observations/metric/health/heart_rate"},
  "property": {"name": "amount", "anchor": "at"},
  "from": "raw",
  "minCoverage": 0.9,
  "range": {"gte": "2026-09-01T00:00:00+03:00", "lt": "2026-10-01T00:00:00+03:00"},
  "bucket": {"every": "PT5M", "origin": "2026-09-01T00:00:00Z"},
  "groupBy": ["source", "about", "code", "metric", "kind"],
  "reducers": ["count", "sum", "avg", "twa", "min", "max", "first", "last", "stddev",
               {"percentile": 0.05}, {"durationIn": [70, 180]}, {"timeInState": ["open"]},
               "increase", "rate", "circularMean"],
  "sources": {"policy": "priority", "order": ["garmin-forerunner-265", "apple-watch-s10"]},
  "unit": "/min",
  "maxGap": "PT3H",
  "fill": "null",
  "rolling": {"buckets": 7, "reducers": ["avg"]},
  "budget": {"maxBuckets": 1440, "maxSamples": 20000000}
}
```

- Exactly one of `metric` and `property`. `property.anchor` names the
  datetime property that places a record in time (`at` by default).
- `bucket` is one of `{"every", "origin"?}`, `{"calendar", "timezone"?,
  "weekStart"?, "offset"?}` (`calendar` in `minute`, `hour`, `day`, `week`,
  `month`, `year`; no `timezone` means the recorded-local day), or
  `{"records": {"filter", "around"?: {"before", "after"}, "maxRecords"?}}`.
  No `bucket` with `groupBy` and the one reducer `last` is the latest read,
  which needs no `range`.
- `from` is `raw` (default), `summaries` or `prefer-raw`.
- `sources.policy` is `separate` (default), `priority` or `summaries`.
- `fill` is `null`, `zero` or `locf`.
- `downsample` is its own query parameter: `{"points": 800, "method": "m4"}`;
  it is refused beside `reducers`, `rolling` and `fill`.

Response:

```json
{
  "metric": "ada.example.com/observations/metric/health/heart_rate",
  "unit": "/min",
  "bucket": {"calendar": "week", "weekStart": "monday", "timezone": "Europe/Athens"},
  "from": "raw",
  "exact": true,
  "rows": [
    {"start": "2026-04-06T00:00:00+03:00", "end": "2026-04-13T00:00:00+03:00",
     "group": {"source": "garmin-forerunner-265"},
     "record": {"ref": "ada.example.com/health/workout/garmin/x/123", "version": 3},
     "from": "raw", "sources": ["garmin-forerunner-265"], "coverage": "P6DT22H10M",
     "values": [{"reducer": {"durationIn": [95, 114]}, "value": "612.4", "unit": "min"},
                {"reducer": "avg", "value": "131.6", "unit": "/min"}],
     "count": 24810, "bounded": 0, "synthetic": false, "unavailable": false}
  ],
  "sources": ["garmin-forerunner-265", "apple-watch-s10"],
  "head": {"changelog": 2171234, "generation": 7}
}
```

- `values[].value` is a decimal string, or `null` when the reducer has no
  answer for the row; `values[].unit` is absent for `count`.
- `group` is present only with `groupBy`; its keys are the `groupBy` words,
  its values record ids for `source`, `about` and `metric`, the kind
  reference for `kind`, the code for `code`.
- `record` is present only in the `records` time mode.
- `unavailable: true` marks a row whose range has expired index points and a
  reducer that needs them; its `values` are `null`.
- A request over budget, a reducer illegal for the form, or a unit outside
  the metric's quantity kind is a `422` naming the limit, the form or the
  kind, before anything runs.
- The downsample response reuses `AggregateResult` with one row per column
  and the four reducers `first`, `last`, `min`, `max`.

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
  the proposal, say so in the work package's report and in the docs page
  that describes the feature, never silently.
