# SubstrateKit

What every Apple app that speaks to a substrate shares, as a Swift package
(iOS and macOS 26, no dependencies): which server it is and whether it is new
enough, the requests and their errors, signing in through the app
authorization flow, keeping the session, asking whether the app is still set
up, and the screens for all of it. Beside that, the record and its values as
the console draws them. The console's shell (Substrate), Substrate Sync and
Substrate Chat use it; an app adds it as a local package (`project.yml`'s
`packages:`) and imports `SubstrateKit`. The design
it implements is [docs/plans/app-setup.md](../../../docs/plans/app-setup.md).

## What is in it

| Folder | What |
| --- | --- |
| `Server/` | `ServerAddress.origin(of:)`, the one normaliser from a typed address to `https://host[:port]`, and `page(_:path:)` for a console path the server gave; `Discovery`, all of `/.well-known/substrate/server.json`; `AuthorizationServerMetadata` (RFC 8414) and `mismatch(server:)`; `ServerVersion`, which orders `v0.113.0`, `v0.113.0-4-gabc1234` and `0.114.0-pre.1`, refuses `dev`, and is said one way on every screen (`display`); `ServerRequirement` and its verdict; `ServerProbe`, which reads both documents |
| `API/` | `SubstrateClient` over an `HTTPTransport` (a `URLSession` that never follows a redirect, unless a test scripts one): the bearer token, `Timeout` tiers, `Retry-After` on 429 and 503 for a request that may run twice, `ServerError`, `TransportError`, `SessionExpired`, `InsufficientScope`, `Redirected`, `explain(_:app:server:)`, `segment(_:)`, records, lists, blobs, webhook deliveries, parked deliveries, `/authorizations/check` and the password door |
| `Auth/` | `AppManifest`, `AuthorizationDetail` and the check's wire shapes, `PKCE`, `OAuthClient`, `LoopbackReceiver`, `WebAuthenticator`, `Session` and its `revoke`, `SessionStore` and its pending revocations, `Grants` and `ScopeWords` |
| `Status/` | `AppStatusCheck`, which turns discovery, the requirement, the check and the expiry into `AppStatus.issues`; `Change`, one thing an approval would do, in words; `AppStatusModel`, the check kept current |
| `UI/` | `ConnectView`, `ServerTooOldView`, `AppStatusBanner`, `ReviewChangesSheet` and its `ReviewChangesSections`, `AccessEndingBanner`, `AccessEndedView`, `AccountSection`, `UnrevokedNotices`, `ChangeRow`, `StatusBadge`, `ProblemBox` |
| `Records/` | `Record`, `JSONValue`, a kind's name and glyph, `RecordPill` and the read-only record view, `RecordDirectory` over a `RecordReader` |

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

**Sign-in.** `ConnectView` takes the address, probes the server when the
person presses Continue, and then shows one of three doors from
`manifest.door(for:)`: the browser sign-in, the app's own password form
(`passwordDoor:`, for a server the app falls back for), or `ServerTooOldView`.
An app that falls back to the password passes its form as `passwordDoor:`;
without one, a server it would fall back for reads as too old.

```swift
ConnectView(manifest: manifest, tagline: "Your iPhone, in your substrate.", address: $address,
            unrevoked: store.pendingRevocations()) { probe in
    guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { return }
    let session = try await oauth.signIn(server: probe.origin)
    try await store.save(session, replacing: nil)
}
```

An app that is the whole console asks for `scopes: ["full"]` alone (decision
0204): the server's metadata must list `full`, the approval is whole, and the
token has no scopes; `Grants.full` reads it as everything.

`OAuthClient.signIn` is a native app's loopback redirect (RFC 8252): it
listens on a port of this device's loopback address at the manifest's
`loopbackPath`, opens the server's approval page (in the default browser on
macOS, with a Cancel on the connect screen while it waits, and the app
brought forward when the redirect lands; in the system's Safari sheet on
iOS, where a backgrounded app's port is suspended), checks
the redirect's `state` and `iss` (required when the metadata says the server
sends it), and exchanges the code once, never again: a code is spent on
first use. A claimed `https` callback (a universal link) is not in the kit.
The metadata must be the chosen server's own: an issuer that is not that
server, or an authorization, token or revocation endpoint off its origin, is
refused before the browser opens (`ServerMismatch`), since whoever receives
the code and its verifier holds the access; `ServerProbe` refuses it first.
The answer is a `Session` with the scopes approved, the scopes asked for,
when they end (counted from the exchange, however long the approval took),
and each webhook's delivery URL. An app that drives its own sheet uses
`authorizeURL`, `callback` and `session(exchanging:…)`.

**The session.** `SessionStore` keeps it in the Keychain, readable after the
first unlock so background work can use it; an access group shares it with
the app's share sheet. `Session` reads every shape an app wrote before it
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
app's request to `/api/v1/authorizations/check` (its `scope` and its
`details`), and reads when the access ends. A server whose metadata lists no
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
and repository. A store made over `SessionStorage` of the caller's stands in
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

## Tests

`mise run apple:test:kit` (or `swift test` here) runs the suite, which stubs
the server with a scripted transport. With `SUBSTRATEKIT_SNAPSHOTS` naming a
directory, `swift test --filter Snapshots` also draws every screen in its
usual states to PNG files there, at an iPhone's size in light mode, for a
person to look at.
