# SubstrateKit

What every app that speaks to a substrate shares, as one Swift package of
libraries: which server it is and whether it is new enough, the requests and
their errors, signing in through the app authorization flow, keeping the
session, asking whether the app is still set up, and the screens for all of
it. Beside that, the record and its values as the console draws them, the
loop every screen runs, what an app keeps on the device, and the bridge
that carries all of it to Android. The console's shell (Substrate),
Substrate Sync, Substrate Chat, Substrate Workouts, Substrate Tasks and the
template `mise run apps:new` copies use it; an app's Apple target adds it as
a local package (`project.yml`'s `packages:`), and an app's core as a path
dependency in its `Package.swift`. The design it implements is
[docs/plans/app-setup.md](../../docs/plans/app-setup.md), and its layout is
[docs/plans/app-kit.md](../../docs/plans/app-kit.md)'s §5.3.

## The libraries

Each library depends only on the ones above it in this table, which
`mise run lint:apps` holds. The portable ones (every one above the Apple rows)
import Foundation and the kit alone, and build and test on iOS, macOS and
Linux (`mise run apps:test:linux`), as Android's Swift does; the Apple ones
compile to nothing elsewhere, and the Android ones build everywhere with
their JNI half on Android alone. The package is in Swift 6 language mode.

| Library | What it holds |
| --- | --- |
| `SubstrateCore` | `JSONValue` (a number keeps its decimal text: `JSONNumber`, read as a `Decimal`, a `Double` or an `Int`; an object its key order: `JSONObject`) with `DecimalText`; `Record`, `FlatRecord` (the record as the REST API speaks it) and `CodecError`; `KindReference`, `RecordReference`, `KindDeclaration`; `Timestamp` (RFC 3339) and `ISODuration`; a kind's and a property's name in words (`KindNames`, `kindDisplayName`, `propertyLabel`), its glyph (`KindGlyphSpec`, `KindHue`), `ValueShape` and `valueWords`; `CivilDay` (a `date` property's day), `Due` (a day, or a moment with its day), `Position` (a record's place among its neighbours as the shortest decimal between them, spread out where a gap runs out); `ServerAddress`; the errors (`ServerError`, `TransportError`, `Redirected`, `SessionExpired`, `InsufficientScope`, `LoginRefused`, `OAuthRefusal`) and `explain(_:app:server:)`; `Grants` with `GrantKind` (a kind as a scope is held to it: its purpose and the traits its declaration binds), `ScopeWords` and `ScopeKind`; `ValueCoding` (any `Codable` to and from a `JSONValue` with every digit kept, a `Date` as RFC 3339, a `URL` and a `UUID` as strings: what crosses to Android, either way) |
| `SubstrateDevice` | the device services' protocols and their values: `HTTPTransport` with `URLSessionTransport`, which never follows a redirect, whose `lines(for:)` reads an answer line by line as a `LineStream`; `SessionStorage`, the secure store, every call of which may suspend, with `StoredItem` (missing told from failed), `SecureStoreRefused` and `SecureStoreNotRemoved` (a removal that failed, the item still there), and `ImmediateSessionStorage` for a store that answers on the calling thread; `BrowserSignIn` with `SignInRoute`, `SignInRedirect`, `SignInAnswer` and `SignInCancelled`; `TXTResolver`, whose `TXTAnswer` keeps records, a confirmed absence, an unconfirmed absence and a failure apart; `Connectivity` with `NetworkPath`; `AppFolders`; `Notifications` with `NotificationPermission`, `NotificationRequest` (`fireAt` nil shows it at once; `showsWhileActive` false keeps it quiet while the app is in front), `NotificationCategory`, `NotificationAction` and `NotificationTextInput` (the categories an app registers through the service), and `NotificationResponse` (a tap, an action or a dismissal, read from `responses()`), and `pending(prefix:)` for an app that schedules a whole plan and takes back what it no longer holds; `OngoingActivities` with `OngoingActivity` (a Live Activity's content as JSON the app's widget side reads); `BackgroundWork` with `BackgroundJob` (its `payload` handed back on the `BackgroundRun` it becomes, such as the repository and link generation it was scheduled under), `BackgroundRun`, `expirations()` (the runs the system takes back) and `ContinuedProcessing` |
| `SubstrateAPI` | `SubstrateClient`: `Timeout` tiers, `Retry-After`, `segment(_:)`, records, lists (`ListQuery`, `RecordPage`, `listAll` and `walk`, which start over once on a `410 compacted`), blobs, webhook deliveries, parked deliveries, the password door; `RecordReader`, `HTTPRecordReader` and `RecordDirectory`; `Discovery`, `AuthorizationServerMetadata` and `ServerMismatch`, `ServerVersion`, `ServerRequirement`, `ServerProbe`; `DomainRecord` and `ServerLocator` over a `TXTResolver`; `RecordWriter`, the record calls an outbox sends through; `implementors(of:)`, a trait's implementors the token reads, each a `KindInfo` with its declaration; the watch: `WatchFollower` (a cursor and its generation, heartbeats, a dropped connection opened again, a relist on a 410 or a `compacted` frame, and `recovers`, one recovery from it a minute) with `WatchFrame`, `WatchRow`, `WatchCursor`, `WatchEvent` and `WatchEnd` |
| `SubstrateAuth` | `AppManifest` (with `asking(_:)`, the scopes an app asks for beside its own, and `beyond(_:)`, what a session holds beyond them), `KindLabel`, `DerivedID` (a UUIDv8 of a name's SHA-256, so every device names one record alike), `AuthorizationDetail` and the check's wire shapes with `checkAuthorization`; `PKCE` (SHA-256 from CryptoKit, or swift-crypto where it is missing); `OAuthClient`, whose `signIn` takes the device's `BrowserSignIn` and refuses a release build without its claimed callback (`UnclaimedRedirect`), `TokenResponse`, `RepositoryMismatch`; `Session` and its `revoke`; `SessionStore`, its pending revocations and its transitions, over a `SessionStorage`, with `SessionUnreadable` and `RevocationsUnreadable` (a session or a queue the store could not read, never taken for none); `AppStatusCheck`, `AppStatus`, `AppIssue`, `Change` and its words, `AppStatusModel` |
| `SubstrateRuntime` | the loop every screen runs (below): `Controller`, `Runtime` with `PresentationNotEncoded`, `AppEnvironment` with `AppEnvironmentKey`, `ClientSource` and `DeviceServices`, `Enrichment`, `PresenterInput`, `Journaling`, `JournalStore`, `Coalescing`, `Work`, `Signal` (a value the UI acts on once, with a serial), `ChildKey`, `LinkSnapshot`, `KeyedList` with its `KeyedListDelta`, `RoutedCommand`, `RuntimeTrace`, `RuntimeClock`, `IDSource`; `KeyedListCrossing` (a presentation's keyed lists carried apart, each as a `KeyedListReference`, in a `PresentationFrame` of `CarriedList`s) and `KeyedListReceiver` |
| `SubstrateLocal` | what an app keeps on the device for one repository: `RepositoryFolder` (a format, or `adopting:` a repository's folder an app laid out before the kit, with nothing set aside and no marker, or `app:` the app's own folder, which no repository owns, laid out the same way; atomic writes, `SetAsideFile`s listed, `AreaUsage` per `FolderArea`, eviction by age and budget), `JournalFile` (in the journals' area, or the one an app kept its journal in), the `Outbox` (Workouts', with a `batch` where it had a session, and a restorable `delete` beside the `purge`; `changes()` reports each `OutboxSummary`, and an `OutboxUpgrade` reads a file an older build wrote, keeping no version, chain or guard of a record its respell moves; a file the device could not read is never written over, and every change is refused, `unavailable` saying why, until a read again succeeds), `RecordCache`, `HeldPackage`, `LinkGeneration`; `RecordFold` (the server's records with the outbox's waiting writes laid over), `LocalWrite`, `RecordWrites` (an owner's create, change, edit on the version seen and restorable delete, each in its record's batch), `WriteLine` (the order writes reach the outbox in), `SettingsFile` (an app's settings at the folder's root); `Outbox.landed` names the writes the server took, so a reader tells a landed write from one discarded; the folder joins the environment as `environment.folder` |
| `SubstrateScreens` | the shared screens as engines, presenters and fixtures, each label written here: `LinkController` (the session and its revocations, the status check, the package versions, connectivity, the outbox; a `LinkSnapshot` with no secret) over the app's root (with one `outbox`, or `outboxes:` for an app that keeps one per repository; the packages read again after each set up check), a `LinkedApp` that tells it a `LinkReport`, `AppMenuPresentation` (an app's sections as data: a tab bar on a phone, a sidebar on a tablet and a computer), `ConnectController`, `AccessPresenter`, `ReviewPresenter`, `ServerTooOldPresentation`, `RepositoryPresenter`, `StatusPresenter` with `LinkStatus`, `PackageStanding` and `StatusWords` (`local`, an app's own signed-out sentence; `footer` and `refusedFooter`, where it writes other than as each change is made), a refused write's kind `label` and its `status` as two facts, `SettingsPresentation` with `SettingsPresenter`, `SettingsValues` and the rows' `SettingsRowID` (a row may carry the `confirmation` its action asks first: Sign Out's), Storage as each area's size and one total against the budget, `ProblemPresenter`, `AboutPresentation`; `LinkedRecords` with `LinkedRecordsState`, `ReadPhase` and `WatchStop`, and `LinkedRecordsApp` with `LinkedRecordsEvent`, `LinkedRecordsEffect` and `LinkedRecordsHolding` (below); `DueWords` (a due day or moment in words, and its tone); `SettingsPresenter.screen` and `SettingsRoute` (every app's Settings in one order, each row's command to what answers it); `ConfirmationPresentation`, `EmptyPresentation`, `NoticeLine` (a line above a column, and the Status screen's `notice` for a refused write), `BuildFacts`; `ScreenFixtures`, every screen in the states an app shows it in; every presentation and command `Bridged`, so its Kotlin is generated |
| `SubstrateTesting` | the fakes tests use: `StubTransport` and `ScriptedServer` (scripted servers, by route and by rule), `ScriptedLines` (answers read line by line), `MemoryStorage` (its reads, writes and removals failed on demand), `TableResolver`, `ScriptedBrowser`, `ScriptedConnectivity`, `ScriptedBackground`, `RecordedNotifications`, `RecordedActivities`, `TemporaryFolders`; and the runtime's harness: `ControllerHarness` with `TestClock`, `SequentialIDs` and `MemoryJournal` (its reads and writes failed on demand) |
| `SubstrateDeviceApple` | the device services on Apple frameworks: `KeychainStorage`, `WebAuthenticator` (`ASWebAuthenticationSession`, the claimed callback or the loopback redirect), `LoopbackReceiver`, `DNSServiceTXTResolver` and the `SubstrateResolv` C target behind it, `PathMonitor` (`NWPathMonitor`), `ApplicationFolders`, `SystemNotifications` (`UNUserNotificationCenter`, with the kit's one delegate, set by `installDelegate()` before the app finishes launching: it keeps a request quiet in front where it asks, and hands every response to `responses()`, the launch's tap kept for the first reader; `badges: true` asks for the icon's number with the permission), `LiveActivities` over an app's `OngoingActivityAttributes` (ActivityKit, iOS), `BackgroundTasksService` (refresh jobs, each one's payload kept in the defaults until its run, and `BGContinuedProcessingTask`, iOS) |
| `SubstrateUI` | the renderers of SubstrateScreens' presentations (`ConnectScreen`, `AccessBannerView`, `AccessEndedScreen`, `ReviewScreen` and `ReviewSectionsView`, `ServerTooOldScreen`, `RepositoryScreen`, `StatusScreen`, `SettingsScreen` and `SettingsSectionView`, `AboutScreen`, `ProblemView`, `ChangeLineView`) with the design tokens, `Gallery` (each screen's renderer), `PresentationModel` (a root's presentations republished on the main actor; `onSignal` runs a handler once per `Signal` raised) and `AppEnvironment.apple`; the views apps call by their own initialisers, now thin wrappers over those (`ConnectView`, which runs a `ConnectController`, `AppStatusBanner`, `ReviewChangesSheet` and its `ReviewChangesSections`, `AccessEndedView`, `AccountSection` (the Repository screen for an app that keeps its own session), `UnrevokedNotices`, `ProblemBox`), `StatusBadge`; an app's shell: `AppShell` (with `changeAccess(adding:)`, the approval of scopes beside the manifest's; the session, a repository's folder, its outbox and record cache, sign-in and Change Access, notification responses and the time zone's moves as the app's commands, from the app's `Configuration`; below), `AppShellRoot`, `AppSettingsView`, `checkForUpdates`, `AppMenuRoot` and `AppMenuSplit` beside `AppMenuView` (the tabs), `LinkNotices`, the `confirmation`, `problem` and `childSheet` modifiers, `ConfirmingButton` (a button that asks its question from itself), `DeviceWords`, `BuildFacts.bundle()`, and `OffscreenDrawing` (a view to PNG on the Mac, as a phone's screen or a Mac window with its toolbar, for a gallery); and the record views (`KindGlyph`, `RecordPill`, `RecordSheet`, `RecordDetailView`, `RecordValueView`); `DesignTokens` (each tone's colour, each mark as an SF Symbol and a Material Symbol, the menu symbols by name, the type roles, spacing and radii, which a test writes as kit-ui's `KitTokens.kt`); `SessionStore`'s reads on the calling thread (`read()`, `load()`, `pendingRevocations()`, `data(account:)`, `string(account:)`, `setString(_:account:)`), over the Keychain, for an Apple app's code that cannot suspend |
| `SubstrateBridge` | the Android bridge (§5.5, [android/](android/README.md)): `BridgeRuntime` (`start`, `post`, what `deliver` is handed), the `Envelope` and its `Op`s, `Handle`s with generations, `BridgeRoot` and `RootRegistry`, `RuntimeRoot` (any `Runtime` as a root) and `RouteCommand`, `BridgeContext` (a handle's presentations, keyed lists and `DeviceChannel`, and its `AppEnvironment`), `ListMessage`, `BridgeFailure`, `Redaction`, `StartConfig` and `BridgeEnvironment`, `ProtocolFingerprint`; the device services over it: `BridgeTransport`, `BridgeSecureStore`, `BridgeTXTResolver`, `BridgeConnectivity`, `BridgeFolders`, with `DeviceRequest` and their answers; the JNI surface in `Android/`, compiled on Android alone over the `SubstrateJNI` C target |
| `SubstrateBridgeCheck` | `CheckRoot`, `Counter` and `Ledger` (a keyed list, the locale, a value kept whole), the roots the bridge's runtime checks open on a device; `GalleryRoot`, which presents every fixture of `ScreenFixtures` to Android's Gallery app |

`SubstrateKit` is the libraries an Apple app draws with under one import:
every one but `SubstrateLocal`, `SubstrateTesting` and the two Android ones,
with the Apple device services as the defaults the single module had: a
`ServerLocator(probe:)` reads DNS through DNS-SD, `OAuthClient.signIn`
without a `browser:` signs in through `WebAuthenticator`, and
`SessionStore(service:accessGroup:…)` keeps the session in the Keychain
through a `KeychainStorage` made of those settings. Sync's and Chat's app
targets import it, and so do Workouts' (in several files, beside
`SubstrateLocal` by name where it reaches the outbox and the journal) and
two files of the console's. An app's core, which must build for Linux and
Android (ChatKit, WorkoutsKit, TasksKit, TemplateKit), imports the portable
libraries it uses by name, `SubstrateLocal` among them where it keeps
anything on the device, and passes its device services in; Tasks' and the
template's Apple targets import the libraries by name as well, with
`SubstrateUI` for the views.

## The loop

A screen is a `Controller` a `Runtime` runs (docs/plans/app-kit.md,
§5.1 and §5.2). The UI sends a command; `events(for:in:)` enriches it with
what a pure function may not read (`context.mint()`, `context.now`, the
environment) into events; `apply(_:to:)`, the engine, applies each; the
journal, where `journal(in:)` names one, is written at once, with no
suspension between the engine and the write, as `journaling(of:)` says
(`.append`, `.restart`, `.clear`, or `.skip` for a step whose outcome waits
on work outside, which comes back as a second event); `present(_:)` draws
the state, the transient state, the children's presentations, the instant,
the locale and the time zone; and only then do `effects(of:on:presented:)`
start, on the live path alone, never on a replay. Where an effect turns on
what the event changed (an account that moved, a package that became
writable), `effects(of:on:from:presented:)` sees the state before it too,
so the engine keeps no flag saying what the last event did. An effect is a value;
`perform(_:in:)` turns it into `Work`, whose results are events through the
same loop. Under a `key(of:)`, starting one cancels the one running and its
late result is dropped; a reset or a close drops every result still on its
way. `coalescing(of:)` says whether an event presents now or within a
window, and a shorter window asked later moves the deadline earlier;
`nextChange(of:)` has the runtime deliver `event(atTick:)` then. A runtime
opens on its own executor at its first call: the journal folded, then
`event(atOpen:)` applied live, so its effects run. A journal the device
could not read (`JournalUnavailable`) is never written over: each event it
would keep is refused, after a read again, until one reads it, which folds
it as at open. The rule for the names, and every default, is in
`Controller.swift`.

A fact the UI acts on rather than draws (a haptic, a cover to open, a row to
scroll to) is a `Signal` in the presentation: the controller `raise`s it,
which moves its serial, and the UI acts once per serial
(`PresentationModel.onSignal`), so the same value raised twice acts twice and
a tree drawn again for something else acts not at all.

A parent keeps its children in its state, by the key the app minted
(`children(in:)`), each an `Equatable` value made from that state, so what
the parent hands a child is part of it and a changed value replaces the
one running; it folds the events of theirs it cares about
(`event(fromChild:_:)`, or `event(fromChild:_:after:)` where its rules
turn on what the child holds, as Chat's list drops a read for a thread its
conversation owns; `events(fromChild:_:)` where one of the child's is
several of its own, as a Tasks editor's save is the editor closed and the
write it made), and presents them in its own tree, which goes out once
per step. A child told a value that differs applies `event(replacing:)`, so
the change reaches its engine and its effects (the thread a conversation
shows moved: it attaches). An event for which `resetting(of:on:)` answers
true starts its controller over: its controller generation moves, its
effects are cancelled, every result still on its way is dropped, and its
children are made again from the state the event left (Chat's sign-out). A command reaches any controller by its path of keys
(`send(_:to:)`). The link is every app's root: `LinkController<App>` keeps
the app's root as its child, its snapshot reaches every controller under
it as `event(forLink:)` inside the step that changed it, and the app tells
it a `LinkReport` (a 401, a 403 `insufficient_scope`) from any event.

```swift
let link = LinkController(app: TasksApp(cache: cache, outbox: outbox, words: words, device: device, build: build),
                          manifest: manifest, sessions: store, outbox: outbox,
                          packages: ["substrate.reamde.dev/tasks": 1], words: words)
let model = PresentationModel(link, environment: .apple(transport: transport))  // SwiftUI reads model.presentation
model.send(TasksApp.Command.complete(taskID), to: LinkController<TasksApp>.appPath)
```

`ControllerHarness` (SubstrateTesting) runs a controller on a `TestClock`
with `SequentialIDs` and a scripted server, and keeps the runtime's trace,
so a test reads what was journaled, presented, published and asked for, in
order. Its `until` checks a condition each time the runtime moves, and
fails after a minute with no move at all (`SUBSTRATE_HARNESS_IDLE`), so
a loaded host slows a test and never fails it, or after ten times that in
all, so a runtime that keeps moving and never gets there (a livelock) ends;
`until(timeout:)` bounds the whole wait by its timeout. `untilIdle(since:)`
waits until every effect started after a mark (`effectsStarted`) has ended
and none waits for a presentation (`Runtime.activity`), moving the
`TestClock` to each sleep that ends within `advancing:` for work that
sleeps on it (`until(advancing:)` does the same for any condition), so a
test asserts what did not happen once nothing that could do it is still at
work.

## Linked records

An app whose screens show records of its own kinds keeps them with
`LinkedRecords` (SubstrateScreens), parameterised with the kinds, the
repository's `RecordCache` and `Outbox` and the app's `StatusWords`. It is
not a controller: the app's root keeps its `LinkedRecordsState` in its own
state as `linked`, carries its events and effects in its own
(`case records(LinkedRecords.Event)`, `case records(LinkedRecords.Effect)`,
each read back by its own `recordsEvent` and `recordsEffect`), and conforms
to `LinkedRecordsApp`, which answers every stage for those cases: the records open with the app, hear the link, apply, ask for their
effects, run them under their keys, report a 401 or a 403 to the link and
are drawn within their window. The app writes what is its own alone:
`apply(_:to:from:)` (its engine, with the records' event applied already),
`ownEffects`, `ownKey` and `performOwn`. `notice` gives its presenter the
line to show.

The records are read once, then followed through the watch; what the read
and the watch do next follows from how an event moved the `ReadPhase`. The
read opens the watch first and lists after its bookmark, and the watch is
followed from that bookmark, since an app token's lists carry no head
(decision 0195): a change written while the list was read comes again from
the watch, and its record is read again after the list. A cursor the server
no longer holds is dropped from the cache before the read starts over, and
a record whose read again failed is kept as such in the cache, so it is
read again after the app opens again. Every
write the app makes (`RecordWrites`) is laid over at once and queued in
order; a write that leaves the queue is laid over as landed only where
`Outbox.landed` says so, until its record is read again, and a read again
that fails is tried when the network comes back. A read or a watch that
stopped keeps why (`WatchStop`): one the server refused for its own reasons
opens again when the app comes forward or the network returns; one whose
token was refused or does not reach the kinds waits for a new token (a
sign-in, an approval), since asking again would be refused the same way. A
read takes at most `LinkedRecords.readLimit` records (10,000) unless the app
names another `limit`, and says so in `notice` when the repository holds
more. An app whose kinds move (Substrate Tasks' collections) tells it
`rekinded`: the read and the watch are closed and opened again over the new
set, from a read, since the watch's cursor followed the old one; the records
of the kinds kept stay shown, and the outbox, which no read owns, keeps every
waiting write. Substrate Tasks and every app `mise run apps:new` makes are
built so.

A position the server can no longer resume is answered `410 compacted`. A
list read page by page (`listAll`, or `walk` for an app that reads its pages
itself, as Workouts does) then drops every row it read and lists again from
the first page, the same query with no `after`, once, whether the page
refused was a later one or the first page of a read begun from an `after`
the app saved: a second `compacted` in the same read is thrown, and the app
shows it as the failure it is (`LinkedRecords` says the records are not
being updated, and reads again when the app comes forward or the network
returns). A `compacted` on a page sent with no `after`, and any other
refusal of a later one (a `422` for a cursor sent with another filter), are
thrown as they are.

A watch told `compacted` (`WatchEvent.relist`) drops the position it saved,
which is never sent again, lists again and follows from where that list
began: one recovery a minute. Another `compacted` within 60 seconds of the
start of the last recovery is a failure, not another relist, whatever came
between: a bookmark, a list that landed or a row delivered does not make
the allowance whole again, and only the minute passing does
(`WatchFollower.recovers`, on the app's clock, which a test moves).
`LinkedRecords` then says the records are not being updated and reads them
again when the app comes forward or the network returns, so a server that
keeps refusing never makes a loop.

## An app's shell

`AppShell` (SubstrateUI) is what an Apple app runs the link and its root
under: the session in the Keychain, the repository's folder, its outbox and
record cache, sign-in and Change Access, the notifications' answers and the
time zone's moves as the app's commands, and Settings as one screen. The app
hands it a `Configuration`. Its `parts` says where a session's folder is and
what the kit keeps in it, as `AppShell.Parts`. There is no default: an app
new to the kit names a folder in Application Support
(`Configuration(…, folder: "SubstrateTasks", …)`, which is
`Parts.repositoryFolders(in:)`, one folder per repository laid out by the
kit); an app that kept its files before the kit hands `parts:` of its own,
since a path on the device never moves (§6): a folder adopted where it was
(`RepositoryFolder(adopting:)`), a record cache under its old name, or
`outboxes`, one per repository, as Workouts keeps them. The shell
fills in the device's facts and the Status screen's words, made once by
`Configuration.words`, and the app's root is made from those parts, so the
words have one source.

## How an app uses it

**A manifest, built once per run.** It is everything the kit asks a server on
the app's behalf: the scopes, what the approval sets up, and the server the
app needs.

```swift
let manifest = AppManifest(
    appName: "Substrate Sync",
    clientID: AppManifest.clientID(infoKey: "SubstrateSyncClientID", localDefault: "http://127.0.0.1/substrate-sync"),
    scopes: ["write:providers.substrate.reamde.dev/apple/*",
             "read:providers.substrate.reamde.dev/applehealth/account"],
    details: [.package("providers.substrate.reamde.dev/apple", atLeast: 3),
              .package("providers.substrate.reamde.dev/applehealth", atLeast: 5),
              .account(kind: "providers.substrate.reamde.dev/applehealth/account", id: "substrate-sync-1a2b3c4d",
                       properties: ["displayName": "Ada's iPhone"]),
              .webhook(trigger: "applehealth-webhook", deliversTo: "providers.substrate.reamde.dev/applehealth")],
    requirement: ServerRequirement(minimumVersion: "0.114.0"),
    passwordFallback: .never,
    kindLabels: ["providers.substrate.reamde.dev/apple/contact": KindLabel(singular: "Contact", plural: "Contacts"),
                 "providers.substrate.reamde.dev/apple/calendar": KindLabel(singular: "Calendar", plural: "Calendars",
                                                                            purpose: "supporting"),
                 "providers.substrate.reamde.dev/applehealth/export": KindLabel(singular: "Delivery", plural: "Deliveries")])
```

`softwareVersion` is this build's version, the bundle's short version and
build number unless the app names one: it goes on the authorization request
as `software_version` and in the set up check as `softwareVersion`, and the
server records it on the token with the floors of the `package` details, so
an upgrade that would break a connected build is held while it is
connected (decision record 0213).
`kindLabels` carries the labels (and a purpose other than `primary`) the
kinds the app names declare, so a scope over one reads as the approval page
words it ("See your Apple Health deliveries"), not as the word built from the
kind's name, and a glob names the primary kinds it reaches and counts the
rest. `names` replaces the word built from an id (a package, an account kind
or a trigger) where it is not the right one. A webhook detail's `deliversTo`
is the package whose callable its trigger runs: it words the webhook ("the
Apple Health webhook") until a check names the package, which then wins, and
it is never sent, since a webhook detail carries its trigger alone.

The requirement is decided by what the server can do: a `dev` build, or one
between releases, passes when its metadata lists the scope verbs and detail
types the app needs, and a release below `minimumVersion` is too old even
when it does. The verbs the scopes use and the types the details use are
required whatever the requirement names, so a server that would ignore
`authorization_details` (and approve the scopes alone) gets the manifest's
`passwordFallback` or the too-old screen, and `authorizeURL` throws rather
than send it a request without them.

**Sign-in.** `ConnectView` takes a server's address or a repository's name,
finds the server when the person presses Continue, and then shows one of
three doors from
`manifest.door(for:)`: the browser sign-in, the app's own password form
(`passwordDoor:`, for a server the app falls back for), or `ServerTooOldScreen`.
The browser sign-in needs no second press: the screen shows the server found
with its check for `ConnectController.foundShown`, then opens it, and offers
it again as a button only after it was closed or failed.
An app that falls back to the password passes its form as `passwordDoor:`;
without one, a server it would fall back for reads as too old.

```swift
ConnectView(manifest: manifest, tagline: "Your iPhone, in your substrate.", address: $address,
            unrevoked: (try? store.pendingRevocations()) ?? []) { probe in
    guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { return }
    let session = try await oauth.signIn(server: probe.origin, repository: probe.repository)
    try await store.save(session, replacing: nil)
}
```

`ServerLocator` reads what was typed in the order of
decision 1 of the authorities plan (`docs/plans/authorities.md`, proposed in pull request 1145):
the address itself when it serves `/.well-known/substrate/server.json`;
for a bare domain name, the `server=` of its `_substrate` TXT record (read
through `DNSServiceQueryRecord`, parsed by the rules of the server's
`internal/domains/record.go`, and refused when malformed), else the
`server` of `https://<name>/.well-known/substrate/repository.json`, else
its parent host as a server. The last two run only after DNS answered that
the name has no record: DNS-SD reports a SERVFAIL as the same NoSuchRecord,
so a negative answer is asked again of libresolv (the `SubstrateResolv`
target), and a lookup either one could not finish stops discovery. A server found from a name carries the name as
`probe.repository`: `signIn` sends it as `login_hint` and revokes and
refuses a token for any other repository (`RepositoryMismatch`).

`probe.discovery` is the server's whole document. Each of its features is a
group of routes with a `stability` (`stable`, `beta` or `experimental`), a
`revision` that only rises and, where the server announces a coming break, a
`notice`. `discovery.serves("records", atLeast: 2)` answers whether the
server serves a feature at that revision or a later one. The server refuses
a request field it does not know, so an app asks this before it sends a
field that a revision added; a server from before revisions reports 0 and
meets no minimum. [The stability policy](../../docs/stability.md) says what
each label promises.

An app that is the whole console asks for `scopes: ["full"]` alone (decision
0204): the server's metadata must list `full`, the approval is whole, and the
token has no scopes; `Grants.full` reads it as everything.

`OAuthClient.signIn` takes the redirect one of two ways, through the
device's `BrowserSignIn` (`WebAuthenticator` on an Apple device). A
manifest with an https client id and a `claimedRedirect` (a release,
stamped from its Info.plist with `AppManifest.claimedRedirect(infoKey:)`)
opens
`ASWebAuthenticationSession` with `callback: .https(host:path:)`, on iOS and
macOS alike; the system hands the callback only to the app the host's
association file names, which the app's associated domains entitlement
claims. Any other build takes a native app's loopback redirect (RFC 8252):
it listens on a port of this device's loopback address at the manifest's
`loopbackPath`, opens the server's approval page (in the default browser on
macOS, with a Cancel on the connect screen while it waits, and the app
brought forward when the redirect lands; in the system's Safari sheet on
iOS, where a backgrounded app's port is suspended). Either way it checks
the redirect's `state` and `iss` (RFC 9207, required when the metadata says
the server sends it), and exchanges the code once, never again: a code is
spent on first use.
The metadata must be the chosen server's own: an issuer that is not that
server, or an authorization, token or revocation endpoint off its origin, is
refused before the browser opens (`ServerMismatch`), since whoever receives
the code and its verifier holds the access; `ServerProbe` refuses it first.
The answer is a `Session` with the scopes approved, the scopes asked for,
when they end (counted from the exchange, however long the approval took),
and each webhook's delivery URL. An app that drives its own sheet uses
`authorizeURL`, `callback` and `session(exchanging:…)`.

**The session.** `SessionStore` keeps it under one account of a
`SessionStorage`, which alone says where and how: on an Apple device a
`KeychainStorage`, readable after the first unlock so background work can
use it, whose access group shares it with the app's share sheet. `Session` reads every shape an app wrote before it
(Sync's `{server, repository, secret, tokenID}`, Chat's with `grants`), and
writes those fields as well, so a rollback keeps the sign-in. A session with
`scopes == nil` is a password one: full access, and its own token id.

```swift
let store = SessionStore(service: "dev.reamde.substrate.sync", accessGroup: "<team>.dev.reamde.substrate.sync")
let client = SubstrateClient(session)
try await client.deliver(authority: session.repository, trigger: "applehealth-webhook", body: day)
```

**Still set up.** At launch and on return to the foreground, at most every
few minutes, `AppStatusCheck` reads discovery and the metadata, posts the
app's request to `/api/v1/authorizations/check` (its `scope`, its `details`
and its `softwareVersion`), and reads when the access ends. A `package`
detail whose floor is above the version the repository holds has the server
upgrade the package from its publisher's document at once, where the upgrade
adds kinds alone; one that changes code waits for the owner, which the
check's issues say. A server whose metadata lists no
detail types has no check, and is not asked. Its issues, any number at once,
are a server too old, an approval needed (with each change in the console's
words), access ending soon (within a week, or a quarter of the approved
lifetime where that is shorter) or ended, a server it could not reach, and a
password session on a server that can now give the app access of its own. A
setup row the token cannot see answers `unknown`, which is never a reason to
ask. A scope the owner left out of the last approval (asked for then, in
`Session.requestedScopes`, and not granted) is never asked about again; a
review offers it back (`manifest.review(_:session:)`). A row the server
blocks carries its reason, its one sentence and, where the owner can fix it,
a console path: the change shows the sentence, and the banner's button opens
that path (`openFix`), or Review where there is none, since the page shows
why with Approve off. A package only a newer server ships, or one whose
requirement it ships too old, is a server update: the row says what it would
do ("Update Apple to version 3 or later") over the server's sentence, which
says which of the two it is, and the approval waits for it with no button. A reset drops the answer of a
check still running, and a forced refresh is never dropped for one.

```swift
let status = AppStatusModel { store.load().map { AppStatusCheck(manifest: manifest, session: $0) } }

AppStatusBanner(status: status.status ?? AppStatus(), appName: manifest.appName, host: host,
                actions: AppStatusActions(review: { reviewing = true }, signInAgain: signIn, moveToAppAccess: move,
                                          openFix: { path in ServerAddress.page(server, path: path).map { openURL($0) } }))
    .task { await status.refresh() }
    .sheet(isPresented: $reviewing) {
        ReviewChangesSheet(appName: manifest.appName, host: host, changes: manifest.review(status.status, session: session),
                           firstSignIn: false, onContinue: signIn, onCancel: { reviewing = false })
    }
```

The review sends the whole request again through the browser; the page there
says what is new and, for a released app approved before, what was already
allowed. Change Access is `ReviewChangesSheet(changingAccessFor:session:…)`,
which names only the controls that page will show: Already allowed where the
server lists it (never for a local build's loopback client id), and Deny for
access that comes with what the request sets up. `session.revoke()` is the one revocation: an app
token at its revocation endpoint under the client id it was approved for
(none, where the session never kept one), a password one with its own
`DELETE /tokens/{id}`, and it returns only once the server says the token is
revoked. A token the app stops using is kept for revocation before it is
forgotten or saved over: `store.signOut(_:)` keeps then forgets the session,
and `store.save(_:replacing:)` keeps the old one before the new one is saved.
Where the Keychain will not keep it, either revokes it first, and one neither
kept nor revoked stays the session in use (`SessionStore.NotRetired`), never
dropped. A sign-out is done only once the session is gone: a removal the
Keychain refuses leaves it signed in, its token still kept, and says so. A
kept token stays in `pendingRevocations()` until `store.revokePending()` gets
the server's word, so a revocation cut short is tried again at the next
launch; `UnrevokedNotices` (in `AccountSection` and `ConnectView`) says so
while one waits, once per server and repository. `revokePending` reads the
session in use itself, between transitions, and never revokes it: a session
it cannot read revokes nothing (`SessionUnreadable`). A queue the store
cannot read is never taken for an empty one: keeping, forgetting and
revoking throw (`RevocationsUnreadable`) rather than write over it, and each
of its changes runs after the one before, whoever makes it. A session the
store cannot read is not signed out either: `save(_:replacing: nil)` will not
write over it, and the link says so and reads it again
(`LinkController.Command.readAgain`, and each return to the foreground).
`SessionStore(account:storage:)` over a storage of the caller's stands in
for the Keychain in a test.

**Errors.** `explain(error, app:, server:)` says any error as a sentence. A
403 with `insufficient_scope` is `InsufficientScope`, which reads as needing
the owner's approval, never as signing in again: signing in approves nothing
more. A 401 is `SessionExpired`. A 3xx is `Redirected`: the transport never
follows a redirect, since every request carries a token, a code and its
verifier, or a token to revoke, and goes to the chosen server alone; a
`URLSessionTransport` refuses it whatever session it runs on, and a session
from `makeSession` refuses it for a stream read from it directly. A body that
is not the server's envelope is a `TransportError`, and a proxy's HTML page
is never shown.

## The words

`ScopeWords`, `Grants` and the change words mirror the console's
(`web/console/src/lib/app-scopes.ts` and `app-setup.ts`), and the record
names and glyphs mirror `kind-names.ts` and `kind-glyph.ts`: the same scope
reads the same on the approval page and in the app. `Grants` matches as
`internal/scope` does, and its tests are that package's cases. A change to
either side is a change to both. A blocked row's words are the server's own
sentence, never rebuilt here.

A trait scope (`read:implements(<trait>)`, decision 0225) reaches what `*`
does among the kinds that bind the trait, so `reads` and `writes` take a
`GrantKind`, made from the kind's `KindDeclaration` where the app read it.
A kind known by its reference alone binds no trait the kit knows of, and no
trait scope reaches it here: the server, which reads the declaration, is the
judge. Without the kinds a trait reaches, its sentence names none, since
only the server can say that none binds it.

## Tests

`mise run apple:test:kit` (or `swift test` here) runs the suite, which stubs
the server with a scripted transport, and `mise run apps:test:linux` runs it
on Linux, where the tests of the Apple libraries and the SwiftUI screens
compile out. With `SUBSTRATEKIT_SNAPSHOTS` naming a
directory, `swift test --filter Snapshots` also draws every fixture of
`ScreenFixtures` through `Gallery`, and the views apps call in their usual
states (under `apps/`), to PNG files there, at an iPhone's size in light
mode, for a person to look at; `mise run apps:gallery` does it into
`build/gallery/apple`, beside kit-ui's Compose renderings of the same
fixtures in `build/gallery/android` ([a sample of eight](docs/gallery/README.md)
is kept in this tree). A screen without a renderer in
`Gallery` fails the suite either way. The fixtures cross to Android as
`android/fixtures/screens.json`, and SubstrateUI's tokens as kit-ui's
`KitTokens.kt`; `SUBSTRATE_UPDATE_FIXTURES=1 swift test` writes both again
after a presentation, a fixture or a token changes, and the suite fails
while either is stale.
