# Authorizing apps

An app can get a token that reaches only the kinds and agents the owner
approves, without registering anywhere first. The app sends the owner to their
substrate, the owner signs in and approves the request on the console's
approval page, and the app exchanges the code it receives for a token. The
protocol is OAuth 2.1's authorization code flow with PKCE, for public clients
whose `client_id` is a URL
([0194](decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)).

Every other token, the console's session, a `substratectl` login, a token
minted with `POST /tokens`, has full access to its repository and is
unchanged ([tokens](auth.md)).

## For the owner

The approval page shows, from top to bottom:

- **The app**: the host of its client id, which is the one fact the app's
  URL proves, and the full client id beneath it. The name and site the app
  gives for itself are shown as its claim, with a warning when that site is
  on another host.
- **After you answer, you go to**: the address your browser returns to,
  which receives the access. It is always on the app's own host, or on
  this computer (a loopback address), which the page warns nothing verifies.
- **The repository** the token opens.
- **The requested access**: one row per scope, with the kinds it covers
  today. Uncheck a row to leave it out. A pattern ending in `*` also covers
  matching kinds you add later. A row that asks to talk to agents names
  each agent and what it can read and change, since a chat reaches whatever
  the agent may do; a row that asks to apply or dismiss what agents suggest
  names the agents and says the app decides with your authority. A pattern
  ending in `*` also covers matching agents you add later.
- **Its changes appear as**: when the app asks to write, the name its
  changes are recorded under, `app:` and its host. History shows them as the
  app's, never as yours. Your triggers and mappings respond to its changes
  like any other, so they may run code you installed, call services that
  charge you, or change records the app cannot reach itself.
- **What it sets up**: when the app needs something on your server, one row
  per thing: a provider to install or upgrade (with what it requires, the
  kinds it adds, the code it runs and its triggers), the account it
  delivers under, with every value it writes, and the webhook it turns on.
  A row already in place says so. A row the server cannot set up says why
  and where to go about it, and the request cannot be approved until it is
  settled or denied.
- **How long the access lasts**: 7, 30 or 90 days, or until you revoke it.
  There is no refresh: when it ends, the app asks again. A token approved
  until revoked works for whoever holds it until you revoke it, a leaked one
  included.

Approving a request that sets something up runs the set up on your server,
as you, before the app receives anything: the page shows each row as it
runs, and the app gets its code once every row landed. If a row fails, the
request waits for you again with what failed; what landed stays, as it does
when you install from a provider's page, and approving again runs what is
left. One set up runs at a time in a repository, so yours may wait for
another; if what yours would do has grown meanwhile (a row you saw in place
needs work again), it stops before it changes anything and waits for you to
approve again. Closing the page does not stop a set up that has started, and
once it finishes the request waits ten minutes for the page to come back and
take you to the app.

An app on your own computer (a command-line tool, a desktop app) has a
`client_id` on `127.0.0.1` or `localhost`. Nothing about it can be verified,
not even its name, so the page says it only claims to run on your computer.
Approve one only if you started it yourself.

App tokens are listed under **Apps** in Settings, with the app, its scopes,
when you approved it and when it expires. **Revoke** deletes the token, like
any other.

## For an app

### Publish a client ID metadata document

A web app's `client_id` is an `https` URL that serves a JSON document
describing the app. Serving it is the whole of registration:

```json
{
  "client_id": "https://notes.example.org/oauth/client.json",
  "client_name": "Notes",
  "client_uri": "https://notes.example.org",
  "redirect_uris": ["https://notes.example.org/oauth/callback"],
  "token_endpoint_auth_method": "none"
}
```

`client_id` must equal the URL the document is served at, and
`redirect_uris` must list every redirect URI the app uses. Each one is on the
`client_id`'s own origin (`https`, the same host and port), or `http` on a
loopback address (a loopback entry matches any port). Serve the document
from the host that receives the access: a document on a shared host (a raw
file on GitHub, a paste) can name only a callback on that origin or a
loopback one, and the approval page would be headed with the shared host's
name. A document
that carries a `client_secret`, a `jwks` or `jwks_uri`, or another
`token_endpoint_auth_method` is refused: an app here is a public client. A
host is named in ASCII: an internationalized name is written as its
punycode (`xn--…`), because the server dials that form and the approval page
must show the host it dials. The
server fetches the document after the owner signs in, from public addresses
only, without following redirects, within 5 seconds and 5 KB, and caches it
for 10 minutes.

A local app uses a loopback `client_id` instead, such as
`http://127.0.0.1/my-tool`, which is never fetched. Its `redirect_uri` must be
a loopback `http` URL, on any port.

### Native apps

- **A mobile app** uses a claimed `https` callback on its own domain. It
  serves its document at, say, `https://notes.example.org/oauth/client.json`
  and lists `https://notes.example.org/oauth/callback`. The owner signs in
  and approves in the system's browser sheet, the app receives the callback,
  and it exchanges the code with its PKCE verifier and keeps the token in
  the Keychain or the Keystore:
  - on iOS 17.4 or later, the app opens `ASWebAuthenticationSession` with
    `callback: .https(host:path:)` for that host and path, and the domain is
    one of the app's associated domains. An older iOS has no `https`
    callback for the session, so it cannot take part;
  - on Android, the callback path is a verified App Link (`autoVerify`, and
    the domain's `assetlinks.json` names the app), opened from a Custom Tab.
    A link that fails verification opens in the browser instead, on the
    app's own site.

  When the platform has verified the domain, only the app the domain names
  receives the callback, so the host on the approval page stands for the
  app.
- **A desktop app or a CLI** listens on a loopback port: either a document
  on its website listing `http://127.0.0.1/callback`, or a loopback
  `client_id` with no document. The page warns that a program on this
  computer receives the access.
- **A custom URL scheme** (`com.example.notes:/callback`) is not accepted:
  any app on the device may claim one, so nothing would tie the callback to
  the app the page names.

A native app asks the owner for their substrate's address, the way an app
for a federated service asks for an instance, and reads
`/.well-known/oauth-authorization-server` there.

### Discover the server

`GET /.well-known/oauth-authorization-server` (RFC 8414) names the issuer and
the endpoints. `GET /.well-known/oauth-protected-resource` (RFC 9728) names
the resource, and a `401` from the API carries
`WWW-Authenticate: Bearer resource_metadata="…"` pointing at it.

### Ask for access

Send the browser to the authorization endpoint:

```
GET /oauth/authorize?response_type=code
  &client_id=https://notes.example.org/oauth/client.json
  &redirect_uri=https://notes.example.org/oauth/callback
  &scope=read:~/tasks/* write:~/tasks/task
  &code_challenge=E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM
  &code_challenge_method=S256
  &state=af0ifjsldkj
```

PKCE with `S256` is required, and `scope` is required. A malformed request is
answered with an error page, never a redirect, because the redirect URI is
not verified yet. The owner's answer comes back to the redirect URI as
`code`, `state` and `iss` (RFC 9207), or as `error=access_denied`.

### Ask for set up

An app that needs more than access, a provider installed, the account it
delivers under, a webhook turned on, names it in `authorization_details`
(RFC 9396) beside `scope`: a JSON array of at most 8 entries in at most
2 KB.

```json
[
  {"type": "package", "package": "providers.substrate.reamde.dev/applehealth", "version": 5},
  {"type": "account", "kind": "providers.substrate.reamde.dev/applehealth/account",
   "id": "phone-1a2b3c4d", "properties": {"displayName": "Ada's iPhone"}},
  {"type": "webhook", "trigger": "applehealth-webhook"}
]
```

- `package`: a provider or a first-party catalogue this server's catalog
  ships, at `version` or later. Installing it installs what it requires
  first, as the provider page does. A sample is not set up this way: the
  owner adds it from All data. A package the catalog does not ship, the
  owner's own or a sample they imported, cannot be named, installed or not.
- `account`: one record of a kind that implements
  `substrate.reamde.dev/core/accountconfig`, in a package a `package` entry
  of the same request names. It is created once: a row already there, live
  or deleted, is left alone. `properties` is at most 1 KB, of properties the
  kind declares, none of them a secret and none that only another hand
  writes.
- `webhook`: a trigger with a webhook source whose callable belongs to a
  package a `package` entry of the same request names. Approving turns it
  on, and gives it a key only when it has none; the key is never shared.
  The token carries `webhook:<trigger>`, which delivers to that trigger
  with the token itself. A request never asks for `webhook:` in `scope`.

An entry with a member its type does not take (`url` included, which is the
token response's), a type the server does not know, or the same entry twice
refuses the request with an error page. The authorization server metadata
lists `authorization_details_types_supported`. A server that does not list
them ignores the parameter and would approve the scopes alone, so send
details only to a server that lists them.

The owner sees one row per entry and approves them with the scopes or not
at all. The approval runs the set up before the code exists, so the code can
take longer to arrive than for scopes alone: up to 15 minutes, and an app
that waits on its redirect URI waits that long.

### Exchange the code

```
POST /oauth/token
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code&code=…&client_id=…&redirect_uri=…&code_verifier=…
```

```json
{
  "access_token": "substrate_tok_…",
  "token_type": "Bearer",
  "scope": "read:ada.example.com/tasks/* write:ada.example.com/tasks/task",
  "expires_in": 2592000,
  "repository": "ada.example.com"
}
```

The code lasts 60 seconds. A presentation that does not match its client,
redirect URI and verifier is refused and spends nothing; a second
presentation of a code that already worked revokes the token it minted (a
revoke the database refuses is logged and retried for several minutes).
`scope` lists what the owner approved, which may be less than the app asked
for, and a `webhook:` scope for each webhook it set up. `repository` is the
authority the token opens. `expires_in` is absent when the owner approved the
app until they revoke it: the token has no expiry.

A request that carried `authorization_details` is answered with them as
granted (RFC 9396 §7): each package with the `version` now held, each
account's `kind` and `id`, and each webhook's `trigger` with the `url` to
deliver to, `<issuer>/webhooks/<repository>/<trigger>`.

```json
"authorization_details": [
  {"type": "package", "package": "providers.substrate.reamde.dev/applehealth", "version": 5},
  {"type": "account", "kind": "providers.substrate.reamde.dev/applehealth/account", "id": "phone-1a2b3c4d"},
  {"type": "webhook", "trigger": "applehealth-webhook",
   "url": "https://ada.example.com/webhooks/ada.example.com/applehealth-webhook"}
]
```

### Check that it is still set up

`POST /api/v1/authorizations/check` takes the app's own token and the request
it would send now, `{"scope": "…", "authorization_details": […]}`, and
answers what has moved since it was approved:

```json
{
  "scopes": {"missing": ["read:ada.example.com/notes/*"]},
  "setup": [
    {"type": "package", "action": "upgrade",
     "package": {"id": "providers.substrate.reamde.dev/applehealth", "required": 6, "held": 5, "shipped": 6}}
  ],
  "expiresAt": "2026-12-01T09:00:00Z"
}
```

- `scopes.missing`: the requested scopes, resolved, that the token does not
  hold;
- `setup`: per entry, in the request's order, what approving would do now
  (`none` when it is in place, or `install`, `upgrade`, `create`, `arm`,
  `blocked`) and, for a package, the versions required, held and shipped.
  A row the token cannot see is `unknown`: a package unless one of its
  `read` or `write` scopes names a kind of the package, an account unless it
  reads the account's kind, a webhook unless it holds that `webhook:`. Treat
  `unknown` as nothing to ask about;
- `expiresAt`: when the token ends, absent for one approved until revoked.

When anything needs action, send the owner through the authorization
endpoint again with the whole request. The approval page compares the scopes
with the newest token the app holds, and shows what is in place as in place.
An owner's own token may ask too, and sees every row.

Ask at launch and when the app comes back to the foreground, at most every
few minutes. A token that asks again within seconds is answered `429` with
`Retry-After`.

### Use the token

Send it as `Authorization: Bearer substrate_tok_…`. An app's token reaches
the records route (`GET /api/v1/records` for lists and lexical search,
`POST /api/v1/records`), the record path (`/api/v1/{authority}/{package}/{kind}/{id}`),
the unit reads, the change feed's live tail, the set up check, blob
storage under the blob rule below, under an `agents:` scope the chat and the
thread routes, and under a `webhook:` scope the webhook door and that
trigger's parked list ([deliver to a webhook](#deliver-to-a-webhook)), held
to its scopes:

- a kind the token cannot read answers `404`, as an unknown kind does;
- a write to a kind it reads but cannot write answers `403` with
  `WWW-Authenticate: Bearer error="insufficient_scope"`;
- a list with no `filter.kinds` lists every kind it reads, and `implements`
  and `purposes` narrow those alone;
- a property condition on a reference, and a reference value in a write,
  must name a kind the token reads, or the request answers `403`;
- a search is lexical (a `semantic` one answers `403`), weighs every word
  alike and carries no scores, since a word's frequency is counted over the
  whole repository;
- a list carries no changelog head, in its body or in its cursor;
- inbound links, property provenance and expanded referents of other kinds
  are left out of every record it receives;
- a trigger (`substrate.reamde.dev/core/trigger`, which only an exact scope
  reaches) never carries its webhook key: `source.webhook.key` is left out
  of every trigger the token receives, and a condition or an order on
  `source` answers `403`;
- `PUT /api/v1/blobs` (and `…/blobs/{digest}`) stores bytes when the token
  writes at least one kind that declares a `blobref` property, and answers
  `403` otherwise. The answer carries the digest and size of the bytes sent
  and the name and media type the request gave, never what an earlier
  upload of the same bytes said.

Everything else answers `403`: the change feed's history, aggregate and
window reads, reading a blob, the vocabulary, the catalog, functions, the
agent call API, the export and the token endpoints. Writes are attributed to the app's own actor,
`app:` and the host of its client id, whatever `X-Substrate-Actor` says; no
request can claim that name. Two apps on one host share it, and the
changelog's principal (the token, which names the exact client id) tells
them apart. Labels and annotations an app writes go in its own namespace,
`app:<host>/…`, like an agent's or a function's.

### Talk to agents

An `agents:<pattern>` scope reaches the agents whose ids match, and the
records their chats are made of:

- the agent rows (`substrate.reamde.dev/core/agent`), to read; an agent
  hidden from chat is listed but refuses a chat with `422`, as a chat
  naming a thread of another agent does;
- `POST /api/v1/substrate.reamde.dev/core/agent/{id}/chat`, the id one
  path segment (`%2F` for its slashes), streaming as it does for the owner.
  The message is written as the app's, `app:<host>`; the agent runs under
  its own permissions, so it reads and writes what it would in the
  owner's chat;
- the threads (`substrate.reamde.dev/llm/thread`) whose `agent` matches: list,
  read, and `PATCH` of `subject` alone, to rename one;
- their messages (`…/llm/message`) and interactions (`…/llm/interaction`):
  list and read, and an interaction's `PATCH` of `state` (`answered` with
  `answers`, or `dismissed`) with `ifVersion`, as the console sends it;
- `GET …/llm/thread/{id}/events` and `POST …/llm/thread/{id}/stop` for
  those threads.

A `decide:<pattern>` scope reaches the change requests
(`substrate.reamde.dev/core/recordpatchrequest`) whose `thread` is a thread
of a matching agent: list and read, and a `PATCH` of `decision` to
`accepted` or `rejected` with the request's `ifVersion`, optionally an
`adjustedDiff` on an accept and a note under the app's own namespace
(`app:<host>/note`). The accept applies the change as the owner's would,
and the decision is recorded as the app's. An accept that would revoke a
token or write an account is refused. `decide:` without `agents:` decides
but does not chat.

A list of one of these kinds names that kind alone in `filter.kinds`. A
thread list is narrowed to the matching agents' threads, and an `agent`
condition must name only those: naming another agent answers `404`. A message, interaction or request list is
narrowed by `filter.properties.thread` (`eq` or `in`, bare thread ids or
paths), and it must name its threads: without a thread condition it
answers `403`, unless a `read:` scope reaches the whole kind. The engine
then filters before it pages, so no page and no cursor passes over a row
the token cannot read. Naming a thread the token cannot read answers `404`.
Any other property in a patch, a label, a create or a delete answers `403`.
A search never reaches these kinds.

### Follow changes

`GET /api/v1/changes?watch=1` is open to an app's token: the ndjson tail,
with its bookmark and generation, heartbeats, terminal error frame, `from`
and `generation` resume and `410` as for the owner
([the change feed](changelog.md)). It carries only the rows of records the
token reads, through its read and write scopes and the rows `agents:` and
`decide:` reach, and each row names alone: `seq`, `ts`, `op`, `kind`,
`recordId`, `actor`, and `affected` cut to records the token reads. No
`payload`, `hash` or trigger states. `values=1`, `q`, the history pages
(`before`, `first`, `runs=1`) and the forward read without `watch=1` answer
`403`; a `kinds` entry the token cannot read answers `404`.

An app's lists carry no head, so it opens the watch first, keeps the
bookmark, then reads its lists: every write after the bookmark arrives on
the watch.

A stream outlives the check at its start, so an app's token is read again
while it runs: before every batch of rows and at every heartbeat on the
watch, and at least every two seconds on an agent's stream (`chat` and
`events`). Once the token is revoked or past its approved time, the watch
ends with a terminal error frame whose code is `auth`, and an agent's
stream with an `error` event, before anything more is written. The turn
itself runs on; the app signs in again to follow it.

### Deliver to a webhook

A `webhook:<trigger>` scope names one webhook trigger by its id. An app
never asks for it in `scope`, which refuses it: only an approved `webhook`
authorization detail puts it on a token. Under it:

- `POST /webhooks/{authority}/{trigger}` takes the token as
  `Authorization: Bearer` in place of the trigger's key. The door admits the
  delivery when the token is live, opens the repository the path names and
  holds `webhook:` for that trigger; any other bearer is read as a key, so a
  revoked, expired or full-access token, or one without the scope, answers
  the door's one `404`, as a wrong key does. A signature the trigger
  declares is still checked, and the header never reaches the callable.
  Revoking the app, or its access ending, ends its deliveries.
- `GET /api/v1/substrate.reamde.dev/core/trigger/{id}/parked` lists that
  trigger's parked deliveries, whoever sent them. Another trigger's list,
  and the other trigger verbs, answer `403`.

`webhook:` does not read the trigger record, and no scope reads its key.

### Revoke

`POST /oauth/revoke` with `token=…` (form-encoded) revokes that token
(RFC 7009). It answers `200` whether or not the token existed.

## Scopes

A scope is a verb and a pattern:

| Verb | Pattern over | Reaches |
| --- | --- | --- |
| `read` | kind references | records of the matching kinds |
| `write` | kind references | the same, and writes them |
| `agents` | agent ids | chats with the matching agents and their threads ([talk to agents](#talk-to-agents)) |
| `decide` | agent ids | the change requests the matching agents propose ([talk to agents](#talk-to-agents)) |
| `webhook` | one trigger id, never a pattern | delivering to that webhook and its parked list ([deliver to a webhook](#deliver-to-a-webhook)) |

| Pattern | Reaches |
| --- | --- |
| `<authority>/<package>/<name>` | one kind, or one agent |
| `<authority>/<package>/*` | every kind or agent of one package |
| `<authority>/*` | every kind or agent of one authority |
| `*` | every kind or agent the owner has |

An agent's id is its authority, package and name,
`ada.example.com/llm/substrate`.
`~` in place of the authority is the repository's own authority, the one an
imported sample lives under: `read:~/tasks/*` becomes
`read:ada.example.com/tasks/*` on the approval page and in the token, and
`agents:~/llm/*` the agents imported with the LLM sample. Some things no
scope reaches:

- the auth kinds (`substrate.reamde.dev/core/token`, `…/credential`,
  `…/secret`, `…/recoverykey`), read or write;
- an `internal` kind through a pattern ending in `*`; an exact scope such as
  `read:substrate.reamde.dev/core/kind` may read one;
- a write to `substrate.reamde.dev/core` or `substrate.reamde.dev/llm`, which
  hold the functions, triggers, agents, providers and policies, beyond the
  few writes `agents:` and `decide:` carry;
- a write to an account or a bundle input, the connections the owner manages.

## Running it

The issuer is `https://` and the request's `Host`, or `http://` on a loopback
host. Set `SUBSTRATE_PUBLIC_URL` when the server answers under more than one
name, so every app sees one issuer ([configuration](operations.md)). A flow
is bound to the issuer it began on.

Parked requests and codes live in the server's memory: a restart forgets
them, and an app that was mid-flow starts over. One address (or one IPv6
/64) holds at most 32 parked requests. The tokens are records like any
other. A set up runs one at a time per repository, for at most 15 minutes;
a restart forgets its request too, and what it had landed stays, so the app
starts over and finds those rows in place.

A binary from before app authorization reads an app's token as a
full-access one, since it does not know `scopes`. Before rolling back past
this change, revoke every app token under Settings > Apps.
