# @substrate/auth

An app's sign-in to a Substrate, with no framework: OAuth 2.1's code flow
with PKCE for a public client whose `client_id` is a URL, plus the package
details its approval installs (decision
[0194](../../../../docs/decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)).
The app never sees a password.

## The manifest

```ts
import { packageDetail, webManifest } from "@substrate/auth"

const manifest = webManifest({
  id: "template", // app.json's id
  name: "Substrate Template",
  scopes: ["write:substrate.reamde.dev/template/*"],
  details: [packageDetail("substrate.reamde.dev/template", 1)],
  softwareVersion: __APP_VERSION__,
  // optional: requirement: { minimumVersion: "0.121.0" }
})
```

`requirementOf(manifest)` is what the server must be able to do: the
manifest's own requirement, else every scope verb and detail type it asks
for.

## Signing in

```ts
await beginSignIn({ manifest, server, metadata, repository }) // leaves for the server
if (isCallback(manifest))
  session = await completeSignIn(manifest, location, (s) => keeper.discard(s))
else discardStrayCallback(manifest) // a code this tab never asked for
```

The return is checked before the code is exchanged: the `state` this tab
sent, the issuer it began on (RFC 9207, required where the server says it
sends one), an `error` in the owner's words, and the repository typed (a
token for another one is refused, and handed to `discard` to revoke: the
keeper's queue, or at once by default). Either way the response
leaves the address bar, and `discardStrayCallback` takes out one no sign-in
here began, so a code never stays in history or a shared link.

`webManifest`'s client id and redirect are on the page's folder
(`appBase`): `https://example.com/substrate/tasks`, opened without its
slash, is the app at `/substrate/tasks/`. An app that routes in its path
rather than its hash passes `base`.

## The session

```ts
const keeper = new SessionKeeper(manifest.id)
keeper.load() // the session every tab runs on
await keeper.signIn(session) // a sign-in's return, kept
await keeper.signOut(session) // { type: "signedOut" } or { type: "replaced", session }
await keeper.end(session, "refused") // a 401, taken up
await keeper.end(session, "timeUp") // the time up by this browser's clock
keeper.onUnrevoked((left) => show(left)) // the revocations still owed
```

The session lives in `localStorage`, one per app, and is the one every tab
of the app runs on: `keeper.watchSession` hears another tab sign out or sign
in, `keeper.watchQueue` another tab's revocations, and `current(token)`
says whether a tab's token is still the kept one.

Every change of it goes through `SessionKeeper`, under one Web Lock all
the app's tabs share, `substrate.session.<app id>`. A change reads the kept
session afresh inside the lock, decides and writes, and awaits nothing
while it holds it, so no other tab's change lands between its read and its
write: a sign-in's return replaces the session kept when it is saved,
whichever tab put it there. Where the browser has no Web Locks (before
Safari 15.4, Firefox 96 or Chrome 69, and in jsdom), the lock holds within
the tab alone (`tabLocks`): its own changes run one at a time, another
tab's may not. `sessionLocks()` picks between the two, and the keeper's
`locks` option replaces it (a test passes `FakeLocks` from
`@substrate/testing`). Reading the session writes nothing; one whose time is up
reads as none, and a change under the lock clears it.

**The token is readable by every script on the page.** A public client has
nowhere else to keep it, so a cross-site scripting hole in an app is the
theft of its token, and with it everything the owner approved for the app.
Two apps on one origin keep their sessions under different keys, but that
is bookkeeping, not a boundary: a script running in either reads both. Keep
an app's dependencies few and its origin its own, render records as text
(never as HTML), and set a Content-Security-Policy that allows no inline
script.

## Staying set up

```ts
const status = await checkAppStatus({ manifest, session })
// status.issues: accessEnded, serverTooOld, needsApproval, accessEndsSoon, unreachable
```

The app status check reads the server's discovery against the app's
requirement, then `POST /api/v1/authorizations/check` with the request the
app would send now and its `softwareVersion` (which the server records on
the token, decision 0213), and words each change as the console's approval
page does (`changesFrom`, `scopeWords`). A scope the owner left out of the
last approval is never asked about again. `AppStatusModel` keeps one
session's status current, at most once per interval unless forced.

## Revoking

One rule keeps every token: a token leaves the kept session only into the
revocation queue, written under the lock before the session changes, and
leaves the queue only once its server confirms revoking it.

- **A sign-in's return** queues the session it replaces, then saves the new
  one. Where the queue will not take the old token, or the storage will not
  take the new session (a full or private storage), nothing changes:
  `signIn` rejects with `NotSignedIn`, the new token queued, or revoked at
  once where the queue will not take it either, and the message says which,
  and where to revoke it by hand when the server did not confirm.
- **A sign-out** queues the tab's token, then clears the session while it is
  still that one. A tab that slept through another tab's sign-in leaves the
  session that stands and answers `replaced` with it, to take up. Where the
  queue will not take the token, `signOut` rejects with `NotSignedOut` and
  nothing changes, even when the server could be reached.
- **An access that ended** is cleared while it is still the kept one. A
  401 to a request carrying the token (`"refused"`) queues nothing: the
  server's word ended it. A time up (`"timeUp"`) rests on this browser's
  clock, which may run ahead of the server's, so the token is queued like
  any other first; where the queue will not take it, the session stays kept
  (no tab runs on it: it reads as none) and the next sign-in queues it.
  A sign-out queues a token whose time looks up too, and the queue forgets
  one only on the server's answer.

The revocations are sent from the queue after the lock is let go, in the
background, each on its own: no change waits on one, and one the server
does not answer holds back no other. A request gets 20 s (the
`revokeTimeout` option) before it counts as not confirmed. One the server
does not confirm is sent again at every open, when the network comes back,
and after a wait that grows while anyone listens (`onUnrevoked`, told as
each token's revocation settles). Only a 2xx
confirms a revocation: RFC 7009's server answers 200 for a token it revoked
and for one already invalid (the substrate answers 200 to every
well-formed request, 503 when it could not revoke), while a 400 or 401
refuses the request and revokes nothing, so the token stays queued and
`unrevokedSentences` tells the owner where to revoke it by hand.
