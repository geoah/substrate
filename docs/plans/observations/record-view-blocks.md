# Record view blocks for observations: stats, trend, table and breakdown

Design for substrate `main` at `b7628e92`. Paths are relative to the checkout. The decision record is [0175](../../decisions/0175-a-record-view-draws-tiles-trends-and-tables-from-one-batched-read-per-block.md), and the mechanism it extends is [0166](../../decisions/0166-a-record-page-layout-is-a-record-in-a-seeded-console-package.md) ([record views](../../console.md#record-views)).

## Summary

Record views (decision 0166) stay the one mechanism. The console package goes to version 2 with four block types this console draws (`stats`, `trend`, `table`, `breakdown`), one declared for the next console (`lanes`), four new fields on `chart` (`from`, `statistic`, `channel`, `bands`) and a looser window (`start` and `end` optional, `open`, `last`, `aboutSelf`, `sourceSelf`). No Go code changes: the engine validates the new grammar as declared data and one Go test gains cases. Every block batches its metrics into one `metrics` aggregate read, so a WHOOP cycle page costs eight aggregate reads, which the engine serves two at a time. Presentation stays data: WHOOP's bands, a room's comfort band, which columns a lab table shows, all live in view records a sample ships or an owner writes; the console names no sample, metric id or sample kind (0166's test keeps holding). Shipped views move with the samples: health to version 4 (sleep and workout additions, lab report, lab result, lab metric views) and healthwhoop to version 8 (cycle and WHOOP sleep views, with the `ADAPTER` stamp the version test ties to it).

## 1. Concepts and what is new

A record view names one kind (`appliesTo`), binds a window by the record's own property names, folds properties out of the generic sheet, and lists sections of one block each. Today a block is one of `heading`, `text`, `properties`, `related`, `chart`, `spans`; a chart reads raw points only, one aggregate read per metric; a kind without `at` and `endsAt` carries no chart, because the console binds a window only from a start and an end instant with the end after the start. On WHOOP data the sleep page says "No heart rate was measured" three times, the cycle page is the generic sheet, and the lab report lists 18 titles with no value, range or flag, the lab's own results and their converted twins mixed together.

Two facts shape every read. A value lands whole in the window or bucket its start lies in: a read over a WHOOP sleep's window answers the whole cycle's energy and strain, because WHOOP stamps every value of a cycle at the sleep onset, so a WHOOP number on a record page is one value over the record's window or a trend across many records, never a line inside the night. And two aggregate reads run at once per process (`aggregateSlots` in `internal/engine/aggregate.go`), each further read waiting for a slot, so a page must count its reads.

What is new, each a widening of the `recordview` grammar:

- **Window.** `start` and `end` become optional. `open: true` reads a record with no end up to now (the current WHOOP cycle, "so far"). `last: P1D` gives a window to a record that has none (a room, a meter): the last day, ending now. `aboutSelf: true` makes the record itself the subject of every read (a room page reads what is about the room); `sourceSelf: true` is declared the same way for a device page and is read by the console but not yet exercised by a shipped view. A view may bind `about` alone, with no start: its `table` blocks then narrow by the record's subject.
- **`chart` gains** `from` (raw, summaries, prefer-raw), `statistic` (the one summary statistic a summaries read keeps), `channel` (the channel kept; the word `none` keeps streams naming no channel) and `bands`. One `metrics` read per block instead of one read per metric; an amount draws bars through `markOf` instead of the hard-coded line.
- **`stats`**: tiles. An item is one reducer of one metric over the window, or one property of the record (a dotted path reaches into a keyed map: `scores.sleepConsistency`). A tile shows the value, the band it falls in, and a delta against a baseline of the `baseline` days before the window (the average, lowest and highest of the daily values, one read per block). `show: dial` draws a bounded score as an arc with its bands (recovery, strain, sleep performance); `show: bar` draws a bullet bar with the baseline range behind the value (HRV against the last 30 nights).
- **`trend`**: strips. An item is one metric bucketed by day, week or month over `span` buckets ending with the bucket that holds the record, that bucket highlighted; `rolling` adds a trailing average as a dashed line. One `metrics` read per block.
- **`table`**: records of one kind as rows with chosen columns: those pointing here (`via`), those sharing a property value with this record (`sameAs`), or those starting inside the window (`inWindow`). For a kind binding `core/observed(single)` the default columns are time, value, flag, reference range and source, and `chart: true` draws the values over time as points with each result's own reference range as a stepped band: the biomarker trend. `nest` folds a row that references another listed row under it (a lab's result and its converted twin, the one whose `derivedFrom` is set), and draws such a row's point hollow.
- **`breakdown`**: bars from the record's own properties, a keyed map (`zoneDurations`) or a list of scalar properties (`awake`, `light`, `deep`, `rem`), each as its share of `total`. Zero reads.
- **`bands`** on `chart`, `stats` and `trend`: labelled value ranges with a tone (`good`, `fair`, `poor`, `none`), scoped to one metric of the block or to all. WHOOP's 0 to 33, 34 to 66 and 67 to 100 live here, in the healthwhoop sample's view, never in console code, which names no sample.
- **`lanes`**: rows of spans on the view's time axis (the sleeps and workouts of a cycle, the held states of a door). Declared in version 2 so the next console draws it without another package bump; this console shows it as the block-not-drawn notice it already shows for an unknown block.

Not new: no console kind, no route, no engine code, no change to how views match (exact kind, id order, all views draw).

## 2. The YAML grammar

Everything below is additive to `kinds/substrate.reamde.dev/console/recordview.yaml`; `kinds/substrate.reamde.dev/console/console.yaml` moves to `version: 2`. Three loader rules bound the shape: a field sits at level 4 at most and a level-4 field is a scalar (`MaxFieldDepth = 4`, `internal/vocabulary/types.go:411`), so a block's list holds scalars only and `bands` sits at block level with a `metric` scope; `title`, `at`, `endsAt` and `dueAt` are reserved for fields too (`reservedProps`, `internal/vocabulary/load.go:1331`), so a lanes row's title property is `spanTitle`; every `values` entry on any datatype must be one lowercase word (`parseEnumValue`, `load.go:1466`), so `from` is a plain string the console checks, as `reducer` is.

### 2.1 The window (replaces the `window` object; `start` and `end` lose `required`)

```yaml
window:
  type: object
  description: which of the record's own properties hold the window the
    blocks read, who it is about and what recorded it; a record with no
    window of its own reads the last `last` of time
  fields:
    start:
      type: string
      description: the property holding when the record starts, such as at
    end:
      type: string
      description: the property holding when the record ends, such as
        endsAt; with open, a record whose end is unset is read up to now
    open:
      type: bool
      default: false
      description: a record whose end property is unset is read up to now,
        and the page says so far
    last:
      type: duration
      description: for a record with no start, the window ends now and is
        this long, such as P1D or P7D
    about:
      type: string
      description: the reference property naming who the record is about;
        a record that names nobody reads the owner's observations
    aboutSelf:
      type: bool
      default: false
      description: the record itself is who the observations are about, as
        a room or a car is
    source:
      type: string
      description: the reference property naming the device or app that
        recorded the record
    sourceSelf:
      type: bool
      default: false
      description: the record itself is the source that recorded them, as a
        device page is
```

Console rules (a notice on the window blocks where broken): `aboutSelf` and `about` are not both set; nor `sourceSelf` and `source`; `last` and `start` are not both set.

### 2.2 Shared field shapes

`bands` appears on `chart`, `stats` and `trend` with one shape. A value is in a band where `low <= value < high`; an absent bound is open.

```yaml
bands:
  type: object
  repeated: true
  description: labelled value ranges in the block's answer unit, drawn
    behind a chart, around a dial and beside a tile; a band naming a metric
    applies to that metric alone
  fields:
    low:
      type: float
      description: the lower bound, inclusive; absent is open
    high:
      type: float
      description: the upper bound, exclusive; absent is open
    label:
      type: string
      description: what the band is called, such as Optimal
    tone:
      type: enum
      default: none
      values:
        - good
        - fair
        - poor
        - none
      description: how the band is coloured and worded
    metric:
      type: reference
      trait: substrate.reamde.dev/core/metric
      description: the one metric of the block the band applies to; absent
        applies to every metric of the block
```

Three scalars recur. `from` is `type: string`, `default: raw`, "where the read takes its values: raw, summaries or prefer-raw" (the console refuses any other word). `statistic` is `type: string`, the one summary statistic a summaries read keeps, such as `vendor`, `avg` or `max`. `channel` is `type: string`, the channel the block's reads keep; the word `none` keeps the streams naming no channel; absent keeps every channel. `sources` keeps its enum (`prefer`, `only`, `priority`; default `prefer` except on `spans` and `lanes`, where it is `only`). `better` is `type: enum`, values `higher`, `lower`, `none`, default `none`: which direction of a change is good; a delta is coloured only where it is set.

### 2.3 `chart` additions

Beside `label`, `metrics`, `reducers`, `bucket`, `unit`, `sources`: `from`, `statistic`, `channel` (as in 2.2) and `bands`. The block reads every metric in one `metrics` read.

### 2.4 `stats`

```yaml
stats:
  type: object
  description: tiles, one per item, each one reducer of one metric over the
    window or one property of the record, with a delta against the days
    before the window
  fields:
    label:
      type: string
      description: the block's heading
    baseline:
      type: int
      min: 0
      max: 400
      default: 0
      description: how many days before the window each metric item is
        compared against, as the average, lowest and highest of its daily
        values; 0 compares against nothing
    channel:
      type: string
      description: the channel the reads keep; none keeps the streams naming
        no channel
    sources:
      type: enum
      default: prefer
      values:
        - prefer
        - only
        - priority
      description: prefer reads the record's own source first, only reads it
        alone, priority reads the sources' priorities
    bands:
      # the shape of 2.2
    items:
      type: object
      repeated: true
      required: true
      description: the tiles in order
      fields:
        metric:
          type: reference
          trait: substrate.reamde.dev/core/metric
          description: the metric record to read
        metricProperty:
          type: string
          description: instead of metric, the record's reference property
            holding the metric to read, such as metric on a lab result
        property:
          type: string
          description: instead of a metric, a property of the record shown
            as the tile's value; a dotted path reads into a keyed map, such
            as scores.sleepConsistency
        reducer:
          type: string
          description: the aggregate read's reducer, such as last, sum, avg,
            increase or timeInState; last where absent
        from:
          type: string
          default: raw
          description: where the item's read takes its values, raw, summaries
            or prefer-raw
        statistic:
          type: string
          description: the one summary statistic kept, such as vendor
        code:
          type: string
          description: the code a coded metric's value is taken for, such as
            open or rem
        rank:
          type: float
          min: 0
          max: 1
          description: a percentile's rank
        low:
          type: float
          description: a durationIn band's lower bound
        high:
          type: float
          description: a durationIn band's upper bound
        unit:
          type: string
          description: the unit the value answers in, such as kcal or h
        label:
          type: string
          description: the tile's heading; the metric's name or the
            property's label where absent
        show:
          type: enum
          default: number
          values:
            - number
            - dial
            - bar
          description: a number with its delta, a dial between min and max
            with the bands as arcs, or a bar with the baseline range behind
            the value
        min:
          type: float
          description: a dial's or bar's lower end; 0 where absent
        max:
          type: float
          description: a dial's or bar's upper end; the largest band bound or
            value where absent
        better:
          type: enum
          default: none
          values:
            - higher
            - lower
            - none
          description: which direction of the delta is good
        precision:
          type: int
          min: 0
          max: 12
          description: decimals shown; the metric's displayPrecision where
            absent
```

Console rules: an item names exactly one of `metric`, `metricProperty`, `property`; `durationIn` needs `low` under `high`; `percentile` needs `rank`; `timeInState` needs `code`; `reducer` must be one the server lists (`ALL_REDUCERS`).

### 2.5 `trend`

```yaml
trend:
  type: object
  description: strips, one per item, of a metric bucketed over the buckets
    ending with the one that holds the record, that bucket highlighted
  fields:
    label:
      type: string
      description: the block's heading
    span:
      type: int
      min: 2
      max: 400
      default: 30
      description: how many buckets the strip covers, the record's last
    bucket:
      type: enum
      default: day
      values:
        - day
        - week
        - month
      description: the calendar bucket, in the reader's zone
    rolling:
      type: int
      min: 0
      max: 400
      default: 0
      description: draw the average over this many buckets, through the
        bucket before each, as a dashed line; 0 draws none
    channel:
      type: string
      description: the channel the read keeps; none keeps the streams naming
        no channel
    sources:
      type: enum
      default: prefer
      values:
        - prefer
        - only
        - priority
      description: as on stats
    bands:
      # the shape of 2.2
    items:
      type: object
      repeated: true
      required: true
      description: the strips in order
      fields:
        metric:
          type: reference
          trait: substrate.reamde.dev/core/metric
          description: the metric record to read
        metricProperty:
          type: string
          description: instead of metric, the record's reference property
            holding the metric to read
        reducer:
          type: string
          description: the reducer per bucket; last where absent
        from:
          type: string
          default: raw
          description: where the item's read takes its values, raw, summaries
            or prefer-raw
        statistic:
          type: string
          description: the one summary statistic kept
        code:
          type: string
          description: the code a coded metric's value is taken for
        unit:
          type: string
          description: the unit the values answer in
        label:
          type: string
          description: the strip's heading; the metric's name where absent
        show:
          type: enum
          default: auto
          values:
            - auto
            - line
            - bars
          description: auto draws by the metric's form and the reducer
        better:
          type: enum
          default: none
          values:
            - higher
            - lower
            - none
          description: which direction of the change against the rolling
            average is good
```

### 2.6 `table`

```yaml
table:
  type: object
  description: records of one kind as rows with chosen columns, and for an
    observed single-value kind their values over time with each result's
    own reference range
  fields:
    label:
      type: string
      description: the block's heading
    kind:
      type: reference
      kind: substrate.reamde.dev/core/kind
      required: true
      description: the kind whose records are listed
    via:
      type: string
      description: the reference property on that kind that points at this
        record, such as report or metric
    sameAs:
      type: string
      description: a property both this record and the listed kind hold;
        lists the records whose value equals this record's, such as metric
        on a lab result
    inWindow:
      type: bool
      default: false
      description: lists the records that start inside the view's window
    columns:
      type: string
      repeated: true
      description: the properties shown as columns, in order; absent shows
        time, value, flag, reference range and source for an observed
        single-value kind, and the title and time otherwise
    orderBy:
      type: string
      description: the list read's order, such as at:asc; at:desc where
        absent
    limit:
      type: int
      min: 1
      max: 100
      default: 25
      description: the most rows shown
    chart:
      type: bool
      default: false
      description: draw the values over time as points, each with its own
        reference range, above the rows; observed single-value kinds only
    nest:
      type: string
      description: a reference property of the listed kind; a row whose
        value names another listed row is shown under it, and its point is
        drawn hollow
    empty:
      type: enum
      default: say
      values:
        - say
        - hide
      description: whether a block with no rows says so or is left out
```

Console rules: exactly one of `via`, `sameAs`, `inWindow`; `inWindow` needs a bound window; `chart` needs the kind to bind `core/observed(single)`.

### 2.7 `lanes` (declared; drawn by the next console)

```yaml
lanes:
  type: object
  description: rows of spans on the view's time axis, each the records of a
    kind inside the window or pointing here, or the held states of a coded
    metric
  fields:
    label:
      type: string
      description: the block's heading
    sources:
      type: enum
      default: only
      values:
        - prefer
        - only
        - priority
      description: as on spans, for the metric rows
    rows:
      type: object
      repeated: true
      required: true
      description: the rows top to bottom
      fields:
        kind:
          type: reference
          kind: substrate.reamde.dev/core/kind
          description: a kind whose records starting inside the window are
            the row's spans, each from its at to its endsAt
        via:
          type: string
          description: with kind, the reference property on it that points
            here; the row is then those records whatever their time
        metric:
          type: reference
          trait: substrate.reamde.dev/core/metric
          description: a coded metric whose values are the row's spans, each
            held to the next value or its hold
        label:
          type: string
          description: the row's name; the kind's or the metric's where
            absent
        spanTitle:
          type: string
          description: a property of the kind whose value names each span,
            such as sport; the record's title where absent
        limit:
          type: int
          min: 1
          max: 500
          default: 100
          description: the most records or values one row reads
```

### 2.8 `breakdown`

```yaml
breakdown:
  type: object
  description: bars from the record's own properties, a keyed map or a list
    of properties, each as its share of a total property; reads nothing
  fields:
    label:
      type: string
      description: the block's heading
    property:
      type: string
      description: a keyed property of the record, such as zoneDurations;
        one bar per key
    keys:
      type: object
      repeated: true
      description: the keys top to bottom with their labels; a key not
        listed goes last
      fields:
        key:
          type: string
          required: true
          description: the key in the map
        label:
          type: string
          description: what the key is called
    items:
      type: object
      repeated: true
      description: instead of property, scalar properties of the record,
        one bar each
      fields:
        property:
          type: string
          required: true
          description: the property's name
        label:
          type: string
          description: the label shown instead of the property's own
    total:
      type: string
      description: the property each bar is a share of, such as timeInBed;
        the bars' sum where absent
```

Console rule: `property` or `items`, not both; a value that is no number, duration or quantity is left out and named.

### 2.9 Blocks and their reads

| Block | Aggregate reads | List reads | Follows the zoom | Needs a window |
| --- | --- | --- | --- | --- |
| `heading`, `text`, `properties` | 0 | 0 | no | no |
| `related` | 0 | 0 or 1 | no | no |
| `chart` | 1 (was 1 per metric) | 0 | yes | yes |
| `spans` | 1 | 1 | yes | yes |
| `stats` | 1, plus 1 for each further way an item reads an already-read metric (another `from`, `statistic` or `unit`), plus the coded items' own reads; twice that with `baseline` | 0 | no | only metric items |
| `trend` | 1 | 0 | no | `start` or `last` |
| `table` | 0 | 1 | no | only `inWindow` |
| `breakdown` | 0 | 0 | no | no |
| `lanes` | 0 (next console) | 1 per row | yes | yes |

## 3. Engine and API changes

None to Go code. The grammar is declared data the engine validates generically. The two YAML edits are `recordview.yaml` (section 2) and `console.yaml` (`version: 2`); `mise run kinds:check` holds both. The boot upgrade lands version 2 in every existing repository at its next open: `TestBootUpgradeSeedsTheConsolePackageIntoAnOlderRepository` covers a repository with no console package, and `TestBootUpgradeMovesAConsole1RepositoryWithLiveViewsToConsole2` a repository at version 1 holding imported, owner-edited and owner-written views (fixed copies of the version 1 files in `internal/engine/testdata/console1/`), which read back unchanged, still write and keep their references.

`internal/engine/recordview_db_test.go` changes: `aRecordView` gains a `stats`, a `trend`, a `table`, a `lanes` and a `breakdown` section; the case "a window with no end" moves from the refusals to an admission; new refusals: a `stats` item whose `metric` is a bare id, a `table` whose `kind` points at a record that is no kind declaration, a `lanes` row whose `metric` is no metric, a `trend` with `span: 1`, a band whose `tone` is outside the list.

Engine behaviour the reads rely on, with evidence: a `metrics` read mixing summary entries with raw ones over a plain range answers one row per entry (on a WHOOP cycle: recovery, strain and HRV from summaries beside energy and the stage sums from raw points); `metrics` with calendar-day buckets and `rolling {through: previous}` answers rolling values per row (five WHOOP vitals against their last 30 days in one read); a metric may appear once per `metrics` read (`internal/engine/aggregate.go:534`), so a block splits repeated metrics into further reads; every entry carries its own `reducers`, `from`, `statistic`, `unit` or `durationUnit` (`forMetric`, `aggregate.go:548-600`), and a top-level `reducers` is not needed where every entry names its own. One behaviour is unverified and used only by the door example: `timeInState` as an entry reducer of a `metrics` read. It is checked against a repository holding door data before a shipped view relies on it; the fallback is a single-metric `metric` read for a block of one item.

The wire golden is untouched: the console sends only `metrics`, `rolling`, `budget` and filter arms `types.ts` already pins.

## 4. Console implementation plan

### 4.1 Types (`web/console/src/lib/record-view.ts`)

```ts
export type Tone = "good" | "fair" | "poor" | "none"
export type Better = "higher" | "lower" | "none"
export type ReadFrom = "raw" | "summaries" | "prefer-raw"
export interface Band { low?: number; high?: number; label?: string; tone: Tone; metric?: string }
/** The status tokens a tone wears: good --ok/--ok-soft, fair --warning/--warn-soft,
 * poor --destructive/--bad-soft, none --faint/--hover. A tone is never the only signal. */
export function toneColor(tone: Tone): string
export function toneSoft(tone: Tone): string

export interface ViewWindow {
  start?: string; end?: string; open: boolean; last?: string
  about?: string; aboutSelf: boolean; source?: string; sourceSelf: boolean
}
export interface Bound {
  window?: TimeRange       // absent: the record holds none the view binds
  open: boolean            // the window ends at the time of reading: "so far"
  about?: string; source?: string; observed: string[]
}

export interface ChartBlock { type: "chart"; label?: string; metrics: string[]; reducers: ChartReducer[]; bucket: ChartBucket; unit?: string; sources: SourcePolicy; from: ReadFrom; statistic?: string; channel?: string; bands: Band[] }

export interface StatItem {
  metric?: string; metricProperty?: string; property?: string
  reducer: AggregateReducer; key: string; from: ReadFrom; statistic?: string; code?: string; unit?: string
  label?: string; show: "number" | "dial" | "bar"; min?: number; max?: number; better: Better; precision?: number
}
export interface StatsBlock { type: "stats"; label?: string; baseline: number; channel?: string; sources: SourcePolicy; bands: Band[]; items: StatItem[] }

export interface TrendItem { metric?: string; metricProperty?: string; reducer: AggregateReducer; key: string; from: ReadFrom; statistic?: string; code?: string; unit?: string; label?: string; show: "auto" | "line" | "bars"; better: Better }
export interface TrendBlock { type: "trend"; label?: string; span: number; bucket: "day" | "week" | "month"; rolling: number; channel?: string; sources: SourcePolicy; bands: Band[]; items: TrendItem[] }

export interface TableBlock { type: "table"; label?: string; kind: string; via?: string; sameAs?: string; inWindow: boolean; columns: string[]; orderBy: string; limit: number; chart: boolean; nest?: string; empty: "say" | "hide" }

export interface LaneRow { kind?: string; via?: string; metric?: string; label?: string; spanTitle?: string; limit: number }
export interface LanesBlock { type: "lanes"; label?: string; sources: SourcePolicy; rows: LaneRow[] }

export interface BreakdownBlock { type: "breakdown"; label?: string; property?: string; keys: { key: string; label?: string }[]; items: { property: string; label?: string }[]; total?: string }

export type Block = HeadingBlock | TextBlock | PropertiesBlock | RelatedBlock | ChartBlock | SpansBlock | StatsBlock | TrendBlock | TableBlock | LanesBlock | BreakdownBlock
export const BLOCK_TYPES = ["heading", "text", "properties", "related", "chart", "spans", "stats", "trend", "table", "lanes", "breakdown"] as const
```

The `lanes` parser admits the block and `BlockView` draws the notice "This console does not draw lanes yet"; nothing else about lanes is built in this change.

### 4.2 The reads, by block (`lib/record-view.ts`)

Every read keeps today's narrowing (`about` eq the bound subject or `exists: false`; `source` eq under `only`; `sources: {policy: priority, order: [own]}` under `prefer`) plus `channel` (`{exists: false}` for `none`, `{eq}` otherwise).

- `boundOf(view, record, kinds, now = Date.now())`: `observed` as today (undefined when no kind binds `core/observed`). `about` is the record's own path under `aboutSelf`, else the reference the `about` property holds; `source` likewise. The window: `last` gives `[now - last, now)` with `open: true`; else `start` is parsed, `end` is the end property where set, `now` where unset and `open`, and the window is absent otherwise; an end at or before the start is no window. A view that binds `about` alone has a Bound with no window.
- `viewNeedsWindow(view)` is `Boolean(window.start || window.last)`; `readsWindow(block)` is true for `chart`, `spans`, `lanes`, `trend`, `table` with `inWindow`, and `stats` with any metric item. A view that needs a window on a record that holds none drops those blocks, as today.
- `chartRead(b, chart, window, tz)`: one `metrics` read: an entry per metric `{ref, reducers, from, statistic?, unit | durationUnit?}` (a time unit beside a `durationIn` is `durationUnit`, as today), shared `range`, `bucket` (none for `whole`), `sources`, the filter with `channel`. Rows split by `group.metric`; the answer's `units[metric]` replaces `unit`.
- `statsReads(b, block, tz, infos)`: `{value: AggregateRead[], baseline: AggregateRead[]}`. Metric items go into reads so that no read names a metric twice (item k of a metric goes to read k); items naming a `code` go into reads of their own with `groupBy: ["code"]`. An entry is `{ref, reducers: [item.reducer], from, statistic?, unit | durationUnit?}` over `range = window`, no bucket. With `baseline > 0`, each value read is repeated over `[dayStart - baseline days, dayStart)` where `dayStart` is the reader's local midnight of the window's start, with `bucket: {calendar: day, timezone}` and the same reducers; the tile takes the average, lowest, highest and count of the daily values client-side.
- `trendRead(b, block, tz, infos, now)`: one `metrics` read, an entry per item; `bucket: {calendar: block.bucket, timezone}`; `rolling: {buckets: block.rolling, reducers: ["avg"], through: "previous"}` where `rolling > 0`. The anchor instant is `window.end - 1` for a `last` window and `window.start` otherwise; for an item whose metric record says `bucketPlace: end` it is `window.end - 1`. The range runs from the start of the bucket `span - 1` buckets before the earliest anchor's bucket to the end of the latest anchor's bucket, buckets computed with local `Date` arithmetic (day, Monday week, month) in `calendarGrid(range, bucket)`, which also gives the strip its full bucket axis so empty buckets still show. `budget.maxBuckets` is set to `span * items` where that passes 1,440.
- `tableParams(b, block, record, listedKind)`: `kinds: [block.kind]`, `first: limit`, `orderBy` (default `at:desc`), and one of `filter.properties[via].eq = <record path>`, `filter.properties[sameAs].eq = <the record's value, a reference's path>`, or `filter.properties.at` `gte`/`lt` the window; plus `about` (eq the bound subject or `exists: false`) where the listed kind declares an `about` property, so a table never lists every subject's records.
- `breakdownItems(block, record, kind)`: numbers from a duration (`durationMs`), a quantity (`value`), an int, decimal or float; text as durations in words, quantities through `readingText`, numbers grouped; shares over `total` or the sum; a value of another shape is left out and named.
- `valueAt(record, path)`: a top-level property, or a dotted path into a keyed map or object; `bandOf(value, bands, metric)`: the first band in order whose bounds hold the value and whose `metric` is absent or the item's; `deltaWords(value, avg, better, unit, precision)`: the change as a signed number and percent with a tone by `better`.
- `viewTimelineSearch` gains every metric a `stats` or `trend` block reads.

Reads a cycle page issues with the view of 5.1: stats block one 2 + 2 (the two heart rate items repeat a metric), stats block two 1 + 1, spans 1 aggregate and 1 list, trend 1, table 1 list. Eight aggregate reads, which `shipped-reads.test.tsx` holds (section 8).

### 4.3 Shared hooks (`components/record-view/parts.tsx`)

- `useMetricReads(paths)`: `useQueries` over `recordQueryOptions` per distinct path, answering `Map<path, {info?: MetricInfo, pending}>` (`metricInfoOf`), replacing the local `useMetricRead` of `observed-blocks.tsx`.
- `useMetricsReads(reads, observed)`: `useQueries` over `aggregateQueryOptions`; when a read is refused with a `422` whose problem starts with `aggregate.metrics[i]`, that entry is dropped and the read reissued without it, the dropped metric reported to the block (one missing metric record never blanks a block). Answers `{answers, pending, errors, dropped}`.
- `Bars` moves here from `observed-blocks.tsx` so `breakdown` and the spans totals share it.

### 4.4 Chart primitives

- `components/observations/timeline-chart.tsx` gains `bands?: {low?: number; high?: number; color: string; label?: string}[]` (washes over the plot between the scaled bounds, open bounds reaching the plot's edge, labels at the right edge, bounds included in the left axis domain), `highlight?: number` (one bucket column tinted `--selection` under the figures), `height?: number` (the plot height, 200 by default, 120 for a strip) and `ChartSeries.dash?: boolean` (a `4 2` dash for the rolling average). `ChartMark` in `lib/overlay.ts` is unchanged.
- `components/record-view/points-chart.tsx` (new): `PointsChart({points, domain, unit, highlight, bands, label})`, a point `{at, value, low?, high?, tone, title, hollow?, kind?, id?}`. Shared `timeAxis`, `linearScale`, `niceDomain`, `tickLabels`; each point's own range as a stepped band from its instant to the next point's; a dot per point in its tone, hollow where `hollow`, the highlighted point ringed; hover by nearest point; click opens the record. Sparse years of lab results do not fit the bucket model of `TimelineChart`.
- `components/record-view/stat-tile.tsx` (new): `StatTile` with `Dial` (an SVG arc from `min` to `max`, band arcs under it in their soft tones, the value as a pointer and a large number, the band's word under it) and `RangeBar` (a bullet: band washes, the baseline's lowest to highest as a grey stretch, the average as a tick, the value as a marker). The tone always sits beside a word.

### 4.5 Block components (`components/record-view/`)

- `stats-block.tsx`: resolves `metricProperty` through the record, reads metric records, runs the value and baseline reads, draws a grid of `StatTile`; a metric item with no row says "Not measured during this <noun>" ("so far" where the window is open); `property` items read the record (`valueAt`) and print through `DeclaredValue` for a top-level property and the sheet's own formatting for a nested value.
- `trend-block.tsx`: one `TimelineChart` strip per item at `height: 120` over `calendarGrid`, `seriesOn` for the item's rows, the mark by `markOf(shapeOf(info), reducer)` under `auto`, the rolling value of each row as a dashed series, `highlight` at the anchor bucket, `bands` filtered by metric, a caption with the record's bucket value and its change against the rolling average.
- `table-block.tsx`: the list read, a plain table with the chosen columns through `DeclaredValue` (the listed kind's declaration from the registry); default observed columns through `readingText`, the exported flag words and `rangeWords`; `nest` rows indented under their referent and marked converted; the page's own record highlighted; "and N more" where a cursor follows; `PointsChart` above the rows under `chart`. A point's tone comes from its `interpretation` codes (`N` good; `H`, `L`, `A`, `HH`, `LL`, `AA`, `HU`, `LU` poor; none otherwise), its band from `referenceRanges[0]` where the bounds are in the value's unit (a range in another unit is listed, not drawn, because some labs print a range in a unit the value cannot convert into).
- `breakdown-block.tsx`: `breakdownItems` into `Bars`, each bar's text the value and its share in percent.
- `record-views.tsx`: `BlockView` gains the cases; the section head says "so far" where the bound is open; a view whose every block is a `table` with `empty: hide` stays hidden until one of them has rows, so the lab view on the metric kind leaves other metric pages unchanged.
- `observed-blocks.tsx`: `ChartBlockView` runs the one batched read through `useMetricsReads` and hands each `MetricChart` its rows; `Line` draws `bands` and takes its mark from `markOf`.

### 4.6 Reuse map

| Need | Reused as is | Changed |
| --- | --- | --- |
| scales, ticks, paths | `lib/chart.ts` | none |
| the time axis | `lib/time-axis.ts` | none |
| buckets and series from rows | `lib/observations-answer.ts` (`bucketsOf`, `seriesOn`, `rowValue`, `numberOf`) | none |
| the line chart | `components/observations/timeline-chart.tsx` | bands, highlight, height, dash |
| marks by form, code colours | `lib/overlay.ts` (`markOf`, `codeColor`) | none |
| bucket sizing and words, durations | `lib/observations-time.ts` | none |
| metric facts, reading text, flags | `lib/observations.ts` | export `rangeWords`, `flagWords` |
| the aggregate query | `lib/api/observations.ts` `aggregateQueryOptions` | none |
| list reads | `lib/api/records.ts` `recordsQueryOptions` | none |
| property rendering | `DeclaredValue`, `sheetRows` | none |
| record links | `components/identity/record-ref.tsx` | none |
| bars | `Bars` | moved to `parts.tsx` |

## 5. Worked example views

Shipped views carry the sample's authority and the import rehomes it (0166); a view on a provider kind survives the import because only `samples.substrate.reamde.dev` is rewritten. Owner-written views (5.7 to 5.9) use the invented repository `ada.example.com`. Every value below is invented.

### 5.1 WHOOP cycle, `samples/healthwhoop/views.yaml`

```yaml
# A WHOOP cycle is the day: one sleep onset to the next. Its page shows the
# recovery, strain and sleep scores WHOOP computed for it, the night's vitals
# against the last 30 nights, the stages, the scores' last 30 days and the
# workouts inside it. The thresholds are WHOOP's published bands, as data
# here and never in the console.
kind: substrate.reamde.dev/console/recordview
metadata:
  id: samples.substrate.reamde.dev/healthwhoop/cycle
data:
  properties:
    appliesTo: substrate.reamde.dev/core/kind/providers.substrate.reamde.dev/whoop/cycle
    name: This cycle
    intro: What WHOOP scored for the day, from one sleep onset to the next.
    window:
      start: at
      end: endsAt
      open: true
    sections:
      - stats:
          label: Recovery, strain and sleep
          baseline: 30
          channel: none
          bands:
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_recovery
              high: 34
              label: Red
              tone: poor
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_recovery
              low: 34
              high: 67
              label: Yellow
              tone: fair
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_recovery
              low: 67
              label: Green
              tone: good
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_strain
              high: 10
              label: Light
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_strain
              low: 10
              high: 14
              label: Moderate
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_strain
              low: 14
              high: 18
              label: High
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_strain
              low: 18
              label: All out
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_sleep_performance
              high: 70
              label: Poor
              tone: poor
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_sleep_performance
              low: 70
              high: 85
              label: Sufficient
              tone: fair
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_sleep_performance
              low: 85
              label: Optimal
              tone: good
          items:
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_recovery
              from: summaries
              statistic: vendor
              label: Recovery
              show: dial
              min: 0
              max: 100
              better: higher
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_strain
              from: summaries
              statistic: vendor
              label: Strain
              show: dial
              min: 0
              max: 21
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_sleep_performance
              from: summaries
              statistic: vendor
              label: Sleep performance
              show: dial
              min: 0
              max: 100
              better: higher
            - metric: samples.substrate.reamde.dev/observations/metric/health/energy_expended
              reducer: sum
              unit: kcal
              label: Calories
            - metric: samples.substrate.reamde.dev/observations/metric/health/heart_rate
              from: summaries
              statistic: avg
              label: Average heart rate
            - metric: samples.substrate.reamde.dev/observations/metric/health/heart_rate
              from: summaries
              statistic: max
              label: Max heart rate
      - stats:
          label: Last night against the last 30 nights
          baseline: 30
          items:
            - metric: samples.substrate.reamde.dev/observations/metric/health/hrv_rmssd
              from: summaries
              statistic: vendor
              label: HRV
              show: bar
              better: higher
            - metric: samples.substrate.reamde.dev/observations/metric/health/resting_heart_rate
              from: summaries
              statistic: vendor
              label: Resting heart rate
              show: bar
              better: lower
            - metric: samples.substrate.reamde.dev/observations/metric/health/respiratory_rate
              from: summaries
              statistic: vendor
              label: Respiratory rate
              show: bar
            - metric: samples.substrate.reamde.dev/observations/metric/health/spo2
              from: summaries
              statistic: vendor
              label: Blood oxygen
              show: bar
            - metric: samples.substrate.reamde.dev/observations/metric/health/skin_temperature
              from: summaries
              statistic: vendor
              label: Skin temperature
              show: bar
      - spans:
          label: Sleep stages
          metric: samples.substrate.reamde.dev/observations/metric/health/sleep_stage
          codes:
            - code: awake
              label: Awake
            - code: rem
              label: REM
            - code: light
              label: Light
            - code: deep
              label: Deep
          totals: true
          sources: priority
      - trend:
          label: The last 30 days
          span: 30
          bucket: day
          rolling: 7
          channel: none
          items:
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_recovery
              from: summaries
              statistic: vendor
              label: Recovery
              show: bars
              better: higher
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_strain
              from: summaries
              statistic: vendor
              label: Strain
              show: bars
      - table:
          label: Workouts in this cycle
          kind: substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/health/workout
          inWindow: true
          columns:
            - sport
            - duration
            - strain
            - averageHeartRate
            - maxHeartRate
          orderBy: at:asc
          empty: hide
```

The cycle mirror names no source and no subject (its properties hold no `source` or `about` reference), so the reads take the owner's observations by source priority; `channel: none` keeps the workouts' heart rate summaries, which name a channel, out of the cycle's; every WHOOP value of the cycle starts at the sleep onset, so the cycle window holds exactly its own scores; `spans` draws totals only for WHOOP, because WHOOP's stage totals overlap by design and give no stage order, so no hypnogram can be drawn from them.

### 5.2 WHOOP sleep, `samples/healthwhoop/views.yaml` (a second view on `health/sleep`)

```yaml
kind: substrate.reamde.dev/console/recordview
metadata:
  id: samples.substrate.reamde.dev/healthwhoop/sleep
data:
  properties:
    appliesTo: substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/health/sleep
    name: WHOOP's scores for this sleep
    window:
      start: at
      end: endsAt
      about: about
      source: source
    sections:
      - stats:
          label: Scores
          baseline: 30
          sources: only
          bands:
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_sleep_performance
              high: 70
              label: Poor
              tone: poor
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_sleep_performance
              low: 70
              high: 85
              label: Sufficient
              tone: fair
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_sleep_performance
              low: 85
              label: Optimal
              tone: good
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_recovery
              high: 34
              label: Red
              tone: poor
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_recovery
              low: 34
              high: 67
              label: Yellow
              tone: fair
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_recovery
              low: 67
              label: Green
              tone: good
          items:
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_sleep_performance
              from: summaries
              statistic: vendor
              label: Sleep performance
              show: dial
              min: 0
              max: 100
              better: higher
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_recovery
              from: summaries
              statistic: vendor
              label: Recovery it scored
              show: dial
              min: 0
              max: 100
              better: higher
            - metric: samples.substrate.reamde.dev/observations/metric/health/hrv_rmssd
              from: summaries
              statistic: vendor
              label: HRV
              show: bar
              better: higher
            - metric: samples.substrate.reamde.dev/observations/metric/health/resting_heart_rate
              from: summaries
              statistic: vendor
              label: Resting heart rate
              show: bar
              better: lower
            - property: scores.sleepConsistency
              label: Sleep consistency
      - trend:
          label: The last 30 nights
          span: 30
          rolling: 7
          sources: only
          items:
            - metric: samples.substrate.reamde.dev/observations/metric/health/whoop_sleep_performance
              from: summaries
              statistic: vendor
              label: Sleep performance
              show: bars
              better: higher
            - metric: samples.substrate.reamde.dev/observations/metric/health/hrv_rmssd
              from: summaries
              statistic: vendor
              label: HRV
              better: higher
```

`statistic: vendor` keeps the cycle's heart rate `avg` and `max` out of the night's window: a read counts every value that starts in its window, and the cycle's values start at the sleep onset. It draws after the health sample's own sleep view (ids sort `health/sleep` before `healthwhoop/sleep`).

### 5.3 Sleep and workout, additions to `samples/health/views.yaml` (health to version 4)

Device-neutral additions from the records' own properties, zero reads. The sleep view's `fold` gains `timeInBed`, `timeAsleep`, `awake`, `light`, `deep`, `rem`, `efficiency`, `disturbances`, `respiratoryRate`, and after its spans block:

```yaml
      - stats:
          label: The night in numbers
          items:
            - property: timeAsleep
              label: Asleep
            - property: timeInBed
              label: In bed
            - property: efficiency
              label: Efficiency
            - property: disturbances
              label: Disturbances
            - property: respiratoryRate
              label: Breathing
      - breakdown:
          label: Stages as a share of time in bed
          items:
            - property: awake
              label: Awake
            - property: light
              label: Light
            - property: deep
              label: Deep
            - property: rem
              label: REM
          total: timeInBed
```

The workout view's `fold` gains `strain`, `averageHeartRate`, `maxHeartRate`, `totalEnergy`, `zoneDurations`, and after its properties block:

```yaml
      - stats:
          label: Effort
          items:
            - property: strain
              label: Strain
            - property: averageHeartRate
              label: Average heart rate
            - property: maxHeartRate
              label: Max heart rate
            - property: totalEnergy
              label: Energy
      - breakdown:
          label: Time in heart rate zones, as the device counted them
          property: zoneDurations
          keys:
            - key: zone0
              label: Zone 0
            - key: zone1
              label: Zone 1
            - key: zone2
              label: Zone 2
            - key: zone3
              label: Zone 3
            - key: zone4
              label: Zone 4
            - key: zone5
              label: Zone 5
          total: duration
```

The existing `durationIn` chart stays for sources that write heart rate samples; a WHOOP workout now shows its zones from `zoneDurations`, a keyed property of the record that no block drew before.

### 5.4 Lab report, `samples/health/views.yaml`

```yaml
kind: substrate.reamde.dev/console/recordview
metadata:
  id: samples.substrate.reamde.dev/health/labreport
data:
  properties:
    appliesTo: substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/health/labreport
    name: This report
    window:
      about: about
    fold:
      - lab
      - accession
      - issuedAt
      - status
      - review
    sections:
      - properties:
          list:
            - property: lab
              label: Lab
            - property: accession
              label: Accession
            - property: issuedAt
              label: Issued
            - property: status
              label: Status
            - property: review
              label: Review
      - table:
          label: Results
          kind: substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/health/labresult
          via: report
          columns:
            - metric
            - value
            - interpretation
            - referenceRanges
          orderBy: at:asc
          limit: 100
          nest: derivedFrom
```

No start or end: the reports carry no window. A converted twin (`derivedFrom` set) sits under the lab's own row instead of beside it as a row of its own.

### 5.5 Lab result, `samples/health/views.yaml`

```yaml
kind: substrate.reamde.dev/console/recordview
metadata:
  id: samples.substrate.reamde.dev/health/labresult
data:
  properties:
    appliesTo: substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/health/labresult
    name: This analyte over time
    intro: Every result of the same analyte, each against the range its own lab printed.
    window:
      about: about
    sections:
      - table:
          kind: substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/health/labresult
          sameAs: metric
          chart: true
          limit: 50
          nest: derivedFrom
          columns:
            - at
            - value
            - interpretation
            - referenceRanges
            - source
```

The page's own result is ringed in the chart and highlighted in the rows; a converted twin is a hollow point. The trend keeps twins, because on some dates a lab printed the result only in another unit and the converted twin is the one value in the metric's unit.

### 5.6 Lab metric, `samples/health/views.yaml` (on the observations sample's metric kind)

```yaml
kind: substrate.reamde.dev/console/recordview
metadata:
  id: samples.substrate.reamde.dev/health/metric
data:
  properties:
    appliesTo: substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/observations/metric
    name: Lab results
    sections:
      - table:
          kind: substrate.reamde.dev/core/kind/samples.substrate.reamde.dev/health/labresult
          via: metric
          chart: true
          limit: 100
          nest: derivedFrom
          empty: hide
```

On `health/ldl_cholesterol` this is the years-long trend with each result's own band; on `health/heart_rate` the block has no rows and the view stays hidden, so every other metric page is unchanged. The generic metric page keeps its Rollups section.

### 5.7 Room page (an owner's kind, `ada.example.com/home/room`)

The owner declares `home/room` (`name`) and writes observations about each room from a `synth/sensors` source with the observations sample's shipped metrics.

```yaml
kind: substrate.reamde.dev/console/recordview
metadata:
  id: home/room
data:
  properties:
    appliesTo: substrate.reamde.dev/core/kind/ada.example.com/home/room
    name: This room
    window:
      last: P1D
      aboutSelf: true
    sections:
      - stats:
          label: Right now, against the last 30 days
          baseline: 30
          sources: priority
          bands:
            - metric: ada.example.com/observations/metric/air_temperature
              high: 18
              label: Cold
              tone: fair
            - metric: ada.example.com/observations/metric/air_temperature
              low: 18
              high: 24
              label: Comfortable
              tone: good
            - metric: ada.example.com/observations/metric/air_temperature
              low: 24
              label: Warm
              tone: fair
            - metric: ada.example.com/observations/metric/relative_humidity
              high: 40
              label: Dry
              tone: fair
            - metric: ada.example.com/observations/metric/relative_humidity
              low: 40
              high: 60
              label: Comfortable
              tone: good
            - metric: ada.example.com/observations/metric/relative_humidity
              low: 60
              label: Damp
              tone: poor
          items:
            - metric: ada.example.com/observations/metric/air_temperature
              reducer: last
              label: Temperature
              show: bar
            - metric: ada.example.com/observations/metric/relative_humidity
              reducer: last
              label: Humidity
              show: bar
            - metric: ada.example.com/observations/metric/illuminance
              reducer: last
              label: Light
      - chart:
          label: The last day
          metrics:
            - ada.example.com/observations/metric/air_temperature
            - ada.example.com/observations/metric/relative_humidity
          reducers:
            - reducer: avg
              label: Average
            - reducer: min
              label: Lowest
            - reducer: max
              label: Highest
          bucket: auto
          sources: priority
          bands:
            - metric: ada.example.com/observations/metric/air_temperature
              low: 18
              high: 24
              label: Comfortable
              tone: good
      - trend:
          label: The last two weeks
          span: 14
          bucket: day
          sources: priority
          items:
            - metric: ada.example.com/observations/metric/air_temperature
              reducer: avg
              label: Daily temperature
            - metric: ada.example.com/observations/metric/relative_humidity
              reducer: avg
              label: Daily humidity
```

### 5.8 Energy meter page (`ada.example.com/home/meter`, the shipped `energy` counter and `power` gauge)

```yaml
kind: substrate.reamde.dev/console/recordview
metadata:
  id: home/meter
data:
  properties:
    appliesTo: substrate.reamde.dev/core/kind/ada.example.com/home/meter
    name: This meter
    window:
      last: P1D
      aboutSelf: true
    sections:
      - stats:
          label: The last day
          baseline: 30
          sources: priority
          items:
            - metric: ada.example.com/observations/metric/energy
              reducer: increase
              unit: kW.h
              label: Energy used
              better: lower
            - metric: ada.example.com/observations/metric/power
              reducer: avg
              unit: W
              label: Average draw
              better: lower
            - metric: ada.example.com/observations/metric/power
              reducer: max
              unit: W
              label: Peak draw
      - trend:
          label: Daily energy, the last 30 days
          span: 30
          bucket: day
          rolling: 7
          sources: priority
          items:
            - metric: ada.example.com/observations/metric/energy
              reducer: increase
              unit: kW.h
              label: Energy per day
              show: bars
              better: lower
      - chart:
          label: Power over the last day
          metrics:
            - ada.example.com/observations/metric/power
          reducers:
            - reducer: avg
              label: Average
            - reducer: max
              label: Peak
          bucket: auto
          sources: priority
```

`increase` is reset-proof on a counter; the baseline (the average daily `increase` of the previous 30 days) is the "8% above usual" chip. The two power items read one metric the same way (raw, in `W`), so they share one entry, each adding its reducer: the stats block costs one value read and one baseline read.

### 5.9 Door page (`ada.example.com/home/door`, an owner-declared coded gauge metric `home/door_state`, codes `open` and `closed`)

```yaml
kind: substrate.reamde.dev/console/recordview
metadata:
  id: home/door
data:
  properties:
    appliesTo: substrate.reamde.dev/core/kind/ada.example.com/home/door
    name: This door
    window:
      last: P7D
      aboutSelf: true
    sections:
      - lanes:
          label: Open and closed, the last week
          sources: priority
          rows:
            - metric: ada.example.com/observations/metric/home/door_state
              label: State
      - stats:
          label: Time open
          baseline: 30
          sources: priority
          items:
            - metric: ada.example.com/observations/metric/home/door_state
              reducer: timeInState
              code: open
              unit: h
              label: Open this week
              better: lower
      - trend:
          label: Hours open per day
          span: 30
          bucket: day
          sources: priority
          items:
            - metric: ada.example.com/observations/metric/home/door_state
              reducer: timeInState
              code: open
              unit: h
              label: Open
              show: bars
```

The engine admits the view; its `lanes` block draws as a notice until a console draws lanes, and its two `timeInState` blocks depend on the entry-reducer check of section 3. A thermostat mode or a car's state (`parked`, `driving`, `charging`) is the same view with another metric.

## 6. Built-in versus data

Built-in (console code, naming nothing of a sample): the block renderers and their rules; the chart primitives; the four tones and the status tokens they wear (`--ok`, `--warning`, `--destructive`, `--faint`), always beside a word; what a metric record says through the core `metric` trait (`name`, `unit`, `form`, `codeSet`, `displayPrecision`, `bucketPlace`); what an observed record says through core `observed` and core `result` (`at`, `endsAt`, `value`, `code`, `source`, `about`, `channel`, `statistic`, `interpretation`, `referenceRanges`, `text`, `absentReason`); the HL7 v3 interpretation codes to tones; the baseline arithmetic and the delta words; number, unit, duration and date formatting in the reader's zone.

Data (records): every view: which kind, which window, which blocks, which metrics, reducers, bands with labels and tones, columns, properties, nest property. WHOOP's thresholds, the comfortable room band, the zone names. A lab's reference ranges and flags, per result. The metric catalogue and the subject and source records the reads narrow by.

A built-in page is allowed here: the console already draws built-in sections keyed on seeded traits, the Measurement section for `core/observed` and the Rollups section for `core/metric`. This design still chooses data: the lab metric page is a shipped view on the metric kind, because only the health sample knows which kind holds lab results. A per-metric presentation record (direction, bands, headline reducer, mark: the `metricview` idea) is the next step, and the block-level `bands` of this change become its per-view override.

## 7. What this change implements and what it defers

This change implements:

- the grammar of section 2 at console package version 2, with the boot upgrade carrying version 1 views across unchanged;
- the window's `open`, `last` and `aboutSelf`, and `sourceSelf` as far as the console reads it;
- the chart block as one batched read with `from`, `statistic`, `channel` and `bands`;
- `stats` with number, dial and bar tiles, the baseline, and property items with dotted paths;
- `trend` with lines, bars, the highlighted bucket and the rolling average;
- `table` with chosen columns, `nest`, and the points chart with each result's own reference range;
- `breakdown`;
- `lanes` admitted by the engine and shown as a notice;
- the shipped views of 5.1 to 5.6, in health 4 and healthwhoop 8;
- the range bar beside the value of the built-in Measurement section, drawn from the record's own `referenceRanges` and `interpretation`;
- the tests of section 8.

The owner views of 5.7 to 5.9 are examples of views an owner writes; no sample ships them. `timeInState` as an entry reducer of a `metrics` read stays unverified (section 3); only the door example uses it.

Where the change lives:

| Area | Files | Contents |
| --- | --- | --- |
| Grammar and reads | `kinds/substrate.reamde.dev/console/recordview.yaml`, `console.yaml`, `internal/engine/recordview_db_test.go`, `web/console/src/lib/record-view.ts`, `record-view.test.ts`, `components/record-view/parts.tsx`, `observed-blocks.tsx` | the grammar at version 2; the types of 4.1 and the parsers with their notices; `boundOf` with `open`, `last`, `aboutSelf`, `sourceSelf`; `chartRead` as one read; `statsReads`, `trendRead`, `calendarGrid`, `tableParams`, `breakdownItems`, `valueAt`, `bandOf`, `deltaWords`, `toneColor`; `useMetricReads`, `useMetricsReads` and `Bars` in `parts.tsx`; the chart block batched with bands and `markOf`; the engine test cases |
| Chart primitives | `components/observations/timeline-chart.tsx`, new `components/record-view/points-chart.tsx` and `stat-tile.tsx`, `components/observations/observed-section.tsx`, and their tests | bands, highlight, height and dash on `TimelineChart`; `PointsChart`; `StatTile` with `Dial` and `RangeBar`; the range bar of the Measurement section |
| Block components | new `components/record-view/stats-block.tsx`, `trend-block.tsx`, `table-block.tsx`, `breakdown-block.tsx`, one test each; `record-views.tsx`, `record-views.test.tsx`; `lib/observations.ts` (exports only) | the four block views wired into `BlockView`; the lanes notice; "so far"; the hidden-until-rows rule |
| Shipped views and docs | `samples/health/views.yaml`, `samples/health/bundle.yaml`, new `samples/healthwhoop/views.yaml`, `samples/healthwhoop/bundle.yaml`, `internal/providertest/health_test.go`, `internal/catalog/recordview_db_test.go`, `web/console/src/lib/record-view.shipped.test.ts`, `components/record-view/shipped-reads.test.tsx`, `docs/console.md`, `docs/builtin-kinds.md`, `docs/observations.md`, `docs/bundles-catalog.md`, decision 0175 | the views of section 5; health 4 and healthwhoop 8 with the `ADAPTER` stamp and test constant; the shipped test's window rule relaxed; the catalog test's view count; the read counts; docs with the upgrade paragraph corrected (a view written through the API, the console or substratectl keeps the owner's copy and stops taking imports) |

This change defers:

- drawing `lanes`;
- `trend` range marks;
- views that use `sourceSelf`;
- baselines for property items;
- a per-metric presentation record (section 6);
- calendar period pages with previous and next;
- previous-period overlays;
- records-mode reads;
- Mneme web rendering of views: Mneme web reads no `recordview` records, so this feature shows in the substrate console of Mneme's substrate until Mneme web adopts it.

## 8. Test plan

Unit, `lib/record-view.test.ts`: every new block parses; a section with two blocks, an item naming two of `metric`, `metricProperty`, `property`, a `table` with `via` and `sameAs`, a `breakdown` with `property` and `items`, a `timeInState` item without `code`, a `trend` with `span` 1, a `from` outside the three words: each a problem naming the rule. `boundOf`: `open` on a record with no end reads to `now`; `last` on a record with no start; `aboutSelf`; `sourceSelf`; a view with `about` alone has a Bound with no window. `chartRead` is one `metrics` read with per-entry `from`, `statistic`, `unit` or `durationUnit`, and `channel: {exists: false}` for `none`. `statsReads`: three items of three metrics give one value read and one baseline read over the 30 local days before the window's day; two items of one metric give two value reads; a `code` item gets its own read with `groupBy: ["code"]`; `baseline: 0` gives no baseline read. `trendRead`: the range covers `span` buckets ending with the anchor's; `rolling` through previous; the anchor moves to `end - 1` for a `last` window and for a metric whose `bucketPlace` is `end`; `calendarGrid` yields `span` buckets across a DST change (the suite runs in Europe/Athens). `tableParams`: `via`, `sameAs` with a reference value as its path, `inWindow`, `about` narrowing only where the listed kind declares it. `breakdownItems` over a keyed map of durations and over quantity items with shares. `bandOf` with open bounds and metric scoping; `deltaWords` with `better`.

Chart text tests: `timeline-chart.test.tsx` adds a band rect at the scaled bounds with its label, a highlighted column, a dashed path, a 120 px plot; `points-chart.test.tsx` draws a stepped band per point, a hollow point, and rings the highlighted point; `stat-tile.test.tsx` the dial's arc path and band arcs, the bar's marker, the band word and the delta words; `observed-section.test.tsx` draws the range bar from the record's own range.

Component tests with stubbed fetch, one file per block, after `record-views.test.tsx`'s pattern: the drawn path, the empty path ("Not measured during this cycle", "None."), `empty: hide` leaving the block out and the view hidden, `nest` indenting the twin, the page's own record highlighted, the baseline chip wording with `better`, the dropped-metric notice, the lanes notice.

Shipped views: `record-view.shipped.test.ts` holds every view in `samples/*/views.yaml` to the console rules, every metric reference to a shipped metric path, every `table.kind` to a shipped or provider kind, and requires a window only where a block reads one.

Guards that must stay green: `console-names-no-sample.test.ts` (the new code names only core words), `wire.golden.test.ts`, `pages/record.test.tsx`.

Engine: `go test -count=1 -run 'TestRecordView|TestBootUpgradeSeedsTheConsolePackage|TestBootUpgradeMovesAConsole1Repository' ./internal/engine/` with the cases of section 3; `go test -count=1 -run 'RecordView|TestHealthAdaptersStampTheirPackageVersion' ./internal/catalog/ ./internal/providertest/`.

Repo checks before the PR: `mise run kinds:check` (console 1 to 2, health 3 to 4, healthwhoop 7 to 8), `mise run ci:console`, `mise run ci:console:test`, `mise run lint`, `mise run fmt:check`, `mise run decisions:check`; `mise run console:render` unchanged and passing.

Read counts: `components/record-view/shipped-reads.test.tsx` mounts every shipped view on a record with a window, answers every read empty, and holds each view to its count of distinct aggregate reads (the cycle page eight, the WHOOP sleep view three, the health sleep two, the workout three, the lab views none) and every view to eight at most.

In a browser: every shipped view and the views of 5.7 to 5.9 drawn over the invented `ada.example.com` repository in a browser set to Europe/Athens (the console takes the reader's zone from the browser), each page with no console error and the cycle page's network log showing the same eight aggregate reads. This check asserts nothing; the read counts are held by the test above.

## 9. Risks

- **Nesting depth.** A block's list holds scalars only; `bands` sits at block level with a `metric` scope. A later grammar change that adds a nested list hits the loader, not a test.
- **`timeInState` in a `metrics` read is unverified**; only the door example depends on it, and a block of one item can fall back to a `metric` read.
- **A read over a window counts values that start in it, whole**. The shipped views name the night's metrics with `statistic: vendor`; an owner's view can get this wrong and the block cannot tell.
- **Two aggregate slots.** A page of many blocks queues its reads; the shipped views keep long ranges on rollup-served reducers or on nightly summaries, which are few.
- **The baseline's daily buckets against an odd window.** A night's `last` against the previous 30 nightly `last`s is sound; a 3-hour window's `sum` against daily sums is not. The tile prints "against the average day of the previous 30".
- **Lab ranges without a tone** (a lab's `referenceRanges` hold `low`, `high` and free `text`, with no machine-readable type): the chart draws a range neutral and takes the tone from the flag; a result with no flag is a neutral point; no free text is parsed. Ranges in another unit are listed, not drawn.
- **Owner-edited views do not take the upgrade** (0166). `docs/console.md` is corrected to say so: a view written through the API, the console or substratectl stops taking imports; Mneme's production applies its forks with `mnemectl apply`, which is the owner's write.
- **Older engines refuse the new blocks.** Deployment order: binary first, views after; Mneme's forks after the production roll.
- **`default` on a plain string field.** If the loader refuses `default: raw` on `from`, the default moves to the description and the console keeps defaulting.
- **Personal data.** The owner's lab values never enter the repository, a fixture or a screenshot in the PR; screenshots show the invented `ada.example.com` repository only.