# Substrate Template

An app built on the kit (`apps/kit`): its items kept in the owner's
substrate, on iPhone, iPad and Mac. Every screen is a presentation
TemplateKit makes, and each platform's UI draws it and sends commands,
nothing else ([docs/plans/app-kit.md](../../docs/plans/app-kit.md)).

A new app made by `mise run apps:new <id>` starts as this one, renamed for
its id, and builds and passes its tests before anyone edits it. Start from
here, then read the kit's
[Linked records](../kit/README.md#linked-records) and
[An app's shell](../kit/README.md#an-apps-shell): this app is those two
paragraphs and little else.

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
`LinkController<TemplateApp>`, a `LinkedRecordsApp`: its items are the kit's
`LinkedRecords`, every write goes through the outbox, and it writes only its
own engine and effects, here Storage's sizes), `TemplatePresenter` and
`TemplatePresentation` (every word the screen shows, Settings included), and
`TemplateFixtures` (the screen in the states the app shows it in).

The items are a `KeyedList` whose revision is the root's own counter
(`State.revision`), moved by the engine whenever the items may be drawn
otherwise, never a count of them: a rename keeps the count and must still
move the revision, or the Android bridge sends no delta.

What the add field holds is the root's (`State.draft`), and it is cleared
only once the outbox has the item it adds (`.records(.enqueued)`): a write
the device could not keep (`.records(.notQueued)`, a full disk) is undone
and leaves the text where it was, with why beside it and Retry, so nothing
typed is lost before it is kept. Every write the owner fills in is held so.

## Growing it

What an app adds next, each as Substrate Tasks does it
([its README](../tasks/README.md)):

- **A change, or an edit.** `RecordWrites.change` is a patch guarded by the
  version the outbox last saw: on a conflict the record is read again and
  the patch sent once more on top, right for a toggle or a rename the owner
  makes in passing. `RecordWrites.edit` is a patch on the version the owner
  saw: a conflict parks it as a refused write the Status screen offers to
  retry or discard, right for a form the owner filled in.

  ```swift
  case let .rename(id, name):
      guard let record = context.state.linked.records[RecordFold.key(TemplateKinds.item, id)] else { return [] }
      return [.records(.wrote(writes.change(TemplateKinds.item, id, properties: ["name": .string(name)],
                                            version: record.version > 0 ? record.version : nil)))]
  ```

- **An editor.** A child controller the root keeps under a key it mints
  (`children(in:)`), its save handed up as the root's own events with
  `events(fromChild:_:)`: the write, and the editor kept open with its
  draft until the outbox has it, as the add field is, so `.records(.enqueued)`
  closes it and `.records(.notQueued)` hands it why, with Save as Retry
  (Tasks' `TaskEditor` and `EditorSave`). It holds the record as it was when
  it opened and patches what the owner changed on that version, so a change
  made elsewhere meanwhile comes back as a conflict. Drawn over the screen
  with `.childSheet(app.editors, dismiss:)`.
- **A question first.** A `ConfirmationPresentation` in the presentation and
  `.confirmation(app.confirmation, confirm:, dismiss:)` on the view; a
  settings row asks its own (`SettingsRow.confirmation`), from its button.
- **Settings of its own.** A section of rows in the presentation's
  `settings`, their commands routed by `Configuration.settings`, the values
  kept in the repository's folder with `SettingsFile`.
- **Reminders.** A plan of `NotificationRequest`s the presenter makes from
  the records, made the device's with `Notifications.reconcile(_:prefix:)`,
  and a tap back as a command through `Configuration.respond`.
- **A second section.** An `AppMenuPresentation` and `AppMenuRoot { tabs }
  split: { sidebar }`: a tab bar on a phone, a sidebar on an iPad and a Mac.

## Running it

```bash
swift test --package-path apps/template/TemplateKit   # the core, on the Mac
mise run apple:project                                # the Xcode project, this app's targets included
xcodebuild -project apps/xcode/Substrate.xcodeproj -scheme SubstrateTemplate -destination 'platform=macOS' \
  test -only-testing:SubstrateTemplateTests           # the views drawn from the fixtures
```

Then build the `SubstrateTemplate` scheme. A local build's client id is the
loopback one (`TemplateManifest.localClientID`), so the approval page calls
the app unverified. Against a local substrate, the approval installs the
package from an authority document written from this tree:

```bash
.mise/python.sh .mise/closures.py --out .dev/authority --authority substrate.reamde.dev template
SUBSTRATE_INSECURE_AUTHORITY_DOCS="substrate.reamde.dev=$PWD/.dev/authority/authority.json" mise run dev:up
```
