# Importer: an Epic MyChart portal (`mychart`)

The `mychart` importer reads what `mychartctl` downloads from one Epic
MyChart portal: the hospital's lab results, written as lab reports and
results, and the owner's record there, written in the `clinical` catalogue.
Its code is in `web/src/import/importers/mychart/`; it holds to [what the
importers share](00-shared.md).

## What it reads

**The folder.** `mychartctl` keeps one folder per portal and updates it in
place:

```
manifest.json                      the portal and the runs that wrote the folder
items/<resource>/<upstream id>.json  one item: a visit, an order, a letter, a conversation
files/<resource>/<upstream id>.<ext> a downloaded file: a PDF, a scan, a message's image
lists/<endpoint>.json              a list endpoint's latest answer
```

An item's or a file's name is MyChart's id made safe for a file name, as
`mychartctl` makes it: MyChart's ids pass through; a trailing `^` is
dropped, any character but `A-Z a-z 0-9 . _ -` becomes `_`, dots at either
end go, and a name longer than 180 characters is cut to 160 and joined by
`-` to the first 8 hex digits of the SHA-256 of the id. Items of a resource
are read in file name order. A downloaded file of an id is the first, by
name, of `files/<resource>/<id>.<any extension>`.

The owner hands the importer the portal's folder, the folder that holds it
as its one portal folder, or a zip of either, unpacked as the shared input
unpacks one.

**Its marks.** The input is the importer's where `manifest.json` (at the
top, or in the one folder at the top that has an `items` folder beside it)
is JSON whose `tool` is `mychartctl` or starts with `mychartctl `, with a
`baseUrl` and a list of `runs`. It is sure (confidence 1) where the
manifest has items beside it, and half sure (0.5) where it has none. It says
what it found: "A mychartctl export of the MyChart portal at
mychart.example.nhs.uk: 24 test results, 18 visits, 30 conversations." A
manifest of another tool (`nhsctl`, a Randox folder's), a Numan export or a
scan printout is not its own.

**The portal** is the host of the manifest's `baseUrl`, in lower case: the
importer's namespace is `mychart/<host>`, so two portals are two accounts.
A portal under `.uk` shows its times in `Europe/London`; any other portal's
zone is asked (What it asks).

**The revision** of every record but the source is the start of the
manifest's newest run (`runs[].startedAt`, its fraction past six digits
dropped), in whole seconds. The UTC date of that start is the day on which a
prescription's validity is judged. A manifest that names no run fails the
read, naming the field `runs`.

**The files read**, each decoded by a schema of only the fields the importer
reads, every one optional and nullable where MyChart leaves it out or
empties it. A field of the wrong type fails the read, naming the file and the
field (`items/visits/1.json`, `IsPastVisit`), never its value.

| Path                                                                                                                                                                     | What                                                      |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------- |
| `items/test-results/<id>.json`                                                                                                                                           | an order (`orderName`) and its `results`: lab and imaging |
| `items/test-result-reports/<result key>.json`                                                                                                                            | an imaging result's report, `reportContent` (HTML)        |
| `items/visits/<csn>.json`                                                                                                                                                | an appointment, past or upcoming                          |
| `items/visit-details/<csn>.json`                                                                                                                                         | a past visit's documents (`avsInfo`)                      |
| `items/visit-notes/<csn>.json`                                                                                                                                           | the notes shared for a visit (`noteList`)                 |
| `items/clinical-notes/<id>.json`                                                                                                                                         | a note's HTML, `reportContent`                            |
| `lists/letters.json`, `items/letters/<id>.json`                                                                                                                          | the letters (dates, authors, visits) and each one's HTML  |
| `items/medications/<id>.json`                                                                                                                                            | a prescription, or a medicine prescribed elsewhere        |
| `items/allergies/<id>.json`, `items/health-issues/<id>.json`                                                                                                             | an allergy, a health issue                                |
| `items/messages/<conversation>.json`                                                                                                                                     | a conversation and its messages                           |
| `files/visit-documents/`, `files/test-results/`, `files/messages/`                                                                                                       | after-visit and discharge PDFs, scans, messages' images   |
| `lists/personal-details.json`, `personal-contact.json`, `health-upkeep.json`                                                                                             | the registration                                          |
| `lists/personal-relationships.json`, `histories.json`, `goals-patient.json`, `goals-care-team.json`, `care-journeys.json`, `health-summary.json`, `items/immunizations/` | only to say what is left out                              |

`files/letters/` (the logos and signatures letters embed) is not read.

## Lab results

**The results** of every order are read in file name order, then sorted by
their sample time (`orderMetadata.prioritizedInstantISO`, as text, none
first) and then their key, as text. A result whose `resultType` is not
`LAB` is an imaging result (The clinical record). A lab result with no
sample time is left out.

**A lab report** is one lab result, keyed by MyChart's result `key` whole:

| Property    | Value                                                                                                  |
| ----------- | ------------------------------------------------------------------------------------------------------ |
| `lab`       | `orderMetadata.resultingLab.name`, else the hospital's name                                            |
| `accession` | the order's `orderName`, else the result's `name`                                                      |
| `at`        | the sample time as written, with its offset                                                            |
| `orderedAt` | null                                                                                                   |
| `issuedAt`  | `resultTimestampDisplay` read as `%d %b %Y %H:%M` in the portal's zone; null where it does not read so |
| `status`    | `resultStatus` (`Final`, `Edited (Final)`), else null                                                  |
| `file`      | the whole order's JSON as canonical JSON, named `MyChart lab report <date>.json`                       |
| `source`    | the hospital's source                                                                                  |

A report is never marked for review. A report none of whose components is
left is not written.

**A component** of a result is read as follows.

1. A component with no `componentInfo.name` is left out.
2. Its analyte is the table's for its name (below), else the catalogue
   entry its name, then its `commonName`, finds. A component with neither is
   left out, counted per name: "MyChart's `<name>`: no metric for it (N
   values)".
3. A component whose `componentComments.hasContent` is true is counted: the
   lab's comments stay in the report's JSON, which the plan says.
4. Its value is `componentResultInfo.value`, stripped. **A value MyChart
   files under two orders** (a TSH under a thyroid profile and under a
   second order; a liver profile inside a renal one) is kept once: a
   component with the same name, sample time, value and unit as one read
   before (in the order above) is left out and counted, "N values MyChart
   files under a second order: kept once, in the first".
5. A comparator before digits is split from them (`<5` is `<` and `5`). A
   plain number is the value, in `componentInfo.units` (an empty unit is
   none); anything else is the result's words, null where there are none.
6. Its band is its reference range: `displayLow` and `displayHigh`,
   stripped; where both are empty, the one end `formattedReferenceRange`
   gives (`<550` is a high end, `>2.9` and `>=2.9` a low one). A band with
   either end is the lab's (`lab`), read as a plain band.
7. Its flag is `abnormalFlagCategoryValue`: `High` is `H`, `Low` `L`,
   `Normal` `N`, each with the word as its text; any other word gives none.
8. Its specimen is `specimensDisplay` before any `(`, stripped and in lower
   case (`Blood (Venous)` is `blood`), none where that is empty.
9. Its `coding` is `{system: "epic", code: commonName, else name, display:
name}`, and its key and `upstreamId` are `<result key>/<componentID>`,
   else `<result key>/<name>` (a component id alone repeats across orders).

The shared lab builders then write the result (Lab records in the shared
page): its metric by the analyte and unit, its value or words, its range,
its interpretation, its specimen with the sample time, status `final`.

**The analytes**, by MyChart's component name:

| Component                                                   | Analyte                                                                                     |
| ----------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| Alanine transaminase                                        | entry `alt`                                                                                 |
| Albumin                                                     | entry `albumin`                                                                             |
| Alkaline phosphatase                                        | entry `alp`                                                                                 |
| Basophils, Eosinophils, Lymphocytes, Monocytes, Neutrophils | the entry of the same name                                                                  |
| Bilirubin (total)                                           | entry `bilirubin`                                                                           |
| C-reactive protein                                          | entry `crp`                                                                                 |
| Calcium                                                     | entry `calcium`                                                                             |
| Calcium (albumin-adjusted)                                  | entry `calcium_adjusted`                                                                    |
| Cholesterol                                                 | entry `total_cholesterol`                                                                   |
| Cholesterol:HDL ratio                                       | entry `cholesterol_hdl_ratio`                                                               |
| Creatinine                                                  | entry `creatinine`                                                                          |
| Estimated GFR                                               | entry `egfr`                                                                                |
| Free T4                                                     | entry `free_t4`                                                                             |
| Glucose                                                     | metric `health/blood_glucose` (a venous glucose, fasting not stated)                        |
| HCT                                                         | entry `haematocrit`                                                                         |
| HDL Cholesterol                                             | entry `hdl_cholesterol`                                                                     |
| Haemoglobin (g/L)                                           | entry `haemoglobin`                                                                         |
| Haemoglobin A1c (mmol/mol)                                  | entry `hba1c`                                                                               |
| LDL Cholesterol (Calculated)                                | entry `ldl_cholesterol`                                                                     |
| MCH, MCV                                                    | entries `mch`, `mcv`                                                                        |
| MCHC (g/L)                                                  | entry `mchc`                                                                                |
| MPV                                                         | entry `mean_platelet_volume`                                                                |
| Non-HDL Cholesterol                                         | entry `non_hdl_cholesterol`                                                                 |
| Phosphate, Potassium, Sodium                                | the entry of the same name                                                                  |
| Platelet count                                              | entry `platelets`                                                                           |
| RDW                                                         | entry `red_cell_distribution_width`                                                         |
| Red cell count                                              | entry `rbc`                                                                                 |
| Serum folate                                                | entry `folate`                                                                              |
| Thyroid stimulating hormone                                 | entry `tsh`                                                                                 |
| Triglyceride                                                | entry `triglycerides`                                                                       |
| Vitamin B12                                                 | entry `vitamin_b12`                                                                         |
| White cell count                                            | entry `wbc`                                                                                 |
| XE Nucleated RBCs abs                                       | entry `nucleated_red_cells`                                                                 |
| ESR                                                         | entry `esr`                                                                                 |
| Acute Kidney Injury Warning                                 | metric `labs/aki_warning_stage`                                                             |
| Prothrombin Time, INR, APTT, APTT Ratio, D-Dimer            | metrics `labs/prothrombin_time`, `labs/inr`, `labs/aptt`, `labs/aptt_ratio`, `labs/d_dimer` |
| Reticulocytes (abs)                                         | metric `labs/reticulocytes`                                                                 |
| Reticulocyte count %                                        | metric `labs/reticulocytes_fraction`                                                        |
| Immature granulocyte count %                                | metric `labs/immature_granulocytes_fraction`                                                |

The shared unit table reads the units MyChart prints: `x10^9/L` and
`x 10^9/L` are `10*9/L`, `L/L` and `ratio` are `1`, `mm/hr` is `mm/h`, a
cholesterol ratio's empty unit is `1`, a glucose's `mmol/L` is
`mmol/L{glucose}` and a D-dimer's `ng/mL FEU` is `ng/mL`. A folate in
`ug/L` and a vitamin B12 in `ng/L` go to the entries' mass forms.

## The clinical record

**The hospital** is an `organization` keyed by the ODS code the owner gives
(namespace `ods`), merged into what any importer wrote there: `name` the
hospital's name, `type` `hospital`, `identifiers` the ODS code and, where a
visit names one, the first visit's `Organization.OrganizationIdentifier`
labelled "Epic Care Everywhere id", and `address` that visit's
`Organization.DiscreteAddress` (`StreetAddress`, `City`, `Zip`, `Country`).
Every record below names it as `organization` where the table says so.

**A pharmacy** that dispensed is an `organization` keyed `pharmacy/<id>`
under the portal's namespace, merged as any organization is: for a medicine
whose `refillDetails.owningPharmacy.id` is set and whose
`refillDetails.lastDispense` holds anything, `name` the pharmacy's name,
else `Pharmacy`, `type` `pharmacy`, and its address from
`formattedAddress`: the lines stripped and the empty ones left out, the last
line the postcode where it reads as a UK postcode, then the new last line
the town where more than one line is left. A pharmacy whose address leaves
nothing writes none.

**The patient registration** is keyed `patient`:

| Property      | Value                                                                                                                                                                                                                                      |
| ------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `service`     | the hospital's name                                                                                                                                                                                                                        |
| `name`        | the name the letters print after "Patient Name"                                                                                                                                                                                            |
| `identifiers` | the hospital number the letters print after "Hospital No" (6 to 10 digits), labelled "Hospital number", and the NHS number after "NHS No" (its spaces removed); null where they print neither                                              |
| `birthDate`   | `lists/health-upkeep.json`'s `pageInfo.dateOfBirthISO`                                                                                                                                                                                     |
| `gender`      | `lists/personal-details.json`'s `legalSex.labelText` in lower case: `male`, `female`, `unknown`, "not known" as `unknown`; else null                                                                                                       |
| `address`     | `lists/personal-contact.json`'s `currentValues.address` (`street`, `city`, `zip`) where it has a street                                                                                                                                    |
| `phone`       | the first phone number given, its invisible marks removed                                                                                                                                                                                  |
| `email`       | `emailAddress`                                                                                                                                                                                                                             |
| `practice`    | the GP practice whose ODS code the letters print after "GP practice identifier", by that code (namespace `ods`), the key the NHS App's importer writes the practice under; left out by the runner where no import has written the practice |
| `about`       | the owner's contact, where the setting names one                                                                                                                                                                                           |

The letters are read in file name order, each as markdown (below); each
number takes the value printed most often, the first seen on a tie. The
patterns ignore case.

**A visit** is keyed by its `Csn` (else its file name). Its start is
`Instant`, a `/Date(<ms>)/` instant shown in the portal's zone; its end, where
`HasDuration`, is `DurationInMinutes` later at the start's own offset, none
where the duration is zero. It is an emergency where `EncounterIsEDVisit` or
`EncounterType` is 5. Its people are the `Providers` with a `Name` and
`IsPerson` (a room or a scanner is not a participant). Its place is its
`PrimaryDepartment`'s `Address` lines, their invisible marks removed, joined
by `, `.

**How a visit happened**: the words of its after-visit summary (the PDF of
its details' `primaryAvs`, whitespace taken as one space) say "You spoke
with us on " for a telephone call and "You saw " and a word for an
attendance; else its `VisitTypeName` in lower case: containing `telephone`,
a telephone call; `video`, video; `face to face` or `outpatient`, in person;
else nothing.

**An appointment** is written for every visit that is not an emergency,
past and upcoming, so a visit that moves from one to the other is updated in
place: `name` its `VisitTypeName` (else "Appointment"), then " with " its
first person, else " at " its department; `type` its `VisitTypeName`;
`status` `cancelled` (`IsCanceled`), `noshow` (`IsNoShow`), `fulfilled`
(`IsPastVisit`) or `booked`; `channel` how it happened; `service` the
department's `Name`; `specialty` its `Specialty.Title`; `location` its place;
`phone` the department's `PhoneNumber`, its invisible marks removed;
`participants` its people; `reason` its `ChiefComplaint`; `at` and `endsAt`
its start and end; `organization` the hospital.

**An encounter** is written for every past visit with details, keyed as its
visit: `summary` "`<VisitTypeName, else Visit>`, `<specialty, else
department, else the hospital's name>`"; `type`; `class` `emergency` for an
emergency, else how it happened; `status` `finished`; `at`, `endsAt`;
`organization`; `location` the department's name; `participants`; `reason`;
`appointment` its appointment (none for an emergency); `reports` the lab
reports of the results whose `reportDetails.reportVars.ordDat` is the visit's
`Dat` (where two visits share a `Dat`, the later in file name order), those
the plan writes; and `text`: the visit type in bold, "Department:",
"Specialty:" and "Seen by:" lines where known, then a blank line,
"Documents:" and a `- <name>` line per document filed at the visit, in the
order below (a document filed twice is named twice). A lab result filed at a
visit whose report the plan does not write (none of its components is left)
is not named, and the plan says so.

**An imaging result** with no `ordDat` visit belongs to the latest visit with
details whose `VisitTypeName` is "Hospital Outpatient Visit", that started on
the result's day in the portal's zone at most two hours before the result.

**Documents** are collected in this order and each written once, the first
under its key; a document filed at a visit with an encounter names it as
`encounter`. Every document writes `name`, `type`, `at`, `precision`,
`author`, `notes`, `text`, `file`, `encounter`, `status` `current` and
`organization` the hospital, null where unknown.

1. **Visit documents**: for each visit's details, its `primaryAvs` and then
   its `additionalDocuments`, each with a `dcsID`, keyed by it: `name` its
   `reportName`, else "Visit summary"; `type` its `reportName`; `at` the
   encounter's start, where the visit has one; `text` the words pdf.js finds
   in its PDF (shared, PDFs), else null; the PDF its file, named `MyChart
visit document <date>.pdf`. One with no file in the export is left out
   and listed; one pdf.js cannot read keeps its file without words, and the
   plan says so.
2. **Letters**: for each entry of `lists/letters.json` with an item, keyed by
   its `hnoId`: `name` "Clinic letter", and ", " and the specialty of its
   visit (`csn`) where it has one; `type` "Letter"; `at` its `dateISO` at
   midnight UTC, `precision` `day`; `author` the name `users` gives its
   `empId`; `notes` its `reason`; `text` its `bodyHTML` as markdown; the HTML
   its file, `MyChart letter <date>.html`. It is filed at its visit.
3. **Clinical notes**: each item of `items/clinical-notes/`, keyed by its
   file name, matched to a visit's note by `hnoID`: `name` the note's
   `displayName`, else "Clinical note"; `type` "Clinical note"; `at` its
   `iso`; `author` its `provider.name`; `notes` "An addendum" for an
   addendum; `text` its `reportContent` as markdown; the HTML its file. It is
   filed at its note's visit.
4. **Imaging results**, in the results' order, keyed by the result's key:
   `name` the order's `orderName`, else the result's `name`, else "Imaging
   result"; `type` "Imaging result" for `IMAGING`, else the result type
   capitalised; `at` its sample time; `author` its `readingProviderName`;
   `text` its report (`items/test-result-reports/<key>.json`) as markdown;
   its first scan with a file (`scans[].dcsId`) its file. Each further scan
   with a file is a document of its own keyed by its `dcsId`, named "`<name>`,
   scan `<n>`" from 2, with no text. A scan with no file is passed over.

**A health issue** is a `condition` keyed by its file name: `name` the
`healthIssueItem`'s name, else the `localItem`'s; `category`
`problemlistitem`; `clinicalStatus` `active`; `organization`. **An
allergy** is an `allergy` keyed by its file name: `name` from `allergyItem`,
else `localItem`; `reactions` a `{manifestation}` per titled reaction, else
null; `organization`. One with no name is left out.

**A medicine** is a `medicationrequest` keyed by its `id`, else its `name`
(a repeat of a key takes `-2`, `-3`). It is prescribed where it has a
`prescriptionNumber`. `medication` its name; `dosage` its `sig`; `quantity`
`refillDetails.writtenDispenseQuantity`; `course` `acute` and `intent`
`order` where prescribed, else null; `status` (below) where prescribed,
else `active`; `at` its `startDate` (`%d %B %Y`) as that day's midnight UTC,
`precision` `day` where it has one; `prescriber` its
`authorizingProvider.name`; `organization` the hospital where prescribed;
`notes`, for a medicine prescribed elsewhere, "On the hospital's medication
list as a medicine prescribed elsewhere; the hospital did not prescribe
it."

A prescription's **status**, from its refill details on the day of the
newest run: `completed` where a dispense has a date
(`lastDispense.dispenseDate`) and no refill remains
(`hasRefillsRemaining`); else `stopped` where it expired
(`refillsRemainingKey` is `numrefills_expired`, or `refillExpirationDate`
is before that day); else `active` where a refill remains, it has an
expiry date or it is refillable (`isRefillable`); else `unknown`.

Its **last dispense**, where `lastDispense.dispenseDate` is set, is a
`medicationdispense` keyed `<medicine key>/<YYYY-MM-DD>`: `medication`;
`quantity` the dispense's `dispenseQuantity`; `status` `completed`; `at` the
dispense date at midnight UTC; `issuedAt` the medicine's start; `prescription`
its `prescriptionNumber`; `request` the medicine; `pharmacy` its pharmacy
where one was written; `prescriber`; `organization`. A dispense date that is
not a date is left out.

**Messages** are read conversation by conversation, in file name order, and
each conversation's messages in order. A message whose author has a
`wprKey` is the owner's own: it is not written (the kind holds what a
provider sent), and the plan counts it. Every other message is a
`communication` keyed by its `wmgId`: `sender` the conversation's
`userOverrideNames` for the author's `empKey`, else the author's
`displayName`, else the hospital's name; `topic` the conversation's
`subject`; `body` the message's HTML as markdown; `at` its
`deliveryInstantISO`; `organization`; `attachments` its images, else null.

A message's **images** are its `Image/Load?fileName=` references, counted
through the whole conversation (the owner's messages included): the n-th
is `files/messages/<conversation>-<n>.<ext>`, named `MyChart message image
<date>.<ext>`. An image is left out, and counted, where:

- its bytes repeat in two or more of the provider's messages that carry
  images, and in more than half of them (a banner every booking message
  opens with); or
- it is a PNG at least 2.4 times as wide as it is tall, or at most 300
  pixels on its longer side (a logo, a footer, a QR code). A JPEG is never
  judged by its shape.

**What is left out** is listed in the plan: the logos and signatures
embedded in the letters; the owner's own messages; the images left out; the
emergency contacts of `lists/personal-relationships.json`; each of
`lists/histories.json`, `goals-patient.json`, `goals-care-team.json` and
`care-journeys.json` that is there (no kind holds them); the health
summary's height and weight (measurements, not clinical records); MyChart's
immunizations (`items/immunizations/`), which this import does not read yet.

## HTML as markdown

Letters, notes, reports and messages arrive as HTML. A tokenizer reads the
markup in the order it is written, as an HTML parser that repairs nothing
reads it: no closes implied, no nodes moved. Tag names are read in lower
case; comments, declarations (`<!DOCTYPE>`) and processing instructions are
dropped; a `<` that starts no tag is text; the body of a `script` or `style`
is raw text up to its end tag; a self-closed tag (`<br/>`) is a start and an
end. Character references are read as such a parser reads them: numeric
references (`&#0;`, a surrogate or a code past U+10FFFF as U+FFFD, `&#128;`
to `&#159;` as Windows-1252, a control character dropped), and the named
references of HTML 4 and `&apos;`, the Latin-1 ones (and `&amp;`, `&lt;`,
`&gt;`, `&quot;`) also without their semicolon and as the longest name a run
of letters starts with (`&ampx` is `&x`). A name outside that table is kept
as written.

The markdown is built line by line:

- `style`, `script`, `head` and `title` are skipped, nested;
- `br` ends the line; `p`, `div`, `tr`, `table`, `ul`, `ol`, `section`,
  `article`, `header`, `footer`, `blockquote`, the headings and `li` end the
  line where they start and where they end; a heading's line is in bold; an
  `li` starts its line with `- `;
- a `td` or `th` that starts after words on the line adds `|`;
- a line's text has each run of spaces, tabs, U+00A0 and U+200B made one
  space, and is stripped; a line of only `-` keeps `- ` for the words of a
  block inside a list item; an empty line is written once between runs of
  lines;
- a line starting "Seen by patient … on" and a digit is left out (MyChart
  rewrites it each time a result is opened); blank lines at either end are
  left out; a line's leading `|` and the spaces after it are dropped.

No words is null.

## What it writes, in order

1. The hospital's source, keyed by the portal's namespace (namespace
   `source`), named the hospital's name, of class `service` (created once).
2. The hospital (keyed by its ODS code) and the pharmacies (merged).
3. Each lab report, then its results.
4. The patient registration, the appointments, the encounters, the
   documents, the health issues, the allergies, each medicine and its
   dispense, and the messages, each after every record it names.

| Record                 | Namespace        | Key                                                                          |
| ---------------------- | ---------------- | ---------------------------------------------------------------------------- |
| lab report             | `mychart/<host>` | MyChart's result key, whole                                                  |
| lab result             | `mychart/<host>` | `<result key>/<component id, else name>`                                     |
| the hospital           | `ods`            | its ODS code                                                                 |
| a pharmacy             | `mychart/<host>` | `pharmacy/<id>`                                                              |
| patient                | `mychart/<host>` | `patient`                                                                    |
| appointment, encounter | `mychart/<host>` | the visit's CSN                                                              |
| document               | `mychart/<host>` | the visit document's, letter's, note's or scan's id; an imaging result's key |
| condition, allergy     | `mychart/<host>` | the item's file name (MyChart's id)                                          |
| medicine               | `mychart/<host>` | its id, else its name                                                        |
| dispense               | `mychart/<host>` | `<medicine key>/<YYYY-MM-DD>`                                                |
| message                | `mychart/<host>` | its `wmgId`                                                                  |

Every record but the source carries the newest run's start as its revision.
Every builder writes all the properties it can write, null where the export
gives nothing, so a later export clears what an earlier one set; an
organization writes only what it knows. A reference names a record by its
key; one to a record no import has written is left out by the runner. The
documents are each lab order's JSON, the visit documents, the letters' and
notes' HTML, the scans and the messages' images, by digest, each under a
neutral name.

## What it asks

- **The hospital's ODS code** (`hospitalOds`): an Epic export never states
  it, and the hospital's record is keyed by it, so the NHS App's import and
  this one name the same hospital once. It is read in upper case without
  spaces, 3 to 10 letters and digits; anything else is asked again.
- **The hospital's name** (`hospitalName`), suggested from the export's
  organization (the visits' `Organization.OrganizationName`, else the
  medicines' `organization.organizationName`), which may be the portal's
  shorter name. Where the export names one, the question may be left
  unanswered and the export's name is taken. It names the source, the
  hospital, the patient's `service`, a report's `lab` where MyChart names
  none and a message's sender where none is named.
- **The portal's time zone** (`timeZone`), only for a portal outside `.uk`.

## Differences from the old script

These are the plan's named differences:

- Ids are minted from the keys above, and values are written under the
  shared catalogue's metrics (a folate in `ug/L` and a vitamin B12 in `ng/L`
  under their mass forms).
- An order's JSON file is canonical JSON, so its digest is new.
- A visit document's text is pdf.js's: the same words, in pdf.js's order and
  spacing.
- References to records no import wrote are left out: a lab order the plan
  writes no report for is not named by its visit's encounter, and the GP
  practice is named by its ODS key and left out where no import has written
  it.

And these follow from the plan's rules:

- The hospital's name and ODS code are the owner's answers, not written in
  the code: they name the source, the hospital, the patient's `service`, a
  report's `lab` where MyChart names none and a sender where none is named,
  where the old script wrote two names of its own. The coding system of a
  result is `epic`, where the old script named the hospital in it.
- A medicine prescribed elsewhere is noted as on "the hospital's"
  medication list, where the old script named the hospital.
- The old script left out two banner images by their hashes, and read the
  same banners to say how a visit was booked. Neither is in the code: an
  image whose bytes repeat in more than half of the messages carrying
  images is left out instead, and a visit whose summary and type do not say
  how it happened has no channel.
- The patient registration names the owner's contact where one is set.
- Every builder writes all its properties, null where the export gives
  nothing (a document's `precision`, `author`, `notes`), which the old
  script left unwritten; the hospital and the pharmacies are merged and
  write only what is known.
- A file is stored under a neutral name, never the export's.
- What the old script stopped on reads on: a lab result with no sample time
  and a dispense date that is not a date are left out and listed; a
  displayed date that does not read is null.
- A component's name the table does not hold is matched to the catalogue by
  its name, where the old script left it out.
- Where the old script wrote an id longer than 128 characters as a hash, the
  key is kept whole.
- A field that is not what MyChart sends fails the read, naming the file and
  the field.
- What is left out is listed in the plan rather than printed.

What the old script did not write, the importer does not either: a medicine
prescribed elsewhere is not a medication statement, the owner's own
messages are not written, and MyChart's immunizations are not read.
