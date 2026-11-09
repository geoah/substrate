# Plan: each chat network as the owner, through a chat bridge beside the server

Status: proposal, 2026-11-09, revised the same day after two independent
reviews (Fable's and Astra's; [section 15](#15-reviews) records what each
found and what changed). Written from five research reports on what already
connects to WhatsApp, Signal, Telegram, Messenger, Instagram, Discord, X,
LinkedIn, Bluesky, iMessage, SMS, Viber and the rest
([chat-bridge/](chat-bridge/README.md)), and from a reading of the Beeper,
Slack and Slack live events providers, the app authorization flow and the
webhook door. It is a plan, not a contract: the code that lands is the
contract, and where the two differ the code is right. [Section 6](#6-the-contract)
is written to be implemented from, so that the package, the bridge and the
console can be built at once; the package's README and fixtures replace it
once they land.

## 1. The problem

Chats reach a repository through one door today: the Beeper provider, which
polls the Beeper Desktop app on one computer every 15 minutes
([bundles catalog](../bundles-catalog.md#beeper)). That door has five limits:

- **It needs a desktop app running.** A sleeping laptop stops the sync, which
  is why [0183](../decisions/0183-an-unreachable-beeper-desktop-throttles-the-sync-and-throttled-reads-as-waiting.md)
  exists. It also needs a Beeper account, and the networks Beeper links on
  its cloud pass through Beeper's servers.
- **It is 15 minutes behind.** Nothing pushes.
- **It reads, and sends one text.** `sendmessage` is the only write: no
  reaction, edit, delete or mark-read
  ([0125](../decisions/0125-a-provider-write-is-a-callable-that-writes-no-record.md)).
- **History is what Beeper indexed**, not what the network holds.
- **An agent that sends sends without review.** A provider write writes no
  record, and the policy door and `confirmation: always` act only on record
  effects, so an agent granted `sendmessage` sends as the owner unasked
  (0125, Consequences).

The owner wants each network connected directly, acting as the owner and never
as a bot: history as far back as the network allows, updates within seconds
and nothing lost across downtime, the people on each network as contacts, and
the owner's own hand in the chat (send, reply, react, edit, delete, mark
read), with the assistant able to do the same under the owner's control.

## 2. What exists

The research is in [chat-bridge/](chat-bridge/README.md), one report per
group of networks. What decides the shape:

1. **mautrix bridgev2 already puts the networks behind one Go interface.**
   [mautrix-go](https://github.com/mautrix/go) (MPL-2.0) is a framework whose
   network connectors (WhatsApp, Signal, Telegram, Messenger, Instagram,
   Google Messages, Google Voice, Slack, X, LinkedIn, Bluesky, Discord on a
   branch, and Beeper's LINE, GroupMe, Teams) each implement one interface:
   login flows, history, live events with catch-up, contacts, and every
   action (send, edit, react, delete, read, typing). Beeper runs the same
   connectors on its devices and in its cloud, so protocol breakages are
   fixed upstream within days.
2. **The connectors run without a Matrix server.** The Matrix side is two
   interfaces (`MatrixConnector`, 21 methods plus `BotIntent`, and
   `MatrixAPI`, 22). Two independent projects from the last month replace
   them with a local store: Courier (11 networks, 965 lines of glue) and
   nook-chatd (8 networks, 2,700 lines with its HTTP API). Both are separate
   AGPL programs beside a permissively licensed app.
3. **Licensing decides where the code may live.** mautrix-go is MPL-2.0. Every
   mautrix connector is AGPL-3.0, Signal also links Signal's AGPL `libsignal`
   (Rust, through cgo), and whatsmeow, MPL-2.0 itself, depends on GPL-3.0
   `go.mau.fi/libsignal`. The server must not link any of it; a separate
   program that talks to it over its public HTTP API may.
4. **Beeper's local API grew in 2026** (edit, delete, reactions, read and
   unread, typing, contacts, login sessions, an experimental WebSocket with no
   resume) and a headless "Beeper Server" exists. It stays proprietary and
   needs a Beeper account, and sign-ins cannot be exported from it.
5. **Nothing else fits.** Matterbridge relays channels with bot accounts.
   Unipile is a paid service that holds the sessions and covers four
   networks. The Texts.com SDK is abandoned. The messaging MCP servers wrap one
   network each. A real Matrix homeserver with stock bridges works, but it is
   one more stateful service and cannot backfill history before what it
   already holds.

Per network, the best way in. The risk column is a qualitative reading of
each network's terms and of reported practice, not a measurement:

| Network | Best way in | Sign-in | History | Risk to the account |
| --- | --- | --- | --- | --- |
| WhatsApp | mautrix-whatsapp (whatsmeow) | QR, or a pairing code typed on the phone | about 3 months, about 1 year with full sync chosen before linking, then 50-message pages from the phone | low to medium |
| Signal | mautrix-signal (signalmeow + libsignal) | QR (linked device) | the one-time transfer at link time (all chats, 45 days of media), then new messages | low, by practice |
| Telegram | mautrix-telegram (Go since April 2026, on an MIT gotd fork) | phone, code, 2FA password; or QR | everything | low by practice; its terms bind client behaviour and AI use ([section 10](#telegram)) |
| Messenger | mautrix-meta (messagix) | browser cookies, or a password through an emulated app | full, including encrypted history restored with the user's PIN | high from datacenter addresses; terms unverified |
| Instagram | mautrix-instagram (new in July 2026) | cookies, or an Android password login | cursor pages; deep history buggy | high |
| Google Messages | mautrix-gmessages (libgm) | Google account cookies, then an emoji match on the phone | what the phone holds | low |
| X | the official XChat API, or mautrix-twitter | OAuth plus the XChat PIN; or cookies | per API | none (paid per message); medium (cookies) |
| LinkedIn | mautrix-linkedin | cookies and headers | full | high; its terms restrict automated access |
| Discord | mautrix-discord's bridgev2 rewrite (unreleased) | QR or token | full | high; its terms forbid automating a user account |
| Bluesky | the official `chat.bsky` API | app password with DM access, or OAuth | full | none |
| iMessage, and SMS relayed by an iPhone | Beeper's MIT Swift package `platform-imessage` (reads `chat.db`, drives Messages.app) on an always-on Mac | the Apple ID signed in to Messages | everything on the Mac | none (on-device) |
| Android SMS and MMS | an Android app reading the telephony provider; RCS only through Google Messages | the phone | everything on the phone | none |
| LINE | Beeper's MIT bridgev2 connector `beeper/line` | email or QR | per the connector | medium; the terms forbid it |
| Viber | Beeper only: a closed connector, linked like Viber Desktop since October 2026 | QR from the phone, through Beeper | limited | through Beeper |
| WeChat, Snapchat | Beeper only (closed) | through Beeper | through Beeper | through Beeper |
| Google Chat, Teams | the official APIs for Workspace and work accounts (Chat API, Microsoft Graph, delegated) | OAuth | per API | none |

## 3. The shape

```
 WhatsApp  Signal  Telegram  Messenger  Instagram  …         (each network)
     │        │        │         │          │
 ┌───┴────────┴────────┴─────────┴──────────┴────────────┐
 │ chat bridge (cmd/chatbridge, its own Go module, AGPL)  │
 │  bridgev2 + network connectors                         │
 │  local Matrix side → bridge store → outbox (seq)       │
 │  action runner · sign-in · behaviour rules             │
 └──────┬──────────────────────────────▲─────────────────┘
        │ POST one batch at a time      │ GET /api/v1/changes?watch=1
        │ (webhook door, app token)     │ + record and blob reads (app token)
 ┌──────▼──────────────────────────────┴─────────────────┐
 │ substrate                                              │
 │  chatbridge-events webhook → chatevents (Python)       │
 │  → instance, account, user, chat, message,             │
 │    messageattachment rows; chataction outcomes         │
 │  owner / agent → chataction rows ──────────────────────┘
 └────────────────────────────────────────────────────────┘
```

**The bridge dials out; the server never dials the bridge.** It pushes what
happened on the networks to a webhook trigger, one batch at a time, and it
follows the change feed for what the owner asked it to do. So it can run
beside the server, on a home box, or on the owner's Mac, behind any NAT, and
the server keeps only the digest of the bridge's token, as for every app, and
opens no port to it.

**What this costs, against the alternative.** Report 01 recommended the other
direction: the bridge serves a REST surface shaped like Beeper Desktop's, and
the server's sandboxed functions poll it and call it, as the Beeper provider
does today. That would reuse the Beeper bundle's row makers, give ordering and
settlement for free (one sync per account at a time, a failed run keeps its
cursor), and keep every credential on the bridge. Its price is the server
reaching the bridge (a private address in `SUBSTRATE_SANDBOX_EGRESS_ALLOW`, a
tailnet for a bridge at home, a pasted bridge token, 0183's sleeping hosts),
a compatibility layer over a proprietary beta API, and no answer to agent
review, since a call writes no record (0125). Messenger, Instagram and
LinkedIn are safest from a residential address, so a bridge at home is a
normal deployment here, not an edge case, and agent review is a stated
requirement. The dial-out direction pays instead with a protocol of its own:
an ordered delivery stream with an applied acknowledgment
([section 6.1](#61-transport-and-delivery)), an action protocol with claims
and uncertain outcomes ([section 6.5](#65-actions-the-execution-protocol)),
and a remote sign-in protocol ([section 8](#8-sign-ins)). bridgev2 reuses the
networks' protocols, not these guarantees; they are this plan's to build.

## 4. Decisions this plan asks for

Each becomes a decision record when it lands; the numbers are taken then.

- **D1. A chat bridge program outside the server, built on bridgev2, under
  the AGPL-3.0.** It lives in this repository as its own Go module,
  `cmd/chatbridge/` with its own `go.mod` and `LICENSE`, imports no package of
  the server's module, and talks to the server only through the public HTTP
  API. The server's module never imports it. It is host software the operator
  installs and trusts, like the server itself; 0220 confines function bodies
  the server runs and says nothing about it. The owner chose this over a
  repository of its own ([section 14](#14-settled-with-the-owner)).
- **D2. The bridge is an app, and its instances trust each other.** It gets
  its token through the app authorization flow
  ([0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)),
  whose approval installs the package, keys and turns on its webhook
  ([0202](../decisions/0202-an-apps-approval-sets-up-what-it-needs-and-its-token-delivers-to-its-webhook.md)).
  Because the package's function writes every kind of the package, the token
  holds write scopes over all of them (0215). The bridge writes records only
  through the webhook's function and stores blobs only through the webhook
  door; that is the program's rule, not the token's limit. Every instance an
  owner approves for one repository reaches every account of the package: an
  instance id in a batch is the bridge's claim, not something the server
  authenticates, so instances are trusted programs of the same owner, and an
  instance acts only on the accounts it runs as a convention between them.
- **D3. One provider package, `providers.substrate.reamde.dev/chatbridge`,
  whose kinds are the same for every network.** The network is a property, as
  in Beeper's bundle ("one product, so no product prefix"): a WhatsApp chat
  and a Signal chat are one kind because the bridge's contract makes them one
  shape. The kinds take their nouns from that contract ([section 7](#7-the-package)).
- **D4. What the owner does in a chat is a `chataction` record the bridge
  carries out.** Send, react, unreact, edit, delete and mark read are each a
  record the owner or an agent writes; the bridge claims it, performs it once,
  and reports the outcome. A shipped gate policy holds every agent write of
  `chataction` for the owner until the owner lifts it per agent. This reopens
  0125 the way 0125 itself foresaw ("when a write must be queued or approved
  before it is sent"); the mirror rows keep 0125's one writer.
- **D5. Delivery is one ordered stream per bridge instance, with an applied
  acknowledgment.** The webhook door answers `202` once a request is spooled
  and runs each fire on its own (`internal/engine/webhookspool.go`,
  `fireDrained`), so neither ordering nor application can be read off the
  door. The bridge keeps one batch in flight; the function applies a batch
  only if it continues the instance's applied sequence and records the new
  high mark on the `instance` row in the same transaction; the bridge sends
  the next batch when it sees that mark.
- **D6. A sign-in is local to the bridge in phase 1, and a record-driven,
  sealed protocol from phase 2.** The phase 2 protocol is the common sign-in
  pattern for every network: bridgev2's steps rendered by the console and the
  apps, the owner's answers sealed to the bridge with HPKE
  ([section 8](#8-sign-ins)).
- **D7. The bridge behaves as the owner, never as a bot**, by rules split
  between the kinds (what only the owner may write), the policy door (what an
  agent may do unasked) and the bridge (pace, limits, what it never does on
  its own) ([section 9](#9-as-the-owner-not-as-a-bot)).
- **D8. A webhook trigger a package ships may arrive disabled and keyless
  when an app's approval is what keys and enables it, and the engine refuses
  to enable such a trigger without a key.** This amends
  [0168](../decisions/0168-a-provider-s-live-events-ship-as-an-opt-in-bundle-that-requires-it.md),
  which ruled out shipping one "not even disabled" because Resume would open
  it keyless, and which Apple Health's trigger already contradicts. The
  engine rule closes the hole for both.
- **D9. The Beeper provider stays.** It gains the writes Beeper now serves,
  as callables no agent is offered, and remains the way in for networks the
  bridge does not reach (Viber, WeChat, Snapchat) and for owners who already
  live in Beeper.

## 5. The chat bridge program

### What it is made of

- **bridgev2 and the network connectors**, imported as libraries
  (`go.mau.fi/mautrix-whatsapp/pkg/connector` and its siblings), one
  `bridgev2.Bridge` per network, each with its own database. All connectors
  in one binary share one mautrix-go version, so they move together on the
  monthly releases, and the `replace` directives the connectors carry
  (Beeper's forks of discordgo, slackgo and req) are copied into the bridge's
  `go.mod`, because Go does not inherit them. Portals are split per login, so
  every chat belongs to exactly one account.
- **A local Matrix side** (`MatrixConnector` and `MatrixAPI`) that writes
  rooms, members, profiles, messages, reactions, edits, redactions, read
  markers and media into the bridge store, and appends each change to the
  outbox as events of the contract. It sets `BatchSending: true` and
  implements `BatchSend`, so the backward history queue runs, and mints the
  deterministic Matrix ids the core needs. Courier's `localmatrix` and
  nook-chatd's `matrix.go` are working examples under the AGPL; code adapted
  from them keeps their copyright lines and a notice of the change.
- **The bridge store**: SQLite, one file per network plus one for the bridge
  itself (the outbox, the claimed actions, the account map, the sign-ins).
  It holds device keys and session cookies, so its directory is `0700` and
  belongs on an encrypted disk, and it is backed up: losing it means linking
  every network again, under a new instance.
- **The outbox and its pusher.** Every event gets the next `seq` and is
  written durably before the network is acknowledged where the network waits
  for one (whatsmeow's synchronous ack, signalmeow's handler return,
  Telegram's stored update state). The pusher follows
  [section 6.1](#61-transport-and-delivery): one batch in flight, the next
  only once the previous is applied. A server down for a day loses nothing.
- **The feed follower**, as [follow changes](../apps.md#follow-changes)
  prescribes: open `GET /api/v1/changes?watch=1` over the package's kinds
  first and keep its bookmark and generation, then list the `chataction` rows
  still `queued` and the `account` rows; on a `410`, list again. The feed's
  rows carry no payload, so the follower reads each named record, and it
  routes a deleted `account` through the bridge's own account map, since a
  deleted record cannot be read.
- **The action runner**: one worker per account, so one account's actions run
  in order. It claims, executes and reports per [section 6.5](#65-actions-the-execution-protocol),
  turning each action into the Matrix event bridgev2 expects (`m.room.message`,
  `m.reaction`, a redaction, an `m.replace` edit, an `m.receipt`) and queueing
  it with `Bridge.QueueMatrixEvent`. It keeps a timer for `notBefore` and
  `notAfter`, because nothing in the feed fires when a time passes.
- **Sign-in**: in phase 1, `chatbridge login <network>` on the bridge's host
  draws the QR in the terminal or prints the pairing code
  ([section 8](#8-sign-ins)).
- **Health**: `/healthz` and `/metrics` on a local port, as `slack-socket`
  serves them; nothing else listens.

### How it is laid out

```
cmd/chatbridge/
  go.mod  go.sum  LICENSE (AGPL-3.0)  NOTICE  README.md
  main.go                 run | authorize | login | status
  internal/config         environment
  internal/store          SQLite: outbox, claims, accounts, key-value
  internal/localmatrix    MatrixConnector and MatrixAPI over the store
  internal/networks       the connector registry; Signal behind a build tag (cgo)
  internal/server         authorize (PKCE on loopback), the token, the pusher,
                          the feed follower, record and blob reads
  internal/actions        the action runner and the behaviour rules
  internal/signin         the sign-in driver; HPKE from phase 2
```

`internal/server` is the bridge's client of the substrate server; the name
`internal/substrate` is the server module's contract package and is not
reused. The phase 1 network (WhatsApp) builds without cgo. Signal adds a build
stage that compiles `libsignal` at the commit mautrix-signal pins, and a build
tag that links it. The image is a new stage of the repository's one
Dockerfile, as `slack-socket`'s is, carrying the `LICENSE`, the `NOTICE`
(every dependency's license) and a pointer to the source of the exact build.

A nested module is outside the root's `./...`, so `mise run test`, `lint:go`,
`fmt:go`, `fmt:check` and `audit` do not see it. The bridge brings its own
tasks (`chatbridge:build`, `chatbridge:test`, `chatbridge:lint`,
`chatbridge:audit`), each joins its family's aggregate, and a CI job runs them.

### Where it runs

One instance carries many networks. A server-side instance next to the
server suits WhatsApp, Signal, Telegram and Google Messages. Messenger,
Instagram and LinkedIn lock accounts that suddenly appear from datacenter
addresses, so those belong on an instance at the owner's home or behind a
residential proxy in the owner's country. That lowers a known trigger; it is
not permission from the network and not a guarantee. Phase 1 runs one
instance per repository; several instances are allowed by the contract and
exercised in phase 2.

## 6. The contract

### 6.1 Transport and delivery

**One request** is `POST /webhooks/{authority}/chatbridge-events` with the
bridge's token as `Authorization: Bearer`, `Content-Type: multipart/form-data`:

- a part named `batch`, with no filename and the media type `text/plain;
  charset=utf-8`, holding the batch JSON (the door hands a text part inline as
  `value`; a part with any other media type or a filename becomes a blob);
- one part per media file the batch names, with a filename and its media
  type; the door stores each as a blob and hands the function its digest
  ([the delivery envelope](../functions.md#the-delivery-envelope)).

The batch:

```json
{
  "version": 1,
  "instance": "0b3f6f2e-6d8c-4c47-9a7e-2d8b1f5d9c11",
  "first": 1042,
  "last": 1043,
  "events": [
    {"seq": 1042, "at": "2026-11-09T10:00:01.123Z", "type": "user.upsert",
     "network": "whatsapp", "account": "447700900123",
     "user": {"id": "lid:113920541", "fullName": "Ada", "phoneNumbers": ["+447700900456"]}},
    {"seq": 1043, "at": "2026-11-09T10:00:01.124Z", "type": "message.upsert",
     "network": "whatsapp", "account": "447700900123", "chat": "447700900456@s.whatsapp.net",
     "backfill": false,
     "message": {"id": "3EB0C1F2A9", "sender": "lid:113920541", "fromOwner": false,
                 "at": "2026-11-09T09:59:58.000Z", "content": "text", "text": "see you at 7"}}
  ]
}
```

**Limits.** At most 200 events and 1 MiB of JSON per batch, history included;
media parts at most 5 MiB each and 32 MiB per request (the door's multipart
bound). The function declares `timeout: PT60S` and read budgets sized for 200
events (64 calls, 4,000 rows).

**One batch in flight, applied before the next.** The `instance` row carries
`appliedSeq`. The function:

1. reads the instance row (absent: `appliedSeq` is 0);
2. skips every event with `seq <= appliedSeq`, which makes a resent or
   hand-retried batch a no-op;
3. refuses the batch, failing the delivery, if `first > appliedSeq + 1`: an
   earlier batch has not applied, and the retry waits for it;
4. applies the rest in `seq` order, reducing events about the same row inside
   the batch first, because a function's reads see committed rows and not its
   own staged effects;
5. patches `appliedSeq: last` with `ifVersion` of the version it read, in the
   same transaction as the batch's effects, so two fires that race (a resent
   batch beside a hand's retry) cannot both apply.

The bridge sends batch `N + 1` only once the instance row reads
`appliedSeq >= last(N)`, which it learns from the change feed. It watches the
trigger's parked list (its `webhook:` scope reads it) for its own fire ids: a
fire that parked means batch `N` failed three times, and the bridge stops,
reports it on its health, and resends `N` with backoff (the skip in step 2
makes that safe). A batch that fails for its own content stalls the stream on
purpose: ordering is worth more than progress, and the parked count on the
account's sync status says so.

**A bad event does not fail a batch.** An event the function cannot apply
(its chat unknown because the owner's toggles left it out, a field the kind
refuses) is skipped, counted on the instance row (`skippedEvents`,
`lastSkipped`), and the batch goes on. Only an infrastructure failure (a
budget, a conflict, a store error) fails the delivery, and the engine retries
it.

**A new store is a new instance.** The instance id is a UUID minted when the
bridge store is created. A bridge whose store was lost starts at `seq` 1
under a new id, links its networks again, and its `account.upsert` events
repoint the accounts at it.

### 6.2 Ids

Every id in an event is the network's or bridgev2's own, verbatim (`account`
is bridgev2's `UserLoginID`, `chat` the portal id, `user` the ghost's
`UserID`, `message` the `MessageID`, `part` the `PartID`), except `instance`
and the `action` ids, which name the bridge and a substrate record. The
function composes every row id with `host.ids.external(provider, account,
external_id)`, normatively:

| Row | provider | account | external_id |
| --- | --- | --- | --- |
| `instance` | `chatbridge` | the instance id | `instance` |
| `account` | the network | the login id | `account` |
| `user` | the network | the login id | `user/` + user id |
| `chat` | the network | the login id | `chat/` + chat id |
| `message` | the network | the login id | `message/` + chat id + `/` + message id |
| `messageattachment` | the network | the login id | `attachment/` + chat id + `/` + message id + `/` + part id |

The instance is not part of an account's id, so a re-link, or a move to a new
instance, keeps every chat and message the account had. A reference in an
event (`sender`, `replyTo`, `members[].user`) names the network id and
resolves inside the same account. PR 1 lands these as test vectors: for each
row, the inputs and the id they produce.

**WhatsApp's two ids for a person.** A user's id is whatever the connector
uses for the ghost (a LID now, a phone-number JID before the migration); the
phone number is an attribute. Where the connector moves a person from one id
to the other, two `user` rows exist, and the contact mapping joins them on the
number.

**A message's parts.** One network message is one `message` row. bridgev2's
parts fold into it: the text parts into `text`, joined with a blank line, the
media parts into `messageattachment` rows keyed by part id. Replies,
reactions and edits target the message, not a part.

### 6.3 Events

Notation: `T?` may be absent (unchanged), `T | null` may be `null` (cleared).
`Instant` is RFC 3339 in UTC with milliseconds. `NetId` is a string.
`Media` is `{"part": "<multipart part name>"}`, bytes carried in this
request.

| Event | Fields |
| --- | --- |
| `instance.hello` | `name: string`, `version: string`, `networks: [{network, displayName}]` |
| `account.upsert` | `network`, `account: NetId`, `remoteName?: string`, `handle?: string`, `phoneNumber?: string`, `self?: NetId` (the owner's user id there) |
| `account.state` | `network`, `account`, `state: connecting \| connected \| transient_disconnect \| bad_credentials \| logged_out \| banned \| unknown_error`, `message?: string`, `warnings?: [{code: phone_offline \| temporary_ban \| reachout_timelock \| client_outdated \| flood_wait \| checkpoint, message: string, until?: Instant}]` |
| `account.removed` | `network`, `account`: the bridge logged it out and forgot it |
| `user.upsert` | `network`, `account`, `user: {id, fullName?, handle?, phoneNumbers?: [string], emails?: [string], isBot?: bool, isSelf?: bool, contactName?: string \| null, isContact?: bool, avatar?: Media \| null}` |
| `chat.upsert` | `network`, `account`, `chat: {id, category?: direct \| group \| channel \| space, chatName?: string \| null, topic?: string \| null, avatar?: Media \| null, otherUser?: NetId \| null, members?: {complete: bool, items: [{user: NetId, membership: join \| invite \| leave \| ban, role?: member \| admin \| owner}]}, mutedUntil?: Instant \| null, archived?: bool, pinned?: bool, markedUnread?: bool, readOnly?: bool, messageRequest?: bool, disappearAfterSeconds?: int \| null, parent?: NetId \| null, features?: Features, lastActivityAt?: Instant}` |
| `chat.delete` | `network`, `account`, `chat: NetId` |
| `chat.read` | `network`, `account`, `chat`, `upTo: NetId`, `at: Instant`: the owner's own read marker, from any device |
| `message.upsert` | `network`, `account`, `chat`, `backfill: bool`, `message: {id, sender: NetId, fromOwner: bool, at: Instant, content: text \| notice \| emote \| image \| video \| audio \| voice \| file \| sticker \| location \| poll \| unsupported, text?: string, editedAt?: Instant, editCount?: int, replyTo?: NetId, threadRoot?: NetId, mentions?: [NetId], mentionsEveryone?: bool, forwarded?: bool, expiresAt?: Instant, attachments?: [Attachment], action?: string}` |
| `message.delete` | `network`, `account`, `chat`, `message: NetId`, `scope: everyone \| owner`, `by?: NetId`, `at: Instant` |
| `message.reactions` | `network`, `account`, `chat`, `message: NetId`, `reactions: [{key: string, user: NetId, at: Instant}]`: the whole set the bridge holds |
| `message.status` | `network`, `account`, `chat`, `message: NetId`, `status: sent \| delivered \| read \| failed`, `at: Instant`: for the owner's own messages |
| `action.claimed` | `action: string`, `attempt: string`, `version: int` |
| `action.result` | `action`, `attempt`, `outcome: done \| failed \| uncertain \| expired \| cancelled`, `message?: NetId`, `error?: {code: ErrorCode, message: string}` |
| `backfill.progress` | `network`, `account`, `chat?: NetId`, `oldestAt?: Instant`, `complete: bool`, `pendingChats?: int` |

`Attachment` is `{part: NetId, content: image | video | audio | voice | file
| sticker, mimeType?, fileName?, size?: int, width?: int, height?: int,
durationMs?: int, caption?: string, file?: Media, mediaState: metadata |
stored | too_large | failed | expired}`. `Features` is `{edit: bool,
editMaxAgeSeconds?: int, deleteForEveryone: bool, deleteMaxAgeSeconds?: int,
reactions: bool, allowedReactions?: [string], replies: bool, threads: bool,
readReceipts: bool, typing: bool, maxTextLength?: int}`, read from bridgev2's
`RoomFeatures`. `ErrorCode` is `unsupported | not_found | rate_limited |
blocked | rejected | network | expired | cancelled | invalid`.

`text` is markdown, converted from the network's formatting by the local
Matrix side. A `version` the function does not know fails the delivery, so a
newer bridge waits for the package to be upgraded rather than writing a shape
it does not understand.

**Not mirrored.** Typing and presence are gone in seconds and would cost a
changelog entry each. Read receipts by others on the owner's messages arrive
as `message.status`, and a group's per-member receipts are left out.

### 6.4 Merge rules

- **Absent, null, empty.** An absent field leaves the property as it is,
  `null` clears it, and an empty list replaces the list with nothing.
- **Members.** `complete: true` replaces the chat's members; `complete:
  false` changes the listed ones only, and a `leave` or `ban` removes one.
- **Reactions.** `message.reactions` always carries the whole set the bridge
  holds for the message, so redelivery converges; the set is complete to the
  extent the network tells the bridge (bridgev2's `ReactionSync` where the
  connector has it).
- **Edits.** A `message.upsert` never lowers `editCount`, so a history page
  that returns an older text cannot undo a live edit.
- **Deletes.** `scope: everyone` marks the row `deleted: everyone` with
  `deletedAt` and keeps its text, its attachments' files and their captions:
  the owner keeps what they received as their archive
  ([section 14](#14-settled-with-the-owner)), and the console shows it under
  the deleted mark so nobody takes it for a live message. `scope: owner` (the
  owner deleted it for themselves on another device) marks `deleted: owner`
  and keeps the content too. A deleted row stays deleted against later
  upserts, its content as it was before the delete.
- **Disappearing messages.** `expiresAt` is recorded and nothing acts on it in
  phase 1; the same limit applies.
- **Chats.** `chat.delete` deletes the row; a later `chat.upsert` (new
  activity in that chat) brings it back, which is what the network did.
- **Unknown targets.** A `message.status`, `message.reactions` or
  `message.delete` for a message the mirror does not hold is skipped and
  counted. A `sender` or `replyTo` that names a row not yet written is
  written anyway and the engine marks it orphaned until the row lands; within
  a batch the bridge orders users before the messages that name them.

### 6.5 Actions: the execution protocol

A `chataction` is a request the owner or an agent writes. Its life:

1. **Queued.** Written without a `state`; the state machine starts it at
   `queued`, and its transitions are only the bridge's path: `queued` to
   `sending`, `failed`, `expired` or `cancelled`; `sending` to `done`,
   `failed` or `uncertain`. The loader takes no `writer:` on a state machine,
   so nothing at the door stops the owner moving it: the console never does,
   an agent's write is held by the gate, and the function moves it from the
   bridge's reports. The request properties are the owner's or the agent's;
   the other outcome properties are the function's (`writer: connector`).
   An action the bridge refuses before claiming it (the chat's `features`
   lack it, the account is not one it runs or is not connected, a rule of
   [section 9](#9-as-the-owner-not-as-a-bot) refuses it) is reported as
   `action.result` with outcome `failed` and no `action.claimed` first; the
   function applies it to a row that is still `queued` and carries no
   attempt.
2. **Claimed.** The worker for the action's account reads the record at
   version `v`, checks the request (the chat belongs to an account it runs, the
   account is connected, the chat's `features` allow the action,
   [section 9](#9-as-the-owner-not-as-a-bot)'s rules hold), mints an
   `attempt` id, and writes the claim `(action, attempt, v, digest of the
   request properties)` to its store before anything leaves for the network.
   It then emits `action.claimed`; the function moves the row to `sending` and
   records `attempt` and `claimedVersion`. The bridge executes the version it
   claimed: a later edit of the record does not change the attempt, and the
   record shows that it changed after the claim.
3. **Executed once.** The action's id is the transaction id bridgev2 carries
   to the network, where the network deduplicates one (WhatsApp's message
   id is chosen by the sender; Telegram's `random_id`). The outcome arrives as
   `action.result`: `done` with the network's message id, `failed` with an
   error code, or `uncertain`.
4. **Uncertain is a state, not a retry.** A bridge that restarts with a claim
   and no outcome looks for the sent message by its transaction id where the
   network allows; found, the action is `done`; not found or not checkable,
   it is `uncertain`, and nothing resends it. The owner decides, and a retry
   is a new action.
5. **Stale reports are ignored.** The function applies an `action.result` only
   when its `attempt` matches the row's, so a late report from an attempt the
   row no longer carries changes nothing.
6. **Cancelled and expired.** The owner sets `cancelRequestedAt`, never the
   state; a worker that has not claimed the action reports `cancelled`, and
   one that has claimed it finishes the attempt. `notAfter` (an hour after creation for
   `send` and `edit` when absent) makes a queued action `expired` instead of
   sending "see you at 7" the next morning. `notBefore` holds it.
7. **The echo links back.** The owner's own message comes back as a
   `message.upsert` carrying `action`; the function links the action's
   `result` to that message row.

`done`, `failed`, `uncertain`, `expired` and `cancelled` are final. PR 2's
tests cover a crash before the network call, a crash after the network
accepted it, a delayed result, a disconnect mid-send and two workers racing
for one action, against the WhatsApp connector's real behaviour as far as a
test can reach it, not only against a fake that deduplicates for free.

## 7. The package

`providers.substrate.reamde.dev/chatbridge`, version 1, published tier.

**Why these nouns.** The kinds take the bridge contract's nouns, and every
network names these things alike: `chat`, `message`, `user`. `message` keeps
its plain name rather than `chatmessage`, as the network's noun (the
kind-review skill's provider rule), and the kinds that belong to one of them
start with it: `messageattachment`, `chataction`. `account` is the connection
the console lists; here that is one sign-in on one network, because each has
its own credential, state and toggles, and there is no single connection to a
service as Beeper's bundle has.

| Kind | Purpose | Titled by | One record is |
| --- | --- | --- | --- |
| `instance` | internal | `instanceName` | one running copy of the chat bridge program: its name, version, the networks it can run, how far its stream has applied |
| `account` | supporting | `displayName`, else `remoteName` | one sign-in on one network through one instance: the connection (binds `accountconfig` and `sync`), its state and warnings, the owner's toggles |
| `user` | supporting | `fullName`, else `handle` | one person or account on a network, as one sign-in sees them: names, handle, numbers, whether they are in the owner's address book there |
| `chat` | primary | `chatName`, else the other person's name | one conversation: direct, group, channel or space, with its members, flags, what it allows, and the owner's read marker |
| `message` | primary | `{sender.fullName}: {text}` | one message in one chat, on whatever network sent it |
| `messageattachment` | supporting | `fileName`, else `content` | one file sent with a message: its metadata and, once fetched, its bytes |
| `chataction` | supporting | `{action}: {text}` | one thing the owner asked to be done in a chat as them, and what came of it |

The property lists, each `writer: connector` unless it says otherwise:

- **`instance`**: `instanceId` (string, required), `instanceName`, `version`,
  `networks` (repeated object: `network`, `displayName`), `appliedSeq` (int),
  `skippedEvents` (int), `lastSkipped` (string), `lastSeenAt` (datetime,
  written at most every five minutes).
- **`account`**: binds `substrate.reamde.dev/core/accountconfig` and
  `substrate.reamde.dev/core/sync`, redeclaring their properties as Apple
  Health's account does (the OAuth trio declared and unwritten; the function
  stamps the sync properties itself, because a webhook fire names no record:
  `connected` is `syncState: ok`, `connecting` is `running`,
  `transient_disconnect` is `throttled` with the network's message in
  `syncMessage`, the other states are `erroring` with the message in
  `syncError` and `syncErrorAt`, the first warning's message goes in
  `syncMessage`, and `lastSyncedAt` moves when a batch touching the account
  applies, at most every five minutes). `network` and `loginId` (both
  required, so the console never offers the owner an account to create:
  the bridge adds them), `instance` (reference), `remoteName`, `handle`,
  `phoneNumber`, `user` (reference to the owner's own `user` row),
  `connectionState` (enum as in `account.state`), `connectionMessage`,
  `warnings` (repeated object: `code`, `message`, `until`). Owner's
  (`writer: owner`): `displayName`, `syncPaused`, `syncRequestedAt`,
  `downloadMedia` (enum `none`, `small`, `all`; `small` means images and voice
  notes up to 5 MiB), `sendsPerHour` (int, default 30), `sendsPerDay` (int,
  default 200), `unlinkRequestedAt` (datetime).
- **`user`**: `account` (reference, cascade), `network`, `userId`,
  `fullName`, `handle`, `phoneNumbers` (repeated), `emails` (repeated),
  `isBot`, `isSelf`, `contactName`, `isContact`, `avatar` (`blobref`).
- **`chat`**: `account` (reference, cascade), `network`, `chatId`,
  `category` (enum), `chatName`, `topic`, `avatar` (`blobref`), `otherUser`
  (reference to `user`), `members` (repeated reference to `user`, with
  `membership` and `role` as link properties), `mutedUntil`, `archived`,
  `pinned`, `markedUnread`, `readOnly`, `messageRequest`,
  `disappearAfterSeconds`, `parent` (reference to `chat`), `features`
  (object, the fields of `Features`), `lastActivityAt`, `readUpTo`
  (reference to `message`), `sourceSeq` (int).
- **`message`**: binds `temporal(point)` on `sentAt`. `account` and `chat`
  (references, cascade), `network`, `messageId`, `sender` (reference to
  `user`), `fromOwner`, `sentAt`, `content` (enum), `text` (`text`, embedded
  for search), `editedAt`, `editCount`, `replyTo` and `threadRoot`
  (references to `message`), `mentions` (repeated reference to `user`),
  `mentionsEveryone`, `forwarded`, `expiresAt`, `reactions` (repeated object:
  `key`, `user` (reference), `reactedAt`), `deleted` (enum `everyone`,
  `owner`), `deletedAt`, `sendStatus` (enum), `backfilled`, `sourceSeq`.
- **`messageattachment`**: `message` (reference, cascade), `partId`,
  `content` (enum), `mimeType`, `fileName`, `size`, `width`, `height`,
  `durationMs`, `caption`, `file` (`blobref`, declaring the media types it
  admits), `mediaState` (enum).
- **`chataction`**: request properties, written by the owner or an agent:
  `action` (enum `send`, `react`, `unreact`, `edit`, `delete`, `markread`,
  required), `chat` (reference, required), `target` (reference to
  `message`), `text`, `replyTo` (reference to `message`), `reactionKey`,
  `forEveryone` (bool), `notBefore`, `notAfter`. Owner's alone (`writer:
  owner`): `allowNewChat` (bool), `cancelRequestedAt`. Outcome properties:
  `state` (a state machine with the transitions of
  [section 6.5](#65-actions-the-execution-protocol), which takes no
  `writer:`), and, `writer: connector`, `attempt`, `claimedVersion`,
  `result` (reference to `message`), `errorCode`, `errorMessage`. Sending a
  file is phase 2: it adds a top-level `file` (`blobref`), which the bridge
  reads under 0219.

A `blobref` is always a property of its own, never a field of an object
(`internal/vocabulary/load.go`), which is why attachments are a kind. Every
enum carries labels the console shows as written (`message.sendStatus`:
Sent, Delivered, Read, Failed; `message.deleted`: For everyone, For you;
`chataction.action`: Send, React, Remove reaction, Edit, Delete, Mark read).

**Disconnect and unlink.** `user`, `chat` and `message` cascade from their
`account`, so deleting the account row (the Providers page's Disconnect)
deletes every chat and message it brought in, and the bridge reads the
delete as an unlink. `unlinkRequestedAt` signs the account out of its
network and keeps them.

**Record views.** The console names no provider's kinds (decision 0166), so
the chat's page (its messages, the composer that writes `chataction` rows,
the owner's recent actions, the agent actions waiting on the gate) and the
account's page (its connection, instance and the owner's settings) are
`substrate.reamde.dev/console/recordview` records the package ships in
`views.yaml`. They use the `conversation` and `request` blocks the console
PR adds in console package version 7, so the package lands with or after it.

**Function `chatevents`** applies one delivery per [section 6.1](#61-transport-and-delivery):
Python, `PT60S`, `permissions.writes` the seven kinds, `permissions.reads`
the same seven with budgets of 64 calls and 4,000 rows, no network. One
delivery is one batch; no cursor, no drain.

**Trigger `chatbridge-events`**: a webhook, shipped disabled and keyless,
`maxBody` at its default (1 MiB for the JSON part), as Apple Health's is.
The bridge's approval keys and enables it (0202); D8's engine rule refuses
enabling it by hand without a key.

**Gate policy** `chatbridge-agent-actions`, shipped in the closure:

```yaml
kind: substrate.reamde.dev/core/recordpatchpolicy
metadata:
  id: chatbridge-agent-actions
data:
  properties:
    selector:
      kinds:
        - providers.substrate.reamde.dev/chatbridge/chataction
      ops: [put, patch, delete]
    action: gate
```

Empty `agents` and `functions` mean every agent write and only agent writes,
directly or through a function tool; the owner's own writes, the console's
and the function's are not held. The gate stays kind-wide because Moss, the
seeded assistant, holds `writes: ["*"]`: a gate on the tools alone would let
its direct `write` of a send through.

**Reactions and mark-read pass unasked** ([section 14](#14-settled-with-the-owner)).
Three `allow` policies ship beside the gate, each naming one agent, one op and
one tool, and `overrides: chatbridge-agent-actions`
([0108](../decisions/0108-an-allow-outranks-a-gate-only-by-naming-it.md)):

```yaml
kind: substrate.reamde.dev/core/recordpatchpolicy
metadata:
  id: chatbridge-moss-reacts
data:
  properties:
    selector:
      kinds:
        - providers.substrate.reamde.dev/chatbridge/chataction
      ops: [put]
      agents:
        - substrate.reamde.dev/assistant/moss
      functions:
        - providers.substrate.reamde.dev/chatbridge/chatbridge_react
    action: allow
    overrides: chatbridge-agent-actions
```

and the same for `chatbridge_unreact` and `chatbridge_markread`. An allow
naming a tool covers writes through that tool and never the agent's direct
writes, so a send, an edit or a delete still waits for the owner whichever
way an agent asks. Another agent the owner wants to react unasked gets its
own three allows. The owner may also give the gate a judge agent with
criteria (the policy's `judge`), which accepts what fits within its
threshold. The policy is the owner's to edit once installed, and an upgrade
leaves an edited one alone. PR 1 tests that an install admits it in the same
apply as the kinds.

**Agent tools**: `chatbridge_send` (`chat`, `text`, `replyTo`),
`chatbridge_react` (`message`, `reactionKey`), `chatbridge_unreact`,
`chatbridge_edit` (`message`, `text`), `chatbridge_delete` (`message`,
`forEveryone`) and `chatbridge_markread` (`chat`, `upTo`), each a small Python
function offered to agents (`offer: {agents: true}`, `effect: write`) that
writes one `chataction` and returns its id. None declares `confirmation:
always`, which would also hold the owner's own calls and which no allow can
lift; the gate does the holding.

**Contact mapping** `substrate.reamde.dev/core/chatbridgeuser`, seeded in
core beside `beeperuser`, from `chatbridge/user` to `core/contact`, matching
`phoneNumbers` to `phones`, then `emails` to `emails`, then `fullName` to
`name`, and copying the name, numbers and addresses. It waits until the
package is installed (0232); core's version is bumped with it.

**The authorization request** the bridge sends:

- `scope`: `write:providers.substrate.reamde.dev/chatbridge/*` and
  `write:providers.substrate.reamde.dev/chatbridge/instance` (a glob never
  reaches an internal kind, so the internal one is named), which covers every
  kind the function and the tools write (0215) and the blob uploads the door
  makes for the function;
- `authorization_details`: `{"type": "package", "id":
  "providers.substrate.reamde.dev/chatbridge", "version": 1}` and
  `{"type": "webhook", "trigger": "chatbridge-events"}`;
- a loopback `client_id` in phase 1 (`app:local`), and a client document
  published under `substrate.reamde.dev` later, so the approval page can name
  the bridge (0209, 0221).

## 8. Sign-ins

Three credentials meet in this design, each with its own door:

| Who proves what to whom | How |
| --- | --- |
| The bridge to the server | `chatbridge authorize --server https://…`: loopback PKCE, the approval page, the token kept in the bridge store. On a headless host, run it with the loopback port forwarded, or on a laptop and hand the bridge the token. |
| The owner to a network | phase 1: on the bridge's host; phase 2: a `signin` record driven step by step |
| The network session | the bridge store; never the server |

**Phase 1: on the bridge's host.** `chatbridge login whatsapp` draws the QR
in the terminal, or with `--phone` prints the pairing code to type on the
phone, after asking how much history to request (WhatsApp decides that at
link time). The server learns of the sign-in only through `account.upsert`
and `account.state`. Nothing secret crosses the server.

**Phase 2: the common sign-in protocol.** It covers every network with one
shape, bridgev2's login steps, so the console and the apps render every
network the same way:

1. The owner picks an instance, a network and a flow, and how much history to
   ask for; the console writes a `signin` record (`instance`, `network`,
   `flow`, `historyDepth`, `account` when re-signing an existing one).
2. The bridge sees it and runs the flow. Each step it reaches becomes a
   `signin.step` event: `kind` (`user_input`, `display_and_wait`, `cookies`,
   `complete`), `stepId`, `txnId` (bridgev2's per-instance step id),
   `instructions`, `fields`, `display` (`qr`, `code`, `emoji`), `expiresAt`,
   and, on a step that takes input, a fresh HPKE public key. A
   `display_and_wait` refresh (a WhatsApp QR every 20 seconds) mints no key,
   the pusher sends it at once rather than waiting for a batch, and the row
   keeps only the current step.
3. The console seals the answers to the step's key: HPKE base mode, suite
   DHKEM(P-256, HKDF-SHA256), HKDF-SHA256, AES-128-GCM, with the info string
   `chatbridge signin` + repository + instance + signin id + `txnId`, and
   patches `input` (`txnId`, `enc`, `ciphertext`, base64). Before sealing it
   checks that the key was written by
   `function:providers.substrate.reamde.dev:chatbridge:chatevents`, which the
   owner's token can see and an app's cannot.
4. The bridge opens it with the step's private key, which it holds in memory
   only and never writes to its store, submits it, records the `txnId` as
   consumed, drops the key and moves on. An input for a consumed or unknown
   `txnId`, or after `expiresAt`, is refused. A bridge that restarts mid-step
   has lost the key, so it fails the step and the owner starts again.

Cookie steps run only in the Apple and Android shells, and only from
templates the apps ship per network (the URL to open, the URL that means done,
the cookie names), named by an id in the step. A bridge never sends a script
or a URL for a credential-bearing web view to run. The web view uses an empty
data store and is discarded afterwards. A browser offers the paste fallback
for the same template.

**What sealing protects.** The stored record, the changelog, every export and
backup: a Telegram 2FA password or a Messenger session cookie never sits
there in the clear, and the ciphertext is useless once the bridge drops the
step's key. It does not protect against the server itself: a server that
serves the console its own script can read what the owner types. Instances
are trusted alike (D2), so the writer check in step 3 keeps a key from
another app, not from another instance. The browser side uses a reviewed
library (`hpke-js`, MIT) checked against RFC 9180's test vectors, Go uses
`crypto/hpke` (in Go 1.27), Apple CryptoKit and Android Tink, with an
interoperability test across all four before the protocol ships.

**Later: a device grant for headless programs.** The OAuth device
authorization grant (RFC 8628: the program shows a code, the owner approves it
in the console) would serve this bridge, `slack-socket` and any future helper
better than a forwarded loopback port. It is its own decision.

## 9. As the owner, not as a bot

Each rule sits where it can be enforced.

**In the kinds.** `allowNewChat`, the send limits, `downloadMedia`,
`syncPaused` and `unlinkRequestedAt` are `writer: owner`: only the owner's
unscoped hand writes them (`mayWriteProp` in `internal/engine/write.go`),
which refuses every app token and every function and agent. An agent's held
request that the owner accepts applies as the owner, which is the owner
deciding. An assistant reaching the repository over MCP with the owner's
token holds the owner's hand (0230); that is the owner's choice to make and
the console says so.

**At the policy door.** An agent's send, edit or delete is held until the
owner accepts it; the assistant's reactions and read markers pass, through
their own tools only, and the owner widens or narrows that per agent and tool
([section 7](#7-the-package)). The bridge trusts the record, not who wrote
it, since an app's reads carry no provenance.

**In the bridge.**

1. **Nothing is sent that nobody asked for.** Every outbound act is a
   `chataction`. There are no automatic replies.
2. **Reading marks nothing.** No read receipt leaves the bridge except a
   `markread` action, and presence stays unavailable, so the owner's phone
   keeps its notifications. This is the default per network, not a rule for
   all: Telegram's terms forbid a client that hides reads, presence or
   typing, so its profile offers no "ghost" feature ([section 10](#telegram)).
3. **Limits.** At most `sendsPerHour` and `sendsPerDay` sends per account, at
   least 3 seconds between sends in one chat, and the same text to one chat
   in an hour. A typing indicator before a send, paced to the text, is
   phase 2.
4. **Strangers are the owner's call.** A first message to someone who is not
   a contact on that network, or a new group, needs `allowNewChat`.
5. **One device, honestly named.** One linked device per account, named
   "Substrate" in the owner's list of linked devices, never re-paired in a
   loop; the connectors kept current, since whatsmeow moves WhatsApp Web's
   version weekly and Signal cuts off clients older than about three months.
6. **Stop on a warning.** A temporary ban, a reachout limit, a checkpoint or
   a flood wait stops all sending on that account and shows on it in the
   network's own words.

None of this is permission from a network. These rules lower the known
triggers for a restriction; they do not exempt the owner from a network's
terms.

## 10. Per network

### WhatsApp

Phase 1, the vertical slice. mautrix-whatsapp's connector over whatsmeow, QR
or pairing code. Full sync and its day limit go into the device properties at
pairing, so `chatbridge login` asks for the history depth before it shows the
QR. History-sync blobs are stored before they are acknowledged (WhatsApp
deletes them from its server once fetched), and older history comes 50
messages at a time while the phone is online. People are keyed on their LID,
with the phone number as an attribute once known ([section 6.2](#62-ids)). The
account warns when the phone has not been seen for 12 days (WhatsApp logs the
device out at 14).

### Signal

Phase 2, because of its build (Rust `libsignal` through cgo). signalmeow is
the only library that receives the history Signal transfers once at link
time, so it must be the library before the first link: a device linked with
anything else never gets that history. The account warns before Signal's
limits bite (the phone online within 30 days, the device within 45).

### Telegram

Mirrored, by the owner's decision ([section 14](#14-settled-with-the-owner)),
and built right after phase 1 on mautrix-telegram's Go connector, with the
operator's `api_id` and `api_hash` from the environment. Technically it is
the cleanest network (full history, gap-free catch-up, every action). What
its terms bind, and how the bridge meets each:

- **The AI clause.** Telegram's API terms (§1.5) and content licensing terms,
  since about February 2026, forbid using Telegram data to "engage in the …
  deployment of artificial intelligence" without explicit, continuing consent
  from the participants. Mirroring Telegram into shared kinds an assistant
  searches plausibly falls inside it, and no agent grant can exclude one
  network's rows from a kind. The clause binds whoever holds the `api_id`,
  which on a shared deployment is the operator who registered it.
  The owner chose to mirror Telegram knowing this, and holds the `api_id`.
- **Client behaviour.** §1.4 forbids clients that interfere with read,
  presence, typing or disappearing-content behaviour, so the bridge offers no
  "ghost" feature: archiving marks nothing read, a `markread` action sends the
  receipt a person's client sends after reading, and presence stays offline
  as an idle official client's does.
- **Sponsored messages.** §3.3 requires them in channels for clients that
  show channels; the bridge follows the connector's handling and says what it
  does.

One process owns a session: Telegram kills one used from two connections at
once, so nothing but the bridge may open it.

### Messenger and Instagram

Phase 2. mautrix-meta and the new mautrix-instagram, on an instance at the
owner's home. Cookie sign-in from the apps' web view templates, or Messenger's
and Instagram's password flows. Messenger's encrypted history is restored
with the owner's 6-digit PIN, which is one more sealed field. Both are
unofficial routes with unresolved permission and real account risk.

### Google Messages

Phase 2. mautrix-gmessages: the Google account's cookies, then an emoji match
on the phone (the QR flow is gone). History is what the phone holds, and the
phone must stay reachable.

### X, LinkedIn

Phase 3, opt-in. X's official XChat API is clean but billed per message and
needs the owner's own developer account; mautrix-twitter (cookies) is the
free path with account risk. LinkedIn's terms restrict automated access and
copying, and mirroring and replying only does not exempt it; it ships behind
an acknowledgment that says so.

### Discord

The live connection is the primary path, by the owner's decision
([section 14](#14-settled-with-the-owner)), built right after Telegram on
mautrix-discord's bridgev2 rewrite (the `megadiscord` branch, unreleased, by
pinned pseudo-version, as Courier imports it). Discord forbids automating a
user account even for replies the owner directs, and no official route reads
one's own DMs, so `chatbridge login discord` states that rule and asks for an
explicit yes before it links. The bridge keeps to the gateway, pages history
lightly, marks read only through `markread`, and stops sending on a rate
limit or a captcha. The data-package importer is not needed for now.

### Bluesky

Phase 3, and no bridge needed: the official `chat.bsky` API with an app
password or OAuth, polled through `getLog`. Either a bridgev2 connector of
our own on indigo (MIT), so it lands in this package, or a small provider of
its own.

### iMessage and SMS

The Mac holds both: `chat.db` to read, Messages.app to send, and an iPhone
relays SMS to it. Beeper open-sourced its macOS engine,
[`platform-imessage`](https://github.com/beeper/platform-imessage) (MIT,
Swift): it reads `chat.db`, watches it for changes, and drives Messages.app
through AppleScript and the Accessibility API to send, reply, react, edit,
undo a send, mark read and type, with System Integrity Protection left on.
Phase 3 puts it in the Substrate Mac app as an on-device bridge speaking this
plan's contract: the contract does not care what language a bridge is written
in. It needs an always-on Mac with a logged-in session and Messages open, and
the Accessibility, Contacts, Full Disk Access and Automation grants. Until
then, Beeper Desktop on the Mac runs the same engine and the Beeper provider
reads it. Nothing works on iOS. The libraries that reimplement Apple's
protocol without a Mac (pypush, rustpush) are SSPL-licensed and are not used.

On Android, an app can read and send SMS and MMS through the telephony
provider, but no third-party app can read RCS; RCS comes through Google
Messages ([above](#google-messages)).

### Viber, WeChat, Snapchat

No public library speaks Viber's linked-device protocol, its bot API is a
paid business account with its own identity, and Viber Desktop's database is
encrypted with a key derived at run time. Beeper added Viber in October 2026
as a linked device, closed source, with limited history. So Viber, WeChat and
Snapchat come through Beeper and the Beeper provider.

### LINE, Google Chat, Teams and the rest

LINE has an MIT bridgev2 connector from Beeper (`beeper/line`), so it can
join the bridge in phase 3, with the account risk the terms imply and one
more question first: it bundles a transpiled copy of LINE's own encryption
module, whose redistribution in a public repository is not settled. Matrix
itself needs no bridge at all: mautrix-go's client (MPL-2.0) reads and
writes it as the owner. Google
Chat on a Workspace account and Teams on a work account have official APIs
that act as the user (the Chat API, Microsoft Graph with delegated
permissions); each is a plain provider of its own if wanted. KakaoTalk,
Threema and consumer Teams have nothing usable. The device-bound and
long-tail report
([chat-bridge/05-device-and-longtail.md](chat-bridge/05-device-and-longtail.md))
has the details per network.

## 11. Beeper

**Writes, now.** `editmessage`, `deletemessage`, `addreaction`,
`removereaction`, `markread` and `markunread`, each a callable like
`sendmessage` (0125), reaching the same private origin with the same token.
The research disagrees on which of these routes Beeper serves (report 01
read them in the SDK and changelog, report 05 in the published OpenAPI
description, which lags the SDK), so each is checked against the SDK before
it ships, and one Beeper does not serve is left out.
None carries `offer:`: an agent offered them would send as the owner unasked,
which is the problem [section 1](#1-the-problem) names. The owner's console
and direct calls reach them; the agent path is the bridge's `chataction`.

**Kept.** The owner keeps the Beeper provider in general
([section 14](#14-settled-with-the-owner)): it is the way in for Viber,
WeChat and Snapchat and for anything an owner already runs through Beeper.

**Live, later.** Beeper's WebSocket has no resume, so the 15-minute poll stays
the catch-up either way. A relay that holds `/v1/ws` could speak this plan's
contract and make Beeper Desktop one more instance of the chat bridge
package, which is what would bring Viber, WeChat and Snapchat close to real
time. That waits until the bridge's own path works.

## 12. Licensing and distribution

The bridge is a separate program under the AGPL-3.0, which is compatible with
whatsmeow's GPL-3.0 dependency and with the AGPL connectors and `libsignal`.
Before it ships: a dependency and license manifest (the `NOTICE`), checked in
CI; the `LICENSE` and `NOTICE` in the image; the source of each published
build reachable from the build itself (the image's labels name the commit of
this public repository); copyright lines kept on anything adapted from
Courier or nook-chatd. The Beeper CLI carries no license and is read, never
copied. The server's own license is still undecided
([section 14](#14-settled-with-the-owner)), and the separate program
keeps that choice open. This is an engineering reading of the licenses, not
legal advice.

## 13. Phases and work

**Phase 1** proves one network end to end. Five pull requests, which can be
built at once from [section 6](#6-the-contract) and
[section 7](#7-the-package):

1. **The package**: the seven kinds, `chatevents`, the trigger, the gate
   policy, the six agent tools, the record views (`views.yaml`, on top of the
   console PR), the contact mapping (with core's version
   bumped), the README carrying the contract and a JSON Schema of the batch,
   golden fixture batches and the id test vectors, the catalog entry, and
   Python body tests in `internal/providertest` (reversed and resent batches,
   a gap, mixed partial events, delete before create, a stale history page,
   duplicates, an action's whole life and a stale result). Decision records
   for D2 to D5 and D7.
2. **The bridge, for WhatsApp**: `cmd/chatbridge` with the store, the local
   Matrix side, the outbox and the one-in-flight pusher, `authorize`, `login`,
   the feed follower, the action runner (send, react, unreact, edit, delete,
   mark read) with the behaviour rules, the Dockerfile stage, the mise tasks
   and CI job, the `NOTICE`, and tests over a fake network connector and an
   `httptest` server, plus the action crash tests of
   [section 6.5](#65-actions-the-execution-protocol). Decision record for D1.
3. **The console**: record-view blocks any package can use, since the
   console names no provider's kinds (0166): a `conversation` block (a
   chat's messages, a composer that writes request records, the owner's
   recent requests with their state, the agent requests waiting on the gate)
   and a `request` block (a button that stamps one owner property), in
   console package version 7; the Providers page's account details and a
   Disconnect that says what it deletes.
4. **Beeper writes**: the six callables, without `offer:`, their e2e
   fixtures and the catalog.
5. **The webhook rule**: refuse enabling a package-shipped webhook trigger
   that has no key and declares no signature, with tests for install,
   approval, upgrade and Resume. Decision record for D8, amending 0168.

**Right after phase 1**, from the owner's decisions: Telegram, then Discord's
live connection, each a pull request stacked on the bridge's; the package
keeps a message deleted for everyone and lets the assistant's reactions and
read markers through; the console shows a deleted message's kept text under
its mark.

**Phase 2**: Signal, the remote sign-in protocol and its console and app
steps, Messenger and Instagram (with the apps' cookie templates), Google
Messages, sending files, a second instance, the typing indicator, the device
grant.

**Phase 3**: X, LinkedIn, Bluesky, LINE, the on-device Mac bridge for
iMessage, Beeper as an instance.

**Testing.** No network is touched in CI. Each network gets a manual run on a
throwaway account, where the network allows one, before it is called done.

## 14. Settled with the owner

Answered on 2026-11-09:

1. **Where the AGPL code lives**: in this repository, as its own Go module
   with its own `LICENSE` (`cmd/chatbridge`).
2. **Telegram**: mirrored ([section 10](#telegram)).
3. **Deleted for everyone**: kept, and marked deleted
   ([section 6.4](#64-merge-rules)).
4. **Discord**: the live connection is the primary path
   ([section 10](#discord)).
5. **Agents**: reactions and mark-read go through unasked; sends, edits and
   deletes wait for the owner ([section 7](#7-the-package)).
6. **Beeper**: kept in general ([section 11](#11-beeper)).

## 15. Reviews

The first draft went to two reviewers, each asked to check its claims against
the code: Fable (a Claude model) and Astra (an OpenAI model). Both agreed that
a separate AGPL program on bridgev2 that dials out, delivers through the
webhook door and takes the owner's requests as records is the right shape for
this codebase, and both found the draft unready to build from. What changed:

- **Ordering and application** (both). The draft assumed the door delivers
  batches one at a time and that a `202` means applied. Fires run
  concurrently (`fireDrained` spawns one per request) and a `202` means
  spooled. Astra showed that a per-row sequence cannot order partial events
  and lets a deleted row come back. Now: one batch in flight, an applied
  watermark on the instance row written with `ifVersion` in the batch's
  transaction, skips for duplicates, a refusal for gaps, events capped at
  200, and a bad event that never fails its batch ([section 6.1](#61-transport-and-delivery)).
- **The action protocol** (Astra). The draft had a transaction id and no
  claim. Now: durable claims, attempt ids, the claimed version executed,
  `uncertain` instead of a resend, stale reports ignored, cancel, expiry and
  local timers ([section 6.5](#65-actions-the-execution-protocol)). Fable
  added `notAfter`.
- **The token and the instances** (both). 0215 makes the token write every
  kind the package's code writes; the draft said it only read. Instances are
  not isolated from each other by anything the server checks. D2 now says
  both plainly.
- **Media** (both). A `blobref` cannot be an object field, so attachments
  are a kind; and an app can read a blob a record names (0219), so sending a
  file needs no new decision. Inbound files ride the webhook request as
  multipart parts.
- **What only the owner writes** (both). The draft had the bridge enforce
  `allowNewChat`, which it cannot, since an app sees no provenance. Now
  `writer: owner` holds it at the door, the gate policy's selector is spelled
  out, and `confirmation: always` is ruled out because no allow can lift it.
- **Sign-in** (both). The key the console seals to is checked against its
  writer, bound to the step's `txnId`, held in memory only, minted only for
  steps that take input; replay, expiry and restarts are defined; web views
  run shipped templates, never a bridge's script; the threat model's limit
  (the server itself) is stated. Astra's simplest path became phase 1: sign
  in on the bridge's host, remote steps in phase 2.
- **Telegram** (Astra) moved out of phase 1: besides the AI clause, its terms
  forbid hiding reads and presence and require sponsored messages in
  channels.
- **0168** (both). Shipping a disabled keyless webhook contradicts it, as
  Apple Health already does; D8 amends it and adds the engine rule.
- **The contract** (both). Ids, field types, merge rules, deletes and the
  authorization request are now written to be built from, with fixtures and
  id vectors as PR 1's first job. Fable moved `beeperlive` out of phase 1 and
  kept the Beeper writes off agents.
- **Words** (Fable). `username`, `capabilities` and `identity` are retired
  words; the draft's properties are now `handle`, `features` and plain prose.
- **Not taken.** Astra's suggestion to drop simulated typing from the first
  increment was taken (it is phase 2); its suggestion to cut everything but
  WhatsApp from phase 1's actions was not: the six actions share one runner,
  and leaving five out would test the protocol less, not more.
