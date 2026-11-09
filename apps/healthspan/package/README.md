# The healthspan package

`substrate.reamde.dev/healthspan`: what Substrate Healthspan keeps in the
owner's substrate that no other record holds, published from this folder
through the authority document (`.mise/closures.py`), and installed by the
app's approval as a `package` detail at the version the build carries or
later. The app applies no vocabulary itself.

| File | What it declares |
| --- | --- |
| `healthspan.yaml` | the package (its version) and its bundle (what it installs) |
| `profile.yaml` | `profile`: the reference basis (the sex the age score's curves and the lab targets are for), a birth date where the owner's contact has none, the order nights and workouts are read in, and when getting started and the record were last seen. One per repository, under the id `owner` |
| `import.yaml` | `import`: one run of an importer (a CSV, a hand entry, or a provider's export or folder: Numan, Randox, DXA, the NHS App, MyChart), naming its plan and every document it writes as blobs, with its state, the lease of the browser writing it and the latest run's counts, so it resumes on any device and can be undone |

Everything else the app reads is the shared catalogues' (`health`, `labs`,
`observations`, `clinical`) and the owner's contact (`core/contact`, named
by the setting `substrate.reamde.dev/core/ownerContact`). What an import
creates (lab reports and results, the clinical record, and the providers'
sources and organizations, which it never rewrites or undoes) points at the
import through `origin` where the kind declares one. How each importer reads
its provider is in [../docs/importers/](../docs/importers/).

## Changing it

A changed declaration ships the package's next `version` in
`healthspan.yaml`, additively: a new kind, a new optional property, a new
enum value. A published version never changes; `.mise/closures.py --check`
holds every published closure to `closures.lock.json`, which the release
writes. The app's codecs (`web/src/kinds.ts`) name only what the kinds
declare, and `web/src/kinds.test.ts` holds them there.
