# Review and decisions

All fourteen findings of the Codex review (gpt-6-astra, 2026-10-10) change the proposal in whole or in part; one sub-rule is declined and one point stays open. Fourteen choices remain the owner's.

## The Codex review, finding by finding

The plan file `docs/plans/observations.md` still carries the pre-review text; this document is the current proposal.

| Finding | Verdict | What this version does |
| --- | --- | --- |
| 1. The engine claims to read nothing from `metric`, yet `wrapAt`, `circular` and the temperature-difference flag live there | adopt | `delta`, `circular` and `wrapAt` move onto the value and the trace, beside `form`, `unit`, `hold` and the reset rule. The metric keeps names, codes, display and recommended reducers only. |
| 2. A rollup cannot answer every `maxGap`, threshold or priority request | adopt | An eligibility rule: a rollup serves a request only when its stored statistics answer it exactly; otherwise the read scans raw points within budget, or says `unavailable` for a range whose points are expired. A `max_gap_ms` column makes it decidable per bucket. |
| 3. Filtering `closed` out before building intervals overcounts door-open time | adopt | New evaluation order: select streams, build held intervals from every transition, then filter intervals by code or threshold. A small executable specification precedes the SQL. |
| 4. Copying the previous period's last value into the next trace fakes a measurement | adopt | No carry-in copies. The read fetches the point current at the window start with one indexed lookup per stream. A hold expires at `maxGap` inside a gap even when a later point arrives. |
| 5. One `resetRule` cannot describe signed totals, several resets, or wraps | adopt, one part declined | Three counter contracts: `monotonic`, `signed` and `wrap`. A trace splits at every reset epoch. Home Assistant's 10 percent drop tolerance is declined as an engine rule: a noisy total declares `signed` or is smoothed in the adapter. |
| 6. Unavailable, not measured and invalid are different and none of them is zero | adopt | `absentReason` on an observation (FHIR's list). In a trace a `null` value is an "unavailable from here" marker: it ends the hold and counts in nothing. `quality` gains `unavailable`. |
| 7. (metric, source, about) merges two channels of one device | adopt in part | The stream key gains `channel`. No stream record yet; it stays an open decision. |
| 8. Choosing between sources is not the same as knowing two rows are one measurement | adopt | A row carries `origin` (the mirror row that delivered it); the source names the producing device. Deduplication only on an exact upstream identity. `prefer-raw` takes `minCoverage` (default 0.9) below which the summary wins. |
| 9. Expiring the index and deleting raw data are different operations | adopt | Phase 2 keeps every raw record. Expiring `observed_points` rows is an index policy and the response says `unavailable` for those ranges. Permanent reduction waits until the supported historical queries are listed. |
| 10. `V.A` sits under `power`; compounds get their kind from the vector again | adopt | `V.A` gets its own `apparent-power` kind in `coretable.py`. A compound unit whose vector matches more than one kind is refused unless the property type or the metric declares the kind. |
| 11. Counters differenced in `float8` lose increments | adopt | Counter endpoints and `increase` stay exact (`numeric`); `float8` only for gauge and amount analytics. Mean pace is total duration over total distance. `circularMean` is undefined below a resultant-length threshold. |
| 12. A trait that must be complete on day one cannot be | adopt the framing | The first contract stays complete, and a later need lands as a second trait a kind binds beside `observed`, never as a property added to it. A sample kind move is expensive, not impossible, and the plan says so. |
| 13. Multi-origin traces and `observed_latest` corrections are underspecified | adopt | One trace per (metric, source, about, channel, period) has one origin provider; two providers delivering one device are two sources. `observed_latest` is recomputed for the old and the new stream when a correction changes a key or moves the newest point; ties break by revision, then record id. The adapter version is stamped separately from the upstream revision. |
| 14. Benchmark the whole write and rebuild path first | adopt as the phase 2 gate | Traces, revisions, points, rollups, WAL, corrections and rebuild are measured together before the rollup layout is frozen. Plain Postgres first. |

The three contradictions it found, fixed: coded `amount` values are allowed (a sleep stage is one); counters are in `observed_rollups` with `increase` and `resets`; `observed_latest` gains `form`, `unit`, `hold` and `utc_offset_min` so a latest value reads without opening its record.

What the review did not change: no persistent stream record, no fifth `state` form, substrate as the archive and analytics store for a home rather than its live bus.

## Decisions still open

Fourteen choices change the shape of what gets built. Set the status as you decide; the lean is a recommendation, not a decision.

| Decision | Options | Lean | Status |
| --- | --- | --- | --- |
| the declaration key for a quantity's kind | `dimension:` with the documented meaning "a named kind", or `measures:` | `measures:`, so nobody reads UCUM's meaning into it | Open |
| the name of the thing observed | `about` or `subject` | `about`; `subject: true` is already the mapping's word | Open |
| the form names | `gauge`, `amount` or keep `instant`, `interval` | `gauge`, `amount` | Open |
| the second domain | `home` through Home Assistant, or `energy` alone | `home`; it exercises four seams health never touches | Open |
| the home adapter's feed | Home Assistant's raw state history, its statistics, or statistics as backfill and states live | statistics as backfill, states as the live feed | Open |
| a persistent stream record | a `stream` kind for per-stream names and defaults, or none | none in v1; `observed_latest` is the stream list | Open |
| money as an observed value | a balance `about` an account, or typed kinds through the records path | typed kinds; money is never a unit | Open |
| the arbitrary-unit prefix rule | keep `m[IU]/L` to `[IU]/L` as a labelled substrate extension, or refuse as UCUM does | keep, labelled; labs send both | Open |
| `dBm` | a local code, or `dB[W]` with an offset | a local code | Open |
| retention tiers | a per-repository setting in phase 2, or wait for a year of home data | wait; phase 2 keeps every point | Open |
| the word for a vendor-method summary | `statistic: vendor` or `score` | `vendor` | Open |
| the `prefer-raw` coverage floor | `minCoverage` default 0.9, or always raw when any raw exists | 0.9, per the review's eighth finding | Open |
| how the trait grows | a second trait bound beside `observed`, or versioned variants of one trait | a second trait; a variant list is a property list by another name | Open |
| a `null` inside a trace | unavailable from here, ending the hold; or a gap the hold bridges | unavailable; a later heartbeat must not turn an outage into coverage | Open |

## What happens next

1. Fold the verdicts above into `docs/plans/observations.md`, so the plan file and this document agree.
2. Write the decision records the plan lists: nine from version 2, plus the review's counter contracts, retention stance and rollup eligibility rule.
3. Phase 1: the `quantity` type with the unit table, the `observed` trait, the `observations` and reduced `health` samples, the shared Python module, and the `healthwhoop` adapter proving the contract on thirty WHOOP rows: a monthly sleep view, an identical re-sync changing nothing, a rescored and a deleted WHOOP record converging, two cycles on one date staying distinct, rebuild and export reproducing the data.
4. Phase 2: traces, the three tables, the aggregate mode, and the benchmark gate before the rollup layout is frozen.
5. Phase 3: `home` with Home Assistant and the four home requests; then the rest of health, then positions, weather and vehicles, each with a consumer.
