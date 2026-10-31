# Authorizing apps

An app can get a token that reaches only the kinds the owner approves,
without registering anywhere first. The app sends the owner to their
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
- **Where the access goes**: the redirect URI the code is sent to, with a
  warning when it is on another host than the client id.
- **The repository** the token opens.
- **The requested access**: one row per scope, with the kinds it reaches
  today. Uncheck a row to leave it out. A pattern ending in `*` also covers
  kinds you add later.
- **What a write also does**: a write by the app counts as a write by you.
  It can start your triggers, which may run code you installed and call
  services that charge you, and it can change or delete records outside the
  approved kinds through a mapping or a cascading reference.
- **How long the access lasts**: 7, 30 or 90 days. There is no refresh: when
  it ends, the app asks again.

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
`redirect_uris` must list every redirect URI the app uses, each `https` or
`http` on a loopback host (a loopback entry matches any port). A document
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
for. `repository` is the authority the token opens.

### Use the token

Send it as `Authorization: Bearer substrate_tok_…`. An app's token reaches
the records route (`GET /api/v1/records` for lists and lexical search,
`POST /api/v1/records`), the record path (`/api/v1/{authority}/{package}/{kind}/{id}`)
and the unit reads, held to its scopes:

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
  are left out of every record it receives.

Everything else answers `403`: the change feed and its watch, aggregate and
window reads, blobs, the vocabulary, the catalog, functions, agents, the
export and the token endpoints. Writes are attributed to `api` whatever
`X-Substrate-Actor` says; the changelog's principal names the token, and the
token names the app.

### Revoke

`POST /oauth/revoke` with `token=…` (form-encoded) revokes that token
(RFC 7009). It answers `200` whether or not the token existed.

## Scopes

A scope is `read:<pattern>` or `write:<pattern>`, and write includes read.

| Pattern | Reaches |
| --- | --- |
| `<authority>/<package>/<name>` | one kind |
| `<authority>/<package>/*` | every kind of one package |
| `<authority>/*` | every kind of one authority |
| `*` | every kind the owner has |

`~` in place of the authority is the repository's own authority, the one an
imported sample lives under: `read:~/tasks/*` becomes
`read:ada.example.com/tasks/*` on the approval page and in the token. Some
things no scope reaches:

- the auth kinds (`substrate.reamde.dev/core/token`, `…/credential`,
  `…/secret`, `…/recoverykey`), read or write;
- an `internal` kind through a pattern ending in `*`; an exact scope such as
  `read:substrate.reamde.dev/core/kind` may read one;
- a write to `substrate.reamde.dev/core` or `substrate.reamde.dev/llm`, which
  hold the functions, triggers, agents, providers and policies;
- a write to an account or a bundle input, the connections the owner manages.

## Running it

The issuer is `https://` and the request's `Host`, or `http://` on a loopback
host. Set `SUBSTRATE_PUBLIC_URL` when the server answers under more than one
name, so every app sees one issuer ([configuration](operations.md)). A flow
is bound to the issuer it began on.

Parked requests and codes live in the server's memory: a restart forgets
them, and an app that was mid-flow starts over. One address (or one IPv6
/64) holds at most 32 parked requests. The tokens are records like any
other.

A binary from before app authorization reads an app's token as a
full-access one, since it does not know `scopes`. Before rolling back past
this change, revoke every app token under Settings > Apps.
