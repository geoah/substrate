# SubstrateKit

What every app that speaks to a substrate shares, as one Swift package of
libraries: which server it is and whether it is new enough, the requests and
their errors, signing in through the app authorization flow, keeping the
session, asking whether the app is still set up, and the screens for all of
it. Beside that, the record and its values as the console draws them. The
console's shell (Substrate), Substrate Sync, Substrate Chat and Substrate
Workouts use it; an app adds it as a local package (`project.yml`'s
`packages:`). The design it implements is
[docs/plans/app-setup.md](../../docs/plans/app-setup.md), and its layout is
[docs/plans/app-kit.md](../../docs/plans/app-kit.md)'s §5.3.

## The libraries

Each library depends only on the ones above it in this table, which
`mise run lint:apps` holds. The portable ones (every one but the last two)
import Foundation and the kit alone, and build and test on iOS, macOS and
Linux (`mise run apps:test:linux`), as Android's Swift does; the Apple ones
compile to nothing elsewhere. The package is in Swift 6 language mode.

| Library | What it holds |
| --- | --- |
| `SubstrateCore` | `JSONValue` (a number keeps its decimal text: `JSONNumber`, read as a `Decimal`, a `Double` or an `Int`; an object its key order: `JSONObject`) with `DecimalText`; `Record`, `FlatRecord` (the record as the REST API speaks it) and `CodecError`; `KindReference`, `RecordReference`, `KindDeclaration`; `Timestamp` (RFC 3339) and `ISODuration`; a kind's and a property's name in words (`KindNames`, `kindDisplayName`, `propertyLabel`), its glyph (`KindGlyphSpec`, `KindHue`), `ValueShape` and `valueWords`; `ServerAddress`; the errors (`ServerError`, `TransportError`, `Redirected`, `SessionExpired`, `InsufficientScope`, `LoginRefused`, `OAuthRefusal`) and `explain(_:app:server:)`; `Grants`, `ScopeWords` and `ScopeKind` |
| `SubstrateDevice` | the device services' protocols and their values: `HTTPTransport` with `URLSessionTransport`, which never follows a redirect, whose `lines(for:)` reads an answer line by line as a `LineStream`; `SessionStorage`, the secure store, with `StoredItem` (missing told from failed) and `SecureStoreRefused`; `BrowserSignIn` with `SignInRoute`, `SignInRedirect`, `SignInAnswer` and `SignInCancelled`; `TXTResolver`, whose `TXTAnswer` keeps records, a confirmed absence, an unconfirmed absence and a failure apart; `Connectivity` with `NetworkPath`; `AppFolders` |
| `SubstrateAPI` | `SubstrateClient`: `Timeout` tiers, `Retry-After`, `segment(_:)`, records, lists (`ListQuery`, `RecordPage`), blobs, webhook deliveries, parked deliveries, the password door; `RecordReader`, `HTTPRecordReader` and `RecordDirectory`; `Discovery`, `AuthorizationServerMetadata` and `ServerMismatch`, `ServerVersion`, `ServerRequirement`, `ServerProbe`; `DomainRecord` and `ServerLocator` over a `TXTResolver`; `RecordWriter`, the record calls an outbox sends through; the watch: `WatchFollower` (a cursor and its generation, heartbeats, a dropped connection opened again, a relist on a 410 or a `compacted` frame) with `WatchFrame`, `WatchRow`, `WatchCursor`, `WatchEvent` and `WatchEnd` |
| `SubstrateAuth` | `AppManifest`, `KindLabel`, `AuthorizationDetail` and the check's wire shapes with `checkAuthorization`; `PKCE` (SHA-256 from CryptoKit, or swift-crypto where it is missing); `OAuthClient`, whose `signIn` takes the device's `BrowserSignIn`, `TokenResponse`, `RepositoryMismatch`; `Session` and its `revoke`; `SessionStore`, its pending revocations and its transitions, over a `SessionStorage`; `AppStatusCheck`, `AppStatus`, `AppIssue`, `Change` and its words, `AppStatusModel` |
| `SubstrateRuntime` | the loop every screen runs (below): `Controller`, `Runtime` with `PresentationNotEncoded`, `AppEnvironment` with `AppEnvironmentKey`, `ClientSource` and `DeviceServices`, `Enrichment`, `PresenterInput`, `Journaling`, `JournalStore`, `Coalescing`, `Work`, `ChildKey`, `LinkSnapshot`, `KeyedList` with its `KeyedListDelta`, `RoutedCommand`, `RuntimeTrace`, `RuntimeClock`, `IDSource` |
| `SubstrateLocal` | what an app keeps on the device for one repository: `RepositoryFolder` (a format, atomic writes, `SetAsideFile`s listed, `AreaUsage` per `FolderArea`, eviction by age and budget), `JournalFile`, the `Outbox` (Workouts', with a `batch` where it had a session; `changes()` reports each `OutboxSummary`, and an `OutboxUpgrade` reads a file an older build wrote), `RecordCache`, `HeldPackage`, `LinkGeneration`; the folder joins the environment as `environment.folder` |
| `SubstrateScreens` | the shared screens as engines, presenters and fixtures, each label written here: `LinkController` (the session and its revocations, the status check, the package versions, connectivity, the outbox; a `LinkSnapshot` with no secret) over the app's root, a `LinkedApp` that tells it a `LinkReport`, `ConnectController`, `AccessPresenter`, `ReviewPresenter`, `ServerTooOldPresentation`, `RepositoryPresenter`, `StatusPresenter` with `LinkStatus`, `PackageStanding` and `StatusWords`, `SettingsPresentation` with `SettingsPresenter`, `SettingsValues` and the rows' `SettingsRowID`, `ProblemPresenter`, `AboutPresentation`; `ScreenFixtures`, every screen in the states an app shows it in |
| `SubstrateTesting` | the fakes tests use: `StubTransport` and `ScriptedServer` (scripted servers, by route and by rule), `ScriptedLines` (answers read line by line), `MemoryStorage`, `TableResolver`, `ScriptedBrowser`, `ScriptedConnectivity`, `TemporaryFolders`; and the runtime's harness: `ControllerHarness` with `TestClock`, `SequentialIDs` and `MemoryJournal` |
| `SubstrateDeviceApple` | the device services on Apple frameworks: `KeychainStorage`, `WebAuthenticator` (`ASWebAuthenticationSession`, the claimed callback or the loopback redirect), `LoopbackReceiver`, `DNSServiceTXTResolver` and the `SubstrateResolv` C target behind it, `PathMonitor` (`NWPathMonitor`), `ApplicationFolders` |
| `SubstrateUI` | the renderers of SubstrateScreens' presentations (`ConnectScreen`, `AccessBannerView`, `AccessEndedScreen`, `ReviewScreen` and `ReviewSectionsView`, `ServerTooOldScreen`, `RepositoryScreen`, `StatusScreen`, `SettingsScreen`, `AboutScreen`, `ProblemView`, `ChangeLineView`) with the design tokens, `Gallery` (each screen's renderer), `PresentationModel` (a root's presentations republished on the main actor) and `AppEnvironment.apple`; the views apps call by their own initialisers, now thin wrappers over those (`ConnectView`, which runs a `ConnectController`, `ServerTooOldView`, `AppStatusBanner`, `ReviewChangesSheet` and its `ReviewChangesSections`, `AccessEndingBanner`, `AccessEndedView`, `AccountSection` (the Repository screen under the apps' word, Account), `UnrevokedNotices`, `ChangeRow`, `ProblemBox`), `StatusBadge`; and the record views (`KindGlyph`, `RecordPill`, `RecordSheet`, `RecordDetailView`, `RecordValueView`) |

`SubstrateKit` is every library but `SubstrateTesting` and `SubstrateLocal`
under one import (`SubstrateLocal`'s outbox shares its names with
WorkoutsSync's until Workouts moves onto it, so an app takes it by name),
with the Apple device services as the defaults the single module had: a
`ServerLocator(probe:)` reads DNS through DNS-SD, `OAuthClient.signIn`
without a `browser:` signs in through `WebAuthenticator`, and
`SessionStore(service:accessGroup:…)` keeps the session in the Keychain
through a `KeychainStorage` made of those settings.
The console, Chat and Sync import it. Code that must build for Linux or
Android (an app's core: WorkoutsCore and WorkoutsSync) imports the
portable libraries it uses instead and passes its device services in.

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
start, on the live path alone, never on a replay. An effect is a value;
`perform(_:in:)` turns it into `Work`, whose results are events through the
same loop. Under a `key(of:)`, starting one cancels the one running and its
late result is dropped; a reset or a close drops every result still on its
way. `coalescing(of:)` says whether an event presents now or within a
window, and a shorter window asked later moves the deadline earlier;
`nextChange(of:)` has the runtime deliver `event(atTick:)` then. A runtime
opens on its own executor at its first call: the journal folded, then
`event(atOpen:)` applied live, so its effects run. The rule for the names,
and every default, is in `Controller.swift`.

A parent keeps its children in its state, by the key the app minted
(`children(in:)`), each an `Equatable` value made from that state, so what
the parent hands a child is part of it and a changed value replaces the
one running; it folds the events of theirs it cares about
(`event(fromChild:_:)`), and presents them in its own tree, which goes out
once per step. A command reaches any controller by its path of keys
(`send(_:to:)`). The link is every app's root: `LinkController<App>` keeps
the app's root as its child, its snapshot reaches every controller under
it as `event(forLink:)` inside the step that changed it, and the app tells
it a `LinkReport` (a 401, a 403 `insufficient_scope`) from any event.

```swift
let link = LinkController(app: TasksController(...), manifest: manifest, sessions: store, outbox: outbox,
                          packages: ["substrate.reamde.dev/tasks": 2], words: words)
let model = PresentationModel(link, environment: .apple(transport: transport))  // SwiftUI reads model.presentation
model.send(TasksController.Command.complete(taskID), to: LinkController<TasksController>.appPath)
```

`ControllerHarness` (SubstrateTesting) runs a controller on a `TestClock`
with `SequentialIDs` and a scripted server, and keeps the runtime's trace,
so a test reads what was journaled, presented, published and asked for, in
order.

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
(`passwordDoor:`, for a server the app falls back for), or `ServerTooOldView`.
An app that falls back to the password passes its form as `passwordDoor:`;
without one, a server it would fall back for reads as too old.

```swift
ConnectView(manifest: manifest, tagline: "Your iPhone, in your substrate.", address: $address,
            unrevoked: store.pendingRevocations()) { probe in
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
dropped. A kept token stays in `pendingRevocations()` until
`store.revokePending(current:)` gets the server's word, so a revocation cut
short is tried again at the next launch; `UnrevokedNotices` (in
`AccountSection` and `ConnectView`) says so while one waits, once per server
and repository. `SessionStore(account:storage:)` over a storage of the
caller's stands in for the Keychain in a test.

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

## Tests

`mise run apple:test:kit` (or `swift test` here) runs the suite, which stubs
the server with a scripted transport, and `mise run apps:test:linux` runs it
on Linux, where the tests of the Apple libraries and the SwiftUI screens
compile out. With `SUBSTRATEKIT_SNAPSHOTS` naming a
directory, `swift test --filter Snapshots` also draws every fixture of
`ScreenFixtures` through `Gallery`, and the views apps call in their usual
states (under `apps/`), to PNG files there, at an iPhone's size in light
mode, for a person to look at; `mise run apps:gallery` does it into
`build/gallery/apple`. A screen without a renderer in `Gallery` fails the
suite either way.
