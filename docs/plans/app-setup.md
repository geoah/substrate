# An app's approval sets up what it needs

Status: implemented, 2026-11-01. Claude Fable 5.1 and GPT-6 Astra reviewed
it twice, the first draft and then the implementation, and each round
changed it (the review record is at the end). The binding choices are
recorded in
[0202](../decisions/0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md). It builds on [app authorization](app-authorization.md) and decisions
[0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)
and
[0195](../decisions/0195-an-app-talks-to-agents-decides-their-requests-and-tails-the-feed-it-reads.md),
and amends both where it says so.

## The problem

Three Apple apps speak to a substrate, and only one of them uses the app
authorization flow.

| App | Signs in with | Holds | Sets up |
| --- | --- | --- | --- |
| Substrate (the console in a native shell) | the console's own `POST /login`, in the web view | a full-access token in the page's storage | nothing: it is the owner's console |
| Substrate Chat | the app authorization flow, or the password on a server without it | `agents:* decide:* read:substrate.reamde.dev/llm/provider read:*` | nothing; a provider key is the owner's to write in the console |
| Substrate Sync, and its Send to Substrate extension | the owner's password, `POST /login` | a full-access token that never expires, shared with the extension through the Keychain | see below |

Substrate Sync's Set Up does, with that token, five things an app's token is
refused:

1. reads the catalog and installs, or upgrades,
   `providers.substrate.reamde.dev/apple` (3) and
   `providers.substrate.reamde.dev/applehealth` (5, which brings
   `substrate.reamde.dev/health` at 2 or later);
2. creates the phone's Health account,
   `providers.substrate.reamde.dev/applehealth/account/substrate-sync-<8>`;
3. arms `applehealth-webhook`: `enabled`, and a key it then reads back from
   the trigger on every Health run and sends to the webhook door;
4. reads that trigger's parked deliveries to tell a refused day from one
   still filing;
5. from the share extension, stores shared images as blobs.

So it asks for the password, and holds a token that reads every record,
applies vocabulary and mints tokens. Nothing tells any app, with its own
token, that it is out of date (a newer package, another scope, a server too
old, access ending), and the approval page lists every protocol fact at once:
Chat's request alone is four scope rows, one of them every kind in the
repository.

## The outcome

- An app's authorization request names its scopes **and** what it needs set
  up: catalog packages at a minimum version, the account it delivers under,
  and the webhook it delivers to. The owner sees both on one page, and
  approving runs the set up, as the owner, before the code is issued.
- A webhook delivery from an app is authenticated by the app's own token.
  Revoking the app, or its access ending, ends its deliveries; no key ever
  leaves the server.
- The page leads with what the app will be able to do and change, in plain
  words, with its choices still in reach. The technical facts are behind the
  console's one Technical details switch (decision 0131), which the page now
  reads and offers in place.
- An app asks, with its own token, whether it is still set up, and sends the
  owner back through the page for the difference, which the page heads by
  what it is: renewing, more access, a server update, or several.
- `SubstrateKit` carries sign-in, requests, compatibility (a minimum server),
  the "still set up" check and the screens for all of it.
- On a server that ships this, no Apple app but the console shell asks for
  the password. On an older server, Chat and Sync fall back to the password,
  as Chat does today, and Sync offers to move to app access once the server
  is updated.

## Protocol

### `authorization_details` (RFC 9396)

`GET /oauth/authorize` accepts `authorization_details`: a JSON array of at
most 8 objects and 2 KB, read strictly (an unknown member, a duplicate entry
or a type this server does not know refuses the request with the error page,
the way a malformed `scope` does). Three types:

```json
[
  {"type": "package", "package": "providers.substrate.reamde.dev/apple", "version": 3},
  {"type": "package", "package": "providers.substrate.reamde.dev/applehealth", "version": 5},
  {"type": "account",
   "kind": "providers.substrate.reamde.dev/applehealth/account",
   "id": "substrate-sync-1a2b3c4d",
   "properties": {"displayName": "Ada's iPhone", "timezone": "Europe/London"}},
  {"type": "webhook", "trigger": "applehealth-webhook"}
]
```

- **`package`**: a package of the catalog this binary ships, in its provider
  tier (a provider or a first-party catalogue, installed under the authority
  that publishes it), at `version` or later, `version` being 1 or more. A
  sample is not set up through this door yet: its requirements are imported
  by the console's own door, so a sample row is blocked with a link to All
  data. Nothing outside the binary's catalog is installable (0048's signed
  remote install is unbuilt).
- **`account`**: one record of a kind that implements
  `substrate.reamde.dev/core/accountconfig` and belongs to a package named
  by a `package` detail of the same request, under an id other than
  `default` (the record a package's inputs resolve first, so an app naming
  it would choose for every package that reads the kind; refused with the
  error page). Created once, with `ifVersion: 0`, so an existing row, live
  or deleted, is left alone: a deleted row whose deletion is not collected
  yet blocks the row (`account-deleted`), and once it is collected a later
  approval creates it again. `properties` is at most 1 KB, and each key is
  a property the kind declares that is not of type `secret`, not `managed`
  and restricted to no `writer` but `owner` (the set up writes as the
  owner). Every value is shown on the page in both views. Written as the
  door the owner approved through (below), since the owner approved it.
- **`webhook`**: a trigger with a webhook source its provider does not sign
  (no `source.webhook.signature`: the provider holds that secret, so no app
  can deliver there) and whose callable belongs to a package named by a
  `package` detail of the same request. Approving enables it, and sets a
  key (32 characters from the system's random source) only when it has
  none, so the public door is never open; an existing key is never read,
  changed or returned. The approved token carries `webhook:<trigger>`.

The authorization server metadata adds
`authorization_details_types_supported: ["package", "account", "webhook"]`.
An older server ignores parameters it does not know and would approve the
scopes alone, so an app sends details only to a server that lists them.

### `webhook:` is a fifth scope verb

`webhook:<trigger id>` names one trigger: no glob, no `~`. A request may not
ask for it in `scope`; only an approved `webhook` detail puts it on a token.
It reaches:

- the webhook door, `POST /webhooks/{authority}/{trigger}`, with the token as
  `Authorization: Bearer`: the door authenticates the token (a revoked or
  lapsed one is refused like a wrong key), checks that it opens this
  authority's repository and holds `webhook:` for this trigger, and then
  admits the delivery in place of the key. The token is read again once the
  body has arrived, so one revoked meanwhile delivers nothing. A trigger's
  signature check, where one is declared, still runs. The header never
  reaches the callable. A lookup is a database read anybody can ask for, so
  a bearer that looks like a token and is none is charged to its source's
  pace, and a source past it has its bearers read as keys, unlooked-up;
- `GET …/core/trigger/{id}/parked` for that trigger: every parked delivery of
  it, whoever sent it, which the page says. A delivery the door accepted
  that is still filing carries `pending: true`, so a client never reads one
  as refused from its text.

The trigger record itself stays unreadable through it. A delivery may name
any account the callable accepts: the package decides what a delivery may
do, and the page says the app may send to every account of that package.

### Webhook keys never reach an app

Separately from the above: an app's token holding an exact
`read:substrate.reamde.dev/core/trigger` reads trigger records today, keys
included. Every record an app's token receives, of the trigger kind, has
`source.webhook.key` removed, on every read path (get, list, search,
expanded references).

### Blob storage for a token that writes a blob property

`PUT /api/v1/blobs` (and `…/blobs/{digest}`) admits an app's token whose
grant writes at least one kind in the registry that declares a `blobref`
property. Its answer carries the digest and size of what it sent, never the
stored name or media type of a blob that already held those bytes. Reading a
blob stays the owner's. This is what the share extension needs. An app's
uploads wait for two slots of their own, never the owner's eight, and the
token is read again once a slot is taken and once the body has arrived, so a
token revoked meanwhile stores nothing.

### The page's view

`GET /api/v1/authorizations/{request}` (after the repository has claimed the
request, as today) answers, beside today's fields:

- **`setup`**: one row per detail, in the request's order:

  | Field | |
  | --- | --- |
  | `type` | `package`, `account` or `webhook` |
  | `action` | what approving would run: `install`, `upgrade`, `create`, `enable`, or `none` (already in place) |
  | `blocked` | present when the row cannot run: its `reason`; `problem`, the server's one sentence for it in the owner's words; and `fix`, a console path where the owner can clear it, absent when only a newer server or the app can |
  | `progress` | while a set up runs, and after one stopped: `waiting`, `running`, `done` or `failed` |
  | `package` | for a package: id, description, tier, required, held (the catalog release it holds: `shippedVersion` for a published package, else its version; absent when it holds none), shipped (absent when the catalog ships none), the requirement chain in the order it lands (each step with its own action, block, versions, kinds, functions and triggers), and the package's own kinds it adds (with labels and purpose), functions (each with the kinds it may write) and triggers (each enabled or not). No name: a client names a package from its id |
  | `account` | kind, label, id, properties |
  | `webhook` | trigger, the package its callable belongs to, enabled, keyed |

  A package is `none` when it holds the floor or more, `install` when absent
  and `upgrade` when held below the floor. One planner holds the whole
  install closure to the same checks, the requested package and every
  requirement the catalog would install first, and a step that cannot run
  blocks the row that needs it. The reasons a package row is blocked:
  `server-update` when the catalog ships less ("This server ships version
  3; the app needs version 4. The server needs an update first."),
  `not-shipped` when the catalog does not ship it, `sample` for a sample,
  `quarantined`, `declared-by-owner` when the owner declared it by hand below
  the floor (installing would take it from them; at or above the floor it is
  `none` and left alone, as decision 0100 leaves a hand-declared
  requirement), and `upgrade-needs-review` when the upgrade's preview has
  blockers, is lossy, discards edits or fails (the provider page asks for
  that confirmation; the row links there). An account row is `create`, or
  `none` while its row is live, and blocked `not-requested` when no
  `package` detail names its package, `unsupported` when its kind is no
  account or a property is one it may not set, and `account-deleted`. A
  webhook row is `enable`, or `none` when the trigger is enabled and keyed,
  and blocked `not-requested` when its callable's package is not named or
  the trigger is neither here nor shipped by a requested package, and
  `unsupported` when it takes no webhook, its provider signs it, or its
  callable belongs to no package. A problem sentence never names a package:
  the row's title does, from its id, so the two views say one thing.
- **`planHash`**: a digest of the setup rows, without their progress. An
  approval names it, and the server refuses with `409` when its own plan
  differs: what runs is what the page showed.
- **`previous`**: the newest token the repository holds for this
  `client_id`: when it was approved, when it ends, its scopes. Never present
  for a local app, whose client id anything on the owner's computer may
  send. Each scope row carries `allowed` when those scopes already cover it,
  by reach rather than spelling (a write covers a read, a wider pattern a
  narrower one, an `internal` kind only by its exact scope), which
  `internal/scope` decides for the check too; a setup row says "already in
  place" by its action.
- **`state`**: `pending`, `setting-up` or `approved`. A pending request with
  nothing approved behind it is planned at every read. Once an approval
  reserved it, the page reads the rows that approval was checked against,
  each with its `progress`, while the set up runs and after it stopped part
  way; a run that found the plan moved drops them, so the next read plans
  afresh. After a failed run, **`failure`**: the message, and `row`, the
  index it stopped at, absent when it stopped before or after every row
  (waiting for the repository's turn, issuing the code). Once approved,
  **`next`**, the redirect carrying the code, answered once.
- Scope rows' `kinds` also list the kinds a requested package would add,
  marked `added`.

### Approving

`POST /api/v1/authorizations/{request}` takes `scopes`, `lifetimeDays` and,
when the request has setup to run, `planHash`.

- Under the store's lock the approval reserves the request: it must be
  pending, claimed by this repository and shown, and the scopes and lifetime
  are checked; it moves to `setting-up` (a second approval or a denial
  answers `409`) and its lapse moves fifteen minutes on. The plan is
  computed once, under the reservation, and every refusal below hands the
  request back, pending.
- What runs is what the page showed: the plan, which must match `planHash`,
  or, after a set up stopped part way, the rows that set up was checked
  against, of which the plan now may run less. A blocked row refuses with
  `422`.
- No row needs action: the code is minted there, `200` with the redirect,
  as before set up existed.
- Otherwise `planHash` is required, and setting up is the owner's act: the
  approval's actor must be one of the three human doors, the console,
  `substratectl` or the API, and any other is refused with `403`. The
  answer is `202` with the state and the rows, each with its first progress.
- The set up runs in the background, detached from the HTTP request (a
  closed tab does not stop it), one set up per repository at a time. Holding
  the repository's turn, it plans again: a row may have come to need nothing
  (another set up landed it), but a row whose action changed, a package that
  gained a requirement or changed what it does to one, or a webhook whose
  callable now belongs to another package stops it before it writes
  anything, and the request is pending again with its rows dropped. Then it
  runs the packages, then the accounts, then the webhooks, whatever order
  the rows are in: each package through the catalog's own install door
  (requirements first, each committing on its own, as from the provider
  page), each account created with `ifVersion: 0`, each webhook enabled and
  keyed at the version it read, after its trigger is checked again (still a
  webhook its provider does not sign, still running the approved package's
  code). Every webhook's binding is checked once more before the code is
  minted.
- On success the code is minted and the request holds the redirect until
  the page's next read, which is answered from what the request holds,
  nothing planned again, and deletes it: a second read is the page's
  "expired or already answered". The code lives 60 seconds from that read,
  and never more than ten minutes after the set up finished. On failure the
  request is pending again with `failure` and the rows as they ran (one left
  running reads `failed`), and Try again approves those rows again and runs
  what still needs action. What landed stays, as an install from the
  provider page does; the page says so before approving.
- A restart forgets the request (0194); what landed stays and the app starts
  over, finding those rows in place.

### The token response

`authorization_details` (RFC 9396 §7) lists what was set up: each package
with the version now held, each account's kind and id, each webhook's trigger
and the URL to deliver to (`<issuer>/webhooks/<authority>/<trigger>`).

### An app asks whether it is still set up

`POST /api/v1/authorizations/check` admits an app's token (and the owner's).
The body is the request the app would send now, `{"scope", "details"}`:
camelCase, as everything under `/api/v1` is, while the OAuth parameter keeps
`authorization_details`; the details are read by the same strict decoder,
under the same bounds, as at `/oauth/authorize`. The answer:

- `scopes.missing`: the requested scopes, resolved, the token does not
  cover, by the coverage `allowed` uses;
- `setup`: per detail, `action` and, where the token may see the row, its
  `blocked`, a package's required, held and shipped versions and a webhook's
  package, nothing more. A row the token cannot see is `unknown` and
  carries only what names it: a package unless the token's read or write
  scopes name one of its kinds, an account unless the token reads its kind,
  a webhook unless the token holds its `webhook:`. Such a package is planned
  no further than its action, with no closure facts and no upgrade preview.
  An app treats `unknown` as nothing to ask about;
- `expiresAt`, the token's.

An app reads it at launch and on return to the foreground (at most every
few minutes). A token's last answer is answered again for the same body for
30 seconds, and a check is never refused for its pace. When anything needs
action the app offers the owner a review that sends its whole request again.

## The approval page

The page is wrapped in the console's preferences provider and reads its
Technical details switch; a **Show technical details** control at the card's
foot flips that same switch, so the choice follows the person (0131, 0132).
Switching keeps every choice made.

### Everyday (the switch off)

```
  An app wants to connect to ada.example.com
  sync.substrate.example.com
  It calls itself Substrate Sync

  It will be able to                                 (each with a checkbox)
    ☑ See, add, change and delete your Apple contacts, events, reminders
      and visits, and 1 more kind, including kinds added later
    ☑ See your Apple Health accounts
    ☑ See your Apple Health deliveries

  It will set up on your server
    ↓ Install Apple             Version 3
                                Contacts, calendars, reminders and places
                                from your iPhone, as Apple stores them.
    ↓ Install Apple Health      Version 5
                                Keeps a copy of the Health data your iPhone
                                sends and turns it into health records.
      Runs code on your server, automatically, each time data arrives.
      Also installs Health.
    + Create the Apple Health account “Ada's iPhone”
      Display name  Ada's iPhone
      Timezone      Europe/London
    ⚡ Turn on the Apple Health webhook
      The app will then be able to send data to your repository through the
      Apple Health webhook, for any Apple Health account, and see every
      delivery to it that could not be filed, whoever sent it. To refuse
      this, deny the request.

  ⚠ Your automations run on its changes …   (kept as a warning)
  Setting up continues even if you close this page. What it sets up stays
  if it stops part way or you later revoke the app.

  Access lasts  [7 days | 30 days | 90 days | Until you revoke it]
  It ends on 1 December. …
                                    [ Deny ]  [ Approve and set up ]
  Show technical details ○
```

- The heading is the host, the one fact the client id proves, with the
  claimed name as a claim beneath it; a local app reads "An app on this
  device". One warning at most for where access goes.
- Each scope is a sentence from its verb and the labels of the kinds it
  reaches: `write` is "see, add, change and delete"; a glob adds "including
  kinds added later"; `agents:` says a chat has the agents do anything they
  may do, and that the app reads every chat with them, yours included,
  answers their questions and may rename or stop one; `decide:` says it may
  edit a suggested change before applying it, as you. Each keeps its
  checkbox.
- A set up row is titled from the package's id (the catalog carries no other
  name), with its versions ("Version 5", "From version 2 to 4") and its
  catalog description; a package that ships a function says it runs code,
  automatically when it has an enabled trigger, and each time data arrives
  when its webhook is one the request turns on; each requirement it installs
  first is named with the code it runs. An account shows every value it
  writes. The webhook's access sits under its row as its consequence, with
  no checkbox: refusing it is denying the request.
- A re-authorization says "An app you approved asks ada.example.com again"
  and is headed by what changed: **Renew access** (nothing new), **Allow
  more access** (new scopes), **Update server setup** (setup rows to do), or
  both of the last two. The new rows come first; what the access the app
  still holds covers folds under "Already allowed". Denying keeps that
  access working, and says so.
- Blocked set up keeps Deny and disables Approve. Each blocked row shows the
  server's sentence, and a link to the console page that fixes it where
  there is one; one line below the buttons says what the owner can do, by
  the rows' reasons (fix it in the console, have the server updated, wait,
  or deny), beside **Check again**.
- While it runs, each row shows its progress. A failure's message sits under
  its row, and one line below says "Setting up stopped at <row>. What landed
  stays; Try again runs the rest."

### Technical (the switch on)

Everything the page showed before it had an everyday view (the client id,
what the app's document claims, where the access goes, the repository, the
access the app holds now, the actor its changes are recorded under), plus
each scope as written with every kind by full reference, and, per set up
row: the package id, the version needed and the one shipped, what it brings
first with each step's own facts, and its Kinds, Functions and Triggers,
each a disclosure with its count (a function's writes one per line); the
account's kind and id; the webhook's trigger id, whether it is on and has a
key, and that the key is never shared; the raw `authorization_details`,
copyable. On a wide screen the card is wider in this view.

## SubstrateKit

| Folder | What |
| --- | --- |
| `Server/` | `ServerAddress` (the one normaliser), `Discovery` (all of `server.json`), `AuthorizationServerMetadata`, `ServerVersion` (`v0.113.0`, `0.113.0-4-g…`; `dev` is not comparable), `ServerRequirement` (a minimum release, the scope verbs and detail types an app needs) and its verdict |
| `API/` | `SubstrateClient` over an injected transport: bearer, timeouts by tier, `Retry-After` on 429 and 503 for idempotent requests only, the error envelope, `explain(_:app:server:)` (an `insufficient_scope` is "needs your approval", never "sign in again"), one path-segment encoder, records, lists, blob upload, webhook delivery, parked deliveries (one still filing told by `pending`, or by its text from a server that sends no mark) |
| `Auth/` | `AppManifest` (built per run: client id, name, scopes, details, the server requirement, which adds every verb its scopes use and every type its details use), `AuthorizationDetail`, `PKCE` (a failed random source throws), `OAuthClient` (metadata, which must be the chosen server's own, issuer and endpoints on its origin, before the browser opens; the authorize URL, which throws rather than drop details; the loopback sign-in, RFC 8252, with no claimed callback; the exchange, never retried, `iss` required when advertised), `LoopbackReceiver`, `WebAuthenticator`, `Session` (an app session or a legacy password one, versioned, with the scopes it asked for, and its `revoke`, the one revocation, which returns only once the server says so), `SessionStore` (Keychain, an access group for extensions, available after first unlock for background work, and the sessions kept for revocation until the server confirms), `Grants` |
| `Status/` | `AppStatusCheck`: discovery, the requirement, `/authorizations/check` (asked only of a server whose metadata lists detail types, and never about a scope the owner left out of the last approval) and the expiry, combined into a set of issues, any number at once: server too old, needs approval (with the changes in words, a blocked row with the server's sentence and fix), access ends soon, access ended, unreachable, legacy access to move |
| `UI/` | `ConnectView`, `ServerTooOldView`, `AppStatusBanner`, `ReviewChangesSheet` (the console's sentences, held to them by the fixture both read, `web/console/src/lib/app-scopes.fixture.json`), `AccessEndingBanner`, `AccessEndedView`, `AccountSection`, `ChangeRow`, `StatusBadge`, `ProblemBox` |

`ServerRequirement` is the server minimum: a release to name and the
capabilities to require. Capabilities decide: a `dev` build passes when it has
them, and a release below the minimum is too old even if it has them.

## The apps

- **Substrate Sync** keeps two kinds of session. A new sign-in on a server
  that lists the detail types goes through the flow with:

  ```
  scope: write:providers.substrate.reamde.dev/apple/*
         read:providers.substrate.reamde.dev/applehealth/account
         read:providers.substrate.reamde.dev/applehealth/export
  authorization_details: package apple ≥3, package applehealth ≥5,
         account substrate-sync-<install>, webhook applehealth-webhook
  ```

  Under it, Health is delivered with the token, the trigger is never read,
  the account is never recreated (a missing one is a review), parked
  deliveries are read under `webhook:`, Sign Out revokes with
  `/oauth/revoke` under its own client id and keeps the token until the
  server confirms, and the Set Up screen becomes the kit's status. On an older
  server it signs in with the password and Set Up works as today. A password
  session on a server that now lists the detail types offers "Move to app
  access": the flow with the same install id (so the account is in place),
  the new session saved, then the old token revoked with its own
  `DELETE /tokens/{id}`.
- **Send to Substrate** reads the session from the shared Keychain group,
  either kind, and uploads under the token's blob rule.
- **Substrate Chat** moves its OAuth client, receiver, sheet, requests and
  errors to the kit, keeps the password fallback for a server without app
  authorization, and shows the kit's status: a server whose metadata lacks
  `agents:` and `decide:`, or a release below the one that ships this, is
  too old, rather than an error page.
- **Substrate** (the shell) uses the kit's address normaliser and discovery.

## What this amends

- 0194: an approval may now run set up the owner sees, before the code
  exists; the flow's own state stays in memory. The scope grammar gains
  `webhook:`. The scoped surface gains the webhook door and parked list
  under `webhook:`, blob storage under the blob rule, and
  `/authorizations/check`. An app still needs nothing but a URL and a JSON
  file: the details are request parameters, so a local app can send them.
- 0195's rule that no scope writes `core` or `llm` stands: the set up's writes
  are the owner's, at approval, limited to what the page showed.
- A binary from before this change refuses a token carrying `webhook:` (its
  scope parser knows four verbs), which fails closed: there is nothing to
  revoke, and the token works again once the binary rolls forward. A binary
  from before 0194 reads every app token as full access; revoke app tokens
  before such a rollback, as today. What a set up installed stays after any
  rollback.

## Out of scope

- Samples through this door, and packages outside the binary's catalog.
- Binding an app's deliveries to its own account (the account header is the
  package's convention, not the door's).
- A provider key or any secret the owner types.
- A webhook its provider signs: the provider holds that secret, so no app
  can deliver there.
- A set up approved by anything but the owner's own doors.
- Refresh tokens, remembered approvals, an app's choice of lifetime.
- A lossy upgrade from the approval page.

## Review record

Round 1 was reviewed by Claude Fable 5.1 and GPT-6 Astra, each against the
code. What changed:

- **Webhook keys** (both, blocking): returning the trigger's key let a
  revoked app keep delivering, shared one secret between apps, and survived
  a replayed code. Now the app's token is the delivery credential, under
  `webhook:`, and keys never leave the server; a `webhook` detail must name a
  trigger whose callable a requested package ships (Fable: any keyed trigger
  could be named). Astra found that an exact `read:…/core/trigger` scope
  already reads every key; app reads now drop it.
- **`record` became `account`** (both): a general record created by the
  owner's hand could configure bundle inputs, carry secrets in a URL and fire
  automations. Now only an account kind of a requested package, no secret or
  writer-restricted property, create-only with `ifVersion: 0` (a put would
  resurrect a tombstone), written under the console actor (Fable: an unscoped
  `app:` write is a third hand nothing was built for).
- **Lifecycle** (both): the set up runs detached from the HTTP request, as a
  state the request moves through under the store's lock after the
  repository binding is checked, with progress the page polls, a plan hash
  the approval names, and one set up per repository at a time.
- **Packages** (both): sample requirements are not auto-imported, so samples
  are out of this door; "held" is the catalog release a package holds
  (`shippedVersion`), and a hand-declared package at the floor is left alone
  (0100).
- **Check** (Astra): `unknown` is never a reason to ask; package rows carry
  versions only; `previous` compares scopes only.
- **Blobs** (both): the rule is a writable `blobref` kind, and the answer
  never reveals another blob's name or media type.
- **Page** (both): the heading is the verified host, not the claimed name;
  checkboxes stay in the everyday view; the sentences keep every power
  (delete, later kinds, reading every chat, editing before applying); code
  is its own consent fact; the automation warning stays a warning;
  re-authorization headings say what changed; blocked set up keeps Deny and a
  link; the page says what stays after a stop.
- **Apps** (both): Sync keeps working on an older server and moves over when
  the server can; the old token is revoked with its own id, not through
  `/oauth/revoke` under the new client id (which answers 200 and revokes
  nothing); the kit's status is a set of issues, not one state.

Round 2 reviewed the implementation: Claude Fable 5.1 and GPT-6 Astra, each
over its architecture, its security, its UX and its docs, tests and
developer experience. What changed:

- **One planner, out of the HTTP layer** (Fable; Astra, blocking:
  requirements skipped the hand-declared check): `internal/appsetup` plans
  and runs over `substrate.Dataset` and the catalog's typed closure facts;
  the callable-package rule and the trigger helpers live once, in
  `internal/vocabulary`; and every package of the install closure meets the
  ownership, quarantine, catalog and upgrade-review checks, a blocked step
  blocking its row.
- **A block is the server's** (both): a row keeps the action it would run
  (`arm` became `enable`, and `blocked` is no action) and carries `blocked`:
  a reason a client branches on, one sentence both views show, which never
  names a package, and a console path where the owner can fix it. The check
  keeps it for the rows a token may see. The catalog's name left the wire:
  a client names a package from its id.
- **An approved request is answered once** (Fable, blocking; Astra): the
  first read after a set up finished hands the redirect over from what the
  request holds, nothing planned again, and deletes the request; the code
  lives 60 seconds from that read, never more than ten minutes from the set
  up's end (Fable: it could live eleven minutes).
- **The page reads what was approved** (Fable): while a set up runs, and
  after one stopped, the page reads the rows the approval was checked
  against, each with its own progress, not a plan recomputed as rows land;
  an approval plans once, under its reservation, and answers `200` there
  when nothing needs action.
- **Holes closed**: a webhook whose callable moved to another package stops
  a waiting set up even where it needs no action, and every binding is
  checked again before the code (Astra); a signed webhook is refused, since
  WHOOP's, enabled, keyless and signed by its provider, would have been
  keyed and offered (Fable); a deleted account is blocked with a recovery
  sentence until its deletion is collected (Astra); the account id
  `default` is refused (Fable); only a human door, the console,
  `substratectl` or the API, approves a set up, and the account is written
  as that door, not always the console's (Fable); `previous` is never set
  for a local app (Fable).
- **The check** (both): the authorization endpoint's own strict reader and
  bound, from `details`; coverage by reach, not spelling, shared with the
  page's `allowed`; `unknown` rows carry only what names them; answers kept
  for 30 seconds, never a `429` (Fable: a status probe was paced by the
  park limiter).
- **The doors** (both): blob storage reads the token again after its slot
  and its body, an app's uploads hold slots of their own, the webhook door
  charges a token-shaped bearer that fails to its source's pace, and a
  parked delivery still filing carries `pending`.
- **The apps** (both): the metadata must be the chosen server's own before
  the browser opens (Astra, blocking); one revocation, which waits for the
  server's word and is retried at launch; a manifest's required verbs and
  detail types follow from its own scopes and details; the check is asked
  only of a server that lists detail types, and never about a scope the
  owner left out; the console and the kit read one fixture of scope
  sentences; Chat's client and errors are the kit's.
- **Tests** (both): the planner's and the runner's branches in
  `internal/appsetup`; the engine's create-only put over a tombstone and an
  app's blob; and one `internal/e2e` case, APP-01, that approves Sync's
  request, exchanges the code, delivers with the token and revokes it.
- **Docs** (both): this plan, 0202 and [apps](../apps.md) say who may
  approve a set up, the order it runs in, that `writer: owner` is allowed
  (Astra), the detail grammar and the block reasons, the timing, and what a
  rollback does to a `webhook:` token (Fable).
