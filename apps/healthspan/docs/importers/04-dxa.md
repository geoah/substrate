# Importers: DXA scans (GE Lunar)

A DXA scan measures bone density and body composition, the whole body's
and each region's. A GE Lunar scanner (Prodigy, with its enCORE software)
prints each scan as three printouts, which the scanning centre hands over
as PDFs: bone density, body composition, and a trend page that lists the
scan beside the earlier scans of the same person. Where only a photo of a
trend page is at hand, its cells can be typed into a JSON file beside the
photo. The importer is `dxa` (`web/src/import/importers/dxa/`); it holds
to this page and to [what every importer shares](00-shared.md).

The printouts this importer reads are the Greek-localised ones: their
labels are Greek, their numbers have `,` before the decimals and `.`
between thousands, their times a 12-hour clock with `πμ` (before noon)
and `μμ` (after). Another language's printouts carry other labels and are
not read.

## What it reads

**The printouts and the transcribed pages**, picked as files (`.pdf`,
`.json`, and the photos) or as a folder. Every file is looked at, wherever
it lies; the file names are not read.

- **A printout** is a PDF whose pages carry the printout header's measured
  time (below). Which printout it is follows from the tables it holds, the
  first that fits: a body composition printout holds the body composition
  table or the fat mass ratios; a trend printout the composition or fat
  distribution trend table; a bone density printout the bone density table
  or its trend. A PDF that does not open, names no measured time or holds
  none of these tables is left out with the reason. A PDF whose title names
  an enCORE report and does not open stops the read (`Unreadable`).
- **A transcribed page** is a JSON file that is an object with `scan`,
  `composition` and `distribution`. Its photo is the file its `photo`
  names: in the JSON's folder, else anywhere in the input by that name.
- Every other file is left out, "it is not a GE Lunar printout or a
  transcribed trend page". An input with neither a printout nor a
  transcribed page is not the importer's (`NotMine`).

### Detection

Detection reads no PDF's pages. A printout's PDF names the enCORE report
template it was printed from in its title (the document information's
`/Title`, as Crystal Reports writes it): `CorePatient.rpt` for bone
density, `CoreTbComp.rpt` for body composition, `CompEnhanced.rpt` for the
trend page. A JSON file with the transcribed page's fields is the other
mark. Either claims the input with confidence 1, and detection says what
it found: "3 GE Lunar enCORE printouts (1 bone density, 1 body
composition, 1 trend) and a transcribed trend page." A PDF past 32 MB is
not opened to look.

## How it reads

### A printout's text

A printout is read from pdf.js's positioned text ([PDFs](00-shared.md#pdfs)),
not from a text layout:

- **Rows.** A page's rows are its items whose baselines lie within two
  units of the row's first item, left to right, the top row first; an
  item holding only whitespace is left out. Each item is a cell, its text
  stripped and its inner runs of whitespace one space.
- **Fields.** A header field is the text right of its label on the label's
  row: the rest of the label's own cell, then the cells right of it.
- **Tables.** A table is found by its header row: a row holding the
  table's column labels, left to right, each label one cell matched
  exactly. A column the header can leave out is optional. The table's rows
  are the rows below the header that are its rows, from the first (within
  40 units of the header, the header's own further lines of units passed
  over) to the last before one that is not.
- **Cells.** A cell of a row is read as the column whose header label it
  overlaps most, of every label in the header row; a cell overlapping none
  is no column's. pdf.js joins two values printed close together into one
  item with a space between: each word of an item is then a cell of its
  own, given the share of the item's width its characters take. A region's
  label is the words wholly left of the table's first value column.

**Numbers.** A decimal is digits with `,` and digits after it or not, a
minus sign or not, read as the [decimal comma](00-shared.md#numbers) is
(`20,8` is `20.8`). A mass in grams is digits with `.` between thousands or
not (`79.205` is `79205`). A centile is digits, or `-` for none. A date is
`D/M/YYYY`, a real date. Each value keeps its digits.

### The header

Every page of a printout prints the scan's header. Read from the first row
holding each label:

| Label | Read as |
| --- | --- |
| `Μετρηθείσα:` (measured) | the scan's date and time: `D/M/YYYY H:MM:SS πμ` |
| `Analyzed:` | the analysis's date and time, the same form |
| `Όνομα αρχείου:` (file name) | the scanner's file name for the scan, its first word |

A printout whose measured time is not a date and a 12-hour time is left
out. The 12-hour clock reads the hour 12 `πμ` as 0 and `μμ` adds 12 to the
others. The times are the centre's clock, in the zone the import screen
asks (What it asks).

### The tables

**The bone density table** (bone density printout): header `Περιοχή`,
`(g/cm²)`, and where printed `T-Score` and `Z-Score`. A row is a region's
label and a BMD. The region is the first of these the label starts with:

| Label starts with | Region |
| --- | --- |
| `Κεφαλή` | `head` |
| `Αριστερός βραχίονας` | `left_arm` |
| `Αριστερό σκέλος` | `left_leg` |
| `Αριστερός κορμός` | `left_trunk` |
| `Σύνολο αριστερών` | `left_total` |
| `Δεξιός βραχίονας` | `right_arm` |
| `Δεξιό σκέλος` | `right_leg` |
| `Δεξιός κορμός` | `right_trunk` |
| `Σύνολο δεξιών` | `right_total` |
| `Βραχίονες` | `arms` |
| `Κνήμες` | `legs` |
| `Κορμός` | `trunk` |
| `Πλευρές` | `ribs` |
| `Πύελος` | `pelvis` |
| `Σπονδ` | `spine` |
| `Σύνολο` | the whole body |

A row whose label is none of these is left out, "its region is not one
this importer knows". The whole body's row gives the total BMD, and its
T-score and Z-score where those columns print numbers (a printout with no
reference population prints `-`). A re-saved printout can add a second
page with the supplementary results, the same table with every left and
right region and their BMC and area: a scan's regions are taken from its
tables the one listing most regions first, each region from the first
table listing it, with that table's page.

**The bone density trend** (bone density printout): header `Μετρηθείσα`,
`BMD`. A row is a scan's measured date and its total BMD; its age and its
change against the baseline are not read. Every row, the baseline's
included, joins the trend merge as that scan's total BMD.

**The body composition table** (body composition printout): header
`Ιστός`, `Περιοχή`, `Ιστός`, `Λίπος`, `Ιστός χωρίς`, `Οστική μάζα`,
`Συνολική`: tissue %fat, region %fat, tissue, fat, lean and BMC in grams,
and total mass in kg (`-` but for the whole body). A row is a label left of
the first column and a tissue %fat. Its labels are abbreviated or cut
short, so a row is its place: the fourteen rows are `left_arm`,
`left_leg`, `left_trunk`, `left_total`, `right_arm`, `right_leg`,
`right_trunk`, `right_total`, `arms`, `legs`, `trunk`, `android`,
`gynoid` and the whole body. A table with another number of rows, whose
twelfth and thirteenth rows are not labelled `Android` and `Gynoid`, or
with a cell that is not a number, is left out with the reason, and the
rest of the printout is read. The whole body's row joins the trend merge
as the scan's own total mass, region %fat, tissue %fat, fat, lean and BMC;
the tissue grams are not read. A first page's tissue quantitation table
(`Ιστός`, `Εκατοστό`, `Συν.`, …) is not this table and is not read.

**The fat mass ratios** (body composition printout): header `Κορμός/`,
`Κνήμες/`, `(Βραχίονες+Κνήμες)/`, then a row of three decimals: trunk to
total, legs to total, arms and legs to trunk.

**The composition trend** (trend printout): header `Ημερομηνία`,
`Ηλικία`, `Ιστός`, `Εκατοστό`, `Συν. Μάζα`, `Περιοχή`, `Ιστός`, `Παχύς`,
`Αδύνατο` (or `Αδύνατος`), `BMC`, `Fat Free`: a scan's measured date, age,
tissue %fat, centile, total mass (kg), region %fat, and tissue, fat, lean,
BMC and fat-free mass in grams. A row is one whose date cell is a date.
Each row joins the trend merge as that scan's total mass, region %fat,
tissue %fat, centile (`-` is none), fat, lean, BMC and fat-free mass; its
age and tissue grams are not read. A row with a cell that is not a number
is left out with the reason.

**The fat distribution trend** (trend printout): header `Ημερομηνία`,
`Ηλικία`, `Android`, `Gynoid`, `A/G`, `Total Body`. Each row joins the
trend merge as that scan's android %fat, gynoid %fat and their ratio; its
age and total %fat are not read.

### A transcribed trend page

A trend page typed by hand from a photo of the printout is a JSON file of
this shape, every cell a string as the printout prints it:

| Field | Read as |
| --- | --- |
| `photo` | the photo's file name |
| `scan` | the scan's date, `D/M/YYYY` |
| `measured` | its measured time, `H:MM:SS πμ` |
| `analyzed` | its analysis's date and time, `D/M/YYYY H:MM:SS πμ` |
| `file` | the scanner's file name for the scan |
| `composition[]` | the composition trend's rows: `date`, `tissue_fat`, `centile`, `body_mass`, `body_fat`, `fat_mass`, `lean_mass`, `bone_mass`, `fat_free_mass` (and `age`, `tissue`, not read) |
| `distribution[]` | the fat distribution trend's rows: `date`, `android_fat`, `gynoid_fat`, `ag_ratio` (and `age`, `tissue_fat`, not read) |
| `note` | free text, not read |

One Schema decodes it, holding each cell to the printout's format: a date
`D/M/YYYY`, a decimal with `,`, grams with `.` between thousands, a centile
of digits or `-`, the times as above. A file that is not that shape, or
whose date or time is not a real one, stops the read (`Unreadable`), naming
the file and the field, never the value. Its rows join the trend merge as
the trend printout's do. A page whose photo is not among the files is read,
said, and its scan's report names no document.

### Scans

A scan is the printouts measured on one date, keyed by that date
(`YYYY-MM-DD`). A printout measured the same date at a later time than the
earliest of that date is of another scan and is left out: a scan's report
is keyed by its date. Of two printouts of one kind of a scan (a re-saved copy
beside the original), the one with more pages is read, else the later in
path order, and the other is left out. A transcribed page is a scan of its
own `scan` date where no printout is of that date; where one is, its rows
only join the trend merge.

### The trend merge

A scan's whole-body figures come from every table that lists the scan: its
own printouts and page, and the trend tables of later scans, which repeat
the earlier ones (a re-analysis can move a figure by a gram between
tables). Of a figure:

- the scan's own printout or page wins over another scan's table;
- then the latest scan's table;
- of two of one rank, the later read: transcribed pages are read first (in
  path order), then each scan's printouts by date, its trend printout, then
  bone density (its trend, then its table's total), then body composition
  (its table's total), so a printout wins over a page of the same scan.

A `-` centile is a figure too: the latest table's dash hides an earlier
table's number.

Every date the merge holds is a report: a scan read from its printouts or
its page, and a scan known only from later scans' tables (a trend-only
scan), so a trend-only scan and its own printouts read later are one
report. A trend-only scan's report names the document listing it: a
printout before a photo, the latest scan's first, its trend printout before
its bone density printout.

## What it writes

**The source** (`observations/source`), keyed `source/dxa/<centre>`, where
`<centre>` is the [slug](00-shared.md#keys) of the centre's name, or for a
name with no Latin letter or digit, its letters and digits lower case,
every other run one `-`: `{name: <the centre's name>, class: "lab"}`,
created where absent and never written again.

**A report per scan** (`labs/labreport`), in the namespace `dxa/<centre>`,
keyed by the scan's date, in date order:

| Property | A scan with printouts | A transcribed scan | A trend-only scan |
| --- | --- | --- | --- |
| `lab` | the centre's name | the same | the same |
| `accession` | the scanner's file name, from the first printout naming it | the page's `file` | the date, `YYYY-MM-DD` |
| `at` | the measured date and time in the centre's zone | the same, from the page | the date at 12:00 in the centre's zone |
| `issuedAt` | the analysis's date and time | the same, from the page | null |
| `orderedAt` | null | null | null |
| `status` | `final` | `final` | `final` |
| `file` | the body composition printout, else the lowest-numbered (bone density 1, body composition 2, trend 3) | the photo | the document listing it (above) |
| `review` | `pending`, on create | the same | the same |

**Its results** (`labs/labresult`), keyed `<date>/<measure>` or
`<date>/<measure>/<region>`, the measure the shared metric's name. Every
result's method is `DXA whole body`, its coding `{system: "ge-lunar", code,
display}` with the scanner's code and name below, its `upstreamId`
`<date>/<code>`, its unit as printed, and its `provenance.page` the page of
its printout it was read from (none for the trend merge's), in this order:

| Results | Metric | Unit | Region (`channel`) | Code, name |
| --- | --- | --- | --- | --- |
| T-score, Z-score | `health/bmd_t_score`, `health/bmd_z_score` | `{score}` | none | `bmd_t_score` "T-score (total body, young adult)", `bmd_z_score` "Z-score (total body, age matched)" |
| the fat mass ratios | `health/trunk_to_total_fat_ratio`, `health/legs_to_total_fat_ratio`, `health/limbs_to_trunk_fat_ratio` | `1` | none | `fat_ratio_trunk_total`, `fat_ratio_legs_total`, `fat_ratio_limbs_trunk` |
| each composition region's tissue %fat, region %fat, lean, fat, BMC | `health/tissue_fat_fraction`, `health/body_fat_fraction`, `health/lean_mass`, `health/fat_mass`, `health/bone_mass` | `%`, `%`, `g`, `g`, `g` | the region | `reg_tissue_fat/<region>` "Tissue %Fat <region>", `reg_fat_fraction/…`, `reg_lean_mass/…`, `reg_fat_mass/…`, `reg_bmc/…` |
| each bone density region's BMD | `health/bone_mineral_density` | `g/cm2` | the region | `reg_bmd/<region>` "BMD <region>" |
| total mass | `health/body_mass` | `kg` | none | "Total mass" |
| region %fat | `health/body_fat_fraction` | `%` | none | "Region %Fat (total)" |
| tissue %fat | `health/tissue_fat_fraction` | `%` | none | "Tissue %Fat (total)" |
| fat, lean, fat-free mass, BMC | `health/fat_mass`, `health/lean_mass`, `health/fat_free_mass`, `health/bone_mass` | `g` | none | "Fat (total)", "Lean (total)", "Fat free (total)", "BMC (total)" |
| android, gynoid %fat | `health/tissue_fat_fraction` | `%` | `android`, `gynoid` | "Android %Fat", "Gynoid %Fat" |
| A/G ratio | `health/android_gynoid_ratio` | `1` | none | "A/G ratio" |
| total BMD | `health/bone_mineral_density` | `g/cm2` | none | "BMD (total body)" |
| centile | `health/body_fat_centile` | `{score}` | none | "Centile (US reference, v107)" |

The whole-body rows (total mass onward) come from the trend merge, each
where its figure is a number. The trend's android and gynoid %fat are the
same measure as the body composition table's tissue %fat of those regions,
under one key: the table's is kept, and where the two differ the plan says
so. The results go through the shared [lab records](00-shared.md#lab-records):
a value whose metric the repository lacks is left out with the reason.

**The revision** of every report and result is the newest scan date of the
import (of its printouts and transcribed pages) at midnight UTC, in seconds:
a later scan's table can restate an earlier scan's figures, so adding a
scan raises every DXA record's revision once, and an older set of files
replayed after a newer one is stale.

**Documents.** A report's printout or photo is stored as it is, under a
neutral name, `DXA <kind> <date>.<ext>`, where the kind is `bone density`,
`body composition`, `trend` or `trend photo` and the date the scan the
document is of: `DXA body composition 2025-01-01.pdf`. A photo's media
type is its extension's. Only the documents of reports written are stored.

## What it asks

The printouts name neither the centre nor its clock's zone, so the import
screen asks both before the read:

- `centre`, "The scanning centre's name", required: the source's name and
  every report's `lab`, and the key the centre's records are found by, so
  the same name is given for every import of one centre's scans. It is
  suggested where the repository holds exactly one source this importer
  made for a centre (a source whose id is the one its own name derives),
  and the owner has not renamed it.
- `zone`, "The scanning centre's time zone", required, a time zone,
  suggested from the browser's. A zone the browser does not know is asked
  again.

A read without them asks for them (`NeedsAnswers`).

## What it leaves out

Said in the plan's left-out list:

- for each scan from printouts, the header's height, weight and BMI (the
  operator types them in) and, where its bone density printout prints the
  BMD trend, the BMD change against the baseline (derived from the BMD
  values read);
- for a transcribed scan, the header's height, weight and BMI, and its bone
  density and regional composition (only the trend page was transcribed);
- the ages, the tissue grams, the trend's total body %fat, the tissue
  quantitation table and the printouts' charts, which are not read.

## Differences from the old script

The old script (`import_labs.py`'s DXA reader) is held to this page on the
owner's files: every report and result matches by scan date, measure and
region in its values, units, times, accessions, documents, codings,
`upstreamId`s, methods, pages and revisions. These differ on purpose:

- Ids are the runner's minted ids of the keys above, and results are keyed
  by measure and region rather than by the slug of their code; the coding
  and `upstreamId` keep the scanner's code.
- Metrics are the shared catalogues' (a regional value under the
  whole-body metric with its region as `channel`), never an owner's own.
  The trend's android and gynoid %fat are tissue %fat with the region as
  `channel`, and where the body composition table gives the same, they are
  kept once (the composition table's), where the old script wrote both.
- The centre's name, the source and the zone are the owner's answers, where
  the old script wrote one centre's name, a source named after its doctor
  and one zone in.
- The printouts are read from pdf.js's positions, by their column headers.
  The bone density table's T-score and Z-score are read from their own
  columns: the old script's pattern read them the other way round, writing
  the age-matched Z-score as the T-score and the T-score as the Z-score.
- Printouts are recognised and grouped into scans by their content (the
  tables they hold, the measured date), not by their file names; a second
  scan of one date and a second copy of a printout are said and left out,
  where the old script let one replace the other.
- The body composition table's whole-body row joins the trend merge as the
  scan's own figures, so a scan with no trend printout still has its
  whole-body results; where the trend printout is there they are the same.
- A trend-only scan's document is the latest scan's printout listing it
  before any photo, as before; a scan listed only in a bone density trend
  is attached to that bone density printout, where the old script attached
  the same scan's trend page (which does not list it) or stopped where
  there was none.
- A cell that is not a number, a composition table that is not the
  fourteen regions, or a region it does not know is left out with the
  reason, where the old script stopped; a transcribed page's cells are held
  to the printout's format, where the old script wrote what it found.
- A photo's media type is its extension's, where the old script wrote
  `image/jpeg` for any; a document's name is neutral, where the old script
  kept the file's own name.
- What is left out is listed in the plan, with the files that are not
  printouts or pages.
