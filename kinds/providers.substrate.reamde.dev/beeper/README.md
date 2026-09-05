# The Beeper bundle — the substrate's first non-OAuth connector

Beeper is a Matrix homeserver with bridges into WhatsApp, Telegram, Signal,
iMessage and the rest. This bundle connects it the way Matrix clients do — a
**pasted access token** against the client-server API — and mirrors the
bridged rooms and messages into the graph.

**SYNC-ONLY.** It reads; it never sends. Outbound messaging needs the outbox
contract (issue 009), which is pending — when that lands, sending becomes a
separate function behind the outbox's delivery states, not a widening of this
one.

Where the Google bundle proved the **host OAuth facility**, this one proves
the other credential path the runner supports: a **connector-own config
secret** (the contract: "A connector's OWN config secrets … ARE
injected and scrubbed"). No `oauth2:` manifest block, no oauth2 trait, no
tokenStatus — the whole flow is: paste a token, flip a toggle, sync.

## What it does

```
account (enabledMessages on, never synced)
      │  on-connect trigger (fires once — guarded on "no lastSyncedAt yet")
      ▼
messagessync (python / uv / Matrix client-server API) — PAGED end to end:
      │
      │  sync      ONE /sync (since = stored next_batch); every mentioned
      │            room is reduced to a small descriptor — nothing staged
      │  rooms     ≤25 room mirrors per invocation (put-if-absent + patch of
      │            only the delta-carried fields), history jobs queued
      │  backfill  ONE /rooms/{id}/messages page per invocation, newest-first
      │            down to the cutoff; a cap hit is recorded, then resumed
      │            by the NEXT run — truncation is never silent
      ▼
room mint-then-patch  +  message if_absent puts (immutable event ids)
      │
      └─ final page stamps the account: syncToken + lastSyncedAt + syncStatus
         (+ backfillResume, the rooms whose history walk continues next run)

hourly schedule ──▶ messagessync ──▶ the ONE live account, when due
```

- **`config`** (NOT oauth2), the `connector` input's kind: the homeserver
  base URL (defaults to `https://matrix.beeper.com`) and the secret-typed
  **`accessToken`** the owner pastes; the bundle's `connector` input
  resolves one record of this kind into the sync body (the sole record,
  the one named `default`, or a bound one). Redacted on every read surface,
  blank-on-edit, injected only into the sync body and scrubbed from everything
  that leaves the runner. **Origin pinning**: the body refuses to send the
  token anywhere except `https` on a `beeper.com` host or loopback (the test
  seam) — an owner-edited `homeserverUrl` pointing elsewhere parks the delivery
  with a clear error before any request is made.
- **`account`** (accountconfig, WITHOUT oauth2): `userId`, the
  `enabledMessages` toggle, an optional `roomFilter`, `syncFrequency`,
  `backfillDepth`, and the connector-written `syncToken`/`lastSyncedAt`/
  `syncStatus`/`backfillResume`. The accountconfig trait contracts
  `tokenRef`/`tokenStatus`/`grantedScopes` onto every implementing type, so
  they are declared — **dormant**: `writer: oauth` and no OAuth flow ever runs,
  so they stay absent for the row's whole life.
- **`room`** / **`message`** (mirror types): Matrix's own shape — room id,
  name, the bridged `network` (derived from ghost-sender prefixes like
  `@whatsapp_…`), and per message the sender, msgtype, body, `sentAt` and the
  raw event. Ids are `host.ids.external("beeper", account, id)`; messages hang
  off their room through a required `room` reference (`onDelete: cascade`, so a
  deleted room takes its mirror with it). Rooms are **mint-then-patch**: an incremental
  `/sync` delta that omits a field (lazy-loaded state, an owner-only timeline
  window) patches only what it carried — it never erases a stored name or
  resets a stored network.
- **`messagessync`** (python / PEP 723 / requests): at most one Matrix HTTP
  call per invocation, everything drained off the causal chain through the
  paged-checkpoint `more`/`resume` contract. Matrix `/sync` has no room
  pagination, so the **rooms themselves are the paged units**: the sync
  phase stages nothing (descriptors only), the rooms phase mirrors at most
  25 rooms per frame, the backfill phase fetches one 100-event page per
  frame — a bridged account with thousands of rooms never produces one
  unbounded frame. The account stamp is the LAST effect of a drain —
  messages first, cursor after, so a crash replays a window the `if_absent`
  puts absorb.

## Honest limits

- **One account per repository, enforced.** The pasted token lives on the
  `connector` input's one resolved config record, so there is one
  credential; the sync
  processes only the lexicographically-first live account and stamps any
  other live account row `syncStatus: "ignored: duplicate account (this
  provider is one-account-per-repository until issue 011)"`.
- **History walks at most 2,000 events per room per drain**
  (20 pages × 100 events). A room with deeper history inside its
  `backfillDepth` window is NOT silently cut off: the drain records the
  room and its resume token in the account's `backfillResume`, stamps
  `syncStatus: "ok (N rooms backfill pending)"`, and the next run (hourly
  by default) resumes the walk where it stopped — deep history arrives
  across runs.
- **`backfillDepth: none` means from connect time, forward-only** (the
  fleet-wide meaning): the first sync mirrors rooms but no history, and
  every later run picks up what arrived since the last completed one.
- **`syncStatus` vocabulary**: `ok`, `ok (N rooms backfill pending)`,
  `erroring: <reason>` (a provider failure stamps the account and the
  delivery completes — with every un-walked room preserved in
  `backfillResume` — instead of parking the chain), and `ignored: duplicate
  account …`.
- **Message edits, reactions and redactions are not folded in** — an edit
  arrives as its own event and the original row stands. Bridged-sender
  identity resolution is a deliberate later slice (below).

## Why the token lives on the config, not the account

An engine necessity, not a taste call: every **secret**-typed property on an
`accountconfig`-trait type is AEAD-**sealed at the storage boundary**
(review-google #3, `write.go sealSecretProps`), and the runner's config
resolution injects account properties **as stored** — a token pasted onto the
account would reach the sync body as `substrate:sealsecret:v1:…` ciphertext. A
non-accountconfig config kind stores a connector-own secret un-sealed, and
the injected `connector` input hands it over usable (the proven `apiToken`
pattern the scrubber tests pin down). The body detects the sealed prefix and
says so rather than failing opaquely.

Consequence: **one Beeper account per repository** (the `connector` input
resolves one config record, and the sync enforces the one-account rule — see
Honest limits). Beeper is one account per human, so this fits; a future
engine wave that opens sealed connector secrets at injection (issue 011)
would let the token move onto the account and lift the limit.

## Why there is no recordmapping

Both candidate mappings were checked against the messaging package and
deliberately skipped:

- **message → conversationmessage**: `conversationmessage` requires a
  `conversation` reference (`onDelete: cascade`) and an `author` person
  reference at write — a mapping mints property-only shells and could satisfy
  neither, and the type carries no external-id property for `match` to probe,
  so every message would try to mint and fail admission.
- **sender → person**: a Beeper sender is a bridge ghost
  (`@whatsapp_15551234567:beeper.local`, `@telegram_…`); only some networks
  embed a phone worth matching, and a zero-match MINTS — every group chat
  would spray person shells named after ghost ids into the people space.

Identity for bridged senders needs a resolver with per-network semantics (the
v4 `ResolveContact` had one); that is a later, deliberate slice.

## Install, configure, connect

```sh
substratectl apply -f bundle.yaml -f triggers.yaml
```

1. **Get an access token.** Either:
   - `bbctl login` — the Beeper bridge CLI stores your Matrix access token in
     `~/.config/bbctl/config.json` after login, or
   - **Beeper Desktop** — Settings → Developer (or Help → Advanced) exposes
     the current access token of the logged-in session.

   The token is a Matrix access token for `matrix.beeper.com`; treat it like
   a password.

2. **Create the config.** One `config` with the pasted `accessToken` (leave
   `homeserverUrl` unset for Beeper's default; a set one must be `https` on a
   `beeper.com` host).

3. **Add the account.** Create an `account` with your `userId`
   (`@you:beeper.com`), set `enabledMessages: true`, pick a `syncFrequency` and
   a `backfillDepth`. The on-connect trigger fires the first sync off the
   create — no OAuth flow, no callback, no consent screen.

`roomFilter` narrows the walk: a case-insensitive substring the room's name
or network must contain (`whatsapp` syncs only WhatsApp rooms). When an
incremental delta omits a room's name and network, the filter evaluates
against the stored mirror — a matching room never silently stops syncing
because one window happened to carry only the owner's replies.

## Files

- `bundle.yaml` — the **schema closure**: package + bundle + config type +
  account type + the two mirror types + the sync function.
- `triggers.yaml` — the **delivery wiring**: the on-connect backfill trigger
  and the hourly re-sync schedule, as ordinary data records.

## Tested

`engine/beeper_bundle_db_test.go` installs this closure from these very
files. It admits the schema through the loader (the non-OAuth shapes: a
`connector` input injected into functions, an accountconfig account WITHOUT
oauth2, no manifest `oauth2:` block, no mapping) and, where uv is available, applies the
closure into a test repository and drives the sync against a **fake homeserver**
(httptest): a first sync of MORE rooms than one page holds drains across
pages and mirrors every room and message; a partial incremental delta
patches without erasing the stored name/network and the roomFilter falls
back to the mirror; a room that hits the per-drain backfill cap leaves
resumable state that the next run drains to completion; a duplicate account
is stamped ignored and never synced; a provider failure stamps
`erroring: <reason>`. No live Beeper call runs in any test.
