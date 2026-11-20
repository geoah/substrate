# Importer: Randox Health

The `randox` importer reads the folder `randoxctl` writes from the Randox
Health portal, or a zip of it: every visit's orders with their full report
data, the health report PDF of each order and the Tanita body composition
sheet of each visit. It writes the Randox source, one lab report per
reported order with its results, and one lab report per body composition
sheet with its results. Its code is in
`web/src/import/importers/randox/`; what it shares with the other importers
is on [00-shared.md](00-shared.md).

## What it reads

The folder is `pkg/rawstore`'s layout, updated in place by each run:

| Path | What it holds |
| --- | --- |
| `manifest.json` | every stored file's resource, id, endpoint, hash and times, and the runs that changed something (`runs[].started`) |
| `items/visit/<visit id>.json` | one visit (`GET v2/Visit/GetAllVisitsByUserIdWithReports`), its orders (`nexusOrders`) and each reported order's report data (`phpReportData`) |
| `items/visit-reports/<visit id>.json` | the visit's published PDFs (`GET v2/Reports/GetPublishedReportsByVisitId`): `fileName`, `published`, `isPreliminaryReport` |
| `files/report/<file name>.pdf` | the PDFs: the health report (`_SCFIRSTPHP_`), the body composition sheet (`_SCFIRSTBCA_`), the tracking report (`_SCTRACKRES_`) |
| `lists/*.json` | the account, the visit lists and the emergency contact: not read |

The export is found wherever it sits in what was handed in: its root is the
folder that holds the first `items/visit/*.json` in path order, and every
other path is read under that root.

### Detection

- An input with no `items/visit/*.json` is not a Randox export.
- An export whose `manifest.json` lists a file stored from the portal's
  visits endpoint (an `endpoint` starting `v2/Visit/GetAllVisitsByUserIdWithReports`)
  is matched with confidence 1: "A Randox Health export of 3 visits:
  randoxctl's manifest names the portal's visits".
- Without such a manifest, an export whose first visit file is an object
  with a `nexusOrders` list is matched with confidence 0.6.
- Anything else is not matched. Detection never fails: a file that cannot
  be read or is not JSON is a file without the marks.

### What it asks

Nothing. The provider's zone is `Europe/London`; the source, the lab and
the method come from the importer and the data.

## How it reads

### Files

Each visit file, each listing and the manifest are decoded by their
schemas; a field that is not what the portal writes stops the read with
`Unreadable`, naming the file and the field
(`nexusOrders[0].phpReportData.summaryData.sampleCollectionDate`). An
order's report data is decoded only where the order is reported, so a
partial report data on an order not reported yet is never read. Every field
the importer does not use is ignored.

- Visits are read in the order of their file's stem as a number (`7`
  before `101`); a stem that is not a number comes after them, by name.
- A visit's listing is `items/visit-reports/` under the same file name; a
  visit with none has no PDFs.
- The manifest is optional. Where present, its newest run start is the
  revision of a visit with no `lastUpdated`.

### Orders

Each order of each visit is one lab report, in the visit's order.

- An order is read only where `isReported` is true and it has report data;
  any other is left out ("it is not reported yet").
- Its sample id (`nexusExternalNumber`) keys the report and is its
  `accession`. A reported order with none stops the read. A second order
  with a sample id already read is left out, the first kept.
- The portal's times are UK wall-clock with no offset and seven fraction
  digits. Each is read as the text before its first `.`, as ISO 8601, and
  placed in `Europe/London` (00-shared.md, Time). A time that is not one, or
  that names an offset, stops the read, naming its field.
- `at` is the sample's collection time (`summaryData.sampleCollectionDate`);
  `issuedAt` the result time (`summaryData.resultDate`), null where there is
  none; `orderedAt` is null.
- The sample was taken fasting where `summaryData.fastingDate` is given and
  falls eight hours or more before the collection time, both read as wall
  clocks (a clock change between them is not counted); otherwise it was
  not.
- The revision is the visit's `lastUpdated` read in London, as whole
  seconds; where the visit has none, the manifest's newest run start
  (`runs[].started`); where neither gives one, the read stops, naming
  `lastUpdated`. Every record of the visit, its body composition sheet's
  included, takes it. The portal does not move `lastUpdated` when an order's
  results are published days later, so a re-read with changed results and
  the same revision is an update, not stale.

### Tests

The tests are read panel by panel, test by test, in the report's order.

- A test named `Blood pressure` is left out: it repeats the systolic value
  and its bands beside the systolic and diastolic rows.
- A test with a `secondaryReferenceRange` keeps its own reference range;
  the second one is left out, with a note naming the panel.
- A test printed in several panels (the metabolic syndrome panel repeats
  HDL cholesterol, triglycerides and glucose) is one result. Its first panel
  is its own: its result, its unit (`referenceRange.unit`) and its bands are
  that panel's. Every other panel only adds its bands, marked as another
  panel's.
- The result is `result.displayValue`, as text, stripped; a missing one is
  empty. A comparator before digits is split from them (`<20` is `<` and
  `20`). Anything that is not then a plain number is the result in words,
  whole (`Negative`, `not detected`, `Normal`).

### Bands

A test's bands come from its `referenceRange.ranges`, sorted by
`sortOrder` (a missing one is 0), the portal's order kept between equal
ones. Each band has:

- its label, `description` stripped;
- its tone, from `statusColourSymbol`: `G` good, `A` fair, `R` poor, any
  other (or none) poor;
- whether the result fell in it, `matchedRange`;
- the range's unit;
- its bounds, by `operator`, `value1` and `value2` (each stripped):
  - operator 0 with two numbers: both bounds;
  - operator 1 (`<`), 2 (`<=`), 3 (`>`) or 4 (`>=`) with `value1` a number:
    one bound, high for 1 and 2, low for 3 and 4, with its strictness;
  - otherwise, a `value1` that is not empty is the band's criterion (a
    urine strip's `Trace`, `Negative`, `Not Detected`; also a band of
    operator 0 with one number); `negative`, `not detected`, `positive` and
    `detected` are also what the band expects;
  - a band with no bound and no criterion keeps its label alone, and one
    with no label either is dropped.

Every band of a Randox test has a tone, so its ranges are toned bands
(00-shared.md, Ranges): the band the result fell in first, the other good
bands, fair, poor, then other panels' bands in the same order; a band two
panels print kept once; each range's text the portal's own label in lower
case, then its criterion, its one-sided cut-off in words (`under 25`, `at
most 25`, `over 1.7`, `at least 1.7`) and, for another panel's band, the
panel (`risk: over 1.7, metabolic syndrome`). A result in words keeps its
bands' bounds in the band's unit where the repository knows it (a urine
strip's `mg/dL`, an albumin to creatinine ratio's `mg/mmol`), else their
words alone.

The flag is the band the portal marks, among the test's own panel's bands
(00-shared.md, Flags, among toned bands): none where it marks none, `N` for
a good band, `H` for one at or above every good band's top, `L` for one at
or below every good band's bottom, else `A`; its word is `Normal`, `High`,
`Low` or `Abnormal`.

### Results

Each test is one result, keyed `<sample>/<test name>`, written by the
shared lab builders (00-shared.md, Lab records), with:

- its metric by the table below, its value in the unit the portal prints
  (00-shared.md, Units and Metrics): a number in a unit the repository does
  not know is kept as received;
- its coding `{system: "randox-health", code: <test name>, display: <test
  name>}`, and the same `<sample>/<test name>` as its `upstreamId`;
- its specimen: urine for a test whose name holds `(Urine)` and for the
  `Microalbumin / Creatinine Ratio`; none for a test of the `Personal Health
  Measurements` panel (the clinic's own measurements); blood for any other.
  A specimen carries whether the sample was taken fasting, `false`
  included; a result without one carries no fasting;
- its method: `measured at the clinic` for the personal health
  measurements, else `<programme> programme, in clinic`, the programme the
  order's `nexusOrderProgrammeName` names (`in clinic` where it names
  none).

A result with no value is left out ("it has no value"); a test the
repository has no metric for is left out, saying so.

The tests and the analytes they are written as: a catalogue entry's code
(the metric chosen by the value's unit, so folic acid in `µg/l` is the mass
form `labs/folate_mass`), else a shared metric. A test the table does not
name is matched by its name against the catalogue's names and aliases.

| Test | Analyte |
| --- | --- |
| Height, Weight, Body Mass Index (BMI) | `height`, `body_mass`, `bmi` |
| Oxygen Saturation, Pulse | `health/spo2`, `health/heart_rate` |
| Waist Circumference, Hip Circumference, Waist / Hip Ratio | `waist_circumference`, `health/hip_circumference`, `waist_hip_ratio` |
| Systolic Blood pressure, Diastolic Blood pressure | `systolic_blood_pressure`, `diastolic_blood_pressure` |
| Haemoglobin, Haematocrit, MCH, MCHC, MCV, Red Blood Cell Count | `haemoglobin`, `haematocrit`, `mch`, `mchc`, `mcv`, `rbc` |
| Basophil, Eosinophil, Lymphocyte, Monocyte, Neutrophil, White Blood Cell and Platelet Counts | `basophils`, `eosinophils`, `lymphocytes`, `monocytes`, `neutrophils`, `wbc`, `platelets` |
| Iron, Ferritin, TIBC, Transferrin, Transferrin Saturation | `iron`, `ferritin`, `tibc`, `transferrin`, `transferrin_saturation` |
| Total, LDL and HDL Cholesterol, Total Cholesterol / HDL Cholesterol Ratio, Triglycerides | `total_cholesterol`, `ldl_cholesterol`, `hdl_cholesterol`, `cholesterol_hdl_ratio`, `triglycerides` |
| Apolipoprotein A-I, B, CIII, E | `apolipoprotein_a1`, `apolipoprotein_b`, `apolipoprotein_c3`, `apolipoprotein_e` |
| Apolipoprotein B / A-I Ratio, Apolipoprotein CII, Small LDL Cholesterol | `labs/apob_apoa1_ratio`, `labs/apolipoprotein_c2`, `labs/small_dense_ldl_cholesterol` |
| Lipoprotein (a), hsCRP, Creatine Kinase | `lipoprotein_a`, `hs_crp`, `creatine_kinase` |
| FABP-3, Cardiovascular Risk Score | `labs/fabp3`, `labs/cardiovascular_risk` |
| Glucose, HbA1c, Insulin, C-peptide | `fasting_glucose`, `hba1c`, `insulin`, `c_peptide` |
| HOMA-IR, Microalbumin / Creatinine Ratio, Leptin, Resistin | `labs/homa_ir`, `labs/urine_albumin_creatinine_ratio`, `labs/leptin`, `labs/resistin` |
| Creatinine, eGFR, Cystatin C, Calcium (adjusted) | `creatinine`, `egfr`, `cystatin_c`, `calcium_adjusted` |
| Chloride, Magnesium, Phosphate, Potassium, Sodium, Urea, Uric Acid | `chloride`, `magnesium`, `phosphate`, `potassium`, `sodium`, `urea`, `uric_acid` |
| Bilirubin, Glucose, Ketones, Nitrite, pH, Protein, Urobilinogen (Urine) | `labs/urine_bilirubin`, `labs/urine_glucose`, `labs/urine_ketones`, `labs/urine_nitrite`, `labs/urine_ph`, `labs/urine_protein`, `labs/urine_urobilinogen` |
| Red Blood Cells (Urine), White Blood Cells (Urine) | `labs/urine_blood`, `labs/urine_leukocytes` |
| ALT, ALP, AST, GGT, Total Bilirubin, Albumin | `alt`, `alp`, `ast`, `ggt`, `bilirubin`, `albumin` |
| FIB-4 | `labs/fib4` |
| Pancreatic Amylase, Lipase | `pancreatic_amylase`, `lipase` |
| H. pylori, Total Antioxidant Status | `labs/h_pylori_antibody`, `labs/total_antioxidant_status` |
| Folic acid, Vitamin B12, Vitamin D | `folate`, `vitamin_b12`, `vitamin_d` |
| Rheumatoid Factor, PTH, CRP | `rheumatoid_factor`, `pth`, `crp` |
| Immunoglobulins E, A, G, M; ASO; Complement C3, C4 | `labs/ige`, `labs/iga`, `labs/igg`, `labs/igm`, `labs/aso`, `labs/complement_c3`, `labs/complement_c4` |
| TSH, FSH, LH, Prolactin, FT4, FT3 | `tsh`, `fsh`, `lh`, `prolactin`, `free_t4`, `free_t3` |
| Anti-Tg, Anti-TPO, TBG | `anti_tg`, `anti_tpo`, `labs/tbg` |
| TPSA, Oestradiol, Testosterone, SHBG, Free Testosterone | `psa`, `estradiol`, `testosterone`, `shbg`, `free_testosterone` |

The portal's full names are in `randox/tests.ts`. The unit spellings it
prints that the shared table reads otherwise: FIB-4's `-` is a score, FSH's
and LH's `U/l` are international units, H. pylori's `U/ml` arbitrary units,
`mg/mmol creatinine` is `mg/mmol` (00-shared.md, Units). `RBC/µl` and
`Leuk/µl` are not known units.

### The health report

The order's health report is the first entry of the visit's listing that is
published, not preliminary, and whose `fileName` holds `_<order id>_SCFIRSTPHP_`;
its file is `files/report/<fileName>.pdf`. It is the report's `file`, stored
as `Randox health report <collection date>.pdf`. Where the listing names
none the report has no file; where the listing names one the folder does
not hold, the report has no file and the plan says so.

The tracking report (`_SCTRACKRES_`) is not read; the plan says so once
per visit.

### The body composition sheet

The visit's body composition sheet is the listing's published, final entry
whose `fileName` holds `_<order id>_SCFIRSTBCA_`. Where the folder does not
hold its file the plan says so and the order is read without it. Its PDF is
read from its raw content streams (00-shared.md, PDFs, content streams),
each hex string's glyph ids 29 below the ASCII code. The sheet's layout is
read by positions in PDF user space (y grows upwards), by rules that stop
the read with `Unreadable`, naming the file and what was not where the
layout puts it, rather than read a wrong value.

- **Finding text.** A label is the first run, in stream order, whose text
  is the label, with `x` under `xmax`, at least `xmin` and in a font where
  given. A value near a line is the one run within `dy` (3 by default) of
  the line's `y` (less `below`), with `x` between `x0` and `x1` inclusive,
  whose text is a number (`^-?\d+(\.\d+)?$`); none, or more than one, stops
  the read.
- **The header.** The printed time is the first run `dd/mm/yyyy HH:MM`,
  London wall-clock; the report's `at` and `issuedAt`, with seconds `:00`.
  The sheet's id is the number (`^\d+$`) near the `ID` label (`xmax` 80),
  `x` 80 to 120. The scanner's model is the first run `MC-<digits>`, where
  the sheet prints one.
- **The result box.** Weight, Fat (written `Fat %`), Fat Mass, FFM, Muscle
  Mass and BMI: the label (`xmax` 100, font `F2`), its value near it at `x`
  125 to 150 (`dy` 2.5), and its desirable range, the first run within 1.5
  of the value's `y`, `x` 180 to 200, `<low>-<high>`.
- **Metabolic Age** near its label (`xmax` 100), `x` 125 to 150. **BMR**
  left of the `kcal` label on its line: `x` 120 to the label's `x` less 1,
  `dy` 1. **Visceral Fat Rating** near the `Rating` label (`xmax` 100), `x`
  125 to 150, `dy` 1.5.
- **TBW**: of the `TBW` label (`xmax` 100, `F2`), the value left of the first
  `kg` run within 10 of its line and left of `x` 180 (in kg), and the value
  left of the first `%` run likewise (TBW %), each `x` 120 to the unit's `x`
  less 1, `dy` 1.
- **ECW** and **ICW**: the label (`x` 150 to 300, `F2`), its value from 5 to
  45 right of it, `dy` 2. **ECW/TBW** near its label (`xmax` 100), `x` 125
  to 150.
- **Bone Mass** and **Protein**: the label (`xmin` 300, `F4`), its value 10.3
  under it, `dy` 12, `x` from 5 left of the label to 20 right of it.
- **Leg Muscle Score**: the one whole number 60 to 130 under its label and
  within 40 of its `x`.
- **The segments.** For each region (Trunk, Left Arm, Right Arm, Left Leg,
  Right Leg), on the left half of the page (`x` under 300) the muscle mass
  (`31.2kg`) and on the right half the fat % and fat mass (`24.1% /
  10.2kg`): the one label in that half (font `F4`), and the run in that half
  5 to 15 under it in that pattern nearest the label's `x`.
- **Phase angles at 50 kHz.** For each code (`H-L` hand to leg, `RL`, `LL`,
  `RH`, `LH`, `L-L`), the label with `x` 380 to 410, and the one run within
  1.5 of its line right of `x` 540, its degrees the leading `\d+\.\d` (the
  degree sign reads as U+FFFD).

A sheet's rows, their measures, metrics and units:

| Row | Measure (its key) | Metric | Unit |
| --- | --- | --- | --- |
| Weight | Weight | the `body_mass` entry | kg |
| Fat % | Fat % | `health/body_fat_fraction` | % |
| Fat Mass | Fat Mass | `health/fat_mass` | kg |
| FFM | FFM | `health/fat_free_mass` | kg |
| Muscle Mass | Muscle Mass | `health/muscle_mass` | kg |
| BMI | BMI | the `bmi` entry | kg/m2 |
| Metabolic Age | Metabolic Age | `health/metabolic_age` | a |
| BMR | BMR | `health/basal_metabolic_rate` | kcal/d |
| Visceral Fat Rating | Visceral Fat Rating | `health/visceral_fat_rating` | {score} |
| TBW | TBW | `health/body_water_mass` | kg |
| TBW % | TBW % | `health/body_water_fraction` | % |
| ECW, ICW | ECW, ICW | `health/extracellular_water`, `health/intracellular_water` | kg |
| ECW/TBW | ECW/TBW | `health/extracellular_water_fraction` | % |
| Bone Mass, Protein | Bone Mass, Protein | `health/bone_mass`, `health/protein_mass` | kg |
| Leg Muscle Score | Leg Muscle Score | `health/leg_muscle_score` | {score} |
| Muscle Mass `<Region>` | Muscle Mass | `health/muscle_mass` | kg |
| Fat % `<Region>` | Fat % | `health/body_fat_fraction` | % |
| Fat Mass `<Region>` | Fat Mass | `health/fat_mass` | kg |
| Phase Angle H-L (50 kHz) | Phase Angle | `health/phase_angle` | deg |
| Phase Angle Right Leg, Left Leg, Right Arm, Left Arm, Leg-Leg | Phase Angle | `health/phase_angle` | deg |

A region is the result's `channel`: `trunk`, `left_arm`, `right_arm`,
`left_leg`, `right_leg`; a limb's or the legs' phase angle `right_leg`,
`left_leg`, `right_arm`, `left_arm`, `leg_to_leg`; the whole body's (hand
to leg) none.

A row with its desirable range has the plain band `{low, high, lab:
desirable}`, written as the range `optimal` (00-shared.md, plain bands),
and is flagged against it (bounds inclusive): `N` Within desirable range,
`H` Above desirable range, `L` Below desirable range.

The sheet is a lab report keyed `<sample>/bia`: `lab` `Randox Health
(Tanita <model>)`, `accession` `<sheet id> <YYYY-MM-DD HH:MM>`, `at` and
`issuedAt` the printed time, `orderedAt` null, `status` `final`, `file` the
PDF stored as `Randox body composition <printed date>.pdf`, the Randox
source, the visit's revision, `review` `pending` when created. Each row is
a result keyed `<sample>/bia/<measure>`, with `/<channel>` after it for a
region or limb: its coding `{system: "tanita-<model in lower case>", code:
<row>, display: <row>}`, its method `BIA, Tanita <model>`, its `upstreamId`
`<sheet id>/<YYYY-MM-DDTHH:MM>/<row>`, its provenance page 1, no specimen.
Where the sheet prints no model, the lab is `Randox Health (Tanita)`, the
coding system `tanita` and the method `BIA, Tanita`.

### A weighing both give

The visit's personal measurements repeat the sheet's weight and BMI. Of a
measurement the order and the sheet both give (the same analyte and the
same region), one row is written: the order's where its row has bands or
the sheet's row has none, else the sheet's. The other is left out, and the
plan says which was kept. So the weight comes from the sheet, which gives
the scanner's desirable range, and the BMI from the order, which gives the
lab's bands.

## What it writes

In write order: the source, then for each visit in order and each reported
order, its report and its results, then its sheet's report and its results.
A report none of whose results is left is not written, nor its document.

| Record | Namespace | Key | Mode |
| --- | --- | --- | --- |
| `observations/source` | `source` | `randox` | create: `{name: Randox, class: lab, manufacturer: Randox Laboratories}`, never written again |
| `labs/labreport` (order) | `randox` | the sample id | maintain, revision the visit's |
| `labs/labresult` (order) | `randox` | `<sample>/<test name>` | maintain |
| `labs/labreport` (sheet) | `randox` | `<sample>/bia` | maintain |
| `labs/labresult` (sheet) | `randox` | `<sample>/bia/<measure>[/<channel>]` | maintain |

An order's report: `lab` `Randox Health`, `accession` the sample id, `at`,
`orderedAt` null, `issuedAt`, `status` `final`, `file`, `source`, `review`
`pending` when created. Its results and the sheet's write every property
the shared builders maintain, null where the provider gives none, so a later
export's rewrite clears what an earlier one set.

What is left out, and why, is in the plan: an order not reported yet, the
blood pressure row, a second reference range, a tracking report, a listed
PDF the folder does not hold, the row of a weighing the other gives, a
value with no metric or no value, and a value kept as received.

## Differences from the old script

The old script (`import_labs.py`'s Randox and Tanita readers) is held to
this page on the owner's files: every report and result matches by
provider key in its values, units, ranges, flags, specimens, methods,
times, codings and revisions. These differ on purpose:

- Ids are the runner's minted ids of the keys above, and the sheet's
  results are keyed by measure and region rather than by the slug of the
  row's name; the coding and `upstreamId` keep the row's own name.
- Metrics are the shared catalogues' (a regional value under the
  whole-body metric with its region as `channel`), never an owner's own.
- The method of an order's tests takes its programme from the order
  (`nexusOrderProgrammeName`), where the old script wrote one programme's
  name in; the sheet's lab, method and coding take the scanner's model from
  the sheet.
- A report with no health report PDF writes `file` null, where the old
  script left the property out.
- A test no table names is matched by its name through the catalogue, and
  left out with the reason where nothing matches, where the old script
  stopped. A layout the sheet reader does not find still stops the read, as
  before; a sheet the listing names and the folder lacks is said, where the
  old script passed it over.
- What is left out is listed in the plan: an unreported order, the blood
  pressure row, a second reference range, the tracking report and the
  weighing's dropped row, which the old script passed over in silence.
- A missing `displayValue` is no value, where the old script read it as the
  word `None`.
