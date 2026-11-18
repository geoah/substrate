---
status: accepted
date: 2026-11-18
decision-makers: George Antoniadis
---

# 0337. A server calls out on its own initiative only to push, only through the relay its app's verified publisher names

## Context and Problem Statement

Until now the server called an outside service only where a person
configured one (a provider row, a connector) or asked for it. Pushing to a
closed phone needs the publisher's Apple key, which a self-hosted server
does not hold, so it posts to the publisher's relay on its own initiative
([the plan](../plans/push.md), sections 2 and 4). Where it may post, what it
may say, and what holds the keys bind every later change to push.

## Considered Options

- Post only to the relay the token's verified app's publisher names, signed
  with a per-token key derived inside the engine, best effort.
- Let the server, or the app, choose the relay.
- Send to Apple from the server with an operator's key.
- Queue and retry pushes until they are delivered.

## Decision Outcome

Chosen: the server posts only to the relay its token's verified app's
publisher names, through the confined dialer, signed per token, and drops
what fails. A relay the server or the app chooses lets a compromised side
collect Apple tokens; an operator's key cannot sign for an app it does not
publish; and a queue would be a second truth beside the records, which the
app reads when it opens anyway.

- **Where.** An app's entry in its authority's document carries
  `push.relay`, per app, because a token names one app and a publisher may
  push for some apps and not others. It is the one key of the document read
  leniently: a value of the wrong shape is dropped and the app reads as
  naming no relay, so a typo cannot unverify the publisher's apps. This adds
  to [0209](0209-an-app-is-verified-by-its-authoritys-txt-record-and-document.md):
  a token with no verified authority, or a local one, has no relay. The app
  registers only when its build, the document and `GET /api/v1/push/key`
  name the same relay; the server never decides where an install registers.
- **How.** Through `egress.Transport()`, following no redirect, 5 seconds, 4
  KiB each way, no retry (`internal/push`).
- **What.** An event (`reply`, `failed`, `request`, `read`), a thread, the
  badge and an event id. `reply` and `failed` only for `chat` threads with a
  person; `request` for the six conversation modes, to the thread's person or
  the admins whose view reads it; nothing for `subagent`, `judge`, `stopped`
  or a recovery's settle. Each waits 5 seconds (`read` 2) and is checked
  again: a `failed` is dropped when the person's mark took a position since
  the turn began, and nothing else stands for "reading" (C7, which replaces
  the plan's stream-reader signal).
- **The event id** is the item's reference and version, the event, the
  thread and when it was issued, sealed with XChaCha20-Poly1305 under a key
  derived for the token, with its own additional data (C8), and resolvable
  by that token alone for 24 hours. It is at most 1,024 characters (C6). A
  `failed` resolves while its thread is still `error` or `overbudget`. A
  `read` names the alerts it takes back by fingerprints of their event ids,
  split over as many ids as they need (C1).
- **Who may see it.** Recipients, the badge and the actions offered are
  decided through the same view an API request with the device's token gets
  (`internal/api` `TokenView`), so a push says nothing its token could not
  read.
- **The keys stay in the engine.** Each token's signing and sealing keys are
  derived with HKDF from the repository's DEK and the token's id; what leaves
  the engine is a `PushSigner` for one token, never the DEK.
- **The switch.** `SUBSTRATE_PUSH` (default `true`); `false` hands out no
  events and answers the routes `push_off`. A restored copy, a dry run and a
  staging server derive the same keys for the same devices, so they run with
  it off, and the dev tasks default it off.

### Consequences

- Good, because the server is inert until every link exists: the operator's
  switch, a publisher's relay, a registered install and its device record.
- Good, because neither the relay nor Apple learns what was said, and a
  stolen push key or event id is useless without the token behind it.
- Bad, because the relay learns that something happened, when, the badge
  and the calling server's address, and links one sign-in's pushes by its
  public key.
- Bad, because a push that fails is lost, and after a restart no `read` is
  sent for what was alerted before it: the app reconciles when it opens.
- Bad, because a compromised server can keep signing content-free pushes to
  its installs until their push keys expire, at most 90 days.

### Confirmation

`internal/push`, `internal/pushdispatch` and the API's push tests on the real
engine, with a fake relay; `internal/authority`'s lenient-key test;
`TestFeaturesMatchTheGolden` and `TestErrorReasons` hold the `push` feature
and the two reasons.

## More Information

Corrections after the plan's final review that this record adopts: C1
(fingerprints), C3 (the device put, in
[0336](0336-an-install-that-takes-pushes-is-a-device-record-named-by-its-token-and-dies-with-it.md)),
C6 (1,024 characters), C7 (no stream-reader signal) and C8 (the seal's
additional data, and a `failed` resolved by status). Revisit if Android or
Web Push needs a relay the server chooses.
