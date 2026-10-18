# Blood tests and biomarkers

A lab value needs a comparator, a coded result, the lab's own reference range, the specimen, a status and the report it belongs to, so it is a `labresult` record that binds the `observed` trait beside its own properties. The engine indexes it like a weighing; the extra properties are for the app.

## What arrives, and what is written

For consumer labs (Medichecks, Function, Randox) a PDF, parsed by a function with a review step. For a health system, FHIR R4 `Observation` resources through Apple Health Records or a SMART on FHIR app, with LOINC codes and UCUM units already present. One `labreport` per document, one `labresult` per analyte:

```yaml
kind: george.example.com/health/labreport
metadata:
  id: medichecks/george/MC-2026-091422
data:
  properties:
    lab: Medichecks
    accession: MC-2026-091422
    issuedAt: 2026-09-16T10:20:00+01:00
    status: final
    file: {blob: sha256-8c1d...}        # the PDF
    review: reviewed
---
kind: george.example.com/health/labresult
metadata:
  id: medichecks/george/MC-2026-091422/ldl
data:
  properties:
    report: {ref: george.example.com/health/labreport/medichecks/george/MC-2026-091422}
    metric: {ref: george.example.com/observations/metric/health/ldl_cholesterol}
    source: {ref: george.example.com/observations/source/medichecks}
    analyteCode: {system: http://loinc.org, code: "22748-8", display: "Cholesterol in LDL [Moles/volume]"}   # as received; `code` is the trait's categorical result
    form: gauge
    hold: PT0S                          # a lab value is a point, never held forward
    value: {value: "3.4", unit: mmol/L}
    at: 2026-09-14T08:05:00+01:00       # specimen collection, not the report date
    utcOffset: 60
    specimen: {type: serum, fasting: true}
    referenceRanges: [{low: {value: "0", unit: mmol/L}, high: {value: "3.0", unit: mmol/L}, text: "optimal"}]
    interpretation: [{system: http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation, code: H}]
    status: final
    revision: 1
```

The shapes a plain number cannot hold. Exactly one of `value`, `codedValue`, `ratio`, `text` and `absentReason` is set on every write, including a patch that switches shapes:

| Result | What is stored | Why |
| --- | --- | --- |
| TSH below the assay's limit | `value: {value: "0.01", unit: "m[IU]/L", comparator: "<"}` | the true value lies below 0.01; `bounded = true` in the index, so it is counted but never averaged |
| HIV antibody | `codedValue: {system: http://snomed.info/sct, code: "260415000", display: "Not detected"}` plus the index key `code: not-detected` | a categorical result, countable, never numeric |
| a titre | `ratio: {numerator: "1", denominator: "80"}` | not a quantity; listed and compared as a ratio |
| a cancelled test | `absentReason: cancelled` | visible by date and code, never zero |
| a unit the table lacks | `sourceValue: "12.3"`, `sourceUnit: "IU/mL"`, `normalizationFailure: unknown-unit`, no `value` | the row still lists; nothing is guessed |
| a preliminary result later finalised | the same id rewritten with `status: final` and a higher `revision` | one row per analyte per report, its history in the changelog |

## What lands in Postgres

| Table | Rows per report of 40 analytes | What a row holds |
| --- | --- | --- |
| `records` | 41: the report and 40 results | the result's `props` with value, code, ranges, specimen, status; `at` is the collection time |
| `observed_points` | up to 40: one per result with a `value` or a `code` | metric `health/ldl_cholesterol`, source `medichecks`, form `gauge`, unit `mmol/L`, value 3.4, `bounded` false; the TSH row has `bounded` true |
| `observed_rollups` | up to 40, sparse | one per stream per 15-minute bucket, `n` 1 |
| `observed_latest` | up to 40 updated | the newest result per analyte and lab, so "my latest LDL" is one indexed lookup |

A result with only `text`, `ratio` or `absentReason` makes no point row; it is still a record, listed by date and `analyteCode`.

The analyte's LOINC coding lives in `analyteCode`, never in `code`: the `observed` trait declares `code` as a string, the categorical result the index keys (`not-detected` above), so one result can carry both.

## The calls, and what you build from them

| Screen | Request, in short | Served from | Build with the result |
| --- | --- | --- | --- |
| one biomarker over three years | list read, kind `labresult`, `metric` equals LDL, `orderBy=at` | `records` | the trend with each point's lab, range and flag; six to ten points, no aggregation needed |
| yearly summary with counts | aggregate, `{"calendar": "year"}`, `avg`, `min`, `max`, `count` | rollups | a yearly table with how many results stand behind each mean |
| a report, whole | list read, kind `labresult`, `report` equals the report's reference | `records` | the report page, grouped by panel in the app |
| everything flagged high or low | list read, `interpretation.code` in `[H, L, HH, LL]` | `records` | the attention list, with the lab's own flags, never computed ones |
| latest value per analyte | `groupBy: [metric, source]`, `last` | `observed_latest` | the biomarker overview card |
| values across two labs | the list read with both sources; derived molar observations where units differ | `records` | one trend across labs, with the derivation stamped on each converted point |

The list read:

```http
GET /api/v1/records?filter={"kinds":["george.example.com/health/labresult"],
  "properties":{"metric":{"eq":"george.example.com/observations/metric/health/ldl_cholesterol"}}}&orderBy=at
```

Two rules keep the trend honest:

- A value in `mg/dL` (mass concentration) and a value in `mmol/L` (amount concentration) are two quantity kinds and never pool. A function reads the `health/analyte` record's molar mass and writes a derived lab result under the paired metric with `derivedFrom` and the formula version. A read of one metric counts each reading once, the lab's own or the derived one.
- A comparator value matches a filter only when the match is certain: a stored `<0.01` matches `lt: 0.1` and not `lt: 0.005`, and `in` is refused on bounded values.

Reference ranges and interpretation flags are shown as the lab reported them, per result, because they vary by lab, sex, age and method. The `analyte` record carries a labelled fallback range only.
