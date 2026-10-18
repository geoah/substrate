# Observations in substrate: the end-to-end proposal

Oct 10, 2026 · @George Antoniadis

Substrate gets five engine pieces and two sample packages. Every observation stays an ordinary record in the changelog; the only new storage is three derived tables a rebuild recreates, and the only new API is one more mode of the records read. This tab is the short version. The tabs beside it drill down: how data flows, each source worked end to end, the API, the engine internals, and what the review and the owner still decide.

&#91;embedded content: write path, truth, fold, read · what exists and what is added\]

A WHOOP recovery reaches the index in two writes: the provider sync writes the mirror row it writes today, and a small adapter function writes the observations it implies. Both are changelog entries; the fold does the rest.

## The five pieces

| Piece | What it is | New storage or route? |
| --- | --- | --- |
| `quantity` value type | `{value: "72.4", unit: "kg"}`: exact digits plus a UCUM code, an optional comparator for lab values below detection | no: a property type beside `money` and `decimal` |
| `observed` trait | the contract a kind binds so the fold indexes its values, one per record (`single`) or many packed into one record (`packed`) | no: a core trait |
| unit table | 198 UCUM codes in 54 named quantity kinds with exact factors; conversion only inside one kind, so kilograms never become joules | no: engine data, copied to the console and the Python module |
| three derived tables | `observed_points`, `observed_rollups`, `observed_latest` | yes: three tables, derived like `refs` and `fts` |
| aggregate read | `GET /api/v1/records?aggregate={...}`: a metric, a range, a bucket rule, reducers, a source policy, budgets | no new route: one more mode |

The two packages are samples, imported under the user's authority like `tasks`: `observations` holds the four base kinds (`metric`, `observation`, `trace`, `source`), and `health` adds what needs structure beyond a value (`sleep`, `workout`, `labreport`, `labresult`, `analyte`) plus the health catalogue. A `home` sample follows.

## What each source becomes

| Source | Arrives as | Becomes | Per day, one user (estimate) | Read with | Worked example |
| --- | --- | --- | --- | --- | --- |
| WHOOP | scored cycle, sleep and workout objects; the mirror exists today | `summary` observations keyed by WHOOP's cycle id, a typed `sleep`, stage amounts by code | about 15 records | `from: summaries`, `last` per day | WHOOP |
| Garmin | individual samples through Apple Health or Health Connect | one `trace` per metric, source and hour | about 100 records, 45,000 index points | `from: raw`; `durationIn` for zones; `sum` for steps | Garmin |
| a scale, or your hand | six measures per weigh-in, or a YAML you apply | six `gauge` observations with ids from the measure group | 6 records | rollups: `avg`, `min`, `max`; a filter across units | Scale and manual |
| a lab | a PDF with a review step, or FHIR R4 | one `labreport`, one `labresult` per analyte | a few per year | a list read by metric; yearly `avg` with `count` | Labs |
| Home Assistant | on-change states and statistics | traces per stream; coded observations for doors and presence | 150 streams, about 3,600 records | `twa`, `increase`, `timeInState`, `groupBy: about` | Reading the data |

When four of them write the same metric, nothing is merged: Unified view shows how one request chooses between them.

## Drill down

- Data flows: the path drawn step by step, who does each step, what a correction does.
- Examples by source: WHOOP, Garmin, Scale and manual, Labs, Unified view: the records as YAML, the Postgres rows, the calls, and what to build from each result.
- Reading the data: the response shape, every screen as one request, writing, budgets, functions and agents.
- Under the hood: the trait, the four forms, the quantity value and unit table, the three tables column by column, which reducer reads what.
- Review and decisions: the Codex review's fourteen findings and the fourteen choices that are yours, each with a status you can set.

## Status

Nothing is built. The plan file `docs/plans/observations.md` still carries the pre-review text; this document is the current proposal. Next, in order: fold the review verdicts into the plan, write the decision records, then phase 1 (the `quantity` type, the `observed` trait, the two samples, the `healthwhoop` adapter proving the contract on thirty WHOOP rows).
