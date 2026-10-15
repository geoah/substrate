# Examples by source

Five sources, each worked end to end in its own sub-tab: the record as YAML, the Postgres rows, the API calls and what an app builds from the results.

## At a glance

| Source | Shape | Kinds written | Example id | Rows per day (estimate) | The call | Build with it | Sub-tab |
| --- | --- | --- | --- | --- | --- | --- | --- |
| WHOOP | vendor scores per cycle, sleep, workout | `observation` (`summary`), `sleep`, `workout` | `whoop/george/1047392/recovery` | 15 records, 15 points, 0 rollups | `from: summaries`, `last` per day | the recovery screen, a 90-day recovery and HRV trend, strain per cycle | WHOOP |
| Garmin watch | dense samples, irregular at rest, 1 Hz in workouts | `trace` (`gauge` heart rate, `amount` steps), `sleep`, `workout` | `applehealth/iphone/garmin-forerunner-265/heart_rate/2026-10-07T06` | 100 records, 45,000 points, 400 rollups | `durationIn` x5 weekly; `sum` per day | heart rate zones over six months, time in zone per workout, daily steps for a year | Garmin |
| a scale, or by hand | six gauges per weigh-in | `observation` (`gauge`) | `withings/george/5f3a91/weight` | 6 records, 6 points, 6 rollups | `avg`, `min`, `max`, `count` weekly; a filter in pounds | a weight and body fat trend with a rolling mean, the current body card | Scale and manual |
| a lab | sparse values with comparators, codes, ranges | `labreport`, `labresult` | `medichecks/george/MC-2026-091422/ldl` | a few per year | a list read ordered by `at`; yearly `avg` with `count` | a biomarker trend across labs, a flagged results list | Labs |
| all of them on one metric | heart rate from four sources, weight from three | the same `metric` record, separate `source` rows | `health/heart_rate` |  | `sources: separate` or `priority` | one chart from the best source per interval, a per-source comparison, the current state of everything | Unified view |

Every sub-tab has the same four parts: what arrives, what is written (YAML), what lands in Postgres (rows), and the calls with what you can build from each result.
