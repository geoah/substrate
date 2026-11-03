# Substrate Template

An app built on the kit (`apps/kit`): its items kept in the owner's
substrate, on iPhone, iPad and Mac. Every screen is a presentation
TemplateKit makes, and each platform's UI draws it and sends commands,
nothing else ([docs/plans/app-kit.md](../../docs/plans/app-kit.md)).

| Where | What |
| --- | --- |
| `app.json` | the app, named once: its ids, the package it publishes and asks for, its scope |
| `package/` | `substrate.reamde.dev/template`, the package its approval installs ([its README](package/README.md)) |
| `TemplateKit/` | the core, a Swift package with no UI framework that builds and tests on Linux: `TemplateCore` |
| `apple/Sources/` | the app target: its scenes and the updater |
| `apple/UI/` | `SubstrateTemplateUI`: the kit's `AppShell` over the app's root, and the SwiftUI views |
| `apple/Tests/` | the views drawn from TemplateKit's fixtures, on the Mac |

## TemplateKit

`TemplateCore` holds, each in a file of its own: `TemplateKinds` (every kind
name the app spells), `TemplateManifest` (what its approval asks for, held
to `app.json` by a test), `TemplateApp` (the root under
`LinkController<TemplateApp>`: its items are the kit's `LinkedRecords`, and
every write goes through the outbox), `TemplatePresenter` and
`TemplatePresentation` (every word the screen shows), and
`TemplateFixtures` (the screen in the states the app shows it in).

## Running it

```bash
swift test --package-path apps/template/TemplateKit   # the core, on the Mac
mise run apple:project                                # the Xcode project, this app's targets included
```

Then build the `SubstrateTemplate` scheme. A local build's client id is the
loopback one (`TemplateManifest.localClientID`), so the approval page calls
the app unverified. Against a local substrate, the approval installs the
package from an authority document written from this tree:

```bash
uv run --with pyyaml .mise/closures.py --out .dev/authority --authority substrate.reamde.dev template
SUBSTRATE_INSECURE_AUTHORITY_DOCS="substrate.reamde.dev=$PWD/.dev/authority/authority.json" mise run dev:up
```
