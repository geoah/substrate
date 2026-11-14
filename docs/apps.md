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

- **Who asks**: "An app wants to connect to" your repository. A **verified**
  app is headed by its name, "by" the authority that publishes it, and the
  name its publisher's authority document gives itself: the authority's own
  DNS confirms the app is its, and the access goes to that authority
  ([name your publisher](#name-your-publisher)). Any other app is
  **unverified**: it is headed by the host of its client id, the name it
  gives itself is shown beneath as its claim, the page says where its
  access goes (an origin, or an app on this device), and, when the app
  names a publisher that could not be confirmed, which step failed. When
  the access goes to a program on this device (a loopback address), one
  warning says that nothing confirms the program is the app.
- **It will be able to**: one sentence per requested scope, from what it
  does and the kinds or agents it reaches, each with a checkbox: uncheck one
  to leave it out. A pattern ending in `*` also covers matching kinds or
  agents you add later, and says so. A row that talks to agents says that a
  chat has them do anything they may do, and that the app reads every chat
  with them, yours included; a row that applies or dismisses what agents
  suggest says the app may edit a suggestion before applying it, as you.
- **It will set up on your server**: when the app needs something on your
  server, one row per thing: a provider to install or update, with the
  version, what it installs first, whether it runs code on your server,
  whether that code uses the internet and what it reads and changes; the
  account it delivers under, with every value it writes; and the webhook
  it turns on, with what that lets the app send. The webhook's access
  comes with the set up and has no checkbox: to refuse it, deny the
  request. A row already in place says so. What a package's code reads
  and changes stays within the access you allow above
  ([0215](decisions/0215-code-an-approval-installs-reaches-no-further-than-the-scopes-it-approves.md)):
  unchecking a scope its code needs plans the set up again and the row
  says which access it needs, and checking it back clears it. A row the
  server cannot set up says why in one sentence and, where the console can
  fix it, links there; Approve stays off until **Check again** finds every
  row clear, or you deny the request.
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
warning when the site it names is on another host; the publisher check: the
authority the app names, the `_substrate` record the server read, the
authority document it names and whether that document lists the client id; the address your browser
returns to; the repository; the access the app holds now; each scope as
written, with every kind it reaches by full reference; per set up row, the
package id, the version the app needs and the one this server ships, what it
installs first, the kinds it adds, its functions with what each may read and
write and the hosts each declares, its triggers, and everything its code
reaches with what reaches it and the scope that covers it; the account's
kind and id; the webhook's trigger, whether
it is on and whether it has a key, which is never shared; the name the app's
changes appear under, which history shows as the app's and never as yours
(`app.appearsAs`, since revision 3 of the `apps` feature): the verified
authority, `local` for an app whose access goes to this device, or the host
of its client id, the `publisher` its [actor record](api.md#actors) will
carry; and the `authorization_details` as the app sent them, to copy.

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
any other. A package an approval installed stays when you revoke the app,
whoever publishes it: the row said so when you approved it, and you pause or
remove it from its page under Providers.

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
loopback address (a loopback entry matches any port). An unverified app
serves the document from the host that receives the access: a document on a
shared host (a raw file on GitHub, a paste) can name only a callback on that
origin or a loopback one, and the approval page would be headed with the
shared host's name. A verified app's callback may instead be on its
authority or a name under it, wherever the document is served
([name your publisher](#name-your-publisher)). A document
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

### Name your publisher

An app is **verified** when a domain whose DNS its publisher controls
confirms the app is its
([0209](decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)).
Serving a file from a host does not prove that, so the proof is a DNS record
the publisher adds once, and a document it names:

1. The client document names the publisher's authority:
   `"substrate_authority": "example.org"`. Anybody can write that line, so
   it only says which name to ask. A value that is not an authority name is
   ignored as a claim, and the document still works.
2. The authority publishes one TXT record at `_substrate.<authority>`:

   ```text
   _substrate.example.org.  TXT  "v=substrate1 doc=https://example.org/.well-known/substrate/authority.json"
   ```

   An optional `sha256=` pins the document's digest, in lower-case hex. Two
   `v=substrate1` records at one name, or one the server cannot parse, are
   no record.
3. The **authority document** at that URL lists the app's exact client id.
   It may be served from any host, because the DNS chose its URL:

   ```json
   {
     "authority": "example.org",
     "name": "Example",
     "description": "Example's apps.",
     "apps": [
       {
         "client_id": "https://cdn.example.net/workouts/client.json",
         "name": "Example Workouts",
         "packages": [],
         "apple": { "appIDs": ["ABCDE12345.org.example.workouts"] }
       }
     ]
   }
   ```

   `authority` must be the name the record was looked up under. The server
   fetches the document the way it fetches a client document (public
   addresses, no redirects, 5 seconds), up to 64 KB, ignores a key it does
   not read, and caches a success for 10 minutes. `packages` lists the
   packages the authority publishes, which an app's approval installs
   ([publish a package](#publish-a-package)).
4. The `redirect_uri` is `https` on the authority or a name under it, such
   as `https://apps.example.org/workouts/callback`.

The server checks this when the approval page reads the request, and once
before parking a request whose redirect is off the client id's origin.
Anything short of all four steps leaves the app unverified, with the step
that failed shown to the owner. A loopback `client_id` or a loopback
redirect is never verified: any program on the device can send either. An
unverified app may still ask for everything a verified one may, `full`
included; for `full` the page adds that nothing confirms which program
receives a token equal to the owner's password.

The token records the verified authority, and the app writes as an
[actor record](api.md#actors) of type `app` whose `publisher` is that
authority. An app whose access went to a loopback address writes as the app
whose publisher is `local`, and any other as the one whose publisher is the
host of its client id; the approval writes the record. Its label and
annotation keys go under its namespace, `app:` and its publisher
(`app:example.org/…`, `app:local/…`), and a token may still write keys under
`app:` and the host of its client id, the namespace an earlier binary gave
it.

The page shows the publisher it checked, and an approval and the token
exchange check it again: when the record was withdrawn or now names
something else, the approval asks the owner to reload the page, and the
exchange is refused.

To test a document before publishing it, the operator can have the server
read it from a file in place of the DNS lookup and the fetch:
`SUBSTRATE_INSECURE_AUTHORITY_DOCS=example.org=/path/to/authority.json`, a
comma-separated list. The server logs a warning naming each authority at
boot and refuses to start when a file cannot be read; `.mise/dev.sh` passes
the variable through. Nothing limits it to a development server, so it
shows where it acts: the approval page says the publisher was verified by a
file on this server, not by DNS, and the token records
`appAuthorityFromFile`. Never set it on a server anybody else reaches.

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

[SubstrateKit](../apps/kit/README.md), the Swift package
Substrate's own Apple apps share, signs in through a claimed `https` callback
in a release that names one, on iPhone and Mac alike, and through the
loopback redirect (RFC 8252) in a local build, and sends the build's
version as `software_version` and in the set up check. `mise run apps:site`
publishes the association files, the authority document, each app's client
document that the claimed callback needs, and every published version of the
packages the apps publish (Substrate Workouts' `training` and
`trainingcoach`, Substrate Tasks' `tasks`) with its closure,
which the app's approval installs as `package` details
([apps](../apps/README.md#releases-and-updates)). Sync asks for
every read and write the applehealth package's functions and triggers reach,
because a package an approval installs reaches no further than the approved
scopes.

A native app asks the owner for their substrate's address, the way an app
for a federated service asks for an instance, or for their repository's
name, and reads `/.well-known/oauth-authorization-server` on the server it
finds. A name that is not itself a server leads to one through its
`_substrate` TXT record (`v=substrate1 server=https://…`), else its
`/.well-known/substrate/repository.json`, else its parent host; the app then
sends the name as `login_hint` and refuses a token for any other
repository.

### Discover the server

`GET /.well-known/oauth-authorization-server` (RFC 8414) names the issuer and
the endpoints. `GET /.well-known/oauth-protected-resource` (RFC 9728) names
the resource, and a `401` from the API carries
`WWW-Authenticate: Bearer resource_metadata="…"` pointing at it.

An app that starts from a repository name rather than a server address finds
the server from the name:

1. If the name serves `/.well-known/substrate/server.json`, it is a server,
   and the sign-in page asks for the repository.
2. Otherwise read the TXT record at `_substrate.<name>`. Its `server=` is the
   origin of the server holding the repository:

   ```text
   _substrate.geoah.example.org.  TXT  "v=substrate1 server=https://substrate.example.net"
   ```

   With no record, try
   `https://<name>/.well-known/substrate/repository.json`. When both answer
   differently, the DNS record wins.
3. With neither, strip the first label and probe the parent as a server:
   `ada.substrate.example.net` sits on `substrate.example.net`, whose
   operator controls the name anyway.

Send the name as `login_hint` on the authorization request, so the sign-in
page fills the repository field. A hint that is not a valid repository name
is dropped. Then check the response's `iss` and the token response's
`repository` against what you resolved.

The record names the server and proves nothing about the repository on its
own: a repository that proved its name carries `domainVerifiedAt` on its
`repository` record ([Verify domain](auth.md#verify-domain)).

`/.well-known/substrate/server.json` also lists the server's features, each
with a stability and a revision ([discovery](api.md#discovery)). App
authorization is the `apps` feature, and it is `beta`: supported, with a
break announced one minor release ahead in that entry's `notice`
([stability](stability.md)). Before an app sends a request field that a
later release added, it checks that the feature's `revision` has reached the
number the field arrived at, because the server refuses a field it does not
know.

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

PKCE with `S256` is required, and `scope` is required. `software_version`
(RFC 7591's name) is optional: the build's own version, at most 64 printable
characters, which the token records beside the least version of each
`package` entry, so the server knows which build holds which packages
([0214](decisions/0214-an-apps-approval-installs-published-packages-and-declare-and-the-agent-detail-are-retired.md)).
A malformed request is answered with an error page, never a redirect,
because the redirect URI is not verified yet. The owner's answer comes back
to the redirect URI as `code`, `state` and `iss` (RFC 9207), or as
`error=access_denied`.

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
ships, or a package an authority document publishes
([keep a package of your own](#keep-a-package-of-your-own)). Approving
installs it, or upgrades it, to the version the catalog ships or the newest
the document lists at `version` or later, after what it requires, as the
provider page does; one held at `version` or later is left as it is.

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
(the server's to write) or restricted by its `writer` to types of actor
other than `person`.

**`webhook`**: the trigger the app delivers to.

| Member | Required | Holds |
| --- | --- | --- |
| `type` | yes | `webhook` |
| `trigger` | yes | one trigger's record id, with no `/`; the same trigger twice is the same entry |

The trigger takes webhook deliveries, declares no
`source.webhook.signature` (a provider that signs what it sends holds the
secret, so no app can deliver there), and runs a callable of a package a
`package` entry of the same request names. Approving turns it on, and gives
it a key only when it has none; the key is never shared. The key and the
switch land in one write, because the server turns on a webhook trigger a
package shipped only with a key ([triggers](functions.md#triggers)). The
token carries `webhook:<trigger>`, which delivers to that trigger with the
token itself. A request never asks for `webhook:` in `scope`.

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
| `not-shipped` | this server ships no catalog at all, or a package the catalog ships requires one it does not | none |
| `not-published` | neither the catalog nor an authority document this server can read offers the package, or one it requires, at the version needed (the owner's own packages and the samples they added included); `problem` carries the catalog's words, which name the package and the reason | none |
| `sample` | the package, or one it requires, is a sample, which the owner adds from All data | `/data` |
| `quarantined` | the repository's copy of the package, or of one it requires, is quarantined | its provider page |
| `declared-by-owner` | the owner declared the package, or one it requires, by hand (its package record is `installed` and no provider install stamped it, whether it declares kinds or only functions or traits), and installing or upgrading the published one would replace theirs; one they hold as a bundle at the version needed is in place and left alone | its provider page |
| `upgrade-needs-review` | the upgrade's preview refuses it, would lose data or discard edits, or could not run: the provider page asks the owner first | its provider page |
| `not-requested` | the account or the webhook belongs to a package no `package` entry names, or the trigger is neither in the repository nor shipped by a requested package | none |
| `account-deleted` | the account was deleted and the server has not collected the deletion yet; once it has, approving creates the account again | none |
| `reaches-past-scopes` | the package's code, or a requirement's, reads or writes a kind the scopes left checked do not cover ([what the code reaches](#what-the-code-reaches)); the problem names each uncovered reach and the scope that would cover it | none: the owner leaves that scope checked, or the app asks for it |
| `unsupported` | the account's kind is no account, its package declares no such kind, or a property is one it may not set; the trigger takes no webhook, its provider signs it, or it runs code of no package | none |

#### What the code reaches

Code an approval installs reaches no further than the scopes it approves
([0215](decisions/0215-code-an-approval-installs-reaches-no-further-than-the-scopes-it-approves.md)),
so an app asks, in `scope`, for every kind the packages it names reach:
each function's `permissions.reads.kinds` and `permissions.writes`, each
agent's, each record trigger's `source.record.kinds`, and each mapping's
`from` and `to`, for the packages it names and every package the catalog
installs first. A `write:` covers a read of the same kinds; a glob covers
the kinds it matches, except a kind whose purpose is `internal`, which
only its exact scope covers, and so is a kind the repository does not hold
and no package of the install declares; no scope writes a `core` or `llm`
kind, so code that does needs `full`. Substrate Sync, whose applehealth
package's functions write that package's kinds, the `health` catalogue's
sleep and workouts and the seeded observations, asks for
`write:providers.substrate.reamde.dev/applehealth/*`, the package's two
internal kinds by name, `write:substrate.reamde.dev/health/sleep`,
`write:substrate.reamde.dev/health/workout` and
`write:substrate.reamde.dev/observations/*`.

The page plans against the scopes left checked: its read takes them as
`?scope=` query parameters (`GET /api/v1/authorizations/{id}?scope=…`,
one per scope, in the spelling the page shows, `~` for the repository's
own; absent, every
requested scope is checked, and `scope=` alone is none), the approval
plans against the scopes it names, and the token's check against the
token's own. A package row carries `reach`, one entry per kind or glob and
verb with what reaches it (`by`), whether a checked scope covers it and
the scope that would, and each function its `reads`, `writes` and declared
`network` hosts.

The owner sees one row per entry and approves them with the scopes or not
at all, as themselves: a request that carries entries is approved with a
full-access token, through the console, `substratectl` or the API, and an
approval that writes as anything but the person (an app's or an assistant's
token) is refused with `403` before anything is planned, the request still
waiting for the owner. The approval runs the set up before the
code exists, so the code can take longer to arrive than for scopes alone:
the set up has up to 15 minutes, and the request then waits up to 10 more
for the approval page to come back and send the browser on, so an app that
waits on its redirect URI may wait up to 25 minutes.

### Keep a package of your own

An app whose records are of kinds no catalogue ships publishes them as a
package under its own authority
([0214](decisions/0214-an-apps-approval-installs-published-packages-and-declare-and-the-agent-detail-are-retired.md)):
the authority's `_substrate` record names an authority document
([name your publisher](#name-your-publisher)), the document lists the
package's versions, each with the URL of its closure (the multi-document
YAML a bundle directory ships: the `package` and `bundle` documents and
every declaration and data document of the package) and the closure's
SHA-256, and the app asks for the package with a `package` entry like any
other:

```
scope=write:substrate.reamde.dev/training/* read:substrate.reamde.dev/health/workout
authorization_details=[{"type":"package","package":"substrate.reamde.dev/health","version":1},
  {"type":"package","package":"substrate.reamde.dev/training","version":3}]
software_version=3.2.1
```

The approval page reads the document and names the publisher beside the
package ("Installs Training 3, published by Substrate
(substrate.reamde.dev)"), with the publisher's verification in its technical
details, and says the package stays after the app is revoked. Approving
installs the newest version the document lists at `version` or later, after
what the package requires, which resolves the same way: a package the
server's catalog ships comes from the catalog, whatever a document lists,
and another published package from its own document. The package lands
`published` under its publisher's authority, closed to every token as a
shipped provider's is, and the records of its kinds are the owner's as any
provider's are. A package listed only below `version`, or whose authority
publishes no document this server can read, blocks the row as
`not-published`, in the server's words. An agent the app needs ships in such
a package, bounded by its own declared reach and its own daily cap, and runs
until the owner pauses or removes the package; nothing an app sends in a
request declares one.

No scoped token applies vocabulary: `POST /api/v1/vocabulary/apply`, its
preview and the bundle doors answer `403` to every app's token. A
`declare:` scope and an `agent` detail, which an earlier server took, are
refused with an error page naming what replaced them, and a token that
still carries `declare:` no longer authenticates: sign in again.

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
  "scope": "read:~/tasks/* write:~/tasks/task",
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
   "url": "https://ada.example.com/webhooks/rnd-0vrkvsgbs6bnz2sjy4196kccck/applehealth-webhook"}
]
```

### Check that it is still set up

`POST /api/v1/authorizations/check` takes the app's own token and the request
it would send now, `{"scope": "…", "details": […], "softwareVersion": "…"}`:
`details` is the `authorization_details` array and `softwareVersion` the
build's version, read by the same rules and bounds as at the authorization
endpoint, and what would refuse the request there answers `400` here. Every
check first records the least version of each `package` entry and the
software version on the token that asked, and a package an authority
document publishes that the repository holds below the version asked for is
refreshed from its publisher, so an update the server applies on its own
lands before the answer. It answers what has moved since the app was
approved:

```json
{
  "scopes": {"missing": ["read:~/notes/*"]},
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

- `scopes.missing`: the requested scopes, with the repository's name folded
  into `~`, that the token does not cover. Coverage is by reach, not spelling: a `write` covers the `read` of
  the same pattern, and a wider pattern a narrower one (`read:*` covers
  `read:~/notes/*`), except that a kind that is `internal`, or that the
  repository does not hold yet, is covered by its exact scope alone;
- `setup`: per entry, in the request's order, the `action` approving would
  run now: `none` when it is in place, else `install`, `upgrade`, `create`
  or `enable`. A row that cannot run keeps its action and carries `blocked`,
  as on the approval page ([the reasons](#ask-for-set-up)). A package row
  carries the versions required, held and shipped (`held` absent when the
  repository holds none, `shipped` the version approving would install,
  absent when neither the catalog nor a document offers one), and a
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
the records route (`GET /api/v1/records` for lists, window and aggregate
reads and every mode of search, `POST /api/v1/records`), the record path
(`/api/v1/{authority}/{package}/{kind}/{id}`), the unit reads, a trait's
implementors, the change
feed ([follow changes](#follow-changes)), the set up check, blob storage
and blob reads under the blob rules below, under a `call:` scope the
function call ([run functions](#run-functions)), under an `agents:` scope
the chat and the thread routes, and under a `webhook:` scope the webhook
door and that trigger's parked list
([deliver to a webhook](#deliver-to-a-webhook)), held to its scopes
([0194](decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md),
[0219](decisions/0219-an-apps-token-reads-windows-aggregates-semantic-search-blobs-and-the-feeds-history-over-the-kinds-it-reads-and-calls-the-functions-its-call-scope-names.md)):

- a kind the token cannot read answers `404`, as an unknown kind does;
- a write to a kind it reads but cannot write answers `403` with
  `WWW-Authenticate: Bearer error="insufficient_scope"`;
- a list with no `filter.kinds` lists every kind it reads, and `implements`
  and `purposes` narrow those alone;
- a property condition on a reference, and a reference value in a write,
  must name a kind the token reads, or the request answers `403`. A write
  whose value names a kind the reference's pin does not admit, which the
  engine resolves through a record mapping, also needs the pinned kind
  readable, or for a `trait:` pin every kind implementing the trait;
- a search runs in any mode over the kinds it reads, the `semantic` and
  `hybrid` ones included, which embed the query with your provider and so
  spend your embedding budget (the approval page says so beside every read
  and write scope); it weighs every word alike and carries no scores, since
  a word's frequency is counted over the whole repository, and reports no
  pending count, which counts the whole repository's embed queue;
- a window read (`at` bounded on both ends) and an aggregate read run over
  the kinds it reads: one naming a kind it cannot read answers `404`, one
  naming none is cut to the readable kinds, never widened, and a metric
  read over none is cut further to the kinds the observed index holds.
  Every row, series, override and expanded referent is cut as a list's
  records are, and the sources an aggregate names are cut to the kinds it
  reads. Neither carries a changelog head;
- a list carries no changelog head, in its body or in its cursor;
- `GET /api/v1/substrate.reamde.dev/core/trait/{id}/implementors` lists, with
  each declaration, only the implementors the token reads, so an app over
  every kind with a trait learns them without a scope on
  `substrate.reamde.dev/core/kind`;
- `GET /api/v1/substrate.reamde.dev/core/kind/{kind}`, the kind percent-encoded,
  answers the declaration of a kind the token reads, and `404` for any other,
  so an app learns the label and the properties of a kind it was approved to
  read;
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
  first;
- `GET /api/v1/blobs/{digest}` reads a blob when a live record of a kind
  the token reads names it through a `blobref` property. A token reading no
  such kind answers `403`; one that does is answered, for a blob no record
  it reads names, exactly what a blob nobody stored answers (`404`), so the
  read says nothing about blobs it cannot reach. The answer carries the
  blob's name and media type only when the token reads the blob manifests,
  as a blob reference does, so without them the `Content-Type` is
  `application/octet-stream` and there is no `Content-Disposition`. An
  app's reads wait for two slots of their own, never the owner's.

Everything else answers `403`: the vocabulary, the catalog, a function
call from a token with no `call:` scope, the agent call API, the export and
the token endpoints. Writes are made by the app's own
[actor record](api.md#actors), on behalf of the person: its `publisher` is
its authority for an app whose publisher was verified, `local` for one whose
access went to a loopback address, and the host of its client id otherwise
([0209](decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)),
and no request names another. Two apps of one publisher, or on one host,
share it, and the write's `token` (which names the exact client id) tells
them apart. Labels and annotations an app writes go in its own namespace,
`app:`, its publisher and a slash (`app:example.org/…`), like an agent's or
a function's.

### Talk to agents

An `agents:<pattern>` scope reaches the agents whose ids match, and the
records their chats are made of:

- the agent rows (`substrate.reamde.dev/core/agent`), to read; an agent
  hidden from chat is listed but refuses a chat with `422`, as a chat
  naming a thread of another agent does;
- `POST /api/v1/substrate.reamde.dev/core/agent/{id}/chat`, the id one
  path segment (`%2F` for its slashes), streaming as it does for the owner.
  The message is written by the app's own actor, as above; the agent runs under
  its own permissions, so it reads and writes what it would in the
  owner's chat. A chat's `context` may name only records the app's own
  token reads; any other path is refused with the `404 not_found` a read
  of it would answer, before the agent runs;
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
`adjustedDiff` on an accept and a note under the app's own namespace (`app:`,
its publisher and `/note`: `app:example.org/note` for an app example.org
publishes). The accept applies the change as the owner's would,
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

`GET /api/v1/changes` is open to an app's token on every route the feed
has: the ndjson tail (`watch=1`, with its bookmark and generation,
heartbeats, terminal error frame, `from` and `generation` resume and `410`
as for the owner), the history pages (`before`, `first`), the run summaries
(`runs=1`) and the forward read from a cursor
([the change feed](changelog.md)). Each carries only the rows of records
the token reads, through its read and write scopes and the rows `agents:`
and `decide:` reach, and each row names alone: `seq`, `ts`, `op`, `kind`,
`recordId`, `actor`, `onBehalfOf` where set, and `affected` cut to records
the token reads. No `payload`, `hash`, `token` or trigger states, and a run
summary carries no `token` or `tokenName`. A history page stays full while more
lies below, so its cursor is right whatever the token cannot see between
its rows. `values=1` and `q` answer `403` on every route; a `kinds` entry
the token cannot read answers `404`.

An app's lists carry no head, so it opens the watch first, keeps the
bookmark, then reads its lists: every write after the bookmark arrives on
the watch, and the history pages reach back from there.

A stream outlives the check at its start, so an app's token is read again
while it runs: before every batch of rows and at every heartbeat on the
watch, and at least every two seconds on an agent's stream (`chat` and
`events`). Once the token is revoked or past its approved time, the watch
ends with a terminal error frame whose code is `auth`, and an agent's
stream with an `error` event, before anything more is written. The turn
itself runs on; the app signs in again to follow it.

### Write while offline

An app that works without a connection keeps its unsent writes in a queue on
the device and sends them in order once the server answers, each with the
`ifVersion` it was made against; the app kit's outbox is that queue. The
server never merges a second history into a repository's changelog, so a
write it refuses goes back to the person
([stability](stability.md#offline-use)).

### Run functions

A `call:<pattern>` scope names functions by their ids
(`<authority>/<package>/<name>`), with the kind pattern grammar: `*`, an
authority's, a package's or one function; `~` is the repository's
local namespace. A function of `substrate.reamde.dev/core` or
`substrate.reamde.dev/llm` is never an app's to call: naming one outright,
or either package's own glob, is refused, and a wider glob
(`substrate.reamde.dev/*`, `*`) skips them. Under it,
`POST /api/v1/substrate.reamde.dev/core/function/{name}/call` (the name
named in full, its slashes percent-encoded) runs a matching function as
the owner's call does, when the token's read and write scopes cover the
function's **reach**: every kind its `permissions.reads.kinds` and
`permissions.writes` name, and those of every function its
`permissions.call` may invoke and every agent its `permissions.agents` may
run, since a call runs whatever runs under it. A write scope covers a
read; no scope writes `core` or `llm`, so a function writing a kind of
either, or `*`, is never callable by an app. A function the token's `call:`
scopes do not match is answered `404`, as a kind it cannot read is; one it reaches but
whose reach its scopes do not cover answers `403` naming what is missing
(`read <kind>`, `write <kind>`). The call runs under the function's own
permissions, as a trigger's does: the app's actor is the caller, and the
function's effects land as the function's, on behalf of the person.

The approval page lists, under a `call:` scope, the functions it matches
today with each one's reach and whether the request's other scopes cover
it. Its sentence says the app runs the owner's code only as far as the
app already reaches.

### Deliver to a webhook

A `webhook:<trigger>` scope names one webhook trigger by its id. An app
never asks for it in `scope`, which refuses it: only an approved `webhook`
authorization detail puts it on a token. Under it:

- `POST /webhooks/{repository}/{trigger}` takes the token as
  `Authorization: Bearer` in place of the trigger's key. The door admits the
  delivery when the token is live, opens the repository the path names and
  holds `webhook:` for that trigger; any other bearer is read as a key, so a
  revoked, expired or full-access token, or one without the scope, answers
  the door's one `404`, as a wrong key does. The token is read again once
  the body has arrived, so one revoked or past its time meanwhile delivers
  nothing. The delivery acts for the token's person: a token whose person
  holds no role in the repository is read as a key, and the callable's run
  is held to the person's roles, read when it runs, so a person made a
  viewer delivers what the callable then may not write. A signature the trigger declares is still checked, and the header
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
| `call` | function ids | running the matching functions, where the token's reads and writes cover each one's reach ([run functions](#run-functions)) |
| `webhook` | one trigger id, never a pattern | delivering to that webhook and its parked list ([deliver to a webhook](#deliver-to-a-webhook)) |

| Pattern | Reaches |
| --- | --- |
| `<authority>/<package>/<name>` | one kind, one agent or one function |
| `<authority>/<package>/*` | every kind, agent or function of one package |
| `<authority>/*` | every kind, agent or function of one authority |
| `*` | every kind, agent or function the owner has |
| `implements(<trait>)` | `read` and `write` only: every kind whose declaration binds the trait |

A trait pattern
([0225](decisions/0225-an-apps-scope-may-name-a-trait-and-covers-what-star-covers-among-the-kinds-that-bind-it.md))
asks for a meaning rather than for names an app cannot know in advance:
`write:implements(substrate.reamde.dev/core/temporal)` reads and writes
every kind that sits on the timeline, whoever published it. It reaches what
`*` would, narrowed to the kinds that bind the trait when each request is
checked, so a kind installed later that binds it is covered from then on,
and the approval page lists the kinds that bind it today. The trait is
named in full, and `~` may stand for its authority. How a kind binds it,
`temporal(point: dueAt)` say, does not matter, only that it does.

An agent's id is its authority, package and name,
`~/notes/notekeeper` for the one the notes sample adds.
`~` in place of the authority is the repository's local namespace, where its
own packages and the samples it added live
([decision 0259](decisions/0259-a-repositorys-own-kinds-live-in-the-local-namespace.md)), and a scope keeps
it as written: `read:~/tasks/*` is `read:~/tasks/*` on the approval page and
in the token, the same for every repository the app is approved in, and
`agents:~/notes/*` reaches the agents added with the notes sample. An app
that spells the repository's full name (`read:ada.example.com/tasks/*`) is
asking for the same thing, and the token stores it as `~`. Some things no
scope reaches:

- the auth kinds (`substrate.reamde.dev/core/token`, `…/credential`,
  `…/secret`), read or write;
- an `internal` kind through a pattern ending in `*` or a trait pattern; an
  exact scope such as `read:substrate.reamde.dev/core/kind` may read one;
- a write to `substrate.reamde.dev/core` or `substrate.reamde.dev/llm`, which
  hold the functions, triggers, agents, providers and policies, beyond the
  few writes `agents:` and `decide:` carry, and a call of a function of
  either;
- a write to an account or a bundle input, the connections the owner manages;
- an agent, through `agents:`, that reads a kind the token's own `read` and
  `write` scopes do not cover: a glob the agent reads is covered by a glob at
  least as wide, an `internal` kind by its exact scope alone, so an agent is
  no channel to data the app could not read itself;
- what an agent reads or writes in a turn the app drives, or a function it
  calls, beyond the token's own scopes: the engine holds each read and each
  write of that work to the grant as it stands, and a revoked token
  refuses the rest of it
  ([0276](decisions/0276-one-check-holds-every-hand-that-writes-and-one-list-names-the-control-kinds.md)).

### Full access

`full` is a scope with no pattern: everything a password sign-in gets
([0204](decisions/0204-an-app-may-ask-for-full-access-and-its-owner-approves-it-whole.md)).
The Console app asks for it, so the owner signs in on the server's own page
in Safari rather than typing a password into the app, and the app holds the
session in the Keychain and hands it to the console it runs. A request names it
alone, with no `authorization_details`; anything beside it is refused. The
approval page leads with a warning, offers nothing to uncheck, and takes the
approval only from the person, never from an app's or an assistant's token.
Approving mints a token without scopes,
which is full access, keeping the app's client id and name: the owner sees
it under Apps, the app revokes it at the revocation endpoint or deletes its
own record (`token_id`) as a sign-in does, and its writes are the person's,
with its token beside them, never an app actor's. A server lists `full` in
`scopes_supported` when it can
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

A binary from before published packages reached the approval
([0214](decisions/0214-an-apps-approval-installs-published-packages-and-declare-and-the-agent-detail-are-retired.md))
takes a `declare:` scope and an `agent` detail again, and reads a token's
`agents` where an older token still carries them; a token minted since
carries neither, and its build facts are ignored, so nothing is refused.
This binary refuses a token that still carries `declare:` (fail closed):
the app signs in again, and its package arrives as a published one.

A binary from before app authorization reads an app's token as a
full-access one, since it does not know `scopes`. Before rolling back past
that change, revoke every app token under Settings > Apps.

## Publish a package

List the package in your authority document's `packages` array. Each entry
has `package` (the package name), optional `addonFor` (a package id), and
`versions`: positive, unique integer versions with `closure` (an HTTPS
URL), `sha256` (64 hexadecimal characters) and optional `dialect`, the
vocabulary dialect the closure needs. Keep every published version's bytes
unchanged.

```json
{"package": "garden",
 "versions": [{"version": 3, "closure": "https://example.com/garden-3.yaml",
               "sha256": "…", "dialect": 3},
              {"version": 4, "closure": "https://example.com/garden-4.yaml",
               "sha256": "…", "dialect": 4}]}
```

A server installs the newest version whose dialect it reads, the one it
publishes as `vocabulary.dialect` in [discovery](api.md#discovery): above,
a server at dialect 3 installs version 3 and passes over version 4. List a
version's dialect wherever its header states one; a version that lists none
is fetched first, and passed over when its header needs a newer dialect
([the dialect](vocabulary.md#vocabulary-evolution-and-the-dialect-contract)).

A closure is multi-document YAML: one package header, one bundle
declaration for that package, and its declarations and records. The header's
version matches the listed version. Every declaration belongs to the
package; records of shared machinery kinds use ids beneath the package id.
The binary wins for packages it already ships. A repository's own packages
live in `~` and are declared locally, never installed from a document. A webhook
trigger the closure ships is disabled and keyless unless it declares a
`signature`: an install of one enabled with neither is refused, and the
app's `webhook` detail is what keys it and turns it on
([decision 0248](decisions/0248-a-webhook-trigger-a-publisher-ships-is-enabled-only-with-a-key-or-a-signature.md)).

Check the candidate with `substratectl publish --check` before uploading
it: it prints the version's entry, with the dialect the header states. Upload the immutable closure first, update the authority document
second, and release the app build requesting that version last. A changed
digest at an existing version and a version below the newest published
version fail the check. Removing a declaration needs `--allow-narrowing`,
but for a kind: removing a kind any listed version published is refused,
with the flag or without it, and so is renaming a kind with `movedFrom`:
keep the old kind, mark it `deprecated: true`, and declare the new one
beside it
([decision 0269](decisions/0269-a-publisher-may-not-rename-a-kind-and-code-from-outside-publishers-is-experimental.md)).
Connected builds can still hold the server upgrade back until they check in
with a sufficient floor or their access ends.

Functions a published package ships are `experimental` at 1.0, whatever the
package's label: their review and their confinement may change in any
release ([stability](stability.md#a-packages-code)).

For packages whose agents use provider inputs, approval offers a provider
picker for each input. Each option shows the provider label and the model
each agent would run on, alongside its daily cap. A sole compatible row is
preselected. No compatible row blocks approval with `no-model`; add a
suitable AI provider and read the approval again. Setup binds the chosen
row after installing the package. The server validates each choice against
the current plan before writing.
## Optional add-ons and configuration

A published package listing may name `addonFor`, the provider package it
extends. Its approval row is optional: the owner checks the add-ons they
want. When that provider is absent, an approved row waits; the token keeps
its package, version floor and provider in `appAddons`. Installing the
provider tries the waiting add-ons again under each live token's approval.
An expired or revoked token cannot authorize that install. The provider's
install answer reports add-ons installed or still waiting; a blocked add-on
does not undo the provider install.

Packages can ship empty core `setting` and `secret` records under their own
id prefix. Approval lists those fields, including requirements' fields, and
asks the owner to fill them. Required empty fields refuse approval with
`needs-setting`. The approval POST's `settings` entries carry `id`,
`secret` and `value`; its `addons` names the optional packages selected.
Values are written as the owner after installation. Secrets use sealed
storage and never return to the app, enter the plan hash or live on the token.
Waiting add-ons collect no configuration values until their package is here.
