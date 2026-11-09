# Substrate Healthspan

Your labs, an age score and your health record, read from your own
substrate, in a browser on a phone, a tablet or a desktop. It keeps what no
other record holds in a package of its own (`substrate.reamde.dev/healthspan`,
[its README](package/README.md)) and reads everything else from the shared
catalogues the providers write: `health` (nights, workouts and the metrics
wearables and scales measure), `labs` (lab reports and results),
`observations` (the values and their sources) and, where a provider fills
it, `clinical` (the record from a GP or a hospital). The design and the
reasons for it are in [docs/plans/healthspan.md](../../docs/plans/healthspan.md).

It runs on [the web kit](../kit/web/README.md), as
[Tasks on the web](../tasks/web/README.md) does: React 19, Vite and
Tailwind CSS 4, the app authorization for its access, the kit's outbox for
its writes.

| Screen | What it shows |
| --- | --- |
| Getting started | Your name from your own contact, the birth date it or a patient record gives, the reference basis the age score and the targets use, and where your data comes from. It opens first, until you finish or skip it |
| Today | Everything that needs attention, the most urgent first: results out of range or off target, important tests overdue, a contributor that slipped, record items due |
| Labs | A card per biomarker in panels, each with its chart, target zones, the lab's range, its trend and its sources; filters, a metric's history, the reports |
| Healthspan | An age score and a pace of aging from sleep, training, steps, VO2 max, resting heart rate and lean mass, against published reference curves; the signals your sources report; how the score works |
| Record | The clinical record from every provider that writes it, with what is due and what is new; shown once there is one |
| Sources | Every source of your values, what it reports and when; which one wins where two report the same thing; sources that are one device; the imports, with Resume and Undo; Import a provider's export or folder ([below](#importers)), a CSV of lab results, or one result by hand |
| Settings | Your details and where each came from, the display settings, then the kit's sections |

A phone has a tab per section; from 768 px the sections sit in a sidebar.
Where you are lives in the address bar (`#/labs/ldl_cholesterol`), so a
link opens the same screen.

## Who you are

Your birth date is your own contact's (`dates.birthday` with a year, on the
contact the core setting `substrate.reamde.dev/core/ownerContact` names),
else the one the app keeps on its profile. The reference basis (which sex's
curves and targets apply) is yours to choose on the profile; a patient
record's gender is offered, never taken. An app cannot write `core`, so a
value typed here goes to the profile, and Settings links to the console to
put it on your contact. The app reads every setting record for the one
naming your contact; none holds a secret.

## Importers

Sources takes what a provider gives you and offers the importer it
recognises, then shows what it will write before anything is written:

| Importer | Hand it | It writes |
| --- | --- | --- |
| Numan | its JSON exports, one or several | each test as a report, its results with the lab's and Numan's ranges |
| Randox | the folder `randoxctl` downloads, or its zip | each visit's results with their bands, and its Tanita body composition sheet, regions included |
| DXA | GE Lunar scan printouts (PDF), and a transcribed trend page | each scan's bone density and body composition, regions included; asks the scanning centre's name and time zone |
| NHS App | the folder `nhsctl` downloads, or its zip | the GP record's test results, and the whole clinical record: consultations, conditions, medicines, documents, messages |
| MyChart | the folder `mychartctl` downloads for a portal, or its zip | the hospital's lab results, and its clinical record and imaging reports; asks the hospital's ODS code |
| CSV | a CSV or TSV of lab results | the rows you tick, matched to the catalogue |

An import is staged from your browser, then written by any device that
resumes it. A newer export of the same provider updates what an older one
wrote, never the other way round, and Undo removes what an import created.
How each importer reads its files is in [docs/importers/](docs/importers/).

## Which source wins

Where two sources report the same metric, the server ranks them by each
source's `priorities` and takes one device's values once through a
`sourcelink` ([observations](../../docs/observations.md)); Sources is where
you set both, and every app reading those metrics follows them. Nights and
workouts are records, whose reads name their own order, so their order is
this app's, kept on its profile.

## Its access

The approval asks for three packages (`healthspan` 1, `health` 3, `labs` 2)
and these scopes: its own package to write; lab reports and results to
write (imports and hand entries), the rest of `labs` to read; sources and
source links to write (ranks and devices), the rest of `observations` to
read; `health` and `clinical` to read; your contact and the settings to
read. `app.json` names them, and `web/src/manifest.ts` is held to it by a
test.

## Run it

```bash
cd apps && pnpm install
pnpm --filter @substrate/healthspan-web dev   # http://127.0.0.1:5175, a local app to any substrate
```

`http://127.0.0.1:5175/?demo` runs on an invented person with no substrate
at all: three years of labs, nights and workouts from invented sources, a
patient record. `?today=2026-10-25` pins the date, `?start` opens getting
started, `?profile=none` forgets who the person is, and
`?healthspan=calibrating|paused|locked|onevo2` shows the age score's other
states.

Against a local substrate, the approval installs the package from an
authority document written from this tree, as for any app here:

```bash
.mise/python.sh .mise/closures.py --out .dev/authority --authority substrate.reamde.dev healthspan
SUBSTRATE_INSECURE_AUTHORITY_DOCS="substrate.reamde.dev=$PWD/.dev/authority/authority.json" mise run dev:up
```

The checks, from `apps/`: `pnpm test`, `pnpm typecheck`, `pnpm lint`,
`pnpm fmt:check`.

## Serve it

```bash
SUBSTRATE_WEB_URL=https://health.example.com/ pnpm --filter @substrate/healthspan-web build
```

`SUBSTRATE_WEB_URL` is where the build is served, an https URL ending in `/`.
The build is static (`web/dist`): the app, `client.json` (its client id is
`<url>client.json`, its one redirect `<url>`) and `manifest.webmanifest`.
Serve `<url>` as `index.html`; nothing else needs rewriting, since the app
routes in the hash. A host serving it must allow `connect-src https:` in
any CSP it sets, because each owner's substrate is on a host of their own.
An app served from a host outside its publisher's authority shows as
unverified on the approval page, and still installs the published package.

## Layout

| Path | What |
| --- | --- |
| `app.json` | the app named once: its package, asks, scopes, icon and accent |
| `package/` | `substrate.reamde.dev/healthspan`: `profile` and `import` |
| `web/src/main.tsx`, `App.tsx`, `route.ts` | the manifest and the shell, the sections and the routes |
| `web/src/kinds.ts` | the package's two kinds and every kind name the app reads |
| `web/src/data/` | every read and write the screens make, one domain each, live and demo |
| `web/src/catalogue/` | the lab catalogue: entries keyed by LOINC codes, panels, targets, info, and their resolution against your metrics |
| `web/src/labs/` | the lab index: specimens, unit forms, series, filters |
| `web/src/engine/`, `web/src/healthspan/` | the age score and its input from the shared kinds; the cards, scales and suggestions |
| `web/src/profile/`, `web/src/record/` | who you are; the clinical record |
| `web/src/sources/`, `web/src/import/` | the sources, ranks and links; the runner that stages, writes and undoes every import |
| `web/src/import/importers/` | one importer per provider behind one interface, with Effect, and what they share |
| `docs/importers/` | how each importer reads its provider |
| `web/src/rules/` | what needs attention, one pure function each |
| `web/src/ui/`, `web/src/lib/` | the charts, cards and controls; formatting, units, time axes, display settings |
| `web/src/demo/` | the invented person |
