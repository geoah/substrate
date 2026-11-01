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
unchanged ([tokens](auth.md)). An app may ask for that too, with the
[`full` scope](#full-access), approved whole on the same page.

## For the owner

The approval page leads with what the app will be able to do, in plain
words. From top to bottom:

- **Who asks**: "An app wants to connect to" your repository, headed by the
  host of the app's client id, which is the one fact the app's URL proves.
  The name the app gives itself is shown beneath it as its claim. When the
  access goes to a program on this device (a loopback address), one warning
  says that nothing confirms the program is the app.
- **It will be able to**: one sentence per requested scope, from what it
  does and the kinds or agents it reaches, each with a checkbox: uncheck one
  to leave it out. A pattern ending in `*` also covers matching kinds or
  agents you add later, and says so. A row that talks to agents says that a
  chat has them do anything they may do, and that the app reads every chat
  with them, yours included; a row that applies or dismisses what agents
  suggest says the app may edit a suggestion before applying it, as you.
- **It will set up on your server**: when the app needs something on your
  server, one row per thing: a provider to install or update, with the
  version, what it installs first and whether it runs code on your server;
  the account it delivers under, with every value it writes; and the
  webhook it turns on, with what that lets the app send. The webhook's
  access comes with the set up and has no checkbox: to refuse it, deny the
  request. A row already in place says so. A row the server cannot set up
  says why in one sentence and, where the console can fix it, links there;
  Approve stays off until **Check again** finds every row clear, or you
  deny the request.
- **Your automations run on its changes**, when the app asks to write: your
  triggers and mappings respond to its changes like any other, so they may
  run code you installed, call services that charge you, or change records
  the app cannot reach itself.
- **Access lasts**: 7, 30 or 90 days, or until you revoke it. There is no
  refresh: when it ends, the app asks again. A token approved until revoked
  works for whoever holds it until you revoke it, a leaked one included.

When you approved the app before, it asks your repository again, and the
page is headed by what changed since, with the host beneath: **Renew
access** (nothing new), **Allow more access**, **Update server setup**, or
both of the last two. While that access still works, what it
already covers folds under **Already allowed**, and denying leaves it
working; access that ended covers nothing, so everything is asked for in
full.

**Show technical details**, at the card's foot, is the console's one
Technical details switch
([0131](decisions/0131-the-console-writes-for-two-readers-behind-one-switch.md)),
so the choice follows you, and flipping it keeps every choice made on the
page. It adds the full client id; what the app's own document claims, with a
warning when the site it names is on another host; the address your browser
returns to; the repository; the access the app holds now; each scope as
written, with every kind it reaches by full reference; per set up row, the
package id, the version the app needs and the one this server ships, what it
installs first, the kinds it adds, its functions with what each may write,
and its triggers; the account's kind and id; the webhook's trigger, whether
it is on and whether it has a key, which is never shared; the name the app's
changes are recorded under, `app:` and its host, which history shows as the
app's and never as yours; and the `authorization_details` as the app sent
them, to copy.

Approving a request that sets something up runs the set up on your server,
as you, before the app receives anything: the packages first, each after
what it requires, then the account, then the webhook, whatever order the
rows are listed in. The page shows each row as it runs, and the app gets its
code once every row landed. Setting up continues if you close the page. If a
row fails, the request waits for you again with what failed under that row;
what landed stays, as it does when you install from a provider's page, and
**Try again** runs the rest. One set up runs at a time in a repository, so
yours may wait for another; if what yours would do has grown while it waited
(a row you saw in place needs work again, or the webhook now runs another
package's code), it stops before it changes anything and asks you to check
the request again. Once it finishes, the request waits ten minutes for the
page to come back and take you to the app. What a set up landed stays when
you later revoke the app.

An app on your own computer (a command-line tool, a desktop app) has a
`client_id` on `127.0.0.1` or `localhost`. Nothing about it can be verified,
not even its name, so the page reads "An app on this device" and warns that
it only claims to run there. Approve one only if you started it yourself.
Its page is always a first approval, never compared with what you approved
before, since anything on your computer may send the same client id.

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

[SubstrateKit](../apps/apple/SubstrateKit/README.md), the Swift package
Substrate's own Apple apps share, signs in through the loopback redirect, on
iPhone and Mac alike (RFC 8252); a claimed `https` callback is not in it.

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

The ids an entry names are the package's own: a provider's package id,
account kind and webhook trigger are in its section of
[the bundles catalog](bundles-catalog.md), and a well-known catalogue's
package in [built-in kinds](builtin-kinds.md).

Each entry is read against its type's members alone. A member the type does
not take (`url` included, which is the token response's), a required member
missing or malformed, a type the server does not know, or the same entry
twice refuses the request with an error page. The authorization server
metadata lists `authorization_details_types_supported`. A server that does
not list them ignores the parameter and would approve the scopes alone, so
send details only to a server that lists them.

**`package`**: a provider or a first-party catalogue this server's catalog
ships. Approving installs it, or upgrades it to the version the catalog
ships, after what it requires, as the provider page does; one held at
`version` or later is left as it is.

| Member | Required | Holds |
| --- | --- | --- |
| `type` | yes | `package` |
| `package` | yes | a package id, `<authority>/<package>` with no `~`; two entries naming one package are the same entry |
| `version` | yes | the least version the app needs, 1 or later |

**`account`**: the record the app delivers under, created once: an account
already there, live or deleted, is never written.

| Member | Required | Holds |
| --- | --- | --- |
| `type` | yes | `account` |
| `kind` | yes | a kind named in full, `<authority>/<package>/<name>` with no `~`, outside `substrate.reamde.dev/core`, `…/llm` and `…/console` |
| `id` | yes | a record id other than `default`, the one a package's inputs resolve first; the same `kind` and `id` twice are the same entry |
| `properties` | no | an object of at most 1 KB as JSON, every value of which the owner sees before it is written |

The kind implements `substrate.reamde.dev/core/accountconfig` and belongs to
a package a `package` entry of the same request names. Each property is one
the kind declares, and none is a `secret` (the owner types those), `managed`
(the server's to write) or restricted to a `writer` other than `owner`.

**`webhook`**: the trigger the app delivers to.

| Member | Required | Holds |
| --- | --- | --- |
| `type` | yes | `webhook` |
| `trigger` | yes | one trigger's record id, with no `/`; the same trigger twice is the same entry |

The trigger takes webhook deliveries, declares no
`source.webhook.signature` (a provider that signs what it sends holds the
secret, so no app can deliver there), and runs a callable of a package a
`package` entry of the same request names. Approving turns it on, and gives
it a key only when it has none; the key is never shared. The token carries
`webhook:<trigger>`, which delivers to that trigger with the token itself. A
request never asks for `webhook:` in `scope`.

What the error page refuses is the request's shape. What the repository
decides is the approval page's: a row approving cannot run keeps the action
it would run and carries `blocked`, `{"reason", "problem", "fix"}`, and the
request cannot be approved while one does. `problem` is the server's one
sentence for it in the owner's words, and `fix`, present where the owner can
clear it in the console, is the console path that does: `/data`, or a
package's provider page, `/providers/<authority>/<package>`. A requirement
that cannot be installed blocks the row of the package that needs it.

| `reason` | The row cannot run because | `fix` |
| --- | --- | --- |
| `server-update` | the catalog ships the package, or one it requires, below the version needed | none: only a newer server ships it |
| `not-shipped` | the catalog does not ship the package, or one it requires (the owner's own packages and the samples they imported included) | none |
| `sample` | the package, or one it requires, is a sample, which the owner adds from All data | `/data` |
| `quarantined` | the repository's copy of the package, or of one it requires, is quarantined | its provider page |
| `declared-by-owner` | the owner declared the package, or one it requires, by hand (its package record is `installed` and no provider install stamped it, whether it declares kinds or only functions or traits), and installing or upgrading the published one would replace theirs; one they hold as a bundle at the version needed is in place and left alone | its provider page |
| `upgrade-needs-review` | the upgrade's preview refuses it, would lose data or discard edits, or could not run: the provider page asks the owner first | its provider page |
| `not-requested` | the account or the webhook belongs to a package no `package` entry names, or the trigger is neither in the repository nor shipped by a requested package | none |
| `account-deleted` | the account was deleted and the server has not collected the deletion yet; once it has, approving creates the account again | none |
| `unsupported` | the account's kind is no account, its package declares no such kind, or a property is one it may not set; the trigger takes no webhook, its provider signs it, or it runs code of no package | none |

The owner sees one row per entry and approves them with the scopes or not
at all, as themselves: a request that carries entries is approved through
the console, `substratectl` or the API, and an approval under any other
`X-Substrate-Actor` is refused with `403` before anything is planned, the
request still waiting for the owner. The approval runs the set up before the
code exists, so the code can take longer to arrive than for scopes alone:
the set up has up to 15 minutes, and the request then waits up to 10 more
for the approval page to come back and send the browser on, so an app that
waits on its redirect URI may wait up to 25 minutes.

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
  "repository": "ada.example.com",
  "token_id": "2mjnmydr2be6"
}
```

The code lasts 60 seconds from the moment the approval page sends the
browser to the redirect URI, and after a set up never more than ten minutes
from the set up's end. A presentation that does not match its client,
redirect URI and verifier is refused and spends nothing; a second
presentation of a code that already worked revokes the token it minted (a
revoke the database refuses is logged and retried for several minutes).
`scope` lists what the owner approved, which may be less than the app asked
for, and a `webhook:` scope for each webhook it set up. `repository` is the
authority the token opens, and `token_id` the token record the owner lists the
app under. `expires_in` is absent when the owner approved the app until they
revoke it: the token has no expiry.

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
it would send now, `{"scope": "…", "details": […]}`: `details` is the
`authorization_details` array, read by the same rules and bounds as at the
authorization endpoint, and what would refuse the request there answers
`400` here. It answers what has moved since the app was approved:

```json
{
  "scopes": {"missing": ["read:ada.example.com/notes/*"]},
  "setup": [
    {"type": "package", "action": "upgrade",
     "package": {"id": "providers.substrate.reamde.dev/applehealth", "required": 6, "held": 5, "shipped": 6}},
    {"type": "account", "action": "none",
     "account": {"kind": "providers.substrate.reamde.dev/applehealth/account", "id": "phone-1a2b3c4d"}},
    {"type": "webhook", "action": "none",
     "webhook": {"trigger": "applehealth-webhook", "package": "providers.substrate.reamde.dev/applehealth"}}
  ],
  "expiresAt": "2026-12-01T09:00:00Z"
}
```

- `scopes.missing`: the requested scopes, resolved, that the token does not
  cover. Coverage is by reach, not spelling: a `write` covers the `read` of
  the same pattern, and a wider pattern a narrower one (`read:*` covers
  `read:~/notes/*`), except that a kind that is `internal`, or that the
  repository does not hold yet, is covered by its exact scope alone;
- `setup`: per entry, in the request's order, the `action` approving would
  run now: `none` when it is in place, else `install`, `upgrade`, `create`
  or `enable`. A row that cannot run keeps its action and carries `blocked`,
  as on the approval page ([the reasons](#ask-for-set-up)). A package row
  carries the versions required, held and shipped (`held` absent when the
  repository holds none, `shipped` when the catalog ships none), and a
  webhook row the package whose code its trigger runs. A row the token
  cannot see is `unknown`, with no `blocked` and nothing but what names it
  (a package's `id` and `required`, an account's `kind` and `id`, a
  webhook's `trigger`): a package unless one of the token's `read` or
  `write` scopes names a kind of the package, an account unless the token
  reads the account's kind, a webhook unless the token holds that
  `webhook:`. Treat `unknown` as nothing to ask about;
- `expiresAt`: when the token ends, absent for one approved until revoked.

When anything needs action, send the owner through the authorization
endpoint again with the whole request. The approval page compares the
scopes with the newest token the app holds, by the same coverage (never for
a local app), and shows what is in place as in place. A row blocked with a
`fix` is the owner's to clear first: its `problem` says why, and
`<server><fix>` opens the console page that clears it. An owner's own token
may ask too, and sees every row.

Ask at launch and when the app comes back to the foreground, at most every
few minutes, and only a server that lists
`authorization_details_types_supported`: the check came with them. Asking
is never refused for its pace: a token that sends the same body again
within 30 seconds is answered what it was answered last, and a check that
must plan waits its turn, one at a time for a token and four at once on the
server.

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
  upload of the same bytes said. An app's uploads wait for two slots of
  their own, never the owner's, and a token revoked or past its time while
  it waits or while the body arrives stores nothing;
- a blob reference in a record the token receives carries the blob's
  digest, size and status, and its name and media type only when the token
  reads the blob manifests (`read:substrate.reamde.dev/core/blob`): bytes
  are stored once by their digest, so the name is whoever uploaded them
  first.

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
  the door's one `404`, as a wrong key does. The token is read again once
  the body has arrived, so one revoked or past its time meanwhile delivers
  nothing. A signature the trigger declares is still checked, and the header
  never reaches the callable. Revoking the app, or its access ending, ends
  its deliveries. Looking a bearer up is a database read anybody can ask
  for, so one that looks like a token and is none is charged to its
  source's pace, and a source past it has its bearers read as keys,
  unlooked-up, until the pace refills.
- `GET /api/v1/substrate.reamde.dev/core/trigger/{id}/parked` lists that
  trigger's parked deliveries, whoever sent them. A delivery the door
  accepted that is still filing is listed too, with `pending: true`: no
  failure, and nothing to send again. Another trigger's list, and the other
  trigger verbs, answer `403`.

`webhook:` does not read the trigger record, and no scope reads its key.

What a delivery carries is the package's to say, since its code reads it:
the body, its content type, and the headers its trigger lets through
([triggers](functions.md#triggers)). Apple Health's webhook takes the Health
Auto Export app's JSON and files it under the account
`X-Substrate-Account: <account id>` names, a header that may be left out
while the repository holds one account; a delivery its code refuses after
the door's `202` parks with its body kept, on the parked list
([Apple Health](bundles-catalog.md#apple-health)). Each provider's section of
[the bundles catalog](bundles-catalog.md) names its package, kinds and
triggers, and, for a webhook, what a delivery to it carries.

### Revoke

`POST /oauth/revoke` with `token=…` (form-encoded) revokes that token
(RFC 7009). It answers `200` whether or not the token existed, and also,
revoking nothing, when the request names a `client_id` other than the one
the token was approved for. A server that could not look the token up or
delete it answers `503`: the token is still live, so ask again.

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

### Full access

`full` is a scope with no pattern: everything a password sign-in gets
([0204](decisions/0204-an-app-may-ask-for-full-access-and-its-owner-approves-it-whole.md)).
The Console app asks for it, so the owner signs in on the server's own page
in Safari rather than typing a password into the app. A request names it
alone, with no `authorization_details`; anything beside it is refused. The
approval page leads with a warning, offers nothing to uncheck, and takes the
approval only through a human door. Approving mints a token without scopes,
which is full access, keeping the app's client id and name: the owner sees
it under Apps, the app revokes it at the revocation endpoint or deletes its
own record (`token_id`) as a sign-in does, and its writes are the owner's,
not `app:<host>`. A server lists `full` in `scopes_supported` when it can
grant it.

## Running it

The issuer is `https://` and the request's `Host`, or `http://` on a loopback
host. Set `SUBSTRATE_PUBLIC_URL` when the server answers under more than one
name, so every app sees one issuer ([configuration](operations.md)). A flow
is bound to the issuer it began on.

Parked requests and codes live in the server's memory: a restart forgets
them, and an app that was mid-flow starts over. One address (or one IPv6
/64) holds at most 32 parked requests. The tokens are records like any
other. A parked request waits 10 minutes for the owner, and 10 again after
a set up stops part way; an approval refused before its set up ran leaves it
the time it had. A set up runs one at a time per repository, for at
most 15 minutes; a restart forgets its request too, and what it had landed
stays, so the app starts over and finds those rows in place.

A binary from before approvals set things up refuses every token that
carries `webhook:`, since its scope parser knows four verbs: the app's calls
answer `401` and its deliveries the door's `404`, which fails closed. There
is nothing to revoke: the token works again once the binary rolls forward,
and an app that took the refusal for its access ending signs in again then.
What a set up installed stays after any rollback.

A binary from before app authorization reads an app's token as a
full-access one, since it does not know `scopes`. Before rolling back past
that change, revoke every app token under Settings > Apps.
