---
status: accepted
date: 2026-11-18
decision-makers: George Antoniadis
---

# 0336. An install that takes pushes is a device record named by its token, and dies with it

## Context and Problem Statement

A server that pushes to a person's phone has to know which installs take
pushes, under which relay key, and for which events
([the plan](../plans/push.md), section 3). That fact belongs somewhere a
restore, an export and every reader of the repository already reach, and it
must not outlive the sign-in it was registered under: a device kept after its
token was revoked would keep receiving pushes for a sign-in that ended.

## Considered Options

- A record of a core kind per install, named by the token it signed in with.
- A record per install under a minted id, with the token as a property.
- A table on the relay, or an engine table beside the records.

## Decision Outcome

Chosen: one `substrate.reamde.dev/core/device` record per install, whose id
is the id of the token it signed in with, deleted with that token. Records
are the truth here, so which installs take pushes is data in the repository
and not a table on the relay. Naming it by its token makes the token the one
source for the install's identity: a second record for one sign-in cannot
exist, and a device cannot be signed for under another sign-in's key, since
each token's pushes are signed with a key derived from its own id
([0337](0337-a-server-calls-out-on-its-own-initiative-only-to-push-only-through-the-relay-its-apps-verified-publisher-names.md)).
A minted id would need a lookup to find a token's device and a rule to keep
it to one.

The record holds `token` and `person` (references, filled by the engine and
fixed at the create), `name`, `platform` (`ios`), `pushKey` (opaque),
`notifies` (`replies`, `requests`, at least one) and `seenAt` (stamped). The
bundle id, Apple's environment and the registration's generation stay inside
the push key, where the relay reads them, so the record cannot say one thing
and the key another. The kind is `internal`, keeps seven days of history
(bookkeeping, [0283](0283-trigger-run-records-keep-a-week-of-history-and-positions-stay-in-the-log.md)),
and `core` goes to 65.

What the engine holds (`internal/engine/devices.go`):

- **The write.** Only a person, or an app acting for them, writes one, and
  only at the id of the token the request carries. An agent, a function and
  a package write none. It is written with a put at its own address, since
  `POST /api/v1/records` mints ids. A put merges: a missing `notifies` is
  refused only on a create, and `required` is held on the record the write
  leaves, so `[]` is refused (corrected after the plan's final review, C3).
- **The delete.** The token itself, any other token of the same person, an
  admin, and the engine as the system.
- **Roles.** The shipped `viewer` role gains `write` over the kind, as it
  gained it over the mark
  ([0313](0313-how-far-a-person-has-read-a-thread-is-a-record-of-their-own-and-its-position-only-moves-forward.md));
  the own-device rule narrows it to the person's own. A scoped app needs
  `write:substrate.reamde.dev/core/device`, an ordinary kind scope.
- **Going away.** The `token` reference is `onDelete: cascade`, so the sweep
  that collects a deleted token collects its device. `RunGC` deletes the
  device of a token past its expiry, which no cascade reaches. After a relay
  says the install is gone, or refuses its key, signature, topic or
  environment, the dispatcher has the engine delete it, only while the
  record still holds the key that send carried, so an install that
  registered again meanwhile keeps its new registration.

### Consequences

- Good, because a revoked or expired sign-in stops its pushes without any
  step of the app's, and nothing on the relay has to be told.
- Good, because a restore, an export and the change feed carry devices as
  they carry every record, and a reader of the record cannot push with it.
- Bad, because whoever reads `core/device` reads every person's devices, as
  a kind is the unit of permission
  ([0275](0275-what-a-person-may-do-is-the-roles-they-hold-and-nothing-is-called-owner.md)).
- Bad, because a restored copy holds the same devices and signs for them:
  every copy runs with `SUBSTRATE_PUSH=false` (0337).
- Bad, because between a token's delete and the sweep that collects it the
  record stays, up to five minutes; nothing is sent to it, since its token
  is not live.

### Confirmation

The device put and delete are exercised through the real handler in
`internal/api`'s push tests, the sweep in the engine's tests, and
`TestEveryShippedKindsRetentionIsHeld` holds the kind's retention class.
`mise run kinds:check` holds the version bump.

## More Information

The Mac and Android add `macos` and `android` to `platform` when their apps
arrive ([the plan](../plans/push.md), section 6).
