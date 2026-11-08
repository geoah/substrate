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
if (isCallback(manifest)) session = await completeSignIn(manifest)
else discardStrayCallback(manifest) // a code this tab never asked for
```

The return is checked before the code is exchanged: the `state` this tab
sent, the issuer it began on (RFC 9207, required where the server says it
sends one), an `error` in the owner's words, and the repository typed (a
token for another one is revoked and refused). Either way the response
leaves the address bar, and `discardStrayCallback` takes out one no sign-in
here began, so a code never stays in history or a shared link.

`webManifest`'s client id and redirect are on the page's folder
(`appBase`): `https://example.com/substrate/tasks`, opened without its
slash, is the app at `/substrate/tasks/`. An app that routes in its path
rather than its hash passes `base`.

## The session

`SessionStore` keeps the session in `localStorage`, one per app, dropping
one whose access has ended. It is the session every tab of the app runs
on: `watch` hears another tab sign out or sign in, `current(token)` says
whether a tab's token is still the kept one, and `clearIf(token)` ends a
session only while it is still that one and answers the session kept
after, so a tab whose access ended never signs out a session another tab
signed in since.

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

```ts
const queue = new RevocationQueue(manifest.id)
queue.keep(session) // first: kept before anything forgets the session
await queue.send(session) // forgotten once the server confirms
await queue.retry() // at every open: the tokens still live
unrevokedSentences(manifest.name, queue.pending())
```

`signOut(session, store, queue)` is the sign-out, in that order: the token
is kept before the session is cleared, so a revocation that meets no server
leaves the live token held by the queue rather than by nothing. Where the
queue will not keep it (a full or private storage), the session is the
token's only copy, so it is revoked first and cleared only once the server
confirms; when neither works, `signOut` rejects with `NotSignedOut` and the
owner is still signed in, to try again. It clears the session only while it
is still the one signed out of: a tab that slept through another tab's
sign-in revokes its own token and answers `replaced` with the session that
stands. Only a 2xx confirms a revocation: RFC 7009's server answers 200
for a token it revoked and for one already invalid (the substrate answers
200 to every well-formed request, 503 when it could not revoke), while a
400 or 401 refuses the request and revokes nothing, so the token stays
queued and the owner is told where to revoke it by hand.
