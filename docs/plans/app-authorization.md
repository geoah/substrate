# Authorizing apps with scoped tokens

Status: implemented, 2026-10-31, in PR 1084, after the review rounds the
review record below lists. It supersedes
the full-access rule of
[0143](../decisions/0143-a-token-has-full-access-to-its-repository-and-a-login-token-does-not-expire.md)
for the tokens apps receive; every existing token keeps full access. The
binding choices are recorded in
[0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md).

## The problem

An app that wants to read or write someone's substrate has two ways in
today, and both hand it everything. It asks for the repository name and
password and calls `POST /login` (docs/api.md tells a web app to do exactly
this), or the owner mints a token with `POST /tokens` and pastes it in.
Either token reads every record, applies vocabulary (so it can declare a
function and run code), mints more tokens and revokes the owner's own. A
notes app that only needs `tasks` gets the health records, the provider
accounts and the keys to the repository.

## The outcome

Any app, without registering anywhere, sends the owner to their substrate.
The owner signs in as usual, reads which app is asking and what for, and
approves all or part of it. The app receives a token that reaches only the
kinds the owner approved, for a lifetime the owner chose. The owner sees
every app token in the console and revokes it there.

## Protocol profile

OAuth 2.1 authorization code with PKCE (S256), public clients only. The app
is identified by a URL it controls, its `client_id`, which serves a JSON
document describing the app: the OAuth Client ID Metadata Document draft
(`draft-ietf-oauth-client-id-metadata-document-02`, "CIMD" below), which is
also what MCP clients use. There is no client registration, no client
secret, no dynamic client registration endpoint and no pre-approved app
list. Publishing the document is the whole of what an app does to take
part.

### Endpoints

| Method and path | Caller | What it does |
| --- | --- | --- |
| `GET /.well-known/oauth-authorization-server` | app | RFC 8414 metadata |
| `GET /.well-known/oauth-protected-resource` | app | RFC 9728 metadata |
| `GET /oauth/authorize` | browser | checks the request's shape, parks it, answers `303 /authorize/{request}` |
| `GET /authorize/{request}` | browser | the console's approval page |
| `GET /api/v1/authorizations/{request}` | console | the parked request: the app as fetched, the scopes resolved against the registry |
| `POST /api/v1/authorizations/{request}` | console | approve a subset of the scopes and a lifetime; answers the redirect URL carrying the code |
| `DELETE /api/v1/authorizations/{request}` | console | deny; answers the redirect URL carrying `error=access_denied` |
| `POST /oauth/token` | app | exchange the code and the PKCE verifier for a token |
| `POST /oauth/revoke` | app | RFC 7009: revoke the token presented |

The inbound routes sit under the root `/oauth/` prefix, beside `/login`.
The outbound provider flow keeps `/api/v1/oauth/start` and
`/api/v1/oauth/callback`; the two share no route, table, key, actor or
setting. The three `/api/v1/authorizations` routes are owner-only: a scoped
token is refused there, so an app cannot approve scopes for itself.

### Metadata

`/.well-known/oauth-authorization-server` answers `issuer`,
`authorization_endpoint`, `token_endpoint`, `revocation_endpoint`,
`response_types_supported: ["code"]`, `grant_types_supported:
["authorization_code"]`, `code_challenge_methods_supported: ["S256"]`,
`token_endpoint_auth_methods_supported: ["none"]`,
`revocation_endpoint_auth_methods_supported: ["none"]`,
`authorization_response_iss_parameter_supported: true`,
`client_id_metadata_document_supported: true` and `scopes_supported:
["read:*", "write:*"]`. `/.well-known/oauth-protected-resource` answers
`resource` (the issuer), `authorization_servers: [issuer]`,
`bearer_methods_supported: ["header"]` and the same `scopes_supported`. A
`401` under `/api/` carries `WWW-Authenticate: Bearer
resource_metadata="<issuer>/.well-known/oauth-protected-resource"`, and a
scope refusal answers `403` with `error="insufficient_scope"`.
`WWW-Authenticate` joins the CORS exposed headers.

### The issuer

`SUBSTRATE_PUBLIC_URL`, when set, is the issuer. Without it the issuer is
the origin the request reached: `http://` for a loopback host (the dev
box), `https://` otherwise, plus the `Host`. A flow is bound to the issuer
it began on: the parked request, the code and the redemption must all reach
the same one, else the exchange is refused. One server is one protected
resource, so `resource`, when an app sends it, must equal the issuer: at
`/oauth/authorize` a different one is the error page, at `/oauth/token`
`invalid_target`. A token is valid on whatever host reaches this server.

### Who the app is

Two `client_id` shapes are accepted.

**A web app** names an `https` URL with a path, no userinfo, no fragment,
no query and no dot segments (CIMD §3). After the owner signs in, the
server fetches it: no redirects followed, a 5 second timeout, at most
5 KB, `200` only, through a fetcher of its own that dials public unicast
addresses alone (every RFC 6890 special-use range refused at connect, so
a DNS answer cannot steer it), uses no proxy and ignores the operator's
`SUBSTRATE_EGRESS_ALLOW`. At most four fetches run at once. The document
must be a JSON object whose `client_id` equals the URL and whose
`redirect_uris` is a non-empty list of strings; `client_name` and
`client_uri` are optional strings; a `client_secret`, a `jwks`/`jwks_uri`
or a `token_endpoint_auth_method` other than `none` refuses it. A
successful fetch is cached for 10 minutes, a failure never, and a refusal
tells the owner what failed (status, timeout, size, field) without naming
an address.

**A local app** (a CLI, a desktop tool, a script) names a loopback `http`
URL: host `127.0.0.1`, `[::1]` or `localhost`, any port. It is never
fetched, its `redirect_uri` must be a loopback `http` URL too, and the page
says the app runs on the owner's computer and that nothing about it can be
verified. Loopback impersonation cannot be prevented (MCP security
considerations), so the page never calls such an app by a name.

### Redirect URIs

A redirect URI is `https`, or `http` on a loopback host; it has no
userinfo and no fragment. A web app's `redirect_uri` is on its `client_id`'s
own origin (`https`, the same host and port) or on a loopback address (CIMD
§8.1 lets a server require the first; IndieAuth does by default), so the
host the page heads with is the host the access goes to, and a document on
a shared host (a raw GitHub file, a paste) can name only a callback on that
origin or a loopback one. It must also equal an entry of the document's
`redirect_uris` exactly, except that a loopback `http` entry matches on any
port (RFC 8252 §7.3). A mobile app uses a claimed `https` link on its own
domain (a Universal Link or an App Link); a custom URL scheme is refused,
since any app may claim one. The same rules hold at the request,
at approval, at denial and at the exchange, so the server never navigates
anywhere a document did not list.

### The request

`GET /oauth/authorize` reads each parameter once (a repeated parameter is
refused) and requires `response_type=code`, `client_id`, `redirect_uri`,
`code_challenge` (43 base64url characters), `code_challenge_method=S256`
and `scope`; `state` is echoed verbatim and `resource` must be the issuer.
It checks the shape of each without fetching anything, so an anonymous
visitor cannot make the server dial a URL. Every refusal at this step is a
`400` page that never redirects (OAuth 2.1 §4.1.2.1), because the redirect
is not verified yet. A valid request is parked, as an immutable snapshot,
under a random 128-bit id for 10 minutes and answered
`303 /authorize/{request}`. Parking is paced per source (an IPv4 address
or an IPv6 /64), one source holds at most 32 parked requests, and the store
holds at most 1,000. The console page carries only that
id in its path, so the console's router never parses an OAuth parameter (it
rewrites query values such as `state=1.0`). This endpoint answers no CORS
headers.

### The approval page

The console route `/authorize/{request}` sits outside the signed-in shell
and sends a signed-out owner through `/login` and back; `/login` and
`/register` accept a return path only when it is a local path (one leading
`/`, never `//`). The page reads the parked request, which is when the
server fetches the app's document, and shows:

- the app: the `client_id` host as the heading and the full URL beneath it;
  the document's `client_name` and `client_uri` as text, labeled as what
  the app says about itself, with a warning when the `client_uri` host
  differs from the `client_id` host;
- where the code goes: the full `redirect_uri`, with a warning when its
  host differs from the `client_id` host, and the local-app warning;
- the repository the token will open;
- each requested scope as a sentence, with a checkbox, and the kinds it
  reaches today; a glob says that it also covers kinds added later;
- when a write is asked for, the actor its changes are recorded as
  (`app:<host>`) and what they set off: the owner's triggers and mappings
  respond to them like any other change;
- the lifetime: 7 days, 30 days (the default), 90 days or until revoked;
- Approve and Deny.

### The lifecycle

A parked request moves `pending → approved | denied`, once, under a lock.
Approval is bound to the repository of the bearer that approves it and
must name a non-empty subset of the requested scopes (resolved: `~`
completed) and one of the offered lifetimes; anything else is refused and
leaves the request pending. Approval mints a one-time code and answers the
redirect URL with `code`, `state` and `iss` (RFC 9207); the page navigates
to it with `window.location.assign`, since a form POST that redirects off
the console's origin would break its `form-action 'self'` policy. Denial
answers the redirect URL with `error=access_denied`, `state` and `iss`.

The code is 32 random bytes, stored by its SHA-256, valid for 60 seconds,
and bound to the issuer, the repository, `client_id`, `redirect_uri`, the
PKCE challenge, the approved scopes, the lifetime and the app's claimed
name. `POST /oauth/token` reads `application/x-www-form-urlencoded` (at
most 8 KB, each parameter once): `grant_type=authorization_code`, `code`,
`client_id`, `redirect_uri`, `code_verifier` (43 to 128 characters of the
RFC 7636 alphabet) and an optional `resource`. Under a lock it checks the
issuer, `client_id`, `redirect_uri` and the verifier against the code: a
presentation that fails any of them is `invalid_grant` and spends nothing,
and the first that passes redeems the code. A code presented again after a
redemption, with every binding and the verifier correct, revokes the token
the first redemption minted (OAuth 2.1 §4.1.3), retrying in the background
if the database refuses the delete; without the verifier it is only
`invalid_grant`, so a leaked code alone cannot revoke anything. A mint that
fails leaves the code spent and the app starts over. Redeemed codes are
remembered until 10 minutes after they were minted. Errors are RFC 6749
JSON: `invalid_request`, `invalid_grant`, `unsupported_grant_type` and
`invalid_target` with status `400`, `server_error` with `500` and
`temporarily_unavailable` with `503`. Every answer is
`Cache-Control: no-store`. The endpoint has no pace of its own: only a
256-bit code with its verifier succeeds there. A success is:

```json
{
  "access_token": "substrate_tok_…",
  "token_type": "Bearer",
  "scope": "read:ada.example.com/tasks/* write:ada.example.com/tasks/task",
  "expires_in": 2592000,
  "repository": "ada.example.com"
}
```

`repository` tells the app which authority it reached, as IndieAuth's `me`
does. There are no refresh tokens: when a token expires, the app sends the
owner through the approval page again.

`POST /oauth/revoke` reads `token` (form-encoded) and revokes exactly the
token whose secret it is, through the ordinary record delete; it answers
`200` whether or not the token existed (RFC 7009 §2.2). A `client_id`, when
sent, must match the token's or nothing is revoked.

Parked requests and codes live in the server's memory, bounded and swept on
expiry. A restart forgets them and the app starts over; tokens themselves
are records in the changelog like every other token. One server process
writes a repository
([0083](../decisions/0083-a-repository-has-one-writer-and-a-second-is-refused-at-open.md)),
so there is no second process to share the state with.

## Scopes

A scope is `read:<pattern>` or `write:<pattern>`, and write includes read
of the same pattern. The pattern is the kind grant grammar of
[0080](../decisions/0080-a-kind-grant-may-glob-and-a-glob-never-reaches-auth-material.md):

| Pattern | Reaches |
| --- | --- |
| `<authority>/<package>/<name>` | one kind |
| `<authority>/<package>/*` | every kind of one package |
| `<authority>/*` | every kind of one authority |
| `*` | every kind the owner has |

`~` in the authority position means the repository's own authority, the one
an imported sample is rehomed under
([0048](../decisions/0048-providers-are-published-samples-are-copied.md)).
An app cannot know `ada.example.com` before the owner signs in, so it asks
for `read:~/tasks/*`; the approval page shows, the code binds and the token
stores `read:ada.example.com/tasks/*`. A token never carries `~`.

The rules every scope keeps, whatever it says:

- **The auth kinds are never reached**: `core/token`, `core/credential`,
  `core/secret` and `core/recoverykey`, read or write. A scope naming one is
  refused.
- **A glob never matches an `internal` kind**
  ([0133](../decisions/0133-a-kind-declares-its-purpose.md)), the `core`,
  `llm` and `console` machinery; an exact scope may read one, such as
  `read:substrate.reamde.dev/core/kind` for an app that renders forms.
- **No scope writes the `core` or `llm` packages**, which hold triggers,
  functions, agents, policies, providers, interactions and the vocabulary.
  A scope naming either for write is refused; a wider glob skips them.
- **No scope writes an account or a bundle input**, the connection records
  the owner manages: the engine refuses every put, patch and delete of one
  from an app's token.

A request whose scope is missing, ill-formed or refused by these rules is a
`400` page at `/oauth/authorize`. `offline_access` is not offered and is
ignored.

## What a scoped token can do

| Surface | Scoped token |
| --- | --- |
| `GET` a record path | readable kinds |
| `PUT`, `PATCH`, `DELETE` a record path | writable kinds |
| `POST /api/v1/records` | writable kinds |
| `GET /api/v1/records`, list and search | readable kinds; no `filter.kinds` means every readable kind; search is lexical only |
| `GET /api/v1/units`, `/units/convert` | yes |
| `POST /oauth/revoke` | the token presented |
| everything else | `403 insufficient_scope` |

Every record a scoped token receives, from a read or as the answer to a
write, is cut to the scope: `linkedFrom` entries, property provenance and
`expand` referents of kinds it cannot read are dropped, and a kind it cannot
read is answered as if it did not exist. A reference value inside a record
it can read stays visible: the target's id is part of the record. Pages
carry no changelog head.

"Everything else" is tokens, export, vocabulary, catalog, bundles,
triggers, function and agent calls, merge and split, blobs, aggregate
reads, window reads, the change feed and its watch, the provider connect
flow and the sync status. Each can be opened later with a design of its own;
the change feed needs one because its payloads, `affected` lists and text
search reach across kinds.

## Enforcement

Three layers, each enough for what it holds:

1. **Routes.** The `/api/v1` routes split into a scoped group (the table
   above) and an owner-only group that refuses a scoped token before its
   handler runs; the door routes (`/tokens`) and the export are owner-only.
2. **The dataset.** When the token record carries `scopes`, the API hands
   handlers a `scopedDataset` instead of the engine's dataset. It implements
   every `substrate.Dataset` method explicitly and embeds nothing, so a
   method added later does not compile until somebody decides what a scoped
   token may do with it; the ones outside the table answer
   `substrate.ErrForbidden`. It checks the kind before any lookup, narrows
   lists, forces lexical search and cuts every returned record. An empty
   expansion of the readable kinds answers an empty page and never reaches
   the engine, where no kinds means every kind.
3. **The engine.** `substrate.WithScopes` marks the request context, and the
   transaction's `ownerHand` (owner tier and no scope) replaces the bare tier
   at every owner-only gate: revoking a token, any write of an account or a
   bundle input, adjusting a change request, `writer: owner` properties and
   `StartOAuth`.

Scopes restrict what an app does directly. What the owner's own
declarations do in answer to an app's write (a trigger the owner installed,
a mapping, a reference's cascade, the embedding of a written property) runs
as it does for any write, and the approval page says so.

The token record (`substrate.reamde.dev/core/token`, version 8) gains three
optional properties: `scopes` (repeated string), `clientId` and
`clientName`. A record without `scopes` has full access, which is the
retrofit 0143 wrote down, so no existing token changes. The mint refuses an
empty list and anything that is not a canonical, resolved scope, and
`Authenticate` refuses a token whose stored `scopes` does not parse, so a
damaged grant never reads as full access. A scoped token writes as the
app's own actor, `app:<host>`, whatever `X-Substrate-Actor` says: the
`app:` prefix is reserved like `function:` and `agent:`, a package may not
declare one, and the engine resolves it to the owner tier ahead of the
registry, so it holds a property as the owner's own client does while the
owner-only actions stay shut through the scope mark. The principal on every
changelog entry is the token, which names the exact app.

## Console

- The approval page above.
- `/login` and `/register` return to a local path after signing in or
  registering, including when the owner was already signed in.
- The tokens page lists app tokens in their own section: the app's host and
  claimed name, its scopes, when it was approved and when it expires, and
  Revoke. The other sections list only full-access tokens.

## Out of scope for this version

Refresh tokens; remembered approvals; narrowing a glob on the approval page;
aggregate and window reads, the change feed's history, blobs, function calls
and the agent call API for apps (chat, decisions and the feed's live tail
have their design in
[0195](../decisions/0195-an-app-talks-to-agents-decides-their-requests-and-tails-the-feed-it-reads.md));
fetching and serving an app's logo (the console's `img-src 'self'`
blocks a remote one, and CIMD §8.8 asks the server to proxy it);
`substratectl` as an app; an MCP endpoint.

## Review record

Round 1 (Codex, gpt-6-astra) raised ten blockers and four other findings.
Resolved here:

- Origin mode removed: an `https` app must publish its document.
- Redirect URIs limited to `https` and loopback `http`, checked the same way
  at every step; return paths after sign-in limited to local paths.
- The document fetcher has its own transport: public unicast only, no
  proxy, no operator exception, bounded and rate-limited.
- The lifecycle is explicit and atomic; replay revocation needs the
  verifier; codes bind the issuer; `resource` is checked at both endpoints.
- Account and bundle-input writes refused on every mutation; `writer: owner`
  and the change-request adjustment read `ownerHand`.
- Search is lexical for a scoped token; indirect effects of a write are the
  owner's automation and are disclosed.
- One evaluator (`internal/scope`) holds the grammar; empty grants refused at
  approval, at mint and at `Authenticate`; empty expansions never reach a
  query.
- Every returned record is cut, write answers included; window reads and the
  change feed deferred.
- Routes split into scoped and owner-only groups ahead of the dataset
  wrapper.
- Metadata, errors, CORS, `WWW-Authenticate` and revocation specified.
- Lifetimes shortened and capped at 90 days; a scoped token's actor pinned to
  `api`. (The owner later asked for "until revoked" as an option and for the
  app's own actor; see the last round.)

Round 2 (Codex) moved the binding checks before a code is spent, made `http`
an issuer only on a loopback host with `SUBSTRATE_PUBLIC_URL` for a fixed
one, refused a null, empty or unparseable stored grant everywhere, bound a
parked request to the first repository that reads it and to the document
the page showed, and spelled out on the page what a write also does.

Round 3 (Codex, on the implementation) found no route or scope bypass and
five smaller leaks, all fixed with a test each: a reference condition naming
a kind the grant does not read is refused (the engine resolves it through
the target's merge history); `implements` and `purposes` are applied to the
readable kinds before the engine sees them, so a hidden implementor cannot
change the answer; a replay's revoke is logged and retried; the first
snapshot of the app's document stands; the approval calls are `no-store`.

A review workflow over the same commit (five area reviewers, each finding
checked by a skeptic) confirmed more, also fixed with tests: a write's
reference values must name kinds the grant reads (the engine loads the
referent for existence and titles); an app's search weighs every term alike
and carries no scores (document frequencies count every kind); an app's
list cursor carries no head; an app's token never moves a record into a
state that applies a diff or a merge; a Unicode host in a `client_id` or a
`redirect_uri` is refused (it is dialed as punycode, so a lookalike would
head the page); dot segments are checked on the decoded path; the special
ranges are checked through NAT64 and 6to4; one source holds at most 32
parked requests; the token and revocation endpoints lost their per-address
pace; record paths split on their first three segments, since an id may
carry a slash.

The owner's review of the screenshots asked for three changes, all made: the
approval page's copy says where you go next and drops the line about what
the token opens; an app writes as its own `app:<host>` actor instead of
`api`, so History shows the app and never the owner; and "until you revoke
it" is a lifetime the owner may pick.

The owner then pointed out that a document on a shared host makes the host
meaningless (`raw.githubusercontent.com`, `pastebin.com`), so a web app's
redirect now has to be on its `client_id`'s origin or a loopback address,
and the page marks a loopback redirect as a program on this computer that
nothing verifies.

Before the merge, Claude Fable 5.1 and Codex (gpt-6-astra) each reviewed the
whole PR. Both found no route or scope bypass. Fixed from their findings: a
reference nested in an object or a keyed map is now held to the grant like a
top-level one (the engine walks those too); a bare id that carries a slash
completes under its pin; an app's search scores with no repository-wide
statistics at all, so neither a term's weight nor the average length moves
with hidden records; the fetcher's loopback test now proves its dial guard;
`fec0::/10` is refused; and the change carries a `BREAKING CHANGE` footer,
since `app:` became a reserved actor prefix.
