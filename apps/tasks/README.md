# Substrate Tasks

A to-do list kept in the owner's substrate: tasks with a due day or a due
time and a priority, in task lists the owner orders, on iPhone, iPad and Mac
(its Android UI is a later step on the same core). It is the first app built
on the kit from its first line ([docs/plans/app-kit.md](../../docs/plans/app-kit.md),
§10): every screen is a presentation TasksKit makes, and each platform's UI
draws it and sends commands, nothing else.

| Where | What |
| --- | --- |
| `app.json` | the app, named once: its ids, the package it publishes and asks for, its scope |
| `package/` | `substrate.reamde.dev/tasks`, the package its approval installs ([its README](package/README.md)) |
| `TasksKit/` | the core, a Swift package with no UI framework that builds and tests on Linux: `TasksCore` |
| `apple/Sources/` | the app target: its scenes, its menu commands and the updater |
| `apple/UI/` | `SubstrateTasksUI`: the shell that runs the link over the app's root, and the SwiftUI views |
| `apple/Tests/` | the views drawn from TasksKit's fixtures, on the Mac |
| `docs/screenshots/` | the app on an iPhone simulator against a local substrate, and the Mac's split layout drawn offscreen |

## TasksKit

`TasksCore` holds, each in a file of its own:

- **The model and its codec.** `TaskItem`, `TaskList`, `Due` (a day, or a
  moment with its day), `CivilDay`, `TaskPriority`, `TaskStatus`;
  `TasksCodec` reads a record leniently and writes a create (never a state:
  the state moves only by patch) or a patch of what changed, with
  `completedAt: null` on a reopen. A test holds its property names to the
  closure the app carries (`Resources/Package/tasks.json`, written by
  `.mise/closures.py --json`). `Position` puts a moved task or list between
  its neighbours as a decimal, so a move writes that one record.
- **`TasksApp`**, the root under `LinkController<TasksApp>`: the records as
  the server last answered them (a list read, then `WatchFollower` over both
  kinds, kept in the `RecordCache`) with the outbox's waiting writes laid
  over them (`RecordFold`), so a change made offline shows at once and stays
  shown until its record is read again. Every write goes to the outbox
  (`TaskWrites`): a batch per record, a create as a PUT that every retry
  lands on the same row, a change as a guarded patch, an edit on the version
  the owner saw, and a delete that can be restored (`DELETE` without purge);
  the link sends them. It plans the reminders this device shows
  (`Reminders`, scheduled through the kit's `Notifications` service) and
  keeps the settings in the repository's folder. A 401 or a 403 from its
  read or watch goes up to the link as a `LinkReport`.
- **`TaskEditor`**, a child kept by the root under a key it minted: one
  task's form, or a new one's, whose Save hands the write up.
- **The presenters**: `TasksPresenter` writes every word the screens show
  (the App menu, the lists column, a list's keyed rows, the open task, the
  delete-list question naming how many tasks go with it, the app's settings
  sections); `TasksSettings.screen` puts them beside the link's Status,
  Repository, Storage and About, and `TasksSettings.route` sends each
  settings command to the controller that answers it. `TaskField` holds the
  labels the read view and the edit view share. `TasksFixtures` is every
  screen in the states the app shows it in.

The Android UI needs nothing new from it: commands in, presentations out,
the lists as `KeyedList`s whose revisions follow `TasksState.revision`.

## Running it

```bash
mise run apple:build:tasks   # the Mac app and the simulator app; registers nothing with Apple
mise run apple:test:tasks    # TasksKit's tests, then the views over its fixtures, on the Mac
mise run apps:test:linux     # TasksKit (with the kit and WorkoutsKit) on Linux
mise run apps:gallery        # Tasks' screens drawn into build/gallery/apple/tasks
```

The Mac build is signed with the machine's development certificate and no
provisioning profile, so it runs on the machine that built it.

**Against a local substrate.** The approval installs the package from its
publisher's authority document, so a local server reads one written from
this tree instead of `substrate.reamde.dev`'s:

```bash
uv run --with pyyaml .mise/closures.py --out .dev/authority --authority substrate.reamde.dev tasks
SUBSTRATE_INSECURE_AUTHORITY_DOCS="substrate.reamde.dev=$PWD/.dev/authority/authority.json" mise run dev:up
mise run console:build && mise run dev:restart   # the approval page is the console's
```

Register a repository, run the app, give the connect screen
`http://localhost:8080`, sign in on the approval page, and approve: it reads
"Install Tasks", and the app's Settings then say "Tasks package: Version 1".
A local build's client id is the loopback one, so the page calls the app
unverified.
