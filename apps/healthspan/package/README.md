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
| `import.yaml` | `import`: one file or hand entry of lab results, its plan (every result and report it writes, keyed), its state and the lease of the browser writing it, so it resumes on any device and can be undone |

Everything else the app reads is the shared catalogues' (`health`, `labs`,
`observations`, `clinical`) and the owner's contact (`core/contact`, named
by the setting `substrate.reamde.dev/core/ownerContact`). The lab results an
import writes are `labs/labreport` and `labs/labresult` records whose
`origin` points at the import.

## Changing it

A changed declaration ships the package's next `version` in
`healthspan.yaml`, additively: a new kind, a new optional property, a new
enum value. A published version never changes; `.mise/closures.py --check`
holds every published closure to `closures.lock.json`, which the release
writes. The app's codecs (`web/src/kinds.ts`) name only what the kinds
declare, and `web/src/kinds.test.ts` holds them there.
