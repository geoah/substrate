# Substrate Tasks

A to-do list kept in the owner's substrate: tasks with a due day or a due
time and a priority, in task lists the owner orders, on iPhone, iPad, Mac
and Android. It is the first app built
on the kit from its first line ([docs/plans/app-kit.md](../../docs/plans/app-kit.md),
§10): every screen is a presentation TasksKit makes, and each platform's UI
draws it and sends commands, nothing else.

| Where | What |
| --- | --- |
| `app.json` | the app, named once: its ids, the package it publishes and asks for, its scope |
| `package/` | `substrate.reamde.dev/tasks`, the package its approval installs ([its README](package/README.md)) |
| `TasksKit/` | the core, a Swift package with no UI framework that builds and tests on Linux: `TasksCore`, and `TasksAndroid`, its roots over the Android bridge |
| `apple/Sources/` | the app target: its scenes, its menu commands and the updater |
| `apple/UI/` | `SubstrateTasksUI`: the kit's `AppShell` over the app's root, and the SwiftUI views |
| `apple/Tests/` | the views drawn from TasksKit's fixtures, on the Mac |
| `android/` | the Android app: the Gradle module `:tasks`, its Compose UI, its bridge configuration and generated Kotlin, and its gallery test over `fixtures/tasks.json` |
| `docs/screenshots/` | the app on an iPhone simulator against a local substrate, and the Mac's split layout drawn offscreen; `android/`, the app on an Android emulator against one |

## TasksKit

`TasksCore` holds, each in a file of its own, what is Tasks' own; the rest
is the kit's ([its README](../kit/README.md)):

- **The model and its codec.** `TaskItem`, `TaskList`, `TaskPriority`,
  `TaskStatus` and `TaskOrder`, over the kit's `CivilDay`, `Due` and
  `Position` (a moved task or list writes that one record, a number between
  its neighbours); `TasksCodec` reads a record leniently and writes a create
  (never a state: the state moves only by patch) or a patch of what changed,
  with `completedAt: null` on a reopen. A test holds its property names to
  the closure the app carries (`Resources/Package/tasks.json`, written by
  `.mise/closures.py --json`), and `dueAt` to the temporal trait's point.
- **`TasksApp`**, the root under `LinkController<TasksApp>`: its tasks and
  lists are the kit's `LinkedRecords` over the two kinds, so a change made
  offline shows at once and stays shown until its record is read again, and
  only a write the outbox says landed is shown as landed. Every write goes
  through the outbox (`TaskWrites`, over the kit's `RecordWrites`): a create
  as a PUT every retry lands on the same row, a change as a guarded patch, an
  edit on the version the owner saw, and a plain `DELETE`, no purge. It plans
  the reminders this device shows (`Reminders`, made current through the
  kit's `Notifications.reconcile`), keeps a reminder's tap until its task's
  record is there (`TasksState.tapped`), and keeps the settings in the
  repository's folder.
- **`TaskEditor`**, a child kept by the root under a key it minted, made in
  the device's time zone: one task's form, or a new one's, whose Save hands
  the write up.
- **The presenters**: `TasksPresenter` writes every word the screens show
  (the App menu, the lists column, a list's keyed rows and their menus, the
  open task with its list and priority as the choices that change them, the
  delete-list question naming how many tasks go with it, the app's settings
  sections); `TasksSettings.screen` puts them beside the link's through the
  kit's `SettingsPresenter.screen`. `TaskField` holds the labels the read
  view and the edit view share. `TasksFixtures` is every screen in the
  states the app shows it in.

The Apple side is the kit's `AppShell<TasksApp>` (`apple/UI/TasksShell.swift`
hands it the app's configuration) and the views: what a phone's stack holds
is the presentation's (the list shown, the task open), never a view's.

## On Android

The same TasksKit, through the kit's bridge, under a Compose UI in
Material 3: commands in, presentations out, the lists as `KeyedList`s
whose revisions follow `TasksState.revision`. TasksKit needed nothing but
`Bridged` conformances, a `TaskField` case named apart from Kotlin's
`name`, and its package read from the bundle the bridge copied out, since
`Bundle.module` stops the process on Android.

- **`TasksKit/Sources/TasksAndroid`** is the app's roots in its own library
  (`libSubstrateTasks.so`, the `SubstrateTasks` product), registered by
  `substrate_bridge_roots`: `tasks` is Apple's `AppShell` in Swift, the
  session in the secure store, the signed-in repository's folder under
  `noBackupFilesDir`, and `LinkController<TasksApp>` as one runtime, each
  tree crossing as a `TasksFrame` with the Settings screen beside it and a
  reminder's tap made the app's command; `tasks.connect` is the kit's
  `SignInController`, the browser sign-in that keeps the session, whose
  count of finished sign-ins Kotlin acts on by opening `tasks` again.
- **`android/`** is the Gradle module `:tasks`. Its ids come from
  `app.json`; a release's `-Psubstrate.release.*` stamp sets the version
  name and code and the client id and redirect, and the module never signs
  a release (`mise run android:release:build tasks` does). `TasksShell`
  says which root a command goes to and opens them; the screens
  (`ui/`) draw the App menu with kit-ui's `AppMenu` (a navigation bar on a
  phone, a rail or a drawer wider, with one, two or three panes), the
  lists with their open counts, a list's open and completed tasks with
  quick add, the open task with its list and priority as the fact rows
  that change them, the editor under the same labels, the delete-list
  question, and Settings, Status, Repository, Storage and About as
  kit-ui's `SettingsScreen` draws them.
- **Sign-in** is a local build's loopback route: kit-device listens on
  `127.0.0.1` for the one path, the owner's default browser shows the
  approval page, and Swift checks the callback. A release's claimed
  callback (an App Link verified by the site) is not taken yet, and a
  release refuses to sign in rather than fall back to loopback.
- **Reminders** are kit-device's `Notifications`: each kept under
  `noBackupFilesDir` with an alarm until it is shown, a tap reaching the
  activity and, from it, the app's `reminderTapped`.

`android/fixtures/tasks.json` is `TasksFixtures.all` as Swift writes it
(`TasksAndroidTests`, `SUBSTRATE_UPDATE_FIXTURES=1` to write it again), and
`TasksGalleryTest` draws each with the app's own composables.

## Running it

```bash
mise run apple:build:tasks   # the Mac app and the simulator app; registers nothing with Apple
mise run apple:test:tasks    # TasksKit's tests, then the views over its fixtures, on the Mac
mise run apps:test:linux     # TasksKit (with the kit and WorkoutsKit) on Linux
mise run apps:gallery        # Tasks' screens drawn into build/gallery/apple/tasks and build/gallery/android/tasks
mise run android:build       # every Android module, the Tasks app's :tasks among them
mise run android:test        # its gallery check with the kit's tests
```

On an emulator, a debug build (`apps/tasks/android/build/outputs/apk/debug/tasks-debug.apk`,
signed with the debug key) reaches a local substrate on the host at
`http://10.0.2.2:<port>`; a debug build alone allows plain HTTP.

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
"Install Tasks", and the app's Settings then say "Tasks package", "Version 1",
with `substrate.reamde.dev/tasks` under it.
A local build's client id is the loopback one, so the page calls the app
unverified.
