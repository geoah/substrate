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
```

The return is checked before the code is exchanged: the `state` this tab
sent, the issuer it began on (RFC 9207, required where the server says it
sends one), an `error` in the owner's words, and the repository typed (a
token for another one is revoked and refused). `SessionStore` keeps the
session in `localStorage`, dropping one whose access has ended.

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
await queue.revoke(session) // kept first, forgotten once the server confirms
await queue.retry() // at every open: the tokens still live
unrevokedSentences(manifest.name, queue.pending())
```
