# Importer: the NHS App (`nhs`)

The `nhs` importer reads what `nhsctl` downloads from the NHS App for one
login: the GP record's test results, written as lab reports and results, and
the whole clinical record, written in the `clinical` catalogue. Its code is in
`web/src/import/importers/nhs/`; it holds to [what the importers
share](00-shared.md).

## What it reads

**The folder.** `nhsctl` writes one folder per run, named after the run's UTC
start (`20260101T120000Z`), holding `manifest.json` and `responses/`. The
owner hands the importer one of:

- a run folder, with `manifest.json` at its top;
- the folder of runs, of which it reads the newest: the folder whose name
  starts with `20` and holds a `manifest.json`, last by name (a name is the
  run's start, so the last by name is the newest); anything else in it, such
  as the `latest` link, is not read;
- a zip of either, unpacked as the shared input unpacks one.

**Its marks.** The importer takes an input as its own where it finds a
manifest that `nhsctl` wrote: one whose `tool` starts with `nhsctl`, or
whose every recorded response came from an `nhsapp.service.nhs.uk` host. It
is sure (confidence 1) where the manifest names `nhsctl` and the run
recorded the GP record, and less sure (0.8) otherwise. It says what it
found: "An NHS App export by nhsctl, the newest of 2 runs, from 2026-01-01,
of 17 responses." A manifest of another tool, a Numan export, a Randox or
MyChart folder, a scan printout or a table is not its own.

**The manifest.** It names the run's start (`startedAt`), the session
(`session.name`, `session.nhsNumber`, `session.gpOdsCode`) and every
response the run recorded (`responses[]`: its `file`, its `url`, its
`status`, its `contentType` and, for a body that is not JSON, its
`bodyFile`). Each response is an envelope file whose `body` is the app's
JSON answer. A file the manifest names and the folder lacks fails the read,
naming the file.

**A response** is found by the path of the URL it answered (no host, no
query): of the responses to a path with status 200, the last one. The run
calls some paths more than once (the GP record three times), and the last
answer is the whole one.

| What                           | URL path                                        | Read from the body                               |
| ------------------------------ | ----------------------------------------------- | ------------------------------------------------ |
| The GP record                  | `/v1/patient/my-record`                         | `response`                                       |
| The GP practice                | `/v1/api/organisation/<the session's ODS code>` | the list of organisations                        |
| The nominated pharmacy         | `/v1/patient/nominated-pharmacy`                | `pharmacyDetails`                                |
| Electronic prescriptions       | `/api/v1/prescriptions`                         | `prescriptions`, then `pastPrescriptions`        |
| Hospital waiting lists         | `/v1/SecondaryCare/summary/`                    | `waitTimes`                                      |
| The registration               | `/v1/patient/demographics`                      | `response`                                       |
| The home address               | `/v1/patient/pds-record`                        | `currentHomeAddress`                             |
| Repeat courses                 | `/v1/patient/courses`                           | `courses`                                        |
| GP appointments                | `/v1/patient/appointments`                      | `pastAppointments`, then `upcomingAppointments`  |
| The inbox                      | `/v2/api/users/me/messages`                     | every answer's `messages`, in order              |
| A document's file              | `/v1/documents/<id>/download`                   | the response's `bodyFile`, the last per document |
| A year's historic test results | `/v1/patient/historic-test-results/<year>`      | the last answer per year                         |

Each body is decoded by a schema of the fields the importer reads; a field
that is not what the app sends fails the read, naming the response's file
and the field inside its body (`body.response.testResults.data[0].id`),
never its value. A response the run did not record, or whose body is null,
is read as empty. JSON integers past what a float holds exactly (the GP
system's code ids run to 17 digits and more) are read whole.

**The run's start** (`startedAt`, read as ISO 8601, its fraction past six
digits dropped) is every record's revision but a message's, and its UTC
date is the day on which a prescription's validity is judged. A start that
is not ISO 8601 fails the read.

## Test results

The GP record's `testResults.data` lists every test the practice holds. Where
it says it holds them only in part (`hasAllHistoricResults` false), the
importer also reads the record's `historicTestResults.data` and each year's
historic results the run downloaded. A year's answer is read for its tests
where it holds them as a list, in its `response`, as a
`historicTestResults` or `testResults` section's `data`, or as its own
`data`; an answer that holds them nowhere the importer looks is left out,
with the file named. A test listed twice is read once, where it is first
listed: the record's own results first, then the record's historic results,
then the years in the order the run downloaded them.

**A test** is one lab report, keyed by its id. **A value** of it is one
result, keyed `<test id>/<term>`; a second value of one term in one test is
left out, the first kept. A value whose term the table below does not name
is left out, and the plan says so once per term, with how many values it
had: "the GP record's <term>: no metric for it (2 values)". A test none of
whose values is left is not written.

| Report property         | Value                                                                                   |
| ----------------------- | --------------------------------------------------------------------------------------- |
| `lab`                   | the GP practice's name (Care providers, below)                                          |
| `accession`             | the test's `term`, its name (not unique)                                                |
| `at`                    | the test's `date.value`, as written                                                     |
| `orderedAt`, `issuedAt` | null                                                                                    |
| `status`                | `final`                                                                                 |
| `file`                  | the test as the record holds it, as canonical JSON, named `NHS test result <date>.json` |
| `source`                | the GP practice's source                                                                |

The `datePart` of a test's date is not read: a day's date is midnight UTC
as the record writes it.

**A value's number** is the first run of digits, with a point and digits or
not, in its `displayValue` (`152 g/L` is `152`; a minus sign is not read);
where the text holds none, its `numericValue` as Python prints a float
(`152.0`). Its comparator is its `numericOperator` where that is one of `<`,
`<=`, `>`, `>=`, `≤` (`<=`) or `≥` (`>=`); another operator is not written,
and the plan says so. A comparator printed only inside `displayValue` is
not read.

**A value in words** is one with no number: its associated texts (each a
string or a `{text}`), the non-empty ones joined by a space, else its
`displayValue`, else nothing. `No flag`, which an AKI warning stage says
when the algorithm flags nothing, is the coded word `none`.

**Its unit** is its `units` as printed, read through the shared unit table.
The percentage of nucleated red cells prints `.` for a percent, which the
table's override for `nucleated_red_cells_per_100_wbc` reads as `%`. Its **range** is one band labelled `lab` (the
range's word `normal`) from `range.minimum` and `range.maximum` where either
is given; `range.units` is not read. A value has no flag and no specimen.
Its **method** is `self reported` where its term starts with
`Self reported`. Its **coding** is `{system: emis, code: <supplierTypeCode,
else the term>, display: <the term>}`, and its `upstreamId` is
`<test id>/<term>`.

| GP record term                                                                                                | Written under                                                                 |
| ------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| Body mass index                                                                                               | entry `bmi`                                                                   |
| Body weight                                                                                                   | entry `body_mass`                                                             |
| Standing height                                                                                               | entry `height`                                                                |
| Total white cell count                                                                                        | entry `wbc`                                                                   |
| Red blood cell (RBC) count                                                                                    | entry `rbc`                                                                   |
| Haemoglobin estimation                                                                                        | entry `haemoglobin`                                                           |
| Haematocrit                                                                                                   | entry `haematocrit`                                                           |
| Mean corpuscular volume (MCV)                                                                                 | entry `mcv`                                                                   |
| Mean corpusc. haemoglobin(MCH)                                                                                | entry `mch`                                                                   |
| Mean corpusc. Hb. conc. (MCHC)                                                                                | entry `mchc`                                                                  |
| Platelet count                                                                                                | entry `platelets`                                                             |
| Neutrophil count, Lymphocyte count, Monocyte count, Eosinophil count, Basophil count                          | entries `neutrophils`, `lymphocytes`, `monocytes`, `eosinophils`, `basophils` |
| Serum ferritin                                                                                                | entry `ferritin`                                                              |
| Serum vitamin B12                                                                                             | entry `vitamin_b12` (its mass form for a mass unit)                           |
| Serum folate                                                                                                  | entry `folate` (its mass form for a mass unit)                                |
| Serum total 25-OH vit D level                                                                                 | entry `vitamin_d`                                                             |
| Se prostate specific Ag level                                                                                 | entry `psa`                                                                   |
| Serum TSH level                                                                                               | entry `tsh`                                                                   |
| Serum free T4 level                                                                                           | entry `free_t4`                                                               |
| Serum alkaline phosphatase                                                                                    | entry `alp`                                                                   |
| Serum albumin                                                                                                 | entry `albumin`                                                               |
| Serum adjusted calcium conc                                                                                   | entry `calcium_adjusted`                                                      |
| Serum calcium                                                                                                 | entry `calcium`                                                               |
| Serum inorganic phosphate                                                                                     | entry `phosphate`                                                             |
| Serum total bilirubin level                                                                                   | entry `bilirubin`                                                             |
| Serum ALT level                                                                                               | entry `alt`                                                                   |
| AST serum level                                                                                               | entry `ast`                                                                   |
| Serum cholesterol                                                                                             | entry `total_cholesterol`                                                     |
| Serum triglycerides                                                                                           | entry `triglycerides`                                                         |
| Serum HDL cholesterol level                                                                                   | entry `hdl_cholesterol`                                                       |
| Se non HDL cholesterol level                                                                                  | entry `non_hdl_cholesterol`                                                   |
| Serum LDL cholesterol level, Calculated LDL cholesterol lev                                                   | entry `ldl_cholesterol`                                                       |
| Serum cholesterol/HDL ratio                                                                                   | entry `cholesterol_hdl_ratio`                                                 |
| Serum sodium                                                                                                  | entry `sodium`                                                                |
| Serum potassium                                                                                               | entry `potassium`                                                             |
| Serum urea level                                                                                              | entry `urea`                                                                  |
| Serum creatinine                                                                                              | entry `creatinine`                                                            |
| eGFRcreat (CKD-EPI)/1.73 m\*2                                                                                 | entry `egfr`                                                                  |
| HbA1c levl - IFCC standardised                                                                                | entry `hba1c`                                                                 |
| Serum testosterone                                                                                            | entry `testosterone`                                                          |
| Red blood cell distribut width                                                                                | entry `red_cell_distribution_width`                                           |
| Mean platelet volume                                                                                          | entry `mean_platelet_volume`                                                  |
| Nucleated red blood cell count                                                                                | entry `nucleated_red_cells`                                                   |
| Percentage nucleated RBCs                                                                                     | metric `labs/nucleated_red_cells_per_100_wbc`                                 |
| QRISK2 cardiovascular disease 10 year risk score, QRISK3 cardiovascular disease 10 year risk calculator score | metric `labs/cardiovascular_risk`                                             |
| Self reported systolic blood pressure, Systolic arterial pressure                                             | entry `systolic_blood_pressure`                                               |
| Self reported diastolic blood pressure, Diastolic arterial pressure                                           | entry `diastolic_blood_pressure`                                              |
| Pulse rate, Heart rate                                                                                        | metric `health/heart_rate`                                                    |
| Peripheral oxygen saturation                                                                                  | metric `health/spo2`                                                          |
| Waist circumference                                                                                           | entry `waist_circumference`                                                   |
| Alcohol units consumed per day                                                                                | metric `health/alcohol_units_per_day`                                         |
| Alcohol units consumed per week                                                                               | metric `health/alcohol_units_per_week`                                        |
| Planned duration of foreign travel                                                                            | metric `health/planned_travel_duration`                                       |
| AKI warning stage                                                                                             | metric `labs/aki_warning_stage`                                               |

## The clinical record

Every clinical record is in the namespace `nhs`. Every builder writes every
property it writes at all, null where the record gives nothing, so a later
export clears what an earlier one set. A reference names a record of this
plan, never one no import writes: a care provider is named only where the
importer writes it.

### Care providers

Each is an `organization` keyed by its ODS code (namespace `ods`), merged into
what any importer wrote there, with the ODS code as its identifier. They are
met in this order, the first meeting of a code making it:

1. **The GP practice**: each organisation the practice's answer lists, keyed
   by its `id`: its name (made readable where it is in capitals), type
   `gppractice`, its address from `address1` to `address3`, `city` and
   `postcode`.
2. **The nominated pharmacy**, where it has an `odsCode` (it replaces a
   practice of the same code): its readable `pharmacyName` (its code where
   it has none), type `pharmacy`, its address from `addressLine1` to
   `addressLine3`, `city` and `postcode`, its `telephoneNumber` and its
   `url`.
3. **Each pharmacy an electronic prescription went to**, not met before:
   its readable `pharmacyName` (else its code), type `pharmacy`, its address
   from `pharmacyAddressText` and `pharmacyPostCode`, its phone and its web
   address.
4. **Each hospital on a waiting list**, not met before: its readable
   `provider` (else its code), type `hospital`, with no address.

Only what is known is written: a hospital has no address key, so a merge
never clears an address another importer wrote. **The GP practice** is the
organisation of the session's `gpOdsCode`; where the run holds no answer
for it, no practice is written and nothing names one.

**The source.** The GP practice is the source of the test results:
`observations/source` keyed `nhs` in the namespace `source`, class
`service`, named the practice's name, else `GP practice <ODS code>`, else
`GP practice`. It is created where absent and never written again.

### The patient registration

One `patient`, keyed `patient`: `service` `NHS England`; `name` the
demographics' `patientName`, else the session's; `identifiers` the NHS
number (the demographics', else the session's, its spaces removed), null
where there is none; `birthDate` the first ten characters of
`dateOfBirth`; `gender` from `sex` (`male`, `female`, `other`, `unknown`,
and `not known` and `not specified` as `unknown`); `address` the home
address's `lines` and `postalCode`; `practice` the GP practice;
`pharmacy` the nominated pharmacy. Where the owner's setting names their
contact, `about` names it (`substrate.reamde.dev/core/contact/<id>`);
otherwise `about` is not written, and one the owner set stands.

### Consultations

Each consultation is an `encounter`. Its key is the hash of its date's
`value`, its `doneBy` and its `consultantLocation`; the second and later
consultation of one hash, in the record's order, take `-2`, `-3`.

- Each header is a section, its `header` stripped (none where empty). The
  text gains `**<section>**` (`**Entry**` for none). Each of its
  `observationsWithTerm` is an entry `{section, term, coding, text,
upstreamId}`: the coding from its `codeId` and term (shared, Clinical
  records), its text its non-empty `associatedTexts` joined by a newline,
  its `upstreamId` its `eventGuid`; the text gains `- <term>`, with
  `: <text>` where it has text. An entry whose `eventGuid` is the id of a
  test this plan writes adds that lab report to `reports`. Then the
  header's own non-empty `associatedTexts`, joined by a newline, are an
  entry `{section, text}` and a paragraph of the text.
- An entry keeps only the fields it has.
- `doneBy` written `<name> (<role>)` is a participant with that name and
  role; otherwise the whole of it is the name, with no role.
- `summary` is the first entry's term; else the first line of the first
  entry's text, cut to 90 characters; else `Consultation`, with
  ` with <name>` where `doneBy` names someone.

`at` the date's `value` as written, `precision` from its `datePart`,
`status` `finished`, `organization` the GP practice, `location` its
`location`, `participants`, `entries`, `text` (the lines joined by a blank
line), `reports`.

### Problems, allergies and vaccinations

**A problem** is a `condition` keyed by the hash of its date and its name,
the first line item's text. Each other line item is read at its first `:`:
`Significance`, `Status` and `Ended` keep the value stripped; `Notes` adds
each of its own line items (a string or a `{text}`). `Ended` read as a day,
a full month name and a year (`3 June 2021`) is `endsAt` at midnight UTC;
otherwise it is added to the notes as `Ended: <value>`. `clinicalStatus`
is `active` for the status Active, and for Past `resolved` where it ended,
else `inactive`; otherwise none. `significance` is `major` for
Significant or Major, `minor` for Minor. `category` `problemlistitem`,
`sourceStatus` the status as written, `at`, `precision`, `organization`
the GP practice, `notes` joined by a newline.

**An allergy** is an `allergy` keyed by the hash of its date and its name:
`name`, `at`, `precision`, `organization`, `reactions` `[{description:
reaction}]` where it names one, `substance` its drug.

**A vaccination** is an `immunization` keyed by the hash of its date and
its term: `name` the term, `at`, `precision`, `organization`, `nextDueAt`
the first ten characters of `nextDate.value`, `notes` its non-empty
`associatedText` joined by a newline.

Two records of one kind under one key (two problems, allergies,
vaccinations or acute issues of one date and name; two repeats of one
course) are one record: the later one in the record's order, written where
the later one comes.

### Medicines

The record's medicines are read in three groups, each a `medicationrequest`:

| Group                           | `course`     | `status`                  | `intent` |
| ------------------------------- | ------------ | ------------------------- | -------- |
| `currentRepeatMedications`      | `continuous` | `active`                  | `plan`   |
| `discontinuedRepeatMedications` | `continuous` | `stopped`                 | `plan`   |
| `acuteMedications`              | `acute`      | from the evidence (below) | `order`  |

An item's line items are its medicine, its dosage and its quantity. A repeat
item's **course** is the first course whose name starts with the item's
first 25 characters, or that the item's name starts with. Its key is the
course's id; otherwise, for a repeat or an acute issue alike, the hash of
its date and its medicine. `at` is its `date` as written, `lastIssuedAt`
and `nextIssueAt` the course's issue dates at midnight UTC,
`organization` the GP practice.

**An acute issue's status**, as FHIR's `MedicationRequest.status`, from the
tracker's words of the electronic prescription items that match it (each
item of a prescription dated within three days of the issue, whose name and
the issue's match by their first 25 characters either way), and whether it
is a vaccine the record says was given that day:

1. `completed` where an item was handed over (collected or dispensed), or
   the vaccine was given;
2. `cancelled` where an item was cancelled and none expired or was not
   dispensed;
3. `stopped` where an item expired or was not dispensed;
4. `active` where an item is with a dispenser or to be dispensed;
5. `active` where the issue is less than six months old on the run's day
   (a prescription-only medicine can be dispensed for six months; a later
   month without the day takes its last day);
6. otherwise `unknown`: a paper, private or in-practice issue has no item.

**A vaccine was given** where an immunisation on the issue's date has a
term that says it was given (`Administration of ...`, `... vaccination`,
`... immunisation`) and nothing that says it was needed, offered, declined
or due, and names an antigen the product protects against. The antigens
are named by their words and their brands: typhoid, hepatitis A and B,
COVID-19, influenza, tetanus, diphtheria, polio, rabies, tick-borne and
Japanese encephalitis, cholera, yellow fever, meningococcal, pneumococcal,
shingles, measles, HPV and RSV.

**A pharmacy's word** for an item is read as the tracker's words, lower
case. The app sends some as one word (`WithDispenser`, `NotTracked`): such a
word is split where its capitals start, and `WithDispenserActive` is
`with dispenser - active`.

| Tracker's words                 | Dispense `status` |
| ------------------------------- | ----------------- |
| collected, dispensed            | `completed`       |
| with dispenser, to be dispensed | `preparation`     |
| with dispenser - active         | `inprogress`      |
| cancelled, expired              | `cancelled`       |
| not dispensed                   | `declined`        |
| any other (not tracked)         | `unknown`         |

**Each item of each electronic prescription**, current then past, is a
`medicationdispense` keyed `<EPS barcode>/<the item's position, from 1>`:
`medication`, `quantity`, `status` from the table, `sourceStatus` the word
as sent, `statusAt` its `statusDate` (`02-Nov-2026 14:05:00`, a London
wall-clock time) with its offset, `prescription` the barcode, `issuedAt`
the prescription's date at midnight UTC, `at` the status time where it was
handed over and else the issue, `request` the first medicine request (in
the order they were read, a course by its name first) whose name and the
item's match by their first 25 characters, `pharmacy` the pharmacy it went
to, `prescriber` its `prescriberName`, `organization` the prescribing
organisation where it is written, else the GP practice.

### Appointments and referrals

**An appointment**, past then upcoming, keyed by its id: `type` its type
stripped, `name` the type with ` with <the first clinician>`, `status`
`booked` for an upcoming one and null for a past one, `channel` from the
type's words (telephone or phone `telephone`; video `video`; SMS, email or
online `online`; face or F2F `inperson`), `service` its session name,
`location`, `phone` (its invisible marks removed), `participants` each
non-empty clinician, `at` its start, `endsAt` its end, `organization` the GP
practice.

**A waiting-list entry** is a `referral` keyed by the hash of its referral
date, its hospital's ODS code and its specialty: `name` `<specialty> at
<hospital>` (the hospital alone where there is no specialty), `specialty`,
`provider` the hospital, `status` `active`, `at` the referral date,
`plannedWait` and `typicalWait` (the planned and the 80th percentile wait,
in weeks) as days (`P126D`), `estimatedTreatmentAt` and `waitsAsOf` (the
census date) as dates, `organization` the GP practice.

### Documents

Each document the record lists is a `document` keyed by its
`documentIdentifier`: `name` its comments, else `<its term, else
Document>, <its date>` (the record's own file name carries the patient's
name and birth date, so it is never used), `type` its term, `coding` from
its `codeId` and term, `at`, `precision`, `status` `current`, `notes` its
comments, `organization` the GP practice. Its **file** is the last download
of it the run recorded: stored as `NHS document <date>.<its extension, lower
case>`, its media type the download's (before any `;`), and where that is
text, its words are `text` (read as UTF-8, a byte that is not UTF-8 read as
U+FFFD). A
document the run holds no file for is written with `file` null, and the plan
says so.

### Messages

Every inbox answer's messages, each once (the last copy, where the first
was listed), live and removed alike, are each a `communication` keyed by
its id: `sender` made readable, `topic` the first line of its body (shared,
Clinical records), `body` as sent, `at` its sent time, `organization` the
GP practice where the sender is the practice's name. Its revision is its own
`version`, else the run's start. The owner's own replies are not read.

## What it writes, in order

1. The GP practice's source (created once).
2. The care providers (merged).
3. Each test's lab report, then its results.
4. The patient registration, consultations, problems, allergies,
   vaccinations, medicine requests, dispenses, appointments, referrals,
   documents and messages, each after every record it names.

The documents are each test's JSON and each downloaded file, by digest.
Every record but the source carries a revision: the run's start, a
message its own version.

## What it asks

Nothing: the run names the practice, its ODS code and everything else the
plan writes.

## Differences from the old script

These are the plan's named differences:

- Ids are minted from the keys above, and values are written under the shared
  catalogue's metrics.
- A test's JSON file is canonical JSON (`152` where the old script wrote
  `152.0`), so its digest is new.
- The historic test results are read where the record holds its results only
  in part; the old script read neither the record's historic results nor
  the years `nhsctl` downloads.
- A pharmacy's one-word status (`WithDispenser`) is read as the tracker's
  words; the old script left such a status unknown. `NotTracked` is still
  unknown.

And these follow from the plan's rules:

- The source and every report's `lab` are the GP practice's name from the
  record; the old script wrote a name of its own. The source is keyed by the
  namespace, `nhs`.
- A care provider is merged, and only what is known is written; the old
  script wrote null for what it did not know.
- Every builder writes all its properties, null where the record gives
  nothing: a document with no file or no words writes `file` and `text`
  null, which the old script left unwritten.
- A document's file is stored under a neutral name, never the record's file
  name.
- The patient registration names the owner's contact where one is set; it
  writes no NHS number where there is none, where the old script wrote an
  empty one.
- A result is keyed by its test and term: a second value of one term in a
  test is left out, where the old script stopped on two values of one code.
- A value whose operator is not a comparator is written without it, and the
  plan says so; a nominated pharmacy with no name is named by its code; a
  waiting-list entry with no specialty is named by its hospital alone.
- A field that is not what the app sends fails the read, naming the file and
  the field.
- What the plan leaves out (a term with no metric, a year's answer the
  importer cannot read, a document with no file) is listed in the plan
  rather than printed.

What the old script did not write, the importer does not either: a medicine
prescribed elsewhere is not a medication statement, and the owner's own
messages are not read.
