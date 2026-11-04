# Plan: one app kit for every Substrate app, on Apple systems and Android

Status: implemented on branch `feat/app-kit`, 2026-11-03 to 2026-11-04;
based on the domains stack (#1163 to #1167, [status](authorities/status.md)).
The plan was reviewed by Fable and Astra and confirmed by each; every step
was reviewed by one of them as it landed (review record at the end, with what
each step settled other than the plan said). Where the build differs from a
section, the section says so under **As built**; what is knowingly left is
§14. The binding choices are decision records 0222 to 0224 (§13).

## 1. The request

The owner, 2026-11-03:

- Every Substrate app shares its business logic, its client, sign-in,
  synchronization and local caching through libraries, and builds for
  Android as well as iOS. **Tens of new apps** will be built on them, so the
  structure and the code quality are the point.
- Common UI for signing in and approving access, records, menus, settings,
  and caching and storage use.
- Device services (camera, QR codes, local storage, location, notifications,
  background sync) behind one shape on both systems.
- The Mac is a target, nice to have now and planned for; other platforms may
  follow. The lowest Android version and the APK size are not constraints.
- Release tooling that ships an app's Apple and Android builds together. The
  Android client documents and association file go on the same host as the
  Apple ones for now; both move soon, so nothing may assume the host.
- Convert the apps built today (the console shell, Chat, Workouts) and add
  **Substrate Tasks**, an app with kinds of its own, to show the whole thing
  working. Sync stays as it is, apart from following the new layout.
- Each big step is reviewed by Fable or Astra before the next one starts.

What "builds for Android" means in this round: the kit and every app's core
(§6) build for Android in CI, and Substrate Tasks and the kit's gallery have
an Android UI. Chat, Workouts and the console get Android UIs later, on the
same kit.

## 2. What the apps share today

Four Apple apps and two Swift packages: about 64,000 lines of Swift and
19,000 of tests.

| Code | Lines | No UI framework | Holds |
| --- | --- | --- | --- |
| `SubstrateKit` | 6,200 | all but `UI/`, two files of `Records/` and the browser sheet | server discovery, the repository locator (DNS), client, OAuth, session, status check, their screens, record views |
| `WorkoutsKit/WorkoutsCore` | 20,500 | all of it | model, an event-sourced session engine, presenters, codec, local files |
| `WorkoutsKit/WorkoutsSync` | 2,900 | all of it | outbox, package reading, remote catalogue, history exchange |
| `SubstrateChat` | 7,600 | ~3,000 | ndjson streams, the watch, its disk cache |
| `SubstrateSync` | 7,800 | ~1,000 | HealthKit, EventKit, Contacts and CoreLocation sources |
| `SubstrateWorkouts` | 16,500 | none | SwiftUI, `SessionController`, `SubstrateLink`, platform code |

`SubstrateKit` and the apps but Workouts build in Swift 5 mode; WorkoutsKit
and Workouts are Swift 6.

The domains stack changed three things this plan builds on: a released app
signs in through a **claimed https callback** on its publisher's site (a
local build keeps the loopback redirect); an app's kinds are a **package its
publisher's authority document serves**, installed by the approval
(`declare:` is gone, 0214); and the connect screen takes a repository's name
and finds its server through a **DNS TXT record**.

**How Workouts is built.** The UI sends a `SessionCommand`. The
`@MainActor` `SessionController` enriches it with what a pure function may
not read (new UUIDv7 ids, the instant floored to the second, history
lookups) into a `SessionEvent`, applies it with
`SessionEngine.apply(envelope, to: state) throws -> SessionState`, a pure
reducer with no effects, writes the event to the journal, presents again
with `SessionPresenter.present(state, catalog:, history:, settings:, now:)`,
and only then runs its platform work and hands the change to the outbox.
On launch the journal folds the events back through the engine, which is
why effects cannot live in the reducer: a replay would run them again. The
470 WorkoutsCore tests assert on presenter output without drawing anything.
Around it sit `AppModel` (403 lines) and `SubstrateLink` (753 lines: the
session, the package version, the outbox state, parked writes, drafts),
which hold what every screen needs.

**How Chat is built.** `ChatStore` (963 lines) holds the chat list, the
watch, the cache and seven ordering rules (read epochs, run numbers, overlay
ownership, `SubstrateChat/DESIGN.md`), and it calls notifications and App
Intents itself. Each `Conversation` (649 lines) runs up to seven tasks with
run epochs that drop late callbacks and a 50 ms flush of streamed deltas.
Its pure parts (the transcript fold, the stream line parsers) sit underneath.

**The same thing is built more than once.**

| Concern | Chat | Sync | Workouts |
| --- | --- | --- | --- |
| Local files | `DiskCache`: per-identity folder, format version, 100 MB budget, 60-day eviction, size in Settings | `LocalState`: `sync-state.json` | `WorkoutsFiles`, `RemoteCatalog`: per-repository folder, unreadable files set aside and listed |
| Writes offline | an optimistic overlay, then a reconcile read | a full scan diffed against `Pushed{hash, keys}` | `Outbox`: queued PUT/PATCH/DELETE, `ifVersion` chains, park, retry, discard, resolve |
| Reading changes | the watch (`/changes?watch=1`), a cursor, relist on 410 | none | whole reads at launch and foreground |
| Status | an online flag and the watch's state | `SourceOverview` badges, `SyncProgress` | `SyncStatus.derive` and its sentence |
| JSON | the kit's `JSONValue` (Double, unordered) | its own ordered `JSON` (a golden needs byte equality) | its own `JSONValue` (Decimal, ordered) |
| Record calls | `API.patch` over `[String: Any]` | `API/Client.swift` over `[String: Any]` | `WorkoutsAPI` over the kit |

`.mise/apple.sh` (1,100 lines) names each app in 13 `case` statements: its
bundle id, its packages, its client document. That does not scale to tens of
apps.

## 3. Evidence

### 3.1 The Swift core builds off Apple systems

Android's Swift uses the same open-source Foundation as Linux. On
2026-11-03, a copy of the tree built in the `swift:6.4` container:

- **WorkoutsCore** unchanged, and its whole suite passed.
- **WorkoutsSync** after `import FoundationNetworking` (under
  `#if canImport`) in the files that do not import SubstrateKit; its whole
  suite passed, the outbox against its scripted server included.
- **SubstrateKit without `UI/`** after six changes, each a seam this plan
  needs anyway: CryptoKit to swift-crypto; `SecRandomCopyBytes` to the
  system generator; the Keychain behind `SessionStorage`; the loopback
  receiver and the browser sheet out (device services); a
  `CFBooleanGetTypeID` check; one read-only `URLSessionConfiguration`
  property. The DNS resolver added since is a device service too.

### 3.2 The Android spike

Run 2026-11-03 on branch `geoah/android-swift-bridge-spike` (3894cc6f),
an API 36 arm64 emulator on an Apple-silicon Mac. It stopped early when the
disk filled; what it did not reach is listed last and moves into step 7.

**The toolchain installs and pins.** Swift 6.4.2, the open-source build,
through mise's own `swift` backend (no swiftly); the Swift SDK for Android
`swift-6.4.2-RELEASE_android.artifactbundle` by URL and checksum, linked to
NDK r30 (`30.0.16248370`) by the bundle's setup script; command-line tools
23.0, platform 36 and the compile SDK 37 that Compose BOM 2026.09.00 needs;
AGP 9.5.0, Gradle 9.8.2, Kotlin 2.4.21, JDK temurin 21. mise holds Swift, the
JDK and Gradle; the SDK packages, the NDK and the bundle are a list an
`android:sdk` task installs, and `lint:toolchain` holds the Swift pin, the
bundle's version and Gradle's `ndkVersion` equal.

**What worked.** WorkoutsCore built for `aarch64-unknown-linux-android28`
unchanged. A JSON bridge (three `@_cdecl` natives in, one Kotlin method out)
carried `RoutinePlanPresenter`'s output for a sample routine, 8.5 KB, into a
Compose screen. A Swift actor streamed values from three executor threads
into a Kotlin `Flow`. ICU formatting matched the Mac's (en_GB dates, de_DE
numbers, WorkoutsCore's "102.5 kg"). Every library the SDK ships is aligned
to 16 KB pages.

**What the design must carry**, each now a rule in §5:

- `Bundle.module` cannot work on Android (`Bundle.main` is `/system/bin`):
  resources ship as APK assets, are copied out once, and reach Swift as a
  `Bundle(path:)` the environment passes in.
- `Locale.current` and `TimeZone.current` do not follow the device (Swift
  saw `en_001` and GMT): the locale and time zone are environment inputs.
- `@MainActor` work and `DispatchQueue.main` never run until libdispatch's
  main queue is bound to the main Looper; the bridge's `start` binds it.
- JNI on Swift's threads: the Kotlin class is cached as a global reference
  in `JNI_OnLoad` (a thread Swift started sees only the system class
  loader), a thread is detached by a thread-local destructor (ART aborts on
  a thread that exits attached), each delivery runs in a local frame, and
  strings cross as UTF-16 (JNI's "UTF" is modified UTF-8 and breaks emoji).
- Swift synthesizes `Codable` only in the declaring file, and an enum case
  encodes as `{"case": {"label": …}}`, `{"case": {"_0": …}}` or
  `{"case": {}}`: a bridged value conforms where it is declared, which is
  what §5.5's restricted model and generator assume.

**Sizes.** ICU is 45.8 MB, FoundationNetworking 16.5, `libswiftCore` 11.7,
FoundationEssentials 10.5, Foundation 9.0, the NDK's `libc++_shared` 9.5; the
debug APK is 122 MB. Linked statically into one stripped library, the Swift
side is 59 MB. The owner set no budget; FoundationEssentials alone would
save about 75 MB if WorkoutsCore's formatters did without ICU.

**Not reached, moved to step 7**: the release APK's size and a run of it,
cold start, a clean round-trip time (the first call took 1.7 s in a debug
build on a cold emulator, 0.6 s on the next start), `URLSession` over a
working network, x86_64, the kit's portable half, a macro cross-compile,
swift-java's jextract. The bridge is chosen on what worked here; jextract is
not compared.

**Encoding a Chat-sized presentation**, measured 2026-11-03 on an M-series
Mac with `JSONEncoder` (release build, macOS and the Linux container alike):
a 500-turn transcript is 204 KB and takes 2.3 to 2.7 ms to encode and as long
again to decode; a 20-turn window is 8 KB and 0.1 ms. Once per screen that is
nothing; at a streamed reply's 20 flushes a second on a slower phone it is a
tenth of a core. §5.5's keyed lists follow from it.

## 4. The choice: Swift is the core; each platform draws natively

| Option | Core | UI | Cost here |
| --- | --- | --- | --- |
| **Swift on Android, a Compose UI** (chosen) | the Swift we have | SwiftUI on Apple systems, Compose on Android, both drawing the same presentations | ~50–60 MB of APK; a young toolchain |
| Kotlin Multiplatform, Compose Multiplatform | rewrite ~30k lines of package code in Kotlin | one Compose UI, Skia-drawn on iOS | a rewrite; iOS loses native SwiftUI |
| Rust (UniFFI or Crux) | rewrite in Rust | native on each | a rewrite and a third language |
| gomobile | rewrite in Go | native on each | no slices of structs, maps, generics or async across the boundary |
| Skip's SwiftUI on Android | the Swift we have | a SwiftUI subset redrawn in Compose | Android gets what the subset allows |

The Swift SDK for Android became official in Swift 6.3 (2026-03-24) and moved
to NDK r30 in 6.4 (2026-09-16); Polymarket ships a ~170k-line Swift core
under a Compose UI this way. The rewrites pay off only if Android, or the Go
server, becomes where the logic lives.

**The UI is not shared; what it shows is.** iOS is the primary platform and
uses Liquid Glass and the system's controls, and a shared renderer gives
that up on one side. Shared presentations give both apps the same screens,
labels, order and states while each looks like its own system; the rule
that read and edit views use the same labels holds by construction. What
presentations do not hold (layout, navigation, interaction) is held by the
gallery check (§7.4).

## 5. Architecture

### 5.1 The loop

Workouts' shape, named once and used for every screen. The words are
defined for apps in `docs/terms.md` in step 3 (§7.3).

```
command ──controller──▶ event ──engine──▶ state ──journal──▶ presenter ──▶ presentation
   ▲        (enriches:                                                         │
   │        ids, now, lookups)               effects(event, state, presentation)
   │                                                     │
   └──────── UI ◀──── presentation          work ──▶ result event ──▶ (the same loop)
```

- **Command**: what the UI asks for (`tap Done on row 3`). The UI sends
  nothing else.
- **Controller**: the one writer of a piece of state. It turns a command
  into an **event** by adding what a pure function may not read: new ids,
  the current instant, lookups in caches. It runs on the kit's runtime, not
  on a UI thread.
- **Engine**: `apply(event, to: state) throws -> State`, pure. Folding the
  journal's events through it gives the same state every time.
- **Journal**: optional per controller. Workouts' session has one (its
  events are its truth); Chat's conversation does not (the server is). The
  engine says which events are journaled.
- **Presenter**: `present(state, inputs) -> Presentation`, pure. Its inputs
  are declared (catalogue, history, settings, the instant, the locale), so a
  test pins every one.
- **Effects**: `effects(of: event, on: state, presented: presentation) ->
  [Effect]`, pure, run only on the live path, never on a replay. An effect is
  work the runtime does through the client, the local store or a device
  service; its result comes back as an event through the same loop. The
  presentation is an input because some effects are made from it (Workouts'
  `platformSync` sends the Live Activity the presenter's text).
- **Presentation**: a value of strings, numbers, enums and ids that a view
  draws. It is the only thing a UI reads. A presentation's `nextChange`
  instant has the runtime deliver a `tick` event then (Workouts' rest
  timer), so no view keeps a timer of its own.

**The order is the contract**, Workouts' today: engine, then the journal
written synchronously with no suspension before it, then present, then
effects. A step that must not be journaled until something outside the
engine succeeds is two events: Workouts' finish becomes `finishRequested`
(applied, not journaled), an effect that writes the history file, then
`finished`, which deletes the journal, or `finishFailed`, which leaves the
session open. A crash between them replays to an open session, never to a
finished one with no history.

**A controller's state is two parts**: the engine's state (journaled where
there is a journal) and transient state the presenter also reads but no
replay needs (Workouts' entry check, scroll target, last refusal). Both are
values, and both are inputs to the presenter.

### 5.2 Composition

The decision tens of apps inherit.

- **Dependencies arrive in an environment value**: the client, the local
  store, the device services, the clock, the id source, the locale and time
  zone, and the bundle the resources come from (§3.2: none of the last three
  can be read from the process on Android). A controller is made with one, a
  test passes fakes, and nothing reaches for a shared instance.
- **The link is every app's root.** `LinkController` owns the connection to
  one repository: the session and its revocations, the status check, the
  versions of the app's packages, connectivity, the outbox's state. Chat's
  `AppModel` is already that half; Workouts' sync run, catalogue, sample and
  drafts stay in a Workouts controller fed by the link, which keeps it as
  its one child, so one runtime runs both as one tree. Its presentation is
  the Connect, Access, Repository and Status screens (§5.6), with the app's
  own beside them. **The link's
  snapshot carries no secret**: the token stays inside `SubstrateAuth` and the
  transport, so it never reaches a child, a presentation or a bridge message
  other than a transport request (§5.4).
- **One presentation tree per root.** A child controller's presentation is a
  subtree of its parent's, and a step that changes both publishes one tree.
  Chat's handover, where a settled turn's rows land and its live overlay
  clears in one step, stays one step: on Android it is one message, not two.
- **State flows down as events and values, and up as events.** The runtime
  delivers the link's snapshot to every controller under it as
  `event(forLink:)`, inside the step that changed it. A parent hands a child
  what it needs (a catalogue, a history index, the settings) as part of the
  child's value, made from the parent's state; a value that changes
  replaces the one running, and the child's state stays. A child reaches
  its parent with `event(fromChild:_:)`, which the parent's engine folds;
  what both need (Chat's transcript rows) is the parent's. The app reaches
  the link the same way: a 401 or a 403 `insufficient_scope` from any of
  its effects, however deep, is a `LinkReport` the link folds. A child
  never calls its parent.
- **Children are keyed by the client's own id**, minted when the child is
  made (Chat's conversation UUID). Server ids arrive later as events (Chat's
  `thread` event binds a thread id), and the parent's engine applies its rules
  to them (binding a thread closes any other conversation on it). A child
  lives until the parent drops its key, so a stream survives navigation.
- **Two grains of epoch.** Starting an effect under a key cancels the one
  running under that key, and its result carries the key's epoch, so a late
  result is dropped. Closing or resetting a controller bumps its **controller
  generation**, which drops every result still outstanding, whatever its key
  (Chat's `run` guard). Chat's seven rules become the runtime's behaviour.
- **Coalescing is per event.** An event says whether it presents now
  (structure: a turn started, a card arrived) or within a window (a streamed
  text delta, Chat's 50 ms), and the runtime presents once per window.

**The proof is Chat, in step 5.** Its list becomes a controller with
conversations as keyed children, a turn's stream a keyed effect, its read
epochs and run numbers the runtime's two grains, and its handover one tree.
If Chat needs a rule the runtime cannot express, the runtime changes before
Workouts and Tasks build on it.

### 5.3 The kit's libraries

One Swift package, `apps/kit`, in Swift 6 mode, built for iOS, macOS, Linux
and Android. A library depends only on those above it.

| Library | Holds | From |
| --- | --- | --- |
| `SubstrateCore` | one `JSONValue` (ordered keys, a number kept as its decimal text, so Workouts' `Decimal` holds); the record and its flat wire form; kind references; RFC 3339 times, ISO 8601 durations; `ExternalID`; errors and `explain`; kind names and labels; scope, grant and change words | `SubstrateKit` `Records/`, `Status/` words; WorkoutsCore `Codec/` |
| `SubstrateDevice` | the device-service protocols and their request and answer values (§5.4) | new |
| `SubstrateAPI` | the client over the transport service: record calls with `ifVersion`, lists, filters, blobs, webhook delivery, line streams, the **watch** (cursor, generation, relist on 410 or `compacted`), agent turn streams; discovery, versions, the requirement; the locator over the DNS service | `SubstrateKit` `API/`, `Server/`; Chat `API/Watch.swift`, `AgentEvent.swift` |
| `SubstrateAuth` | manifest and details, PKCE, `authorizeURL`, the callback check and code exchange, `Session`, the session rules over the secure-store service, pending revocations, the status check | `SubstrateKit` `Auth/`, `Status/` |
| `SubstrateRuntime` | command, event, engine, effects, presenter, controller, environment, journal, keyed children, epochs, coalescing, keyed lists, the link's snapshot (§5.1, §5.2) | Workouts' `SessionController`, `SessionJournal`; Chat's ordering rules |
| `SubstrateLocal` | the per-repository folder (format version, atomic writes, unreadable files set aside, sizes, budget, eviction); the journal file; the **outbox** (Workouts', with a batch key in place of `session`; its routine helpers stay in the app); the **record cache** the watch keeps current; reading the version of a package the repository holds; the link generation | Workouts `Outbox`, `SessionJournal`, `RemoteCatalog`, `TrainingPackage` (read side); Chat `DiskCache` |
| `SubstrateScreens` | the shared screens: `LinkController` and each screen's engine, presenter and fixtures (§5.6) | `SubstrateKit` `UI/` (the logic now inside its views); Workouts' `SyncStatus` |
| `SubstrateTesting` | a scripted server, a fake of every device service, the runtime's harness (a fixed clock and id source, a journal in memory) | Workouts' and Chat's `ScriptedServer` |
| `SubstrateDeviceApple` (Apple) | the device services on Apple frameworks | `WebAuthenticator`, `LoopbackReceiver`, `DNSServiceTXTResolver` and `SubstrateResolv`, the Keychain half of `SessionStore`, Chat's `Notifier` and background code |
| `SubstrateUI` (Apple) | SwiftUI views of the shared screens; the design tokens (§7.4); the `@Observable` wrapper that republishes a controller's presentations on the main actor | `SubstrateKit` `UI/`, Chat's `Components.swift` |
| `SubstrateBridge` (Android) | the JNI surface (§5.5) | new |

The Android side is a Gradle build of three modules: `kit-core` (the Swift
libraries as `.so` files with `SubstrateBridge` and the generated Kotlin, one
AAR), `kit-ui` (a composable per SwiftUI view, fed the same presentations)
and `kit-device` (the device services in Kotlin).

**As built.** The table above, plus `SubstrateBridgeCheck` (the roots the
bridge's runtime checks open, and the one that presents the gallery's
fixtures), two C targets (`SubstrateResolv` under `SubstrateDeviceApple`,
`SubstrateJNI` under `SubstrateBridge`) and `SubstrateKit`, one import of
every library an Apple app draws with; the kit's
[README](../../apps/kit/README.md) is the table as it stands. Beside the
three Gradle modules are two apps that are never published, `bridge-check`
(the runtime checks on a device) and `gallery` (the shared screens live),
and the Gradle root includes each app's own module (`:tasks`).

Sync is not converted. It keeps its own `JSON`, `LocalState`, `pushRows` and
client code until it is, and the kit does not take them; that duplication is
listed in `apps/sync/README.md` as the work its conversion does.

### 5.4 Device services

A protocol in `SubstrateDevice`, an Apple implementation, an Android
implementation and a fake. Four rules:

- A request and its answer are values. An implementation holds no callback
  into the core.
- An answer is typed by outcome, never collapsed: DNS answers records, a
  confirmed absence, an unconfirmed absence (which allows only the
  parent-host fallback), or a failure that stops discovery, as
  `ServerLocator` tells them apart today.
- A long-lived source is a stream with an explicit end, error and cancel
  (§5.5). The transport's line stream (`HTTPTransport.lines(for:)`) landed
  in step 4 with the kit's watch; Chat's watch and agent turn streams,
  which read `URLSession.bytes` directly, move onto it in step 5, where
  their tests hold the change.
- A service that is a screen (camera, code scanner, photo picker) is a UI
  **component**: the UI shows it and sends the controller a command with its
  result.

First cut, the ones the converted apps and Tasks use:

| Service | What the core sees | Apple | Android |
| --- | --- | --- | --- |
| Transport | request → head, then body or ordered chunks; cancel | `URLSession` | OkHttp |
| DNS | TXT records, confirmed absent, unconfirmed absent, or failed | dnssd, libresolv on a negative answer | `DnsResolver.rawQuery` (API 29+), TXT segments joined and bounded in Swift |
| Secure store | read, write, delete by key; missing vs unavailable | Keychain, access group | Keystore-wrapped Tink keyset, ciphertext in DataStore |
| Browser sign-in | open this URL; the callback's query, or cancelled | `ASWebAuthenticationSession`: claimed callback in a release, loopback in a local build | Auth Tab with a verified App Link in a release (a Custom Tab where Auth Tab is missing), loopback in a local build |
| App folders | data, cache; protected until first unlock; excluded from backup | `FileManager`, data protection | `filesDir`, `noBackupFilesDir`, `cacheDir` |
| Connectivity | online, offline, expensive | `NWPathMonitor` | `ConnectivityManager` |
| Notifications | permission; schedule, cancel by id; a tap becomes a command | UserNotifications | `NotificationManager` |
| Background work | named jobs with constraints; `run(job, budget)` in a cold process | `BGTaskScheduler` | WorkManager |
| Updates | a newer build on the feed; install it, with each step's state | Sparkle (Mac), the manifest link (iOS) | the feed, then the package installer |

Ongoing activity, location, health, contacts and calendars land with the
app that needs them on Android.

**As built.** Transport, DNS, the secure store, app folders, connectivity
and notifications are built on both systems, notifications on Android as
`NotificationManager` with an alarm per scheduled request, for Tasks'
reminders. Browser sign-in on Android takes a local build's loopback route
alone; a release refuses to sign in until its App Link and Auth Tab are
built (§14). Ongoing activities joined the list on Apple systems (Workouts'
Live Activity). Background work is built on Apple systems only
(`BGTaskScheduler` refresh jobs, which Chat runs, and continued processing),
with the link generation checked before a run's result lands; WorkManager
and the headless start below are not built, since no Android app has
background work yet. Updates is not a device service: the Apple apps keep
the updater they had (`xcode/Shared/Updater.swift`, Sparkle on the Mac and
the manifest link on iOS), whose offer reaches an app's About screen (as
a command, in Tasks and the template), and an Android app has no updater
(§9, §14).

**Security parity is an acceptance test, not a library choice.** Each
Android implementation passes the same contract tests as the Apple one, and
the contract is what the Apple apps do today:

- No request follows a redirect, a streamed one included
  (`API/Transport.swift`'s `RefusingRedirects`).
- The secure store tells missing from unavailable, holds secrets readable
  after first unlock and on this device only, keeps them out of backups and
  device transfers, and survives a corrupt or invalidated key by asking to
  sign in again, never by crashing or by keeping plaintext. On Android the
  keyset is wrapped by a Keystore key and DataStore holds ciphertext only.
- A token to revoke is kept until the server confirms the revocation
  (`SessionStore`'s pending revocations), across restarts.
- A token never appears in a presentation, a link snapshot, a WorkManager
  input or a log. The only bridge messages that carry one are the secure
  store's and the transport's (the bearer header out, a token endpoint's
  answer back), and the bridge redacts both wherever it records a message.
- A release signs in only through the claimed callback; a failed App Link
  verification is a failure, never a fallback to loopback. Loopback is a
  local build's client id and nothing else, bound to the loopback address,
  with PKCE, `state` and `iss` checked as `OAuthClient` does. A callback that
  arrives after the activity was recreated completes the exchange it belongs
  to, or is refused; it never starts another.

**Where state lives on Android.** The secure store's DataStore file and
Tink keyset, and the repository's folder (outbox, record cache, journal),
live under `noBackupFilesDir`; the backup and device-transfer rules exclude
them by name as well, so a restore never revives a token or a queue of
writes for a session that no longer exists. Caches live under `cacheDir`.
A body too large for a bridge message is a file under
`noBackupFilesDir/bridge`, since it may hold the owner's records; each one
is deleted by whichever side holds it last, and the folder is swept at
start. A credential exchange's body and answer never go to a file.

**Background work runs headless.** A job starts the runtime without an
activity, opens the repository's folder, reads the current session at run
time (never from its inputs), checkpoints through the outbox, and stops when
cancelled. The **link generation** is persisted in the repository's folder,
bumped by a sign-out, named by every job when it is scheduled and checked
before every write, so a job from before a sign-out writes nothing. It runs
in the app's own process, because actors do not coordinate file writes across
processes.

### 5.5 The Android bridge

Values cross as JSON messages over a fixed JNI surface: the bridge never
changes when an app adds a controller.

**The surface**, written in Swift with `@_cdecl`:

- `start(config)`, once per process, from `Application.onCreate` on the
  main thread, so a background job starts it without an activity: records
  the `JavaVM`, takes global references to the Kotlin receiver (cached at
  `JNI_OnLoad`, since a Swift thread sees only the system class loader),
  binds libdispatch's main queue to the main Looper, takes the device's
  locale, time zone and resource folder, and compares the protocol
  fingerprint (below) with the one Kotlin was built with. A failure (a
  mismatch, a library that will not load) is a structured, terminal answer
  the app shows as a problem; nothing is half started.
- `post(message) -> reply`: every message from Kotlin, synchronous only in
  that it validates and queues; the work runs on Swift's executors.
- Swift calls one Kotlin method, `deliver(message)`, from a thread it has
  attached to the JVM, keeping that call short, inside a local frame, with
  the string as UTF-16; a thread-local destructor detaches the thread when
  it ends. Every JNI call's failure (attach, local frame, a pending Kotlin
  exception) is checked and becomes a structured error; nothing is
  force-unwrapped. The spike's bridge took none of these checks, so it is a
  seed, not the bridge.

**The messages** are one envelope: `{v, op, handle, id, seq, body}`.

- `open` (a root controller, its environment's settings) answers a
  **handle**: an id and a **handle generation**. One handle carries one
  presentation tree (§5.2), children included. `close` is idempotent: it
  cancels the handle's effects and answers each request still outstanding
  with `cancelled`, exactly once. A message for a closed handle or an old
  generation is dropped and counted.
- `command` sends a command to a handle. `presentation` delivers a handle's
  latest presentation; presentations are snapshots, so a slow consumer gets
  the newest and the ones in between are skipped, after encoding is
  coalesced on the Swift side.
- A long collection in a presentation (a transcript, a task list) is a
  **keyed list** with a revision. The first message carries it whole at a
  revision; each later one is a delta from a base revision to the next,
  carrying the rows inserted, changed and removed by id and the order as
  ids, so a streamed reply re-encodes one row, not the transcript (§3.2).
  Deltas are never coalesced: Kotlin applies them in revision order and
  only then publishes the snapshot its `StateFlow` may coalesce. A delta
  whose base is not the list's revision asks for the whole list again; the
  reset carries its revision, and deltas older than it are dropped.
- `request` and `reply` carry a device-service call by id, answered exactly
  once; a reply to an unknown or answered id is refused. `chunk`, `end`,
  `error` and `cancel` carry a stream by id and sequence number, in order,
  with a bounded queue whose overflow cancels the stream with an error the
  controller sees; a stream ends exactly once, with `end`, `error` or
  `cancel`, whichever comes first. Watch rows and device answers are never
  coalesced, and a value Kotlin cannot queue is that overflow, never a
  silent drop.
- Bytes (a blob, an image) never travel in JSON: they are files in the app
  folders, and messages carry their paths.
- `error` is structured: malformed message, unknown controller, a Swift
  failure, a Kotlin exception. Nothing throws across the boundary and
  nothing crashes the process. A message Kotlin does not take is a failure
  of what waited on it: a request's call, or the handle.
- `environment` carries a new locale and time zone from Kotlin's
  broadcast receiver to every open root, in order with its commands.

**Delivery is serial per handle**, on one Swift queue, so a controller's
presentations arrive in order. `kit-core` turns them into a `StateFlow` per
handle on the main thread, and runs device requests on the threads Android
wants (the main thread for a browser or a notification permission, a worker
otherwise).

**The Kotlin is generated, from a restricted model.** A value that crosses
conforms to `Bridged`, and `Bridged` admits only: structs of bridged
values; `String`-raw enums; enums whose cases have labelled payloads;
optionals; arrays; string-keyed dictionaries; `Bool`, `Int`, `Double`;
`Decimal` and large integers as strings; `Date` as RFC 3339; no custom
`CodingKeys` and no hand-written codecs. A tool built on swift-syntax reads
the `Bridged` declarations and writes `bridge.golden.json` (every value, its
fields, optionality and cases); a generator writes the Kotlin from it
(data classes, sealed interfaces, kotlinx.serialization, the decoder for
Swift's synthesized enum encoding). It covers commands, presentations,
device requests, replies and errors. The golden's hash is the protocol
fingerprint `start` checks. Round-trip fixtures (Swift encodes, Kotlin
decodes and encodes, Swift decodes) cover payload enums, an absent field
against a null one, Unicode, decimals and large integers. Nobody edits the
golden or the Kotlin by hand.

**Why not generated JNI objects** (swift-java's jextract, Skip's bridge).
Both would work for one call. A fixed surface means one place for every
lifetime and threading rule above instead of one per value, the bridge does
not grow with the apps, and Kotlin values are plain data the JVM tests and
screenshots use without loading Swift. The spike built this bridge and not
jextract (§3.2).

### 5.6 The shared screens

Each is an engine, presenter and fixtures in `SubstrateScreens`, a SwiftUI
view in `SubstrateUI` and a composable in `kit-ui`.

**First cut**, the ones every app needs:

| Screen | What it shows | Today |
| --- | --- | --- |
| Connect | address or repository name, finding the server, the door (browser, password, too old), unrevoked tokens | `ConnectView`, `ServerTooOldScreen`, `UnrevokedNotices` |
| Access | the status banner's issues; the approval review; access ended | `AppStatusBanner`, `ReviewChangesSheet`, `AccessEndedView` |
| Repository | server, repository, access, change access, sign out | `AccountSection` |
| Status | a sentence (up to date, sending, offline, needs approval, signed out), the writes waiting and the ones refused, each with retry and discard | Workouts' `SubstrateSection`, `SyncStatus` |
| Settings | sections and rows as data (toggle, choice, value, link, destructive action), with the repository, status, storage and about sections every app has | each app's own |
| Problems | one problem in words, and what fixes it | `ProblemBox`, Chat's `ProblemLine` |
| About | the build, the channel, an update when one is offered | each app's footer, `Updater.swift` |

**With Tasks**: **Records** (a list row and a detail from a kind's
declaration and labels, references as pills, read and edit with the same
labels; from the declaration in this round, from the console's `recordview`
records (0166) later, so the two do not drift), **Storage** (each area's
size and share of its budget, a clear action that never drops unsent writes
silently) and the **App menu** (sections as data, drawn as a tab bar on a
phone and a sidebar on iPad, Mac and a wide Android screen).

**As built.** The App menu is built on both systems. Storage is a section of
the Settings screen, not a screen of its own: each area's size and one total
against the budget the saved copies share, with Clear Saved Copies. The
Records screen is not built: what the kit took from Tasks is how an app keeps
its records (`LinkedRecords`: read once, followed through the watch, the
outbox's writes laid over), and Tasks draws its own list and detail over it;
a screen made from a kind's declaration waits for an app that shows records
it does not own (§14).

## 6. Where things live

By app, not by platform: one app is one folder.

```
apps/
  README.md            how the apps are built, tested and released
  kit/                 the Swift package of §5.3, one README with a row per library, android/ (Gradle) inside
  xcode/               project.yml (includes each app's apple/targets.yml), Shared/, scripts/
  gradle/              settings.gradle.kts (includes each app's android/), build-logic/
  console/             app.json, apple/, docs/
  chat/                app.json, ChatKit/, apple/, docs/
  workouts/            app.json, WorkoutsKit/, apple/ (app, widgets), package/, library/, scripts/, docs/
  sync/                app.json, apple/ (app, share target): moved, not converted
  tasks/               app.json, TasksKit/, apple/, android/, package/
  template/            what apps:new copies
```

- **`app.json` names an app once** (JSON, because every reader parses it
  with no dependency: the Swift and Go tests, Gradle, `jq` in the release
  scripts, Python): id, display name, bundle id and Android
  application id, platforms, the Info.plist and manifest keys of its client
  id and redirect, the packages it publishes from `package/`, its scopes and
  package details, its icon, its feed folder. It replaces
  `apps/apple/scopes.json`: the server's first-party reach test reads the
  scopes from it, and the app's Swift manifest is held to it by a test, as
  `ReachTests` and `CoachTests` hold the manifests to `scopes.json` today. The
  Apple project, the Gradle settings, the site and the release read it.
- **An app's core** is a Swift package of its controllers, engines,
  presenters and model, with no UI framework, in a folder named for it
  (`WorkoutsKit/`, `TasksKit/`). SwiftPM names a local package by its
  folder, and one Xcode project resolves every app's, so two apps' folders
  named `core/` would collide; `lint:apps` holds package folder names
  unique. `apple/` and `android/` are thin UIs and the app's own device
  code, each `apple/` with its own `Support/` (Info.plists, entitlements).
- **Nothing in `apps/<app>` imports another app**, and each app's scripts,
  docs and locks live under it: Workouts' `package/closures.lock.json` and
  `library/library.lock.json` move with it, and the site reads them there.
- **An app's Android side has two halves.** Its Kotlin UI is the Gradle
  module in `apps/<app>/android`; the Swift it loads (its roots over the
  bridge, registered by name) is a library of its core's package, since that
  package is what builds its Swift for Android: Tasks' is `TasksKit`'s
  `TasksAndroid`, linked as `libSubstrateTasks.so`. Tasks is the one app with
  an `android/` folder in this round.
- **What does not move**: bundle ids, Keychain services and access groups,
  App Groups, and every on-device path (`Application Support/Workouts/`,
  `SubstrateChat/`), so an installed app upgrades in place.

## 7. Code quality, held by tools

### 7.1 Layering, checked

`mise run lint:apps` reads every `Package.swift`, `targets.yml`, Gradle
module and `app.json`, and refuses:

- a dependency that points up §5.3's list, or from one app to another;
- an app's core or a kit library outside the Apple and Android rows that imports
  anything but Foundation, FoundationNetworking, Observation, swift-crypto
  and the kit;
- a Kotlin file outside `kit-device` or an app's `android/` that touches an
  Android service;
- an `app.json` the generators cannot read, or an app without one;
- a third-party package not listed, with its reason, in `apps/README.md`
  (today: Sparkle, swift-crypto, swift-syntax for the tool only, OkHttp,
  AndroidX, kotlinx.serialization, Tink).

The kit and every app's core build and test on Linux in CI and cross-compile for
Android, which catches what an allow-list misses.

### 7.2 Formatting, linting, API

`swift-format` (in the toolchain) and `ktfmt` with `detekt` join the
families: `fmt:swift`, `fmt:kotlin`, `lint:swift`, `lint:kotlin`, inside the
bare `fmt`, `fmt:check` and `lint`, each configured once so the writer and
the checker agree. The existing Swift is reformatted once, in a commit of its
own. `swift package diagnose-api-breaking-changes` runs on a pull request
that touches the kit, and a kit change fixes every app it breaks in the same
pull request: `mise run apps:build` builds all of them on a Mac, and CI
builds every core on Linux and for Android.

**As built.** Formatters: the next commit, `fmt:swift` (swift-format) and
`fmt:kotlin` (ktfmt) in `fmt` and `fmt:check`, with the existing code
reformatted in that commit alone. Deferred: `detekt`, a second Kotlin
configuration and Gradle plugin for a Kotlin side that is still the kit and
one app, where the compiler's warnings, `lint:apps` and the review hold it;
the API-break check, since the kit has no consumer outside this tree and
`diagnose-api-breaking-changes` needs a released baseline the kit has never
had, while a kit change already fixes its apps in the same pull request,
which CI's Linux and Android builds of every core enforce; and
`apps:build`, since each app has its `apple:build:<app>` and
`apple:test:<app>` and nothing runs the Apple side in CI to call one task
for all of them.

### 7.3 Conventions

- Swift 6 language mode and complete concurrency checking everywhere; the
  kit leaves Swift 5 mode in step 3.
- Swift Testing. A controller is tested through its engine, effects and
  presenter, by value, with the clock, ids and locale pinned (presenter text
  depends on the locale today: `Format/Formatters.swift`).
- One concept per file, named for it; no `utils`, no `types.swift`.
- The words are `docs/terms.md`'s. Step 3 adds an apps section defining,
  "in an app", command, controller, engine, effect, presenter,
  presentation, journal, link, device service and outbox, with the
  generations named apart (watch generation, handle generation, controller
  generation, link generation). Engine, effect and link already mean
  something on the server (`internal/engine`, a function's returned effects,
  a reference), so the section says which sense an app's code means. The
  kit takes none of adapter, mirror, stream, sync, feature, group or
  account.
- Each library and each app has a README saying what it holds. As built,
  the kit has one README with a row per library, and an app's README is
  `apps/README.md`'s section for it, beside its own where it has one (Tasks,
  Workouts, the template; Chat's `DESIGN.md`).
- `mise run apps:new <id>` scaffolds `apps/<id>/` from `apps/template/`;
  CI runs it and builds the result, so the template cannot rot. Substrate
  Tasks is the first app it makes. As built, Tasks came first, and the
  template was extracted from it once the kit had taken what every app
  repeated (linked records, the shell, the App menu's views); every app
  after Tasks starts there.
- `.github/review.md` gains an apps section: the layering, the security
  contract, presentations instead of logic in views, both renderers changed
  together.

### 7.4 One look, two systems

- **Design tokens** (colours, type scale, spacing, radii, each kind's glyph
  and tint as an SF Symbol and a Material Symbol) are Swift in `SubstrateUI`;
  a test writes them as Kotlin for `kit-ui`, the same golden pattern as the
  bridge, with no Node toolchain added.
- **The gallery is a check, not a demo.** Every screen's fixtures render on
  both platforms: `ImageRenderer` under `swift test` for SwiftUI (the kit's
  `SUBSTRATEKIT_SNAPSHOTS` already does), Paparazzi for Compose, no emulator.
  A fixture without both renderers fails the build, so a presentation change
  ships both views in one pull request. `mise run apps:gallery` writes the
  two sets side by side, and a Gallery app on each platform shows them
  live. As built, Compose draws under Robolectric with Roborazzi (step 7's
  record), and the Gallery app is Android's alone; on Apple systems the
  gallery is `apps:gallery`'s pictures and each app's view tests.

## 8. Building and testing

- **The toolchain is pinned whole**, in mise where mise can and checked by
  `lint:toolchain` where it cannot: the open-source Swift toolchain (exactly
  the Swift SDK for Android's version), the SDK's artifact URL and checksum,
  the NDK, the JDK, the Gradle wrapper and its checksum, the Android Gradle
  Plugin, Kotlin, compile SDK and build tools, Gradle dependency locks, and
  the `swift` container image by digest.
- **Four kinds of test**: host (`swift test` on the Mac for the kit and every
  core), Linux (the same in the container, CI), Android build (cross-compile
  the kit and every core, build the APKs, CI), and Android runtime (on an
  emulator: the bridge's contract tests under a release, minified build,
  repeated open and close, cancellation races, a slow consumer, a large
  response, a cold background job, a browser callback after the activity is
  recreated). Presenter tests and screenshots cannot catch JNI lifetimes or
  missing native resources; the runtime tests can.
- **A real server is one command away**: `mise run dev` serves the simulator
  and the emulator. As built, the emulator reaches it through
  `adb reverse` at `localhost`, not at `10.0.2.2`: a server with no public
  URL names itself `http://` only on a loopback host, and the app refuses
  the mismatch.
- **Debugging** Swift on Android has no breakpoints in Android Studio, so
  logic is debugged on the host where its tests run.
- **Tasks**: `apps:build`, `apps:test`, `apps:test:linux`, `android:build`,
  `android:test`, `android:smoke`, `apps:gallery`, `apps:new`. As built,
  `apps:build` and `apps:test` are each app's `apple:build:<app>` and
  `apple:test:<app>` (the kit's is `apple:test:kit`), and the rest are as
  named, with `apps:new:check`, `android:generate`, `android:lock`,
  `android:release:build`, and `ci:apps` and `ci:android`, the two CI jobs.
  The Android runtime tests run the bridge's contract and kit-device's
  services; a cold background job is not among them, since none is built.

## 9. Releasing both platforms together

`mise run apps:release [channel] [app…]` replaces the per-app release tasks
and keeps every guarantee `apple.sh` gives today.

1. **Build and validate everything first.** Every selected app on every
   platform, from one commit, under one version
   (`0.113.1-pre.20261021.153012+ac85863e`). Nothing is uploaded until all
   of them built and passed their checks.
2. **Stage the artifacts** under `releases/<build>/`, immutable.
3. **The site** (`apps:site`, today's `apple:site` generalised): published
   closures stay as they are, a new closure goes up only after
   `substratectl publish --check` admits it, closures go up before the
   authority document and the document last; each client document lists
   every platform's callback; Apple's association file; Android's
   `assetlinks.json` with the fingerprint read from the signed APK itself.
4. **Activate** last, per app: its `release.json` names every platform's
   build and is written after their artifacts, then the feeds the installed
   apps read today (`index.json`, Sparkle's `appcast.xml`, the iOS manifest)
   are rewritten from it, so an installed app finds the release that moves
   its feed. Atomicity is per app: a release of three apps can leave one
   activated and two not, and a rerun of the same build activates the rest,
   reading what is already published before it writes.
5. **One publisher at a time.** The publisher creates a lease object with a
   conditional write (`If-None-Match: *`), holding an expiry and a fencing
   token; it renews while it works, re-reads the published state after
   taking the lease, and checks the token before each write. A lease past
   its expiry may be taken over, and the taker re-reads everything; an
   expired lease is never renewed. A bucket compares no token, so every
   write is itself conditional, on the version of the key read under the
   lease (`If-Match`) or on its absence (`If-None-Match: *`), and bounded
   to end before the lease expires: a publisher that stalls past its
   lease cannot land a write over the next one's. A bucket that cannot
   write conditionally is refused, not raced.

`SUBSTRATE_APPS_RELEASE_DRY_RUN=1` runs every step against `build/`, the site
included, and changes no lock file.

**Android specifics.** `versionCode` is allocated under the lease: the
larger of the minutes since 2026-01-01 at the build's UTC time and the
highest published or reserved code plus one, reserved for the build in
the release folder before it builds and written into the staged
`release.json`; a rerun of the same staged build reuses it, and nothing
else does. The signing
key is made once, kept by the owner
as a secret outside the tree, and its fingerprint is what `assetlinks.json`
names; losing it means every install is replaced by hand, as losing
Sparkle's key does on the Mac, and `apps/README.md` says how to rotate it.
The updater shows each state: a newer build, permission to install from this
app (`REQUEST_INSTALL_PACKAGES`), the system's confirmation, cancelled,
failed. As built, a release lists the APK in the app's feed and signs it as
above, and no Android updater reads the feed yet (§14).

## 10. Substrate Tasks

The app built on the kit from its first line, on iPhone, iPad, Mac and
Android.

- **Its package** is `substrate.reamde.dev/tasks`, published from
  `apps/tasks/package/` through the authority document as Workouts' is. It
  is not the `tasks` sample: the sample is a template a repository copies
  under its own authority, with a Linear mapping and `people` and
  `scheduling` behind it, and an app's approval installs only published
  packages. A repository that imported the sample holds both, each under its
  full reference, which every surface already shows.
- **Its words are the sample's** wherever the meaning is the same: a task
  has `name` (its `displayTemplate`), `description`, `status`, `dueAt` and
  `priority` on the same ladder. Tasks belong to a `list`, not a `project`:
  the sample's project has a lifecycle and dates of its own, while a list is
  only the owner's grouping and order. The kinds go through the kind review
  before they ship, which may still settle on the sample's word.
- **What it exercises**: sign-in and approval, the package version, the
  record screens, offline writes through the outbox, live updates through
  the watch, due-date reminders through notifications, background work,
  settings, status and storage, the app menu. As built, all but the record
  screens (its own over the kit's linked records, §5.6) and background work
  (reminders are planned on the device while the app runs; nothing refreshes
  in the background).
- **Not in the first cut**: sharing, location reminders, attachments, QR
  codes.

## 11. The work, in order

| Step | What | Review |
| --- | --- | --- |
| 1 | This plan | Fable and Astra, then a confirmation round by each |
| 2 | **Relocation, mechanical**: the layout of §6 with `git mv` and path fixes only (`apple.sh`, `.mise.toml`, the scripts, `internal/catalog`'s reach test, `#filePath` goldens, XcodeGen paths, docs links); module names, bundle ids, Keychain groups, `scopes.json`'s readers, serialized fields and on-device paths unchanged | Astra |
| 3 | **The kit split**: §5.3's libraries behind compatibility exports, Swift 6 mode, the duplicates retired, `app.json`, `lint:apps`, the terms, the Linux job | Astra |
| 4 | **The runtime and the shared screens**: `SubstrateRuntime` (§5.1, §5.2), `LinkController`, the first cut of §5.6 with their Apple views, `SubstrateLocal` | Fable |
| 5 | **Chat** onto the runtime: the composition proof; the console shell's sign-in (`ConsoleSignIn`, `ConsoleConnect`, `ServerStore`) onto `SubstrateAuth` | Astra |
| 6 | **Workouts** onto the link, the outbox, the runtime's journal order and the shared screens | Fable |
| 7 | **Android**: the pinned toolchain, the bridge (§5.5) and its contract tests, the generator, `kit-core`, `kit-ui`, `kit-device`, the gallery, and what the spike did not reach (§3.2) | Astra |
| 8 | **Substrate Tasks**: kinds through the kind review, `apps:new`, both platforms | Fable |
| 9 | **`apps:release` and `apps:site`**, dry run end to end | Astra |
| 10 | Screenshots from the gallery and the simulators, the pull request | |

Steps 2 to 4 land in order, and the runtime's interfaces freeze with step 4;
7's toolchain and generator start after 3, and 9 after 2. One owner merges
and holds the shared files (`apps/xcode/`, `apps/gradle/`, `.mise.toml`,
`docs/terms.md`, the kit's public interfaces); every other step writes inside
its own app or module.

## 12. What this plan does not do

- Convert Sync, or give Chat, Workouts or the console an Android UI.
- Share the UI itself.
- Localize: presenters write English, and the string table comes with the
  first localized app.

## 13. Decision records

- [0222](../decisions/0222-swift-is-the-language-of-the-apps-shared-code-and-android-runs-it-through-the-swift-sdk-for-android.md):
  Swift is the language of the apps' shared code, and Android runs it
  through the Swift SDK for Android.
- [0223](../decisions/0223-a-screen-is-a-controller-over-a-pure-engine-and-presenter-and-android-receives-its-presentations-over-one-fixed-bridge.md):
  a screen is a controller over a pure engine and presenter, and Android
  receives its presentations over one fixed bridge whose Kotlin is
  generated.
- [0224](../decisions/0224-an-app-is-one-folder-named-once-by-its-app-json-and-the-kits-layering-is-checked.md):
  an app is one folder named once by its `app.json`, and the kit's layering
  is checked.

## 14. Later

What this round knowingly leaves, each for the change that needs it.

- **Release sign-in on Android.** A release's claimed callback, an App Link
  the site's `assetlinks.json` verifies, received through an Auth Tab (a
  Custom Tab where Auth Tab is missing). Until then a release refuses to
  sign in rather than fall back to loopback, which is §5.4's contract.
- **Chat and Workouts on `AppShell`.** Tasks and the template run under the
  kit's shell; Chat and Workouts keep their own hosts over the same link,
  because the shell lacks what they use: the password door, the link's
  `offers` and `checkInterval`, Change Access reporting its problem to the
  app, background work and badges, and a transport of the app's own.
- **Android UIs for Chat, Workouts and the console**, on the same kit (§1).
- **Sync's conversion** (§5.3): its own `JSON`, `LocalState`, `pushRows`
  and client code until then. The views kept for apps that hold their own
  session, named `*Sheet`, `*Section` and `*Banner` (`ReviewChangesSheet`,
  `AccountSection`, `AppStatusBanner`), retire with it, and so does Sync's
  `Components.swift`.
- **Device services not built** (§5.4): background work on Android
  (WorkManager, the headless start, a cold job in the runtime tests) and
  Updates on both, with an Android updater that reads the feed and shows
  §9's states.
- **The Records screen** (§5.6), from a kind's declaration and later the
  console's `recordview` records, for an app that shows records it does not
  own.
- **Tooling** (§7.2): `detekt`, the API-break check once the kit has a
  baseline, and `apps:build` once something runs every app's Apple side at
  once. An Apple Gallery app beside Android's.
- **Words.** `StubTransport` and its kin renamed for what they script, as
  `ScriptedServer` is; `LineStream`, which still says stream, a word the kit
  does not take; Workouts' `AccountRef` and its `.sync` command and effect,
  which say account and sync where the kit says repository and flush.
- **One way to ask first.** A question is asked three ways (the
  `confirmation` modifier over a presentation's `ConfirmationPresentation`,
  a settings row's `confirmation` through `ConfirmingButton`, and the
  Repository screen's own Sign Out question); the modifier is the one to
  keep.
- **The umbrella.** `SubstrateKit` and the defaults in
  `AppleDefaults.swift` go once each caller imports the libraries it uses
  and passes its device services in, as Tasks does.
- **Label casing per platform** (Astra's ruling on the Android review):
  platform-aware wording is a kit presentation and localization concern.
  Android receives authored sentence-case strings and Apple title case, the
  read and edit wording identical within a platform, and Compose never
  lowercases an arbitrary string.
- **Play distribution of Tasks** needs `SCHEDULE_EXACT_ALARM` (asked of the
  owner) in place of `USE_EXACT_ALARM`, which Play reserves for alarm and
  calendar apps.
- **The gallery's offscreen drawing** shows an on switch grey and a
  sidebar's selection black, where the device shows the accent; the
  pictures, not the apps, are wrong.
- **The APK's size.** ICU is most of it (§3.2); FoundationEssentials alone
  would do once the cores' formatters do without ICU.

## Review record

**Draft 2, 2026-11-03.** Fable (design, structure, apps) and Astra (bridge,
security, toolchain, release, migration) reviewed it. What changed:

- §5.1 claimed Workouts' shape and described another. The loop is now
  Workouts' actual one: command, enrichment, a pure replayable engine,
  effects decided outside it, a presenter with declared inputs (Fable).
- Composition was missing; §5.2 adds the environment, the link as root,
  state flowing down as events, keyed children, keyed effects with epochs,
  and makes Chat the proof (Fable).
- The bridge was four calls and a claim of no lifetimes. §5.5 now specifies
  the runtime start, handles with generations, the envelope, ordered streams,
  coalescing of presentations only, bytes as files, structured errors, the
  restricted model behind the generator and a protocol fingerprint (Astra).
- Security parity is a list of contract tests (§5.4), loopback is a local
  build's alone, and background work runs headless with link generations
  (Astra).
- The release is a staged transaction with activation last, a lease, kept
  feed URLs, a monotonic `versionCode` and the updater's states (Astra).
- Relocation is its own mechanical step, shared files have one owner, and
  the shared screens come before Workouts and Chat use them (Astra, Fable).
- Tasks no longer collides with the sample's words (Fable). The kit's words
  avoid the ones `docs/terms.md` already defines (Fable). Sync's code stays
  Sync's (Fable). The kit moves to Swift 6 (Fable). The first cut of screens
  and device services shrank, Style Dictionary and the WebAssembly aside
  went, and the facts in §2 were corrected (Fable, Astra).

**The Android spike, 2026-11-03** (§3.2), after draft three: the bridge
start binds the main Looper and caches the class, the environment carries
the locale, time zone and resource bundle, and the JNI thread rules are
written into §5.5.

**Step 4, as built, 2026-11-03.** Where the code settled other than the
text above says:

- The library order is §5.3's table as it now stands: `SubstrateRuntime`
  comes after `SubstrateAuth` (it holds the client in its environment),
  `SubstrateLocal` after it (the journal file implements the runtime's
  `JournalStore`), and `SubstrateScreens` and `SubstrateTesting` after
  both. `SubstrateKit` exports every library but `SubstrateTesting` and
  `SubstrateLocal`, whose outbox shares its names with WorkoutsSync's until
  step 6.
- A screen is one `Controller` protocol with the engine's, the presenter's
  and the effects' requirements on it, each pure but the enrichment and
  `perform`. An effect is a value a test compares; `perform` turns it into
  `Work` (once, a sequence, or cancel). The transient state moves through
  `noting` and `refusing`, the journal through `journaling` (`skip`,
  `append`, `restart`, `clear`), and the runtime's trace is what a test
  holds the order to.
- The environment carries the clock, ids, locale, time zone, resources,
  the client (read when an effect runs, never kept) and the device
  services, and grows by key: SubstrateLocal adds the repository's folder.
- A parent keeps one type of child, in its state by key; a child reaches
  it through `event(fromChild:_:)`, and the parent's presenter composes the
  children's presentations with its own state, so what both need is the
  parent's without a second input into the child.
- `LinkSnapshot` lives in `SubstrateRuntime`, since the runtime delivers it
  down every tree; `LinkController` makes it in `SubstrateScreens`.
- The watch, its follower and the line stream landed here (§5.4); the
  record cache keeps the watch's cursor.
- The Status screen generalises Workouts' `SyncStatus` with `StatusWords`
  (the app, the device, the package, what waits), so Workouts' own
  sentences come back through those words in step 6. The Account screen
  keeps §5.6's name and `AccountSection`'s.
- The fixtures are `ScreenFixtures`, made by the presenters themselves, and
  the gallery check is `Gallery.renderers` against them, under `swift test`.

**Step 4's review, 2026-11-03** (Fable), before Chat, Workouts, Tasks and
the bridge build on it. What changed:

- The link is the tree's root: `LinkController<App: LinkedApp>` keeps the
  app's root as its one child, so one runtime holds both as one
  presentation tree. The link's state makes the snapshot
  (`link(in:)`), and a new one reaches every controller under it inside
  the step that changed it; `Runtime.linkChanged` stays only for a root
  run without a link. What the app's events report (`LinkReport`: access
  ended, approval needed) is the link's own event.
- A child is `Equatable`, and a value that differs replaces the one
  running under its key, its state kept, so a parent hands a child the
  catalogue, the history index and the settings as part of its value. A
  command reaches a controller by a path of keys, at any depth.
- A controller has an event at open, `event(atOpen:)`, applied on the live
  path after the replay, so its effects run (a replayed session checked
  against the history, a Live Activity restored). A runtime opens on its
  own executor at its first call, so a journal is never replayed on the
  main actor.
- The outbox reports its state and counts (`Outbox.changes()`); the link
  follows them, and a write the app queues starts a flush wherever the
  link can send. A file at an older format is read through the app's
  `OutboxUpgrade`. The outbox's `group` is `batch`, written with
  Workouts' `session` beside it so a build before the kit's outbox reads it.
- The Status screen derives each package on its own, and says a change not
  kept (the outbox's save, a journal's write); a reason is a fact of its
  own, never run into the sentence. Settings rows and sections have typed
  ids, and the approved access is one row per scope. Account became
  Repository in the kit's types; `AccountSection` keeps the apps' word.
- `Controller`'s requirements are named on one rule: the three stages are
  verbs (`apply`, `present`, `perform`), the rest nouns for what they
  answer, `journaling(of:)`, `transient(after:applied:from:)`,
  `coalescing(of:)`, `event(atTick:)`, `event(forLink:)`, beside
  `effects(of:on:presented:)`. A shorter coalescing window asked later
  moves the deadline earlier. A presentation that does not encode reaches
  the bridge as its failure.

**The Android bridge's review, 2026-11-03** (Astra), before an app runs
on it. What changed:

- A message's body is recorded only when what it belongs to is known to
  carry no secret: a reply, chunk or error for a call nothing waits on
  (one that raced its cancel, a second reply) and a request that does not
  read keep no body, on both sides.
- The envelope is read and written with `JSONValue`'s own reader and
  writer and a coder over `JSONValue` (`ValueCoding`), never Foundation's,
  so a number keeps every digit past a `Decimal`'s and a `Double`'s; the
  round trip holds one of each.
- A handle's first presentation is never lost: Kotlin holds a message for
  a handle it does not know yet until the `open` it belongs to is
  registered.
- A message Kotlin does not take finishes what waited on it: a request's
  call fails with the JNI failure, and a presentation or a list fails the
  handle, which closes from Swift's side as Kotlin's `close` would.
- The transport cancels the OkHttp call the moment its caller is cancelled,
  while the head and the body are still coming. A body too large for a
  message spills under `noBackupFilesDir/bridge`, is deleted on every path
  (taken, refused, cancelled, cut short) and swept at start; a credential
  exchange (token, revocation, login, registration) never spills.
- `start` answers every failure, a full disk or a missing asset included,
  as the terminal `Failed`; connectivity's changes are never coalesced and
  one with no room ends the stream with `overflow`; the generator refuses
  coding written in an extension and a property wrapper; the Swift build's
  inputs are every local package it depends on and its script.
- A locale or time zone change reaches every open root
  (`environment`, `BridgeRoot.environmentChanged`), in order with its
  commands.

**Step 7's kit-ui and gallery, as built, 2026-11-03.** Where the code
settled other than the text above says:

- A runtime root's presentation crosses with each `KeyedList` in it as a
  reference, `{"$list": <its place in the tree>, "revision": n}`; the
  lists whose rows moved go first, whole then as deltas, at a revision of
  the crossing's own that only moves forward, and Kotlin draws a tree only
  once every list it names is at the revision it names.
  `Runtime.environmentChanged` presents the whole tree again, and a
  command is read by the exact reader and `ValueCoding`, which moved to
  SubstrateCore.
- The restricted model reads what the shared screens already were rather
  than reshape them: generic structs (`ActionButton<Action>`,
  `LinkPresentation<App>`), unlabelled payloads keyed `_0` as Swift writes
  them, `Date` as RFC 3339, `URL` and `UUID` as strings, dictionaries keyed
  by `ChildKey`, and types a configuration names opaque (a probe's whole
  answer), which Kotlin carries as JSON. A configuration may use another's
  declarations, so an app's golden generates only its own values.
- The design tokens are data in SubstrateUI; a Swift test writes them as
  kit-ui's `KitTokens.kt`. The gallery check on Android is Robolectric
  with Roborazzi, not Paparazzi, from the one fixture file Swift writes
  (`fixtures/screens.json`); the Gallery app draws the same fixtures made
  on the device and its instrumented test holds them to kit-ui.

**Confirmation round, 2026-11-03**, on draft three with the spike. Fable:
the loop's order is now the contract (journal before any suspension, a
finish that is two events, transient state beside the journaled state),
effects see the presentation and a `nextChange` becomes a `tick`, children
reach their parent by `child(key, event)` inside one presentation tree,
children are keyed by the client's id, epochs have two grains, coalescing is
per event, the link's snapshot holds no secret, the kit's words are defined
"in an app", and the shared screens now come before Chat and Workouts.
Astra: the transport may carry credentials and the bridge redacts them,
DNS keeps the unconfirmed absence, state on Android lives under
`noBackupFilesDir` and the link generation is persisted, the bridge starts
from `Application.onCreate` and checks every JNI call, close completes what
is outstanding, keyed lists carry revisions and are never coalesced, a
stream ends once, the lease is a conditional write with a fencing token,
activation is per app and rewrites the old feeds, and `versionCode` is
allocated under the lease.

**Steps 2 and 3, 2026-11-03** (Astra). The relocation named Workouts'
package folder for its package, so two apps' cores cannot collide in one
Xcode project, and an app came to be named once in `app.json`, which every
reader parses without a dependency. The kit split kept every digit of a JSON
number and every key's place in a request body, read a loosely decoded
`true` as a `Bool` off Apple systems, and left a session store's Keychain
settings to its storage alone; `lint:apps` read Swift 6's access-level and
attributed imports, and `app.json` told the packages an app publishes from
the ones it asks for (`publishes`, `asks`).

**Step 5, Chat, 2026-11-03** (Astra). Chat moved onto the runtime whole
(7c0e67f3): the list as the app's root under the link, each conversation a
keyed child, its seven rules the runtime's behaviour with a test each, and
the console shell's sign-in onto `SubstrateAuth`. The review found a chat
the device held nothing of opening empty (e68853ee), a folder a save could
reach after a sign-out, background runs not checked against the link
generation and a failed transcript read never asked again (0515e89a), and
notifications Chat kept outside the kit; now a session reaches its folder
only through the lease its open answered, opening a chat takes back its
pending notifications as well as its delivered ones, and Chat runs on the
kit's notification service and signals (c074355f).

**Step 6, Workouts, 2026-11-03** (Fable). The session became a controller on
the runtime with the kit's journal and a Finish that is two events, and the
sync run the app's root under the link with the kit's outbox (289ab41a). The
review found a retried Save that could read the last one's problem,
`syncing` and `starting` left set where nothing ran, and flags the engine
kept to say what the last event did; each Save now carries a receipt, the
effects compare the state before and after, the cue, the cover and the
scroll target are signals, and the alert, its category and its tap go
through the kit's notifications (37e5fa64).

**Step 8, Tasks, 2026-11-03 to 2026-11-04** (Fable, twice). The first review
found Tasks repeating what any app on the kit would, the detail showing List
and Priority twice, a reminder's tap lost at a cold launch and a write
discarded as stale still drawn as landed. The kit took the repeated parts
(0921377d: linked records, `AppShell`, the App menu's views, Settings
composed, offscreen drawing) and Tasks moved onto them with each fact shown
once (62d20085, 3ab5e600). The second review found an app over linked
records still writing the records' stages and a stopped read lifting on any
change; `LinkedRecordsApp` now answers those stages and a stop keeps its
reason (c16cbc75, 439a3f1e), an app hands its shell the folder and outboxes
it already keeps (8267b952), and the template became an app built and tested
as Tasks is (9ecd5182, 65ff4335).

**Step 9, the release, 2026-11-03 to 2026-11-04** (Astra, twice).
`apps:release` landed whole (c1509f3a). The first review found writes
fenced only by the publisher's own checks and a stage a rerun could resume
from another release folder or a dry run; every write became conditional on
what the run read under its lease, a stage records what it was made for and
versionCodes are reserved before the build (b899d8ed, a25c4af3). The second
found a release that could ship without a claimed callback, a write's time
bound read before its file was hashed, and a dry run that read the release
key; now every release needs its site, a write is bounded at the moment it
is sent, and a dry run signs with a throwaway key (99113850).

**The first run on devices, 2026-11-03.** Sign Out left the app's token for
the next launch to revoke and said so on every Sign Out; the link's effect
now revokes it (9ebf9fad). A chat whose run failed before it was opened
showed only its question; it says why (87973c98).

**Integrating main, 2026-11-03** (c31e694d). Main rebased the domains stack
this branch is built on; the merge took main's later commits and ported its
Apple changes to where this branch moved them: the `_substrate` record's
whitespace and the set-up words into the kit, and the outbox upgrade that
uploads a phone's history instead of deleting it into the kit's
`OutboxUpgrade` and Workouts' own.

**Tasks on Android, 2026-11-04.** Browser sign-in and notifications became
device services on Android (c5f639fc), TasksKit's presentations and commands
crossed the bridge (425b6abf) and the app was drawn in Compose (73475e23).
Running it on an emulator found the connect screen's commands refused
(33fa30f5), a sign-in lost while Android froze the app in the browser, a
link back the browser would not open and late reminders (5fca562e), the
kit's own library shipped beside the app's (8e3017be), and the gallery's
index not written (cb468bbc). Astra's review found that a malformed loopback
request could crash the listener, an unstamped release build chose
loopback, the sign-in had no overall deadline for its callback, a
notification tap could be forged through the exported launcher, and debug
builds allowed cleartext to every host; the pass after this record fixes
each.

**The final reviews, 2026-11-04.** Fable read the whole branch: an app's
records event and effect were found by reflection, and an app naming its
case otherwise went unread (6d8a34f7); dead views, words and coders went
(4e8093d4); tests now assert what they claim and wait for the runtime's
effects to end rather than for time (f57bf567, 35693f79, 2805a8d4), with a
time limit on every suite (9a201fc0); a refused write, a package and
Connect's footer carry their labels (efaaf669); the words moved to the
kit's (5162aa77, 54770667, 03bc1fda); and every file holds one concept
(1dab9c36, 27e77994), ending with 59477e0b. Astra's security sweep found a
malformed reply's spilled body left on disk (9e96ea7a), a release build
without its claimed callback signing in through loopback (88e573b2), Gradle
artifacts checked by version and not by bytes (91fa4c32), and the
deployment's names in the docs and tests of a public tree (80e6dce8); the
agent review gained the apps' contract (8db0436b).
