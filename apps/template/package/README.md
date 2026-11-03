# The template package

`substrate.reamde.dev/template`: what Substrate Template keeps in the owner's
substrate, published from this folder through the authority document
(`.mise/closures.py`), and installed by the app's approval as a `package`
detail at the version the build carries or later. The app applies no
vocabulary itself.

| File | What it declares |
| --- | --- |
| `template.yaml` | the package (its version) and its bundle (what it installs) |
| `item.yaml` | `item`, the one example kind: a `name`, its title |

## Changing it

A changed declaration ships the package's next `version` in
`template.yaml`, additively: a new kind, a new optional property, a new enum
value. A published version never changes; `.mise/closures.py --check` holds
every published closure to `closures.lock.json`, which the release writes.
The app's codec names only what the kind declares, and its tests hold it
there.
