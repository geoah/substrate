# Plan: push to a closed device through the publisher's relay

Status: proposed 2026-11-18, revised twice the same day after two rounds of
review, not built. Part of
[#1400](https://github.com/geoah/substrate/issues/1400). This is change 7 of
[the chat experience plan](chat-experience.md#7-push-to-a-closed-device-l),
which points here and keeps nothing of its own about push. A plan, not a
contract: when it is built, the code is the contract and this page says what
was built.

## The short version

Moss answers while the phone is in a pocket and the app is closed. Today
nothing reaches the phone until iOS runs the app's background refresh, when
it chooses. After this plan the phone shows that Moss replied within
seconds, with the start of the reply; a question or an approval waiting on
the person shows the same way; and a long run that fails says so. Reading
the chat on the Mac, or deciding the approval there, lowers the phone's
badge and takes the notification back off it whenever iOS lets the app run.

A self-hosted server cannot send to a published app by itself: the key that
signs an Apple push belongs to the app's publisher. So the publisher runs a
small relay, and the server reaches the phone through it. Five pieces:

1. **The relay**, `substrate-push-relay`, its own image built from this
   repository on the same commits as the server's. It holds the publisher's
   Apple keys, accepts a push only when it is signed for the one sign-in the
   install registered under, and builds every push from what happened,
   which thread, the unread count and an opaque event id. It stores nothing.
2. **The publisher names it.** The relay's address is built into the signed
   app at release and listed in the publisher's authority document beside
   the app; the server reads the same document. The app registers only when
   its build, the document and its server name the same relay
   ([0209](../decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)).
   The server never decides where the app registers.
3. **A device record.** Each install that takes pushes is one
   `substrate.reamde.dev/core/device` record, named by the token it signed
   in with and deleted with it.
4. **The server** waits five seconds after something happens, sends nothing
   if the person has seen it by then, and otherwise posts one signed request
   to the relay for each of their installs that may see it. Best effort:
   nothing is retried or queued past a restart.
5. **The apps.** Substrate Chat on iPhone and iPad registers with the relay
   and writes its device record, and a notification service extension reads
   what the push is about from the person's own server, with the app's own
   token, and shows it.

Four pull requests build it at the same time, because the interfaces between
them are fixed below:

| # | Pull request | Size | Holds |
| --- | --- | --- | --- |
| 1 | The relay | M | the wire with its fixtures and test vectors, the binary, Apple's push service, its image |
| 2 | The server and the device kind | L | `core/device`, the events, who receives them, the per-sign-in signer, the call to the relay, three routes |
| 3a | The app: registering, the badge, a generic alert | M | permission, registration, the alert that opens the chat, the badge, reconciling on open |
| 3b | The app: the notification service extension | M | previews, decisions from the lock screen, taking notifications back |

They merge in that order. 3a and 3b reach people in the same app release,
so no install trades today's local previews for a generic alert. The relay
is deployed before any server is upgraded, and every piece is inert until
the pieces after it exist.

## What holds

The design was checked against these rules:

- **Records are the truth.** Which installs take pushes is data in the
  repository, a record per install, not a table on the relay. How far a
  person has read is the mark of
  [0313](../decisions/0313-how-far-a-person-has-read-a-thread-is-a-record-of-their-own-and-its-position-only-moves-forward.md),
  and pushes read it; nothing keeps a second read state.
- **The relay never sees content.** No title, no text, no sender, no
  subject. The app fetches what to show from the owner's own server.
- **Bound to the sign-in.** A push is signed with a key of the one token the
  install signed in with, carries the install's own registration
  generation, and names its item by an id only that sign-in can resolve, so
  nothing meant for one sign-in reaches another.
- **What a push says, its device's token could read.** Recipients, the
  badge and the actions offered are decided through the same view an API
  request with that token gets, and the actions again when they are shown.
- **One source for each fact.** The relay's address is the publisher's, and
  the app holds its build, the document and its server to one value. Which
  Apple app and which Apple environment an install belongs to live only
  inside its push key. The device's id is its token's id. The badge is
  counted in one place, the server, wherever the server pushes.
- **A link is a reference.** The device names its token and its person as
  references
  ([0044](../decisions/0044-a-reference-is-the-only-link-between-records.md)).
- **The client owns the words.** Nothing on the server or the relay writes a
  sentence a person reads. The fallback text is a key into the app's own
  strings.
- **Best effort, and inert by default.** A push that fails is dropped; the
  records are the truth and the app reads them when it opens. With no relay
  named anywhere, no install can register and nothing is posted.
- **This repository is public.** No relay address, Apple key, key id or team
  id is in this tree. The examples use `push.example.com` and
  `apps.example.com`.

## How a push travels

```text
install                        owner's server               publisher: site, relay      Apple
   |                                  |                                 |                 |
   |<------------------------------------------------------------------ 1. device token --|
   |-- 2. GET its authority document ---------------------------------->|                 |
   |<----------------------------------------------------- push.relay --|                 |
   |-- 3. GET /api/v1/push/key ------>|                                 |                 |
   |<----------- {publicKey, relay} --|                                 |                 |
   | build, document, server agree    |                                 |                 |
   |-- 4. POST /v1/register ------------------------------------------->|                 |
   |<------------------------------------------------------ {pushKey} --|                 |
   |-- 5. PUT core/device/{token} --->|                                 |                 |
   | (pushKey, notifies)              |                                 |                 |
   :                                  :                                 :                 :
   |                                  | Moss replies; 5 s pass;         |                 |
   |                                  | the person has not read it      |                 |
   |                                  |-- 6. POST /v1/push, signed ---->|                 |
   |                                  |                                 |-- 7. HTTP/2 --->|
   |<------------------------------------------------------------- 8. alert, no content --|
   |-- 9. GET push/events/{id} ------>|                                 |                 |
   |-- 10. GET the message ---------->|                                 |                 |
   | (the app's own token)            |                                 |                 |
   shows "Moss: Here is the summary you asked for..."
```

Steps 1 to 5 run when the app opens or refreshes in the background and
something changed. Steps 6 to 10 run once per push; 9 and 10 are the
extension's, and if they cannot run the alert shows the app's own fallback
words.

## 1. The relay

### Where it lives and how it ships

- `internal/pushwire`: the wire both sides share: the request and response
  types, the error codes, the signature, the address construction, and, in
  `testdata/`, the fixtures and test vectors every other pull request tests
  against (section 8).
- `internal/apns`: Apple's push service, written against `net/http` and
  `crypto/ecdsa` alone. Go speaks HTTP/2 to Apple without help, and the JWT
  is one ES256 signature, so about two hundred lines replace a lightly
  maintained dependency.
- `internal/pushrelay`: the relay's HTTP handler, the seal, the limits, the
  replay window.
- `cmd/substrate-push-relay`: the binary, its configuration, its listener,
  and one subcommand, `check`.

It is its own image, not a stage of the server's. The Dockerfile gains a
`push-relay-build` stage and a `push-relay` runtime target, as it has
`slack-socket-build` and `slack-socket`: the pinned Go, the pinned Alpine
with `ca-certificates`, uid 65532, `EXPOSE 8080`, and a `HEALTHCHECK` on
`/healthz`. The image carries no server, no CLI, no console, no Python and
no uv, so a process that faces the internet and holds the publisher's Apple
keys links and ships nothing else; a test holds the binary's dependency list
to that (no `internal/engine`, `internal/api`, `internal/runner`, no
database driver). `latest.yml` builds and publishes
`ghcr.io/geoah/substrate-push-relay` on the same commits and under the same
rules as the server image, `main-<sha12>` and `latest`, with the same version
and commit stamped in. The `ci:image` job builds the target too and boots
it. Like `slack-socket`, it has no release tag of its own; a deployment pins
a `main-<sha12>` build by digest.

### Its API, version 1

Every route carries its version in the path. A relay keeps serving `/v1/`
while any server or app that speaks it may exist; a change of meaning is
`/v2/`. A key a relay does not know in a request body is ignored, so a newer
caller can talk to an older relay; a value it does not know, an `event`, a
`platform` or an `environment`, is refused.

**Register.** The app calls this itself, with no substrate token and
following no redirect, so neither the owner's server nor its history ever
sees Apple's token.

```http
POST /v1/register
Content-Type: application/json

{
  "platform": "apns",
  "token": "5f8a9c...(64 hex characters)",
  "topic": "dev.reamde.substrate.chat",
  "environment": "production",
  "publicKey": "q1x3Lk...(43 characters)",
  "generation": "Hk2m0QZr...(22 characters)"
}
```

| Field | Holds | Bounds |
| --- | --- | --- |
| `platform` | the push service | `apns`; `fcm` is reserved for Android |
| `token` | what Apple calls this install | hex, 32 to 400 characters |
| `topic` | the app's bundle id | one of the relay's topics |
| `environment` | which Apple service issued the token | `production` or `development`, one the relay holds a key for |
| `publicKey` | the public half of the key the server will sign this sign-in's pushes with (below) | base64url, no padding, 32 bytes |
| `generation` | a value the install picks at random for this registration | base64url, no padding, 16 bytes |

```http
200 OK
{"pushKey": "v1.Qm9vbGVhbi..."}
```

**Push.** Only a server calls this.

```http
POST /v1/push
Content-Type: application/json
Substrate-Push-Signature: t=1763459123,s=3nq0Yw...

{"pushKey": "v1.Qm9vbGVhbi...", "event": "reply", "thread": "8f2c41d07a", "badge": 3, "eventId": "dGhpcyBpcyBzZWFsZWQ..."}
```

| Field | Holds | Bounds |
| --- | --- | --- |
| `pushKey` | what register returned | at most 1,024 characters |
| `event` | what happened | `reply`, `failed`, `request` or `read` |
| `thread` | the thread it happened on | a record id (`vocabulary.ValidID`) |
| `badge` | the person's unread count after it | 0 to 999 |
| `eventId` | an id only the owner's server can resolve, to the exact item and version (section 4) | base64url, no padding, 32 to 512 characters |

```http
200 OK
{"status": "sent"}
```

A body over 4 KiB is refused before it is read further. Every refusal is
one envelope with a code from a closed set; `message` is for a log, never
for a person:

```json
{"error": {"code": "gone", "message": "Apple says this install is no longer registered"}}
```

| Status | Code | Means | What the server does |
| --- | --- | --- | --- |
| 400 | `bad_request` | not JSON, a field missing or out of bounds, an unknown event, platform or environment | logs it as a bug |
| 401 | `stale_signature` | `t` is more than 300 s from the relay's clock, either way | logs the skew, keeps the device |
| 401 | `signature_refused` | no signature, or one the public key inside the push key does not verify | deletes the device |
| 403 | `key_refused` | the push key does not open under the relay's seal keys | deletes the device |
| 403 | `key_expired` | the push key was issued more than 90 days ago | deletes the device |
| 403 | `topic_refused` | the topic is not one this relay serves | deletes the device; at register the app stops |
| 403 | `environment_refused` | the relay holds no Apple key for that environment | deletes the device; at register the app stops |
| 409 | `replayed` | this exact signed request was accepted before (below) | logs it as a bug |
| 410 | `gone` | Apple says the install is gone | deletes the device |
| 413 | `bad_request` | the body is over 4 KiB | logs it as a bug |
| 429 | `rate_limited` | a limit below, with `Retry-After` | drops this push |
| 502 | `upstream` | Apple was unreachable, answered 5xx or 429, or refused the publisher's key | drops this push |
| 500 | `internal` | anything else | drops this push |

"Deletes the device" is conditional: the server deletes the record only if
it still holds the push key the failed send carried, so an install that
registered again in the meantime is left alone (section 4).

### The push key

The relay seals what it needs into the key it returns and keeps nothing:

```json
{"p": "apns", "t": "5f8a9c...", "o": "dev.reamde.substrate.chat", "e": "production", "k": "q1x3Lk...", "g": "Hk2m0QZr...", "i": 1763459000}
```

`p` the platform, `t` Apple's token, `o` the topic, `e` the environment,
`k` the public key, `g` the generation, `i` when it was issued. The key is
`v1.` and the base64url of a 24-byte nonce and the XChaCha20-Poly1305
ciphertext of that JSON under the relay's seal key, with
`substrate-push-key-v1` as additional data
(`golang.org/x/crypto/chacha20poly1305`, already a dependency). To everyone
but the relay it is opaque: the server stores it and learns nothing from it,
and a reader of the record cannot recover Apple's token.

A key older than 90 days, by `i`, is refused `key_expired`. An install
renews its key every 30 days when it opens or refreshes in the background
(section 5). One neither opened nor refreshed for 90 days stops receiving
pushes until it next opens, and so does a key held only by a lost phone, a
stale copy of a database or a compromised server.

The relay opens a key with `SUBSTRATE_PUSH_RELAY_SEAL_KEY`, then with
`SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS` when that is set, and always seals
with the first. Rotating is setting the old key as previous and a new one as
current; installs move to the new one as they register again, and the
previous one is dropped after 90 days, when every key it sealed has expired.

### The signature

The server signs every push with the signing key of the token the device
belongs to: an Ed25519 key derived inside the engine, HKDF-SHA256 over the
repository's data-encryption key (the DEK the host's credential key wraps),
salt `substrate push`, info `signing v1 ` and the token's record id. The DEK
never leaves `internal/engine`: what leaves it is a signer for one token
(section 4). One key per sign-in, so a device record cannot be signed for
under another one's key. It is the same on every boot, it stays the same
when the repository is renamed (the derivation reads ids, not names), and it
changes only when the repository's DEK does: a restore without secrets,
which mints a new one. Nothing re-wraps or rotates a DEK
([operations](../operations.md#configuration)), so nothing else changes it.

```text
Substrate-Push-Signature: t=<unix seconds>,s=<base64url of the Ed25519 signature>

signed bytes:  "substrate-push-v1\n" + t + "\n" + lowercase hex of SHA-256(body)
```

The relay accepts `t` within 300 seconds of its clock, before or after, and
verifies the signature with the `k` sealed inside the push key. The path is
not signed, so a relay behind a prefix or a proxy that rewrites it still
verifies. Every body carries a new `eventId`, sealed under a random nonce,
so two pushes never sign the same bytes, even with the same key, event,
thread and badge in the same second.

**Replays.** The relay keeps each accepted signature until `t` + 300
seconds, so one dated ahead of the relay's clock is kept as long as it could
be accepted, at most 200,000 entries, and refuses a repeat as `replayed`.
The set is per replica, so a request replayed to another replica inside the
window is sent again. The deployment in section 9 runs one replica; one that
runs more accepts that a captured request can bring a dismissed alert or an
old badge back once per replica, within ten minutes.

### What it sends to Apple

The relay builds the whole payload from `event`, `thread`, `badge`,
`eventId` and the sealed generation. No field of a request becomes text a
person reads, so not even a valid server can send words through the
publisher's key.

`reply`, and `failed` and `request` with `SUBSTRATE_PUSH_FAILED` and
`SUBSTRATE_PUSH_REQUEST` as their keys:

```json
{
  "aps": {
    "alert": {"loc-key": "SUBSTRATE_PUSH_REPLY"},
    "badge": 3,
    "sound": "default",
    "mutable-content": 1,
    "thread-id": "8f2c41d07a"
  },
  "substrate": {"v": 1, "event": "reply", "thread": "8f2c41d07a", "g": "Hk2m0QZr...", "eventId": "dGhpcyBpcyBzZWFsZWQ..."}
}
```

`read`:

```json
{
  "aps": {"badge": 2, "content-available": 1},
  "substrate": {"v": 1, "event": "read", "thread": "8f2c41d07a", "g": "Hk2m0QZr...", "eventId": "c2VhbGVkIGFnYWlu..."}
}
```

| Header | `reply`, `failed`, `request` | `read` |
| --- | --- | --- |
| `apns-push-type` | `alert` | `alert` (it sets a badge) |
| `apns-priority` | `10` | `5` |
| `apns-expiration` | now + 6 hours | now + 1 hour |
| `apns-collapse-id` | the thread id, when it is 64 bytes or fewer | none |
| `apns-topic` | the sealed topic | the sealed topic |

`loc-key` names a string in the app's own bundle, so the words are the
app's, in the person's language, even when no extension runs. `aps.badge` is
kept on every push, because it is the one part iOS applies when no code of
the app runs: without it, a closed app's badge could not go down. The cost is
stated in section 5.

### Apple's keys and the two environments

Apple's token-auth keys are PKCS#8 P-256 `.p8` files, each with its key id,
under one team id, given at run time and nowhere else. A key may be issued
for one environment or, for older keys, both, so the relay is configured per
environment: a key file and a key id for `production`, and for
`development`, either or both; the same file may serve both. An environment
with no key refuses registrations for it (`environment_refused`), and the
relay starts with at least one.

Each environment has its own JWT and its own pool of HTTP/2 connections:
`production` to `api.push.apple.com`, `development` to
`api.sandbox.push.apple.com`, port 443, a 10-second timeout, connections
kept open between pushes. A JWT has header `{"alg":"ES256","kid":"<key id>"}`
and claims `{"iss":"<team id>","iat":<now>}`. Apple refuses one older than
an hour, and refuses a provider that replaces its token more often than every
20 minutes, so:

- a JWT is reused for 50 minutes, then replaced;
- replacing one is serialized per environment: one mint at a time, and the
  pushes waiting on it take the new one;
- `ExpiredProviderToken` replaces the JWT at once, but only if the failing
  request carried the current one, so many such answers to one JWT cause one
  mint; that push is then sent once more;
- `InvalidProviderToken` means the key itself is wrong: the relay logs it as
  an error and answers `upstream`, and the metric below counts it. It stays
  in service, since a relay out of service never sees a fixed key accepted.

| Apple answers | The relay answers |
| --- | --- |
| 200 | 200 `sent` |
| 410 (`Unregistered`, `ExpiredToken`), 400 `BadDeviceToken`, 400 `DeviceTokenNotForTopic` | 410 `gone` |
| 403 `ExpiredProviderToken` | replaces the JWT, sends once more, then as that answer says |
| 403 `InvalidProviderToken`, 400 `TopicDisallowed`, 400 `BadTopic` | 502 `upstream`, logged as the publisher's configuration |
| 429, 5xx, a timeout, a refused connection | 502 `upstream` |
| anything else | 500 `internal`, logged |

### Abuse

The relay is a public endpoint. What stops it being a way to send pushes to
other people:

- **A push key alone sends nothing.** A push must be signed by the private
  key whose public half was sealed into the push key at register. That key
  is derived inside the owner's server, one per sign-in, and never leaves
  it. Whoever reads a device record, holds an export or watches the relay's
  traffic has push keys and no signing key.
- **Apple's token never leaves the install and the relay.** The app learns
  the relay's address from its own signed build, never from a server alone,
  so a compromised server cannot point it elsewhere to collect tokens. No
  record, history or archive holds a token.
- **Register is open, and harmless while tokens stay secret.** Holding an
  install's Apple token is what lets one register it, and only the install,
  Apple and the relay hold it. Registering one's own install under one's own
  key pushes to nobody else.
- **No caller chooses words.** The payload is built from a closed event set,
  a record id, a number and an opaque id.
- **Topics and environments are lists.** The relay pushes only to the
  bundle ids and the environments its operator configured.
- **Limits**, each a token bucket holding one minute's allowance, in memory,
  per replica:

| Limit | Keyed by | Charged | Default per minute | Variable |
| --- | --- | --- | --- | --- |
| pushes per install | a keyed digest of Apple's token | after the signature verifies | 20 | `SUBSTRATE_PUSH_RELAY_LIMIT_INSTALL` |
| requests per caller | the caller's address | every request to `/v1/` | 600 | `SUBSTRATE_PUSH_RELAY_LIMIT_CALLER` |
| registrations per caller | the caller's address | every register | 30 | `SUBSTRATE_PUSH_RELAY_LIMIT_REGISTER` |
| pushes in all | nothing | after the signature verifies | 12,000 | `SUBSTRATE_PUSH_RELAY_LIMIT_TOTAL` |
| registrations in all | nothing | every register | 3,000 | `SUBSTRATE_PUSH_RELAY_LIMIT_REGISTER_TOTAL` |
| refused requests in all | nothing | every request refused before Apple is called | 3,000 | `SUBSTRATE_PUSH_RELAY_LIMIT_REFUSED_TOTAL` |

The install's bucket is keyed by HMAC-SHA256 of Apple's token under a key
derived from the seal key, so it survives a new registration of the same
install and holds no token in memory. It is charged only once the signature
verifies, so a stranger holding a push key cannot use up an install's
allowance; an unsigned or mis-signed request is charged to its caller's
address and to the refused total alone. The caller address is read the way
the server reads one: the transport peer, or, past proxies listed in
`SUBSTRATE_TRUSTED_PROXIES`, the address they name in `X-Forwarded-For`.
Pull request 1 moves that code out of `internal/api/peer.go` into
`internal/peer`, which both import. The buckets live in a bounded map of
100,000 entries, oldest dropped.

### What it keeps and logs

The relay keeps nothing on disk and no database. In memory it holds the
current JWT per environment, the limit buckets, the replay window and its
connections to Apple.

A log line carries the route, the status and code, the topic, the
environment, the event, Apple's status, reason and `apns-id`, and the push
key's fingerprint (the first 12 hex characters of SHA-256 of the key). It
never carries a push key, Apple's token, a public key, an event id, a thread
id or a badge, at any level. The caller address appears on refusals
(signature, limits, replays, malformed bodies), the lines abuse is read
from, and never on a push that was sent. Fingerprints and times still let a
reader of the logs link the pushes of one registration, so the deployment
keeps relay logs a short time (section 9).

Metrics are Prometheus, on a listener of their own at
`SUBSTRATE_PUSH_RELAY_METRICS_ADDRESS` (unset is none), because the relay's
own port is public by design:

- `substrate_push_relay_requests_total{route, code}`
- `substrate_push_relay_apple_total{environment, status, reason}`
- `substrate_push_relay_apple_seconds{environment}` (a histogram)
- `substrate_push_relay_limited_total{limit}`
- `substrate_push_relay_tokens_minted_total{environment}`

### Health

`GET /healthz` answers 200 while the process serves. `GET /readyz` answers
200 once every configured environment's key parsed and a JWT was minted for
it. Neither calls Apple: an outage at Apple should not restart the relay or
take it out of service. A key Apple refuses shows in the logs, in
`substrate_push_relay_apple_total{reason="InvalidProviderToken"}`, and in
`check`.

### What a leak costs

- **The seal key.** Its holder can open the push keys they have, reading
  Apple's tokens and the public keys, and can seal keys of their own for
  those tokens: with an Apple token and a key they sign with, they can send
  through the relay to that install. What they send is still one of the
  content-free alerts, inside the limits, and the extension shows it only as
  the fallback (its event id resolves nowhere). The answer is a new seal
  key with the leaked one dropped, not kept as previous: every push key is
  then refused once, its device deleted, and each install registers again
  when it next opens or refreshes. **What that does not undo:** Apple
  tokens read out of the leaked keys stay valid until the install is
  reinstalled or restored, and registration is open, so their holder can
  register them again under a key of their own and keep sending fallback
  alerts within the limits. Closing that would need the relay to check
  that a registration comes from a genuine install of the app (Apple's App
  Attest), which this plan does not propose.
- **An Apple key.** Its holder can send anything to any install of the
  team's apps, without the relay. It is revoked and replaced in Apple's
  developer account; nothing else changes.
- **A compromised server.** It holds the DEKs and the push keys of the
  installs registered to it, so it can keep signing content-free pushes to
  them after their sign-ins are revoked, until those keys expire, at most
  90 days after the install last registered. It cannot learn their Apple
  tokens, and an install that registered again since, or signed out, shows
  such a push only as the fallback (section 5).

## 2. The publisher names the relay

### In the signed app

A release builds the relay's origin into the app. One release variable,
`SUBSTRATE_APPLE_PUSH_RELAY`, read by both `apps:release` and `apps:site`:

- `apps:release` stamps it into a build setting, `SUBSTRATE_PUSH_RELAY`,
  which the app reads from its Info.plist (`SubstratePushRelay`). It refuses
  to build an app whose `app.json` says `"push": true` unless the variable
  is set, either to a relay or explicitly to `none`, so a release never
  leaves the build without a relay while the document still names one. A
  build with `none`, or a local build, registers nowhere.
- `apps:site` writes it into the authority document entry of every app whose
  `app.json` says `"push": true`. Unset, it carries forward what the live
  document says for that app, as `name` is carried forward;
  `SUBSTRATE_APPLE_PUSH_RELAY=none` removes it.

### Three that must agree

The app registers only when three say the same relay:

1. its build (`SubstratePushRelay`);
2. the authority document `apps:site` publishes at its site, the origin of
   the app's own client id, at `/.well-known/substrate/authority.json`: the
   entry whose `client_id` is the app's own, its `push.relay`;
3. its server: `GET /api/v1/push/key` answers the relay the server will send
   to, read from the document the authority's `_substrate` record names
   (section 4). The app compares it and never sends anything to it.

The third is there because the server and the app may read two documents:
the server follows the TXT record's `doc=`, which may name another origin
than the site the app reads, and nothing else holds the two equal. All three
compare after the normalization below; the app follows no redirect and
sends no token to the document or the relay. A mismatch, or any of the three
unreadable, means the app does not register now. If it already holds a
registration it stops using it: it deletes its device record, forgets the
registration, and goes back to its local notifications as they are today.
So a publisher that moves its relay moves the document, and installs follow
it as their builds do, rather than registering where no server sends.

### In the authority document

```json
{
  "authority": "substrate.reamde.dev",
  "name": "Substrate",
  "apps": [
    {
      "client_id": "https://apps.example.com/apps/chat/client.json",
      "name": "Substrate Chat",
      "apple": {"appIDs": ["TEAMID.dev.reamde.substrate.chat"]},
      "push": {"relay": "https://push.example.com"}
    }
  ]
}
```

It is per app, not per authority, because a token names one app
(`clientId`) and the document already says what belongs to one app per app
(`apple.appIDs`); because a relay serves the bundle ids its operator lists,
and not every app of a publisher takes pushes (Sync and the console do not);
and because a publisher can move one app to another relay, or turn push off
for one, without touching the others. A publisher that runs one relay for
every app repeats one URL.

`push.relay` is an `https` URL with a host, an optional path, and no user
info, query or fragment, at most 2,048 characters. Everyone builds the two
addresses, and compares relays, the same way (`pushwire.Endpoint`): take
the value, remove one trailing `/` from its path if there is one, and append
`/v1/register` or `/v1/push`.

| `push.relay` | Push goes to |
| --- | --- |
| `https://push.example.com` | `https://push.example.com/v1/push` |
| `https://push.example.com/` | `https://push.example.com/v1/push` |
| `https://example.com/push` | `https://example.com/push/v1/push` |
| `https://example.com/push/` | `https://example.com/push/v1/push` |

### How a server reads it

`AuthorityApp` in `internal/authority/reader.go` gains `Push`. The reader
holds every key it knows to its type and refuses the whole document for a
known key of the wrong type or `null` (`knownJSON` in
`internal/authority/decode.go`), which here would make a typo in `push`
unverify the publisher's every app. So `push` is the one key read
leniently: `knownJSON` passes it through raw, and a value that is not an
object, a `relay` that is not a string, or a relay that is not a valid
`https` URL is dropped, and the app reads as naming no relay. Every other key
stays strict.

The server reads the document through the `authority.Reader` it already
has, from its 10-minute cache: the token's `appAuthority`, then the entry
whose `client_id` is the token's `clientId`, then its `push.relay`. A token
with no `appAuthority` (an unverified app, or no app at all) or with
`appLocal` set has no relay, by 0209's rule. A document a development file
supplies (`SUBSTRATE_INSECURE_AUTHORITY_DOCS`) counts, so a local server can
test against a local relay. A server older than the key ignores it, as the
reader drops keys it does not know; production's server today is such a
server.

## 3. The device record

### The declaration

`kinds/substrate.reamde.dev/core/device.yaml`, in `core` because nothing
about an install is the agent runtime's (Tasks may push later). `core` goes
from version 64 to 65.

```yaml
# one install of an app that takes pushes (the device kind's decision
# record): its id is the id of the token it signed in with, the engine
# refuses any other, and it is deleted with that token.
kind: substrate.reamde.dev/core/kind
metadata:
  id: substrate.reamde.dev/core/device
data:
  authority: substrate.reamde.dev
  package: core
  description: >-
    One install of an app that takes pushes, as signed in with one token:
    the push key its publisher's relay sealed for it and which events its
    person wants pushed there. It is deleted with its token.
  purpose: internal
  ids:
    type: named
  # Bookkeeping, as a trigger run is: the latest state is always kept, and
  # its history of registrations a week (decision record 0283).
  retention: P7D
  names:
    singular: device
  displayTemplate: "{name}"
  properties:
    token:
      type: reference
      kind: substrate.reamde.dev/core/token
      required: true
      onDelete: cascade
      description: the token this install signed in with; filled in with the
        token the write arrives with, and fixed when the record is created
    person:
      type: reference
      kind: substrate.reamde.dev/core/actor
      required: true
      description: the person that token acts for; filled in, and fixed when
        the record is created
    name:
      type: string
      required: true
      max: 100
      description: what the device calls itself, as the app read it
    platform:
      type: enum
      required: true
      values:
        - ios
      description: the system the app runs on
    pushKey:
      type: string
      required: true
      max: 1024
      description: what the publisher's push relay sealed for this install;
        opaque here
    notifies:
      type: enum
      repeated: true
      required: true
      values:
        - replies
        - requests
      description: which events the person wants pushed to this install, at
        least one
    seenAt:
      type: datetime
      managed: true
      description: when the install last wrote this record, stamped by the
        engine
```

| Property | Holds | Points at |
| --- | --- | --- |
| `token` | the sign-in this install holds | one `core/token`, the one whose id is this record's |
| `person` | whose install it is | one `core/actor` of type `person` |
| `name` | "iPhone", "iPad" | text |
| `platform` | `ios`, for iPhone and iPad; `macos` and `android` arrive with those apps | an enum |
| `pushKey` | the relay's sealed key | opaque text |
| `notifies` | `replies` (a reply and a failure), `requests` | a set of at least one |
| `seenAt` | the last registration | a time |

The bundle id, Apple's environment and the registration's generation are not
properties. They are inside the push key, where the relay reads them, so the
record cannot say one thing and the key another. There is no state for a
device that receives nothing: such a device is deleted (below), so no
retired rows accumulate.

### The writes

`POST /api/v1/records` creates under an id the server mints and refuses a
body that names one (`postRecords` in `internal/api/records.go`), so a
device, whose id is chosen, is written with a put at its own address. The
app knows its token's id: the token exchange answers it as `token_id`
(`internal/api/authorize.go`, `tokenResponse`), and the kit keeps it in the
session (`Session.tokenID`).

```http
PUT /api/v1/substrate.reamde.dev/core/device/71ac09be44
Authorization: Bearer <the app's token>
Content-Type: application/json

{
  "properties": {
    "name": "iPhone",
    "platform": "ios",
    "pushKey": "v1.Qm9vbGVhbi...",
    "notifies": ["replies", "requests"]
  }
}
```

The body is the put input (`substrate.PutInput`): `properties`, with the
kind taken from the path; the handler decodes it strictly
(`putResource` in `internal/api/rest.go`). The reply is the record as every
record route answers it, with its id at the top (`"id": "71ac09be44"`).

| Answer | When |
| --- | --- |
| `201` with the record | there was none, or only a deleted one that the sweep had already purged: a fresh record |
| `200` with the record | it existed, or a deleted one not yet purged, which the put restores, and the write merged into it |
| `400 bad_request` | a key the body may not carry, such as `data` or a misspelled `ifVersion` |
| `422 validation` | a property out of bounds, an unknown value, `notifies` missing or empty, a name over 100 characters |
| `403 forbidden` | an agent, a function or a package wrote; a token whose person's roles do not reach `core/device`; an id that is not the request's own token's, or a `token` or `person` that is not the request's |

The app treats `200` and `201` alike. It sends no `ifVersion`: the record is
the install's alone, and a put that repeats lands the same record. Every put
names `notifies`: it is `required`, and a required repeated property with no
values is no value (`emptyValue` in `internal/engine/write.go`, which
`required` reads), so an empty list is refused. A person who wants neither
turns both switches off, and the app deletes its device (section 5).
`token` and `person` are left out of the body; the engine fills them.

The app reads its record with `GET` and deletes it with `DELETE` at the same
address. PR 2's tests run these fixtures through the real handler.

### Who writes it

- **The install, and only its own.** The engine admits a put of
  `core/device` only at the id of the token the request carries, fills
  `token` and `person`, and stamps `seenAt`. A person's token, or an app's
  token acting for a person, writes the device of that token. An agent, a
  function and a package write none.
- **Deleting.** The token itself; any other token of the same person, such
  as a console listing `person` = `me`; and an admin. The engine, as the
  system, deletes one after the relay's answer and in its periodic sweep
  (below).
- **Roles.** The shipped `viewer` role gains `write` over `core/device`, as
  it gained it over the mark
  ([0313](../decisions/0313-how-far-a-person-has-read-a-thread-is-a-record-of-their-own-and-its-position-only-moves-forward.md)),
  so a viewer's phone takes pushes too; the own-device rule narrows it to
  their own.
- **App scopes.** The chat app asks for `full` and reaches the kind. A scoped
  app needs `write:substrate.reamde.dev/core/device`, an ordinary kind scope,
  so the grammar does not change
  ([0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)),
  and the own-device rule holds it to its own record whatever the scope
  says.

### How it goes away

| What happened | What becomes of the device |
| --- | --- |
| The token was deleted: revoked, signed out, signed out everywhere ([0272](../decisions/0272-signing-out-everywhere-ends-every-token-the-person-holds-and-every-stream-ends-with-its-token.md)) | `onDelete: cascade`: the sweep that collects the token deletes it, within five minutes; meanwhile nothing is sent to it, since its token is not live |
| The token expired | the engine's periodic sweep (`RunGC`, every five minutes) deletes every device whose token has expired, whether or not anything was about to be sent |
| The relay answered `gone`, a refusal of the key or the signature, `topic_refused` or `environment_refused` | the server deletes it, if it still holds the key that send carried |
| The person turned both switches off, the app lost permission, signed out, or found the three relays disagree | the app deletes it |

An install that registers again after its device was deleted puts it again:
a fresh record once the sweep has purged the old one, the old one restored
before that.

### Privacy

The record holds a name the system gives the device (an iPhone calls itself
"iPhone" unless the app holds Apple's entitlement for the person's own name,
which it does not ask for), the system, and an opaque key. Whoever may read
`core/device` reads every person's devices, as whoever reads the mark kind
reads every person's marks: a kind is the unit of permission
([0275](../decisions/0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md)).
What they read cannot push to anyone.

## 4. The server

### Which threads, for which event

The modes differ by event, on purpose:

| Event | Modes | Why |
| --- | --- | --- |
| `reply`, `failed` | `chat`, and only a thread with a person (`heldTo.onBehalfOf`) | a `call` returns its answer to its caller, whose person `heldTo` names (`holdersOf` in `internal/engine/heldto.go`), and a function calling an agent opens one (`internal/engine/runner.go`); nobody marks those threads, so every call would push. `manual`, `record`, `schedule` and `webhook` are trigger deliveries (`agentFire` in `internal/engine/agents.go`) and act for nobody |
| `request` | the six conversation modes: `chat`, `manual`, `call`, `record`, `schedule`, `webhook` | an approval a scheduled run or a call waits on is when the phone matters most |
| the badge | `chat` | it counts what Substrate Chat lists by default (below) |

`subagent` and `judge` threads push nothing: an approval or a question a
sub-agent raises names its root thread
([agents](../agents.md#approvals)). A request goes to the thread's person,
or, for a thread that acts for nobody, to the people holding `admin` whose
view reads the thread.

### The events

| Event | When | Its item | To whom |
| --- | --- | --- | --- |
| `reply` | a `chat` thread with a person settles `ok`, `error` or `overbudget`, its `lastMessage` moved, and the message it names is an `assistant` message | that message, at its version | the thread's person, on devices whose `notifies` has `replies` |
| `failed` | a `chat` thread with a person settles `error` or `overbudget` after its turn ran longer than 5 seconds, and no `reply` was sent for that settle | the thread, at that settle's version | the thread's person, on devices whose `notifies` has `replies` |
| `request` | an interaction, an approval or a change request of a thread in the conversation modes becomes pending (`waitingOnPerson` rises) | that record, at that version | the thread's person, or the `admin` people whose view reads it; devices whose `notifies` has `requests` |
| `read` | the person's mark moves forward, clears `markedUnread`, or sets `archived`; or a pending record the server alerted for is decided, wherever (`waitingOnPerson` falls) | what it covers (below) | that person's devices holding an alert it covers, but the one whose token wrote the mark |

A reply needs an `assistant` message because `newestMessage` names the
person's own message when a new chat's first call fails
(`internal/engine/threadmarks.go`): `lastMessage` moving does not by itself
mean Moss replied. A run that errors after its tool calls may leave no
`assistant` message at all; that is the `failed` event's case. A settle that
also raised a request sends the `request`, not a `reply` too. A request is
sent for an archived thread too, since what waits on a person is always
notified
([0322](../decisions/0322-a-person-pins-and-archives-a-thread-on-their-own-mark-and-a-list-is-narrowed-by-the-records-that-point-at-its-rows.md)).

Nothing is pushed for:

- a thread that settles `stopped`: the person stopped it;
- a failure in a turn of 5 seconds or less: the person is still where they
  sent the message;
- a settle the boot's recovery writes for an interrupted run, so a restart
  sends no burst; the app reconciles when it opens.

### Holds and rechecks

Each event waits before it is sent. `reply`, `failed` and `request` are
keyed by their recipient, their thread, their event and their item: one
person reading cannot cancel another's alert, and a second item on the same
thread is its own wait. A second event with the same key restarts its wait.

- **`reply`**: 5 seconds. Then it is dropped if the person's mark reads
  through that message (the mark's `readThrough` is it or a later message,
  by 0313's order) or the mark says `archived`.
- **`failed`**: 5 seconds. Then it is dropped if the person is reading the
  thread: their mark took a position since the turn began, or a client
  acting for them was attached to the thread's event stream when it settled.
  The mark alone cannot say it, since a failure often writes no message and
  a mark write that moves nothing stamps nothing (`prepareThreadMark`), so
  the turn's hub records which person each attached reader acts for.
- **`request`**: 5 seconds. Then it is dropped if the record is no longer
  pending at that version, or, when a settle raised it, if the mark reads
  through the message that settle wrote. A newer version of the same record
  is a new item.
- **`read`**: 2 seconds, keyed by the recipient and the thread alone, so
  every source in that time folds into one `read` that covers all of them.

In front, the app also stays quiet for any push about the chat on its
screen (section 5).

### The event id

Every push names its item by an `eventId`: the item's reference and version,
the event, the thread and when it was issued, sealed with XChaCha20-Poly1305
under a key derived inside the engine from the repository's DEK and the
token's record id (info `events v1 `), under a random nonce. Only the owner's
server opens it, and only for the token it was sealed for. It carries the
exact item, so what the extension shows and the actions it offers are about
that item and no other, even when two cards wait on one thread; and its
random nonce makes every signed body unique (section 1).

```http
GET /api/v1/push/events/dGhpcyBpcyBzZWFsZWQ...
Authorization: Bearer <the app's token>

200 OK
{
  "event": "request",
  "thread": "8f2c41d07a",
  "item": {"kind": "substrate.reamde.dev/llm/approval", "id": "01h2xcejqtf2nbrexx3vqjhp42", "version": 4},
  "actions": true
}
```

| Event | `item` | Also |
| --- | --- | --- |
| `reply` | the `llm/message` | |
| `failed` | the `llm/thread`, at the version that settled | |
| `request` | the `llm/interaction`, `llm/approval` or `core/recordpatchrequest` | `actions` |
| `read` | none | `covers` |

`actions` is decided now, when the id is resolved, through the asking
token's view: true only if it can read the record, the record is still
pending at that version, and it could write its decision. A `read` answers
what it covers:

```json
{
  "event": "read",
  "thread": "8f2c41d07a",
  "covers": {
    "readThrough": {"ref": "substrate.reamde.dev/llm/message/5b90e3c1f2"},
    "archived": false,
    "decided": [{"kind": "substrate.reamde.dev/llm/approval", "id": "01h2xcejqtf2nbrexx3vqjhp42"}]
  }
}
```

A `reply` or `failed` alert of the thread is covered when the mark reads
through its message (for `failed`, when the mark took a position after it,
or the thread ran again), or when `archived` is true; a `request` alert is
covered when its record is in `decided`. A `read` holds at most 16 decided
records; more start another.

The id is answered `404 not_found` when it was sealed for another token, is
older than 24 hours, does not open, or names an item the asking token's view
no longer reads. Under `push_off` it is `403 push_off`.

### Who receives it, and what they may see

For each recipient, in this order, stopping where nothing is left:

1. Their devices that hold a `pushKey`, whose `notifies` has the event's
   class, and whose token is live and not expired. No device left: nothing
   else is done, no authority lookup and no badge.
2. Of those, the devices whose token can read the thread, decided through
   the same view an API request with that token gets: the person's roles as
   they are now and, for an app's token, its grant, with `agents:` covering
   an agent's reads transitively (`personView` in `internal/api/auth.go`,
   `coversAgentReads` in `internal/api/scopedagents.go`). A push never names
   a thread its device could not open.
3. The relay of each device's app (section 2); none, and that device is
   skipped.
4. The badge, counted for each device.
5. The event id, sealed for each device's token.
6. One request to that relay per device.

Pull request 2 exports one function from `internal/api` that builds, from a
token record, the view a request with that token gets. The dispatcher, the
badge route and the event route read only through it, so there is one
definition of what a token may see.

### The badge

One rule, counted by the server: the live `llm/thread` rows the token's view
reads with `mode` `chat` and `status` not `running`, that are unread for the
person, and that the person has not archived unless `waitingOnPerson` is
above 0. Unread is 0313's: `markedUnread` set, or `lastMessage` set and not
the person's `readThrough`; with no mark there is no `readThrough`, so a
thread with a `lastMessage` is unread. It stops counting at 999, the one cap
everywhere: the server's count, the relay's bound and the app's display.

It counts `chat` alone because that is what Substrate Chat lists by default
(`showAutomated` is off in `ChatPreferences`), and a server cannot follow an
install's preference: counting automated runs would raise a number the
person never sees go down. The app adopts the same rule (section 5).

The engine finds the unread candidates in one query; the view keeps those
the token reads, memoizing coverage per agent, so a scoped token costs one
check per agent, not per thread.

```http
GET /api/v1/push/badge
Authorization: Bearer <the app's token>

200 OK
{"badge": 3}
```

It answers the same count for the asking token, so the app shows on screen
exactly what a push sets. Under `push_off` it answers `403 push_off`, and the
app counts for itself as it does today.

### Taking notifications back

The dispatcher remembers, for each device, the alerts it sent: their thread,
event, item and position. A `read` goes to a device only when what it covers
covers at least one of them, and those entries are then dropped. A partial
read, one that stops before the newest alerted message, covers only the
older alerts, so a newer notification is not taken back. An approval decided
on the Mac covers its alert on the phone. The map holds at most 10,000
entries, oldest dropped, and lives in memory: after a server restart no
`read` is sent for what was alerted before it, and the app reconciles when
it next comes to the front.

### Where it hooks in

The engine finds events in the transactions that make them, through the
`afterCommit` hooks a transaction already runs after it commits
(`internal/engine/dataset.go`):

- the thread write that settles a thread, where `stampLastMessage` runs
  (`internal/engine/threadmarks.go`), for every path that settles one, with
  how long the turn ran and whether a reader acting for the person was
  attached; the boot's recovery marks its settles and they are skipped;
- `countWaitingOnPerson`, when it writes a higher count, with the record
  that raised it, and when it writes a lower one, with the record that was
  decided;
- `prepareThreadMark`, when a write moves the position forward, clears
  `markedUnread` or sets `archived`, with the token that wrote it.

The engine hands each event to a sink it is given at start (an interface in
`internal/substrate`); it sends nothing itself. The sink is
`internal/pushdispatch`: the holds, the rechecks, the recipients through the
API's view, the badge, the alerted map, and the deletes after the relay's
answer, which it asks the engine to make as the system.

The keys stay in the engine. `internal/substrate` gains a signer the engine
hands out for one token, which `internal/push` and the routes use and never
see inside:

```go
// PushSigner signs and seals for one token's pushes. The engine derives it
// from the repository's DEK and the token's id; the DEK never leaves it.
type PushSigner interface {
	PublicKey() ed25519.PublicKey
	Sign(t time.Time, body []byte) string // the Substrate-Push-Signature value
	SealEvent(e PushEvent) (string, error)
	OpenEvent(id string) (PushEvent, error) // ErrNotFound when it does not open, is another token's, or is past 24 hours
}
```

`internal/push` makes the call with a signer it is given. `cmd/substrated`
wires the engine, the dispatcher and `internal/push`.

The dispatcher is bounded: at most 1,024 holds waiting, the oldest dropped
first and counted; at most four requests to relays in flight. A database has
one writer process
([0325](../decisions/0325-a-database-has-one-writer-process-and-a-retaken-lease-checks-who-else-started.md)),
so each event is found once and each push is sent once. A restart drops what
was waiting; the app reads the records when it opens.

### The call out

This is the first time the server calls an outside service on its own
initiative rather than one a person configured (a provider row, a
connector) or asked for. `internal/push` makes the call and nothing else
does:

- only to the relay the token's verified app's document names, at the
  address section 2 builds;
- through `egress.Transport()`, which dials public addresses only unless
  the operator's `SUBSTRATE_EGRESS_ALLOW` lists one, following no redirect
  (`egress.CredentialClient`);
- a 5-second timeout, at most 4 KiB sent and 4 KiB read back;
- `User-Agent: substrate/<version>`;
- no retry.

### After the relay answers

Each answer is classified by the table in section 1. For the answers that
delete the device, the delete runs in one transaction that reads the record
first and deletes it only if its `pushKey` is still the one the send carried,
so an install that registered again while the send was in flight keeps its
new registration. Every other answer is counted and dropped.

### The routes the app asks

```http
GET /api/v1/push/key
Authorization: Bearer <the app's token>

200 OK
{"publicKey": "q1x3Lk...", "relay": "https://push.example.com"}
```

`publicKey` is the asking token's own. `relay` is the relay the server will
send this token's pushes to, which the app compares with its build and the
document (section 2) and never sends to.

| Answer | When |
| --- | --- |
| 200 | the token's app is verified, not local, and its document names a relay for it |
| 403 `forbidden`, reason `no_relay` | the token is no app's (a password sign-in's, a console's), its app is unverified or local, or its document names no relay for it |
| 403 `forbidden`, reason `push_off` | the operator turned push off |
| 503 `unavailable`, `Retry-After: 60` | the authority document could not be read just now |

`GET /api/v1/push/badge` and `GET /api/v1/push/events/{eventId}` are above.
The three replies are named structs in `internal/substrate` (`PushKey`,
`PushBadge`, `PushEventInfo`), held by the wire golden file. `no_relay` and
`push_off` are two new reasons, added by CLAUDE.md's steps for a reason, and
the `errors` feature goes from revision 7 to 8 for them.

### The operator's switch, the feature, and copies

`SUBSTRATE_PUSH` (default `true`). Set to `false`, the engine hands out no
events, the three routes answer `push_off`, and discovery does not list the
feature.

Discovery lists a new feature, `push`, revision 1, experimental, holding the
three routes, listed only while push is on (`listed` in `featureTable`). An
app reads it before it registers and never probes.

| Feature, revision | Carries |
| --- | --- |
| `push` 1 | `GET /api/v1/push/key`, `GET /api/v1/push/badge`, `GET /api/v1/push/events/{eventId}`, the `core/device` kind, and pushes for `reply`, `failed`, `request` and `read` |
| `errors` 8 | the reasons `no_relay` and `push_off` |

The server is inert until every link exists: an operator who left push on, a
publisher whose document names a relay, an app that registered with it, and
a device record that holds its key. Remove any one and nothing is posted. A
device record written by hand, with a key, for a token whose app names no
relay, is never sent to.

**A copy pushes to real phones.** A restored copy of a repository, a dry run
against a copy of production or a staging server holds the same device
records and, with the same DEK, derives the same signing keys, so its pushes
reach the owner's phone like the original's. Every such copy runs with
`SUBSTRATE_PUSH=false`, which `docs/operations.md` says where it describes
restores and copies. The `dev*` tasks export `SUBSTRATE_PUSH=false`; a
developer testing push sets `SUBSTRATE_DEV_PUSH=true`, which the dev script
turns into `SUBSTRATE_PUSH=true`.

### Logs and metrics

The server logs each delete after a relay's answer at info (the device's id
and the code), each relay failure at warn (the relay's host, the status, the
code), and each send at debug, never with a push key, an event id, a thread
id or a badge.

- `substrate_push_total{event, outcome}`, where `outcome` is one of `sent`,
  `seen`, `archived`, `no_device`, `no_relay`, `unreadable`, `deleted`,
  `limited`, `failed`, `dropped`
- `substrate_push_relay_seconds` (a histogram)

Neither carries a repository, so `SUBSTRATE_METRICS_ANONYMIZE` has nothing
to hash.

## 5. The apps

Substrate Chat on iPhone and iPad first. The Mac follows once a real
`development` push has reached a development-signed Mac (section 6). The
pieces that are not the chat's own go in the kit, so Tasks can take pushes
later. 3a and 3b are separate pull requests and one app release: an install
moves from today's local notifications, with previews, straight to pushes
with previews.

### Which sign-ins can take pushes

Only an app sign-in, through the authorization flow, gets push: the server
reads the relay from the token's verified app. The password door
(`ChatHost.signIn(_:name:repository:password:totp:)`) mints a plain login
token with no client id, so `GET /api/v1/push/key` answers `no_relay`. The
settings then say that this sign-in does not notify a closed app, and offer
moving to app access where the server allows it (`canMoveToAppAccess`).

### Registering (pull request 3a)

The app's two existing switches decide what it asks for: Replies
(`notifyReplies`, a reply or a failure) and Requests (`notifyAttention`).
Nothing below runs while both are off, and turning both off deletes the
device; otherwise `notifies` names the ones that are on.

1. The app reads discovery. Without `push` 1 it keeps its local
   notifications as today and stops here.
2. It asks permission after the first reply settles, with a sentence saying
   what will be notified, as it does today. It keeps a device only while
   permission is granted, and deletes its own when it finds permission was
   taken away.
3. It registers for remote notifications and gets Apple's token.
4. It reads its publisher's document (section 2) and asks
   `GET /api/v1/push/key`. On `no_relay` or `push_off` it stops and deletes
   its device if it has one.
5. It checks that its build, the document and the server's answer name the
   same relay (section 2). If not, it stops, and gives up a registration it
   holds.
6. It finds its Apple environment (below).
7. It picks a new generation, 16 random bytes, and posts `/v1/register` to
   the relay: no substrate token, no redirect followed.
8. It puts its device record (section 3).

It keeps, in the Keychain beside the session, the current generation, the
relay, the public key, Apple's token, the environment and when it last
registered. A new generation becomes current only when the server answers
the put with `200` or `201`; until then the old one stays current, and while
the new one is pending the app accepts a push carrying either. So a put
that fails (offline, in a background refresh) leaves pushes under the old
registration working rather than falling back.

It runs 3 to 8 again, at launch and at each background refresh, when any of
these changed: Apple's token, the relay, the public key, the switches, the
record is gone, or 30 days passed since the last registration. Thirty days
keeps every active install well inside the relay's 90-day expiry and moves
installs to a new seal key.

**Signing out** deletes the device first, then revokes the token; if the
delete fails, the cascade deletes the device with the token. It also clears
the current generation, so every push already delivered or still queued
for that sign-in no longer matches: it opens nothing, fetches nothing and
offers no action.

### The environment

The environment is that of the exported build's own signature, read at run
time, never a build setting: an export re-signs the app and replaces the
entitlement.

- In a simulator it is always `development`
  (`#if targetEnvironment(simulator)`): a simulator build embeds no profile,
  and reading "no profile" as `production` would register a sandbox token
  as a production one, which Apple then refuses as `BadDeviceToken`.
- On a device the app reads its embedded profile
  (`embedded.mobileprovision`) and the `aps-environment` entitlement in it.
  A build with no embedded profile, which is how TestFlight and the App
  Store deliver one, is `production`.

| Channel | How it is exported today | Environment |
| --- | --- | --- |
| A simulator | no export | `development` |
| Xcode run on a device | development signing | `development` |
| The iPhone feed | `debugging` unless `SUBSTRATE_APPLE_EXPORT_IOS` says otherwise (`.mise/apple.sh`) | `development`; an export as `release-testing` would make it `production` |
| TestFlight, App Store | `app-store` | `production` |

So the relay serves the feed's builds through `development` and TestFlight's
through `production`, and holds a key for each. Pull request 3a's acceptance
reads the environment each channel's build reports in its diagnostics and
checks it against `codesign -d --entitlements` of the exported app.

### The badge and reconciling (pull request 3a)

- **One count.** Where the server lists `push`, the app shows the server's
  number: it reads `GET /api/v1/push/badge` whenever it would recount, and
  sets the badge from that alone. It no longer sets it from its own
  `unreadCount` (`ChatApp.swift` asks for `.badge(state.unreadCount)` on every
  change today). Where the server does not list `push`, it counts for itself
  by the same rule: `unreadCount` counts `chat` rows only, even with
  `showAutomated` on.
- **Requests in any mode.** The list's waiting read covers all six
  conversation modes whatever `showAutomated` says, so a person who holds
  `admin` can open the scheduled run a request push names.
- **Reconciling.** Each time the app comes to the front it reads the badge
  and removes delivered notifications that the marks and the decided records
  say are seen. This, not the `read` push, is what keeps a phone right: iOS
  does not run an app the person force-quit, and a server that restarted
  sends no `read` for what it alerted before.
- **No doubles.** While the install holds a registration, the app posts no
  local reply, failure or request notifications of its own, in the
  background refresh or anywhere else. The refresh still reconciles marks
  and the badge. In front, the delegate keeps quiet any push about the chat
  on screen, as it does a local one.
- **What the badge cannot guard.** `aps.badge` is applied by iOS before any
  code of the app runs, so a push already queued at Apple for a sign-in that
  has since signed out or switched accounts can set a stale number until the
  app next opens. The extension (3b) drops the badge from a push whose
  generation does not match; a push that reaches no extension, a `read` among
  them, cannot be held to it.

### The generic alert (pull request 3a)

Without an extension, a push shows as Apple delivers it: the app's own words
for `SUBSTRATE_PUSH_REPLY`, `SUBSTRATE_PUSH_FAILED` or
`SUBSTRATE_PUSH_REQUEST`, from its strings, which a test holds present, and
the badge. Tapping it: the local notifications put `thread` at the top of
their `userInfo` (`ChatNotifications.threadKey`); a push carries it at
`substrate.thread`. The kit's tap decoding reads the top-level key, then
`substrate.thread`, and opens that chat only if `substrate.g` is the current
generation or the pending one; otherwise it opens the chat list.

The diagnostics sheet says whether the install is registered, with which
environment and since when; a development build also shows Apple's token,
so the relay's operator can send it one test push (section 9).

### The notification service extension (pull request 3b)

A new target, `SubstrateChatNotifications`, bundle id
`dev.reamde.substrate.chat.notifications`, listed in `app.json`'s
`apple.extensions`. For `reply`, `failed` and `request` iOS runs it before
the alert shows.

- **The session.** It reads the session the app already keeps in the
  Keychain (`SessionStore.chat`), readable after the first unlock. The app
  and the extension share the access group
  `$(AppIdentifierPrefix)dev.reamde.substrate.chat`, which is the app's own
  identifier, so the item every earlier build wrote is already in it, as
  Substrate Sync's share extension reads its app's session.
- **The generation first.** If `substrate.g` is neither the current
  generation nor the pending one, the push was meant for an earlier
  registration of this install (another server, another sign-in, or one
  signed out): it fetches nothing, leaves the fallback words, offers no
  action, and drops the badge it carried.
- **The item, exactly.** It resolves `substrate.eventId` with
  `GET /api/v1/push/events/{eventId}`, then reads that item with the app's
  token and checks its version. For a reply it shows the start of that
  message; for a failure, the sentence the app has for the thread's `cause`;
  for a request, that card, with the record's version in its `userInfo`. It
  sets `threadIdentifier` and the category with the same code the local
  notifications use (`ChatNotifications`), so a push and a local one look
  and act the same. When two cards wait on one thread, each push shows its
  own.
- **Actions only for that item.** An approval uses the static
  `chat.approval` category, with Approve and Decline, only when the
  resolution answered `actions: true` and the record is still pending at the
  version it named. A question uses the open-only category: a question's
  own options live in a category registered at run time
  (`chat.question.<id>`), and nothing shows that one registered from an
  extension applies before its alert does. A device check may lift this
  later.
- **Time.** One deadline for all its reads, 20 seconds from when it starts,
  each read getting what is left, and `serviceExtensionTimeWillExpire`
  delivering what it has. On any failure the alert shows with the fallback
  words. Apple holds an extension to a memory limit it does not publish,
  about 24 MB in practice, so the extension links the kit's API and the
  notification builder and never the views.
- **A server at home.** A server reachable only on the person's home network
  or a VPN cannot be reached from the extension away from it, and an
  extension may not be granted local network access. For such servers the
  fallback alert is the normal case, and the app says so in its settings.

**Deciding from the lock screen.** Approve and Decline require the device to
be unlocked (`authenticationRequired`), on pushed and local notifications
alike. Before it writes, the app checks the notification's generation
against the current one, and refuses one from another sign-in; then it
writes over the version the notification showed, as the local ones do
today, so a card that changed since is refused by the server, never decided
unseen. Reply with text and a question's options stay as they are.

**Taking notifications back.** The app gains the `remote-notification`
background mode beside `fetch`. When iOS runs it for a `read` push whose
generation matches, it resolves the push's event id and removes exactly the
delivered notifications it covers, then sets the badge; a `read` whose
generation does not match removes nothing. When iOS does not run it, the
badge in the push still applies, and the next time the app comes to the
front reconciles the rest.

### Entitlements, the App ID and the rules that hold them

- The app gains `aps-environment`, `development` in its entitlements files;
  an export for TestFlight or the App Store signs with a distribution
  profile, which sets `production`. Only the site variant of the
  entitlements carries it, because a local build has a loopback client id,
  which 0209 never verifies, and so has no relay.
- The app and the extension gain the shared `keychain-access-groups` entry
  (pull request 3b).
- The App ID `dev.reamde.substrate.chat` needs the Push Notifications
  capability. `appstoreconnect.swift profiles` turns it on through the API
  for every app whose `app.json` says `"push": true`, before it makes the App
  Store profile, so a release never depends on a development build having
  run first. The extension's App ID needs no capability of its own.
- `lint:apps` holds `"push": true` to the `aps-environment` entitlement
  (pull request 3a), and to the notification service extension and the
  `remote-notification` background mode (pull request 3b).

## 6. The Mac, Android and the console

**The Mac** comes after iPhone and iPad, once a real `development` push has
reached a development-signed Mac, which is how the Mac app is released
today. It adds `macos` to the device kind's platforms and
`com.apple.developer.aps-environment` to the Mac entitlements, reads its
environment from `Contents/embedded.provisionprofile`, and moves the
session into the data protection keychain, where an access group applies on
the Mac: the first launch of that build reads the item where it was, writes
it there and deletes the old one. A rollback to an older build then finds no
session and signs the person out once.

**Android** is a follow-up. Substrate Chat has no Android build today. When
it has one, push is Firebase Cloud Messaging through the same relay: the
relay gains the `fcm` platform, configured with the publisher's Firebase
service account file, and sends a high-priority data message carrying the
same values; the app's messaging service resolves the event and posts the
notification itself, since Android has no extension step. The device kind
gains `android`; the server changes nothing but accepting it. A relay for
installs without Google's services (UnifiedPush) is not proposed.

**The console** can use Web Push, which needs no publisher key and no relay:
the server would send to the browser's push service with keys of its own.
That is a follow-up with its own properties on the device record. A console
page listing a person's devices, with a button to remove one, is a small
follow-up too.

**A Live Activity** for a running turn needs Apple's push-to-start tokens and
a push type of its own on the relay. It follows this.

## 7. Decision records

Three, numbered when each pull request is opened, after `git fetch --prune`
and `mise run decisions:check`:

| Record | Pull request | Says |
| --- | --- | --- |
| A publisher's push relay seals each install's address to one sign-in's key, and builds every push from an event, a thread, a count and an opaque id | 1 | its own image; the sealed key, its generation and its expiry; the signature and the replay window; no content and no state; the limits; the versioned API; what a dropped seal key leaves |
| An install that takes pushes is a device record named by its token, and dies with it | 2 | the kind, the id, the put, who writes it, the cascade, the sweep of expired tokens, the deletes after the relay's answer, `viewer`'s rule |
| A server calls out on its own initiative only to push, only through the relay its app's verified publisher names | 2 | `push.relay` per app and read leniently; build, document and server agreeing; the events and their modes; the holds; the event id; the recipients and the badge through the API's view; the signer that keeps the DEK in the engine; best effort; the egress rules; the operator's switch and copies; it adds to 0209 |

The apps' choices (the extension, the Keychain group, the environment from
the signature, the generation, unlocking to decide) are commits.

## 8. The pull requests

### What they share, fixed before they start

| Interface | Between | Fixed in |
| --- | --- | --- |
| `POST /v1/register` and `POST /v1/push`: bodies, statuses, codes | relay and app; relay and server | section 1 |
| `Substrate-Push-Signature`, the signed bytes and the replay window | relay and server | section 1 |
| The Apple payload, the `loc-key` names, `substrate.*`, `thread-id`, the collapse id | relay and app | section 1 |
| `apps[].push.relay`, its leniency, the address construction and comparison | `apps:site`, the server and the app | section 2 |
| `SUBSTRATE_APPLE_PUSH_RELAY`, `SubstratePushRelay`, `"push": true` in `app.json` | apps tooling and the app | section 2 |
| The `core/device` declaration, its put and its answers | server and app | section 3 |
| `GET /api/v1/push/key`, `/badge`, `/events/{eventId}`, `no_relay`, `push_off`, `push` 1, `errors` 8 | server and app | section 4 |
| `PushSigner` | engine, `internal/push` and the routes | section 4 |

The first three rows are executable before any of the four is written.
Pull request 1 lands `internal/pushwire` with `testdata/`: a fixture of
every request, response and error envelope above; the exact Apple payload of
each event; two `request` pushes on one thread, for two cards, with their
event ids; and signature vectors (an Ed25519 seed, `t`, a body and the
header it signs to, and refusals: stale either way, another key, a changed
body, a repeat). Pull request 2 imports them and adds the resolutions of
those event ids, one answering `actions: true` and the other `false`, and
the device put and its answers. Pull requests 3a and 3b copy the ones they
read into the kit's test resources, and a Go test in `internal/pushwire`
holds each copy byte-equal to its original.

```go
package pushwire

const (
	PathRegister    = "/v1/register"
	PathPush        = "/v1/push"
	SignatureHeader = "Substrate-Push-Signature"
	SignatureWindow = 300 * time.Second
	KeyLifetime     = 90 * 24 * time.Hour
	MaxBody         = 4 << 10
	MaxPushKey      = 1024
	MaxEventID      = 512
	MaxBadge        = 999
)

type RegisterRequest struct {
	Platform    string `json:"platform"`
	Token       string `json:"token"`
	Topic       string `json:"topic"`
	Environment string `json:"environment"`
	PublicKey   string `json:"publicKey"`
	Generation  string `json:"generation"`
}

type RegisterResponse struct {
	PushKey string `json:"pushKey"`
}

type PushRequest struct {
	PushKey string `json:"pushKey"`
	Event   string `json:"event"` // reply, failed, request, read
	Thread  string `json:"thread"`
	Badge   int    `json:"badge"`
	EventID string `json:"eventId"`
}

type PushResponse struct {
	Status string `json:"status"`
}

type ErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Sign returns the header value for body at t; Verify checks one against pub.
func Sign(key ed25519.PrivateKey, t time.Time, body []byte) string
func Verify(header string, pub ed25519.PublicKey, now time.Time, body []byte) error // ErrStale, ErrRefused

// Endpoint builds the address of path under relay (section 2); two relays
// are the same when Endpoint gives the same address for both.
func Endpoint(relay, path string) (string, error)
```

### Pull request 1: the relay (M)

`internal/pushwire` with its fixtures and vectors, `internal/apns`,
`internal/pushrelay`, `cmd/substrate-push-relay`, the move of the client
address code to `internal/peer`, the Dockerfile's `push-relay-build` stage
and `push-relay` target, `latest.yml`, `ci:image`, a "Running the push
relay" section in `docs/operations.md`, and its decision record. It lands
first.

Tests:

- `internal/pushwire`: the vectors sign and verify; every fixture
  round-trips through the types; `Endpoint` for each row of section 2's
  table and a refusal of a query, user info and `http`.
- `internal/apns`: the JWT's header, claims and signature, verified with the
  public half of a key the test generates; reuse under 50 minutes and a new
  one past it, on a fake clock; one mint for many concurrent
  `ExpiredProviderToken` answers to one JWT, and none for an answer to an
  older JWT; each environment its own key, JWT and host; every row of the
  answer table, against an `httptest` HTTP/2 server.
- `internal/pushrelay`: the seal opens, a wrong key and a changed byte do
  not, the previous key opens and never seals; a key past 90 days is
  `key_expired`; register's bounds, the topic list and an environment with no
  key; the signature valid, stale before and after, from another key, over
  another body, and repeated; a signature dated 300 s ahead still refused as
  a repeat 590 s later; two pushes alike but for their event ids both sent;
  the exact Apple payload of each event against the fixtures; each limit on a
  fake clock, the install's bucket untouched by unsigned requests and kept
  across a new registration of the same install; the 4 KiB bound; unknown
  routes and methods; `/healthz` and `/readyz`; and that no log line at any
  level holds a push key, Apple's token, a public key, an event id, a thread
  id or a badge, read from a captured handler.
- `cmd/substrate-push-relay`: it refuses to start without a team, an
  environment's key and id, a topic or a seal key, and on a public bind
  without `SUBSTRATE_INSECURE_ALLOW_CLEARTEXT`; `check` against a fake
  Apple, with and without `--device-token`.
- Its dependency list holds none of `internal/engine`, `internal/api`,
  `internal/runner` or a database driver.
- `ci:image` builds the `push-relay` target, starts it with keys the job
  generates (never an Apple key), and probes `/healthz` and `/readyz`.

### Pull request 2: the server and the device kind (L)

Stacked on 1: it imports `internal/pushwire` and its fixtures, and touches
`internal/api` only after 1's move of the client address code.
`kinds/substrate.reamde.dev/core/device.yaml` and `core` at 65; the device's
admission and its sweep in `RunGC`; `viewer`'s rule; the engine's events,
sink and `PushSigner`; the turn hub's record of whom each reader acts for;
`internal/pushdispatch`; `internal/push`; the exported token view in
`internal/api`; the lenient `push` key in `internal/authority`; the three
routes, the feature, the two reasons, `errors` 8 and the wire structs;
`SUBSTRATE_PUSH` and the dev tasks' default; `docs/agents.md` (a Push section
beside Read state), `docs/api.md` (the routes, the feature table, the
reasons), `docs/operations.md` (`SUBSTRATE_PUSH`, copies, what the server
sends where); its two decision records.

Tests, run with `-run`, never the whole suite:

- the device's put, through the real handler with the fixtures: `201`, then
  `200`, at the token's own id; another token's id `403`; `POST
  /api/v1/records` with an id `422`; a body with `data` `400`; `notifies`
  missing or `[]` `422`; an agent `403`; a viewer writes their own; a
  person's other token and an admin delete it; `token` and `person` filled
  and fixed; a put after the purge `201`;
- the sweeps: deleting a token deletes its device at the next sweep, and
  nothing is sent to it in between; an expired token's device is deleted by
  `RunGC` with no event in flight;
- the events, on a fake clock and a fake relay: a reply after 5 s; a mark
  that catches up first stops it; archived stops a reply and not a request;
  a settle that raised a request sends one `request`; two cards on one
  thread send two pushes with different event ids, and their resolutions
  answer different `actions` for tokens with different reach; a pending
  record answered within 5 s sends nothing, and a newer version of it is its
  own item; two people on one thread, one reading, the other still alerted;
  a partial read covers only older alerts; an approval decided on another
  device sends a `read` covering its alert, never to the writer's device; a
  new chat whose first call failed sends no `reply`, and a `failed` only if
  its turn ran past 5 s; a `failed` dropped when a reader of the person was
  attached; `stopped`, recovery and `subagent` send nothing; a `call` and a
  `manual` thread send no reply but do send their requests; a thread acting
  for nobody sends its requests to admins whose view reads it;
- recipients through the view: a scoped token whose `agents:` scope does not
  cover the agent's reads is not sent to; a person whose role was taken away
  is not sent to; with no eligible device the authority reader and the badge
  are not called;
- the badge: `chat` only, `running` not counted, no mark with a
  `lastMessage` counted, archived not counted unless something waits, 999 at
  most, the same for the route as for a push;
- the event route: an id resolves for its own token only, `404` for another
  token's, for one past 24 hours and for one whose item the view no longer
  reads; `actions` false once the record is decided;
- inert: no relay, an unverified app, a local app, a password sign-in and
  `SUBSTRATE_PUSH=false` send nothing;
- after the relay: `gone` and each refusal delete the device; a device that
  registered again while the send was in flight is kept; `stale_signature`,
  `replayed`, 429 and 502 delete nothing;
- `internal/push`: the request is the fixture's JSON with a signature
  `pushwire.Verify` accepts, signed through a `PushSigner`; end to end through
  `pushrelay`'s handler to a fake Apple; a private address and a redirect
  are refused; the 4 KiB bounds; and the signer is stable, differs per
  token, and survives a rename;
- `internal/authority`: `push.relay` parsed; `"push": "x"`, `"push": null`,
  `{"relay": 1}`, `{"relay": "http://..."}` and a relay with a query each
  dropped with the app still verified, and every other key still strict;
- the API, on the real engine: the routes' answers, with an authority
  document from a test file; the feature listed and not; the features, error
  reasons and wire golden files regenerated with their `-update` flags;
- `mise run kinds:check` and the retention golden.

### Pull request 3a: the app registers, the badge, a generic alert (M)

The kit's push pieces (a `PushRegistration` the device layer implements,
its Apple implementation, the relay client that sends no token and follows
no redirect, the three-way relay check, the environment reader, the
generation with its pending state), the chat app's registration gated on its
switches, diagnostics, the tap decoding, the server's badge and the chat-only
count, the waiting read over all conversation modes, reconciling, sign-out's
order, the strings, `"push": true` in `apps/chat/app.json`,
`SUBSTRATE_APPLE_PUSH_RELAY` in `apps:release` (with its refusal) and
`apps:site`, the capability in `appstoreconnect.swift`, the `aps-environment`
entitlement and its `lint:apps` rule, `apps/README.md` and
`apps/chat/DESIGN.md` (push leaves the non-goals).

Tests:

- the kit, in `swift test`: when to register again (each input changed, a
  deleted record, 30 days, a switch); nothing registered with both switches
  off; the relay client's JSON against the fixtures; a redirect from the
  relay refused; the three-way check (all agree, each one differing, one
  unreadable, a relay with and without a trailing `/`), and a held
  registration given up on a mismatch; the generation kept until the put
  answers, either accepted while pending, and the old one kept when the put
  fails; the environment from profile fixtures, and `development` in a
  simulator;
- ChatKit: tap decoding with the current, the pending and another generation;
  no local reply, failure or request notification while a registration is
  held; the badge from the server where `push` is listed and the chat-only
  count where it is not; the waiting read over all six modes; reconciling on
  coming to the front; sign-out deleting the device and clearing the
  generation;
- the strings hold `SUBSTRATE_PUSH_REPLY`, `SUBSTRATE_PUSH_FAILED` and
  `SUBSTRATE_PUSH_REQUEST`;
- `apps:release` refuses a `"push": true` app without the variable;
  `apps:site` and `apps:release` in a dry run: the relay written and stamped
  when the variable is set, carried forward in the document when it is not,
  removed by `none`;
- the acceptance, by hand: a real `development` push through a real relay to
  an iPhone, its alert shown with the fallback words and opening the chat;
  the environment each channel's build reports, checked against its
  signature.

### Pull request 3b: the notification service extension (M)

The extension target, the shared Keychain group, the event resolution, the
content for a reply, a failure and a card, the generation check on showing,
on acting and on taking back, the deadline, unlocking to decide, the
`remote-notification` background mode and `read` handling, and the rest of
the `lint:apps` rule.

Tests:

- ChatKit: the content for a reply, a failure and each card, built from the
  fixtures, is the same for a push as for a local notification; the two-card
  fixture shows each card with the actions its own resolution allows; a
  question's push uses the open-only category; a mismatched generation
  fetches nothing and drops the badge; an old delivered approval's action
  after switching accounts is refused; a `read` removes exactly what its
  resolution covers, and one with a mismatched generation removes nothing, on
  a fake `Notifications`;
- `lint:apps`: `"push": true` without the extension or the background mode
  fails;
- on a simulator, `xcrun simctl push` with each fixture payload, against a
  development server, and the fallback with the server stopped;
- the acceptance, on a real iPhone through a real relay, since a simulator's
  injected push is not what Apple delivers: the `loc-key` alert rewritten by
  the extension; the session read through the shared group by a build
  upgraded from one before it; a locked device after its first unlock; the
  fallback when the server is unreachable; Approve asking for the unlock; a
  `read` taking a notification back while the app is in the background.

### Order

All four are built now. They merge in the order 1, 2, 3a, 3b, and 3a and 3b
reach people in one app release. 3a may merge before 2: it registers only
where discovery lists `push`, so against today's servers it changes nothing.

## 9. Deploying the relay alone first

The relay goes out before any server is upgraded, from the first `main`
build that holds pull request 1, pinned by digest. Nothing calls it until a
server with pull request 2 and an app with pull requests 3a and 3b exist, so
it can run for weeks answering only its health checks. One replica (section
1, replays).

**What it needs:**

| Variable | Holds | Example |
| --- | --- | --- |
| `SUBSTRATE_PUSH_RELAY_APNS_TEAM_ID` | the team id | |
| `SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_FILE`, `..._PRODUCTION_KEY_ID` | the `.p8` for `production` and its id, readable by uid 65532 | `/keys/production.p8` |
| `SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_FILE`, `..._DEVELOPMENT_KEY_ID` | the same for `development`; the same file where one key serves both | `/keys/development.p8` |
| `SUBSTRATE_PUSH_RELAY_APNS_TOPICS` | the bundle ids it pushes to, comma separated | `dev.reamde.substrate.chat` |
| `SUBSTRATE_PUSH_RELAY_SEAL_KEY` | base64 of 32 random bytes (`openssl rand -base64 32`) | |
| `SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS` | the seal key before a rotation, optional | |
| `SUBSTRATE_PUSH_RELAY_METRICS_ADDRESS` | where metrics listen, optional | `:9090` |
| `SUBSTRATE_PUSH_RELAY_LIMIT_*` | the limits of section 1, optional | |
| `PORT`, `SUBSTRATE_BIND_ADDRESS`, `SUBSTRATE_INSECURE_ALLOW_CLEARTEXT` | the listener, as the server's; the image sets 8080, every interface, and true | |
| `SUBSTRATE_TRUSTED_PROXIES` | the TLS terminator in front, so limits key on the caller | `10.0.0.0/8` |
| `LOG_LEVEL`, `LOG_FORMAT` | as the server's | `info`, `json` |

The seal key and the Apple keys are the secrets; section 1 says what each
leak costs. The relay's own logs hold no content, but they link a
registration's pushes over time, so the deployment keeps them a short time,
a week or less.

**Running it.** Its image, pinned by digest, behind a TLS terminator on a
public host the operator picks. With `TEAM_ID`, `KEY_ID`, `SEAL_KEY` and
`DIGEST` set in the operator's shell:

```bash
docker run -d --name push-relay --restart unless-stopped \
  -p 127.0.0.1:8090:8080 \
  -v /srv/push-relay/keys:/keys:ro \
  -e "SUBSTRATE_PUSH_RELAY_APNS_TEAM_ID=$TEAM_ID" \
  -e "SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_FILE=/keys/apns.p8" \
  -e "SUBSTRATE_PUSH_RELAY_APNS_PRODUCTION_KEY_ID=$KEY_ID" \
  -e "SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_FILE=/keys/apns.p8" \
  -e "SUBSTRATE_PUSH_RELAY_APNS_DEVELOPMENT_KEY_ID=$KEY_ID" \
  -e "SUBSTRATE_PUSH_RELAY_APNS_TOPICS=dev.reamde.substrate.chat" \
  -e "SUBSTRATE_PUSH_RELAY_SEAL_KEY=$SEAL_KEY" \
  "ghcr.io/geoah/substrate-push-relay@sha256:$DIGEST"
```

with `https://push.example.com` proxied to `127.0.0.1:8090`; this example
uses one key for both environments. On Kubernetes it is a Deployment of one
replica of the same image, the keys and the seal key mounted from secrets, a
Service and an Ingress for the host, and probes on `/healthz` (liveness) and
`/readyz` (readiness). The ingress passes `/v1/` and `/healthz` and nothing
else; metrics are scraped from the pod.

**Checking it.** After the first start and after any change of key:

```bash
docker exec push-relay substrate-push-relay check
```

`check` reads the same configuration, mints a JWT for each environment and
sends one alert, to a device token no install holds, to the first topic. It
prints what Apple answered for each. A key Apple refuses answers
`InvalidProviderToken`, which `check` reports as a failure. `BadDeviceToken`
says the request reached Apple and was read; it does not prove the key may
send to that topic in that environment, and `check` says so beside its exit
status. The acceptance is one real push to a consenting test install:

```bash
docker exec push-relay substrate-push-relay check \
  --device-token "$TEST_INSTALL_TOKEN" --environment development
```

which sends one `reply` alert, with the fallback words and an event id that
resolves nowhere, to that install. A development build of the app shows its
own Apple token in its diagnostics (section 5). Once a TestFlight build
exists, the same for `production`. Then `curl https://push.example.com/healthz`
from outside.

**Afterwards**, in this order, each harmless until the next:

1. Merge pull request 2. A server that upgrades lists `push` and sends
   nothing.
2. Merge pull requests 3a and 3b, and release the app with
   `SUBSTRATE_APPLE_PUSH_RELAY=https://push.example.com`: the document names
   the relay, the build carries it, and servers older than pull request 2
   ignore the key.
3. Send the app to TestFlight.
4. Upgrade a server. Its app installs register at their next open or
   refresh, and pushes start.

**When it is down**, servers drop their pushes and count `failed`; nothing
queues and nothing is deleted, and apps show what they missed when opened.

## What it costs

- **What the relay learns.** For each push: that something happened in some
  thread on the calling server, when, the unread count, the calling
  server's address, and, inside the push key, one sign-in's public key that
  links every push of that sign-in. At register: the install's address,
  Apple token and environment. It keeps none of it past the replay window
  and its limits, and its logs hold the fingerprints, events and times of
  section 1.
- **What Apple learns.** The same payload, and which install it reached.
- Neither learns what was said or who said it, and neither can open an
  event id. App Store Connect's privacy answers for the app are written
  from these paragraphs when the first build with push goes to review.
- One more image and process the publisher runs, and the keys it holds.
- A device record per install, a changelog line per registration (about one
  a month per install), kept a week.
- A query per push for the badge, over the person's `chat` threads: tens of
  milliseconds where `referencedBy` was measured, a few times a minute at
  most, and none when nobody has a device; and one for each time the app
  comes to the front.
- After a sign-out or an account switch, a push already queued at Apple can
  set a stale badge until the app next opens (section 5).
- A restore without secrets mints a new DEK, so every install of that
  repository registers once again after it.

## Not proposed

- **Content in a push**, even encrypted for the device. The extension reads
  the item with the person's own token, and a payload with no content has
  nothing to protect.
- **A relay with a database of registrations.** The sealed key carries what
  it would store, and a stateless relay needs nothing else.
- **Registration through the server, or a relay address the server alone
  decides.** The server would see Apple's token, or could send the install
  to a relay of its choosing.
- **The relay authenticating servers by fetching a key from them.** A home
  server behind a router or on a private network is unreachable from the
  relay; the app always reaches its server.
- **A badge per install's own list.** A server cannot follow an install's
  `showAutomated` or how far it has scrolled, so the badge counts what the
  app lists by default, and the app shows that number.
- **Retries, receipts, or a queue that outlives a restart.** The records are
  the truth.
- **A hosted push service.** It would see every push, and the publisher's
  key with it.
- **Sending to Apple from the owner's server with an operator's key.** Only
  the app's publisher can sign for the app. A fork of the app, signed by
  another team, names its own relay in its own build and document.
- **Checking that a registration comes from a genuine install** (Apple's App
  Attest). It would close the residual of a dropped seal key (section 1) at
  the cost of a check on every registration; not now.
- **Apple's notification filtering entitlement** as a launch dependency. It
  would let the extension drop a push read elsewhere in the seconds before
  it arrives; the publisher may apply for it separately, and nothing here
  waits for it.

## Public practice this draws on

- Matrix's [push gateway](https://spec.matrix.org/latest/push-gateway-api/),
  ntfy's [iOS relay](https://docs.ntfy.sh/config/#ios-instant-notifications),
  Mattermost's
  [push proxy](https://docs.mattermost.com/deployment-guide/mobile/mobile-faq.html)
  and Home Assistant's
  [notification relay](https://contributors.home-assistant.io/companion/mobile-app-api/notifications/):
  a publisher's relay in front of a self-hosted server.
- Apple's
  [token-based connection](https://developer.apple.com/documentation/usernotifications/establishing-a-token-based-connection-to-apns),
  [sending a request](https://developer.apple.com/documentation/usernotifications/sending-notification-requests-to-apns),
  [handling its responses](https://developer.apple.com/documentation/usernotifications/handling-notification-responses-from-apns),
  [the payload](https://developer.apple.com/documentation/usernotifications/generating-a-remote-notification),
  [modifying content in an extension](https://developer.apple.com/documentation/usernotifications/modifying-content-in-newly-delivered-notifications)
  and the
  [`aps-environment` entitlement](https://developer.apple.com/documentation/bundleresources/entitlements/aps-environment).
- Firebase's
  [HTTP v1 API](https://firebase.google.com/docs/cloud-messaging/send/v1-api)
  for the Android follow-up, and
  [RFC 8030](https://www.rfc-editor.org/rfc/rfc8030.html) and
  [RFC 8292](https://www.rfc-editor.org/rfc/rfc8292.html) for Web Push.
