# SubstrateKit

What every Apple app that speaks to a substrate shares, as a Swift package
(iOS and macOS 26, no dependencies): which server it is and whether it is new
enough, the requests and their errors, signing in through the app
authorization flow, keeping the session, asking whether the app is still set
up, and the screens for all of it. Beside that, the record and its values as
the console draws them. Substrate Chat uses it; an app adds it as a local
package (`project.yml`'s `packages:`) and imports `SubstrateKit`. The design
it implements is [docs/plans/app-setup.md](../../../docs/plans/app-setup.md).

## What is in it

| Folder | What |
| --- | --- |
| `Server/` | `ServerAddress.origin(of:)`, the one normaliser from a typed address to `https://host[:port]`; `Discovery`, all of `/.well-known/substrate/server.json`; `AuthorizationServerMetadata` (RFC 8414); `ServerVersion`, which orders `v0.113.0`, `v0.113.0-4-gabc1234` and `0.114.0-pre.1` and refuses `dev`; `ServerRequirement` and its verdict; `ServerProbe`, which reads both documents |
| `API/` | `SubstrateClient` over an `HTTPTransport` (a `URLSession` unless a test scripts one): the bearer token, `Timeout` tiers, `Retry-After` on 429 and 503 for a request that may run twice, `ServerError`, `TransportError`, `SessionExpired`, `InsufficientScope`, `explain(_:app:server:)`, `segment(_:)`, records, lists, blobs, webhook deliveries, parked deliveries, `/authorizations/check` and the password door |
| `Auth/` | `AppManifest`, `AuthorizationDetail` and the check's wire shapes, `PKCE`, `OAuthClient`, `LoopbackReceiver`, `WebAuthenticator`, `Session`, `SessionStore`, `Grants` and `ScopeWords` |
| `Status/` | `AppStatusCheck`, which turns discovery, the requirement, the check and the expiry into `AppStatus.issues`; `Change`, one thing an approval would do, in words; `AppStatusModel`, the check kept current |
| `UI/` | `ConnectView`, `ServerTooOldView`, `AppStatusBanner`, `ReviewChangesSheet`, `AccessEndingBanner`, `AccessEndedView`, `AccountSection`, `ChangeRow`, `StatusBadge`, `ProblemBox` |
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
              .webhook(trigger: "applehealth-webhook")],
    requirement: ServerRequirement(minimumVersion: "0.114.0", detailTypes: AuthorizationDetail.Types.all),
    passwordFallback: .belowRequirement)
```

The requirement is decided by what the server can do: a `dev` build, or one
between releases, passes when its metadata lists the scope verbs and detail
types the app needs, and a release below `minimumVersion` is too old even
when it does. An app sends `authorization_details` only to a server that
lists every type it uses, since an older one ignores the parameter and
approves the scopes alone.

**Sign-in.** `ConnectView` takes the address, probes the server when the
person presses Continue, and then shows one of three doors from
`manifest.door(for:)`: the browser sign-in, the app's own password form
(`passwordDoor:`, for a server the app falls back for), or `ServerTooOldView`.

```swift
ConnectView(manifest: manifest, tagline: "Your iPhone, in your substrate.", address: $address) { probe in
    guard let oauth = OAuthClient(manifest: manifest, probe: probe) else { return }
    let session = try await oauth.signIn(server: probe.origin)
    try store.save(session)
}
```

`OAuthClient.signIn` listens on a loopback port, opens the server's approval
page in the system sheet, checks the redirect's `state` and `iss` (required
when the metadata says the server sends it), and exchanges the code once,
never again: a code is spent on first use. The answer is a `Session` with
the scopes approved, when they end, and each webhook's delivery URL. An app
that drives its own sheet uses `authorizeURL`, `callback` and `exchange`.

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
app's request to `/api/v1/authorizations/check`, and reads when the access
ends. Its issues, any number at once, are a server too old, an approval
needed (with each change in the console's words), access ending soon or
ended, a server it could not reach, and a password session on a server that
can now give the app access of its own. A setup row the token cannot see
answers `unknown`, which is never a reason to ask.

```swift
let status = AppStatusModel { store.load().map { AppStatusCheck(manifest: manifest, session: $0) } }

AppStatusBanner(status: status.status ?? AppStatus(), appName: manifest.appName, host: host,
                actions: AppStatusActions(review: { reviewing = true }, signInAgain: signIn, moveToAppAccess: move))
    .task { await status.refresh() }
    .sheet(isPresented: $reviewing) {
        ReviewChangesSheet(appName: manifest.appName, host: host, changes: status.status?.changes ?? [],
                           firstSignIn: false, onContinue: signIn, onCancel: { reviewing = false })
    }
```

The review sends the whole request again through the browser; the page there
says what is new and what was already allowed. Moving a password session to
app access is the same sign-in, then the old token revoked with its own
`client.revokeToken(id:)`, never through `/oauth/revoke` under the new client
id.

**Errors.** `explain(error, app:, server:)` says any error as a sentence. A
403 with `insufficient_scope` is `InsufficientScope`, which reads as needing
the owner's approval, never as signing in again: signing in approves nothing
more. A 401 is `SessionExpired`. A body that is not the server's envelope is
a `TransportError`, and a proxy's HTML page is never shown.

## The words

`ScopeWords`, `Grants` and the change words mirror the console's
(`web/console/src/lib/app-scopes.ts` and `app-setup.ts`), and the record
names and glyphs mirror `kind-names.ts` and `kind-glyph.ts`: the same scope
reads the same on the approval page and in the app. `Grants` matches as
`internal/scope` does, and its tests are that package's cases. A change to
either side is a change to both.

## Tests

`mise run apple:test:kit` (or `swift test` here) runs the suite, which stubs
the server with a scripted transport. With `SUBSTRATEKIT_SNAPSHOTS` naming a
directory, `swift test --filter Snapshots` also draws every screen in its
usual states to PNG files there, at an iPhone's size in light mode, for a
person to look at.
