---
status: accepted
date: 2026-11-18
decision-makers: George Antoniadis
---

# 0335. A publisher's push relay seals each install's address to one sign-in's key, and builds every push from an event, a thread, a count and an opaque id

## Context and Problem Statement

A self-hosted server cannot push to a published app on its own: the key that
signs an Apple push is the app's publisher's. So the publisher runs a relay
the owner's servers send through, and that relay is a public endpoint holding
a key that can reach every install of the publisher's apps. Whatever it
accepts, stores and sends is what a stranger, a stolen database or a
compromised server can make it do. The design is section 1 of the push plan
(`docs/plans/push.md`, written in
[#1511](https://github.com/geoah/substrate/pull/1511)), part of
[#1400](https://github.com/geoah/substrate/issues/1400); this record keeps
what binds the relay, the servers and the apps beyond that plan.

## Considered Options

- A stateless relay: it seals what an install registers into a push key it
  hands back, and accepts a push only when it is signed by the key sealed
  inside.
- A relay with a database of registrations, and servers authenticated by a
  credential it issues them.
- The relay authenticating a server by fetching a public key from it.
- Registration through the owner's server, which forwards Apple's token.
- No relay: the owner's server sends to Apple with a key its operator holds.

## Decision Outcome

Chosen: a stateless relay, its own image, that seals each registration to one
sign-in's key and builds every push from an event, a thread, a count and an
opaque id, because it holds nothing worth stealing and no request can make it
say anything. A database of registrations is a second store of who has which
phone. Fetching a key from a server fails for a server at home behind a
router, which the app always reaches and the relay never may. Registration
through the server shows the server Apple's token and lets it pick the relay.
Only the publisher can sign for its app, so an operator's key does not exist.

What binds:

- **Its own image.** `substrate-push-relay`, the Dockerfile's `push-relay`
  target, published as `ghcr.io/geoah/substrate-push-relay` on the same
  commits as the server. It links none of the server's packages and no
  database driver, and keeps nothing on disk.
- **The push key** is `v1.` and the base64url of a 24-byte nonce and the
  XChaCha20-Poly1305 ciphertext, under the relay's seal key with
  `substrate-push-key-v1` as additional data, of the platform, Apple's token,
  the topic, the environment, the sign-in's Ed25519 public key, the
  install's generation and when it was issued. It is refused 90 days after
  it was issued. A previous seal key opens and never seals.
- **The signature** is `Substrate-Push-Signature: t=<unix>,s=<base64url>`
  over `substrate-push-v1\n`, `t`, `\n` and the hex SHA-256 of the body. It
  is checked within 300 seconds either way, against the key sealed in the
  push key. A signature is held until `t` plus 300 seconds and a repeat is
  refused as `replayed`, per replica. It is held only once its push passed
  the install's and the total limits, in one step under one lock and at one
  clock, so the window holds at most eleven minutes of the total limit and
  never drops a live entry: the total limit is bounded so that fits 200,000,
  and a window full anyway answers `rate_limited`.
- **No words from a caller.** The Apple payload is built from the event (a
  closed set), the thread (a record id), the badge (0 to 999), the event id
  (opaque, 32 to 1,024 characters) and the sealed generation; the alert is a
  `loc-key` into the app's own strings. A `request` carries no collapse id.
- **Limits** are token buckets of a minute's allowance, in memory: per
  install by a keyed digest of Apple's token and in all, charged only after
  the signature verifies; per caller and per registering caller, the address
  read through `internal/peer` as the server reads one, an IPv6 caller by its
  /64. Refusals in all bound the log and not the answers: past the bound a
  refusal keeps its own code, which a server acts on, and is counted instead
  of logged, and a request its caller's own limit holds is not charged to it.
- **The API is versioned in its path.** `/v1/` is served while anything that
  speaks it may exist; a key it does not know is ignored, a value it does not
  know is refused, and every refusal is one envelope with a code from a
  closed set (`internal/pushwire`).

### Consequences

- Good, because a copy of a device record, an export or the relay's traffic
  holds push keys and no signing key, and sends nothing.
- Good, because the relay has nothing to back up, migrate or erase, and can
  be deployed before any server that calls it.
- Good, because no server, however compromised, can put a word on a person's
  lock screen through the publisher's key.
- Bad, because a leaked seal key lets its holder read Apple's tokens out of
  the push keys they have and seal keys of their own. Dropping it (not
  keeping it as previous) refuses every push key once and every install
  registers again, but the tokens read stay valid until the app is
  reinstalled and registration is open, so their holder can keep sending
  content-free alerts within the limits. Closing that needs a check that a
  registration comes from a genuine install (App Attest), which is not done.
- Bad, because the replay window and the limits are per replica: a request
  replayed to another replica within ten minutes is sent again.
- Bad, because the window's bound caps the pushes in all at 18,181 a minute
  on one replica.
- Bad, because a compromised server keeps signing for the installs it held
  until their keys expire, up to 90 days after each last registered.
- Bad, because the relay's logs still link one registration's pushes by the
  push key's fingerprint, so they are kept a short time.

### Confirmation

`internal/pushwire`'s fixtures and their tests (`TestFixtures`,
`TestSignatureVectors`, `TestTheApplePushesFollowThePlan`,
`TestTwoCardsOnOneThreadAreTwoAlerts`), which the server and the apps test
against; `internal/pushrelay`'s tests, `TestEveryPushFixtureReachesAppleAsWritten`
and `TestNoLogLineHoldsWhatAPushCarries` among them;
`TestTheRelayLinksNothingOfTheServers` in `cmd/substrate-push-relay`; and
`ci:image`, which builds the image and boots it (`.mise/relaysmoke.sh`).

## More Information

How to run it is [the operations page](../operations.md#running-the-push-relay).
Revisit when a relay serves a second platform (`fcm` is reserved), when one
runs more than one replica, or if App Attest becomes worth its cost.
