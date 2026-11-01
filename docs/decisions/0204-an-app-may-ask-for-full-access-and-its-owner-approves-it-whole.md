---
status: accepted
date: 2026-11-01
decision-makers: George Antoniadis
---

# 0204. An app may ask for full access, and its owner approves it whole

## Context and Problem Statement

The Console app (`apps/apple`, the console in a native shell) is the whole
console, so it needs what a password sign-in gives: every route, the
vocabulary, the tokens. The authorization flow of
[0194](0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)
mints only scoped tokens, and no scope writes `core` or `llm` or reaches an
owner-only route. So the shell showed the console's own password form inside
its web view, where the person cannot see which server they are typing into,
and where password managers do not fill. Signing in has to move to the
server's own page in Safari, and for that the flow has to be able to hand
over everything.

## Considered Options

- A `full` scope in the existing flow, asked for alone, approved whole.
- A separate device sign-in flow: the same code and PKCE exchange, but a
  door of its own that only ever mints a full-access token.
- A native password form in the shell, with the token handed to the page,
  and no server change.

## Decision Outcome

Chosen: a `full` scope in the existing flow, because it reuses everything the
flow already holds (the client id document, the loopback and web redirects,
PKCE, the approval page, the revocation endpoint, the Apps list), and the
owner sees it on the same page that names every other app. A separate door
would repeat all of that for one difference. A native form keeps the
password in the app, which is what 0194 set out to end, and is still not the
server's own page.

The rules this binds:

- **`full` is the bare word, asked for alone.** A request naming it beside
  any other scope, or with `authorization_details`, is refused: it already
  holds every scope, and nothing is set up for it.
- **It is approved whole, through a human door**, as a set up is
  ([0202](0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md)):
  the page offers no checkbox, and an approval naming it on a request that did
  not ask for it is refused.
- **Approving it mints a token without scopes**, which is full access
  ([0143](0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md)),
  carrying the app's client id and name. The owner sees it under Apps, the
  app revokes it at the revocation endpoint, and `full` is never stored:
  `Parse`, `CheckStored` and `NewGrant` refuse it.
- **It writes as the owner does**, not as `app:<host>`: it is a sign-in,
  and the page says so.
- **The token response names the token record** (`token_id`), so a client
  holding full access can delete its own session as a sign-in's holder does.
- The metadata lists `full` in `scopes_supported`, and an app reads that
  before it asks.

This amends 0194's rule that the flow mints only scoped tokens. The rest of
0194 stands.

### Consequences

- Good, because the Console app signs in on the server's own page in
  Safari's sheet (or the default browser on a Mac), which names the server
  and fills from the person's password manager, and the app never sees the
  password.
- Good, because the owner sees, revokes and lets lapse a full-access app the
  way they do every other app.
- Bad, because any app can now ask for everything, and a careless owner can
  grant it. The page leads with a warning that approving is the same as
  signing in with the password, but a warning is not a refusal.
- Bad, because a full-access token is unscoped, so a binary from before
  this change treats it exactly as it does today: nothing on the token
  records that an app holds it except its client id.
- Bad, because a full-access app's writes carry the owner's actor, and only
  the principal (the token) in history tells them apart.

### Confirmation

`TestFullIsRequestedAloneAndNeverStored` (internal/scope),
`TestAnAppAskingForFullAccessGetsAFullAccessTokenNamingIt`,
`TestFullAccessIsAskedForAloneAndSetsNothingUp` and
`TestASecondFullRequestReadsAsAllowedBefore` (internal/api), the approval
page's and the login page's full-access cases in the console's tests, and
the kit's `FullAccess` suite.

## More Information

The shell's side is `apps/apple/Substrate/Sources/Shell/ConsoleSignIn.swift`
and `shellSignIn` in `web/console/src/lib/shell.ts`. Revisit if a client
other than the owner's own needs full access often enough that the warning
stops being read.
