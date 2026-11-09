# Importers: what they share

Every provider importer reads what the owner hands it into a plan
([docs/plans/healthspan.md, Importers](../../../../docs/plans/healthspan.md#importers)),
and the one runner writes the plan. The pieces every importer reads with and
builds from are in `web/src/import/importers/shared/`, the input in
`web/src/import/importers/input.ts` and the test helpers in
`web/src/import/importers/testing.ts`. This page states the rules those
pieces keep. Each importer's own page states what it reads and writes; an
importer holds to both.

## Input

What the owner hands in is one `Input`: a name and a set of files, each by
its path inside what was handed in (`items/visit/12.json`), `/`-separated,
with no leading `/`.

- A picked file is a file named by its own name. Several loose files keep
  their names, and the input is named after the first ("a.json and 2
  more").
- A picked folder (`<input webkitdirectory>`) keeps each file's
  `webkitRelativePath` without the folder's own name, and the folder names
  the input.
- A dropped folder is read whole, through every level, its files' paths
  inside it, and it names the input. Several dropped items keep every path
  from the drop.
- A zip, picked or dropped alone, is unpacked into the same input: when
  every file in it sits under one top-level folder, that folder is taken
  off. The zip's file name names the input.
- macOS's `__MACOSX` folders and `.DS_Store` files, and Windows's
  `Thumbs.db`, are left out of every input.

Files are listed in path order. A file's bytes are read only when an
importer asks for them, once; a zip's file is unpacked only then. Text is
UTF-8, a byte-order mark dropped. A file's type is the browser's for a
picked file and its extension's for a file of a zip, "" for an extension the
table under Documents does not hold.

## Reading files

A provider's JSON is decoded by an Effect `Schema`. A file that is not what
the schema says fails the read with `Unreadable`, naming the file's path,
the first field that is wrong as a path (`data[0].attributes.short_id`) and
the decoder's message ("Expected string", "Missing key"). The message never
holds the value, which can be the owner's data. A file that is not JSON is
`Unreadable` with "the file is not JSON".

A reading that finds nothing it can use (a time that is not ISO 8601, a
manifest that names no run) gives `undefined`, and the importer turns that
into `Unreadable`, naming the file and the field, where its page says the
read stops.

## Time

**A wall-clock time in a named zone** is an instant with the zone's offset
at that moment. A time the zone repeats (the hour clocks go back) takes the
earlier offset, the summer one; a time in the hour the zone skips (the hour
clocks go forward) keeps the offset from before the change, so 01:30 on the
spring morning in London is `01:30+00:00`. Python's zoneinfo gives these at
fold 0; Temporal's `compatible` would move the skipped time forward
instead. The zone is the provider's, never the browser's.

| Provider | Zone |
| --- | --- |
| Numan (sample times written as wall-clock with a false `Z`) | Europe/London |
| Randox's portal, and the body composition sheet of a visit | Europe/London |
| The NHS App's prescription status dates | Europe/London |
| MyChart's displayed times and `/Date(ms)/` instants, for UK portals | Europe/London |
| The scanning centre's printouts | the zone the import screen asks with the centre |

A stamp that carries its offset (the NHS App's, MyChart's ISO times) is
written as received.

**ISO 8601 text** is `YYYY-MM-DDTHH:MM:SS` with the offset as `+HH:MM`, its
microseconds as six digits only where they are not zero. A date is
`YYYY-MM-DD`. A clinical date with no time, where a kind wants a datetime,
is midnight UTC (`2026-01-02T00:00:00Z`). A stamp is read as Python's
`fromisoformat` reads one: a date alone, a time to the minute or the second,
a fraction (of which the digits past six are dropped), and `Z`, `+HH:MM`,
`+HHMM` or `+HH`.

**A provider's own formats** are read by `strptime` directives as Python
reads them: `%d %m %Y %y %H %I %M %S %f %p %b %B`, English month names in
any case, a space matching any run of whitespace, the whole text matched.

**A revision** is whole seconds since the epoch, truncated as Python's
`int(timestamp)` truncates. A stamp with no offset is read in the
provider's zone. A fetcher's run started at its manifest's `startedAt`, else
at the newest of its `runs[].startedAt` or `runs[].started`; a manifest that
names none gives no revision.

**Calendar months** are added with the day kept where the month has it and
the month's last day where it has not: 31 January and one month is the end
of February.

## Numbers

A value keeps the digits the lab wrote, as a string: `2.90` stays `2.90`.

- **A comparator** before digits is split from them: `< 5` is `<` and `5`,
  `≤` reads as `<=` and `≥` as `>=`. Anything else is the whole text,
  stripped.
- **A number** is digits, with a point and digits after it or not, and a
  minus sign or not. `+1`, `1e5`, `.5` and `1,5` are not numbers.
- **A decimal comma**, where a provider prints one, is read by removing
  every `.` (a thousands separator) and then reading `,` as the point:
  `12.345` is `12345`, `20,8` is `20.8`.
- **Arithmetic** is decimal, never floating point: two values compare as
  decimals (`2.90` equals `2.9`), and a range scaled by a factor is the
  product with no trailing zeros, written in fixed point (`3.0` times 10 is
  `30`).
- **A float's digits**, where a provider's digits are only a parsed float,
  are printed as Python's `str` prints them: `140.0`, `1e-05`, `0.0001`.
- **Stripping** removes Python's whitespace from either end, which holds
  the information separators U+001C to U+001F and not U+FEFF.

## Units

A unit as a provider prints it is a UCUM code by this table. A spelling not
in it is kept as printed. One analyte can mean a spelling otherwise (below).

| UCUM code | Spellings |
| --- | --- |
| `g/L` | `G_L`, `g/l`, `g/L` |
| `g/dL` | `g/dL` |
| `10*9/L` | `10E9-L`, `10⁹/L`, `x10^9/L`, `x 10^9/L` |
| `10*12/L` | `10E12-L`, `10¹²/L`, `X10^12/L`, `x10^12/L` |
| `1` | `RATIO`, `ratio`, `Ratio`, `-`, `1`, `L/L`, `.` |
| `pmol/L` | `PMOLL`, `pmol/l`, `pmol/L` |
| `[IU]/L` | `IU_L` |
| `nmol/L` | `NMOL-L`, `nmol/l`, `nmol/L` |
| `mmol/mol` | `MMOL_MOL`, `mmol/mol` |
| `%` | `PERCENT`, `%` |
| `mmol/L` | `MMOL_L`, `mmol/l`, `mmol/L` |
| `fL` | `FL`, `fl`, `fL` |
| `pg` | `PG`, `pg` |
| `m[IU]/L` | `MIU_L`, `mIU/l`, `mIU/L`, `mU/L` |
| `ug/L` | `UG_L`, `µg/l`, `µg/L`, `ug/L` |
| `U/L` | `U_L`, `U/l`, `U/L`, `IU/L` |
| `umol/L` | `UMOL-L`, `µmol/l`, `µmol/L`, `umol/L` |
| `mL/min/{1.73_m2}` | `ml/min/1.73m2`, `ml/min/1.73m²`, `mL/min/1.73m^2`, `mL/min/1.73m²` |
| `ng/L` | `NG_L`, `ng/l`, `ng/L` |
| `u[IU]/mL` | `µIU/mL` |
| `ng/mL` | `ng/mL`, `ng/ml` |
| `pg/mL` | `pg/mL` |
| `mg/dL` | `mg/dl` |
| `mg/L` | `mg/l`, `mg/L` |
| `ug/mL` | `µg/ml` |
| `k[IU]/L` | `kU/l` |
| `[IU]/mL` | `IU/ml` |
| `[arb'U]/mL` | `U/ml` |
| `/min` | `BPM`, `beats/min` |
| `mm[Hg]` | `mmHg` |
| `cm`, `m`, `kg`, `g`, `a`, `s`, `deg`, `{score}`, `kcal/d`, `g/cm2` | as written |
| `kg/m2` | `kg/m²`, `kg/m2` |
| `[pH]` | `pH` |
| `mg/mmol` | `mg/mmol creatinine`, `mg/mmol` |
| `/d` | `/day` |
| `/wk` | `U/week` |
| `d` | `day` |
| `mm/h` | `mm/hr` |

The two micro signs (U+00B5 and the Greek U+03BC) are one.

An analyte is named, for these overrides, by its catalogue entry's code, or
for a measure outside the catalogue by its shared metric's id after the
package (`blood_glucose`). A missing unit is written `(none)`.

| Analyte | Spelling | UCUM code |
| --- | --- | --- |
| `fsh`, `lh` | `U/l`, `U/L` | `[IU]/L` |
| `free_androgen_index` | `RATIO`, `ratio` | `%` |
| `fib4` | `-` | `{score}` |
| `egfr` | `mL/min` | `mL/min/{1.73_m2}` |
| `cholesterol_hdl_ratio` | (none), the empty string | `1` |
| `nucleated_red_cells_fraction` | `.` | `%` |
| `blood_glucose` | `mmol/L` | `mmol/L{glucose}` |
| `d_dimer` | `ng/mL FEU` | `ng/mL` |

**A known unit** is one whose code the tables write and the repository's
unit graph holds (`GET /api/v1/units`, matched by id, code or alias). A
value in a known unit is written as a quantity in its UCUM code; the server
stores the unit's id and keeps the digits. A value in any other unit is
kept as received (Lab records, below).

**A provider's own reference range** in a unit other than its value's is
moved into the value's unit only by a fixed factor: `g/dL` to `g/L` by 10,
and `pg/mL` and `ng/L`, `ng/mL` and `ug/L`, `m[IU]/L` and `u[IU]/mL` both
ways by 1. A range no fixed factor moves (a mass concentration against an
amount concentration) is left out, and the plan says so.

## Metrics

A value is written under a metric of the shared catalogues (`labs`,
`health`), read from the repository, never one of an owner's own
authority. An importer's table names each analyte by its catalogue entry's
code, by a shared metric's id for a measure the catalogue has no entry for,
or by a LOINC code or a name the catalogue's matching finds.

1. The analyte's metrics are the shared metric its table names, then its
   entry's (as the catalogue claims them, by LOINC code, then the entry's
   shared metric), then any other holding its LOINC code. None: the value
   is left out, "the repository has no metric for" it.
2. A number in a known unit is written under the named metric where its
   unit converts to that metric's, else under the entry's metric of the
   value's unit form (the display form first), else under the first metric
   whose unit takes it.
3. A number in a unit that is not known is kept as received under the
   named metric, else the entry's display form, with `normalizationFailure:
   unknown-unit`.
4. A number in a known unit no metric of the analyte takes is kept as
   received the same way, with `normalizationFailure: unconvertible-unit`.
5. Words are written under the named metric, else the entry's display
   form.

A value kept as received is listed in the plan's left-out list with the
reason, though its record is written.

## Lab records

**A provider's source** (`observations/source`) is keyed `source/<the
provider's namespace>`: created where absent with its name and class (and
its manufacturer where the importer knows one), and never written again, so
the owner's renaming and ranking stand. Every report and result of the
provider names it as `source`.

**A lab report** (`labs/labreport`) writes these properties, null where the
provider gives none:

| Property | Value |
| --- | --- |
| `lab` | the lab that issued it, as the importer's page words it |
| `accession` | the lab's own number for the report |
| `at` | when the specimen was taken, with its offset |
| `orderedAt`, `issuedAt` | when ordered and issued |
| `status` | the report's status as the provider sent it |
| `file` | the report's document, by its digest |
| `source` | the provider's source |
| `review` | `pending` for a report read from a document, written when the report is created and never again; absent for the others |

**A lab result** (`labs/labresult`) writes every one of these properties,
null where the provider gives none, so a later export's rewrite clears what
an earlier one set:

| Property | Value |
| --- | --- |
| `metric` | the metric chosen (Metrics, above) |
| `source`, `report` | the provider's source and the result's report |
| `form`, `hold` | `gauge`, `PT0S` |
| `at`, `utcOffset` | the report's `at`, and its offset in minutes |
| `channel` | the body region of a regional body composition result (`trunk`, `left_arm`) |
| `value` | a number in a known unit: `{value, unit, comparator?}`, the digits as written |
| `sourceValue`, `sourceUnit`, `normalizationFailure` | a number kept as received: the comparator and digits, the unit as printed, and why |
| `code`, `codedValue` | words that are a code (below) |
| `text` | other words |
| `referenceRanges` | the ranges from the provider's bands (below), null for none |
| `interpretation` | the flag (below) |
| `coding` | the analyte's code as the provider sent it: `{system, code, display}`, the system the importer's own name for the provider's coding |
| `upstreamId` | the provider's own id for the value |
| `specimen` | `{material, collectedAt, fasting?}` where the provider names the material; `fasting` only where the provider says, `false` included |
| `method` | how it was measured, where the provider says |
| `status` | the result's status as sent, `final` where the provider gives none |
| `provenance` | `{page}` for a value read from a document's page |

The runner writes `origin` and both kinds' `revision`, the provider's, from
the plan. A row with neither a number nor words is left out ("it has no
value"). A report none of whose results is left is not written. A second
row under a result key the report already has is left out, the first kept.

**Coded words** are written as a code, their words kept as received:

| Words (any case) | `code` | `codedValue` |
| --- | --- | --- |
| negative | `negative` | SNOMED CT 260385009 |
| not detected | `not-detected` | SNOMED CT 260415000 |
| no flag | `none` | the words alone |

### Reference ranges

A provider's bands are read one of two ways: as toned bands where every
band has a tone (the provider colours its bands good, fair or poor), else
as plain bands.

**Plain bands.** Each band's bounds are written in the value's known unit,
each only where it is a number; a value kept as received or written as
words keeps its bands' words alone. A band whose own unit differs from the
value's is moved by a fixed factor or left out (Units, above). A band with
no bound and no label is dropped, and a band equal to one before it (the
same bounds and text) is kept once. Its text is its word, then what it
expects, the word's detail, the band's own detail and, for another panel's
band, the panel, in lower case after a colon and joined by commas
(`normal`, `low: deficient`, `normal: Numan clinical range`). The word is
the band's label read through this table, an unknown label reading `normal`
with the label as its detail:

| Label | Word | Detail |
| --- | --- | --- |
| lab, normal, satisfactory, sufficiency | normal | |
| low risk | normal | low risk |
| optimal, desirable | optimal | |
| high | high | |
| very high, overweight, obese | high | the label |
| low | low | |
| deficient, underweight | low | the label |
| borderline | borderline | |

A band whose word is high or low has the tone `poor`; others none (read as
good). Ranges are ordered normal and optimal first, then the others, then
other panels' bands, then a provider's own range (a band with a detail of
its own), each group in the provider's order.

**Toned bands.** Each band's bounds are written in the value's known unit,
or, for words, in the band's own unit where that is known. A band two
panels print is kept once (the same bounds, text and tone). Its text is the
provider's own word for it, lower case and not read through the table, then
its criterion, its one-sided cut-off in words and, for another panel's
band, the panel: `deficiency: under 25`, `risk: over 1.7, metabolic
syndrome`. A one-sided cut-off keeps its strictness in words, which a
quantity cannot hold: `under X` (`<`), `at most X` (`<=`), `over X` (`>`),
`at least X` (`>=`). The tone is written unless it is good. Ranges are
ordered the good band the value fell in first, then other good bands, fair,
poor, then other panels' bands in the same order.

### Flags

A flag is written as the result's `interpretation`: `{system, code,
display}` in HL7's `v3-ObservationInterpretation`, the display the
provider's words where they are longer than the letter, else the flag's
word (`N` Normal, `H` High, `L` Low, `A` Abnormal, `HH` Critically high,
`LL` Critically low). A flag HL7 does not hold is written as its code
alone. This is the shape the CSV import writes.

- **Among toned bands** the flag is the band the provider marks as the
  value's: none where it marks none, `N` for a good band, `H` for a band
  whose low bound is at or above every good band's high bound, `L` for one
  whose high bound is at or below every good band's low bound, else `A`.
- **Against a range** the flag is `L` below the low bound, `H` above the
  high one, else `N`, compared as decimals with both bounds inclusive.

## Keys

A planned record is found by its kind, a namespace and a key; the runner
derives its id (`DerivedMintedID` of `healthspan/<namespace>/<kind
name>/<key>`). The namespace is the provider's account (`numan`,
`dxa/<the centre's key>`, `mychart/<the portal's host>`), `source` for a
provider's source and `ods` for an organization. The key comes from the
provider's data, as each importer's page states.

- **A hash**, for a key the provider gives no id for, is SHA-256 over the
  parts, each written as its JSON text (a missing part as `null`), joined
  by U+001F, as lower-case hex.
- **Repeats** of one key in the order the provider lists them take `-2`,
  `-3` after the first.
- **A slug** is lower case, `%` read as ` pct `, every run of anything but
  ASCII letters, digits and `.` one `-`, with no `-` at either end:
  `Region %Fat (total)` is `region-pct-fat-total`.
- **A reference** to another planned record is `{$ref: <its key>}`, and a
  document's place in a property is `{$doc: <its digest>}`.

## Clinical records

- **A GP system's code** (an EMIS id and its term) is a SNOMED CT
  description id where the id's partition digits (the second and third
  from the end) are `01` or `11`, a SNOMED CT concept where they are `00`
  or `10`, and the GP system's own code (system `emis`) otherwise; no id,
  no coding.
- **An address** is its lines, town, postcode and country, each stripped,
  the empty ones left out; null where nothing is left.
- **A name in capitals** (every cased letter upper case) is made readable,
  each word with its first letter upper case and the rest lower case, the
  abbreviations NHS, GP, UK, CCG, ICB, PCN and HCA kept. A name with any
  lower-case letter is kept as written.
- **A date's precision** is the GP system's `datePart`: `Year` year,
  `YearMonth` month, `YearMonthDay` day, `YearMonthDayTime` time.
- **A phone number** is kept as written, stripped of the invisible marks a
  portal wraps it in (U+200B, U+202A to U+202E). **A web address** with no
  scheme takes `https://`. **A message's topic** is its first non-blank
  line, tags read as spaces, cut to 80 characters with an ellipsis.
- **Identifiers**: an organization's ODS code has the system
  `https://fhir.nhs.uk/Id/ods-organization-code` and the label "ODS code";
  an NHS number the system `https://fhir.nhs.uk/Id/nhs-number`, the label
  "NHS number", and no spaces.
- **An organization** (`clinical/organization`) is keyed by its ODS code in
  the namespace `ods`, so the GP practice the NHS App names and the same
  practice in the hospital's record are one record. It is merged, never
  replaced: its identifiers are the ODS code and any the importer adds, and
  only the fields the importer knows are written, so an importer that knows
  no address never clears one another wrote.

## Documents

A file a record names (a report's PDF, a scan, a JSON attachment, a
clinical document) is stored once by its bytes' digest,
`blob-sha256-<hex>`, under a neutral name: `<provider> <kind>
<date>.<ext>` (`Randox health report 2026-01-19.pdf`). The provider's own
file name is never kept, as it can carry the owner's name and birth date.

| Extension | Media type |
| --- | --- |
| pdf | application/pdf |
| jpg, jpeg | image/jpeg |
| png | image/png |
| gif | image/gif |
| tif, tiff | image/tiff |
| json | application/json |
| html, htm | text/html |
| txt | text/plain |
| csv | text/csv |
| rtf | application/rtf |
| xml | application/xml |
| zip | application/zip |

Any other is `application/octet-stream`. A JSON attachment (a report's own
JSON, kept as its file) is written as canonical JSON: no whitespace, every
object's keys in code-point order, characters as they are rather than
`\u` escapes, numbers as JavaScript prints them. This differs on purpose
from the scripts this replaces, which printed Python's floats (`140.0`
where canonical JSON has `140`), so such a file's digest is new.

## PDFs

Both PDF readers load only with the importer that reads a PDF.

**Positioned text.** pdf.js gives each page's text items: the text, its
`x` from the page's left edge and `y` from its bottom edge to the baseline
(PDF user space), its width, height and font size, and pdf.js's name for
its font. Empty items are left out. A parser reads the items by their
positions, not by another library's text layout. A page's lines are the
items whose baselines lie within two units of each other, read left to
right, the top line first; a document's text is its pages' lines, pages
apart by a blank line. pdf.js reads a copy of the bytes, so the caller's
stay whole for the plan to store, and stops when the read is interrupted.
In a browser its worker is the one Vite bundles. A file pdf.js cannot open
is `Unreadable`.

**Content streams.** A file pdf.js does not open (the body composition
sheet) is read from its raw content streams:

1. Every `stream` keyword not part of `endstream`, followed by an
   end-of-line, starts a stream's data. The data runs for the dictionary's
   `/Length` where it is a number in range, else to the next `endstream`.
2. A stream filtered by FlateDecode is inflated with fflate, which ignores
   whatever follows the deflate data (the checksum, an end-of-line byte
   some generators leave inside the stream) where the browser's
   `DecompressionStream` refuses it; one that does not inflate is left out.
   A stream with no filter is read as it is; one under any other filter
   (an image) is left out.
3. The bytes are read as true Latin-1, one character a byte
   (`TextDecoder("latin1")` is windows-1252).
4. A stream that sets a font (` Tf`) and shows text (`BT`) is read operator
   by operator. Each text block, `BT` to `ET`, is one run: it sits where its
   first `Tm` puts it (the matrix's last two numbers), else its first `Td`
   or `TD` (no `cm` transform is applied; y grows upwards); a block with
   neither is left out. Its text is every string it shows (`Tj`, `TJ`, `'`,
   `"`) in order, stripped. Its font is the one the last `Tf` before its
   first string set, which holds across blocks. An inline image's data is
   skipped.
5. A string's bytes become text by the reader's glyph decoder: Latin-1 by
   default; for a font whose glyph ids follow the standard order at an
   offset, two bytes a glyph, each the character at the id plus the offset,
   U+FFFD where that is not printable ASCII.
6. A file that shows no text is `Unreadable`.

## Testing an importer

Each importer is tested on invented files in its provider's exact shape,
never the owner's, with the plan it must make as a golden fixture.
`testing.ts` gives:

- `inputFromRecord(name, files)`: an input of invented files by path.
- `fakeContext(options)`: the context the import screen hands an importer,
  with the shared catalogues' metrics as `labs/metrics.yaml` and
  `health/metrics.yaml` ship them (all, or the ids named), the unit graph a
  repository holding `labs` and `health` answers (the seeded units and each
  package's own, rooted through their conversions), sources and answers.
- `runResult(effect)`: an effect's outcome with its failure as a value.
- `viewPlan(plan)` and `planDiff(plan, golden)`: a plan as JSON (each
  document by digest, name, type and size), and every difference from the
  fixture by kind, namespace, key and property path, with the records'
  order, empty when there is none.
- `plannedRecord(plan, kind, key)`: one planned record.
- `pdfFixture(pages, options)`: a minimal PDF with the given content
  streams and the standard fonts as `/F1` to `/F4`, deflated or not, with
  or without a trailing byte after the deflate data.
