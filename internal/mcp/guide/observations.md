# Measurements and units

Record measured values with their metric, time and unit, then read them with reducers that respect their form and calendar.

## Shared kinds and metric identity

The seeded observations package supplies:

- `substrate.reamde.dev/observations/metric`: what can be measured, its units, codes and reading guidance.
- `substrate.reamde.dev/observations/observation`: one measured value.
- `substrate.reamde.dev/observations/trace`: many values of one metric over a fixed period.
- `substrate.reamde.dev/observations/source`: the device, app, service or lab producing values.
- `substrate.reamde.dev/observations/sourcelink`: sources that represent one device reached by different routes.

Metric records come from catalogues and providers; the seeded package alone ships none. Metric ids use the shipping package's prefix, such as `health/heart_rate`, `labs/ldl_cholesterol` or `whoop/strain`. Give an owner's custom metric another prefix. A metric reference includes its kind AND id: `substrate.reamde.dev/observations/metric/health/heart_rate`.

Read the metric record with `query` before writing measurements. Its `unit` and optional `units` are references to permitted unit records; its `form` and `hold` guide writers but must be copied onto values. Its `reducers` guide reads. The form belongs to each measured value, so editing the metric does not change the form of existing measurements.

## Record a value

`substrate.reamde.dev/observations/observation` binds `substrate.reamde.dev/core/observed(single)` and `substrate.reamde.dev/core/temporal(range)`. The observed trait drives validation and indexing; other kinds binding it participate in the same aggregate reads.

Use `describe_kind` before writing. Important input properties are:

- `metric`: a reference to an existing metric; `form`: `gauge`, `amount`, `counter` or `summary`.
- `at`: when measured; `endsAt`: the exclusive end of a span. Amounts and summaries need a span, and every trace needs one.
- `value`: a quantity, `{"value":"72.4","unit":"kg"}`. Exact decimal strings preserve the digits.
- `code`: a categorical reading. A coded gauge uses a code instead of a numeric value; a coded amount uses a quantity plus its category.
- `source`: the producing source; `about`: the thing observed, with absence meaning the repository's owner; `channel`: a source's distinct channel.
- `hold`: how long a gauge stands; `PT0S` makes a point reading. `utcOffset` is minutes east of UTC at observation time.
- `statistic` and `period` describe a summary. A counter can declare `resetRule` (`monotonic`, `signed` or `wrap`); inspect the declaration for reset details.
- `origin` identifies the provider record that delivered the value. `sourceValue`, `sourceUnit`, `absentReason` and `normalizationFailure` preserve readings that could not yield a normal value; do not invent a zero.

After installing `substrate.reamde.dev/health`, this `write` records a weighing for the owner. An observation takes the id the substrate mints, so leave `id` out and give the create an `idempotencyKey`:

```json
{
  "op":"put",
  "kind":"substrate.reamde.dev/observations/observation",
  "idempotencyKey":"weight-2026-11-08",
  "properties":{
    "metric":"substrate.reamde.dev/observations/metric/health/body_mass",
    "form":"gauge",
    "at":"2026-11-08T08:00:00Z",
    "utcOffset":0,
    "value":{"value":"72.4","unit":"kg"},
    "hold":"PT0S",
    "recordingMethod":"manual"
  }
}
```

References can be sent as full path strings and are stored as `{ref: ...}`. On updates, read first and pass `ifVersion`. Do not write the derived title.

A trace binds `substrate.reamde.dev/core/observed(packed)`: it uses `unit`, `values` or `codes`, and either regular spacing `every` or millisecond `offsets` from `at`. Null points mean unavailable and end a gauge's hold. Its `stats` are server-computed. Inspect its declaration rather than copying a single observation into it.

## Units

Units and conversions are records of `substrate.reamde.dev/core/unit` and `substrate.reamde.dev/core/conversion`. Quantities store unit ids, not full unit references. The metric's `unit` property, by contrast, is a reference to a unit record.

Use `units` action `list`, optionally with `q`, `first` and `after`, to discover the repository's units and conversions. Use `convert` for conversion; `value` is a string. `delta` is a boolean on convert, not a separate action:

```json
{"action":"convert","value":"72.4","from":"kg","to":"lb"}
```

```json
{"action":"convert","value":"5","from":"Cel","to":"[degF]","delta":true}
```

A difference converts without the temperature offset. Stored observation `delta: true` similarly marks a difference. Conversion follows the repository's conversion graph; do not assume every pair of named units converts. Inspect declarations before adding custom unit or conversion records.

## Aggregate reads

Pass `aggregate` to `query`. Do not fetch a page of raw records and mistake it for the complete population. A metric aggregate names `metric: {ref: <full metric record path>}`. The half-open `range` uses `gte` and `lt` RFC 3339 instants. A calendar bucket uses `calendar` (`hour`, `day`, `week`, `month`) and an IANA `timezone`; a fixed-width bucket instead uses `every`, an ISO 8601 duration, optionally with `origin`.

This `query` reads daily body-mass averages, extremes and counts in UTC:

```json
{
  "aggregate":{
    "metric":{"ref":"substrate.reamde.dev/observations/metric/health/body_mass"},
    "range":{"gte":"2026-11-01T00:00:00Z","lt":"2026-11-09T00:00:00Z"},
    "bucket":{"calendar":"day","timezone":"UTC"},
    "reducers":["avg","min","max","count"],
    "groupBy":["source"],
    "unit":"kg"
  }
}
```

`groupBy` accepts `source`, `about`, `code`, `metric`, `channel`, `statistic`. The default source policy is separate; it does not silently reconcile measurements from several devices. A priority policy can choose sources using `sources: {policy: "priority", order: [...]}`. Read source records and sourcelinks before deciding that two streams duplicate one another.

`unit` selects the answer unit, within the metric unit's linear conversion component. `durationUnit` selects the unit for duration reducers, seconds by default. `from` is `raw` by default, or `summaries` or `prefer-raw`; do not add nightly summaries to raw readings as if they were independent observations.

Without a timezone, `calendar: "day"` means recorded-local days, using each point's own UTC offset. This is not an implicit UTC calendar. A missing offset needs `fallbackTimezone`; recorded-local date ranges use `range.recordedLocal: true` with whole dates. Use an explicit timezone when that is what the question means.

## Choose reducers by form

- Numeric gauge: `count`, `avg`, `min`, `max`, `first`, `last`, `stddev`, `percentile`, `durationIn`, `circularMean`. Gauge means normally weight time held; point readings with zero hold are different. Never sum levels.
- Coded gauge: `count`, `first`, `last`, `timeInState`.
- Numeric amount: `count`, `sum`, `avg`, `min`, `max`, `first`, `last`, `stddev`, `percentile`.
- Coded amount: `count`, `sum`, `min`, `max`.
- Counter: `count`, `first`, `last`, `min`, `max`, `increase`, `rate`. Do not sum successive meter totals.
- Summary: `count`, `first`, `last`, `sum`, `min`, `max`, `avg`, with further restrictions: only additive statistics such as sum/count can themselves be summed.

Argument-taking reducers are objects, for example `{"percentile":0.9}`, `{"durationIn":[95,114]}` or `{"timeInState":["open"]}`. Form eligibility alone is not sufficient: units, circular values and summary statistics impose additional checks. Follow a refusal rather than substituting an unrelated reducer.

For each group's newest point, use `latest: true` without range, bucket or reducers:

```json
{"aggregate":{"metric":{"ref":"substrate.reamde.dev/observations/metric/health/body_mass"},"latest":true,"groupBy":["source"],"unit":"kg"}}
```

## Downsample for a chart

`downsample` sits beside `aggregate`, not inside it. It needs one metric and a range. `points` is the requested number of columns (1–10000); each column preserves first, last, minimum and maximum. It is not a request for that many arbitrary raw records.

```json
{
  "aggregate":{
    "metric":{"ref":"substrate.reamde.dev/observations/metric/health/heart_rate"},
    "range":{"gte":"2026-11-08T08:00:00Z","lt":"2026-11-08T09:00:00Z"},
    "groupBy":["source"]
  },
  "downsample":{"points":120}
}
```

Do not add `bucket`, `reducers`, `rolling`, `fill` or `latest` to that read. Do not send a downsample `method`; the server uses first/last/min/max. Read `guide` topic `query` for filters and `console` for record views that display measurement reads.
