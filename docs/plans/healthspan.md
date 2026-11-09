# Healthspan: a health app on the web kit

Status: the app built, 2026-11-09, on `main` at `6717954da`: the fourth
draft, approved after four reviews ([Reviews](#reviews)), then corrected
where the build found the server's rules
([0228](../decisions/0228-a-person-an-app-or-an-agent-creates-a-record-under-a-minted-id.md)'s
minted ids). The age score was checked against the old engine on one
owner's data, read-only: over 19 weekly snapshots of WHOOP's own records the
two agree exactly (no age difference above 0.1 years, no contributor
difference above 1%), and with every source, WHOOP ranked first, the same 19
weeks agree and 147 earlier weeks appear from the watch's history. The
[importers](#importers) were proposed the same day, after the owner asked
for them: not built.

## The problem

The owner runs a health dashboard over their substrate in another tree: labs
with target bands and trends, an age score (a pace-of-aging estimate from
sleep, training, steps, VO2 max, resting heart rate and lean mass), the
clinical record, and a list of what needs attention. It is about 15,000
lines of React and it works, but nobody else can use it:

- **It signs in with the repository's password** and mints a full-access
  token, behind an nginx that forwards four routes to one allowlisted
  substrate. The [web kit](../../apps/kit/web/README.md) now gives an app a
  scoped token through the app authorization, an outbox and a record cache.
- **It names one person's data.** Source ids are written into the code
  (`applehealth/<one device id>/apple-watch`, a named lab per body scan
  method), the birth date comes from one provider's patient record by id,
  and 72 of its lab metrics are under the owner's own authority, which no
  other repository holds.
- **The age score reads one vendor's mirror** (the WHOOP provider's
  `cycle`, `recovery`, `sleep` and `workout` kinds), although the WHOOP and
  Apple Health adapters already map into `substrate.reamde.dev/health/sleep`
  and `health/workout`, and resting heart rate, steps and VO2 max into
  observations.
- **It keeps nothing.** What it needs that no record holds (the sex the
  targets are for, a birth date the contact lacks, which record findings were
  seen) sits in `localStorage` or in a deployment's environment.
- **It cannot take data in.** A lab report that no provider delivers has no
  way into the repository but `substratectl`.
- **Its UI is another tree's component library**, read as source.

The owner wants it rebuilt as a Substrate app on the web kit, keeping its
own vocabulary in a package of its own, using the owner's own contact
([0231](../decisions/0231-a-contact-is-a-core-kind-of-the-owners-data-and-a-setting-names-the-owners-own.md))
for who they are, asking for what is missing on a getting-started page,
with a Sources tab that also imports, and in a shape anyone can run against
their own repository.

## What is settled

- **It lives here, as `apps/healthspan/`**, beside Tasks and the template:
  `app.json`, `package/` and `web/`, in the pnpm workspace, on the kit's
  packages by name. It is web only. It is served like Tasks on the web, a
  static build whose `client.json` names the URL it is served at.
- **Its package is `substrate.reamde.dev/healthspan`**, published from
  `package/` through the authority document (`.mise/closures.py`), asked
  for at version 1 and installed by the approval, as Tasks' is
  ([0214](../decisions/0214-an-apps-approval-installs-published-packages-and-declare-and-the-agent-detail-are-retired.md)).
  The app applies no vocabulary itself.
- **It reads the shared catalogues, never a vendor's mirror.** Sleep and
  training come from `health/sleep` and `health/workout`, measurements
  from observations and traces, lab and body composition results from
  `labs/labresult`, from whichever sources the repository holds. A vendor's
  own score (WHOOP's recovery, strain and sleep performance) is shown where
  a source reports it and is never needed.
- **Measured values follow the repository's ranking; sessions follow the
  app's.** A metric read takes the priority policy with no order, so the
  server ranks by each source's `priorities` per metric and takes one
  device's values once through a `sourcelink`
  ([observations](../observations.md)); every reader of those metrics then
  agrees. A read of `health/sleep` or `health/workout` records must name
  its order and reads neither `priorities` nor `sourcelink`
  (`internal/engine/aggregaterecords.go`), so the order of nights and of
  workouts is the app's own, kept on its profile ([Sources](#sources)).
- **No deployment in the tree.** No host, source id, person or lab name of
  anyone's. The demo is synthetic.

## The app

```text
apps/healthspan/
  app.json         the app named once: platforms ["web"], its package, asks, scopes
  README.md        what it is, its screens, running it, serving it elsewhere
  package/         substrate.reamde.dev/healthspan: healthspan.yaml, profile.yaml, import.yaml, README.md
  web/
    src/main.tsx   the manifest and SubstrateApp
    src/App.tsx    the shell: a tab bar on a phone, a sidebar from 768 px; routes in the hash
    src/kinds.ts   the package's two kinds, and the codecs of what it reads
    src/data/      every read the screens make, behind one store (live or demo)
    src/engine/    the age score: curves, weekly snapshots, focus, report (pure, no I/O)
    src/catalogue/ the lab catalogue: entries, panels, targets, cadence, info content
    src/rules/     the attention rules, one pure function each
    src/import/    CSV parsing, column and metric matching, the plan, the runner, undo
    src/ui/        charts, the scale bar, the chart key, cards, readouts
    src/screens/   one file per screen
    src/demo/      the synthetic member
```

| Screen | What it shows |
| --- | --- |
| Getting started | The owner's name from their contact, what the app found (a birth date on the contact or a patient record), a field for each value still missing, the sex the reference curves use (a patient record's value offered, never taken unasked), then where data comes from: the sources already there, and links to connect a provider in the console or import a file. Shown on first open until finished or skipped; Settings opens it again |
| Today | Every finding, most urgent first: results outside the lab's range or off target, important tests overdue, an age score contributor that slipped, record items due (an appointment in 14 days, a repeat medicine due, a referral past its wait, a document or message since the last visit) |
| Labs | One card per catalogue entry in panels, with the filters, the chart key and the display settings of today's app; a metric's history; the reports and one report |
| Healthspan | The age score and pace of aging, what to focus on, the nine contributors, the signals and body readings, each with its scale and suggestion; how the score works; one contributor over time. Without a birth date or sex: the signals, and a link to finish getting started |
| Record | The clinical record from every provider that writes the `clinical` kinds; hidden from the menu while the repository holds none |
| Sources | Every source of measured values by class (watch, ring, scale, lab, service, manual), with what it reports and when last; its rank per group; duplicates linked as one device; the imports, each with its state, Resume and Undo; Import and Enter a result |
| Settings | Profile (birth date and sex, where each came from, an edit for the ones the app keeps), display settings (kept per browser), then the kit's sections |

The screens and their arithmetic are today's app's: the card grids, the one
time axis, the trend fit, the target zones, the hover readouts, the age
score's weekly snapshots and its explainer, the suggestions. What changes
is where each number comes from, the look (the kit's tokens and components,
with the charts and cards ported to them), and the screens added.

## The package

`substrate.reamde.dev/healthspan` version 1, two kinds, both
`purpose: primary` (the owner's data, which Moss and the console show as
such). Nothing in it references outside `core`, `labs` and `observations`.

### `profile`

What the age score, the targets and the session reads need that no other
record holds. One record per repository, under the id `owner`: the kind
declares `ids: {type: named}`, because an app creates a record of a minted
kind only under a minted id
([0228](../decisions/0228-a-person-an-app-or-an-agent-creates-a-record-under-a-minted-id.md)),
and a minted id cannot be the one row every device names.

| Property | Type | What |
| --- | --- | --- |
| `name` | string, required | the profile's title, "Healthspan profile" |
| `contact` | reference to `core/contact` | whose profile it is: the owner's contact when there was one at getting started |
| `birthDate` | date | written only where the contact gives no whole birthday |
| `sex` | enum `female`, `male` | the reference basis: which sex's curves the age score and which targets the labs use. Not a statement of identity; the owner chooses it |
| `nightSources` | repeated reference to `observations/source` | the order `health/sleep` records are read in, first highest |
| `workoutSources` | repeated reference to `observations/source` | the same for `health/workout` |
| `startedAt` | datetime | when getting started was finished or skipped |
| `recordSeenAt` | datetime | the last time the owner marked the record read; a document or message after it is new |

**Creating it.** The kit's outbox treats a create that meets an existing row
as its own retry that landed, and drops what it carried
(`apps/kit/web/local/src/outbox.ts`), which is right for an id one device
minted and wrong for a row every device names. So the profile is never
created through the outbox:

1. Getting started waits until the linked records are listed
   (`useListed`). Where `owner` is there, it saves with a guarded `edit` on
   the version shown.
2. Where it is not, Save sends a PUT at `ifVersion: 0` through the client,
   with the form kept on screen. A 409 reads the row: where every field the
   owner filled holds the same value, it is done; where one differs, the page
   shows both ("Saved from another device") and the owner picks, then an
   `edit` on that version writes the pick.

Every later write is a kit write on the linked record: an `edit` (held to
the version seen; a conflict parks with Keep My Edit and Use Theirs) from
Settings, a `change` (re-read and sent again on top) for `nightSources`,
`workoutSources` and `recordSeenAt`, which no form holds.

### `import`

One run of an importer: a file, a folder or a hand entry taken in, durable
enough to resume on any device and to undo ([Importers](#importers)).

| Property | Type | What |
| --- | --- | --- |
| `name` | string, required | the file's or the folder's name, or "Entered by hand" |
| `importer` | enum `csv`, `manual`, `numan`, `randox`, `dxa`, `nhs`, `mychart` | which importer read it; a new importer is a new value |
| `status` | state | `planned` → `importing` → `imported`; `importing` → `stopped` → `importing`; `planned`, `importing`, `stopped`, `imported` → `undoing` → `undone`; `undone` → `undoing`, to clean up what landed late |
| `runner` | object | the lease of the browser running it: a device id the browser minted, and `until`, a datetime |
| `lab` | string | for a CSV or a hand entry: the lab's name as the file or the owner gave it, which names its source where none exists yet |
| `source` | reference to `observations/source` | the provider the results are from, where one source stands for the import |
| `plan` | blobref | the plan, as JSON: every record the import writes, its key, kind, properties, references by key and revision, and every document's bytes by digest |
| `counts` | object | what the run did so far: `created`, `updated` (a revision-only write counts here, as the scripts count it), `unchanged`, `stale` and `left` (ints) |
| `problem` | string | why it stopped |
| `importedAt` | datetime | when it finished |
| `skipped` | markdown | what the importer left out and why |

The plan is a blob, not properties, so an import is not capped by the
1 MiB a request body may be (the NHS App's record alone is hundreds of
records); the plan as serialized is held to the blob limit, 64 MiB, and
each record write to 1 MiB. The plan names every document by its digest and
holds no document bytes.

**Staging.** Until the import record exists nothing is resumable from
another device, because a token reads only a blob some record it reads
names. So the import is staged from this browser, kept in its IndexedDB
until the record is confirmed: its id is minted here; every document's
bytes go up as blobs (a PUT by digest, which is idempotent); the plan goes
up; then the import record is a PUT at `ifVersion: 0` under the minted id
naming the plan, and a 409 reads the row and takes it where it names the
same plan digest. A page closed before then offers, on its next open in
this browser, to finish staging or to drop it. Once the record exists, any
device resumes it from the plan, and every document it writes is already
up.

Every record an import creates carries `origin` set to the import, but a
source and an organization, which declare no `origin` or are shared
between importers ([Importers](#importers)).

## The owner's details

The owner's contact is the one the core setting
`substrate.reamde.dev/core/ownerContact` names. No scope writes `core`
([0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)),
so the app never writes the contact or the setting.

- **Birth date:** the contact's `dates.birthday` where it has a year; then
  `profile.birthDate`. A `clinical/patient` record's `birthDate` (where
  exactly one patient record is about the owner: none names `about`, or it
  names the owner's contact) is offered on the getting-started page and
  saved to the profile once the owner confirms it.
- **Sex:** `profile.sex` alone. A patient record's `gender` is FHIR's
  administrative gender, not the reference basis, so where it is `female`
  or `male` it is offered as the starting choice and saved only once the
  owner confirms it.

A birth date typed on the getting-started page goes to `profile.birthDate`,
with a link to add it to the contact in the console. Without an owner's
contact the page asks for the birth date and links to the console's step
that sets one. Settings names where each value came from; a value from the
contact is changed in the console, not here.

## Sources

**The list** reads every `observations/source` record, and for each the
metrics it reports with the newest value's time, from one latest read
(`{"latest": true, "groupBy": ["metric", "source"]}`), and the nights and
workouts it holds from a count per source. Lab sources also count their
reports. The WHOOP and Apple Health adapters create a source record only
where it is absent, so what the owner sets on one stands.

**Rank** is per group the screens read together:

| Group | Read as | Rank kept in |
| --- | --- | --- |
| Heart (resting heart rate, HRV), steps and distance, VO2 max, body composition, lab values | metrics, priority policy with no order | each source's `priorities`, for every metric of the group, 1 first |
| Nights | `health/sleep` records, priority policy | `profile.nightSources` |
| Workouts | `health/workout` records, priority policy | `profile.workoutSources` |

The owner drags a group's sources into order. A record read names the
profile's order, then every other source holding such records in a default
order (the class order watch, ring, service, app, then by name), at most the
server's cap. A group with one source shows no rank. The page says that the
metric ranks are the repository's, so every app reading those metrics
follows them, and the session ranks are this app's. A patch replaces a keyed
map whole, so a rank writes the source's `priorities` as read plus the
group's; a rank another device writes for another metric of the same source
at the same moment can be lost to the resend.

**One device** is a `sourcelink`: the owner picks two or more sources that
are the same device (a scale synced to its maker's app and to Apple
Health; a band re-exported through Apple Health), names it, and the app
writes the link. A metric read then takes that device's values once. A
session read does not read links, so linking also moves the linked sources
together in the night and workout orders, the one the owner keeps first and
the other after it.

## Import

The import screen runs one of the [importers](#importers); this section is
the CSV importer's reading, and [The runner](#the-runner) is every
importer's writing.

**A CSV or TSV of lab results**, one row per result:

1. The file is parsed in the browser. Columns are recognised by header
   (date, test or analyte, LOINC code, value, unit, range or low and high,
   flag, lab, accession) and the owner can reassign any of them. A template
   file can be downloaded.
2. Each row is resolved to a catalogue entry (by LOINC code where a column
   holds one, else by the entry's names), then to a metric by the entry and
   the row's unit ([The catalogue](#the-catalogue)). An unmatched name gets a
   picker of the repository's lab metrics, or is left out. A value whose
   unit converts to none of the entry's metrics, through
   `/api/v1/units/convert` (which no scope limits), is left out with the
   reason. A row matching a result the repository already holds (the same
   analyte on the same day, the value equal within the display precision,
   from any source) is marked "may already be here" and left unticked; the
   owner can tick it.
3. The plan is shown: results per report, left out and why. Import writes
   the `import` record with its plan at `planned`.

### The runner

Every step below is idempotent, and every change of the import record is a
patch held to the version last read (`ifVersion`), so two browsers never
both win a transition.

- **The lease.** A browser runs an import only while it holds
  `runner`: it takes it with a guarded patch where `runner` is absent or its
  `until` has passed, for 60 seconds, and renews it every 20 seconds with a
  guarded patch that also writes `counts` so far. It sends a write only while at
  least 20 seconds of the lease remain, and gives up waiting on one 5
  seconds before the lease ends. Giving up stops this browser, not the
  server: a write the server accepted may still commit later, and nothing
  bounds when (What is left, below). A renewal refused (the version moved:
  another browser took over, or the owner pressed Undo) stops it at once. A Web Lock per import keeps
  two tabs of one browser from contending.
- **Setup** (from `planned`): the lab's source is a lab source already
  named as the import's `lab`, else a PUT at `ifVersion: 0` under the id
  derived from the lab's name (lowercased and hyphenated); a 409 reads the
  row and takes it where it is of class `lab`, and otherwise derives the
  next id. Then the import moves to `importing`.
- **Writing** (in `importing`): the plan's records in its order (sources
  and organizations, then each report and its results, then the clinical
  records, a record after every record it names), each a PUT at
  `ifVersion: 0` under its derived id. A 409 reads the row. For a CSV or a
  hand entry, whose ids are the import's own, the row is compared with what
  the plan writes: the same is a write that landed; different stops the
  import as `stopped` with the problem, as any other refusal does. For a
  provider import, whose ids are the provider's, the row is decided by its
  revision ([Writing a provider's plan](#importers)). At the end, one
  guarded patch moves it to `imported` and writes `importedAt` and the
  final `counts`, and clears `runner` and `problem`.
- **Recovery.** Every state but `imported` and `undone` is resumable from
  the import's row on Sources, on any device, once no live lease holds it:
  `planned` runs setup; `stopped` first moves to `importing` in the patch
  that takes the lease, then runs the writing; `importing` runs the writing
  from the start (what landed answers 409 and compares equal); `undoing`
  runs the undo again.
- **Undo** is best-effort reconciliation, not a fence. It takes the lease
  as above (waiting out a live one, which the page shows), moves the import
  to `undoing` in that patch, and waits until the previous lease's `until`
  has passed by 30 seconds, a heuristic for a write still in flight to land
  first. Then it deletes what the import created: for a CSV or a hand
  entry, every report and result the plan names (its ids are the import's
  own, a 404 is already gone); for a provider import, every record the plan
  names whose `origin` is this import, each read first and deleted held to
  the version read, so a record another import created and this one only
  updated is never deleted. Then everything that still points at the
  import through `origin`, read again, and the import moves to `undone`.
  Undo never deletes a source or an organization. A
  result's derived twins in the other unit go with it: `convertlab` deletes
  the derivations of a deleted input, asynchronously.
- **What is left.** No record write carries a cross-record guard, and the
  server bounds receiving a request's body, not applying it. A late write
  onto an id Undo deleted is refused: a PUT at `ifVersion: 0` meets the
  tombstone's version before it could restore it. A late first write of an
  id that had not landed when Undo deleted it (a 404) can still land after
  Undo. Sources therefore counts what points at each `undone` import and,
  where the count is not zero, offers Undo again (`undone` → `undoing`),
  which deletes what landed.

**Enter a result** writes one `labresult`, its report and an import with a
one-row plan, through the same runner.

A PDF, an Apple Health export and hand-entered body measurements are not in
this plan ([Later](#later)).

## Importers

The owner's labs and record reach a repository today through three
scripts of another tree, which read what the owner downloads from Numan
and the fetchers write from Randox, the NHS App and MyChart, and write
them under that one owner's ids and metrics. The owner wants them as the
app's own, one interface and an importer per provider, so anyone with the
same downloads can take them in. Their reading is specified, rule by rule,
from the scripts and the owner's files; the app reimplements it in
TypeScript. Nothing of the scripts is carried over but what they read and
what they mean.

**One interface.** An importer is a pure reading of what the owner hands
it into a plan, and every importer's plan is written by the one runner:

```ts
interface Importer {
  id: ImporterId            // "csv" | "numan" | "randox" | "dxa" | "nhs" | "mychart"
  name: string              // what the import screen offers: "Numan export"
  accepts: Accepts          // a file of a type, files, or a folder (or its zip)
  detect(input: Input): Effect<Match, never>        // how sure it is this input is its own
  read(input: Input, ctx: Context): Effect<Plan, ImportError>
}
```

`Context` is what reading needs from the repository: the catalogue's
resolution against the repository's metrics, the unit graph, the owner,
the sources, and the answers to the importer's questions (below). `Plan`
holds records by kind, each with a key, its properties, its references to
other records by key, its revision and its documents; and what was left out
and why. The import screen offers the importers whose `detect` matches what
was dropped, the best first, then shows the plan before anything is written.

**Effect, and not Foldkit.** The importers are written with
[Effect](https://effect.website): each provider's files are decoded by an
Effect `Schema`, so a field that is not what the provider promised is an
error naming the field, not a value that is quietly wrong; `read` is one
typed program whose errors are values; the PDF and archive readers run as
interruptible effects the screen can cancel. Effect stays behind the
interface: the screen calls `Effect.runPromise`, the kit and React never
see it, and it loads with the import screen alone. Foldkit is a UI
framework on Effect in Elm's architecture: adopting it would replace React
and with it the web kit the app stands on, so it is not used.

**The importers.**

| Importer | The owner hands it | It writes |
| --- | --- | --- |
| `csv` | a CSV or TSV of lab results | lab reports and results (as today) |
| `numan` | Numan's JSON export, one or several (each export repeats the older tests; a test is read from the export holding its newest version) | the Numan source, one report per test, its results with the lab's and Numan's ranges |
| `randox` | the folder `randoxctl` writes (or its zip) | the Randox source, one report per visit with its health report PDF, its results with their bands; and each visit's Tanita body composition sheet (a PDF read from its raw content stream) as a report of its own |
| `dxa` | GE Lunar scan printouts (PDF), and a hand-transcribed trend page (JSON beside its photo) | the scanning centre's source, one report per scan with its printouts, the whole-body and regional results (regions as each result's `channel`), the trend table's earlier scans as reports of their own |
| `nhs` | the newest run folder `nhsctl` writes (or its zip) | the GP practice's source and its test results as lab reports and results; the clinical record: organizations, the patient registration, consultations, conditions, allergies, immunizations, medicines and their dispensing, appointments, referrals, documents with their files, messages |
| `mychart` | the folder `mychartctl` writes for one MyChart portal (or its zip) | the hospital's source and its lab results; its clinical record: the patient registration, appointments with their visit documents, letters, medicines, allergies, conditions, immunizations, messages with their images, imaging reports as documents |

A provider's parts that depend on each other are one importer: the Tanita
sheet needs its Randox visit, the transcribed DXA page shares the PDF
scans' trend table, and the NHS App's and MyChart's clinical records name
their own lab reports. A record of another importer is named by a key
both derive the same way (an organization by its ODS code), and a
reference to a record no import has written is left out, never written
dangling.

**What an importer asks.** What a provider's files do not say, and the
scripts wrote in by hand, the import screen asks once and keeps on the
record it names: the scanning centre's name and its place (the DXA
printouts name neither), a hospital's name and ODS code where its MyChart
export does not carry them. A provider's own time zone is the importer's
(the NHS, MyChart's UK portals and Randox are in `Europe/London`); a
scanning centre's is asked with the centre. A document's file is stored
under a neutral name (`<provider> <kind> <date>`), never the file name the
provider gave it, which can carry the owner's name and birth date.

**Ids, and re-imports.** A provider import names each record by
`DerivedMintedID` of `healthspan/<namespace>/<kind>/<key>`, the minted
shape an app may create under
([0228](../decisions/0228-a-person-an-app-or-an-agent-creates-a-record-under-a-minted-id.md)):
the derivation hashes the whole string, so a key of any length gives an id
of fixed length. The namespace is the provider's account where one
provider can have several: `numan`, `randox`, `dxa/<the centre's key>`,
`nhs`, `mychart/<the portal's host>`. The key is the one the scripts
compose from the provider's data, which they made stable across exports:

| Importer | Record | Key |
| --- | --- | --- |
| `numan` | report | the test's short id |
| | result | `<short id>/<analyte code>`; an analyte seen twice in a test keeps its first value, so the code is unique within the test (Hurdle rows share an `upstreamId`, so it is not the key) |
| `randox` | report | the sample id |
| | result | `<sample>/<test name>` |
| | Tanita report | `<sample>/bia` |
| | Tanita result | `<sample>/bia/<row name>/<channel>` |
| `dxa` | report | the scan's date, `YYYY-MM-DD`, from a printout or a trend table alike, so a scan first known from a later scan's trend table is the same report once its own printout arrives |
| | result | `<date>/<measure>/<channel>` |
| `nhs` | test report | the EMIS test id |
| | test result | `<test id>/<term>` |
| | patient | `patient` |
| | consultation | a hash of its date, clinician and place, with `-2`, `-3` for the second and later consultation of one hash in the record's order (the scripts' key: an EMIS entry id is not on every consultation) |
| | problem, allergy, immunisation, referral, acute medicine | the scripts' hash of the record's identifying fields |
| | repeat course | the course id, else the scripts' hash |
| | dispensing | `<EPS barcode>/<item position>` |
| | appointment, document, message | the NHS App's own id |
| `mychart` | lab report | MyChart's result key, whole |
| | lab result | `<result key>/<component id, else name>` |
| | patient | `patient` |
| | visit (appointment, encounter) | the CSN |
| | document, condition, allergy, message | MyChart's own id, under its kind |
| | medicine | its id, else its name |
| | dispensing | `<medicine id>/<date>` |
| | pharmacy | `pharmacy/<id>` |

A hash here is SHA-256 over the parts, each written as its JSON text,
joined by U+001F: fresh copies need no agreement with the scripts' hashes.
The one key whose stability rests on order is a consultation's suffix: a
later export that lists two consultations of one date, clinician and place
in the other order swaps them, and each is updated with the other's
content, which the owner sees as the record it is. A CSV has no provider
key, so its records keep the import's own ids, as today.

**Sources and organizations** are shared, and written once.

- A source (`observations/source`, which declares no `origin` or
  `revision`) is `healthspan/source/<namespace>`, created where absent with
  its name and class from the data or from the import screen's questions,
  and never written again: the owner's later edits of its name and its
  ranks stand, and Undo never deletes it, as the scripts never rewrite a
  source.
- An organization is `healthspan/ods/<ODS code>`, one id whatever importer
  meets it, so the NHS App's GP practice and MyChart's naming of the same
  practice are one record. It is merged, never replaced: its identifiers are
  the union of every importer's (by system and value), and every other
  field is written only where the stored record has none, so an importer
  that knows no address never clears one another wrote. Undo never deletes
  one.
- A report's `review` is written when the report is created and never
  again, as the scripts do: the owner's review stands.

**Writing a provider's plan.** The runner's lease, recovery and Undo are
every importer's. What a provider import adds is the decision on a record
that is already there, the scripts' `decide`, by the record's `revision`,
which is each provider's:

| Records | Revision |
| --- | --- |
| Numan's | the test's `updated_at`, else its `created_at` |
| Randox's and their Tanita sheets' | the visit's `lastUpdated` (London), else the newest run's start in the folder's manifest |
| DXA's | the newest scan date of the import, at UTC midnight, for every DXA record (a later trend table can restate an earlier scan) |
| The NHS App's | the run's start; a message, its own `version` |
| MyChart's | the newest run's start |

- absent: created, `origin` the import, counted `created`;
- a tombstone (deleted, not yet collected): restored by a PUT held to the
  tombstone's version, `origin` the import, counted `created`;
- present with the same content and a revision no lower than the plan's:
  `unchanged`, nothing written;
- present with the same content and a lower revision: the revision alone
  raised, counted `updated`, as the scripts count it;
- present with other content and a revision no higher than the plan's:
  `updated`, a PUT that names every property the importer maintains (a
  value the provider no longer gives is written as null, since a PUT merges
  and never prunes), `origin` left as it is;
- present with a higher revision: `stale`, not written: an older export
  never overwrites a newer one, as the server's own `stale_revision` would
  refuse it.

Every write is held to the version read; one the version moved under (409
or 412) reads again and decides again, three times at most, then stops the
import with the problem. Content is compared as the scripts compare it:
references and documents by their target and digest, quantities by number
and unit, instants as instants, a property neither side holds as equal. A
document's bytes are already up when the records are written (staging).

**Undo** deletes what the import created, and only that ([The
runner](#the-runner)): what it updated stays updated, and the screen says so
before the owner confirms. Undoing an earlier import of a provider takes
with it the records a later one only updated; importing that later export
again writes them anew.

**The owner's existing records.** The owner's repository holds about 1,600
records the scripts wrote, under their own ids and 143 metrics of the
owner's own authority. The owner chose fresh copies: the importers never
look for them, the owner's first import writes a new set beside them, and
once it is checked the owner deletes the old set, with a one-off tool of
the owner's tree that lists them by the scripts' sources and id prefixes
and deletes only after a dry run. The scripts retire with it.

**Reading PDFs.** The DXA printouts' tables are read from pdf.js's
positioned text, by a parser written for positions, not the scripts' reading
of another library's text layout. The Tanita sheet, which neither library
opens whole, is read as the scripts do: its content streams inflated (with
fflate, which tolerates the stream's trailing byte that the browser's
`DecompressionStream` refuses) and its text placed by the operators'
positions. Both load with the importer that needs them.

**Checking an importer.** What each importer reads and writes is written
down, rule by rule, in `apps/healthspan/docs/importers/` (one page per
importer and one for what they share), drawn from the scripts and kept free
of any person's or practice's names: the port is held to those pages, not
to the scripts. Each importer has fixtures of invented files in its
provider's exact shape, with the plan it must make. On the owner's own
files, read only on the owner's machine and never committed, every
importer's plan is compared with what the scripts write for the same files
(the scripts' offline run, as a reference): the same reports, results,
values, units, ranges, flags and records, by provider key. These differ on
purpose, and nothing else may:

- ids and metrics, which the importers take from minted ids and the shared
  catalogues;
- a JSON attachment's bytes (a report's own JSON, kept as its file), written
  as canonical JSON (keys sorted, numbers as JavaScript prints them), where
  the scripts print Python's floats (`140.0`);
- a document's text read from a PDF, by pdf.js rather than the scripts'
  library: compared with spacing normalized, and listed rather than failed;
- the NHS App's historic test results, which the scripts ignore when the
  record says it holds them all only in part, and which `nhs` reads (the
  year-by-year results the fetcher already downloads);
- the NHS App's prescription statuses written as one word (`NotTracked`),
  which `nhs` maps to the spaced words the scripts expect, where the
  scripts left them unknown;
- references to records no import wrote, which the scripts write dangling
  and the importers leave out.

What the scripts do not write, the importers do not either, for now: a
medication statement for a medicine prescribed elsewhere, and the owner's
own messages (both open in the owner's other tree).

## The catalogue

Today's catalogue keys each biomarker on a metric id, and 72 of those ids
are under one owner's authority. Each entry instead carries its LOINC codes,
its names, its display unit, its panel, its target bands (in the display
unit), its trend goal, its cadence and its info content, and no metric id.

**Resolving an entry.** The app reads every `observations/metric` record of
category `labs` or `body` and every `labs/analyte` record. An entry's
metrics are those holding one of its codes, then the `labs/` or `health/`
metric of its name. They fall into unit forms: the metrics whose units
convert to the display unit through ordinary conversions (the display
form, however many metrics), and the others (a mass form beside a molar
one), which convert only through the analyte's `molarMass` or formula.

**Reading an entry** reads its results in every form, then makes one point
per specimen:

1. A result and its derivations (`derivedFrom`, which `convertlab` writes on
   each twin in the analyte's other form) are one specimen. Its point is the
   member in the display form: the original where it is in that form, else
   the twin.
2. A specimen with no member in the display form (an analyte with no
   conversion, or a twin not written yet) is converted in the app through
   the analyte record where one converts it, and otherwise shown apart, in
   its own unit, beside the card's chart.
3. Two specimens are one only by identity: the same `report`, or the same
   report `accession` from the same lab, and the same analyte. Two results
   that merely agree (one day, one value) stay two points; the import's
   "may already be here" mark is where that duplicate is caught.

**Importing** picks the metric of the entry's form whose unit the row's
unit converts to; `convertlab` then writes the twin.

**The catalogues grow to cover the entries.** An import needs a metric to
point at, and a repository that never had one has none:

- `labs` version 2 adds a metric record for each lab analyte the catalogue
  names that `labs` lacks (67 today: renal, lipid, thyroid, hormone, liver,
  iron, blood count, electrolyte and other markers), each with its LOINC
  codes, unit, precision and description, and `analyte` records with their
  conversions where a second unit form is common (testosterone, vitamin D,
  B12, folate, creatinine, urea, uric acid, calcium, phosphate, magnesium,
  iron, insulin). Computed scores (cardiovascular risk, FIB-4) and urine pH
  are left to a later version.
- `health` version 3 adds the five body metrics it lacks: `fat_free_mass`,
  `lean_mass`, `visceral_fat_rating`, `waist_circumference` and
  `waist_hip_ratio`.
- The importers name 71 more measures no shared metric holds (the owner
  keeps them under their own authority today): urine strip results, bone
  density and its T and Z scores, coagulation, immunoglobulins and the
  rest of the providers' panels, and the GP's own measures. Both versions,
  unreleased, grow to hold them, each with its LOINC codes where one
  exists. A DXA or Tanita region is not a metric of its own: it is the
  `channel` of the whole-body metric's result (`trunk`, `left_arm`), as
  `labresult` declares.

The final declarations are held by `mise run kinds:check`. The app asks for
`labs` at 2 and `health` at 3.

## The age score from the shared kinds

The engine keeps its curves, its weekly snapshots and its report, and takes
a new input built from the shared kinds instead of the WHOOP mirror. Each
rule below is chosen; "kept" means the same number on WHOOP data, and every
change is listed under the table and in "How the score works".

**Which records count.**

- A `health/sleep` record counts only when it carries stage totals
  (`light`, `deep` or `rem`). The WHOOP adapter writes them only for a
  scored sleep (`bundle.yaml`, the sleep mapping's `if scored`), so this is
  today's "scored sleeps" rule.
- A `health/workout` counts toward strength minutes only when it carries a
  measure of effort: `zoneDurations`, `strain` or `averageHeartRate`. WHOOP
  writes all three from the score, so an unscored WHOOP workout carries none
  and is left out, as today; a workout entered by hand with nothing
  measured, from any source, is left out the same way.

**Which night.** A sleep's night is the local day before the day it ended
on, from `endsAt` and `utcOffsetEnd` (WHOOP writes the record's one offset
into both). A resting heart rate summary's night is the night of the
sleep of the same source, counted or not, that its span overlaps most (the
WHOOP adapter spans a recovery over the sleep it scores); one that overlaps
no sleep (WHOOP's span when a recovery has no sleep, which is its cycle's)
is the local day its span starts on when it starts at noon or later, else
the day before. So a recovery moves with its sleep wherever the sleep's
night differs from its cycle's.

| Contributor | Today, from the mirror | On the shared kinds | |
| --- | --- | --- | --- |
| Hours of sleep | each scored sleep's light, deep and REM time, naps included, on its cycle's night | each counted sleep's `light`, `deep` and `rem`, naps included | kept, but for the night (below) |
| Sleep consistency | `sleepConsistencyPercentage` of scored sleeps that are not naps | `scores.sleepConsistency` of counted sleeps that are not naps (`isNap`) | kept; a repository whose sources write none has no consistency contributor, with the reason |
| Zone 1–3, zone 4–5 minutes | each scored workout's zone durations | `zoneDurations` | kept |
| Strength minutes | scored workouts whose WHOOP sport is in the white paper's strength list | counted workouts whose `sport` is in the generic strength set (`strength`, `yoga`, `pilates`) or whose `sportSource`, normalized, is in the WHOOP list | kept for WHOOP; Apple Health's traditional and functional strength training (`sport: strength`) now count |
| Steps | Apple Health steps, daily sums | the daily sum of `health/steps`, priority policy | kept |
| VO2 max | Apple Watch readings | `health/vo2_max` readings over two years, priority policy | kept |
| Resting heart rate | each scored recovery's resting heart rate, on its cycle's night | the `health/resting_heart_rate` summary, on its night (above) | kept, but for the night (below) |
| The 21-in-31 gate (the score updates only with 21 recoveries in the 31 nights before a week) | nights with a scored recovery, on the cycle's night | nights with a resting heart rate summary, on its night | kept, but for the night (below); a source with a daily resting heart rate (Apple Watch) passes it on the days it reports one |
| Lean body mass | two named labs' scans | body composition results (`health/fat_free_mass` over the same scan's `health/body_mass`, or 100 less `health/body_fat_fraction`) in `labs/labresult`, priority policy | kept, without the names |
| Days worn | a night with a scored sleep or recovery | a night with a counted sleep or a resting heart rate summary | kept, but for the night (below) |

**What changes on purpose.** A sleep's night comes from its own end, not
its cycle's day, so a sleep that ends after the next cycle has started (a
late nap, a second sleep after waking) moves to the night it ended in, and
with it the recovery spanning it: that night's resting heart rate, its
place in the 21-in-31 gate and its day worn move too.
Strength minutes count other sources' strength workouts. Consistency is
WHOOP's own score, so a repository without one has no consistency
contributor rather than one computed some other way.

**The checks.** The unit fixtures are written in the shape the WHOOP and
Apple Health adapters write (their mappings in
`kinds/providers.substrate.reamde.dev/`), each with the expected
contributor values chosen above: naps, unscored sleeps and workouts, a
sleep ending after a cycle starts with the recovery spanning it (its
resting heart rate, gate night and day worn asserted), a recovery over an
unscored sleep, a recovery spanning a cycle, a time zone
change, uncommon WHOOP sports, and Apple Health's traditional and
functional strength training. Then the owner's own data: the old engine on
the mirror and the new one on the shared kinds, both read from one
repository through a read-only token, every weekly snapshot of the last
year compared, and every difference above 0.1 years or one unit of a
contributor traced to a rule above in the pull request. A difference no
rule explains is a bug. Nothing from that run is committed.

The score is described as an estimate the app computes from published
reference curves, with the sources linked, not as any vendor's product.

## Its access

`app.json`'s asks and scopes, in the manifest's order:

```json
"asks": [
  {"package": "substrate.reamde.dev/healthspan", "atLeast": 1},
  {"package": "substrate.reamde.dev/health", "atLeast": 3},
  {"package": "substrate.reamde.dev/labs", "atLeast": 2},
  {"package": "substrate.reamde.dev/clinical", "atLeast": 1}
],
"scopes": [
  "write:substrate.reamde.dev/healthspan/*",
  "write:substrate.reamde.dev/labs/labreport",
  "write:substrate.reamde.dev/labs/labresult",
  "read:substrate.reamde.dev/labs/*",
  "write:substrate.reamde.dev/observations/source",
  "write:substrate.reamde.dev/observations/sourcelink",
  "read:substrate.reamde.dev/observations/*",
  "read:substrate.reamde.dev/health/*",
  "write:substrate.reamde.dev/clinical/*",
  "read:substrate.reamde.dev/core/contact",
  "read:substrate.reamde.dev/core/setting"
]
```

`clinical` is written by the NHS App's and MyChart's importers, so it is
asked for and installed with the approval; the Record tab shows once a
record is in it. `core/setting` is an internal kind, which an exact scope
reaches and a glob does not; it is read for the one setting that names the
owner's contact, but reaches every setting record (none holds a secret,
`core/setting.yaml`), and the README and the getting-started page say so.

## The kit

Two additions every health app needs, made in the kit rather than the app:

- **`SubstrateClient.aggregate(filter, body, signal)`** in `@substrate/client`,
  typed after [the aggregate read](../api.md#the-aggregate-read): the
  request (metric or metrics, latest, range, bucket, reducers, `from`,
  `statistic`, sources, groupBy, budget) and the answer (units, bucket, rows
  with group, values, sources, count, head). Tested against the fake
  substrate, which learns to answer a canned aggregate.
- **`useRead(key, read, {kinds, staleAfter})`** in `@substrate/react`: a read
  through the session's client that is not a linked record (an aggregate,
  a list of a kind the app does not keep).
  - Its cache belongs to the session: a sign-out or another session drops
    it, and each read gets an `AbortSignal` aborted on that, on unmount and
    when a newer read of its key starts.
  - It is read again when one of `kinds` changes: through a write the outbox
    lands; through `invalidate(kinds)`, which the app calls after a write of
    its own through the client (an import step, a rank); or through the
    change feed.
  - **The feed.** One follower in the provider (`follow` from
    `@substrate/client`) watches the kinds the mounted reads name. A scoped
    token's list carries no `head` or `generation` (`internal/api/scoped.go`
    strips them), so the bookmark is the watch's own: the follower starts
    from the live head (`from` null), and a read it covers starts only once
    the watch has `opened`, so a change after the read began is always
    delivered. When the set of kinds grows it restarts from the last cursor
    it saw, not the live head. On `relist` (the history moved on) it opens a
    fresh watch first, and only once that has `opened` invalidates every
    followed read; on `ended` it stops, and the reads fall back to
    `staleAfter`.
  - It follows only kinds the repository holds: the kinds of the packages
    the app asks for, which the approval installs, and an optional one
    (`clinical`) only once a list of it has answered, so a kind the server
    does not know never ends the follower.
  - Sample kinds that change by the minute (`observations/observation`,
    `observations/trace`) are not followed: a read of them is read again
    when the page comes back in front after `staleAfter` (five minutes by
    default), and on Refresh.

The client keeps the transport and React the hook, as the layering requires
(`apps/eslint.config.js`).

And `app.json` admits `web` as a platform: `lint:apps` holds that an app
naming it has a `web/` folder, counts `web/` as a UI folder and checks its
icon, and the release, which ships `apple/` and `android/` builds, skips it.

## The demo

`?demo` opens the app without a substrate: a synthetic member (three years
of labs from three invented labs, DXA and bioimpedance scans, nights and
workouts from two invented sources, a patient record), with `?today=` to pin
the date. The store has a demo implementation beside the live one, so every
screen runs on it; writes in the demo change the demo's memory alone. The
screenshot script captures only the demo.

## Tests

- The package's kinds held to the codecs (`kinds.ts` names only what the
  YAML declares), as the template's test does.
- The engine's tests, carried over, and the adapter-shaped fixtures above.
- Catalogue resolution: by code, by name, two metrics of one form, a mass
  and a molar form with and without derived twins, a twin not written yet,
  an analyte with no conversion, two results of one report under two
  metrics (one point), two equal results of two reports (two points).
- Profile: each source of each value, a yearless birthday, two patient
  records, no owner's contact, the 409 on create with the same and with
  different values.
- Import: header detection, every column form of a range, comparators
  (`<5`), units that do not convert, the split at 2,000 rows, the derived
  ids, a lost answer (409 that compares equal), a conflict (409 that does
  not), the "may already be here" mark, the lease taken, renewed, lost and
  waited out, resume from each state (`planned`, `importing`, `stopped`
  through `importing`, `undoing`), a second browser resuming during and
  after Undo, undo of a partial import, a late write onto a tombstoned id
  (refused), a delayed first write after Undo (lands, counted, Undo again
  from `undone`).
- Sources: the priorities written for a group, a link of two sources, the
  record order with ranked and unranked sources.
- `useRead`: invalidation by the outbox, by `invalidate`, by the feed, and
  by `staleAfter`; a read held until the watch has `opened` (a change
  between the read and the stream is seen), the restart from the last
  cursor, `relist` reopening before it refetches, an optional kind left out
  until it answers; the abort on sign-out; all of it under a scoped app
  token in the fake substrate.
- Today's rules, filters, chart maths, scales and suggestions, carried over.
- Every importer: its fixtures of invented files in the provider's exact
  shape and the plan each must make; detection between importers (a
  Randox folder is not taken for MyChart's); every key of the identity
  table, a DXA scan known first from a trend table and then from its own
  printout landing on one report, two consultations of one hash; the
  decision on a record already there (created, a tombstone restored,
  unchanged, a raised revision only counted updated, updated with a value
  the provider dropped written null, stale, a version moved under the write
  decided again); a source written once and an owner's edit of it kept;
  one organization from the NHS App then MyChart and the other way round,
  identifiers merged, an address kept; a report's `review` kept; staging
  interrupted before the record and finished on the next open; Undo of the
  later and of the earlier of two imports of one provider, deleting only
  what each created and never a source or an organization. On the owner's
  machine only, every importer's plan against the scripts' offline output
  for the owner's files, within the named differences.
- End to end on a local substrate (`mise run dev:up` with the authority
  document from this tree): connect, approve (the three packages install),
  getting started, import the template file, the cards and the score appear,
  rank two sources, undo the import.

## Serving it

`SUBSTRATE_WEB_URL=https://<host>/<path>/ pnpm build` in `apps/healthspan/web`,
as Tasks: the build is static, routes in the hash, and needs `connect-src
https:` in any CSP. The README says how to serve it anywhere. The owner's
deployment publishes the closures with the next apps site publish and serves
the build beside Tasks on the web.

## Steps

The importers come before the first release, so the package is first
published with the `import` kind they need:

1. The importers' pages under `apps/healthspan/docs/importers/`; the
   `import` kind's plan as a blob with staging, the runner's provider
   decision and its Undo by `origin`, the CSV and hand entry moved onto the
   interface.
2. `labs` and `health` grow to the 71 measures.
3. In parallel: `numan`; `randox` with the Tanita sheet; `dxa`; `nhs`;
   `mychart`.
4. The parity runs on the owner's files, the end-to-end run, the README.

Before them, as built:

One pull request, built in this order:

1. The kit's `aggregate`, `useRead` and the `web` platform, with tests.
2. `labs` version 2 and `health` version 3.
3. `apps/healthspan`: `app.json`, the package, the shell on the template,
   the store, the demo's skeleton, the shared UI.
4. In parallel: the engine on the shared kinds with Healthspan's screens;
   the catalogue with Labs, Today and the rules; Record, the profile,
   Getting started and Settings; Sources and Import.
5. The parity run on the owner's data, the end-to-end run, the README,
   `apps/README.md` and the kit's README naming the app,
   `docs/health-apps.md` linking it.

## Later

- Thiva, once the owner has a file of its results to read.
- Offering to delete what a newer export of a provider no longer holds
  (the scripts never delete, and neither do the importers yet).
- A medication statement for a medicine prescribed elsewhere, and the
  owner's own messages, once the owner settles them.

- Importing a PDF lab report, through an agent the package ships.
- Hand-entered body measurements (weight, blood pressure), which need a
  write scope on `observations/observation`.
- The owner's own target bands as records of a `target` kind.
- Session reads that follow `priorities` and `sourcelink` on the server, so
  the night and workout orders can leave the profile.
- A service worker, so the app opens with no network.

## Reviews

- **Astra (gpt-6-astra), first draft: rework.** Taken: a session read needs
  an explicit order and reads neither `priorities` nor links, so sessions
  rank on the profile; the parity claim, replaced by a contributor-by-
  contributor contract and a check on real data; the fixed-id create, which
  the outbox would have merged silently, now created through the client
  with a compare; mass and molar forms, read in one form with derived twins
  left out; the import's durable plan, states, derived report ids, the
  compare on 409 and an undo that stops the runner first; `useRead`'s
  invalidation by the feed and by direct writes, with aborts per session;
  `health` version 3 for the body metrics; a patient record's gender
  offered, never taken. Answers to the questions: one profile, kept by the
  app; priorities and links are the repository's to set, said plainly; the
  analytes in `labs` and the body metrics in `health`; the exact
  `core/setting` read is admissible, and disclosed.
- **Astra, second draft: rework.** Resolved there: the session order, the
  profile's create, the health version, the gender. Taken now: Undo fenced
  by a lease with bounded requests and a wait past the previous lease, every
  transition guarded, and a count that offers Undo again for what the time
  fence cannot stop; recovery from every state; strength as the union of the
  generic sports and WHOOP's list, so Apple Health's strength training
  counts; which sleeps and workouts count and which night a sleep and a
  resting heart rate fall on, each chosen and fixtured, with the changes
  named; every unit form read and one point per specimen by `derivedFrom`,
  report and accession, never by equal values; the follower's bookmark
  before the read, its cursor kept across restarts, `relist` and `ended`,
  and only kinds the repository holds.
- **Astra, third draft: rework.** Taken: the watch's `opened` as the
  bookmark, because a scoped token's list carries none; Undo described as
  best-effort with the 30 seconds a heuristic, since the server bounds a
  request's body and not its commit, with fixtures for a tombstoned id
  (refused) and a delayed first create (lands, then Undo again); a
  recovery, its gate night and its day worn moving with the sleep it spans,
  named as a change; `stopped` → `importing` before a resumed write and
  `undone` → `undoing` declared.
- **Astra, the importers' first draft: rework.** Taken: sources are written
  once and organizations merged, neither carrying `origin` nor undone; a
  report's `review` written once; Undo for a provider import deletes only
  what the import created, by `origin`, read and held to its version, and
  the promise that a later import's Resume rewrites what an earlier Undo
  took is gone; a tombstone restored by a PUT held to its version; the
  identity table, with Numan's analyte codes, the consultations' hash and
  suffix, DXA keyed by the scan's date so a trend-only scan and its
  printout are one report, MyChart's key whole; organizations under one
  ODS namespace, identifiers merged and no field cleared; revisions per
  provider, a revision-only write counted `updated`, a moved version
  decided again; staging until the import record exists, documents up
  before it, the plan holding digests and no bytes; the importers' pages in
  the tree and the differences from the scripts named, with the NHS App's
  historic results and prescription statuses taken in; `counts` in place of
  `results` through the runner.
