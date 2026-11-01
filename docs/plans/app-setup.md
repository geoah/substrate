# An app's approval sets up what it needs

Status: proposal, 2026-11-01, round 2: the first draft reviewed by Claude
Fable 5.1 and GPT-6 Astra, and changed by both (the review record is at the
end). It builds on [app authorization](app-authorization.md) and decisions
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
  that publishes it), at `version` or later. A sample is not set up through
  this door yet: its requirements are imported by the console's own door,
  so a sample row is blocked with "add it from All data". Nothing outside the
  binary's catalog is installable (0048's signed remote install is unbuilt).
- **`account`**: one record of a kind that implements
  `substrate.reamde.dev/core/accountconfig` and belongs to a package named
  by a `package` detail of the same request. Created once, with
  `ifVersion: 0`, so an existing row, live or deleted, is left alone; a row
  whose deletion has been collected can be created again by a later
  approval, which says "Create" when it does. `properties` is at most 1 KB,
  and each key is a property the kind declares that is not of type `secret`
  and carries no `writer` restriction. Every value is shown on the page in
  both views. Written under the console actor: the owner's door, since the
  owner approved it.
- **`webhook`**: a trigger with a webhook source whose callable belongs to a
  package named by a `package` detail of the same request. Approving enables
  it, and sets a key (32 characters from the system's random source) only
  when it has none, so the public door is never open; an existing key is
  never read, changed or returned. The approved token carries
  `webhook:<trigger>`.

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
  admits the delivery in place of the key. A trigger's signature check, where
  one is declared, still runs. The header never reaches the callable;
- `GET …/core/trigger/{id}/parked` for that trigger: every parked delivery of
  it, whoever sent it, which the page says.

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
blob stays the owner's. This is what the share extension needs.

### The page's view

`GET /api/v1/authorizations/{request}` (after the repository has claimed the
request, as today) answers, beside today's fields:

- **`setup`**: one row per detail, in the request's order:

  | Field | |
  | --- | --- |
  | `type` | `package`, `account` or `webhook` |
  | `action` | `install`, `upgrade`, `create`, `arm`, `none` (already in place), or `blocked` |
  | `problem` | why it is blocked, in the owner's words, and `fix` where to go |
  | `package` | for a package: id, name, description, tier, required, held (the catalog release it holds: `shippedVersion` for a published package, else its version), shipped, the requirement chain (each with its own action), the kinds it adds (with labels and purpose), its functions (each with the kinds it may write) and its triggers (each enabled or not) |
  | `account` | kind, label, id, properties |
  | `webhook` | trigger, the package, enabled, keyed |

  A package is `none` when it holds the floor or more; `install` when absent
  and the catalog ships the floor; `upgrade` when held below the floor and
  the catalog ships it; `blocked` when the catalog ships less ("this server
  ships version 3, the app needs 4: update the server"), when it is not in
  the catalog or is a sample, when the owner declared it by hand below the
  floor (installing would take it from them; at or above the floor it is
  `none` and left alone, as decision 0100 leaves a hand-declared
  requirement), when it is quarantined, or when the upgrade's preview has
  blockers, is lossy or discards edits (the provider page asks for that
  confirmation; the row links there).
- **`planHash`**: a digest of the setup rows. An approval names it, and the
  server computes the plan again and refuses with `409` when it differs:
  what runs is what the page showed.
- **`previous`**: the newest token the repository holds for this
  `client_id`: when it was approved, when it ends, its scopes. Only scopes
  are compared with it; a setup row says "already in place" by its action.
- **`state`**: `pending`, `setting-up` or `approved`; while setting up,
  **`progress`** (each row `waiting`, `running`, `done` or `failed`); after a
  failed run, **`failure`** (the row and the error); once approved after a
  set up, **`next`**, the redirect carrying the code.
- Scope rows' `kinds` also list the kinds a requested package would add,
  marked `added`.

### Approving

`POST /api/v1/authorizations/{request}` takes `scopes`, `lifetimeDays` and,
when the request has setup, `planHash`.

- No row needs action: the approval is what it is today, `200` with the
  redirect.
- Otherwise, under the store's lock: the request must be pending, claimed by
  this repository and shown; the scopes and lifetime are checked; it moves
  to `setting-up` (a second approval or a denial answers `409`) and its lapse
  moves to fifteen minutes on. The plan is computed again, compared with
  `planHash`, and a blocked row refuses. The answer is `202` with the state.
- The set up runs in the background, detached from the HTTP request (a
  closed tab does not roll it back), one set up per repository at a time:
  each package through the catalog's own install door (requirements first,
  each committing on its own, as from the provider page), each account,
  each webhook (re-checking the trigger's callable before writing).
- On success the code is minted and the request holds the redirect until the
  page reads it. On failure the request is pending again with `failure`, and
  Try again runs the rows still needing action. What landed stays, as an
  install from the provider page does; the page says so before approving.
- A restart forgets the request (0194); what landed stays and the app starts
  over, finding those rows in place.

### The token response

`authorization_details` (RFC 9396 §7) lists what was set up: each package
with the version now held, each account's kind and id, each webhook's trigger
and the URL to deliver to (`<issuer>/webhooks/<authority>/<trigger>`).

### An app asks whether it is still set up

`POST /api/v1/authorizations/check` admits an app's token (and the owner's).
The body is the request the app would send now, `{"scope", "authorization_details"}`.
The answer:

- `scopes.missing`: requested scopes the token does not hold (resolved);
- `setup`: per detail, `action` and, for a package, required, held and
  shipped versions, nothing more. A row the token cannot see is `unknown`: a
  package unless the token's scopes name one of its kinds, an account unless
  the token reads its kind, a webhook unless the token holds its `webhook:`.
  An app treats `unknown` as nothing to ask about;
- `expiresAt`, the token's.

An app reads it at launch and on return to the foreground (at most every
few minutes), and when anything needs action it offers the owner a review
that sends its whole request again.

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

  It will be able to                                   (each with a switch)
    ☑ See, add, change and delete your Apple contacts, calendars,
      reminders, places and shares, including kinds Apple adds later
    ☑ See your Apple Health accounts and the Health deliveries it sent
  It will also, because it sets these up
    • Send Health data to your repository, for any Apple Health account,
      and see which deliveries could not be filed

  It will set up on your server
    ↓ Install Apple             Contacts, calendars, reminders and places
                                from your iPhone, as Apple stores them.
    ↓ Install Apple Health      Keeps a copy of the Health data your iPhone
                                sends and turns it into health records.
      Runs code on your server, automatically, each time data arrives.
      Also installs Health.
    + Create the Apple Health account “Ada's iPhone” (time zone Europe/London)
    ⚡ Turn on the Apple Health webhook

  ⚠ Your automations run on its changes …   (kept as a warning)
  If you stop part way, what was installed stays.

  Access lasts [30 days ▾]  ends 1 December
                                    [ Deny ]  [ Approve and set up ]
  Show technical details ○
```

- The heading is the host, the one fact the client id proves, with the
  claimed name as a claim beneath it; a local app reads "An app on this
  computer". One warning at most for where access goes.
- Each scope is a sentence from its verb and the labels of the kinds it
  reaches: `write` is "see, add, change and delete"; a glob adds "including
  kinds added later"; `agents:` says it reads every chat with those agents,
  yours included, and may rename or stop one; `decide:` says it may edit a
  suggested change before applying it, as you. Each keeps its checkbox.
- A set up row is its catalog name and description; a package that ships a
  function says it runs code, automatically when it has an enabled trigger.
- A re-authorization heads by what changed: **Renew access** (nothing new),
  **Allow more access** (new scopes), **Update server setup** (setup rows to
  do), or both of the last two. The new rows come first; what was allowed
  before folds under "Already allowed". Denying keeps the old access working,
  and says so.
- Blocked set up keeps Deny, shows what to do with a link (the provider page,
  or "your server needs an update"), and disables Approve.
- While it runs, each row shows its progress; a failure names the row, keeps
  what landed ticked and offers Try again.

### Technical (the switch on)

Everything the page shows today, plus, per set up row: the package id, held
and shipped versions, the requirement chain, each kind by full reference,
each function with what it may write, each trigger; the account's kind, id
and properties; the webhook's trigger id, whether it has a key, and that the
key is never shared; the raw `authorization_details`, copyable.

## SubstrateKit

| Folder | What |
| --- | --- |
| `Server/` | `ServerAddress` (the one normaliser), `Discovery` (all of `server.json`), `AuthorizationServerMetadata`, `ServerVersion` (`v0.113.0`, `0.113.0-4-g…`; `dev` is not comparable), `ServerRequirement` (a minimum release, the scope verbs and detail types an app needs) and its verdict |
| `API/` | `SubstrateClient` over an injected transport: bearer, timeouts by tier, `Retry-After` on 429 and 503 for idempotent requests only, the error envelope, `explain(_:app:)` (an `insufficient_scope` is "needs your approval", never "sign in again"), one path-segment encoder, records, lists, blob upload, webhook delivery |
| `Auth/` | `AppManifest` (built per run: client id, name, scopes, details, server requirement), `AuthorizationDetail`, `PKCE` (a failed random source throws), `OAuthClient` (metadata, the authorize URL, the exchange, never retried, `iss` required when advertised, revoke), `LoopbackReceiver`, `WebAuthenticator`, `Session` (an app session or a legacy password one, versioned), `SessionStore` (Keychain, an access group for extensions, available after first unlock for background work), `Grants` |
| `Status/` | `AppStatusCheck`: discovery, the requirement, `/authorizations/check` and the expiry, combined into a set of issues, any number at once: server too old, needs approval (with the changes in words), access ends soon, access ended, unreachable, legacy access to move |
| `UI/` | `ConnectView`, `ServerTooOldView`, `AppStatusBanner`, `ReviewChangesSheet` (the same sentences the console will show), `AccessEndingBanner`, `AccessEndedView`, `AccountSection`, `ChangeRow`, `StatusBadge`, `ProblemBox` |

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
  `/oauth/revoke`, and the Set Up screen becomes the kit's status. On an older
  server it signs in with the password and Set Up works as today. A password
  session on a server that now lists the detail types offers "Move to app
  access": the flow with the same install id (so the account is in place),
  the new session saved, then the old token revoked with its own
  `DELETE /tokens/{id}`.
- **Send to Substrate** reads the session from the shared Keychain group,
  either kind, and uploads under the token's blob rule.
- **Substrate Chat** moves its OAuth client, receiver and sheet to the kit,
  keeps the password fallback for a server without app authorization, and
  shows the kit's status: a server whose metadata lacks `agents:` and
  `decide:` is too old, rather than an error page.
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
  scope parser knows four verbs), which fails closed. A binary from before
  0194 reads every app token as full access; revoke app tokens before such a
  rollback, as today. What a set up installed stays after any rollback.

## Out of scope

- Samples through this door, and packages outside the binary's catalog.
- Binding an app's deliveries to its own account (the account header is the
  package's convention, not the door's).
- A provider key or any secret the owner types.
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
