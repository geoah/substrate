# Domains: where a repository lives, and what a publisher's apps may install

Status: implemented in pull requests #1163 to #1167, not merged; see
[the status page](authorities/status.md).
Each binding choice gets its own decision record before it lands; the
records it amends or supersedes are listed under
[decisions this needs](#decisions-this-needs).

## The problem

A substrate is about to be handed to more people, and three of its names
are taken on trust.

- **A repository's name is unproven.** Registration accepts `geoah.me` on
  the user's word
  ([0046](../decisions/0046-a-repository-owns-one-authority-chosen-at-registration.md)
  lists this as a known gap), so on a shared server anyone can take a name
  before its owner does. An app also cannot find a repository's server from
  the name: the person has to know and type the server's address.
- **An app is named by wherever its files are hosted.** The approval page,
  the actor `app:<host>` and "Managed by" show the host of the app's
  `client_id` URL
  ([0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)).
  `.mise/apple.sh` uploads Workouts' `client.json` beside the release build,
  so the app reads as published by the release bucket's host. Its document
  lists only a loopback redirect, so any program on the device can send
  Workouts' `client_id`, be approved as Substrate Workouts and inherit its
  package (0205 keys ownership on the client id alone).
- **An app that brings its own vocabulary has one narrow door.** Under
  [0203](../decisions/0203-an-app-keeps-its-own-package-under-a-declare-scope.md)
  and [0205](../decisions/0205-an-app-owns-the-package-it-declares.md) an app
  may declare kinds only, under each repository's own authority, so
  Workouts' set is `geoah.me/training/set` in one repository and
  `ada.example.com/training/set` in the next. A function, trigger, agent or
  mapping cannot come through an app at all, and the approval's `package`
  detail installs only what the binary's catalog ships
  ([0202](../decisions/0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md)).

The third point is the one that limits what can be built. Take an assistant
app: several packages of its own kinds, functions that call out to the
internet, agents, scheduled and record triggers, mappings from the mail,
calendar and chat providers the owner may or may not have connected, and a
web app to use it all. Today it can only be installed by hand with a
full-access token. It must be possible to set all of it up through the
approval page.

## Requirements

| # | Requirement |
| --- | --- |
| R1 | An app finds a repository's server from the repository's name, through one small DNS record. |
| R2 | A name outside the server's own host is proven before a repository takes it; an existing repository can prove its name later. |
| R3 | An app's connect screen takes either a server address or a repository name, fills the sign-in page, and cannot be mixed up between servers. |
| R4 | An app, and the packages it brings, are named by a domain whose DNS its publisher controls, never by a host it can merely serve a file from. |
| R5 | Where the platform can bind a domain to a signed binary, the server relies on it; where it cannot, the page marks the app unverified. |
| R6 | One document describes everything a domain publishes: its apps and its packages, whatever the packages carry. |
| R7 | An app's packages live under its publisher's authority, with the same names in every repository. The owner cannot edit them (the owner's decision in 0205) but can pause, uninstall or take them over. |
| R8 | Everything an app like the assistant needs is set up through the approval page: packages with functions, agents and triggers; the LLM provider its agents use; settings and secrets the owner types; optional parts for providers the owner may connect later; webhooks. |
| R9 | App builds of different versions work against one repository at once, and an upgrade never breaks a build still connected. |
| R10 | Code an app installs reaches no further than the app's own approval, and the page shows what it reaches. Holding egress to declared hosts is [#1144](https://github.com/geoah/substrate/issues/1144). |
| R11 | It works with static hosting; DNS holds pointers, never content. |
| R12 | A standard OAuth or MCP client that knows none of this keeps working as it does today. |
| R13 | A local build, and a package version not yet published, can be tested against a development server. |

## Prior art

| System | What it does | What this takes from it |
| --- | --- | --- |
| atproto handles | `_atproto.<handle>` TXT `did=…`, or `https://<handle>/.well-known/atproto-did`; DNS wins on conflict; the DID document must name the handle back | the TXT label, the HTTPS fallback, the bidirectional check |
| atproto lexicons | `_lexicon.<authority>` TXT names the account whose repository stores the schemas as records; no walking up or down the DNS tree | DNS as a pointer to where the vocabulary lives, one record per authority |
| OAuth client ID metadata documents | the `client_id` is an HTTPS URL serving the client's metadata; MCP adopted it and tells servers to warn on `localhost`-only redirects, which it cannot protect | already in use (0194); stays the client registration |
| ACME (RFC 8555) | proves control of a name before a certificate is issued: DNS-01 puts a token in a TXT record at `_acme-challenge.<name>`, HTTP-01 serves it under `/.well-known/acme-challenge/` | DNS as the proof that a publisher controls its authority |
| RFC 8252 | a claimed `https` redirect is how a native public client is identified; a loopback or custom scheme can be impersonated | the rule for R5 |
| Apple associated domains | the domain's `apple-app-site-association` file names the apps (team id and bundle id) it vouches for; `ASWebAuthenticationSession` delivers an `https` callback only to an app associated with the callback's host | how an Apple build proves it is the app |
| RFC 9207 | the authorization response carries `iss`, which the client checks against the server it meant | R3's mix-up check |
| RFC 7591 | `software_id` and `software_version` name a client's software and its build | how a build says which package version it was built for |
| OAuth attestation-based client authentication (draft) | the publisher's backend vouches for a client instance with a signed attestation, backed by App Attest or Play Integrity | a later, stronger R5 for publishers who run a backend; not proposed now |

Two facts bound what DNS can carry. A TXT string is 255 bytes; longer values
are split into strings, and a response over 1232 bytes (the EDNS size agreed
at DNS Flag Day 2020) falls back to TCP, which some networks block. And few
domains are signed: about 4% of `.com` in Verisign's 2023 figure, so a TXT
answer is only as trustworthy as the resolver that fetched it. A browser
cannot read TXT records without a DNS-over-HTTPS service.

## What a publisher sets up

One DNS record and three files carry the publisher's side. Two of the files
exist today.

| Item | Exists | One per | Where | Says |
| --- | --- | --- | --- | --- |
| TXT record | new ([decision 3](#decision-3-how-an-app-proves-who-publishes-it)) | authority | `_substrate.<authority>` in the domain's DNS | where the authority document is; it is the proof that the publisher controls the name |
| client document | yes, since [0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md) | app | the URL that is the app's `client_id`, such as `https://substrate.reamde.dev/apps/workouts/client.json` | the app's name and the redirect URIs its codes may be sent to |
| authority document | new ([decision 6](#decision-6-the-one-document)) | authority | the URL the TXT record names, conventionally `https://<authority>/.well-known/substrate/authority.json` | the apps that speak for the authority (by `client_id`) and the packages it publishes |
| association file | Apple's | domain | `https://<domain>/.well-known/apple-app-site-association` | which signed apps (team id and bundle id) receive that domain's links |

A `client_id` is what an app sends on every authorization request to say
which app it is. Here it is always a URL: the address of its client
document, or a loopback URL for a local app, which has no document.

## Decision 1: how a repository is found

| Option | For | Against |
| --- | --- | --- |
| A. TXT at `_substrate.<name>` naming the server | one record, no web host needed, the atproto pattern | browsers need DNS over HTTPS; trust is the resolver's |
| B. `https://<name>/.well-known/substrate/repository.json` | TLS-backed, readable from a browser | needs a web server on the name, which a personal domain often lacks |
| C. SRV `_substrate._tcp.<name>` (Matrix, XMPP) | standard for "where is the service" | carries a host and port, not a URL; poorly supported in DNS hosting UIs |
| D. WebFinger (RFC 7033) | standard account discovery | needs a web server, and keys on an account, not a domain |

**Chosen: A, with B as the fallback** (atproto's order: DNS first, and DNS
wins when both answer differently).

```text
_substrate.geoah.me.  TXT  "v=substrate1 server=https://substrate.rxtx.dev"
```

The connect screen resolves what is typed:

1. If it serves `/.well-known/substrate/server.json`, it is a server; the
   sign-in page asks for the repository.
2. Otherwise it is a repository name: read `_substrate.<name>`, else
   `https://<name>/.well-known/substrate/repository.json`
   (`{"server": "https://…"}`, held to the record's rule for `server=`),
   and probe the server it names. Only a lookup that answers that the name
   has no `v=substrate1` record (NXDOMAIN, or no matching record) lets
   discovery go on to the web host. A failed lookup (a timeout, SERVFAIL),
   a malformed record, or a record that names no server stops discovery
   with an error and a way to try again, because DNS outranks the web host.
3. Neither: strip the first label and probe the parent host as a server,
   which covers a default name such as `alice.substrate.rxtx.dev` without a
   record. Whoever controls the parent controls its subdomains, so this is
   not a weaker proof. The parent is never a top-level domain.
4. Send `login_hint=<repository>` to `/oauth/authorize` so the sign-in page
   fills the name, check the response's `iss` (RFC 9207, which the server
   already sends), and check that the token response's `repository` is the
   name resolved. A token for another repository is revoked, and kept only
   in the app's pending-revocation store until the server confirms it.

Apple apps read TXT records with `DNSServiceQueryRecord`. It reports a
SERVFAIL as `NoSuchRecord`, the same as a name without a record, so a
negative answer is asked again through libresolv before discovery goes on.
The console needs
none of this: the server serves it. Moving a repository to another server is
then a change of one record.

## Decision 2: proving a repository's name

| Option | For | Against |
| --- | --- | --- |
| A. No proof (today) | nothing to set up | anyone takes any name on a shared server |
| B. The record must point at this server at registration | one record | between adding the record and registering, someone else on the same server can register first |
| C. B, plus a one-time code from the register page in the record (ACME DNS-01) | binds the record to one registration | one more field while registering |
| D. Register freely, verify later, show a badge | no friction | the squatting stays; a badge is a warning, not a refusal |

**Chosen: D for now.** Registration stays as it is. A repository proves its
name afterwards with **Verify domain** on its settings page, which shows the
record to add with a one-time `proof`:

```text
_substrate.geoah.me.  TXT  "v=substrate1 server=https://substrate.rxtx.dev proof=k3j9x2"
```

The server checks the record, stamps the result on the repository record,
and the `proof` field may be removed afterwards. With `SUBSTRATE_PUBLIC_URL`
configured, `server=` must equal that origin exactly. Without one, the
server fetches the `server=` origin's `server.json` and compares an instance
id it minted at first boot. That comparison is a consistency check that
`server=` reaches this server, not proof of which server it is: a proxy can
relay it. Passing still needs the proof in DNS, which only the name's
controller can publish. A name exactly one label under the public URL's host
is verified without a record. The request's `Host` header decides nothing,
because a caller chooses it, so a server with no public URL verifies every
name through the record. Until a name is verified, the console and the
approval page mark it unverified. Nothing is renamed, so the authority stays permanent
([0052](../decisions/0052-the-authority-is-the-repository-id.md)).

Discovery does not wait for verification: an app that starts from a name
reaches the server the name's own DNS points at, so a repository squatting
the name on another server is never found that way. It is only reached by
someone who types that server's address. C is the step up when a server
opens registration to people its operator does not know; it uses the same
record.

## Decision 3: how an app proves who publishes it

The server starts from the one thing the app sends, its `client_id`, and has
to decide which domain the approval page names as the app's publisher, and
so under which authority the app may install packages. That answer must come
from whoever controls the domain. Serving a file from a host does not prove
it: anyone can publish a file at a path on `github.com`,
`gist.githubusercontent.com` or `pastebin.com`, and none of them owns those
names.

| Option | For | Against |
| --- | --- | --- |
| A. The `client_id`'s host (today) | nothing new | a file at any path makes an app the host's: a client document on `pastebin.com` names `pastebin.com` |
| B. A file at the authority's `/.well-known/` path lists the `client_id` (ACME's HTTP-01, atproto's fallback) | no DNS change | proves control of a web server, not of the name; a host that lets its users publish under `/.well-known/` gives the name away, and the authority must run a web server |
| C. A TXT record at `_substrate.<authority>` names the authority document, the document lists the `client_id`, and the client document names the authority | only whoever controls the domain's DNS can add the record (ACME's DNS-01); every file after it may be hosted anywhere, because the domain chose its URL | one DNS change per publisher; the answer is as trustworthy as the resolver |
| D. As C, with the client ids in the TXT record itself: `"v=substrate1 app=https://cdn.example.net/workouts/client.json app=…"` | no authority document | each client id is a whole URL of 40 to 80 bytes, so a 255-byte string holds three or four; every new app is a DNS change |
| E. A signed software statement (RFC 7591) or a client attestation (draft) | strongest; per instance with App Attest | the publisher runs a backend and manages keys; nothing else here needs one |

**Chosen: C.**

```text
_substrate.substrate.reamde.dev.  TXT  "v=substrate1 doc=https://substrate.reamde.dev/.well-known/substrate/authority.json"
```

Workouts, step by step:

1. Workouts opens `/oauth/authorize` with `client_id`
   `https://substrate.reamde.dev/apps/workouts/client.json` and
   `redirect_uri` `https://substrate.reamde.dev/apps/workouts/callback`.
2. The server fetches the client document. It carries
   `"substrate_authority": "substrate.reamde.dev"`, which is a claim: anyone
   can write that line into a document of their own. It only tells the
   server which name to ask.
3. The server resolves `_substrate.substrate.reamde.dev`. Without a record
   carrying `doc=` there is no publisher, and the app is unverified.
4. It fetches the document the record names and looks for the `client_id`
   under `apps`. Only whoever controls the DNS chose that URL, so a listing
   there is the domain's word, wherever the file is hosted.
5. The redirect's host is the authority or a name under it, and is a
   claimed link ([decision 4](#decision-4-how-an-app-proves-it-is-the-binary)),
   so the code reaches only the app the association file names.
6. The page reads "Substrate Workouts, by substrate.reamde.dev"; the app
   writes as `app:substrate.reamde.dev` and may install the packages the same
   document lists.

What this refuses:

- A client document on `pastebin.com` claiming `substrate.reamde.dev`: not
  listed in that document, so unverified.
- One on `pastebin.com` claiming `pastebin.com`, or naming nothing: there is
  no `_substrate.pastebin.com` record, so unverified. An unverified app is
  never shown as published by a host; the page shows its claimed name
  marked unverified, and where the access goes.
- `alice.github.io`: GitHub controls the `github.io` zone, so Alice cannot
  add `_substrate.alice.github.io`. Publishing needs a domain whose DNS the
  publisher controls.
- A lapsed domain: once the record is gone, the next check (every approval
  and token exchange, and the daily reread of documents for its packages)
  finds no publisher. A token keeps the actor it was minted with until the
  app is approved again.
  Its apps become unverified and its packages stop upgrading; what is
  installed stays. A new owner who adds a record is a new publisher to every
  server, and telling the two apart is
  [#1144](https://github.com/geoah/substrate/issues/1144).

Each authority stands alone: `_substrate.reamde.dev` says nothing about
`substrate.reamde.dev`, and the server never walks up or down the DNS tree,
as atproto's lexicon lookup does not. More than one `v=substrate1` record at
one label leaves the name unverified. The server resolves through its
configured resolver, so a signed zone is checked only where that resolver
validates; an operator who wants more points the server at a validating or
DNS-over-HTTPS resolver.

The `client_id`'s host no longer names anything: a verified app's client
document may live on a CDN. E stays open for a publisher that wants
per-install proof later.

## Decision 4: how an app proves it is the binary

| Option | For | Against |
| --- | --- | --- |
| A. Loopback redirect (Workouts, Sync, Chat today) | works everywhere, no setup | any local program can send the same `client_id` (RFC 8252, MCP's warning) |
| B. Claimed `https` redirect: an associated domain through `ASWebAuthenticationSession`, App Links on Android | the OS hands the code only to the signed app the domain names | a domain association file per publisher |
| C. App Attest or Play Integrity through a client attestation | per installed instance | the publisher's backend, as decision 3's E |

**Chosen: B for every native first-party app, A kept as the unverified
path.** An app is **verified** when decision 3 finds it listed and its
redirect's host is the authority or a name under it: a claimed `https` link
for a native app, the web origin for a web app (the browser enforces it). A loopback redirect
is never verified, whatever the `client_id`: the page reads "An app on this
device that says it is Substrate Workouts".

An unverified app may still ask for anything a verified one may, `full`
included. The page marks it unverified, and for `full` says that nothing
confirms which program receives a token equal to the password.

### What Apple requires

Checked against Apple's documentation on 2026-11-02:

- **The callback API.** `ASWebAuthenticationSession.Callback.https(host:path:)`
  is available from iOS 17.4 and macOS 14.4, and "the host must be a member
  of a domain associated with the app". Every Apple app here targets iOS and
  macOS 26 or later, so no older fallback is needed.
- **Which service.** Apple's callback page does not name the associated
  domains service; third-party guides report `webcredentials`. The apps list
  the host under both `webcredentials:` and `applinks:`, and the association
  file names each app id under both.
- **macOS outside the App Store.** Apple's macOS capability table marks
  Associated Domains as supported for Developer ID and the paid program, and
  not for a free Apple Developer account. A build that carries the
  entitlement needs a provisioning profile that includes the capability; one
  forum report has a Developer ID build without it refusing to launch.
  Sparkle delivery changes nothing: the signing does.
- **Development builds.** Since iOS 14 and macOS 11 devices fetch the
  association file from Apple's CDN, never from the domain. The CDN requests
  it within 24 hours of a change and devices recheck about weekly. A
  development-signed build (the `debugging` export `.mise/apple.sh` uses by
  default) reads it from the CDN like any other build. `?mode=developer` is
  only for a domain the internet cannot reach: it needs a development
  profile, an opt-in on each device, and removing before App Store
  submission. Nothing here needs it.
- **Hosting.** The association file is served at
  `https://<host>/.well-known/apple-app-site-association`, with a valid
  certificate and no redirects, and each subdomain serves its own. A
  path-style bucket address cannot serve a host's root, which is one more
  reason the first-party files move to a host of their own (below).
- **macOS browsers.** When the default browser does not support callback
  matching, the system opens Safari for the session.

## Decision 5: whose authority an app's kinds live under

| Option | For | Against |
| --- | --- | --- |
| A. The repository's own, `~/training` (0203, 0205) | no publisher proof needed | a different name in every repository; ownership needs app-specific locks and a takeover operation |
| B. The publisher's, `substrate.reamde.dev/training`, installed as `published` | one name everywhere, so agents, imports and other apps agree; the published tier already refuses the owner's edits | needs decision 3's proof and a way to fetch the package |

**Chosen: B.** `authorizeDeclarationWrite` already refuses any write to a
`published` package, so 0205's rule ("I should not be able to edit kinds
that are not mine") is the tier's ordinary rule rather than an app-specific
lock. The owner keeps the bundle's **Pause** and **Resume**, may uninstall
(the app's next approval installs it again, from its publisher), and may
**Take over…**, which stops upgrades and makes the package editable under
its own authority name, as 0205's takeover does today.

The same rule closes squatting from the other side: **a token creates a new
package only under the repository's own authority.** Any other authority
comes from that authority's document or the binary's catalog. Today any
token may declare `google.com/anything`. Samples are unaffected, since an
import rehomes them onto `~`. A repository never installs a published
package under its own authority, because that repository is the publisher.
A package the repository already holds is not new, including one a
quarantine took out of the live registry, so the owner can still repair it.
Until decision 8's local document covers packages,
`SUBSTRATE_INSECURE_ALLOW_FOREIGN_PACKAGES` lifts the rule for the suites
that apply shipped provider files directly.

With B in place, `declare:` has no remaining use, and development uses
[decision 8](#decision-8-development)'s local document. 0203 and 0205 are
superseded. An app whose publisher has no domain of its own cannot bring
packages; it works with the kinds the repository already holds, such as the
samples and catalogues.

## Decision 6: the one document

| Option | For | Against |
| --- | --- | --- |
| A. Keep three shapes: the client document, provider bundles, an authority record | each stays as it is | three places to say one thing; nothing ties them together |
| B. One authority document listing apps and package versions, closures hosted beside it, client documents kept per app | one place; static hosting; standard OAuth untouched | the client document stays a separate file |
| C. Put everything in the client document | one file per app | a package two apps share would be published twice; OAuth's document is not the place for vocabulary |
| D. Serve packages from the publisher's own substrate repository, as atproto serves lexicons from a PDS | publishing is an apply; the engine validates before anyone installs | every install depends on the publisher's server being up, and a public read surface on every server |

**Chosen: B**, in a format a substrate could later generate and serve, so D
becomes a hosting choice rather than a protocol.

Location: the URL the authority's TXT record names (decision 3). The record
is required, because it is the proof; the URL may be the domain's own
`/.well-known/substrate/authority.json` or a file host, because the domain
chose it:

```text
_substrate.example.org.  TXT  "v=substrate1 doc=https://cdn.example.net/example-org/authority.json"
```

A record may carry both `server=` (the domain is a repository) and `doc=`
(it publishes). An optional `sha256=` pins the document's digest in DNS for
a publisher that wants DNS, not the file host, to be the whole proof, at the
cost of a DNS change per release.

```json
{
  "authority": "substrate.reamde.dev",
  "name": "Substrate",
  "description": "The apps made with substrate.",
  "packages": [
    {
      "package": "training",
      "versions": [
        { "version": 2, "closure": "https://substrate.reamde.dev/packages/training-2.yaml", "sha256": "…" },
        { "version": 1, "closure": "https://substrate.reamde.dev/packages/training-1.yaml", "sha256": "…" }
      ]
    }
  ],
  "apps": [
    {
      "client_id": "https://substrate.reamde.dev/apps/workouts/client.json",
      "name": "Substrate Workouts",
      "packages": ["training"],
      "apple": { "appIDs": ["ABCDE12345.dev.reamde.substrate.workouts"] }
    },
    {
      "client_id": "https://substrate.reamde.dev/apps/sync/client.json",
      "name": "Substrate Sync",
      "packages": [],
      "apple": { "appIDs": ["ABCDE12345.dev.reamde.substrate.sync"] }
    }
  ]
}
```

- A **closure** is the multi-document YAML a bundle ships today, under
  `<authority>/<package>`, carrying anything a bundle may: kinds, traits,
  property types, functions, triggers, agents, mappings, settings, secrets
  and `oauth2`. A third-party provider is a closure with `oauth2`.
- Each **version** is immutable: fetched from `closure`, refused unless its
  bytes hash to `sha256`. How long an old version stays listed is the
  publisher's choice (see decision 7).
- **`addonFor`**, on a package, names a provider it only makes sense beside.
  The assistant's mappings, functions and triggers that read Google ship as
  one package with `"addonFor": "providers.substrate.reamde.dev/google"`, and
  likewise per provider.
- **`apps`** lists the clients that speak for the authority (decision 3),
  the packages each may ask for, and the native app ids the approval page
  names beside the OS's own check. A package from the binary's catalog (Sync's `apple`
  and `applehealth`) is asked for as today and is not listed.

The server fetches with the guards `internal/authorize` applies to client
documents (HTTPS, public addresses, no redirects, size caps, a short cache).
A document is capped at 64 KB and a closure at 4 MB; a 14-package assistant
measured about 100 KB of bundle files.

## First-party apps publish under `substrate.reamde.dev`

Sync, Chat, Workouts and the Console app publish under
`substrate.reamde.dev`, the authority core and the first-party catalogues
already use. Workouts' kinds become `substrate.reamde.dev/training/…`.

- **The binary wins for what it ships.** The binary embeds its own snapshot
  of `substrate.reamde.dev`'s packages, as today. The fetched document may
  add packages the binary does not ship (`training`) and may list apps; a
  package the binary ships is never installed or upgraded from the fetched
  document, so whoever can change the hosted file cannot replace `core`,
  `llm` or a catalogue. `providers.substrate.reamde.dev` and
  `samples.substrate.reamde.dev` stay binary-only.
- **The record.** `_substrate.substrate.reamde.dev` goes in the
  `reamde.dev` zone, naming the authority document.
- **The domain is served.** `substrate.reamde.dev` serves a static site:
  `/.well-known/substrate/authority.json`,
  `/.well-known/apple-app-site-association`, each app's
  `/apps/<app>/client.json` and its callback path
  `/apps/<app>/callback`, and the package closures. The release task
  publishes them; the release builds themselves may stay in the bucket.
- **What it changes in this repo's guide.** `AGENTS.md` calls
  `substrate.reamde.dev` a placeholder. Kind identity is unchanged (the
  authority stays a DNS name; nothing moves to URLs), but the name becomes a
  served domain whose document other servers read, so that sentence is
  rewritten when phase 1 lands.
- **Attribution.** The first-party apps write as
  `app:substrate.reamde.dev`. The `app:` prefix keeps it apart from the
  substrate's own hands.

## Decision 7: versions across app builds

An app build is written against one version of a package, and a phone and a
laptop may run different builds against one repository.

| Option | For | Against |
| --- | --- | --- |
| A. Exact pin: the repository holds the version the app asks for | simple | two builds on two devices cannot both be satisfied |
| B. A floor, additive changes and tolerant readers | every build at or above the floor works | a removal needs a deprecation period |
| C. Semantic versions, a major per package name | familiar | substrate packages carry one integer; a major is already a new package name |
| D. B, plus the server tracks which version each connected build needs | an upgrade that would break a connected build is held, naming the device | the build reports its version |

**Chosen: D.**

- **A build asks for a floor.** It carries the version of each package it
  was built against and asks for that version or later, which the `package`
  detail already expresses. Approval installs the newest version the
  document lists, never one under the floor, and the server never
  downgrades. A floor the document does not list yet blocks the row with
  `not-published`.
- **Changes are additive.** New kinds, optional properties, enum values and
  states upgrade cleanly; a narrowing is refused while live records hold the
  old shape, as for every package today. An older build therefore keeps
  writing validly.
- **An older build ignores what it does not know.** A put merges, so a
  property it was not built with survives its writes; it skips unknown
  properties and enum values on read. The app kits hold this rule.
- **The server knows what each build needs.** The token records the
  package versions its build asked for, and the set up check (0202), which
  the apps call at launch, updates them along with `software_version`. An
  upgrade that removes something (a deprecated property's drop, a rename
  through `renamedFrom`) is held while a live token's build needs the old
  shape, and the console names the app and when it last checked in.
- **The publisher's numbers are kept.** A published package's versions are
  the document's, not the engine's `stored+1`. A closure with the stored
  version and a different digest is refused, the rule `kinds:check` holds
  in this tree. `substratectl publish --check` runs the same comparison in
  the publisher's CI against the last published closure.
- **Release order is document first, app second.**
- **Upgrades come from two places.** An app's set up check reports a floor
  above the installed version; the server rereads the documents of the
  authorities it holds, at boot and daily. A kinds-only additive upgrade
  applies by itself. One that adds or changes functions, triggers, agents,
  mappings or `oauth2`, or reaches further
  ([decision 9](#decision-9-what-installed-code-may-reach)), waits for the
  owner, as `upgrade-needs-review` does today.
- **A break is a new package name** (`training2`), and the app copies its
  records across. A published package does not use `movedFrom`, which stays
  limited to the shipped boot upgrade.
- **Old versions stay listed as long as the publisher likes.** The server
  installs the newest listed version no connected build would break, and
  never needs an older one once installed. Listing the versions in between
  lets a repository held back by an old build step forward to the newest
  safe one; a document that lists only the latest leaves it where it is
  until that build updates.

## Decision 8: development

A local build is an unverified app (decision 4) and may still ask for a
published package: the package comes from its publisher's authority document, never
from the app, so the build gets exactly what a release gets. For a package
version that is not published yet, a development server reads a local
document in place of the DNS lookup and the fetch:
`SUBSTRATE_INSECURE_AUTHORITY_DOCS` maps an authority to a file. The server
has no development mode to tie it to, so it follows the other
`SUBSTRATE_INSECURE_` settings: an operator's opt-in, warned about at boot,
and disclosed wherever it acts. The approval page says a file on this server
vouched for the publisher, not DNS, and the token records it. A local
document for `substrate.reamde.dev` still cannot replace what the binary
ships.

## Decision 9: what installed code may reach

Today approving an install consents to the catalog's code and bounds
nothing: a function that reads every kind and declares network can send any
record anywhere, whatever scopes the app was given (0202 says so). An app
that brings its own code makes this the app's reach in practice.

| Option | For | Against |
| --- | --- | --- |
| A. Unbounded, disclosed (today) | nothing to check | the scopes stop meaning what the page says |
| B. Installed code reaches no further than the app's approved scopes | the page's sentences stay true; one rule to check at install | a package that needs wide reads asks for them as scopes, which the owner sees |
| C. A separate permission grant per function | finest grain | a page no owner reads |

**Chosen: B.** At planning, every function's `permissions.reads.kinds` and
`writes`, every agent's grants, every trigger's selector and every mapping's
source and target must fall inside the scopes being approved. A row outside
them blocks with a new reason, `reaches-past-scopes`, naming the scope that
would cover it. Unchecking a scope on the page re-plans. Network access is
shown per function ("may send data to the internet"); holding it to the
declared hosts, upgrades that change code, a server setting that refuses
outside code and a domain that changes hands are
[#1144](https://github.com/geoah/substrate/issues/1144).

## What a full app needs from the approval page

| Need | Today | Proposed |
| --- | --- | --- |
| Install several packages with code, some requiring each other | catalog only | decision 6, one batch per authority, since admission resolves declarations within a batch |
| Parts that read a provider the owner may not have | applied by hand later | `addonFor` packages, approved as optional: installed now if the provider is there, otherwise installed when the owner installs that provider, under the approval already given |
| Agents that use the owner's model | an agent names an `llm/provider` row id, which the publisher cannot know | an agent may name its provider through a bundle input of kind `llm/provider`, resolved by the input rules (bound, `default`, sole record); the page asks the owner to pick a row and shows that agents spend on it, under its spend cap ([0149](../decisions/0149-a-spend-cap-holds-an-agent-trigger-a-third-state-beside-running-and-parked.md)) |
| An API key or a setting | refused (0202's reopen trigger "a secret the owner types") | the page collects the bundle's empty `setting` and `secret` records ([0076](../decisions/0076-a-bundle-ships-its-settings-as-core-setting-and-secret-records.md)) and writes them as the owner; the app never sees them |
| Connected provider accounts | the console | stays the console: the page lists the providers the add-ons wait for, with a link to each; connecting one later lands its add-on |
| Webhooks | `webhook` detail (0202) | unchanged |
| A web app showing a schedule, trends, files and the agents' history | `403`: window and aggregate reads, semantic search, blob reads, the function call API and the change feed's history are owner-only for an app (0194) | each opened under the scopes it reads: a window or aggregate read over readable kinds, a semantic search over readable kinds, a blob read where the token reads a kind whose `blobref` names it, a `call:<pattern>` verb for functions, the feed's history cut to readable kinds |

## Attribution

| Option | For | Against |
| --- | --- | --- |
| A. `app:<client_id host>` (today) | nothing changes | names the file host, and a loopback impersonator gets the real name |
| B. Follow the proof: `app:<authority>` when verified, `app:<host>` for an unverified web app (the host its access is sent to, never shown as its publisher), `app:local` for a loopback redirect | history says only what was proven | Sync, Chat and Workouts share `app:substrate.reamde.dev` |
| C. `app:<authority>:<app>` | tells a publisher's apps apart | widens the actor grammar ([0025](../decisions/0025-an-actor-carries-the-full-authority.md)); the token already names the exact client |

**Chosen: B.** The changelog's principal (the token) names the exact client
and build, as 0194 already relies on. `app:local` covers a loopback client
id and a loopback redirect alike, and an https client id on the host `local`
is refused so no web app can produce it. Labels an app writes go under its
actor's namespace. A token may also keep writing the keys it wrote under
`app:<client id host>`, the namespace an earlier binary gave it, so the
change of actor breaks no client.

## Phases

1. **Before more people test.** The repository record and **Verify
   domain**; `login_hint` and `iss`; the publisher record and an
   authority document listing apps alone (decision 3), claimed redirects
   and attribution by proof; `substrate.reamde.dev`'s record and site, with
   Sync, Chat, Workouts and the Console app moved onto it; the rule that a token creates packages only
   under its own authority.
2. **Published packages through approval.** Packages in the authority
   document (decision 6), fetching closures, the
   published install, versions and the build tracking of decision 7, the
   development override; Workouts moves to `substrate.reamde.dev/training`;
   `declare:` is retired. Decision 0207's `agent` detail requires a
   `declare:~/<package>` scope, so retiring `declare:` needs a way for an
   app's agent to ship in a published package.
3. **Full apps.** Code bounded by scopes, optional add-ons, agent providers
   through inputs, settings and secrets on the page, the wider scoped
   surface, and #1144's egress and review work.

All three phases are implemented in pull requests #1163 to #1167. Their
merge order, review record, where they run and what is left are on
[the status page](authorities/status.md).

## Decisions this needs

- The `_substrate` record, repository discovery and **Verify domain**,
  registration unchanged (amends 0046; 0052 stands). Drafted as 0208.
- Verified apps proven through the authority's TXT record and document,
  attribution by proof, and `full`
  for an unverified app marked as such (amends 0194, 0195's `app:<host>`
  and 0204). Drafted as 0209.
- First-party apps under `substrate.reamde.dev`, the binary winning for what
  it ships.
- A token creates packages only under its repository's authority (amends
  `authorizeNewPackage`, which today admits any new authority but the
  publisher's and keeps a hand-apply door open for shipped closures).
  Drafted as 0210.
- Published packages installed from a document through approval, and the
  retirement of `declare:` (amends 0202, supersedes 0203 and 0205).
- Published package versions and build tracking; no `movedFrom` for a
  published package.
- Installed code bounded by the approved scopes (amends 0202).
- The wider scoped surface: window, aggregate, semantic, blob reads, `call:`
  and the feed's history (amends 0194 and 0195).
- Agent providers through bundle inputs.

## Existing installs

Production holds Workouts' `~/training`, locked to its current client id.
The new build copies nothing. At its first sync it uploads the phone's own
history into `substrate.reamde.dev/training`: the respell of the old
build's files drops what that build knew about `~/training` (its record
versions, edit chains and queued session writes), so the published package
reads as never synced from this phone and the exchange uploads a session
the package lacks instead of reading its absence as a delete. Records the
old package holds that the phone does not have (another phone's sessions, a
routine the coach wrote) stay there until the owner copies them into the
published package, as was done for `geoah.me` with a script, or deletes
them; a phone whose history was copied by hand takes the copies as the
server has them and sends nothing again. The old package is the owner's to
take over and remove. Anything else installed by hand under another
authority is expected to break and is reinstalled through its app's
approval.
