---
status: accepted
date: 2026-10-31
decision-makers: George Antoniadis
---

# 0194. An app gets a scoped token through a registration-free authorization flow

## Context and Problem Statement

An app that wants to use somebody's substrate either asks for their password
and calls `POST /login`, or has the owner mint a token with `POST /tokens`
and paste it in. Both hand the app everything: every record, vocabulary apply
(so a function, so code), more tokens and the owner's own tokens.
[0143](0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md)
kept that while nothing needed less, named "a token the owner wants to hand
to something they do not fully trust" as the trigger to reopen it, and wrote
down the retrofit: an optional `scopes` property whose absence keeps full
access. A third-party app is that trigger. The design and its three review
rounds are in [the plan](../plans/app-authorization.md).

## Considered Options

- Keep full-access tokens; an app asks for the password or a pasted token.
- OAuth with registered clients: an app registers with each substrate (or
  through dynamic client registration) and holds a client id and secret.
- OAuth 2.1 authorization code with PKCE for public clients whose
  `client_id` is a URL serving a client ID metadata document (the IETF
  draft MCP clients use), with a loopback `client_id` for local apps;
  scopes over kind references; the token a record with optional `scopes`.
- The same, with a scope grammar of fixed words (`tasks.read`) instead of
  kind references.

## Decision Outcome

Chosen: the registration-free OAuth 2.1 flow with scopes over kind
references, because it is the only option where any app can take part
without the owner or the app setting anything up first, and a standard
OAuth or MCP client library speaks it. Keeping full access was rejected
because it leaves an app no way in short of everything. Registered clients
were rejected because a personal server has no developer portal and
dynamic registration is deprecated in MCP. Fixed scope words were rejected
because the kinds are the repository's own and change with every import:
the 0080 grant grammar already names them, and `~` stands for the
repository's authority an app cannot know in advance.

The rules this binds:

- **A scope is `read:<pattern>` or `write:<pattern>`** over the 0080
  grammar; `write` includes `read`. The auth kinds are never reached, a glob
  never reaches an `internal` kind, no scope writes the `core` or `llm`
  package, and no app's token writes an account or a bundle input.
- **A token without `scopes` keeps full access**, exactly as 0143 said, so
  every session, script and device token is unchanged. A stored `scopes`
  that is null, empty or does not parse refuses the token rather than read
  as absent.
- **Scopes bound what an app does directly**, not what the owner's own
  declarations do in answer: a trigger, a mapping or a cascade runs as for
  any write, and the approval page says so.
- **Three layers enforce it**: owner-only routes by default (only the
  records route, the record path and the unit reads admit an app's token),
  a dataset wrapper that implements every method explicitly, and the
  engine's owner-only gates, which read the scope mark on the context.
- **The flow's state lives in memory**: parked requests and codes are
  forgotten by a restart, and the token itself is a record like any other.

### Consequences

- Good, because an app needs nothing but a URL it controls and a JSON file
  served there, and the owner sees the app's host, its claims, where the
  code goes and every scope before approving.
- Good, because every existing token, client and script keeps working: the
  change is additive on the token kind (version 8).
- Good, because a route or a dataset method added later is the owner's until
  somebody decides otherwise: the route groups default to owner-only and the
  wrapper does not compile until the method is spelled out.
- Bad, because an app's write can still reach past its scopes through the
  owner's automation (a trigger that runs code and calls a provider, a
  cascade that deletes), which the page discloses but does not prevent.
- Bad, because nothing about a local app can be verified, and a glob
  scope widens when the owner imports a package after approving.
- Bad, because a binary from before this change reads an app's token as a
  full-access one: a rollback past it needs every app token revoked first.
- Bad, because there are no refresh tokens: an app's access ends with its
  lifetime (7, 30 or 90 days) and the owner approves again.
- Bad, because the scoped surface is narrow: the change feed, aggregate and
  window reads, blobs, functions and agents stay the owner's until each gets
  a design of its own.

### Confirmation

`internal/scope` holds the grammar (`scope_test.go`), `internal/authorize`
the client ids, redirects, PKCE, the document fetch and the lifecycle
(`authorize_test.go`). `internal/api/authorize_test.go` runs the flow end to
end against the fake and holds the scoped surface, the owner-only routes and
every refusal. `TestAnAppsTokenWritesNoAccountAndRevokesNoToken`,
`TestAScopedTokenCarriesItsGrantAndItsApp` and
`TestADamagedGrantNeverReadsAsFullAccess` (internal/engine) hold the token
record and the engine's gates.

## More Information

Amends 0143: its rule stands for every token without `scopes`, and its
reopen trigger is what this answers.
[0069](0069-the-owner-export-is-the-snapshot-streamed-as-a-tar.md) and
[0088](0088-a-single-record-read-carries-its-inbound-mapping-owned-links.md)
said a bearer token reads the whole repository; an app's token does not
reach the export and reads inbound links cut to its scopes. Reopen when an
app needs a surface outside the scoped one, a refresh token, or a second
issuer.
