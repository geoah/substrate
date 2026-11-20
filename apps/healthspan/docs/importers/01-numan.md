# Importers: Numan

Numan is an online clinic in the UK. The owner orders blood tests through
it and a partner lab runs them: Randox (at a clinic from a vein, or at home
with a Tasso+ device) or Hurdle (a capillary home kit). Numan's web app
offers a JSON download of every test ordered, each with the lab's own rows
embedded as a JSON string. The importer is `numan`
(`web/src/import/importers/numan/`); it holds to this page and to
[what every importer shares](00-shared.md).

## What it reads

**One or several Numan exports**, picked as files (`.json`). Every export
repeats the older tests, so all are read together and each test is taken
once, from the export holding its newest version.

**Which files.** A JSON file at the top of what was handed in (a picked
file, or a file directly inside a picked folder or zip) is an export when it
carries Numan's marks: it is an object whose `data` is a list of at least
one resource, each of `type` `medical_test` with a string
`attributes.short_id`. Every other file is left out, "it is not a Numan
export"; a file inside a sub-folder is not opened. An input with no export
is not the importer's.

**Detection** opens the same files and claims the input on those marks
alone: with confidence 1 where every file of the input is an export, 0.9
where other files are beside them. It says how many exports and tests
(distinct short ids) it found: "A Numan export of 3 tests.", "2 Numan
exports of 9 tests."

**An export** is a JSON:API document. Its schema declares only what the
importer reads; every other field stays in the test's own JSON, which the
report keeps as its file.

| Field | Read as |
| --- | --- |
| `data[].id` | Numan's id of the test, in Randox results' `upstreamId` |
| `data[].attributes.short_id` | the test's key and its report's accession |
| `data[].attributes.status` | the report's status (string or null) |
| `data[].attributes.created_at`, `updated_at` | the test's version (below) |
| `data[].attributes.sample_taken_date`, `sample_accessioned_date`, `results_received_at` | the report's times (below) |
| `data[].attributes.results_raw` | the lab's rows, a JSON document as a string |
| `data[].attributes.results[].collection_detail`, `collection_method` | how the sample was taken |
| `data[].attributes.observations_measurements[].identifier_short`, `value`, `normal_value` | Numan's clinical ranges |
| `data[].relationships.medical_test_review.data.id` | the test's clinical review |
| `included[]` | an object or a list of objects; those of `type` `medical_test_review` are reviews, by `id` |
| `meta.pagination.current_page`, `last_page`, `next_page` | the page the export is |

`results_raw` is one of three shapes, chosen by its shape:

- **a Randox order**, an object: `orderCreatedDate`,
  `patientSystolicBloodPressure`, `patientDiastolicBloodPressure` (a number
  or text) and `reportResults`, a list of Randox rows;
- **a Randox list**, a list whose first item has `analyte`: Randox rows;
- **a Hurdle list**, any other list: Hurdle rows. An empty list is a Hurdle
  list with no rows.

A Randox row has `analyte` (required), `displayName`, `result` and `units`
(required, text or null), `refLow`, `refHigh`, `lowHigh`, `sampleType` and
`dateOfReport`. A Hurdle row has `biomarker` with `code` (required), `name`
and `unit` (required, text or null), `result`, `interpretation`,
`interpretationData.value` (the rule) and `testId`.

## How it reads

### The newest version of a test

Each test, by its short id, is taken from the export holding its newest
version, with that export's review of it:

1. the later `updated_at` (an instant; a test with none is the oldest);
2. then the later export, by the newest time the export itself holds: the
   latest `created_at` or `updated_at` of any of its tests or reviews
   (Numan's export carries no time of its own);
3. then the later date in the file's name, its last run of exactly eight
   digits (`YYYYMMDD`), where it has one;
4. then the later file in path order.

A test an export leaves out (a later page, or a test Numan dropped) keeps
the copy another export has. The tests are written newest created first.
An export whose `meta.pagination.next_page` is set is listed among what was
left out: "it is page 1 of 3; the tests on later pages come only from other
exports".

### A test's report

- **When the sample was taken** (`at`): `sample_taken_date`, its first
  nineteen characters (`YYYY-MM-DDTHH:MM:SS`) read as London wall-clock
  time, as Numan writes it under a `Z` it does not mean, and written with
  London's offset at that moment. Where it is missing, `sample_accessioned_date`,
  a true UTC time, its `Z` written `+00:00`. A test with neither is left
  out, "it names no time its sample was taken or received".
- **Issued** (`issuedAt`): a Randox test's first `dateOfReport` among the
  rows read (below), its first nineteen characters in London; else
  `results_received_at` as written.
- **Ordered** (`orderedAt`): a Randox order's `orderCreatedDate`, its first
  nineteen characters in London; null otherwise.
- **The lab** (`lab`): `Randox (ordered through Numan)` or `Hurdle (ordered
  through Numan)`, by the shape of `results_raw`.
- `accession` is the short id; `status` is the test's as sent.
- **The revision** is the test's `updated_at`, else its `created_at`, in
  whole seconds.
- **How the sample was taken** (each result's `method`): of the pairs of
  `collection_detail` and `collection_method` the test's `results` name, the
  first in the order Python sorts their tuples' text (a quoted word before
  `None`), read through this table; any other pair, or none, is
  `capillary microtainer, home kit`.

  | `collection_detail`, `collection_method` | Method |
  | --- | --- |
  | `in-clinic`, `venous` | venous, in clinic |
  | `tasso`, `capillary` | capillary, Tasso+ device |
  | `fingerprick`, `capillary` | capillary, finger prick |

- **The file**: the test's own JSON and its review's (null where the export
  holds none), as `{"review": …, "test": …}` in canonical JSON, stored as
  `Numan test <the sample's date in London>.json`, `application/json`.

### Randox's rows

1. An analyte is read once, from its first row: Randox prints a value once
   in each panel that holds it. A later row of the same analyte whose
   result, unit, range, flag or sample differs is left out, "a second value
   under the same key; the first is kept"; an identical one is passed over.
2. The result is split into a comparator and digits (`<10` is `<` and
   `10`); digits that are a number are the value, anything else is words.
3. The lab's band, where `refLow` or `refHigh` is set: those bounds,
   labelled `lab` (its text `normal`).
4. The flag is `lowHigh` (`H`, `L`), its words the HL7 flag's; with none,
   a number with the lab's band is `N`.
5. Numan's clinical range follows (below).
6. The specimen is `sampleType` in lower case (`serum`, `whole blood`),
   none where it is empty; `fasting` is never written.
7. `coding` is `{system: "randox", code: <analyte>, display: <displayName,
   else the analyte>}`; `upstreamId` is `<test id>/<analyte>`.

A Randox order's blood pressure, where `patientSystolicBloodPressure` or
`patientDiastolicBloodPressure` is set, is a result of its own: the number
as written (a whole number with no point), in `mmHg`, under the systolic or
diastolic blood pressure, with no range, flag or specimen, the method
`measured at the clinic, sent with the order`, its code and display the
field's name and `upstreamId` `<test id>/<field>`.

### Hurdle's rows

1. A biomarker is read once, from its first row; a later row of it that
   differs is left out as above.
2. The result is split as Randox's.
3. The rule (`interpretationData.value`, `{"NORMAL": ["<3.0"], "HIGH":
   [">=3.0"]}`) gives one band per interpretation in the rule's order,
   labelled with the interpretation in lower case: a `>` or `>=` sets its
   low bound, a `<` or `<=` its high one, so `NORMAL` reads `normal`,
   `HIGH` `high`, `DEFICIENT` `low: deficient`. A rule of any other shape
   (a list of rules by age) gives no band, and the plan says so.
4. Numan's clinical range follows.
5. The flag is `N` for `NORMAL`, `H` for `HIGH`, `L` for `LOW` and
   `DEFICIENT`, none for any other; its words are the interpretation, its
   first letter a capital and the rest lower case (`Deficient`).
6. The specimen is `capillary blood`.
7. `coding` is `{system: "hurdle", code: <biomarker code>, display: <its
   name, else the code>}`; `upstreamId` is `testId`, the same on every row
   of a test.

### Numan's clinical range

Numan keeps its own range of a measure in the test's
`observations_measurements`, under its own code (the table below), the
last of one code standing. Its band:

- `normal_value`, stripped: `<low> - <high>` gives both bounds, an upper
  bound of 10000 or more read as none (`0 - 99999` is open-ended); `< X`
  the high bound; `> X` the low one. Anything else, or nothing, is no band.
- Its unit is what follows the first space of `value` (`2.9 mmol/L`), none
  where it has no space.
- It has no label: its word is `normal` and its detail `Numan clinical
  range`, so its text is `normal: Numan clinical range`, and it sorts after
  the lab's.
- Its bounds are moved into the value's unit by a fixed factor (`g/dL` to
  `g/L` by 10; `pg/mL` and `ng/L`, `ng/mL` and `ug/L`, `m[IU]/L` and
  `u[IU]/mL` by 1). A range no fixed factor moves (folate's in `nmol/L`
  against a value in `ug/L`) is left out, and the plan says so. Beside a
  value kept as received, the band keeps its words alone.

### The analytes

Each lab's code names a catalogue entry, and the entry the metric
([Metrics](00-shared.md#metrics)). A code the tables do not hold is taken by
its name (the row's display name, then the code) through the catalogue's
names and aliases; one nothing finds is left out, "the repository has no
metric for" it.

| Randox `analyte` | Hurdle `biomarker.code` | Catalogue entry | Numan's code |
| --- | --- | --- | --- |
| `Albumin` | `ALB` | `albumin` | `ALB` |
| `AlkalinePhosphataseA` | `ALP` | `alp` | `ALP` |
| `AlanineAminotransferase` | `ALT` | `alt` | `ALT` |
| `AspartateTransferase` | | `ast` | `AST` |
| `BasophilCount` | | `basophils` | `BASOPHILS` |
| `TotalBilirubin` | | `bilirubin` | `BILI` |
| `Cholesterol / HDL Ratio` | `CHDD` | `cholesterol_hdl_ratio` | `CHDD` |
| `Creatinine` | `CREA` | `creatinine` | `CREA` |
| `eGFR` | `EGFR` | `egfr` | `GFR` |
| `EosinophilCount` | | `eosinophils` | `EOSINOPHILS` |
| `Estradiol` | | `estradiol` | `ESTRADIOL` |
| `Ferritin` | `FERR` | `ferritin` | `FERR` |
| `FolicAcid` | `FOL` | `folate` | `FOLA` |
| `Free Testosterone` | | `free_testosterone` | `FREE_TESTOSTERONE` |
| `FreeAndrogenIndex` | | `free_androgen_index` | `FAI` |
| `FT4` | `FT4` | `free_t4` | `FT4` |
| `FollicleStimulatingHormone` | | `fsh` | `FSH` |
| `GammaGTP` | `GGT` | `ggt` | `GGT` |
| `Globulin` | | `globulin` | `GLOB` |
| `HCTRatio` | | `haematocrit` | `HAEMATOCRIT` |
| `HGB` | | `haemoglobin` | `HAEMOGLOBIN` |
| `Hb A1C` | `HBA1C` | `hba1c` | `B_HBA1C` |
| `HDLCholesterol` | `HDL` | `hdl_cholesterol` | `HDL` |
| `LDLCholesterol` | `LDL` | `ldl_cholesterol` | `LDL` |
| `LuteinisingHormone` | | `lh` | `LH` |
| `LymphocyteCount` | | `lymphocytes` | `LYMPHOCYTES` |
| `Magnesium` | | `magnesium` | `MG` |
| `MeanCellHaemoglobin` | | `mch` | `MCH` |
| `MeanCellHaemConc` | | `mchc` | `MCHC` |
| `MCV` | | `mcv` | `MCV` |
| `MonocyteCount` | | `monocytes` | `MONOCYTES` |
| `NeutrophilCount` | | `neutrophils` | `NEUTROPHILS` |
| `Non HDL cholesterol` | | `non_hdl_cholesterol` | `NHDF` |
| `Platelets` | | `platelets` | `PLATELETS` |
| `Prolactin` | | `prolactin` | `PROLACTIN` |
| `TPSA` | | `psa` | `PSA` |
| `RedBloodCells` | | `rbc` | `RBC` |
| `SHBG` | | `shbg` | `SHBG` |
| `Testosterone` | `TEST` | `testosterone` | `TEST` |
| `TotalCholesterol` | `CHO` | `total_cholesterol` | `CHO` |
| `TotalProtein` | | `total_protein` | `PROT` |
| `Triglyceride` | `TRI` | `triglycerides` | `TRI` |
| `ThyroidStimulatingHormone` | `TSH` | `tsh` | `TSH` |
| `Urea` | `UREA` | `urea` | `UREA` |
| `VitaminB12` | `B12` | `vitamin_b12` | `B12` |
| `25-OHVitaminD` | `VIDC` | `vitamin_d` | `VIDC` |
| `WhiteBloodCells` | | `wbc` | `WCC` |
| `patientSystolicBloodPressure` (an order's field) | | `systolic_blood_pressure` | |
| `patientDiastolicBloodPressure` (an order's field) | | `diastolic_blood_pressure` | |

The units are the shared table's ([Units](00-shared.md#units)): Randox's
codes (`MMOL_L`, `10E9-L`, `IU_L`) and Hurdle's spellings (`µmol/L`,
`mL/min/1.73m^2`); FSH's and LH's `U/L` are international units and the
free androgen index's `RATIO` a percent. Folate and vitamin B12 in mass
units go under their mass metrics. Hurdle's TSH, printed `µIU/mL`
(`u[IU]/mL`), is kept as received while the repository's units hold no
such unit.

### When the read stops

The read fails with `Unreadable`, naming the file and the field and never
the value, where an export carrying Numan's marks is not what its schema
says (`data[0].attributes.created_at`, "Expected string"), where
`results_raw` is not JSON or not one of its three shapes, or a row in it is
not (`data[2].attributes.results_raw.reportResults[4].analyte`), and where
a time it reads is not a time: `sample_taken_date`, `dateOfReport` or
`orderCreatedDate` not `YYYY-MM-DDTHH:MM:SS`, `sample_accessioned_date` not
ISO 8601 with an offset, the revision's stamp not ISO 8601.

## What it writes

| Record | Namespace | Key | Mode |
| --- | --- | --- | --- |
| `observations/source` | `source` | `numan` | created where absent: `{name: "Numan", class: "service"}` |
| `labs/labreport` | `numan` | the test's short id | maintained by revision |
| `labs/labresult` | `numan` | `<short id>/<the lab's code>` (the Randox analyte, the Hurdle biomarker code, the order's field name) | maintained by revision |

Every test is credited to the Numan source, whichever lab ran it; the lab
is the report's `lab`. Reports and results are built by the shared lab
builders ([Lab records](00-shared.md#lab-records)), every property they
maintain written, null where the test gives nothing. A report's `review`
is never written. Every record of a test carries the test's revision. A
report whose results are all left out is not written, nor its file, and
the plan says so. The records come in write order: the source, then each
report followed by its results, the newest created test first. Each report
names its file, the test's JSON, among the plan's documents.

## What it asks

Nothing: Numan's exports say all the importer needs, and Numan's times are
London's.

## Differences from the old script

The importer holds to this page. Where it differs from the script it
replaces:

- **Metrics** are the shared catalogue's (`labs/ldl_cholesterol`,
  `labs/folate_mass`), never the owner's own.
- **The file** is canonical JSON, where the script printed Python's floats
  (`140.0`); a test whose numbers both print alike keeps the script's
  bytes.
- **The newest version** is decided by the exports' own content, then the
  date in a file's name only to break a tie, then path order; the script
  ordered the exports by that date alone and stopped on a file without one.
  The exports need no folder and no names of their own.
- **An unknown code** is taken by its name through the catalogue, or left
  out with the reason; the script stopped the run.
- **A second row of an analyte** with another value is listed as left out;
  the script passed over every second row in silence.
- **A test with no sample time**, and **a Hurdle rule of another shape**,
  are left out with the reason; the script stopped on both.
- **A report with no result left** is not written; the script wrote it.
- **A result in words** keeps its ranges' words alone, as every importer
  does ([Reference ranges](00-shared.md#reference-ranges)); the script kept
  their bounds in the unit the words were not in.
- **A unit the repository does not hold** (Hurdle's TSH in `µIU/mL`) is
  kept as received, with its ranges' words, in the plan; the script did the
  same against the repository but not in its offline count.
- **A page of several** is listed among what was left out; the script
  printed a warning.
- **A result's key** is the lab's code itself; the script's id was its
  slug.
- **A review that changes while the test's `updated_at` does not** is
  written by whichever export is imported last, as in the script: both
  carry the same revision, so an older export's file is not stale.
