# Plan: push to a closed device through the publisher's relay

Status: proposed 2026-11-18, not built. Part of
[#1400](https://github.com/geoah/substrate/issues/1400). This is change 7
of [the chat experience plan](chat-experience.md#7-push-to-a-closed-device-l),
which now points here and keeps nothing of its own about push. A plan, not a
contract: when it is built, the code is the contract and this page says what
was built.

## The short version

Moss answers while the phone is in a pocket and the app is closed. Today
nothing reaches the phone until iOS runs the app's background refresh, when
it chooses. After this plan the phone shows "Moss replied" with the start of
the reply within seconds, and a question or an approval Moss is waiting on
shows the same way, with its buttons. Reading the chat on the Mac takes the
notification back off the phone and lowers its badge.

A self-hosted server cannot send to a published app by itself: the key that
signs an Apple push belongs to the app's publisher. So the publisher runs a
small relay, and the server reaches the phone through it. Five pieces:

1. **The relay**, `substrate-push-relay`, a second binary in the substrate
   image. It holds the publisher's Apple key, accepts a push only from the
   repository the install registered with, and builds every push from three
   values: what happened, which thread, and the unread count. It stores
   nothing.
2. **The authority document names it.** Each app entry in a publisher's
   document may name its relay (`push.relay`). A server pushes only through
   the relay the document of the token's verified app names
   ([0209](../decisions/0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md)).
3. **A device record.** Each install that takes pushes is one
   `substrate.reamde.dev/core/device` record, named by the token it signed
   in with. Revoking the token retires it.
4. **The server** waits five seconds after a reply settles, sends nothing if
   the person has read it by then, and otherwise posts one signed request to
   the relay. Best effort: nothing is retried or queued past a restart.
5. **The apps.** Substrate Chat asks permission, registers with the relay,
   writes its device record, and shows each push through a notification
   service extension that reads the reply from the person's own server with
   the app's own token.

Three pull requests build it, and they can be built at the same time
because the interfaces between them are fixed below:

| # | Pull request | Size | Holds |
| --- | --- | --- | --- |
| 1 | The relay | M | the binary, its API, Apple's push service, the image, its operator docs |
| 2 | The server and the device kind | L | `core/device`, the dispatcher, the call to the relay, the route the app asks, the authority document key |
| 3 | The apps | L | registration, the notification service extension, entitlements, the site setting |

The relay can be deployed before any server is upgraded, and every piece is
inert until the pieces after it exist.

## What holds

The design was checked against these rules:

- **Records are the truth.** Which installs take pushes is data in the
  repository, a record per install, not a table on the relay. How far a
  person has read is the mark of
  [0313](../decisions/0313-how-far-a-person-has-read-a-thread-is-a-record-of-their-own-and-its-position-only-moves-forward.md),
  and the push reads it; nothing keeps a second read state.
- **The relay never sees content.** No title, no text, no sender, no
  subject. The app fetches what to show from the owner's own server.
- **One source for each fact.** The relay's address comes only from the
  authority document; the server reads it and tells the app. Which Apple app
  and which Apple environment an install belongs to live only inside its
  push key, where the one reader that acts on them reads them. The device's
  id is its token's id.
- **A link is a reference.** The device names its token and its person as
  references
  ([0044](../decisions/0044-a-reference-is-the-only-link-between-records.md)).
- **The client owns the words.** Nothing on the server or the relay writes a
  sentence a person reads. The fallback text is a key into the app's own
  strings.
- **Best effort, and inert by default.** A push that fails is dropped. The
  records are the truth and the app reads them when it opens. With no relay
  named anywhere, no device can register and nothing is ever posted.
- **This repository is public.** No relay address, Apple key, key id or team
  id is in this tree. The examples use `push.example.com` and
  `apps.example.com`.

## How a push travels

```text
install                        owner's server                   publisher's relay     Apple
   |                                  |                                 |               |
   |<---------------------------------------------------------------- 1. device token --|
   |-- 2. GET /api/v1/push/relay ---->|                                 |               |
   |<----------- {relay, serverKey} --|                                 |               |
   |-- 3. POST /v1/register ------------------------------------------->|               |
   |<------------------------------------------------------ {pushKey} --|               |
   |-- 4. POST /api/v1/records ------>|                                 |               |
   | (core/device, pushKey)           |                                 |               |
   :                                  :                                 :               :
   |                                  | a chat thread settles,          |               |
   |                                  | 5 s pass, the mark has          |               |
   |                                  | not caught up                   |               |
   |                                  |-- 5. POST /v1/push, signed ---->|               |
   |                                  |                                 |-- 6. HTTP/2 ->|
   |<----------------------------------------------------------- 7. alert, no content --|
   |-- 8. GET thread, message ------->|                                 |               |
   | (the app's own token)            |                                 |               |
   shows "Moss: Here is the summary you asked for..."
```

Steps 1 to 4 happen when the app opens and something changed. Steps 5 to 8
happen once per push.

## 1. The relay

### Where it lives and how it ships

- `cmd/substrate-push-relay`: the binary. Configuration, the listener, and
  one subcommand, `check`.
- `internal/pushrelay`: its HTTP handler, the seal, the limits.
- `internal/apns`: Apple's push service, written against `net/http` and
  `crypto/ecdsa` alone. Go speaks HTTP/2 to Apple without help, and the JWT
  is one ES256 signature, so about two hundred lines replace a lightly
  maintained dependency.
- `internal/pushwire`: the wire both sides share: the request and response
  types, the error codes, the signature. The server imports it too, so the
  two cannot drift.

It is a second binary, not a `substrated` subcommand. A subcommand would
link the engine, the function runner and the Postgres driver into a process
that faces the internet and holds the team's Apple key; the relay links none
of them, and a test holds its dependency list to that (no `internal/engine`,
`internal/api`, `internal/runner`, no database driver). `substrated` takes no
subcommands today.

It ships in the substrate image with no separate build. The Dockerfile's
`build` stage compiles it beside the server and the CLI, the `prebuilt`
stage copies goreleaser's build of it, and the runtime stage puts it at
`/usr/local/bin/substrate-push-relay`. `.goreleaser.yaml` gains a build id
for it (linux, amd64 and arm64). A deployment runs the same image with the
entrypoint overridden. It listens on `PORT` and `SUBSTRATE_BIND_ADDRESS`
exactly as the server does, so the image's `HEALTHCHECK`, which probes
`/healthz` on that port, holds for it unchanged.

### Its API, version 1

Every route carries its version in the path. A relay keeps serving `/v1/`
while any server that speaks it may exist; a change of meaning is `/v2/`. A
key a relay does not know in a request body is ignored, so a newer server
can post to an older relay; a value it does not know, an `event` or a
`platform`, is refused.

**Register.** The app calls this itself, never through its server, so the
server never sees Apple's device token and its history never holds one.

```http
POST /v1/register
Content-Type: application/json

{
  "platform": "apns",
  "token": "5f8a9c...(64 hex characters)",
  "topic": "dev.reamde.substrate.chat",
  "environment": "production",
  "serverKey": "q1x3Lk...(43 characters)"
}
```

| Field | Holds | Bounds |
| --- | --- | --- |
| `platform` | the push service | `apns`; `fcm` is reserved for Android |
| `token` | what Apple calls this install | hex, 32 to 400 characters |
| `topic` | the app's bundle id | one of the relay's topics |
| `environment` | which Apple service issued the token | `production` or `development` |
| `serverKey` | the public key of the repository the install signs in to | base64url, no padding, 32 bytes |

```http
200 OK
{"pushKey": "v1.Qm9vbGVhbi..."}
```

**Push.** Only a server calls this.

```http
POST /v1/push
Content-Type: application/json
Substrate-Push-Signature: t=1763459123,s=3nq0Yw...

{"pushKey": "v1.Qm9vbGVhbi...", "event": "reply", "thread": "8f2c41d07a", "badge": 3}
```

| Field | Holds | Bounds |
| --- | --- | --- |
| `pushKey` | what register returned | at most 1,024 characters |
| `event` | what happened | `reply`, `request` or `read` |
| `thread` | the thread it happened on | a record id (`vocabulary.ValidID`) |
| `badge` | the person's unread count after it | 0 to 99,999 |

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
| 400 | `bad_request` | not JSON, a field missing or out of bounds, an unknown event or platform | logs it as a bug |
| 401 | `stale_signature` | `t` is more than 300 s from the relay's clock | logs the skew, keeps the device |
| 401 | `signature_refused` | no signature, or one the server key inside the push key does not verify | retires the device, `refused` |
| 403 | `key_refused` | the push key does not open under the relay's seal keys | retires the device, `refused` |
| 403 | `topic_refused` | the topic is not one this relay serves | retires the device, `refused`; at register the app stops |
| 410 | `gone` | Apple says the install is gone | retires the device, `unregistered` |
| 413 | `bad_request` | the body is over 4 KiB | logs it as a bug |
| 429 | `rate_limited` | a limit below, with `Retry-After` | drops this push |
| 502 | `upstream` | Apple was unreachable, answered 5xx or 429, or refused the publisher's key | drops this push |
| 500 | `internal` | anything else | drops this push |

### The push key

The relay seals what it needs into the key it returns and keeps nothing:

```json
{"p": "apns", "t": "5f8a9c...", "o": "dev.reamde.substrate.chat", "e": "production", "k": "q1x3Lk...", "i": 1763459000}
```

`p` the platform, `t` Apple's token, `o` the topic, `e` the environment,
`k` the server key, `i` when it was issued. The key is `v1.` and the
base64url of a 24-byte nonce and the XChaCha20-Poly1305 ciphertext of that
JSON under the relay's seal key, with `substrate-push-key-v1` as additional
data (`golang.org/x/crypto/chacha20poly1305`, already a dependency). To
everyone but the relay it is opaque: the server stores it and learns nothing
from it, and a reader of the record cannot recover Apple's token.

The relay opens a key with `SUBSTRATE_PUSH_RELAY_SEAL_KEY`, then with
`SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS` when that is set, and always seals
with the first. Rotating the seal key is setting the old one as previous and
a new one as current; installs move to the new one as they register again
(at least weekly, section 5), and the previous one is dropped after that.

### The signature

The server signs every push with the repository's signing key, an Ed25519
pair the server derives and never stores: HKDF-SHA256 over the host's
`SUBSTRATE_CREDENTIAL_KEY`, salt `substrate push`, info `signing v1 ` and
the repository's id (`rnd-...`). It is the same on every boot and in every
process, it is one per repository, and it changes when the credential key
changes or the repository is restored on a host with another one. Its
public half is the `serverKey` the app sends at register.

```text
Substrate-Push-Signature: t=<unix seconds>,s=<base64url of the Ed25519 signature>

signed bytes:  "substrate-push-v1\n" + t + "\n" + lowercase hex of SHA-256(body)
```

The relay accepts `t` within 300 seconds of its clock and verifies the
signature with the `k` sealed inside the push key. The path is not signed,
so a relay behind a prefix or a proxy that rewrites it still verifies. A
request replayed inside the window sends the same push again, which
replaces the notification it already showed (the collapse id below) and
changes nothing else.

### What it sends to Apple

The relay builds the whole payload from `event`, `thread` and `badge`. No
field of a request becomes text a person reads, so not even a valid server
can send words through the publisher's key.

`reply`, and `request` with `SUBSTRATE_PUSH_REQUEST` as its key:

```json
{
  "aps": {
    "alert": {"loc-key": "SUBSTRATE_PUSH_REPLY"},
    "badge": 3,
    "sound": "default",
    "mutable-content": 1,
    "thread-id": "8f2c41d07a"
  },
  "substrate": {"v": 1, "event": "reply", "thread": "8f2c41d07a"}
}
```

`read`:

```json
{
  "aps": {"badge": 2, "content-available": 1},
  "substrate": {"v": 1, "event": "read", "thread": "8f2c41d07a"}
}
```

| Header | `reply`, `request` | `read` |
| --- | --- | --- |
| `apns-push-type` | `alert` | `alert` (it sets a badge) |
| `apns-priority` | `10` | `5` |
| `apns-expiration` | now + 6 hours | now + 1 hour |
| `apns-collapse-id` | the thread id, when it is 64 bytes or fewer | none |
| `apns-topic` | the sealed topic | the sealed topic |

`loc-key` names a string in the app's own bundle, so the words are the
app's, in the person's language, even when the extension cannot run.

### Apple's key, its token and the two environments

The publisher's token-auth key is a PKCS#8 P-256 `.p8` file, its key id and
its team id, given at run time and nowhere else. The relay signs a JWT with
it, header `{"alg":"ES256","kid":"<key id>"}`, claims
`{"iss":"<team id>","iat":<now>}`, and reuses it for 50 minutes. Apple
refuses a token older than an hour and one replaced more often than every 20
minutes, so the relay mints a new one only past 50 minutes, or at once when
Apple answers `ExpiredProviderToken`, and then retries that one push once.
`InvalidProviderToken` means the key itself is wrong: the relay logs it as
an error and answers `upstream`, and the metric below counts it. It does not
take the relay out of service, since a relay out of service never sees a
fixed key accepted.

One key and one JWT serve both of Apple's services: a push whose sealed
environment is `production` goes to `api.push.apple.com`, one whose
environment is `development` to `api.sandbox.push.apple.com`, port 443,
HTTP/2, a 10-second timeout, connections kept open between pushes.

| Apple answers | The relay answers |
| --- | --- |
| 200 | 200 `sent` |
| 410 (`Unregistered`, `ExpiredToken`), 400 `BadDeviceToken`, 400 `DeviceTokenNotForTopic` | 410 `gone` |
| 403 `ExpiredProviderToken` | mints, retries once, then as that answer says |
| 403 `InvalidProviderToken`, 400 `TopicDisallowed`, 400 `BadTopic` | 502 `upstream`, logged as the publisher's configuration |
| 429, 5xx, a timeout, a refused connection | 502 `upstream` |
| anything else | 500 `internal`, logged |

### Abuse

The relay is a public endpoint. What stops it being a way to send pushes to
other people:

- **A push key alone sends nothing.** A push must be signed by the private
  key whose public half was sealed into the push key at register. That key
  is derived inside the owner's server and never leaves it. Whoever reads a
  device record, holds an export or watches the relay's traffic has push
  keys and no signing key.
- **Apple's token never leaves the install and the relay.** The server
  never sees it, so no record, history or archive holds it. Registering
  someone else's install needs their token.
- **Register is open, and harmless.** Anyone may register an install whose
  token they hold, under a key they hold. That lets them push to their own
  install and nobody else's.
- **No caller chooses words.** The payload is built from a closed event
  set, a record id and a number.
- **Topics are a list.** The relay pushes only to the bundle ids its
  operator lists.
- **Limits.** Each is a token bucket holding one minute's allowance, kept in
  memory, per replica:

| Limit | Default per minute | Variable |
| --- | --- | --- |
| pushes per push key | 20 | `SUBSTRATE_PUSH_RELAY_LIMIT_KEY` |
| pushes per caller address | 600 | `SUBSTRATE_PUSH_RELAY_LIMIT_CALLER` |
| registrations per caller address | 30 | `SUBSTRATE_PUSH_RELAY_LIMIT_REGISTER` |
| pushes in all | 12,000 | `SUBSTRATE_PUSH_RELAY_LIMIT_TOTAL` |

The caller address is read the way the server reads one: the transport peer,
or, past proxies listed in `SUBSTRATE_TRUSTED_PROXIES`, the address they name
in `X-Forwarded-For` (`internal/api/peer.go`, moved to a package both
import). The buckets live in a bounded map of 100,000 entries, oldest
dropped.

### What it logs and counts

A log line names a push key by its fingerprint, the first 12 hex characters
of SHA-256 of the key, and never in full. Apple's token is never logged. A
line carries the route, the status, the code, the topic, the environment,
the event and Apple's status, reason and `apns-id`. The thread id and the
badge appear only at debug level. The caller address appears on refusals
(signature, limits, malformed bodies), which are the lines abuse is read
from, and never on a push that was sent.

Metrics are Prometheus, on a listener of their own at
`SUBSTRATE_PUSH_RELAY_METRICS_ADDRESS` (unset is none), because the relay's
own port is public by design:

- `substrate_push_relay_requests_total{route, code}`
- `substrate_push_relay_apple_total{environment, status, reason}`
- `substrate_push_relay_apple_seconds` (a histogram)
- `substrate_push_relay_limited_total{limit}`
- `substrate_push_relay_tokens_minted_total`

### Health and state

`GET /healthz` answers 200 while the process serves. `GET /readyz` answers
200 once the Apple key parsed and a JWT was minted. Neither calls Apple: an
outage at Apple should not restart the relay or take it out of service. A
key Apple refuses shows in the logs and in
`substrate_push_relay_apple_total{reason="InvalidProviderToken"}`, and
`check` (section 9) says so directly.

The relay holds no state on disk and no database. In memory it holds the
current JWT, the limit buckets and its connections to Apple. Replicas need the same Apple key and the same seal keys and nothing
else; each counts its own limits.

## 2. The authority document names the relay

### The key, per app

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

`push.relay` is an `https` URL with a host and no user info, query or
fragment, at most 2,048 characters; a path is allowed, and the server
appends `/v1/push` to it. `AuthorityApp` in `internal/authority/reader.go`
gains `Push *AppPush` with `Relay string`. A malformed `push` is dropped and
the app reads as naming no relay, the way a malformed `movedTo` is dropped:
a mistake in it never makes the app unverified.

It is per app, not per authority, because:

- a token names one app (`clientId`), and the document already says what
  belongs to one app per app (`apple.appIDs`);
- a relay serves the bundle ids its operator lists, and not every app of a
  publisher takes pushes (Sync and the console do not);
- a publisher can move one app to another relay, or turn push off for one,
  without touching the others.

A publisher that runs one relay for every app repeats one URL, a few bytes.

### `apps:site` publishes it

`.mise/apps.sh`'s `site_build` writes `push: {relay: ...}` into the entry of
every app whose `app.json` says `"push": true`, from a new setting,
`SUBSTRATE_APPLE_SITE_PUSH_RELAY`, the relay's URL. Unset, it carries forward
what the live document says for that app, as `name` is carried forward, so a
release from a machine without the variable does not turn push off;
`SUBSTRATE_APPLE_SITE_PUSH_RELAY=none` removes it. `"push": true` is a new
`app.json` key, held by `lint:apps` to the presence of the app's
notification service extension and its `aps-environment` entitlement.

### How a server reads it

The server reads the document through the `authority.Reader` it already
has, from its 10-minute cache: the token's `appAuthority`, then the entry
whose `client_id` is the token's `clientId`, then its `push.relay`. A token
with no `appAuthority` (an unverified app, or no app at all) or with
`appLocal` set has no relay, by 0209's rule. A document a development file
supplies (`SUBSTRATE_INSECURE_AUTHORITY_DOCS`) counts, so a local server can
test against a local relay.

A server older than the key ignores it: the reader keeps the keys it knows
and drops the rest (`internal/authority/decode.go`). Production's server
today is such a server.

## 3. The device record

### The declaration

`kinds/substrate.reamde.dev/core/device.yaml`, in `core` because nothing
about an install is the agent runtime's (Tasks may push later). `core`
goes from version 64 to 65.

```yaml
# one install of an app that takes pushes (decision record on the device
# kind): its id is the id of the token it signed in with, which the engine
# fills in for a create that names none, so an install never keeps an id of
# its own and a new sign-in is a new device.
kind: substrate.reamde.dev/core/kind
metadata:
  id: substrate.reamde.dev/core/device
data:
  authority: substrate.reamde.dev
  package: core
  description: >-
    One install of an app that takes pushes, as signed in with one token:
    the push key its publisher's relay sealed for it and which events its
    person wants pushed there. Revoking the token retires it.
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
      max: 100
      description: what the device calls itself, as the app read it
    platform:
      type: enum
      values:
        - ios
        - macos
      description: the system the app runs on
    pushKey:
      type: string
      max: 1024
      description: what the publisher's push relay sealed for this install;
        opaque here, and absent while nothing is pushed to it
    notifies:
      type: enum
      repeated: true
      values:
        - replies
        - requests
      description: which events the person wants pushed to this install;
        absent is both, and an empty list is none
    seenAt:
      type: datetime
      managed: true
      description: when the install last wrote this record, stamped by the
        engine
    retiredBecause:
      type: enum
      writer: system
      values:
        - unregistered
        - refused
        - signedOut
      description: why the server stopped pushing here; cleared when the
        install writes a new push key
    retiredAt:
      type: datetime
      writer: system
      description: when the server stopped pushing here
```

| Property | Holds | Points at |
| --- | --- | --- |
| `token` | the sign-in this install holds | one `core/token`, the one whose id is this record's |
| `person` | whose install it is | one `core/actor` of type `person` |
| `name` | "iPhone", "MacBook Pro" | text |
| `platform` | `ios` (iPhone and iPad) or `macos` | an enum |
| `pushKey` | the relay's sealed key | opaque text |
| `notifies` | `replies`, `requests` | a set |
| `seenAt` | the last registration | a time |
| `retiredBecause`, `retiredAt` | why and when pushes stopped | an enum, a time |

The bundle id and Apple's environment are not properties. They are inside
the push key, which is where the relay reads them, so the record cannot say
one thing and the key another.

### Who writes it

- **The install, and only its own.** The app creates its record with
  `POST /api/v1/records`, kind `substrate.reamde.dev/core/device`, and no
  id. The engine gives it the id of the token the request carries, fills
  `token` and `person`, and stamps `seenAt`. The same post later updates it,
  as a mark's post does (0313). A write naming another token's id, or a
  `token` or `person` that is not the request's, is refused.
- **Who acts for a person.** A person's token, or an app's token acting for
  a person, writes the device of that token. An agent, a function and a
  package write none.
- **Deleting.** The token itself, any other token of the same person (a
  console listing `person` = `me` and removing one), and an admin.
- **Roles.** The shipped `viewer` role gains `write` over `core/device`, as
  it gained it over the mark, so a viewer's phone takes pushes too. The
  engine's own-device rule narrows it to their own.
- **App scopes.** The chat app asks for `full` and reaches the kind. A
  scoped app needs `write:substrate.reamde.dev/core/device`, an ordinary
  kind scope, so the grammar does not change
  ([0194](../decisions/0194-an-app-gets-a-scoped-token-through-a-registration-free-authorization-flow.md)).
  The own-device rule holds it to its own record whatever the scope says.
- **`retiredBecause` and `retiredAt`** are the engine's (`writer: system`).
  A write that sets `pushKey` clears both.

### How it is retired

Retiring clears `pushKey` and sets `retiredBecause` and `retiredAt`. The
record stays; the install writes a new key the next time it opens.

| What happened | When | `retiredBecause` |
| --- | --- | --- |
| The token was deleted: revoked, signed out, signed out everywhere ([0272](../decisions/0272-signing-out-everywhere-ends-every-token-the-person-holds-and-every-stream-ends-with-its-token.md)) | in the transaction that deletes the token | `signedOut` |
| The token expired | the first time the server would have pushed to it | `signedOut` |
| The relay answered `gone` | on that answer | `unregistered` |
| The relay answered `key_refused`, `signature_refused` or `topic_refused` | on that answer | `refused` |

`refused` covers a server whose signing key changed: a rotated credential
key, or a repository restored on another host. Every install is retired once
and registers again at its next open, under the new key.

### Privacy

The record holds a name the system gives the device (an iPhone calls itself
"iPhone" unless the app holds Apple's entitlement for the person's own name,
which it does not ask for), the system, and an opaque key. Whoever may read
`core/device` reads every person's devices, as whoever reads the mark kind
reads every person's marks: a kind is the unit of permission
([0275](../decisions/0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md)).
What they read cannot push to anyone.

## 4. The server

### What pushes

| Event | When | To whom | Not sent when |
| --- | --- | --- | --- |
| `reply` | a `chat` thread settles `ok`, `error` or `overbudget` and its `lastMessage` moved | the thread's person; their devices whose `notifies` has `replies` | at 5 s the person's mark reads through `lastMessage`, or the person archived the thread |
| `request` | a thread settles with `waitingOnPerson` above 0 and its `lastMessage` moved; or `waitingOnPerson` rises while the thread is not settling | the thread's person, or every person holding `admin` when the thread acts for nobody; devices whose `notifies` has `requests` | at 5 s the mark reads through `lastMessage` (a settle), or the count is back down (otherwise) |
| `read` | the person's mark moves forward, `markedUnread` is cleared, or `archived` is set | that person's devices the server sent an alert for this thread to since, except the device of the token that wrote the mark | no device was alerted; several within 2 s fold into the last |

A settle that leaves something waiting is a `request`, not a `reply`, so one
settle sends one push. A request is sent for an archived thread too: what
waits on a person is always notified
([0322](../decisions/0322-a-person-pins-and-archives-a-thread-on-their-own-mark-and-a-list-is-narrowed-by-the-records-that-point-at-its-rows.md)).

Nothing is pushed for:

- a thread that settles `stopped`: the person stopped it;
- `subagent` and `judge` threads, and the reply of a thread that acts for
  nobody (a trigger's run);
- a settle the boot's recovery writes for an interrupted run, so a restart
  does not send a burst;
- a run that fails before it writes any reply: its `lastMessage` does not
  move. The app shows the failure when it opens.

The thread's person is `heldTo.onBehalfOf`. "Reads through" is 0313's
order: the mark's `readThrough` is `lastMessage`, or a message after it.

### What goes out

One request to the relay per device: `pushKey`, `event`, `thread`, `badge`.
The thread's id, and nothing about it.

`badge` is counted for each device at send time: the threads a chat list
shows (every mode but `subagent` and `judge`) that the device's token can
read and that are unread for the person by 0313's rule, leaving out the ones
the person archived unless something in them waits on a person. It is
capped at 999.

A device is sent to when all of these hold:

- its `person` is the one the event is for, and it holds a `pushKey`;
- its `notifies` includes the event's class (`read` follows the class of the
  alert it takes back);
- its token is live and not expired;
- its token can read the thread: a token without scopes by its person's
  roles, a scoped app's token by a `read:` scope reaching
  `llm/thread`, or by an `agents:` scope reaching the thread's agent
  ([0195](../decisions/0195-an-app-talks-to-agents-decides-their-requests-and-tails-the-feed-it-reads.md)),
  read through `internal/scope` as the API reads it. A push never names a
  thread its device could not open;
- the token's app has a relay (section 2).

### Where it hooks in

The engine detects events in the transactions that make them, through the
`afterCommit` hooks a transaction already runs after it commits
(`internal/engine/dataset.go`):

- the thread write that settles a thread, where `stampLastMessage` runs
  (`internal/engine/threadmarks.go`), for every path that settles one: the
  loop, a stop, a continuation; the boot's recovery is marked and skipped;
- `countWaitingOnPerson`, when the count it writes is higher than the one it
  replaced;
- `prepareThreadMark`, when a write moves the position forward, clears
  `markedUnread` or sets `archived`;
- the delete of a `core/token`, which retires its device in the same
  transaction, not after it.

A dispatcher in `internal/engine/push.go` keeps one timer per repository
and thread: 5 seconds for `reply` and `request`, 2 for `read`; a second
event for the same thread restarts it. When it fires, it reads the thread,
the mark, the devices and their tokens in one read transaction, decides, and
hands each send to `internal/push`. It also keeps, in memory, which devices
it sent an alert to for which thread, so `read` goes only where there is a
notification to take back.

It is bounded: at most 1,024 events waiting, the oldest dropped first and
counted; at most four requests to relays in flight. A database has one
writer process
([0325](../decisions/0325-a-database-has-one-writer-process-and-a-retaken-lease-checks-who-else-started.md)),
so each event is seen once and each push is sent once. A restart drops what
was waiting; the app reads the records when it opens.

### The call out

This is the first time the server calls an outside service on its own
initiative, rather than one a person configured (a provider row, a
connector) or asked for. `internal/push` makes the call and nothing else
does:

- only to the relay the token's verified app's document names, at
  `<relay>/v1/push`;
- through `egress.Transport()`, which dials public addresses only unless
  the operator's `SUBSTRATE_EGRESS_ALLOW` lists one, and following no
  redirect (`egress.CredentialClient`);
- a 5-second timeout, at most 4 KiB sent and 4 KiB read back;
- `User-Agent: substrate/<version>`;
- no retry. Each answer is classified by the table in section 1; `gone` and
  the three refusals retire the device with an engine write, and everything
  else is counted and dropped.

`internal/push` also derives the repository's signing key (section 1), so
the engine asks it for the public half and never handles key material of its
own.

### The route the app asks

The app learns where to register, and which key to bind to, from its own
server:

```http
GET /api/v1/push/relay
Authorization: Bearer <the app's token>

200 OK
{"relay": "https://push.example.com", "serverKey": "q1x3Lk..."}
```

| Answer | When |
| --- | --- |
| 200 | the token's app is verified, not local, and its document names a relay |
| 403 `forbidden`, reason `no_relay` | the token is no app's, its app is unverified or local, or its document names no relay for it |
| 403 `forbidden`, reason `push_off` | the operator turned push off |
| 503 `unavailable`, `Retry-After: 60` | the authority document could not be read just now |

Any token may ask; the answer is a public key and an address the
publisher's document already publishes. `serverKey` is the same for every
token of the repository. The reply is `substrate.PushRelay` in
`internal/substrate`, held by the wire golden file. `no_relay` and
`push_off` are two new reasons, added by CLAUDE.md's steps for a reason.

### The operator's switch, the feature, and being inert

`SUBSTRATE_PUSH` (default `true`). Set to `false`, the engine detects no
events, the route answers `push_off`, and discovery does not list the
feature.

Discovery lists a new feature, `push`, revision 1, experimental, holding the
one route, listed only while push is on (`listed` in `featureTable`). An app
reads it before it registers and never probes.

| Feature, revision | Carries |
| --- | --- |
| `push` 1 | `GET /api/v1/push/relay`, the `core/device` kind, and pushes for `reply`, `request` and `read` |

The server is inert until every link exists: an operator who left push on,
a publisher whose document names a relay, an app that registered with it,
and a device record that holds its key. Remove any one and nothing is
posted. A device record written by hand, with a key, for a token whose app
names no relay, is never sent to.

### Logs and metrics

The server logs each retirement at info (the device's id, the reason), each
relay failure at warn (the relay's host, the status, the code), and each
send at debug. It never logs a push key; the device's id names it.

- `substrate_push_total{event, outcome}`, where `outcome` is one of `sent`,
  `seen`, `archived`, `no_device`, `no_relay`, `unreadable`, `gone`,
  `refused`, `limited`, `failed`, `dropped`
- `substrate_push_relay_seconds` (a histogram)

Neither carries a repository, so `SUBSTRATE_METRICS_ANONYMIZE` has nothing
to hash.

## 5. The apps

Substrate Chat first, on iPhone, iPad and Mac. The pieces that are not the
chat's own go in the kit, so Tasks can take pushes later.

### Asking and registering

1. The app reads discovery. Without `push` 1 it keeps its local
   notifications as today and stops here.
2. It asks permission after the first reply settles, with a sentence saying
   what will be notified (as change 8a of the chat plan already does). It
   writes a device only while permission is granted, and deletes its own
   when it finds permission was taken away.
3. It registers for remote notifications and gets Apple's token.
4. It asks `GET /api/v1/push/relay`. On `no_relay` or `push_off` it stops,
   deletes its device if it has one, and its settings say this server does
   not notify a closed app.
5. It reads its Apple environment from its own provisioning profile:
   `embedded.mobileprovision` on iOS, `Contents/embedded.provisionprofile`
   on the Mac, the `aps-environment` (on the Mac
   `com.apple.developer.aps-environment`) entitlement inside it. No profile,
   as in an App Store or TestFlight build, is `production`. The build
   setting is not used: an export re-signs the app and replaces the
   entitlement, and only the profile says what the signed app holds.
6. It posts `/v1/register` to the relay with `apns`, the token, its bundle
   id, the environment and `serverKey`, and gets its push key.
7. It posts its device record: `name`, `platform`, `pushKey`, `notifies`.

The app's settings get two switches, Replies and Requests, which write
`notifies`.

### Keeping the record current

At every launch the app repeats steps 3 and 4, reads its own device record
by the id the first post returned, and runs 5 to 7 again when any of these
changed: Apple's token, the relay, the server key, the record is gone or
retired, or seven days passed since the last registration. It keeps the last
inputs in the Keychain beside the session. The seven days are what moves
installs to a new seal key.

Signing out revokes the app's token, which retires its device on the
server. The app deletes the record first all the same, as a courtesy that
may fail.

### The notification service extension

A new target, `SubstrateChatNotifications`, bundle id
`dev.reamde.substrate.chat.notifications`, listed in `app.json`'s
`apple.extensions`. For `reply` and `request` the system runs it before the
alert shows, with about 30 seconds.

- It reads the session the app already keeps in the Keychain
  (`SessionStore.chat`), readable after the first unlock. The app and the
  extension share the access group `$(AppIdentifierPrefix)dev.reamde.substrate.chat`,
  which is the app's own identifier, so the item every earlier build wrote
  is already in it, as Substrate Sync's share extension reads its app's
  session. On the Mac an access group applies only in the data protection
  keychain, so the session moves there once: the first launch of the build
  that brings push reads the item where it was, writes it there and deletes
  the old one, and nothing reads two places after that.
- For `reply` it reads the thread, then its `lastMessage`, with the app's
  token, and sets the title, the body (the start of the reply as plain
  text), `threadIdentifier` and the category with the same code the app's
  local notifications use (`ChatNotifications`), so a push and a local
  notification look and act the same.
- For `request` it reads what waits in the thread (the pending
  interaction, approval or change request) and builds the card's
  notification with its actions and the record's version, as the local one
  does.
- Each read has an 8-second timeout. On any failure the alert shows as
  Apple delivered it: the app's own words for `SUBSTRATE_PUSH_REPLY` or
  `SUBSTRATE_PUSH_REQUEST`, which ship in the app's strings and which a test
  holds present.
- It links the kit's API and the notification builder, never the views:
  Apple holds an extension to about 24 MB of memory.

### Read, badge and the app's own notifications

- A `read` push wakes the app when the system allows: it removes the
  delivered notifications of that thread (`cancel(thread:)` already does)
  and sets the badge. The badge itself is set by the push even when the app
  does not wake.
- Every alert carries the badge too, and the app sets the badge from the
  marks whenever it opens, so a dropped `read` is put right at the next
  open.
- While the install holds a push key, the background refresh posts no reply
  or request notifications of its own, so nothing is notified twice. It
  still reconciles marks and the badge.
- In front, the app's delegate keeps a push for the chat on screen quiet,
  as it does a local one.

### Entitlements, the App ID and the two environments

- The app gains `aps-environment` (`com.apple.developer.aps-environment` on
  the Mac) set to `development` in its entitlements files. An export for
  TestFlight, the App Store or the iPhone feed signs with a distribution
  profile, which sets `production`. Only the site variant of the
  entitlements carries it, because a local build has a loopback client id,
  which 0209 never verifies, and so has no relay.
- The app and the extension gain the shared `keychain-access-groups` entry.
- The App ID `dev.reamde.substrate.chat` needs the Push Notifications
  capability. `appstoreconnect.swift profiles` turns it on through the API
  for every app whose `app.json` says `push`, before it makes the App Store
  profile, so a release never depends on a development build having run
  first. The extension's App ID needs no capability of its own.

| Build | Signed with | Environment |
| --- | --- | --- |
| Xcode run on a device or a simulator | development profile | `development` |
| The Mac app as released today | development profile | `development` |
| The iPhone feed's build | ad hoc profile | `production` |
| TestFlight, App Store | distribution, no embedded profile | `production` |

A simulator receives real `development` pushes, and `xcrun simctl push`
delivers a payload file to a simulator with no relay at all, which is how
the extension is tested.

## 6. Android and the console

**Android** is a follow-up. Substrate Chat has no Android build today. When
it has one, push is Firebase Cloud Messaging through the same relay: the
relay gains the `fcm` platform, configured with the publisher's Firebase
service account file, and sends a high-priority data message carrying the
same three values; the app's messaging service reads the reply with its
token and posts the notification itself, since Android has no extension
step. The device kind gains `android`. Nothing on the server changes but
accepting the platform. A relay for installs without Google's services
(UnifiedPush) is not proposed.

**The console** can use Web Push, which needs no publisher key and no relay:
the server would send to the browser's push service with keys of its own.
That is a follow-up with its own properties on the device record. A console
page listing a person's devices, with a button to remove one, is a small
follow-up too; until it lands the records are visible through the records
browser.

**A Live Activity** for a running turn needs Apple's push-to-start tokens and
a push type of its own on the relay. It follows this.

## 7. Decision records

Three, numbered when each pull request is opened, after `git fetch --prune`
and `mise run decisions:check`:

| Record | Pull request | Says |
| --- | --- | --- |
| A publisher's push relay seals each install's address to the repository it registered with, and builds every push from an event, a thread and a count | 1 | the relay's place in the image, the sealed key, the signature, no content, no state, the versioned API |
| An install that takes pushes is a device record named by its token | 2 | the kind, the id, who writes it, retiring it, `viewer`'s rule |
| A server calls out on its own initiative only to push, only through the relay its app's verified publisher names | 2 | the `push.relay` key per app, the events, the hold, best effort, the egress rules, the operator's switch; it adds to 0209 |

The apps' choices (the extension, the Keychain group, reading the
environment from the profile) are commits.

## 8. The pull requests

### The interfaces between them

Fixed here so the three can be built at once. A change to any of these is a
change to this plan first.

| Interface | Between | Fixed in |
| --- | --- | --- |
| `POST /v1/register` and `POST /v1/push`: bodies, statuses, codes | relay and app; relay and server | section 1 |
| `Substrate-Push-Signature` and the signed bytes | relay and server | section 1 |
| The Apple payload, the `loc-key` names, `thread-id`, the collapse id | relay and app | section 1 |
| `apps[].push.relay` in the authority document | `apps:site` and the server | section 2 |
| `"push": true` in `app.json`, `SUBSTRATE_APPLE_SITE_PUSH_RELAY` | apps tooling | section 2 |
| The `core/device` declaration | server and app | section 3 |
| `GET /api/v1/push/relay`, `no_relay`, `push_off`, feature `push` 1 | server and app | section 4 |

`internal/pushwire` is the Go form of the first two rows:

```go
package pushwire

const (
	PathRegister    = "/v1/register"
	PathPush        = "/v1/push"
	SignatureHeader = "Substrate-Push-Signature"
	SignatureWindow = 300 * time.Second
	MaxBody         = 4 << 10
	MaxPushKey      = 1024
)

type RegisterRequest struct {
	Platform    string `json:"platform"`
	Token       string `json:"token"`
	Topic       string `json:"topic"`
	Environment string `json:"environment"`
	ServerKey   string `json:"serverKey"`
}

type RegisterResponse struct {
	PushKey string `json:"pushKey"`
}

type PushRequest struct {
	PushKey string `json:"pushKey"`
	Event   string `json:"event"`
	Thread  string `json:"thread"`
	Badge   int    `json:"badge"`
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
```

### Pull request 1: the relay (M)

`internal/pushwire`, `internal/apns`, `internal/pushrelay`,
`cmd/substrate-push-relay`, the Dockerfile and `.goreleaser.yaml`, the
image smoke, a "Running the push relay" section in `docs/operations.md`,
and its decision record.

Tests:

- `internal/apns`: the JWT's header, claims and signature, verified with the
  public half of a key the test generates; reuse under 50 minutes and a new
  one past it, on a fake clock; no new one inside 20 minutes but after
  `ExpiredProviderToken`; the request's path, host per environment and
  headers, against an `httptest` HTTP/2 server; every row of the answer
  table.
- `internal/pushrelay`: the seal opens, a wrong key and a changed byte do
  not, the previous key opens and never seals; register's bounds and the
  topic list; the signature valid, stale, from another key and over another
  body; the exact Apple payload of each event, as golden JSON; each limit on
  a fake clock; the 4 KiB bound; unknown routes and methods; `/healthz` and
  `/readyz`; and that no log line at any level holds a push key or Apple's
  token in full, read from a captured handler.
- `cmd/substrate-push-relay`: it refuses to start without the Apple key,
  its id, the team, a topic or a seal key, and on a public bind without
  `SUBSTRATE_INSECURE_ALLOW_CLEARTEXT`; `check` against a fake Apple.
- Its dependency list holds none of `internal/engine`, `internal/api`,
  `internal/runner` or a database driver.
- `.mise/imagesmoke.sh` starts the binary from the image with a key the
  script generates (never an Apple key) and probes `/healthz`.

### Pull request 2: the server and the device kind (L)

Stacked on 1: it imports `internal/pushwire`, and its end-to-end test runs
the real relay handler. `kinds/substrate.reamde.dev/core/device.yaml` and
`core` at 65; the device's admission, retiring and the token-delete hook;
`viewer`'s rule; the dispatcher; `internal/push`; the `push` key in
`internal/authority`; the route, the feature, the two reasons and the wire
struct; `SUBSTRATE_PUSH`; `docs/agents.md` (a Push section beside Read
state), `docs/api.md` (the route, the feature table, the reasons),
`docs/operations.md` (`SUBSTRATE_PUSH`, what the server sends where); its two
decision records.

Tests, run with `-run`, never the whole suite:

- the device's admission: the id is the token's, `token` and `person` are
  filled and fixed, another token's id is refused, an agent is refused, a
  viewer writes their own, a person's other token and an admin delete it,
  `retiredBecause` is the engine's, a new key clears it;
- deleting a token retires its device in the same transaction;
- the dispatcher on a fake clock and a fake relay: a settle pushes after 5
  s; a mark that catches up first stops it; archived stops a reply and not
  a request; a waiting settle is one `request`; a count that rises outside a
  settle pushes and one answered within 5 s does not; `read` goes only to a
  device that was alerted, never to the writer's; the badge; a scoped token
  that cannot read the thread is not sent to; no relay, an unverified app,
  a local app and `SUBSTRATE_PUSH=false` send nothing; `stopped`, recovery,
  `subagent` and a failure before any reply send nothing; `gone` retires
  `unregistered`, the refusals retire `refused`, `stale_signature`, 429 and
  502 retire nothing;
- `internal/push`: the derived key is stable and differs per repository; the
  request is the exact JSON with a signature `pushwire.Verify` accepts; end
  to end through `pushrelay`'s handler to a fake Apple; a private address
  and a redirect are refused; the 4 KiB bounds;
- `internal/authority`: `push.relay` parsed, a malformed one dropped with
  the app still read;
- the API, on the real engine: the route's four answers, with an authority
  document from a test file; the feature listed and not; the features,
  error reasons and wire golden files regenerated with their `-update`
  flags;
- `mise run kinds:check` and the retention golden.

### Pull request 3: the apps (L)

The kit's push pieces (a `PushRegistration` the device layer implements,
its Apple implementation, the relay client, the profile reader), the chat
app's registration and settings, the extension target, the entitlements,
the Keychain group, the strings, `"push": true` in `apps/chat/app.json`,
`SUBSTRATE_APPLE_SITE_PUSH_RELAY` in `apps:site`, the capability in
`appstoreconnect.swift`, `lint:apps`' rule, `apps/README.md` and
`apps/chat/DESIGN.md` (push leaves the non-goals).

Tests:

- the kit, in `swift test`: when to register again (each input changed, a
  retired record, seven days); the relay client's JSON against the pinned
  bodies; the environment from profile fixtures for each row of the table;
- ChatKit: the notification content for a reply and for each card, built
  from fixture records, is the same for a push as for a local notification;
  `read` takes back the thread's notifications and sets the badge, on a fake
  `Notifications`;
- the strings hold `SUBSTRATE_PUSH_REPLY` and `SUBSTRATE_PUSH_REQUEST`;
- `lint:apps`: `"push": true` without the extension or the entitlement
  fails;
- `apps:site` in a dry run: the key written when the setting is set, carried
  forward when it is not, removed by `none`;
- on a simulator: `xcrun simctl push` with each payload of section 1, the
  extension against a dev server, and the fallback with the server stopped;
- by hand, before a release: a real iPhone through a real relay, both
  environments, and the Mac.

It may merge before 2: it registers only where discovery lists `push`, so
against today's servers it changes nothing.

### Order

Build all three at once. Merge 1, then 2 (stacked), and 3 when ready. Deploy
in the order of section 9. Each step is inert until the next.

## 9. Deploying the relay alone first

The relay goes out before any server is upgraded, from the first `main`
image that holds pull request 1. Nothing calls it until a server with pull
request 2 and an app with pull request 3 exist, so it can run for weeks
answering only its health checks.

**What it needs:**

| Variable | Holds | Example |
| --- | --- | --- |
| `SUBSTRATE_PUSH_RELAY_APNS_KEY_FILE` | the publisher's `.p8`, readable by uid 65532 | `/keys/apns.p8` |
| `SUBSTRATE_PUSH_RELAY_APNS_KEY_ID` | its key id | `<key id>` |
| `SUBSTRATE_PUSH_RELAY_APNS_TEAM_ID` | the team id | `<team id>` |
| `SUBSTRATE_PUSH_RELAY_APNS_TOPICS` | the bundle ids it pushes to, comma separated | `dev.reamde.substrate.chat` |
| `SUBSTRATE_PUSH_RELAY_SEAL_KEY` | base64 of 32 random bytes (`openssl rand -base64 32`) | |
| `SUBSTRATE_PUSH_RELAY_SEAL_KEY_PREVIOUS` | the seal key before a rotation, optional | |
| `SUBSTRATE_PUSH_RELAY_METRICS_ADDRESS` | where metrics listen, optional | `:9090` |
| `SUBSTRATE_PUSH_RELAY_LIMIT_*` | the limits of section 1, optional | |
| `PORT`, `SUBSTRATE_BIND_ADDRESS`, `SUBSTRATE_INSECURE_ALLOW_CLEARTEXT` | the listener, as the server's; the image sets 8080, every interface, and true | |
| `SUBSTRATE_TRUSTED_PROXIES` | the TLS terminator in front, so limits key on the caller | `10.0.0.0/8` |
| `LOG_LEVEL`, `LOG_FORMAT` | as the server's | `info`, `json` |

The seal key and the Apple key are the two secrets. Losing the seal key
retires every install once (each registers again at its next open). A
leaked seal key lets its holder read Apple's tokens out of the push keys
they already hold, and still push nothing without a repository's signing
key; it is rotated as section 1 says. A lost or leaked Apple key is revoked
and replaced in Apple's developer account, and needs no change anywhere
else.

**Running it.** The image, the entrypoint overridden, behind a TLS
terminator on a public host the operator picks:

```bash
docker run -d --name push-relay --restart unless-stopped \
  -p 127.0.0.1:8090:8080 \
  -v /srv/push-relay/apns.p8:/keys/apns.p8:ro \
  -e SUBSTRATE_PUSH_RELAY_APNS_KEY_FILE=/keys/apns.p8 \
  -e SUBSTRATE_PUSH_RELAY_APNS_KEY_ID=<key id> \
  -e SUBSTRATE_PUSH_RELAY_APNS_TEAM_ID=<team id> \
  -e SUBSTRATE_PUSH_RELAY_APNS_TOPICS=dev.reamde.substrate.chat \
  -e SUBSTRATE_PUSH_RELAY_SEAL_KEY="$(cat /srv/push-relay/seal.key)" \
  --entrypoint /usr/local/bin/substrate-push-relay \
  ghcr.io/geoah/substrate:main-<commit>
```

with `https://push.example.com` proxied to `127.0.0.1:8090`. On Kubernetes
it is a Deployment of the same image with `command` set, the two secrets
mounted, a Service and an Ingress for the host; the image's health check, or
probes on `/healthz` (liveness) and `/readyz` (readiness). The ingress
passes `/v1/` and `/healthz` and nothing else; metrics are scraped from the
pod.

**Checking it.** Once, after the first start and after any change of key:

```bash
docker exec push-relay substrate-push-relay check
```

`check` reads the same configuration, mints a JWT and sends one alert, to a
device token no install holds, in each environment, to the first topic.
Apple refuses a key it does not accept with `InvalidProviderToken` before it
looks at the device token, so `BadDeviceToken` is the answer that says the
key and the topic were accepted. `check` prints, for each environment, that
the key was accepted, or exactly what Apple said instead, and exits non-zero
on anything but `BadDeviceToken`. The first run confirms that Apple still
answers in that order. Then `curl https://push.example.com/healthz`
from outside.

**Afterwards**, in this order, each harmless until the next:

1. Merge pull request 2. A server that upgrades lists `push` and sends
   nothing.
2. Merge pull request 3 and run `apps:site` with
   `SUBSTRATE_APPLE_SITE_PUSH_RELAY=https://push.example.com`. The document
   names the relay; servers older than pull request 2 ignore it.
3. Send the app to TestFlight.
4. Upgrade a server. Its app installs register at their next open, and
   pushes start.

**When it is down**, servers drop their pushes and count `failed`; nothing
queues and nothing is retired, and apps show what they missed when opened.

## What it costs

- The relay learns, for each push, that something happened in some thread
  of some repository on the calling server, when, and the unread count; at
  register, the install's address and Apple token. Apple learns the same
  payload. Neither learns what was said or who said it.
- One more process the publisher runs and two secrets it keeps.
- A device record per install, a changelog line per registration, kept a
  week.
- A query per push for the badge, over the person's threads: tens of
  milliseconds where `referencedBy` was measured, a few times a minute at
  most.
- The badge counts every unread chat, and the app, when open, counts the
  chats it has loaded; past its first page the two can differ until the
  next push.
- A rotated credential key retires every install of every repository on
  that host once.

## Not proposed

- **Content in a push**, even encrypted for the device. The extension reads
  the reply with the person's own token, and a payload with no content has
  nothing to protect.
- **A relay with a database of registrations.** The sealed key carries what
  it would store, and a stateless relay scales by adding replicas.
- **Registration through the server.** The server would see Apple's token,
  and its history would keep it.
- **The relay authenticating servers by fetching a key from them.** A home
  server behind a router or on a private network is unreachable from the
  relay; the app always reaches its server.
- **Retries, receipts, or a queue that outlives a restart.** The records are
  the truth.
- **A hosted push service.** It would see every push, and the publisher's
  key with it.
- **Sending to Apple from the owner's server with an operator's key.** Only
  the app's publisher can sign for the app. A fork of the app, signed by
  another team, names its own relay in its own document.
- **Apple's notification filtering entitlement**, which would let the
  extension drop a push that turned out to be read already. It is granted by
  Apple on request; see the questions.

## Questions for the owner

1. **The relay's name and place.** A second binary,
   `substrate-push-relay`, in the main image, run by overriding the
   entrypoint. Agreed, or a different name?
2. **The relay's host.** Which public host it runs on, and which first
   `main` image it is deployed from. Nothing in this tree names it.
3. **Requests nobody started.** A question or an approval in a thread that
   acts for nobody (a scheduled run) is pushed to every person holding
   `admin`. Agreed, or nobody until Moss's `notify` exists?
4. **A run that fails before it replies** sends no push, because nothing
   in the thread moved. Agreed, or should a failure push on its own?
5. **The badge** counts every unread chat of the person, archived ones left
   out unless something waits in them, capped at 999. Agreed?
6. **A restart's recovery sends nothing** for the turns it settles. Agreed?
7. **The device record keeps a week of history**, as a trigger run does.
   Agreed, or all of it?
8. **The Mac.** Its builds are signed for development, so its pushes go
   through Apple's development service. Push on the Mac in the first
   release, or iPhone and iPad only?
9. **The limits' defaults**: 20 pushes a minute per install, 600 per
   calling server, 30 registrations a minute per address, 12,000 a minute in
   all.
10. **App Privacy.** The relay receives each install's Apple token, briefly,
    for the app's function, and stores none. App Store Connect's privacy
    answers for Substrate Chat need updating by hand before a release with
    push; how should they read?
11. **Apple's filtering entitlement.** Ask Apple for it, so a push read
    elsewhere in the seconds before it arrives can be dropped by the
    extension? Without it the extension must always show something.
12. **Approving from a locked phone.** Approve and Decline on a
    notification run today without unlocking the device. Pushes make a
    locked phone the usual place they are seen. Should those two actions
    require unlocking (`authenticationRequired`)?

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
