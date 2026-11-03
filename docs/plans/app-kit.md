# Plan: one app kit for every Substrate app, on Apple systems and Android

Status: proposal, third draft, 2026-11-03; based on the domains stack
(#1163 to #1167, [status](authorities/status.md)). Draft two was reviewed by
Fable and Astra; this draft answers both (review record at the end). Nothing
is built. The binding choices become decision records when the plan is
accepted (§13).

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
Intents itself. Each `Conversation` (649 lines) runs up to six tasks with
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

Workouts' shape, named once and used for every screen. The words are this
plan's and go into `docs/terms.md` in step 3 (§7.3); none is taken there.

```
command ──controller──▶ event ──engine──▶ state ──presenter──▶ presentation
   ▲        (enriches:                        │
   │        ids, now, lookups)                └─ effects(event, state) ──▶ work ──▶ event
   └──────────────────────── UI ◀─────────────────────────────────────────────────────┘
```

- **Command**: what the UI asks for (`tap Done on row 3`). The UI sends
  nothing else.
- **Controller**: the one writer of a piece of state. It turns a command
  into an **event** by adding what a pure function may not read: new ids,
  the current instant, lookups in caches. It runs on the kit's runtime, not
  on a UI thread.
- **Engine**: `apply(event, to: state) throws -> State`, pure. Folding the
  journal's events through it gives the same state every time.
- **Effects**: `effects(of: event, on: state) -> [Effect]`, pure, run only
  on the live path, never on a replay. An effect is work the runtime does
  through the client, the local store or a device service, and its result
  comes back as an event.
- **Presenter**: `present(state, inputs) -> Presentation`, pure. Its inputs
  are declared (catalogue, history, settings, the instant, the locale), so a
  test pins every one.
- **Presentation**: a value of strings, numbers, enums and ids that a view
  draws. It is the only thing a UI reads.

A **journal** is optional per controller: Workouts' session has one (its
events are its truth), Chat's conversation does not (the server is).

### 5.2 Composition

Fable's review found this missing, and it is the decision tens of apps
inherit.

- **Dependencies arrive in an environment value**: the client, the local
  store, the device services, the clock, the id source, the locale and time
  zone, and the bundle the resources come from (§3.2: none of the last three
  can be read from the process on Android). A
  controller is made with one, a test passes fakes, and nothing reaches for
  a shared instance.
- **The link is every app's root.** `LinkController` (today's generic half
  of Workouts' `SubstrateLink` and Chat's `AppModel`) owns the connection to
  one repository: the session and its revocations, the status check, the
  versions of the app's packages, connectivity, the outbox's state. Its
  presentation is the Connect, Access, Account and status screens (§5.6).
- **Shared state flows down as events.** A screen's controller does not
  hold the link; the runtime delivers the link's snapshot to it as an event
  (`linkChanged`), so a test drives it like any other input.
- **A parent owns its children's lifetimes.** Children are keyed (Chat's
  conversations by thread id) and live until the parent drops the key, so a
  stream survives navigation as Chat's does today.
- **Effects are keyed and cancellable.** Starting an effect with a key
  cancels the one running under it, and every result carries the epoch it
  was started in, so a late result is dropped. That is Chat's seven rules
  as the runtime's behaviour rather than each app's code.
- **Long-lived effects** (the watch, an agent turn's stream) deliver events
  until cancelled. A controller that receives many coalesces before it
  presents: Chat's 50 ms flush becomes a setting of the runtime.

**The proof is Chat, in step 4.** Its list becomes a controller with
conversations as keyed children; a turn's stream is a keyed effect; its read
epochs and run numbers are the runtime's epochs. If Chat needs rules the
runtime cannot express, the runtime changes before Workouts and Tasks build
on it.

### 5.3 The kit's libraries

One Swift package, `apps/kit`, in Swift 6 mode, built for iOS, macOS, Linux
and Android. A library depends only on those above it.

| Library | Holds | From |
| --- | --- | --- |
| `SubstrateCore` | one `JSONValue` (ordered keys, a number kept as its decimal text, so Workouts' `Decimal` holds); the record and its flat wire form; kind references; RFC 3339 times, ISO 8601 durations; `ExternalID`; errors and `explain`; kind names and labels; scope, grant and change words | `SubstrateKit` `Records/`, `Status/` words; WorkoutsCore `Codec/` |
| `SubstrateRuntime` | command, event, engine, effects, presenter, controller, environment, journal, keyed children, epochs, coalescing (§5.1, §5.2) | Workouts' `SessionController`, `SessionJournal`; Chat's ordering rules |
| `SubstrateAPI` | the client over the transport service: record calls with `ifVersion`, lists, filters, blobs, webhook delivery, line streams, the **watch** (cursor, generation, relist on 410 or `compacted`), agent turn streams; discovery, versions, the requirement; the locator over the DNS service | `SubstrateKit` `API/`, `Server/`; Chat `API/Watch.swift`, `AgentEvent.swift` |
| `SubstrateAuth` | manifest and details, PKCE, `authorizeURL`, the callback check and code exchange, `Session`, the session rules over the secure-store service, pending revocations, the status check | `SubstrateKit` `Auth/`, `Status/` |
| `SubstrateLocal` | the per-repository folder (format version, atomic writes, unreadable files set aside, sizes, budget, eviction); the **outbox** (Workouts', with a group key in place of `session`; its routine helpers stay in the app); the **record cache** the watch keeps current; reading the version of a package the repository holds | Workouts `Outbox`, `RemoteCatalog`, `TrainingPackage` (read side); Chat `DiskCache` |
| `SubstrateScreens` | the shared screens: `LinkController` and each screen's engine, presenter and fixtures (§5.6) | `SubstrateKit` `UI/` (the logic now inside its views); Workouts' `SyncStatus` |
| `SubstrateDevice` | the device-service protocols and their request and answer values (§5.4) | new |
| `SubstrateTesting` | a scripted server, a fake of every device service, a fixed clock and id source, fixture presentations | Workouts' and Chat's `ScriptedServer` |
| `SubstrateUI` (Apple) | SwiftUI views of the shared screens; the design tokens (§7.4); the `@Observable` wrapper that republishes a controller's presentations on the main actor | `SubstrateKit` `UI/`, Chat's `Components.swift` |
| `SubstrateDeviceApple` (Apple) | the device services on Apple frameworks | `WebAuthenticator`, `LoopbackReceiver`, `DNSServiceTXTResolver` and `SubstrateResolv`, the Keychain half of `SessionStore`, Chat's `Notifier` and background code |
| `SubstrateBridge` (Android) | the JNI surface (§5.5) | new |

The Android side is a Gradle build of three modules: `kit-core` (the Swift
libraries as `.so` files with `SubstrateBridge` and the generated Kotlin, one
AAR), `kit-ui` (a composable per SwiftUI view, fed the same presentations)
and `kit-device` (the device services in Kotlin).

Sync is not converted. It keeps its own `JSON`, `LocalState`, `pushRows` and
client code until it is, and the kit does not take them; that duplication is
listed in `apps/sync/README.md` as the work its conversion does.

### 5.4 Device services

A protocol in `SubstrateDevice`, an Apple implementation, an Android
implementation and a fake. Four rules:

- A request and its answer are values. An implementation holds no callback
  into the core.
- An answer is typed by outcome, never collapsed: DNS answers records, no
  record, or a failure that stops discovery, as `ServerLocator` tells them
  apart today.
- A long-lived source is a stream with an explicit end, error and cancel
  (§5.5).
- A service that is a screen (camera, code scanner, photo picker) is a UI
  **component**: the UI shows it and sends the controller a command with its
  result.

First cut, the ones the converted apps and Tasks use:

| Service | What the core sees | Apple | Android |
| --- | --- | --- | --- |
| Transport | request → head, then body or ordered chunks; cancel | `URLSession` | OkHttp |
| DNS | TXT records, no record, or failed | dnssd, libresolv on a negative answer | `DnsResolver.rawQuery` (API 29+), TXT parsed in Swift |
| Secure store | read, write, delete by key; missing vs unavailable | Keychain, access group | Keystore-wrapped Tink keyset, ciphertext in DataStore |
| Browser sign-in | open this URL; the callback's query, or cancelled | `ASWebAuthenticationSession`: claimed callback in a release, loopback in a local build | Auth Tab with a verified App Link in a release (a Custom Tab where Auth Tab is missing), loopback in a local build |
| App folders | data, cache; protected until first unlock; excluded from backup | `FileManager`, data protection | `filesDir`, `noBackupFilesDir`, `cacheDir` |
| Connectivity | online, offline, expensive | `NWPathMonitor` | `ConnectivityManager` |
| Notifications | permission; schedule, cancel by id; a tap becomes a command | UserNotifications | `NotificationManager` |
| Background work | named jobs with constraints; `run(job, budget)` in a cold process | `BGTaskScheduler` | WorkManager |
| Updates | a newer build on the feed; install it, with each step's state | Sparkle (Mac), the manifest link (iOS) | the feed, then the package installer |

Ongoing activity, location, health, contacts and calendars land with the
app that needs them on Android.

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
- A token never appears in a presentation, a WorkManager input, a bridge
  message other than the secure store's own, or a log.
- A release signs in only through the claimed callback; a failed App Link
  verification is a failure, never a fallback to loopback. Loopback is a
  local build's client id and nothing else, bound to the loopback address,
  with PKCE, `state` and `iss` checked as `OAuthClient` does. A callback that
  arrives after the activity was recreated completes the exchange it belongs
  to, or is refused; it never starts another.

**Background work runs headless.** A job starts the runtime without an
activity, opens the repository's folder, reads the current session at run
time (never from its inputs), checkpoints through the outbox, and stops when
cancelled. Every job names the link's generation it was scheduled under, and
a job from before a sign-out does nothing. It runs in the app's own process,
because actors do not coordinate file writes across processes.

### 5.5 The Android bridge

Values cross as JSON messages over a fixed JNI surface: the bridge never
changes when an app adds a controller.

**The surface**, written in Swift with `@_cdecl`:

- `start(config)`, once per process: records the `JavaVM`, takes global
  references to the Kotlin receiver (cached at `JNI_OnLoad`, since a Swift
  thread sees only the system class loader), binds libdispatch's main queue
  to the main Looper, takes the device's locale, time zone and resource
  folder, and compares the protocol fingerprint (below) with the one Kotlin
  was built with, refusing to start on a mismatch.
- `post(message) -> reply`: every message from Kotlin, synchronous only in
  that it validates and queues; the work runs on Swift's executors.
- Swift calls one Kotlin method, `deliver(message)`, from a thread it has
  attached to the JVM, keeping that call short, inside a local frame, with
  the string as UTF-16; a thread-local destructor detaches the thread when
  it ends.

**The messages** are one envelope: `{v, op, handle, id, seq, body}`.

- `open` (a controller, its environment's settings) answers a **handle**: an
  id and a generation. `close` is idempotent. A message for a closed handle
  or an old generation is dropped and counted.
- `command` sends a command to a handle. `presentation` delivers a handle's
  latest presentation; presentations are snapshots, so a slow consumer gets
  the newest and the ones in between are skipped, after encoding is
  coalesced on the Swift side.
- A long collection in a presentation (a transcript, a task list) is a
  **keyed list**: the first message carries it whole, later ones carry the
  rows inserted, changed and removed by id, so a streamed reply re-encodes
  one row, not the transcript (§3.2). Kotlin keeps the list; a gap in the
  sequence asks for the whole list again.
- `request` and `reply` carry a device-service call by id, answered exactly
  once; a reply to an unknown or answered id is refused. `chunk`, `end`,
  `error` and `cancel` carry a stream by id and sequence number, in order,
  with a bounded queue whose overflow cancels the stream with an error the
  controller sees. Watch rows and device answers are never coalesced.
- Bytes (a blob, an image) never travel in JSON: they are files in the app
  folders, and messages carry their paths.
- `error` is structured: malformed message, unknown controller, a Swift
  failure, a Kotlin exception. Nothing throws across the boundary and
  nothing crashes the process.

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
| Connect | address or repository name, finding the server, the door (browser, password, too old), unrevoked tokens | `ConnectView`, `ServerTooOldView`, `UnrevokedNotices` |
| Access | the status banner's issues; the approval review; access ended | `AppStatusBanner`, `ReviewChangesSheet`, `AccessEndedView` |
| Account | server, repository, access, change access, sign out | `AccountSection` |
| Status | a sentence (up to date, sending, offline, needs approval, signed out), the writes waiting and the ones refused, each with retry and discard | Workouts' `SubstrateSection`, `SyncStatus` |
| Settings | sections and rows as data (toggle, choice, value, link, destructive action), with the account, status, storage and about sections every app has | each app's own |
| Problems | one problem in words, and what fixes it | `ProblemBox`, Chat's `ProblemLine` |
| About | the build, the channel, an update when one is offered | each app's footer, `Updater.swift` |

**With Tasks**: **Records** (a list row and a detail from a kind's
declaration and labels, references as pills, read and edit with the same
labels; from the declaration in this round, from the console's `recordview`
records (0166) later, so the two do not drift), **Storage** (each area's
size and share of its budget, a clear action that never drops unsent writes
silently) and the **App menu** (sections as data, drawn as a tab bar on a
phone and a sidebar on iPad, Mac and a wide Android screen).

## 6. Where things live

By app, not by platform: one app is one folder.

```
apps/
  README.md            how the apps are built, tested and released
  kit/                 the Swift package of §5.3, README per library, android/ (Gradle) inside
  xcode/               project.yml (includes each app's apple/targets.yml), shared Support/
  gradle/              settings.gradle.kts (includes each app's android/), build-logic/
  console/             app.yaml, apple/
  chat/                app.yaml, core/, apple/, docs/
  workouts/            app.yaml, core/, apple/ (app, widgets), package/, scripts/, docs/
  sync/                app.yaml, apple/ (app, share target): moved, not converted
  tasks/               app.yaml, core/, apple/, android/, package/
  template/            what apps:new copies
```

- **`app.yaml` names an app once**: id, display name, bundle id and Android
  application id, platforms, the Info.plist and manifest keys of its client
  id and redirect, the packages it publishes from `package/`, its scopes and
  package details, its icon, its feed folder. It replaces
  `apps/apple/scopes.json`: the server's first-party reach test reads the
  scopes from it, and the app's Swift manifest is held to it by a test, as
  `ReachTests` and `CoachTests` hold the manifests to `scopes.json` today. The
  Apple project, the Gradle settings, the site and the release read it.
- **`core/`** is a Swift package of the app's controllers, engines,
  presenters and model, with no UI framework. `apple/` and `android/` are
  thin UIs and the app's own device code.
- **Nothing in `apps/<app>` imports another app**, and each app's scripts,
  docs and locks live under it.
- **What does not move**: bundle ids, Keychain services and access groups,
  App Groups, and every on-device path (`Application Support/Workouts/`,
  `SubstrateChat/`), so an installed app upgrades in place.

## 7. Code quality, held by tools

### 7.1 Layering, checked

`mise run lint:apps` reads every `Package.swift`, `targets.yml`, Gradle
module and `app.yaml`, and refuses:

- a dependency that points up §5.3's list, or from one app to another;
- a `core/` or a kit library outside the Apple and Android rows that imports
  anything but Foundation, FoundationNetworking, Observation, swift-crypto
  and the kit;
- a Kotlin file outside `kit-device` or an app's `android/` that touches an
  Android service;
- an `app.yaml` the generators cannot read, or an app without one;
- a third-party package not listed, with its reason, in `apps/README.md`
  (today: Sparkle, swift-crypto, swift-syntax for the tool only, OkHttp,
  AndroidX, kotlinx.serialization, Tink).

The kit and every `core/` build and test on Linux in CI and cross-compile for
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

### 7.3 Conventions

- Swift 6 language mode and complete concurrency checking everywhere; the
  kit leaves Swift 5 mode in step 3.
- Swift Testing. A controller is tested through its engine, effects and
  presenter, by value, with the clock, ids and locale pinned (presenter text
  depends on the locale today: `Format/Formatters.swift`).
- One concept per file, named for it; no `utils`, no `types.swift`.
- The words are `docs/terms.md`'s. Step 3 adds an apps section defining
  command, controller, engine, effect, presenter, presentation, journal,
  link, device service and outbox, and none of them takes a word the table
  already uses: not adapter, mirror, stream, sync, feature, group or account.
- Each library and each app has a README saying what it holds.
- `mise run apps:new <id>` scaffolds `apps/<id>/` from `apps/template/`;
  CI runs it and builds the result, so the template cannot rot. Substrate
  Tasks is the first app it makes.
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
  live.

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
  and the emulator (the emulator reaches the host at `10.0.2.2:8080`).
- **Debugging** Swift on Android has no breakpoints in Android Studio, so
  logic is debugged on the host where its tests run.
- **Tasks**: `apps:build`, `apps:test`, `apps:test:linux`, `android:build`,
  `android:test`, `android:smoke`, `apps:gallery`, `apps:new`.

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
4. **Activate** last, with one `release.json` per app that names every
   platform's build, written after their artifacts. A failure before this
   step leaves the previous release current, and a rerun with the same
   build resumes.
5. **One publisher at a time**: a lease object in the bucket, taken before
   step 3 and released after step 4.

The feeds the installed apps read today keep being written, so an app finds
the release that moves its feed. `SUBSTRATE_APPS_RELEASE_DRY_RUN=1` runs
every step against `build/`, the site included, and changes no lock file.

**Android specifics.** `versionCode` is the minutes since 2026-01-01 at the
build's UTC time, checked against the highest published one, so it rises
across channels and reruns. The signing key is made once, kept by the owner
as a secret outside the tree, and its fingerprint is what `assetlinks.json`
names; losing it means every install is replaced by hand, as losing
Sparkle's key does on the Mac, and `apps/README.md` says how to rotate it.
The updater shows each state: a newer build, permission to install from this
app (`REQUEST_INSTALL_PACKAGES`), the system's confirmation, cancelled,
failed.

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
  `priority` on the same ladder; a `list` holds tasks the way the sample's
  `project` does. The kinds go through the kind review before they ship.
- **What it exercises**: sign-in and approval, the package version, the
  record screens, offline writes through the outbox, live updates through
  the watch, due-date reminders through notifications, background work,
  settings, status and storage, the app menu.
- **Not in the first cut**: sharing, location reminders, attachments, QR
  codes.

## 11. The work, in order

| Step | What | Review |
| --- | --- | --- |
| 1 | This plan | Fable and Astra; a confirmation round |
| 2 | **Relocation, mechanical**: the layout of §6 with `git mv` and path fixes only (`apple.sh`, `.mise.toml`, the scripts, `internal/catalog`'s reach test, `#filePath` goldens, XcodeGen paths, docs links), `app.yaml` beside `scopes.json` | Astra |
| 3 | **The kit**: the split into §5.3's libraries behind compatibility exports, Swift 6 mode, `SubstrateRuntime`, the duplicates retired, `lint:apps`, the terms, the Linux job | Fable |
| 4 | **Chat** onto the runtime: the composition proof (§5.2); the console shell's sign-in (`ConsoleSignIn`, `ConsoleConnect`, `ServerStore`) onto `SubstrateAuth` | Astra |
| 5 | **The shared screens' first cut and `SubstrateLocal`**, with the Apple views | Fable |
| 6 | **Workouts** onto the link, the outbox and the shared screens | Astra |
| 7 | **Android**: the pinned toolchain, the bridge and its contract tests, the generator, `kit-core`, `kit-ui`, `kit-device`, the gallery, and what the spike did not reach (§3.2) | Astra |
| 8 | **Substrate Tasks**: kinds through the kind review, `apps:new`, both platforms | Fable |
| 9 | **`apps:release` and `apps:site`**, dry run end to end | Astra |
| 10 | Screenshots from the gallery and the simulators, the pull request | |

Steps 2 and 3 land in order; 7's toolchain and generator start after 3, and
9 after 2. One owner merges and holds the shared files (`apps/xcode/`,
`apps/gradle/`, `.mise.toml`, `docs/terms.md`, the kit's public interfaces
once step 3 freezes them); every other step writes inside its own app or
module.

## 12. What this plan does not do

- Convert Sync, or give Chat, Workouts or the console an Android UI.
- Share the UI itself.
- Localize: presenters write English, and the string table comes with the
  first localized app.

## 13. Decision records

When the plan is accepted:

- Swift is the language of the apps' shared code, and Android runs it
  through the Swift SDK for Android.
- A screen is a controller, engine, effects and presenter; a UI reads
  presentations only, and Android receives them as JSON over a fixed bridge
  whose Kotlin is generated.
- An app is one folder named once by its `app.yaml`, and the kit's layering
  is checked.

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
