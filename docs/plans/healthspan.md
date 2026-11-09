# Healthspan: a health app on the web kit

Status: proposal, 2026-11-09, against `main` at `6717954da`, fourth draft
after three reviews ([Reviews](#reviews)). Nothing here is built yet.

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
record holds. One record per repository, under the id `owner`.

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

One file or one hand entry taken in, durable enough to resume on any
device and to undo.

| Property | Type | What |
| --- | --- | --- |
| `name` | string, required | the file's name, or "Entered by hand" |
| `format` | enum `csv`, `manual` | how it came in |
| `status` | state | `planned` → `importing` → `imported`; `importing` → `stopped` → `importing`; `planned`, `importing`, `stopped`, `imported` → `undoing` → `undone`; `undone` → `undoing`, to clean up what landed late |
| `runner` | object | the lease of the browser running it: a device id the browser minted, and `until`, a datetime |
| `source` | reference to `observations/source` | the lab it is from |
| `rows` | repeated object | the normalized plan, one entry per result: its key, metric, time, value or text, unit, comparator, range, flag, and the key of its report |
| `reports` | repeated object | one entry per report: its key, its date, its lab's accession |
| `results` | int | results confirmed written |
| `problem` | string | why it stopped |
| `importedAt` | datetime | when it finished |
| `skipped` | markdown | the rows left out and why |

At most 2,000 rows a record (a request body is capped at 1 MiB): a larger
file becomes several imports, split between reports. Every id an import
writes derives from the import's id and a key in its plan
(`<import id>/report/<key>`, `<import id>/<key>`), and every `labreport` and
`labresult` carries `origin` set to the import, so the plan alone says what
the import writes, and what points at it says what it wrote.

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
follows them, and the session ranks are this app's.

**One device** is a `sourcelink`: the owner picks two or more sources that
are the same device (a scale synced to its maker's app and to Apple
Health; a band re-exported through Apple Health), names it, and the app
writes the link. A metric read then takes that device's values once. A
session read does not read links, so linking also moves the linked sources
together in the night and workout orders, the one the owner keeps first and
the other after it.

## Import

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
  guarded patch that also writes `results`. It sends a write only while at
  least 20 seconds of the lease remain, and gives up waiting on one 5
  seconds before the lease ends. Giving up stops this browser, not the
  server: a write the server accepted may still commit later, and nothing
  bounds when (What is left, below). A renewal refused (the version moved:
  another browser took over, or the owner pressed Undo) stops it at once. A Web Lock per import keeps
  two tabs of one browser from contending.
- **Setup** (from `planned`): the lab's source is a PUT at `ifVersion: 0`
  under `lab/<the lab's name, lowercased and hyphenated>`; a 409 reads the
  row and takes it where it is of class `lab`, and otherwise suffixes the id.
  Then the import moves to `importing`.
- **Writing** (in `importing`): each report, then each of its results, in
  plan order, each a PUT at `ifVersion: 0` under its derived id. A 409
  reads the row and compares it with what the plan writes: the same is a
  write that landed; different stops the import as `stopped` with the
  problem, as any other refusal does. At the end, `imported`, `importedAt`
  and `runner` cleared, in one guarded patch.
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
  first. Then it deletes every report and result the plan names (a 404 is
  already gone), then everything that still points at the import through
  `origin`, reads that list again, and moves the import to `undone`. A
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
  {"package": "substrate.reamde.dev/labs", "atLeast": 2}
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
  "read:substrate.reamde.dev/clinical/*",
  "read:substrate.reamde.dev/core/contact",
  "read:substrate.reamde.dev/core/setting"
]
```

`clinical` is read, not asked for: the Record tab appears once a provider
installs it. `core/setting` is an internal kind, which an exact scope
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
