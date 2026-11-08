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
| `docs/screenshots/` | the app on an iPhone and an iPad simulator against a local substrate (the approval, Needs you, the lists, a list, one with its done tasks and its options, a task, the editor, offline, Settings, and the iPad's three columns), and the Mac's split layout drawn offscreen; `android/`, the app on an Android emulator against one (sign-in, a list, a task, offline, Settings, a reminder) |

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
- **`TasksApp`**, the root under `LinkController<TasksApp>`, a
  `LinkedRecordsApp`: its tasks and lists are the kit's `LinkedRecords` over
  the two kinds, whose stages it writes none of, so a change made
  offline shows at once and stays shown until its record is read again, and
  only a write the outbox says landed is shown as landed. Every write goes
  through the outbox (`TaskWrites`, over the kit's `RecordWrites`): a create
  as a PUT with `ifVersion: 0`, which a retry whose earlier try landed counts
  as landed and never writes again, a change as a guarded patch, an edit on
  the version the owner saw (a task not sent yet at the version its create
  made), and a plain `DELETE`, no purge. It plans
  the reminders this device shows (`Reminders`, made current through the
  kit's `Notifications.reconcile`), keeps a reminder's tap until its task's
  record is there (`TasksState.tapped`), and keeps the settings in the
  repository's folder.
- **`TaskEditor`**, a child kept by the root under a key it minted, made in
  the device's time zone: one task's form, or a new one's. It keeps the task
  as it was when it opened (`State.seen`), and Save patches the fields the
  owner changed on that version, so a change made on another device
  meanwhile is a conflict the outbox parks for the owner, never written over.
  Save is the root's write, as any other the owner makes
  (`events(fromChild:_:)`), and the editor stays open with its draft until
  the outbox has it (`TasksState.saves`): a write the device could not keep
  is undone and said in the editor, whose Save becomes Retry.
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
  a release (`mise run android:release:build tasks` does). A release-type
  build without an https client id and redirect fails before anything is
  built (build-logic's `requireReleaseSignIn`); `mise run android:build`
  stamps placeholders on `example.com`. `TasksShell`
  says which root a command goes to and opens them; the screens
  (`ui/`) draw the App menu with kit-ui's `AppMenu` (a navigation bar on a
  phone, a rail or a drawer wider, with one, two or three panes), the
  lists with their open counts, a list's open and completed tasks with
  quick add, the open task with its list and priority as the fact rows
  that change them, the editor under the same labels, the delete-list
  question, and Settings, Status, Repository, Storage and About as
  kit-ui's `SettingsScreen` draws them.
- **Sign-in** is a debug build's loopback route, with the local client id:
  kit-device listens on `127.0.0.1` for the one path, the owner's default
  browser shows the approval page, and Swift checks the callback (`state`,
  `iss`, PKCE). Only a debug build (`BuildConfig.DEBUG`) takes that route;
  a release-type build never does, whatever it was stamped with. The
  listener takes as the callback only the request carrying the attempt's
  `state`, answers anything else (another path, another `state`, a request
  it cannot read) without ending the attempt, bounds every request in size
  and time, and gives up after ten minutes, the server's own limit, which
  the connect screen says. Android freezes an app in the background within
  seconds, and a frozen app's listener takes no callback, so a short
  foreground service ("Waiting for your browser") holds the process while
  the owner approves; Android ends it after about three minutes, and a
  callback that waited longer than the server keeps its code is refused
  and asked for again. The app brings itself forward once the callback is
  in where Android lets it, and the page the browser lands on links back
  to it either way.
- **A release signs in through its claimed callback**, an App Link: the
  stamped redirect (`/apps/tasks/android/callback` on the site) is
  declared, with `autoVerify`, for kit-device's `SignInCallback` in a
  release's manifest alone, and the site's `assetlinks.json` names the
  app's package with the certificate its APK is signed with. The owner's
  default browser follows the approval there, and Android hands the
  callback to the app alone. Where Android has not verified the link for
  the app, the sign-in is refused, never answered through loopback, which
  is the contract (docs/plans/app-kit.md §5.4).
- **Reminders** are kit-device's `Notifications`: each kept under
  `noBackupFilesDir` with an exact alarm (the app holds `USE_EXACT_ALARM`:
  an inexact one may come minutes late, after the app has stopped
  planning it) until it is shown, a tap reaching kit-device's unexported
  `NotificationOpened` and, from it, the app's `reminderTapped`; the
  exported launcher activity reads nothing from its intent, so no other
  app can make a tap. Reminders are off until the owner turns them on in
  Settings, and turning them on is when the permission is asked.
  `USE_EXACT_ALARM` suits a sideloaded app; distributing through
  Google Play would need `SCHEDULE_EXACT_ALARM` with the flow that asks
  the owner for it, unless Play accepts the app's use as one that
  qualifies for `USE_EXACT_ALARM`.

`android/fixtures/tasks.json` is `TasksFixtures.all` as Swift writes it
(`TasksAndroidTests`, `SUBSTRATE_UPDATE_FIXTURES=1` to write it again), and
`TasksGalleryTest` draws each with the app's own composables.

## Running it

```bash
mise run apple:build:tasks   # the Mac app and the simulator app; registers nothing with Apple
mise run apple:test:tasks    # TasksKit's tests, then the views over its fixtures, on the Mac
mise run apps:test:linux     # TasksKit, with the kit, WorkoutsKit and ChatKit, on Linux
mise run apps:gallery        # Tasks' screens drawn into build/gallery/apple/tasks and build/gallery/android/tasks
mise run android:build       # every Android module, the Tasks app's :tasks among them
mise run android:test        # its gallery check with the kit's tests
```

On an emulator, a debug build (`apps/tasks/android/build/outputs/apk/debug/tasks-debug.apk`,
signed with the debug key; a debug build alone allows plain HTTP, and only
to `localhost`, `127.0.0.1` and `10.0.2.2`) reaches
a local substrate through `adb reverse tcp:<port> tcp:<port>` at
`http://localhost:<port>`. Not at `10.0.2.2`: a server with no
`SUBSTRATE_PUBLIC_URL` names itself `http://` only on a loopback host, so
from `10.0.2.2` it answers as `https://10.0.2.2:<port>` and the app
refuses the mismatch.

The Mac build is signed with the machine's development certificate and no
provisioning profile, so it runs on the machine that built it.

**Against a local substrate.** The approval installs the package from its
publisher's authority document, so a local server reads one written from
this tree instead of `substrate.reamde.dev`'s:

```bash
.mise/python.sh .mise/closures.py --out .dev/authority --authority substrate.reamde.dev tasks
SUBSTRATE_INSECURE_AUTHORITY_DOCS="substrate.reamde.dev=$PWD/.dev/authority/authority.json" mise run dev:up
mise run console:build && mise run dev:restart   # the approval page is the console's
```

Register a repository, run the app, give the connect screen
`http://localhost:8080`, sign in on the approval page, and approve: it reads
"Install Tasks", and the app's Settings then say "Tasks package", "Version 1",
with `substrate.reamde.dev/tasks` under it.
A local build's client id is the loopback one, so the page calls the app
unverified.
