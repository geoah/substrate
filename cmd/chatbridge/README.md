# chatbridge

Connects the owner's chat networks to a substrate as the owner, never as a
bot. It carries WhatsApp and Telegram. The bridge runs the mautrix
[bridgev2](https://github.com/mautrix/go/tree/main/bridgev2) framework with
each network's connector against a local Matrix side instead of a homeserver,
pushes what happens on the network to the substrate's webhook door one
ordered batch at a time, and carries out the chat actions the owner asks for
(send, react, unreact, edit, delete, mark read) by following the change
feed. The design is [the chat bridge plan](../../docs/plans/chat-bridge.md);
the package it delivers to is `providers.substrate.reamde.dev/chatbridge`.

It is its own program and its own Go module, under the
[GNU Affero General Public License, version 3](LICENSE), because the network
connectors it links (mautrix-whatsapp, mautrix-telegram) are AGPL-3.0 and
whatsmeow links the GPL-3.0
`go.mau.fi/libsignal` ([NOTICE](NOTICE) lists every dependency and its
license). It imports no package of the substrate server and talks to it
only over the server's public HTTP API
([0249](../../docs/decisions/0249-the-chat-bridge-is-a-separate-agpl-program-in-its-own-module.md)).

## Running it

Three steps on the bridge's host, each with the store in `CHATBRIDGE_DATA`
(default `./data`).

1. **Authorize** the bridge against the substrate:

   ```sh
   chatbridge authorize --server https://substrate.example.com
   ```

   This is the app authorization flow with PKCE and a loopback redirect
   ([apps](../../docs/apps.md)): the owner approves on the server's approval
   page, which installs the `chatbridge` package and keys and turns on its
   `chatbridge-events` webhook. The token is kept in the store. On a headless
   host, `--print-url` prints the page instead of opening a browser, and
   `--listen 127.0.0.1:8765` fixes the callback port so it can be forwarded
   (`ssh -L 8765:127.0.0.1:8765 host`). The page shows the bridge as an app on
   the owner's device (`app:local`), because nothing verifies a loopback
   client. A running bridge picks a new token up within half a minute.

2. **Link** an account, with the bridge stopped (the store is locked while it
   runs):

   ```sh
   chatbridge login whatsapp
   ```

   It asks how much history the phone should send (`recent`, about three
   months; `year`; `all`), because WhatsApp fixes that when the device links,
   then draws the QR in the terminal. `--phone +447700900123` links with a
   pairing code typed on the phone instead, `--history` answers the question
   ahead, and `--invert-qr` draws for a light terminal. The device shows as
   "Substrate" under the phone's linked devices.

   `chatbridge login telegram` asks for the phone number, the code Telegram
   sends, and the two-step verification password when the account has one,
   read without echo; `--qr` links by scanning a QR in the Telegram app
   instead. See [Telegram](#telegram) first: it needs the operator's app
   registration.

3. **Run** it:

   ```sh
   chatbridge run
   ```

`chatbridge status` prints the instance, the server, the outbox and the
accounts, and works while the bridge runs.

## Configuration

| Variable                 | Default        | Meaning                                                              |
| ------------------------ | -------------- | -------------------------------------------------------------------- |
| `CHATBRIDGE_DATA`        | `data`         | The store directory                                                  |
| `CHATBRIDGE_LISTEN_ADDR` | `:8080`        | Where `/healthz` and `/metrics` are served                           |
| `CHATBRIDGE_NAME`        | the host name  | The instance's name, as the console shows it                         |
| `CHATBRIDGE_NETWORKS`    | every network  | The networks to run, comma-separated (`whatsapp`, `telegram`)        |
| `CHATBRIDGE_DEVICE_NAME` | `Substrate`    | The name a network shows in the owner's linked devices               |
| `CHATBRIDGE_DEBUG`       | `false`        | Debug logs from bridgev2 and the connectors, which include message text |
| `CHATBRIDGE_TELEGRAM_API_ID`   | none     | The operator's Telegram app id, from my.telegram.org; Telegram is offered only with both |
| `CHATBRIDGE_TELEGRAM_API_HASH` | none     | That app's hash                                                      |

The server address comes from the store, which `authorize` fills. The token
and the webhook URL are never logged.

## The store

`bridge.db` (the outbox, its sequence, the claimed actions, the account map,
the mirror the events are folded from), one database per network
(`whatsapp.db`: bridgev2's and whatsmeow's tables, including the device's
identity keys and Signal sessions) and `media/`. The directory is created
`0700`. It belongs on an encrypted disk and in the backups: losing it means
linking every network again, under a new instance. A new store is a new
instance, whose stream starts at 1 under a new id.

SQLite runs through `mattn/go-sqlite3`, the driver the mautrix bridges run
on. A pure-Go driver would not spare cgo: the WhatsApp connector links
`go.mau.fi/webp`, which compiles libwebp. The build needs a C compiler and no
C libraries.

## What it sends

Every event of the contract ([section 6.3](../../docs/plans/chat-bridge.md#63-events))
is written to the outbox with the next sequence number inside the call that
produced it. bridgev2 handles a network event synchronously and whatsmeow
acknowledges it only after, so nothing is acknowledged that is not durable.
The pusher posts `multipart/form-data` to the webhook with the bridge's
token: a `batch` part (no filename, `text/plain; charset=utf-8`) holding at
most 200 events and 1 MiB of JSON, and one part per file the batch names. It
keeps one batch in flight and sends the next only once the `instance` row's
`appliedSeq` reaches the last one, which it reads from the change feed (and
polls). A batch that parks on the trigger after its retries stops the stream,
shows on `/healthz`, and is sent again with backoff; the function skips what
it already applied.

Ids are the network's and bridgev2's own, verbatim. A message's parts are
folded into one `message.upsert`, users are sent to an account before the
first event that names them, and a chat is sent whole before its first
message.

## What it does for the owner

The feed follower opens `GET /api/v1/changes?watch=1` over the package's
`chataction`, `account` and `instance` kinds, keeps its bookmark, then lists
the queued actions and the accounts; on a `410` it lists again. Each action
runs on its account's one worker, by the protocol of
[section 6.5](../../docs/plans/chat-bridge.md#65-actions-the-execution-protocol):

- only a row in `queued` is acted on, and the owner cancels by writing
  `cancelRequestedAt`;
- the claim (action, attempt, version, digest of the request) is written to
  the store, with `action.claimed` in the same transaction, before anything
  leaves; a claim records when it left;
- the action's id becomes its transaction id: on WhatsApp the message id
  itself (`3EB0` and 18 hex digits, the shape WhatsApp Web uses), so a repeat
  would be the same message;
- the outcome and what it changed (the owner's message, carrying the action,
  the reactions, the deletion, the read marker) are written with
  `action.result` in one transaction, and a second report of an attempt
  changes nothing;
- a restart runs a claim that never left, and reports one that left as
  `done` when the network shows the message, else `uncertain`. Nothing that
  may have left is sent again;
- an action refused before a claim is reported as `failed` with a fresh
  attempt and no `action.claimed`;
- `notAfter` (an hour after creation for `send` and `edit` when absent)
  expires a queued action, and `notBefore` holds one.

The rules of [section 9](../../docs/plans/chat-bridge.md#9-as-the-owner-not-as-a-bot)
are the bridge's: no read receipt leaves except for a `markread` action, and
presence stays unavailable; nothing is sent that no action asked for; at most
`sendsPerHour` (30) and `sendsPerDay` (200) sends per account, three seconds
between sends in one chat, and the same text to one chat once an hour; a
first message to someone who is not a contact, or into a message request,
needs the owner's `allowNewChat`; a temporary ban, a reachout timelock, a
flood wait or a checkpoint stops sending on the account, and shows on it. An
account that is connecting or briefly disconnected holds its actions for five
minutes; one that is not coming back refuses them.

A deleted `account` row unlinks the login and forgets what was sent for it,
so a new link sends its chats and users again; `unlinkRequestedAt` unlinks
and keeps the row.

## Telegram

Telegram is carried by mautrix-telegram's Go connector, on its MIT fork of
gotd, beside WhatsApp in the same binary and on the same mautrix-go.

**Setup.** Telegram's API takes a client only under an application
registered at [my.telegram.org](https://my.telegram.org): its `api_id` and
`api_hash` go in `CHATBRIDGE_TELEGRAM_API_ID` and
`CHATBRIDGE_TELEGRAM_API_HASH`, and the network is offered only when both
are set. The account's active sessions list the device as "Substrate",
beside the registered app's name.

**The terms.** Telegram's API terms bind whoever holds the `api_id`, which
on a shared deployment is its operator. Since about February 2026 they
forbid using Telegram data for the deployment of artificial intelligence
without the participants' explicit, continuing consent (section 1.5), and an
assistant that searches the mirrored chats plausibly falls inside that; the
owner chose to mirror Telegram knowing it, and no agent grant can keep one
network's rows out of a kind. Section 1.4 forbids clients that hide reads,
presence or typing, and section 3.3 requires sponsored messages in channels.
None of this is legal advice.

**How it behaves**, against section 1.4:

- Reading marks something read only through a `markread` action, as a
  person's client does after they read. Mirroring and backfill mark nothing:
  the connector reads history without marking it, and increments no view
  counter.
- Presence stays offline, as an idle official client's does: the connector
  never sets the account online, and nothing types.
- Before a `markread` action, the bridge reports the chat viewed, as a client
  does when the owner opens it. In a broadcast channel the connector then
  fetches the channel's sponsored message (at most every five minutes, when
  the newest post changed), and the read that follows reports it viewed. The
  connector posts it as a notice from its bridge bot, which carries no
  network message id or sender, so the mirror does not carry it, and the
  owner reading a channel in the console does not see it: a gap against
  section 3.3 until the contract can carry one.
- One process owns a session. Two bridges over one store are refused by the
  store's lock. A copy of the store running elsewhere makes Telegram close the
  session (`AUTH_KEY_DUPLICATED`); the account then reads `bad_credentials`
  with a message saying so, nothing reconnects it, and the owner links again
  after stopping the copy. An unknown error waits for a restart for the same
  reason.
- A `FLOOD_WAIT` stops sending on the account until it ends, and
  `PEER_FLOOD` (Telegram's limit on writing to people who have not written
  first) for a day, since Telegram does not say how long it lasts (its
  @SpamBot tells the owner). Both show as a `flood_wait` warning with its
  end. The wait lives in the running bridge, so a restart forgets it and the
  next send meets Telegram's again.

**History.** Telegram keeps everything, so the depth `login` asks for bounds
the first backfill: `recent` fetches the latest 1,000 messages of each chat
and creates the 50 most recent chats at login, `year` 5,000 and 200, `all`
everything. The deepest depth any linked account asked for bounds the
network from then on.

**What is mapped.** Users by their numeric id, with their own name, their
username as `handle`, their number when Telegram shares it, and whether they
are in the owner's contacts as the owner's own session last saw them; the
owner's address book names are not read (`contactName` stays unset).
Direct chats are direct, groups and supergroups are groups, broadcast
channels are channels. Messages, edits, deletions, reactions, read markers
and files map as for WhatsApp. Not mapped: secret chats, which live only on
the device that started them, and sponsored messages (above). A send that
left before a crash cannot be found afterwards, because Telegram numbers
messages on its server, so it settles as `uncertain`.

## Endpoints

- `GET /healthz`: the instance, the pusher, the feed and each account's state
  and warnings. `503` while the bridge cannot deliver: not authorized, a
  token the server refuses, or a parked batch.
- `GET /metrics`: Prometheus text format, `chatbridge_*`.

## The image

A stage of the repository's one Dockerfile:

```sh
docker build --target chatbridge -t chatbridge .
docker run --rm -it -p 127.0.0.1:8765:8765 -v chatbridge:/var/lib/chatbridge chatbridge \
  authorize --server https://substrate.example.com --print-url --listen 0.0.0.0:8765
docker run --rm -it -v chatbridge:/var/lib/chatbridge chatbridge login whatsapp
docker run -d --name chatbridge -v chatbridge:/var/lib/chatbridge chatbridge
```

It runs `chatbridge run` as uid 65532 with the store in `/var/lib/chatbridge`,
serves port 8080, and carries `LICENSE` and `NOTICE` under
`/usr/share/doc/chatbridge`. In a container the callback binds every address
(`--listen 0.0.0.0:8765`) and the redirect still names `127.0.0.1`, so publish
the port on the host's loopback only.
Its `org.opencontainers.image.revision` label names the commit of this public
repository it was built from, whose `cmd/chatbridge` is the source.

## Development

```sh
mise run chatbridge:build   # bin/chatbridge
mise run chatbridge:test    # go test -race over the module
mise run chatbridge:lint    # golangci-lint, the module files, the import boundary, NOTICE
mise run chatbridge:audit   # govulncheck over the module
mise run chatbridge:notice  # write NOTICE again after a dependency changes
```

The tests touch no network. A fake bridgev2 connector (`internal/fakenet`)
takes a transaction id as the message id the way the WhatsApp connector does
and never deduplicates, so the tests see every network call: the action
crash cases (before the network call, after it accepted, a delayed result, a
disconnect mid-send, two workers racing), every action's echo, the rules,
and the local Matrix side's events over a real `bridgev2.Bridge`. An
`httptest` server stands in for the webhook door, the instance row, the
parked list and the authorization endpoints.

bridgev2 is pre-1.0 and moves monthly; keep `maunium.net/go/mautrix` and
`go.mau.fi/mautrix-whatsapp` on versions that agree, as the connector's own
`go.mod` pins them, and keep whatsmeow current, since WhatsApp refuses an
outdated client.
