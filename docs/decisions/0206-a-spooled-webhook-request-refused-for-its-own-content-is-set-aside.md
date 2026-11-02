---
status: accepted
date: 2026-11-02
decision-makers: George Antoniadis
---

# 0206. A spooled webhook request refused for its own content is set aside as `.refused`

## Context and Problem Statement

[0167](0167-the-webhook-door-answers-after-a-sealed-spool-write.md) says a
drain that cannot record a spooled request stops at it, retries, and lets the
spool fill rather than skip it. A request whose content no admission can
store is refused by every retry. Before
[PR 1132](https://github.com/geoah/substrate/pull/1132), a multipart part
name holding a NUL was one such request: its file stopped the repository's
drain for good, and the door answered `503` once the spool was full.

## Considered Options

- Set the request aside when its admission is refused for its own content,
  and keep 0167's retry for every other failure.
- Keep 0167 as it is and rely on the door refusing every such request.
- Set the request aside on any Postgres data exception (SQLSTATE class 22).

## Decision Outcome

Chosen: the first. A drain whose admission of a spooled request fails with
`ErrValidation`, or with SQLSTATE `22P05` (a NUL in text or jsonb) or `22021`
(bytes that are not valid UTF-8), renames the file `<name>.refused`, logs the
file and a fixed reason, and goes on to the next request
(`spoolAdmissionRefused` in `internal/engine/webhookspool.go`). Every other
admission failure, the rest of class 22 included, still stops the drain at
the request and retries it as 0167 says.

The door now refuses a NUL part name, so the set-aside is a backstop. Without
it, the next content refusal the door misses blocks a repository's webhooks
until an operator removes the file by hand. The rest of class 22 is left out
because the drain's own queries over stored rows (`webhookRecordedSQL`,
`blobRecordQuery`) can raise those codes, and an engine fault would then set
aside every request instead of stopping the drain.

A `.refused` file is kept for inspection. The spool caps do not count it, the
restart does not re-read it, and snapshots and exports do not carry it,
exactly as for 0167's `.unreadable` file. Nothing deletes it yet.

This amends the stop-and-retry rule of 0167 for these errors only; the rest
of 0167 stands.

### Consequences

- Good, because one request the engine can never store no longer blocks
  every later request of its repository.
- Bad, because the sender's `202` for a set-aside request is not honored: its
  fire id never resolves, and no API lists the request.
- Bad, because `ErrValidation` is wider than the request's content. A
  validation fault in the engine itself would set requests aside rather than
  stop the drain.
- Bad, because `.refused` files stay on disk outside the spool caps until an
  operator removes them, and only a log line marks each one.
  `substrate_webhook_spool_drain_failures_total` counts a set-aside and a
  retry alike.

### Confirmation

`TestSpoolRefusedClassifiesOnlyContentRefusals` (internal/engine, internal
package) pins which errors set a request aside and that other class 22 codes
and connection errors do not. `TestNULSpoolPoisonIsSetAside` and
`TestNULSpoolPartRefusedAtTheDoor` (internal/engine) and
`TestWebhookNULPartNameRefused` (internal/api) hold the drain and the door.

## More Information

Reopen if a set-aside needs an owner-visible signal (a core `alert`), a
cleanup policy, or a retry by hand.
