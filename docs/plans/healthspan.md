# Healthspan: a health app on the web kit

Status: proposal, 2026-11-09, against `main` at `6717954da`. Nothing here is
built yet.

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
- **Which source wins is the repository's, not the app's.** The app reads
  under the priority policy without naming an order, so the server ranks by
  each source's `priorities` and takes one device's values once through a
  `sourcelink` ([observations](../observations.md)). The Sources tab is
  where the owner sets both, and every other reader then agrees.
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
    src/import/    CSV parsing, column and metric matching, the plan, the writes, undo
    src/ui/        charts, the scale bar, the chart key, cards, readouts
    src/screens/   one file per screen
    src/demo/      the synthetic member
```

| Screen | What it shows |
| --- | --- |
| Getting started | The owner's name from their contact, what the app found (a birth date on the contact or a patient record, the sex on a patient record), a field for each value still missing, then where data comes from: the sources already there, and links to connect a provider in the console or import a file. Shown on first open until finished or skipped; Settings opens it again |
| Today | Every finding, most urgent first: results outside the lab's range or off target, important tests overdue, an age score contributor that slipped, record items due (an appointment in 14 days, a repeat medicine due, a referral past its wait, a document or message since the last visit) |
| Labs | One card per metric in panels, with the filters, the chart key and the display settings of today's app; a metric's history; the reports and one report |
| Healthspan | The age score and pace of aging, what to focus on, the nine contributors, the signals and body readings, each with its scale and suggestion; how the score works; one contributor over time. Without a birth date or sex: the signals, and a link to finish getting started |
| Record | The clinical record from every provider that writes the `clinical` kinds; hidden from the menu while the repository holds none |
| Sources | Every source of measured values by class (watch, ring, scale, lab, service, manual), with what it reports, how many values and when last; its rank per group of metrics; duplicates linked as one device; the imports, each with Undo; Import and Enter a result |
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

**`profile`**: what the age score and the targets need about the owner that
no other record holds. One record per repository, created under the id
`owner` (a PUT at `ifVersion: 0`, so two devices creating it meet at one
row).

| Property | Type | What |
| --- | --- | --- |
| `name` | string, required | the profile's title, "Healthspan profile" |
| `contact` | reference to `core/contact` | whose profile it is: the owner's contact when there was one at getting started |
| `birthDate` | date | written only where neither the contact nor a patient record gives a whole one |
| `sex` | enum `female`, `male` | the sex the reference curves and the targets are for |
| `startedAt` | datetime | when getting started was finished or skipped |
| `recordSeenAt` | datetime | the last time the owner marked the record read; a document or message after it is new |

**`import`**: one file or one hand entry taken in, so it can be shown and
undone.

| Property | Type | What |
| --- | --- | --- |
| `name` | string, required | the file's name, or "Entered by hand" |
| `format` | enum `csv`, `manual` | how it came in |
| `status` | state `imported` → `undone` | undone once its results are deleted |
| `source` | reference to `observations/source` | the lab it is from |
| `report` | reference to `labs/labreport` | the report it made |
| `importedAt` | datetime | when |
| `results` | int | results written |
| `skipped` | markdown | the rows left out and why |

Every `labresult` and `labreport` an import writes carries `origin` set to
the import record, so Undo lists what points at it, deletes it, and moves
the import to `undone`.

## The owner's details

The birth date and the sex are read in this order, each on its own:

1. **Birth date:** the owner's contact's `dates.birthday`, where it has a
   year; then `profile.birthDate`; then a `clinical/patient` record's
   `birthDate`, where exactly one patient record is about the owner (none
   names `about`, or it names the owner's contact).
2. **Sex:** `profile.sex`; then that patient record's `gender`, where it is
   `female` or `male`.

The owner's contact is the one the core setting
`substrate.reamde.dev/core/ownerContact` names. No scope writes `core`
([0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)),
so the app never writes the contact or the setting: a birth date typed on
the getting-started page goes to `profile.birthDate`, with a link to add it
to the contact in the console. Without an owner's contact the page asks for
the birth date and links to the console's step that sets one. Settings
names where each value came from; a value from the contact or a patient
record is changed there, not here.

## Sources and import

**The list** reads every `observations/source` record, and for each the
metrics it reports with the newest value's time, from one latest read
(`{"latest": true, "groupBy": ["metric", "source"]}`). Lab sources also
count their reports. The WHOOP and Apple Health adapters create a source
record only where it is absent, so the `priorities` the owner sets stand.

**Rank** is per group of metrics the screens read together (nights,
workouts, steps and distance, heart, body composition, lab values): the
owner drags the sources of a group into order and the app writes each
source's `priorities` for every metric of the group, 1 first. A group with
one source shows no rank.

**One device** is a `sourcelink`: the owner picks two or more sources that
are the same device (a scale synced to its maker's app and to Apple
Health; a band re-exported through Apple Health), names it, and the app
writes the link. A priority read then takes that device's values once.

**Import** takes a CSV or TSV of lab results, one row per result:

1. The file is parsed in the browser. Columns are recognised by header
   (date, test or analyte, value, unit, range or low and high, flag, lab,
   accession) and the owner can reassign any of them. A template file can
   be downloaded.
2. Each test name is matched to a catalogue entry, by LOINC code where a
   column holds one, else by the entry's names, then to the repository's
   metric for that entry ([The catalogue](#the-catalogue)). An unmatched
   name gets a picker of the repository's lab metrics, or is left out.
3. Units are checked against the metric's unit through `/api/v1/units/convert`
   (which no scope limits); a value whose unit does not convert is left out
   with the reason.
4. The plan is shown (results per date, left out and why); Import writes
   the `import` record, a `source` for the lab where none matches its name
   (class `lab`), one `labreport` per date and the `labresult`s, through the
   client rather than the outbox, in order, each a PUT at `ifVersion: 0`
   under an id derived from the import id and the row, so a retry after a
   lost answer writes nothing twice. Progress shows; a refusal stops the
   import, which can be resumed or undone.

**Enter a result** writes one `labresult` (and its report and import) from a
form: metric, value, unit, date, lab.

A PDF, an Apple Health export and hand-entered body measurements are not in
this plan ([Later](#later)).

## The catalogue

Today's catalogue keys each biomarker on a metric id, and 72 of those ids
are under one owner's authority. Each entry instead carries its LOINC codes
(one per unit form), its names, its panel, its target bands, its trend goal,
its cadence and its info content, with no metric id. The app resolves an
entry against the repository's `observations/metric` records: every metric
of category `labs` or `body` with one of the entry's codes, then the
`labs/` or `health/` metric of the entry's name. A card reads all of the
metrics an entry resolves to as one series, each value converted to the
first's unit, so a repository that holds the same analyte under two metrics
(its own and the catalogue's) shows one card.

**The `labs` catalogue grows to cover them.** An import needs a metric to
point at, and a repository that never had one has none. So `labs` version 2
adds a metric record for each analyte the Healthspan catalogue names that
`labs` lacks (about 60; the body composition ones go to `health`), with its
LOINC codes, unit, precision and description, and the analyte-annotated
units the mass and molar pairs need. That is first-party data any app can
use; the app asks for `labs` at version 2.

## The age score from the shared kinds

The engine keeps its curves, its weekly snapshots and its report, and takes
a new input shape built from the shared kinds instead of the WHOOP mirror:

| Contributor | Read from |
| --- | --- |
| Sleep consistency, hours of sleep | `health/sleep` records that are not naps, one per night under the priority policy; the night is the day before the local day it ended |
| Zone 1–3 and zone 4–5 minutes | `health/workout` `zoneDurations` |
| Strength training minutes | `health/workout` whose `sport` is a strength sport |
| Steps | the daily sum of `health/steps` |
| VO2 max | `health/vo2_max` readings over the last two years |
| Resting heart rate | the nightly `health/resting_heart_rate` summary |
| Lean body mass | body composition results in `labs/labresult`: fat-free mass over the same scan's weight, or 100 less its body fat percent |

"Days worn", which turns weekly minutes into a rate today, becomes the days
with a night or a resting heart rate. A source that reports zones only for
some workouts, or no nights, leaves those contributors out with a reason, as
a missing VO2 max does today. The demo generates one synthetic person as
both the mirror's rows and the shared kinds, and a test holds the two inputs
to the same report, so the switch changes no number for the vendor the
engine was fitted on.

The score is described as an estimate the app computes from published
reference curves (proposal and sources linked in "How the score works"),
not as any vendor's product.

## Its access

`app.json`'s asks and scopes, in the manifest's order:

```json
"asks": [
  {"package": "substrate.reamde.dev/healthspan", "atLeast": 1},
  {"package": "substrate.reamde.dev/health", "atLeast": 2},
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
installs it. `core/setting` is read for the one setting that names the
owner's contact; the scope reaches every setting record (none holds a
secret), which the approval page shows.

## The kit

Two additions every health app needs, made in the kit rather than the app:

- **`SubstrateClient.aggregate(filter, body)`**, typed after
  [the aggregate read](../api.md#the-aggregate-read): the request (metric or
  metrics, range, bucket, reducers, `from`, `statistic`, sources, groupBy,
  budget) and the answer (units, bucket, rows with group, values, sources,
  count). Today each app writes its own. Tested against the fake substrate,
  which learns to answer a canned aggregate.
- **`useRead(key, read, {kinds})`** in `@substrate/react`: a read through
  the session's client that is not a linked record (an aggregate, a list of
  a kind the app does not keep), cached per key for the page's life, read
  again when the outbox lands a write to one of `kinds`, on `refresh()`, and
  dropped on sign-out.

And `app.json` admits `web` as a platform: `lint:apps` holds that an app
naming it has a `web/` folder, and the release, which ships `apple/` and
`android/` builds, skips it.

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
- The engine's tests, carried over, and the mirror-to-shared-kinds parity
  test above.
- Catalogue resolution: by code, by name, two metrics for one analyte, unit
  conversion between them.
- Profile resolution: each order above, a yearless birthday, two patient
  records, no owner's contact.
- Import: header detection, every column form of a range, comparators
  (`<5`), units that do not convert, the derived ids, resume after a
  refusal, undo.
- Sources: the priorities written for a group, a link of two sources.
- Today's rules, filters, chart maths, scales and suggestions, carried over.
- End to end on a local substrate (`mise run dev:up` with the authority
  document from this tree): connect, approve (the three packages install),
  getting started, import the template file, the cards and the score appear,
  rank two sources, undo the import.

## Serving it

`SUBSTRATE_WEB_URL=https://<host>/<path>/ pnpm build` in `apps/healthspan/web`,
as Tasks: the build is static, routes in the hash, and needs `connect-src
https:` in any CSP. The README says how to serve it anywhere. The owner's
deployment publishes the closure with the next apps site publish and serves
the build beside Tasks on the web.

## Steps

One pull request, built in this order:

1. The kit's `aggregate`, `useRead` and the `web` platform, with tests.
2. `labs` version 2: the analytes the catalogue names, with codes and units.
3. `apps/healthspan`: `app.json`, the package, the shell on the template,
   the store, the demo's skeleton.
4. In parallel: the engine on the shared kinds with Healthspan's screens;
   the catalogue with Labs, Today and the rules; Record, the profile,
   Getting started and Settings; Sources and Import.
5. The end-to-end run, the README, `apps/README.md` and the kit's README
   naming the app, `docs/health-apps.md` linking it.

## Later

- Importing a PDF lab report, through an agent the package ships.
- Hand-entered body measurements (weight, blood pressure), which need a
  write scope on `observations/observation`.
- The owner's own target bands as records of a `target` kind.
- A service worker, so the app opens with no network.

## Questions for review

1. Is one fixed-id `profile` the right shape, or should the sex live where
   other apps can read it (a property core's contact does not have)?
2. Is writing `priorities` and `sourcelink`s from an app right, given that
   every reader then follows them?
3. Should `labs` version 2 carry the extra analytes, or should the app's own
   package ship them?
4. Is `read:substrate.reamde.dev/core/setting` acceptable for one setting?
